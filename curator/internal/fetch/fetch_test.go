package fetch_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/catalog"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/dbtest"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/fetch"
)

// fakeClaude answers each call with the next response, recording the prompts.
type fakeClaude struct {
	responses []any
	prompts   []string
}

func (f *fakeClaude) Run(_ context.Context, req claude.Request) (claude.Result, error) {
	f.prompts = append(f.prompts, req.Prompt)
	if len(f.responses) == 0 {
		return claude.Result{}, errors.New("no more responses")
	}
	next := f.responses[0]
	f.responses = f.responses[1:]
	if err, ok := next.(error); ok {
		return claude.Result{}, err
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return claude.Result{}, err
	}
	return claude.Result{Output: raw, Model: "claude-test", CostUSD: 0.5}, nil
}

func setup(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := dbtest.New(t)
	cat, err := catalog.Parse([]byte(`
professions:
  - {slug: engineer, name: Engineer, topics: [languages, security], launch: true}
topics:
  - slug: languages
    name: Languages
    priority: 3
    children:
      - {slug: languages/go, name: Go, hints: [https://go.dev/blog/feed.atom]}
  - {slug: security, name: Security, priority: 3}
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Seed(t.Context(), pool, cat); err != nil {
		t.Fatal(err)
	}
	return pool
}

func story(title, url string, published time.Time, mutate func(map[string]any)) map[string]any {
	s := map[string]any{
		"title": title, "summary": "s", "body_md": "b", "kind": "release", "severity": "none", "importance": 2,
		"published_at": published.UTC().Format(time.RFC3339),
		"topics":       []string{"languages/go"},
		"sources":      []map[string]string{{"url": url, "name": "Go Blog"}},
		"dedupe":       map[string]any{"project": "", "version": "", "cve_ids": []string{}},
	}
	if mutate != nil {
		mutate(s)
	}
	return s
}

func testConfig() config.Config {
	return config.Config{
		MaxCallsPerRun: 5, TopicsPerCall: 5, ItemMaxAge: 14 * 24 * time.Hour,
		MergeWindow: 14 * 24 * time.Hour, StoriesPerTopic: 5, Concurrency: 1,
		HotMinEngaged: 1, WarmInterval: 24 * time.Hour,
		PriorityIntervals: [5]time.Duration{48 * time.Hour, 96 * time.Hour, 168 * time.Hour, 336 * time.Hour, 672 * time.Hour},
		DealsMaxCountries: 5, DealsMaxCallsPerRun: 4, DealsMaxAge: 7 * 24 * time.Hour,
	}
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func TestFetchStoresAndMerges(t *testing.T) {
	pool := setup(t)
	ctx := t.Context()
	now := time.Now()

	fake := &fakeClaude{responses: []any{map[string]any{"stories": []any{
		story("Go 1.27", "https://go.dev/blog/go1.27", now.Add(-time.Hour), func(s map[string]any) {
			s["dedupe"] = map[string]any{"project": "go", "version": "1.27.0", "cve_ids": []string{}}
		}),
		// Same release from another page: merged by project+version.
		story("Go 1.27 is out", "https://github.com/golang/go/releases/tag/go1.27", now.Add(-time.Hour), func(s map[string]any) {
			s["importance"] = 4
			s["topics"] = []string{"languages/go", "security"}
			s["dedupe"] = map[string]any{"project": "Go", "version": "v1.27.0", "cve_ids": []string{}}
		}),
		// Shares only the hint URL with the first story: not merged.
		story("Go survey results", "https://go.dev/blog/feed.atom", now.Add(-time.Hour), nil),
		// Two stories citing the same aggregator page in one answer: both kept.
		story("Gopls v0.20", "https://news.example.com/weekly", now.Add(-time.Hour), nil),
		story("Delve 1.30", "https://news.example.com/weekly", now.Add(-time.Hour), nil),
		// Too old: rejected, the rest still stored.
		story("Ancient", "https://go.dev/blog/old", now.Add(-30*24*time.Hour), nil),
	}}}}
	f := fetch.New(pool, fake, testConfig())

	sum, err := f.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Both topics are leaves (languages has a child), never fetched, so one call covers them.
	if sum.Calls != 1 || sum.Added != 4 || sum.Merged != 1 || sum.Rejected != 1 || sum.CostUSD != 0.5 {
		t.Fatalf("summary = %+v", sum)
	}
	if !strings.Contains(fake.prompts[0], "hints: https://go.dev/blog/feed.atom") {
		t.Errorf("prompt lacks hints:\n%s", fake.prompts[0])
	}

	var importance, sources, topics int
	var model, promptVersion string
	if err := pool.QueryRow(ctx, `
		SELECT s.importance, s.model, s.prompt_version,
			(SELECT count(*) FROM story_sources WHERE story_id = s.id),
			(SELECT count(*) FROM story_topics WHERE story_id = s.id)
		FROM stories s WHERE s.title = 'Go 1.27'`).Scan(&importance, &model, &promptVersion, &sources, &topics); err != nil {
		t.Fatal(err)
	}
	if importance != 4 || sources != 2 || topics != 2 || model != "claude-test" || promptVersion != fetch.PromptVersion {
		t.Errorf("merged story: importance %d, sources %d, topics %d, model %q, prompt %q", importance, sources, topics, model, promptVersion)
	}
	if n := count(t, pool, `SELECT count(*) FROM fetch_runs WHERE status = 'succeeded' AND stories_added = 4 AND cost_usd = 0.5 AND cardinality(topic_ids) = 2`); n != 1 {
		t.Errorf("succeeded fetch runs = %d", n)
	}

	// Topics without demand were just fetched, so nothing is due.
	sum, err = f.Run(ctx)
	if err != nil || sum.Calls != 0 {
		t.Fatalf("second run: %+v, %v", sum, err)
	}

	// Users who opened or saved its stories make the topic hot, so it is due again; known stories go into the prompt, and a story
	// with an already stored URL is merged rather than duplicated.
	if _, err := pool.Exec(ctx, `INSERT INTO topic_stats (topic_id, env, engaged_7d) SELECT id, 'staging', 3 FROM topics WHERE slug = 'languages'`); err != nil {
		t.Fatal(err)
	}
	fake.responses = []any{map[string]any{"stories": []any{
		story("Go 1.27 (again)", "https://go.dev/blog/go1.27?utm_source=feed", now, nil),
	}}}
	sum, err = f.Run(ctx)
	if err != nil || sum.Calls != 1 || sum.Merged != 1 || sum.Added != 0 {
		t.Fatalf("third run: %+v, %v", sum, err)
	}
	if !strings.Contains(fake.prompts[1], "Go 1.27 (https://") {
		t.Errorf("prompt lacks known stories:\n%s", fake.prompts[1])
	}
	if n := count(t, pool, `SELECT count(*) FROM stories`); n != 4 {
		t.Errorf("stories = %d, want 4", n)
	}
}

func TestFetchCompactPrompt(t *testing.T) {
	pool := setup(t)
	ctx := t.Context()

	fake := &fakeClaude{responses: []any{map[string]any{"stories": []any{
		story("Go 1.27", "https://go.dev/blog/go1.27", time.Now().Add(-time.Hour), nil),
	}}}}
	cfg := testConfig()
	cfg.PromptStyle = config.PromptCompact
	if _, err := fetch.New(pool, fake, cfg).Run(ctx); err != nil {
		t.Fatal(err)
	}
	p := fake.prompts[0]
	if !strings.Contains(p, "## Steps") || !strings.Contains(p, "Do not put dates in queries") || !strings.Contains(p, "hints: https://go.dev/blog/feed.atom") {
		t.Errorf("compact prompt lacks steps, query rule or hints:\n%s", p)
	}
	if strings.Contains(p, "## Deals") {
		t.Errorf("compact prompt has the deals section without deal topics:\n%s", p)
	}
	if n := count(t, pool, `SELECT count(*) FROM stories WHERE prompt_version = $1`, fetch.PromptVersionCompact); n != 1 {
		t.Errorf("compact stories = %d", n)
	}
}

func TestFetchRecordsFailure(t *testing.T) {
	pool := setup(t)
	fake := &fakeClaude{responses: []any{errors.New("claude call failed (error_max_turns)")}}

	sum, err := fetch.New(pool, fake, testConfig()).Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "error_max_turns") || sum.Failed != 1 {
		t.Fatalf("summary %+v, err %v", sum, err)
	}
	if n := count(t, pool, `SELECT count(*) FROM fetch_runs WHERE status = 'failed' AND error LIKE '%error_max_turns%' AND finished_at IS NOT NULL`); n != 1 {
		t.Errorf("failed fetch runs = %d", n)
	}
	// A failed call does not count as a fetch: the topics are still due.
	fake.responses = []any{map[string]any{"stories": []any{}}}
	if sum, err := fetch.New(pool, fake, testConfig()).Run(t.Context()); err != nil || sum.Calls != 1 {
		t.Fatalf("retry: %+v, %v", sum, err)
	}
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

type overlapClaude struct {
	inflight, maxInflight atomic.Int32
	story                 map[string]any
}

func (o *overlapClaude) Run(_ context.Context, _ claude.Request) (claude.Result, error) {
	n := o.inflight.Add(1)
	defer o.inflight.Add(-1)
	for {
		m := o.maxInflight.Load()
		if n <= m || o.maxInflight.CompareAndSwap(m, n) {
			break
		}
	}
	time.Sleep(100 * time.Millisecond)
	raw, err := json.Marshal(map[string]any{"stories": []any{o.story}})
	return claude.Result{Output: raw, Model: "claude-test"}, err
}

func TestFetchConcurrentCallsDedupe(t *testing.T) {
	pool := setup(t)
	cfg := testConfig()
	cfg.TopicsPerCall = 1
	cfg.Concurrency = 2

	// Both calls (one per topic) find the same release: the serialised stores add it once.
	fake := &overlapClaude{story: story("Go 1.27", "https://go.dev/blog/go1.27", time.Now().Add(-time.Hour), func(s map[string]any) {
		s["dedupe"] = map[string]any{"project": "go", "version": "1.27.0", "cve_ids": []string{}}
	})}
	sum, err := fetch.New(pool, fake, cfg).Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Calls != 2 || sum.Added != 1 || sum.Merged != 1 {
		t.Errorf("summary = %+v, want 2 calls, 1 added, 1 merged", sum)
	}
	if got := fake.maxInflight.Load(); got != 2 {
		t.Errorf("calls in flight at once = %d, want 2", got)
	}
	if n := count(t, pool, `SELECT count(*) FROM stories`); n != 1 {
		t.Errorf("stories = %d, want 1", n)
	}
}

func TestFetchDealsPerCountry(t *testing.T) {
	pool := dbtest.New(t)
	ctx := t.Context()
	now := time.Now()
	cat, err := catalog.Parse([]byte(`
professions:
  - {slug: enthusiast, name: Tech Enthusiast, topics: [deals], launch: true}
topics:
  - slug: deals
    name: Deals
    priority: 3
    children:
      - {slug: deals/laptops, name: Laptop Deals}
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Seed(ctx, pool, cat); err != nil {
		t.Fatal(err)
	}
	f := fetch.New(pool, &fakeClaude{}, testConfig())

	// No user chose a country yet: nothing to fetch.
	if sum, err := f.Run(ctx); err != nil || sum.Calls != 0 {
		t.Fatalf("no countries: summary %+v, err %v", sum, err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO country_stats (env, country, users) VALUES ('prod', 'BD', 3), ('prod', 'US', 1)`); err != nil {
		t.Fatal(err)
	}
	deal := func(title string, countries []string) map[string]any {
		return story(title, "https://shop.example.com/laptop", now.Add(-time.Hour), func(s map[string]any) {
			s["kind"] = "deal"
			s["topics"] = []string{"deals/laptops"}
			s["countries"] = countries
		})
	}
	fake := &fakeClaude{responses: []any{
		// BD: no countries defaults to BD.
		map[string]any{"stories": []any{deal("Laptop deal BD", nil)}},
		// US: same URL still a separate story; a deal only valid in BD and one with a bad code are rejected.
		map[string]any{"stories": []any{deal("Laptop deal US", []string{"us"}), deal("Not for US", []string{"BD"}), deal("Bad code", []string{"ZZ"})}},
	}}
	f = fetch.New(pool, fake, testConfig())

	sum, err := f.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Calls != 2 || sum.Added != 2 || sum.Merged != 0 || sum.Rejected != 2 {
		t.Fatalf("summary = %+v", sum)
	}
	if !strings.Contains(fake.prompts[0], "Bangladesh (BD)") || !strings.Contains(fake.prompts[1], "United States (US)") {
		t.Errorf("prompts lack the country:\n%s\n%s", fake.prompts[0], fake.prompts[1])
	}
	for title, want := range map[string]string{"Laptop deal BD": "{BD}", "Laptop deal US": "{US}"} {
		var got string
		if err := pool.QueryRow(ctx, `SELECT countries::text FROM stories WHERE title = $1`, title).Scan(&got); err != nil || got != want {
			t.Errorf("%q countries = %q, err %v; want %q", title, got, err, want)
		}
	}
	if n := count(t, pool, `SELECT count(*) FROM fetch_runs WHERE country IN ('BD', 'US') AND status = 'succeeded'`); n != 2 {
		t.Errorf("country fetch runs = %d, want 2", n)
	}

	// Both countries were just fetched: not due again.
	f = fetch.New(pool, &fakeClaude{}, testConfig())
	if sum, err := f.Run(ctx); err != nil || sum.Calls != 0 {
		t.Fatalf("second run: summary %+v, err %v", sum, err)
	}
}

func TestFetchStaysInTheRankBand(t *testing.T) {
	// Both leaf topics have priority 3 and no demand, so the rank is by slug: languages/go, then security.
	pool := setup(t)
	empty := func() *fakeClaude { return &fakeClaude{responses: []any{map[string]any{"stories": []any{}}}} }

	tests := []struct {
		band         string
		wantGo, want bool
	}{
		{band: "1-1", wantGo: true},
		{band: "2-", want: true},
	}
	for _, tc := range tests {
		band, err := fetch.ParseBand(tc.band)
		if err != nil {
			t.Fatal(err)
		}
		// A fetch marks its topics fetched, so reset that for the next case.
		exec(t, pool, `DELETE FROM fetch_runs`)
		fake := empty()
		if sum, err := fetch.New(pool, fake, testConfig()).WithBand(band).Run(t.Context()); err != nil || sum.Calls != 1 {
			t.Fatalf("fetch %s: %+v, %v", tc.band, sum, err)
		}
		p := fake.prompts[0]
		if got := strings.Contains(p, "- languages/go:"); got != tc.wantGo {
			t.Errorf("fetch %s: prompt has languages/go = %v, want %v:\n%s", tc.band, got, tc.wantGo, p)
		}
		if got := strings.Contains(p, "- security:"); got != tc.want {
			t.Errorf("fetch %s: prompt has security = %v, want %v:\n%s", tc.band, got, tc.want, p)
		}
	}

	exec(t, pool, `DELETE FROM fetch_runs`)
	fake := empty()
	if sum, err := fetch.New(pool, fake, testConfig()).WithBand(fetch.Band{From: 3}).Run(t.Context()); err != nil || sum.Calls != 0 || len(fake.prompts) != 0 {
		t.Errorf("fetch beyond the last rank: %+v, %v, %d prompts", sum, err, len(fake.prompts))
	}
}

func TestLeastFirstTakesTheLowestPriorityTopicFirst(t *testing.T) {
	pool := setup(t)
	// languages/go keeps priority 3; security becomes priority 1, so it is the more important one.
	exec(t, pool, `UPDATE topic_priority SET priority = 1 WHERE topic_id = (SELECT id FROM topics WHERE slug = 'security')`)
	cfg := testConfig()
	cfg.MaxCallsPerRun, cfg.TopicsPerCall = 1, 1

	for _, leastFirst := range []bool{false, true} {
		exec(t, pool, `DELETE FROM fetch_runs`)
		fake := &fakeClaude{responses: []any{map[string]any{"stories": []any{}}}}
		f := fetch.New(pool, fake, cfg).WithBand(fetch.Band{LeastFirst: leastFirst})
		if sum, err := f.Run(t.Context()); err != nil || sum.Calls != 1 {
			t.Fatalf("least-first=%v: %+v, %v", leastFirst, sum, err)
		}
		wantSecurity := !leastFirst
		if got := strings.Contains(fake.prompts[0], "- security:"); got != wantSecurity {
			t.Errorf("least-first=%v: prompt has security = %v, want %v", leastFirst, got, wantSecurity)
		}
	}
}
