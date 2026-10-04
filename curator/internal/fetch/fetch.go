// Package fetch finds new stories for the topic catalog with Claude and stores them in the local
// database.
package fetch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/db"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/urlnorm"
)

// maxKnownStories caps the existing stories listed in a prompt.
const maxKnownStories = 200

// tools are the only tools a fetch call may use.
var tools = []string{"WebSearch", "WebFetch"}

// Runner makes one Claude call; *claude.Client implements it.
type Runner interface {
	Run(ctx context.Context, req claude.Request) (claude.Result, error)
}

// Fetcher runs fetch calls.
type Fetcher struct {
	pool   *pgxpool.Pool
	claude Runner
	cfg    config.Config
	now    func() time.Time
	// storeMu serialises the store transactions of concurrent calls, so dedupe cannot race.
	storeMu sync.Mutex
}

// New returns a fetcher that writes to pool.
func New(pool *pgxpool.Pool, runner Runner, cfg config.Config) *Fetcher {
	return &Fetcher{pool: pool, claude: runner, cfg: cfg, now: time.Now}
}

// Summary describes a fetch run.
type Summary struct {
	Calls    int
	Failed   int
	Added    int
	Merged   int
	Rejected int
	// Deferred are due topics that did not fit in CURATOR_MAX_CALLS_PER_RUN calls.
	Deferred []string
	CostUSD  float64
}

func (s *Summary) add(r groupResult) {
	s.Added += r.added
	s.Merged += r.merged
	s.Rejected += r.rejected
	s.CostUSD += r.cost
}

// catalog is the topic data shared by all calls of a run.
type catalog struct {
	slugs    []string
	valid    map[string]bool
	topicIDs map[string]int64
	hintURLs map[string]bool
	parents  map[string]string
	related  map[string][]string
}

// also lists the slugs besides g's topics that a story of g may be tagged with: their parents
// and related topics.
func (c catalog) also(g Group) []string {
	in := map[string]bool{}
	for _, t := range g.Topics {
		in[t.Slug] = true
	}
	var out []string
	for _, t := range g.Topics {
		for _, slug := range append([]string{c.parents[t.Slug]}, c.related[t.Slug]...) {
			if slug != "" && !in[slug] {
				in[slug] = true
				out = append(out, slug)
			}
		}
	}
	slices.Sort(out)
	return out
}

