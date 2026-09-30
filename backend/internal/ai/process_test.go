package ai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/ai"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/dbtest"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/topics"
	"github.com/saniuzzaman-robin/changeloom/backend/seed"
)

type fakeClient struct {
	submitted []ai.Request
	results   []ai.Result
	ended     bool
}

func (f *fakeClient) SubmitBatch(_ context.Context, reqs []ai.Request) (string, error) {
	f.submitted = append(f.submitted, reqs...)
	return fmt.Sprintf("msgbatch_%d", len(f.submitted)), nil
}

func (f *fakeClient) GetBatch(context.Context, string) (ai.BatchStatus, error) {
	if f.ended {
		return ai.BatchStatus{Status: "ended", Ended: true}, nil
	}
	return ai.BatchStatus{Status: "in_progress"}, nil
}

func (f *fakeClient) BatchResults(context.Context, string) ([]ai.Result, error) {
	return f.results, nil
}

func (f *fakeClient) Complete(_ context.Context, req ai.Request) (ai.Result, error) {
	return ai.Result{CustomID: req.CustomID, Outcome: ai.OutcomeSucceeded, Text: storyJSON("Go 1.25 released", "go", "1.25.0", nil), StopReason: "end_turn"}, nil
}

func storyJSON(title, project, version string, cves []string) string {
	out := ai.Output{
		Relevant: true, Kind: "release", Severity: "none", Importance: 3, Topics: []string{"languages/go"},
		Title: title, Summary: "A summary.", BodyMD: "## What changed\nThings.",
		Dedupe: ai.Dedupe{Project: project, Version: version, CVEIDs: cves},
	}
	if cves == nil {
		out.Dedupe.CVEIDs = []string{}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func settings() ai.Settings {
	return ai.Settings{Model: "test-model", DailyTokenBudget: 1_000_000, BatchMaxItems: 50, MaxAttempts: 2, InputMaxChars: 100, MergeWindow: 7 * 24 * time.Hour}
}

// setup returns a migrated database with topics, one source and the given raw items.
func setup(t *testing.T, contents ...string) (*pgxpool.Pool, []int64) {
	t.Helper()
	pool := dbtest.New(t)
	ctx := t.Context()
	if _, err := topics.Sync(ctx, pool, seed.TopicsYAML); err != nil {
		t.Fatalf("sync topics: %v", err)
	}
	var sourceID int64
	err := pool.QueryRow(ctx, `INSERT INTO sources (name, kind, poll_interval) VALUES ('Go Blog', 'rss', '1 hour') RETURNING id`).Scan(&sourceID)
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}
	ids := make([]int64, len(contents))
	for i, c := range contents {
		err := pool.QueryRow(ctx, `INSERT INTO raw_items (source_id, url, url_hash, title, content, published_at)
			VALUES ($1, $2, $3, $4, $5, now() - make_interval(hours => $6::int)) RETURNING id`,
			sourceID, fmt.Sprintf("https://example.com/%d", i), []byte(fmt.Sprintf("hash-%d", i)), fmt.Sprintf("Item %d", i), c, i).Scan(&ids[i])
		if err != nil {
			t.Fatalf("insert raw item: %v", err)
		}
	}
	return pool, ids
}

func rawStatus(t *testing.T, pool *pgxpool.Pool, id int64) (status string, attempts int, errMsg *string) {
	t.Helper()
	err := pool.QueryRow(t.Context(), `SELECT status, attempts, error FROM raw_items WHERE id = $1`, id).Scan(&status, &attempts, &errMsg)
	if err != nil {
		t.Fatalf("read raw item %d: %v", id, err)
	}
	return status, attempts, errMsg
}

func count(t *testing.T, pool *pgxpool.Pool, query string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestSubmitAndPollAppliesResults(t *testing.T) {
	long := strings.Repeat("x", 500)
	pool, ids := setup(t, "a", "b", "c", "d", long)
	fc := &fakeClient{}
	p := ai.NewProcessor(pool, fc, settings())

	n, err := p.Submit(t.Context())
	if err != nil || n != 5 {
		t.Fatalf("Submit = %d, %v; want 5, nil", n, err)
	}
	if len(fc.submitted) != 5 {
		t.Fatalf("submitted %d requests, want 5", len(fc.submitted))
	}
	if st, _, _ := rawStatus(t, pool, ids[0]); st != "submitted" {
		t.Fatalf("status after submit = %s, want submitted", st)
	}
	// A second submit has nothing pending.
	if n, err := p.Submit(t.Context()); err != nil || n != 0 {
		t.Fatalf("second Submit = %d, %v; want 0, nil", n, err)
	}

	// Batch still running: nothing changes.
	if err := p.Poll(t.Context()); err != nil {
		t.Fatalf("Poll (running): %v", err)
	}
	if st, _, _ := rawStatus(t, pool, ids[0]); st != "submitted" {
		t.Fatalf("status while running = %s, want submitted", st)
	}

	fc.ended = true
	usage := ai.Usage{Input: 100, Output: 50}
	fc.results = []ai.Result{
		{CustomID: fmt.Sprintf("raw-%d", ids[0]), Outcome: ai.OutcomeSucceeded, StopReason: "end_turn", Usage: usage,
			Text: storyJSON("Go 1.25 released", "Go", "v1.25.0", []string{"cve-2026-1"})},
		// Same CVE, different wording: merges into the first story.
		{CustomID: fmt.Sprintf("raw-%d", ids[1]), Outcome: ai.OutcomeSucceeded, StopReason: "end_turn", Usage: usage,
			Text: storyJSON("Go patch", "", "", []string{"CVE-2026-1"})},
		{CustomID: fmt.Sprintf("raw-%d", ids[2]), Outcome: ai.OutcomeSucceeded, StopReason: "end_turn", Usage: usage,
			Text: `{"relevant":false,"reject_reason":"marketing","kind":"article","severity":"none","importance":1,"topics":[],"title":"","summary":"","body_md":"","dedupe":{"project":"","version":"","cve_ids":[]}}`},
		{CustomID: fmt.Sprintf("raw-%d", ids[3]), Outcome: ai.OutcomeSucceeded, StopReason: "refusal"},
		{CustomID: fmt.Sprintf("raw-%d", ids[4]), Outcome: ai.OutcomeErrored, Error: "overloaded"},
	}
	if err := p.Poll(t.Context()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	for i, want := range []string{"processed", "processed", "skipped", "needs_review", "pending"} {
		if st, _, _ := rawStatus(t, pool, ids[i]); st != want {
			t.Errorf("item %d status = %s, want %s", i, st, want)
		}
	}
	if _, attempts, msg := rawStatus(t, pool, ids[4]); attempts != 1 || msg == nil || !strings.Contains(*msg, "overloaded") {
		t.Errorf("errored item attempts=%d error=%v, want 1 and overloaded", attempts, msg)
	}
	if n := count(t, pool, `SELECT count(*) FROM stories`); n != 1 {
		t.Errorf("stories = %d, want 1 (second item merged)", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM story_sources`); n != 2 {
		t.Errorf("story_sources = %d, want 2", n)
	}
	if n := count(t, pool, `SELECT count(*) FROM story_topics st JOIN topics t ON t.id = st.topic_id WHERE t.slug = 'languages/go'`); n != 1 {
		t.Errorf("story topics = %d, want 1", n)
	}
	var model, promptVersion string
	if err := pool.QueryRow(t.Context(), `SELECT model, prompt_version FROM stories`).Scan(&model, &promptVersion); err != nil || model != "test-model" || promptVersion != ai.PromptVersion {
		t.Errorf("story model=%q prompt_version=%q err=%v", model, promptVersion, err)
	}
	var in, out int64
	var ended bool
	if err := pool.QueryRow(t.Context(), `SELECT input_tokens, output_tokens, ended_at IS NOT NULL FROM ai_batches`).Scan(&in, &out, &ended); err != nil {
		t.Fatal(err)
	}
	if in != 300 || out != 150 || !ended {
		t.Errorf("batch usage input=%d output=%d ended=%t, want 300 150 true", in, out, ended)
	}

	// The errored item is retried and, at MaxAttempts, becomes failed.
	fc.ended = false
	if n, err := p.Submit(t.Context()); err != nil || n != 1 {
		t.Fatalf("resubmit = %d, %v; want 1, nil", n, err)
	}
	fc.ended = true
	fc.results = []ai.Result{{CustomID: fmt.Sprintf("raw-%d", ids[4]), Outcome: ai.OutcomeExpired, Error: "expired"}}
	if err := p.Poll(t.Context()); err != nil {
		t.Fatalf("Poll (retry): %v", err)
	}
	if st, attempts, _ := rawStatus(t, pool, ids[4]); st != "failed" || attempts != 2 {
		t.Errorf("after max attempts status=%s attempts=%d, want failed 2", st, attempts)
	}
}

func TestSubmitTrimsContentAndUsesCachedSystemPrompt(t *testing.T) {
	pool, _ := setup(t, strings.Repeat("y", 500))
	fc := &fakeClient{}
	if _, err := ai.NewProcessor(pool, fc, settings()).Submit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(fc.submitted[0].User, "y"); got != 100 {
		t.Errorf("content sent = %d chars, want 100 (InputMaxChars)", got)
	}
	if !strings.Contains(fc.submitted[0].User, "cut for length") {
		t.Error("trimmed content should tell the model it was cut")
	}
	if !strings.Contains(fc.submitted[0].System, "languages/go") {
		t.Error("system prompt should list topic slugs")
	}
}

func TestSubmitRespectsDailyBudget(t *testing.T) {
	pool, _ := setup(t, "a", "b", "c")
	fc := &fakeClient{}
	s := settings()
	s.DailyTokenBudget = 1 // nothing fits
	n, err := ai.NewProcessor(pool, fc, s).Submit(t.Context())
	if err != nil || n != 0 || len(fc.submitted) != 0 {
		t.Fatalf("Submit over budget = %d, %v (%d requests); want 0, nil, 0", n, err, len(fc.submitted))
	}
	if got := count(t, pool, `SELECT count(*) FROM raw_items WHERE status = 'pending'`); got != 3 {
		t.Errorf("pending items = %d, want 3", got)
	}

	// A running batch counts against the budget through its estimate.
	s.DailyTokenBudget = 1_000_000
	p := ai.NewProcessor(pool, fc, s)
	if _, err := p.Submit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE ai_batches SET estimated_tokens = 1000000`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `UPDATE raw_items SET status = 'pending', ai_batch_id = NULL`); err != nil {
		t.Fatal(err)
	}
	if n, err := p.Submit(t.Context()); err != nil || n != 0 {
		t.Errorf("Submit with budget used up = %d, %v; want 0, nil", n, err)
	}
}

func TestProcessOneDryRunStoresNothingAndApplyStores(t *testing.T) {
	pool, ids := setup(t, "a")
	p := ai.NewProcessor(pool, &fakeClient{}, settings())

	prev, err := p.ProcessOne(t.Context(), ids[0], false)
	if err != nil || prev.ParseErr != nil || prev.Output.Title != "Go 1.25 released" {
		t.Fatalf("dry run = %+v, %v", prev, err)
	}
	if n := count(t, pool, `SELECT count(*) FROM stories`); n != 0 {
		t.Fatalf("dry run stored %d stories", n)
	}
	if _, err := p.ProcessOne(t.Context(), ids[0], true); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := rawStatus(t, pool, ids[0]); st != "processed" {
		t.Errorf("status after apply = %s, want processed", st)
	}
	if n := count(t, pool, `SELECT count(*) FROM stories`); n != 1 {
		t.Errorf("stories = %d, want 1", n)
	}
}

func TestParseOutputRejectsBadAnswers(t *testing.T) {
	valid := map[string]bool{"languages/go": true}
	good := storyJSON("t", "go", "1", nil)
	if _, err := ai.ParseOutput(good, valid); err != nil {
		t.Fatalf("good output rejected: %v", err)
	}
	long := strings.Replace(good, `"A summary."`, `"`+strings.Repeat("s", 281)+`"`, 1)
	for name, text := range map[string]string{
		"not json":     "nope",
		"long summary": long,
		"bad topic":    strings.Replace(good, "languages/go", "made/up", 1),
		"bad kind":     strings.Replace(good, `"release"`, `"news"`, 1),
	} {
		if _, err := ai.ParseOutput(text, valid); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
