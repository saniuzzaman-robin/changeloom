package ai_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/ai"
)

// answerClient answers every Complete call with a fixed text.
type answerClient struct {
	fakeClient
	text  string
	calls int
}

func (c *answerClient) Complete(_ context.Context, req ai.Request) (ai.Result, error) {
	c.calls++
	return ai.Result{CustomID: req.CustomID, Outcome: ai.OutcomeSucceeded, Text: c.text, StopReason: "end_turn"}, nil
}

func insertStory(t *testing.T, pool *pgxpool.Pool, title, topic string, importance int) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(t.Context(), `
		INSERT INTO stories (title, summary, body_md, kind, importance, published_at, model, prompt_version)
		VALUES ($1, 'summary', 'body', 'security', $2, now(), 'm', 'v0') RETURNING id`, title, importance).Scan(&id)
	if err != nil {
		t.Fatalf("insert story: %v", err)
	}
	ctx := t.Context()
	if _, err := pool.Exec(ctx, `INSERT INTO story_topics SELECT $1::bigint, id FROM topics WHERE slug = $2`, id, topic); err != nil {
		t.Fatalf("attach topic: %v", err)
	}
	src := fmt.Sprintf("https://example.com/%d", id)
	if _, err := pool.Exec(ctx, `INSERT INTO story_sources (story_id, url, source_name) VALUES ($1, $2, $2)`, id, src); err != nil {
		t.Fatalf("attach source: %v", err)
	}
	return id
}

func dedupeSettings() ai.Settings {
	s := settings()
	s.DedupeMaxPerRun = 10
	return s
}

func TestDedupeMergesNearDuplicate(t *testing.T) {
	pool, _ := setup(t)
	old := insertStory(t, pool, "OpenSSL fixes heap overflow", "languages/go", 3)
	dup := insertStory(t, pool, "Heap overflow patched in OpenSSL", "languages/go", 5)
	if _, err := pool.Exec(t.Context(), `UPDATE stories SET dedupe_checked_at = now() WHERE id = $1`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO users (firebase_uid) VALUES ('u')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO user_bookmarks SELECT id, $1 FROM users`, dup); err != nil {
		t.Fatal(err)
	}

	client := &answerClient{text: fmt.Sprintf(`{"duplicate_of": %d, "reason": "same CVE"}`, old)}
	merged, err := ai.NewProcessor(pool, client, dedupeSettings()).DedupeStories(t.Context())
	if err != nil || merged != 1 {
		t.Fatalf("DedupeStories = (%d, %v), want (1, nil)", merged, err)
	}
	if n := count(t, pool, fmt.Sprintf(`SELECT count(*) FROM stories WHERE id = %d`, dup)); n != 0 {
		t.Errorf("duplicate story still exists")
	}
	if n := count(t, pool, fmt.Sprintf(`SELECT count(*) FROM story_sources WHERE story_id = %d`, old)); n != 2 {
		t.Errorf("kept story has %d sources, want 2", n)
	}
	if n := count(t, pool, fmt.Sprintf(`SELECT count(*) FROM user_bookmarks WHERE story_id = %d`, old)); n != 1 {
		t.Errorf("bookmark was not moved to the kept story")
	}
	if n := count(t, pool, fmt.Sprintf(`SELECT importance FROM stories WHERE id = %d`, old)); n != 5 {
		t.Errorf("kept story importance = %d, want 5", n)
	}
}

func TestDedupeKeepsDistinctStoriesAndChecksOnce(t *testing.T) {
	pool, _ := setup(t)
	a := insertStory(t, pool, "Go 1.25 released", "languages/go", 3)
	b := insertStory(t, pool, "Go vulnerability in net/http", "languages/go", 4)

	client := &answerClient{text: `{"duplicate_of": 0, "reason": "different events"}`}
	p := ai.NewProcessor(pool, client, dedupeSettings())
	if merged, err := p.DedupeStories(t.Context()); err != nil || merged != 0 {
		t.Fatalf("DedupeStories = (%d, %v), want (0, nil)", merged, err)
	}
	if n := count(t, pool, `SELECT count(*) FROM stories`); n != 2 {
		t.Fatalf("stories = %d, want 2 (%d, %d)", n, a, b)
	}
	calls := client.calls
	if _, err := p.DedupeStories(t.Context()); err != nil || client.calls != calls {
		t.Fatalf("second run made %d extra calls (err %v), want none", client.calls-calls, err)
	}
}

func TestDedupeRejectsUnknownCandidate(t *testing.T) {
	pool, _ := setup(t)
	insertStory(t, pool, "A", "languages/go", 3)
	insertStory(t, pool, "B", "languages/go", 3)

	client := &answerClient{text: `{"duplicate_of": 424242, "reason": "?"}`}
	if _, err := ai.NewProcessor(pool, client, dedupeSettings()).DedupeStories(t.Context()); err == nil {
		t.Fatal("expected an error for an answer outside the candidate list")
	}
	if n := count(t, pool, `SELECT count(*) FROM stories`); n != 2 {
		t.Errorf("stories = %d, want 2 (nothing merged)", n)
	}
	// Failed checks stay pending for the next run.
	if n := count(t, pool, `SELECT count(*) FROM stories WHERE dedupe_checked_at IS NULL`); n != 2 {
		t.Errorf("unchecked stories = %d, want 2", n)
	}
}
