package fetch_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	nodes, err := catalog.Parse([]byte(`
- slug: languages
  name: Languages
  children:
    - {slug: languages/go, name: Go, hints: [https://go.dev/blog/feed.atom]}
- {slug: security, name: Security}
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Seed(t.Context(), pool, nodes); err != nil {
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
		MaxCallsPerRun: 5, TopicsPerCall: 5, ItemMaxAge: 7 * 24 * time.Hour,
		MergeWindow: 14 * 24 * time.Hour, UnfollowedInterval: 24 * time.Hour,
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

	// Unfollowed topics were just fetched, so nothing is due.
	sum, err = f.Run(ctx)
	if err != nil || sum.Calls != 0 {
		t.Fatalf("second run: %+v, %v", sum, err)
	}

	// A follower makes the topic due again; known stories go into the prompt, and a story with an
	// already stored URL is merged rather than duplicated.
	if _, err := pool.Exec(ctx, `INSERT INTO topic_stats (topic_id, env, followers) SELECT id, 'staging', 3 FROM topics WHERE slug = 'languages'`); err != nil {
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
