// Package fetch finds new stories for the topic catalog with Claude and stores them in the local
// database.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// catalog is the topic data shared by all calls of a run.
type catalog struct {
	slugs    []string
	valid    map[string]bool
	topicIDs map[string]int64
	hintURLs map[string]bool
}

// Run plans this run's calls and makes them one after another. A failed call is recorded in
// fetch_runs and does not stop the others; the returned error joins all failures.
func (f *Fetcher) Run(ctx context.Context) (Summary, error) {
	rows, err := db.New(f.pool).ListFetchTopics(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("list topics: %w", err)
	}
	if len(rows) == 0 {
		return Summary{}, errors.New("no topics in the local DB (run `curator seed` first)")
	}

	cat := catalog{valid: map[string]bool{}, topicIDs: map[string]int64{}, hintURLs: map[string]bool{}}
	topics := make([]Topic, len(rows))
	for i, r := range rows {
		topics[i] = Topic{
			ID: r.ID, Slug: r.Slug, Name: r.Name, Description: r.Description, Followers: int(r.Followers),
			HasChildren: r.HasChildren, Hints: r.Hints, LastFetchedAt: r.LastFetchedAt,
		}
		if r.ParentSlug != nil {
			topics[i].ParentSlug = *r.ParentSlug
		}
		cat.slugs = append(cat.slugs, r.Slug)
		cat.valid[r.Slug] = true
		cat.topicIDs[r.Slug] = r.ID
		for _, h := range r.Hints {
			if u, err := urlnorm.Normalize(h); err == nil {
				cat.hintURLs[u] = true
			}
		}
	}

	groups, deferred := Plan(topics, PlanSettings{
		TopicsPerCall:      f.cfg.TopicsPerCall,
		MaxCalls:           f.cfg.MaxCallsPerRun,
		UnfollowedInterval: f.cfg.UnfollowedInterval,
		MaxAge:             f.cfg.ItemMaxAge,
	}, f.now())
	sum := Summary{Deferred: deferred}
	if len(deferred) > 0 {
		slog.InfoContext(ctx, "due topics deferred to a later run (raise CURATOR_MAX_CALLS_PER_RUN to fetch more)", "topics", deferred)
	}
	if len(groups) == 0 {
		slog.InfoContext(ctx, "no topics are due for a fetch")
		return sum, nil
	}

	var errs []error
	for _, g := range groups {
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
		sum.Calls++
		res, err := f.runGroup(ctx, g, cat)
		sum.CostUSD += res.cost
		sum.Added += res.added
		sum.Merged += res.merged
		sum.Rejected += res.rejected
		if err != nil {
			sum.Failed++
			slog.ErrorContext(ctx, "fetch call failed", "group", g.Slug, "err", err)
			errs = append(errs, fmt.Errorf("group %s: %w", g.Slug, err))
		}
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
	runID, err := q.StartFetchRun(ctx, db.StartFetchRunParams{GroupSlug: g.Slug, TopicIds: ids})
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
		Since: now.Add(-f.cfg.ItemMaxAge), TopicIds: ids, MaxRows: maxKnownStories,
	})
	if err != nil {
		return fail(fmt.Errorf("list recent stories: %w", err))
	}
	known := make([]StoryRef, len(refs))
	for i, r := range refs {
		known[i] = StoryRef{Title: r.Title, URL: r.Url}
	}

	slog.InfoContext(ctx, "fetch call started", "group", g.Slug, "topics", slugs, "since", g.Since.UTC().Format(time.RFC3339), "known_stories", len(known))
	out, err := f.claude.Run(ctx, claude.Request{Prompt: Prompt(g, known), Schema: Schema(cat.slugs), Tools: tools})
	if err != nil {
		return fail(err)
	}
	res.cost = out.CostUSD

	stories, rejected, err := ParseOutput(out.Output, cat.valid, f.now(), f.cfg.ItemMaxAge)
	if err != nil {
		return fail(err)
	}
	res.rejected = len(rejected)
	for _, r := range rejected {
		slog.WarnContext(ctx, "story rejected", "group", g.Slug, "reason", r)
	}

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
		"output_tokens", out.Usage.OutputTokens, "model", out.Model)
	return res, nil
}
