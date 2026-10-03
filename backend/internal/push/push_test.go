package push_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
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
	// failStory, when set, fails only that story's sends.
	failStory int64
}

func (f *fakeSender) Send(_ context.Context, m push.Message) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.failStory != 0 && m.StoryID == f.failStory {
		return nil, errors.New("send failed")
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
	pushed, remaining, err := n.Run(t.Context())
	if err != nil || pushed != 1 || remaining != 0 {
		t.Fatalf("Run = (%d, %d, %v), want (1, 0, nil)", pushed, remaining, err)
	}
	if len(sender.sent) != 1 || sender.sent[0].StoryID != story || len(sender.sent[0].Tokens) != 1 || sender.sent[0].Tokens[0] != "tok-follower" {
		t.Fatalf("sent = %+v, want one message for story %d to tok-follower only", sender.sent, story)
	}

	// Already notified: nothing more is sent.
	pushed, _, err = n.Run(t.Context())
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
	if pushed, remaining, err := n.Run(t.Context()); err == nil || pushed != 0 || remaining != 1 {
		t.Fatalf("Run with failing sender = (%d, %d, %v), want error, 0 pushed, 1 remaining", pushed, remaining, err)
	}

	sender.err, sender.dead = nil, []string{"tok-dead"}
	if pushed, _, err := n.Run(t.Context()); err != nil || pushed != 1 {
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
	if pushed, _, err := push.NewNotifier(pool, sender).Run(t.Context()); err != nil || pushed != 0 || len(sender.sent) != 0 {
		t.Fatalf("Run = (%d, %v), sent %d, want nothing", pushed, err, len(sender.sent))
	}
}

func TestNotifierRunsInBatches(t *testing.T) {
	pool := setup(t)
	high := "high"
	addUser(t, pool, "u", "languages/go", "tok")
	const stories = 13
	for i := range stories {
		addStory(t, pool, "CVE "+strconv.Itoa(i), "security", &high, "languages/go")
	}

	sender := &fakeSender{}
	n := push.NewNotifier(pool, sender)
	pushed, remaining, err := n.Run(t.Context())
	if err != nil || pushed == 0 || pushed >= stories || remaining != stories-pushed {
		t.Fatalf("first Run = (%d, %d, %v), want a partial batch with the rest remaining", pushed, remaining, err)
	}
	total := pushed
	for remaining > 0 {
		if pushed, remaining, err = n.Run(t.Context()); err != nil || pushed == 0 {
			t.Fatalf("next Run = (%d, %d, %v), want progress", pushed, remaining, err)
		}
		total += pushed
	}
	if total != stories || len(sender.sent) != stories {
		t.Fatalf("pushed %d stories in %d sends, want %d each", total, len(sender.sent), stories)
	}
}

func TestNotifierSkipsFailedStoryAndMarksOthers(t *testing.T) {
	pool := setup(t)
	high := "high"
	addUser(t, pool, "u", "languages/go", "tok")
	bad := addStory(t, pool, "Bad CVE", "security", &high, "languages/go")
	good := addStory(t, pool, "Good CVE", "security", &high, "languages/go")

	sender := &fakeSender{failStory: bad}
	pushed, remaining, err := push.NewNotifier(pool, sender).Run(t.Context())
	if err == nil || pushed != 1 || remaining != 1 {
		t.Fatalf("Run = (%d, %d, %v), want the good story pushed, the bad one remaining, and an error", pushed, remaining, err)
	}
	var notified bool
	if err := pool.QueryRow(t.Context(), `SELECT notified_at IS NOT NULL FROM stories WHERE id = $1`, good).Scan(&notified); err != nil || !notified {
		t.Fatalf("good story notified = %v (%v), want true", notified, err)
	}
}

func TestNotifierConcurrentRunsPushEachStoryOnce(t *testing.T) {
	pool := setup(t)
	high := "high"
	addUser(t, pool, "u", "languages/go", "tok")
	const stories = 8
	for i := range stories {
		addStory(t, pool, "CVE "+strconv.Itoa(i), "security", &high, "languages/go")
	}

	sender := &lockedSender{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if _, _, err := push.NewNotifier(pool, sender).Run(t.Context()); err != nil {
				t.Errorf("Run: %v", err)
			}
		})
	}
	wg.Wait()
	seen := map[int64]int{}
	for _, m := range sender.sent {
		seen[m.StoryID]++
	}
	if len(seen) != stories || len(sender.sent) != stories {
		t.Fatalf("sent %d messages for %d stories, want each of %d once", len(sender.sent), len(seen), stories)
	}
}

// lockedSender is a fakeSender safe for concurrent Runs.
type lockedSender struct {
	mu   sync.Mutex
	sent []push.Message
}

func (l *lockedSender) Send(_ context.Context, m push.Message) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sent = append(l.sent, m)
	return nil, nil
}
