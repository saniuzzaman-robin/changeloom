package push_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/dbtest"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/push"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/topics"
	"github.com/saniuzzaman-robin/changeloom/backend/seed"
)

type fakeSender struct {
	sent []push.Message
	dead []string
	err  error
}

func (f *fakeSender) Send(_ context.Context, m push.Message) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.sent = append(f.sent, m)
	return f.dead, nil
}

func setup(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := dbtest.New(t)
	if _, err := topics.Sync(t.Context(), pool, seed.TopicsYAML); err != nil {
		t.Fatalf("sync topics: %v", err)
	}
	return pool
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func addUser(t *testing.T, pool *pgxpool.Pool, uid, follow, token string) {
	t.Helper()
	exec(t, pool, `INSERT INTO users (firebase_uid) VALUES ($1)`, uid)
	exec(t, pool, `INSERT INTO user_topics SELECT u.id, t.id FROM users u, topics t WHERE u.firebase_uid = $1 AND t.slug = $2`, uid, follow)
	if token != "" {
		exec(t, pool, `INSERT INTO device_tokens (token, user_id, platform) SELECT $2, id, 'android' FROM users WHERE firebase_uid = $1`, uid, token)
	}
}

func addStory(t *testing.T, pool *pgxpool.Pool, title, kind string, severity *string, topic string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(t.Context(), `
		INSERT INTO stories (title, summary, body_md, kind, severity, importance, published_at, model, prompt_version)
		VALUES ($1, 'summary', 'body', $2, $3, 5, now(), 'm', 'v0') RETURNING id`,
		title, kind, severity).Scan(&id)
	if err != nil {
		t.Fatalf("insert story: %v", err)
	}
	exec(t, pool, `INSERT INTO story_topics SELECT $1, id FROM topics WHERE slug = $2`, id, topic)
	return id
}

func TestNotifierPushesToFollowersOnly(t *testing.T) {
	pool := setup(t)
	high := "high"
	low := "low"
	// languages follows cover languages/go through the ancestor rule.
	addUser(t, pool, "follower", "languages", "tok-follower")
	addUser(t, pool, "other", "languages/rust", "tok-other")
	addUser(t, pool, "notoken", "languages/go", "")

	story := addStory(t, pool, "Go CVE", "security", &high, "languages/go")
	addStory(t, pool, "Low severity", "security", &low, "languages/go")
	addStory(t, pool, "Release", "release", nil, "languages/go")

	sender := &fakeSender{}
	n := push.NewNotifier(pool, sender)
	pushed, err := n.Run(t.Context())
	if err != nil || pushed != 1 {
		t.Fatalf("Run = (%d, %v), want (1, nil)", pushed, err)
	}
	if len(sender.sent) != 1 || sender.sent[0].StoryID != story || len(sender.sent[0].Tokens) != 1 || sender.sent[0].Tokens[0] != "tok-follower" {
		t.Fatalf("sent = %+v, want one message for story %d to tok-follower only", sender.sent, story)
	}

	// Already notified: nothing more is sent.
	pushed, err = n.Run(t.Context())
	if err != nil || pushed != 0 || len(sender.sent) != 1 {
		t.Fatalf("second Run = (%d, %v), sent %d, want nothing new", pushed, err, len(sender.sent))
	}
}

func TestNotifierRemovesDeadTokensAndRetriesFailures(t *testing.T) {
	pool := setup(t)
	high := "critical"
	addUser(t, pool, "u", "languages/go", "tok-dead")
	addStory(t, pool, "Go CVE", "security", &high, "languages/go")

	sender := &fakeSender{err: errors.New("fcm down")}
	n := push.NewNotifier(pool, sender)
	if pushed, err := n.Run(t.Context()); err == nil || pushed != 0 {
		t.Fatalf("Run with failing sender = (%d, %v), want error and 0 pushed", pushed, err)
	}

	sender.err, sender.dead = nil, []string{"tok-dead"}
	if pushed, err := n.Run(t.Context()); err != nil || pushed != 1 {
		t.Fatalf("retry Run = (%d, %v), want (1, nil)", pushed, err)
	}
	var count int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM device_tokens`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("device_tokens count = %d (%v), want 0 after unregistered", count, err)
	}
}

func TestNotifierIgnoresOldStories(t *testing.T) {
	pool := setup(t)
	high := "high"
	addUser(t, pool, "u", "languages/go", "tok")
	id := addStory(t, pool, "Old CVE", "security", &high, "languages/go")
	exec(t, pool, `UPDATE stories SET created_at = $2 WHERE id = $1`, id, time.Now().Add(-48*time.Hour))

	sender := &fakeSender{}
	if pushed, err := push.NewNotifier(pool, sender).Run(t.Context()); err != nil || pushed != 0 || len(sender.sent) != 0 {
		t.Fatalf("Run = (%d, %v), sent %d, want nothing", pushed, err, len(sender.sent))
	}
}