func toTopic(r db.ListFetchTopicsRow) Topic {
	return Topic{
		ID: r.ID, Slug: r.Slug, Name: r.Name, Description: r.Description, ParentSlug: deref(r.ParentSlug),
		Followers: int(r.Followers), ProfessionUsers: int(r.ProfessionUsers), Views7d: int(r.Views7d),
		HasChildren: r.HasChildren, Hints: r.Hints, Professions: r.Professions, LastFetchedAt: r.LastFetchedAt,
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// load reads every topic and the data shared by all calls of a run.
func (f *Fetcher) load(ctx context.Context) ([]Topic, catalog, error) {
	q := db.New(f.pool)
	rows, err := q.ListFetchTopics(ctx)
	if err != nil {
		return nil, catalog{}, fmt.Errorf("list topics: %w", err)
	}
	if len(rows) == 0 {
		return nil, catalog{}, errors.New("no topics in the local DB (run `curator seed` first)")
	}
	relations, err := q.ListTopicRelations(ctx)
	if err != nil {
		return nil, catalog{}, fmt.Errorf("list topic relations: %w", err)
	}

	cat := catalog{
		valid: map[string]bool{}, topicIDs: map[string]int64{}, hintURLs: map[string]bool{},
		parents: map[string]string{}, related: map[string][]string{},
	}
	topics := make([]Topic, len(rows))
	for i, r := range rows {
		topics[i] = toTopic(r)
		cat.slugs = append(cat.slugs, r.Slug)
		cat.valid[r.Slug] = true
		cat.topicIDs[r.Slug] = r.ID
		cat.parents[r.Slug] = topics[i].ParentSlug
		for _, h := range r.Hints {
			if u, err := urlnorm.Normalize(h); err == nil {
				cat.hintURLs[u] = true
			}
		}
	}
	for _, r := range relations {
		cat.related[r.Slug] = append(cat.related[r.Slug], r.RelatedSlug)
	}
	return topics, cat, nil
}

// Run plans this run's calls and makes them, up to CURATOR_CONCURRENCY at a time. A failed call
// is recorded in fetch_runs and does not stop the others; the returned error joins all failures.
func (f *Fetcher) Run(ctx context.Context) (Summary, error) {
	topics, cat, err := f.load(ctx)
	if err != nil {
		return Summary{}, err
	}

	var global, deals []Topic
	for _, t := range topics {
		if isDealSlug(t.Slug) {
			deals = append(deals, t)
		} else {
			global = append(global, t)
		}
	}
	settings := PlanSettings{
		TopicsPerCall:   f.cfg.TopicsPerCall,
		MaxCalls:        f.cfg.MaxCallsPerRun,
		StoriesPerTopic: f.cfg.StoriesPerTopic,
		HotMinViews:     f.cfg.HotMinViews,
		WarmInterval:    f.cfg.WarmInterval,
		ColdInterval:    f.cfg.ColdInterval,
		MaxAge:          f.cfg.ItemMaxAge,
	}
	groups, deferred := Plan(global, settings, f.now())
	dealGroups, dealDeferred, err := f.planDeals(ctx, deals, settings)
	if err != nil {
		return Summary{}, err
	}
	groups, deferred = append(groups, dealGroups...), append(deferred, dealDeferred...)
	if len(deferred) > 0 {
		slog.InfoContext(ctx, "due topics deferred to a later run (raise CURATOR_MAX_CALLS_PER_RUN to fetch more)", "topics", deferred)
	}
	if len(groups) == 0 {
		slog.InfoContext(ctx, "no topics are due for a fetch")
		return Summary{Deferred: deferred}, nil
	}
	sum, err := f.runGroups(ctx, groups, cat)
	sum.Deferred = deferred
	return sum, err
}

// isDealSlug reports whether slug is the deals topic or one below it. Deal topics are fetched per
// country, never globally.
func isDealSlug(slug string) bool {
	return slug == "deals" || strings.HasPrefix(slug, "deals/")
}

// planDeals plans the deal topics once for each of the countries with the most users, most users
// first, sharing CURATOR_DEALS_MAX_CALLS_PER_RUN calls. A topic's last fetch is per country. Deal
// topics are only fetched for countries users chose, so with none there are no calls.
func (f *Fetcher) planDeals(ctx context.Context, deals []Topic, s PlanSettings) (groups []Group, deferred []string, err error) {
	if len(deals) == 0 {
		return nil, nil, nil
	}
	q := db.New(f.pool)
	countries, err := q.ListActiveCountries(ctx, int32(f.cfg.DealsMaxCountries)) //nolint:gosec // bounded by config
	if err != nil {
		return nil, nil, fmt.Errorf("list countries: %w", err)
	}
	fetches, err := q.ListCountryFetches(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list country fetches: %w", err)
	}
	type key struct {
		topic   int64
		country string
	}
	last := make(map[key]time.Time, len(fetches))
	for _, r := range fetches {
		last[key{r.TopicID, r.Country}] = r.LastFetchedAt
	}

	s.MaxAge = f.cfg.DealsMaxAge
	budget := f.cfg.DealsMaxCallsPerRun
	for _, c := range countries {
		if _, ok := countryNames[c.Country]; !ok {
			slog.WarnContext(ctx, "skipping deals for an unknown country code", "country", c.Country)
			continue
		}
		topics := slices.Clone(deals)
		for i := range topics {
			topics[i].LastFetchedAt = cmp.Or(last[key{topics[i].ID, c.Country}], time.Unix(0, 0))
		}
		s.MaxCalls = budget
		gs, d := Plan(topics, s, f.now())
		for i := range gs {
			gs[i].Country = c.Country
			gs[i].Slug = c.Country + ":" + gs[i].Slug
		}
		budget -= len(gs)
		groups, deferred = append(groups, gs...), append(deferred, d...)
	}
	return groups, deferred, nil
}

// Backfill fills leaf topics that have fewer than CURATOR_BACKFILL_TARGET recent stories, with
// CURATOR_BACKFILL_TOPICS_PER_CALL topics per call and at most maxCalls calls. A topic a backfill
// call already covered within the item window is skipped, so repeated runs move on to the topics
// that are still short.
func (f *Fetcher) Backfill(ctx context.Context, maxCalls int) (Summary, error) {
	_, cat, err := f.load(ctx)
	if err != nil {
		return Summary{}, err
	}
	now := f.now()
	since := now.Add(-f.cfg.ItemMaxAge)
	rows, err := db.New(f.pool).ListBackfillTopics(ctx, db.ListBackfillTopicsParams{
		Since: since, Target: int64(f.cfg.BackfillTarget), MaxRows: int32(maxCalls * f.cfg.BackfillTopicsPerCall), //nolint:gosec // bounded by config
	})
	if err != nil {
		return Summary{}, fmt.Errorf("list topics to backfill: %w", err)
	}
	if len(rows) == 0 {
		slog.InfoContext(ctx, "no topics need a backfill")
		return Summary{}, nil
	}

	var groups []Group
	for chunk := range slices.Chunk(rows, f.cfg.BackfillTopicsPerCall) {
		g := Group{Since: since, PerTopic: f.cfg.BackfillTarget}
		slugs := make([]string, len(chunk))
		for i, r := range chunk {
			slugs[i] = r.Slug
			g.Topics = append(g.Topics, Topic{
				ID: r.ID, Slug: r.Slug, Name: r.Name, Description: r.Description, ParentSlug: deref(r.ParentSlug),
				Hints: r.Hints, Professions: r.Professions,
			})
		}
		g.Slug = "backfill:" + strings.Join(slugs, "+")
		groups = append(groups, g)
	}
	return f.runGroups(ctx, groups, cat)
}

// runGroups makes the calls of groups, at most CURATOR_CONCURRENCY at once.
func (f *Fetcher) runGroups(ctx context.Context, groups []Group, cat catalog) (Summary, error) {
	var (
		mu   sync.Mutex
		sum  Summary
		errs []error
		wg   sync.WaitGroup
	)
	sem := make(chan struct{}, max(1, f.cfg.Concurrency))
launch:
	for _, g := range groups {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break launch
		}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			res, err := f.runGroup(ctx, g, cat)
			mu.Lock()
			defer mu.Unlock()
			sum.Calls++
			sum.add(res)
			if err != nil {
				sum.Failed++
				slog.ErrorContext(ctx, "fetch call failed", "group", g.Slug, "err", err)
				errs = append(errs, fmt.Errorf("group %s: %w", g.Slug, err))
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		errs = append(errs, err)
	}
	return sum, errors.Join(errs...)
}

type groupResult struct {
	storeResult
	rejected int
	cost     float64
}

func (f *Fetcher) runGroup(ctx context.Context, g Group, cat catalog) (groupResult, error) {
	q := db.New(f.pool)
	ids := make([]int64, len(g.Topics))
	slugs := make([]string, len(g.Topics))
	for i, t := range g.Topics {
		ids[i], slugs[i] = t.ID, t.Slug
	}
	var country *string
	if g.Country != "" {
		country = &g.Country
	}
	runID, err := q.StartFetchRun(ctx, db.StartFetchRunParams{GroupSlug: g.Slug, TopicIds: ids, Country: country})
	if err != nil {
		return groupResult{}, fmt.Errorf("record fetch run: %w", err)
	}

	var res groupResult
	fail := func(err error) (groupResult, error) {
		msg := err.Error()
		if ferr := q.FinishFetchRun(context.WithoutCancel(ctx), db.FinishFetchRunParams{ID: runID, Status: "failed", Error: &msg, CostUsd: &res.cost}); ferr != nil {
			err = errors.Join(err, fmt.Errorf("record failed fetch run: %w", ferr))
		}
		return groupResult{rejected: res.rejected, cost: res.cost}, err
	}

	now := f.now()
	refs, err := q.ListRecentStoryRefs(ctx, db.ListRecentStoryRefsParams{
		Since: now.Add(-f.cfg.ItemMaxAge), TopicIds: ids, Country: g.Country, MaxRows: maxKnownStories,
	})
	if err != nil {
		return fail(fmt.Errorf("list recent stories: %w", err))
	}
	known := make([]StoryRef, len(refs))
	for i, r := range refs {
		known[i] = StoryRef{Title: r.Title, URL: r.Url}
	}

	g.Also = cat.also(g)
	slog.InfoContext(ctx, "fetch call started", "group", g.Slug, "topics", slugs, "since", g.Since.UTC().Format(time.RFC3339), "known_stories", len(known))
	out, err := f.claude.Run(ctx, claude.Request{Prompt: Prompt(g, known), Schema: Schema(slices.Concat(slugs, g.Also)), Tools: tools})
	if err != nil {
		return fail(err)
	}
	res.cost = out.CostUSD

	maxAge := f.cfg.ItemMaxAge
	if g.Country != "" {
		maxAge = f.cfg.DealsMaxAge
	}
	stories, rejected, err := ParseOutput(out.Output, cat.valid, f.now(), maxAge, g.Country)
	if err != nil {
		return fail(err)
	}
	res.rejected = len(rejected)
	for _, r := range rejected {
		slog.WarnContext(ctx, "story rejected", "group", g.Slug, "reason", r)
	}

	f.storeMu.Lock()
	defer f.storeMu.Unlock()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return fail(fmt.Errorf("begin: %w", err))
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	qtx := q.WithTx(tx)
	stored, err := storeStories(ctx, qtx, stories, storeOptions{
		topicIDs:   cat.topicIDs,
		hintURLs:   cat.hintURLs,
		mergeSince: now.Add(-f.cfg.MergeWindow),
		model:      out.Model,
		promptVer:  PromptVersion,
	})
	if err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return fail(err)
	}
	if err := qtx.FinishFetchRun(ctx, db.FinishFetchRunParams{
		ID: runID, Status: "succeeded", StoriesAdded: int32(stored.added), CostUsd: &res.cost, //nolint:gosec // bounded by the stories in one answer
	}); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return fail(fmt.Errorf("record fetch run: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return fail(fmt.Errorf("commit: %w", err))
	}
	res.storeResult = stored

	slog.InfoContext(ctx, "fetch call finished", "group", g.Slug, "added", stored.added, "merged", stored.merged,
		"rejected", res.rejected, "cost_usd", out.CostUSD, "turns", out.Turns, "duration", out.Duration.Round(time.Second),
		"output_tokens", out.Usage.OutputTokens, "model", out.Model, "account", out.Account)
	return res, nil
}
