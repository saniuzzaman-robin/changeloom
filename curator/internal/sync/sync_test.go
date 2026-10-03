package sync_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/catalog"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/dbtest"
	cursync "github.com/saniuzzaman-robin/changeloom/curator/internal/sync"
)

const testCatalog = `
- slug: databases
  name: Databases
  children:
    - {slug: databases/postgres, name: PostgreSQL, related: [security]}
- {slug: security, name: Security}
`

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
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

// setup returns a seeded local DB and an empty "hosted" DB whose topic ids are shifted.
func setup(t *testing.T) (local, remote *pgxpool.Pool) {
	t.Helper()
	local, remote = dbtest.New(t), dbtest.New(t)
	nodes, err := catalog.Parse([]byte(testCatalog))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Seed(t.Context(), local, nodes); err != nil {
		t.Fatal(err)
	}
	exec(t, remote, `INSERT INTO topics (slug, name) VALUES ('zzz', 'Zzz')`)
	return local, remote
}

func addStory(t *testing.T, local *pgxpool.Pool, title, topicSlug, url string) {
	t.Helper()
	exec(t, local, `
		WITH s AS (
			INSERT INTO stories (title, summary, body_md, kind, importance, published_at, model, prompt_version)
			VALUES ($1, 'sum', 'body', 'release', 3, now(), 'm', 'v') RETURNING id
		), src AS (
			INSERT INTO story_sources (story_id, url, source_name) SELECT id, $3, 'src' FROM s
		)
		INSERT INTO story_topics (story_id, topic_id) SELECT s.id, t.id FROM s, topics t WHERE t.slug = $2`,
		title, topicSlug, url)
}

func TestPushTopicsAndStories(t *testing.T) {
	ctx := t.Context()
	local, remote := setup(t)
	addStory(t, local, "PG 18", "databases/postgres", "https://pg.example/18")

	res, err := cursync.Push(ctx, local, remote, config.EnvStaging)
	if err != nil {
		t.Fatal(err)
	}
	if res.Topics != 3 || res.Stories != 1 {
		t.Errorf("result = %+v, want 3 topics and 1 story", res)
	}
	if n := count(t, remote, `SELECT count(*) FROM topics t JOIN topics p ON p.id = t.parent_id WHERE t.slug = 'databases/postgres' AND p.slug = 'databases'`); n != 1 {
		t.Errorf("parent link not synced")
	}
	if n := count(t, remote, `SELECT count(*) FROM topic_relations`); n != 1 {
		t.Errorf("relations = %d, want 1", n)
	}
	if n := count(t, remote, `SELECT count(*) FROM story_topics st JOIN topics t ON t.id = st.topic_id JOIN story_sources ss ON ss.story_id = st.story_id WHERE t.slug = 'databases/postgres'`); n != 1 {
		t.Errorf("story topic/source not synced")
	}
	if n := count(t, remote, `SELECT count(*) FROM stories WHERE notified_at IS NULL AND created_at < now()`); n != 1 {
		t.Errorf("remote story should start un-notified with its local created_at")
	}

	// A second push with no changes writes no stories; a changed story replaces its sources and
	// keeps the hosted notified_at.
	if res, err = cursync.Push(ctx, local, remote, config.EnvStaging); err != nil || res.Stories != 0 {
		t.Fatalf("idempotent push = %+v, %v; want 0 stories", res, err)
	}
	exec(t, remote, `UPDATE stories SET notified_at = now()`)
	exec(t, local, `UPDATE stories SET importance = 5, updated_at = now()`)
	exec(t, local, `DELETE FROM story_sources`)
	exec(t, local, `INSERT INTO story_sources SELECT id, 'https://pg.example/new', 'src' FROM stories`)
	if res, err = cursync.Push(ctx, local, remote, config.EnvStaging); err != nil || res.Stories != 1 {
		t.Fatalf("re-sync = %+v, %v; want 1 story", res, err)
	}
	if n := count(t, remote, `SELECT count(*) FROM stories WHERE importance = 5 AND notified_at IS NOT NULL`); n != 1 {
		t.Errorf("update not applied or notified_at reset")
	}
	if n := count(t, remote, `SELECT count(*) FROM story_sources WHERE url = 'https://pg.example/new'`); n != 1 {
		t.Errorf("sources not replaced")
	}
	if n := count(t, remote, `SELECT count(*) FROM story_sources`); n != 1 {
		t.Errorf("story_sources = %d, want 1", n)
	}
	if n := count(t, remote, `SELECT count(*) FROM stories`); n != 1 {
		t.Errorf("stories = %d, want 1 (upsert by uid)", n)
	}

	// The prod watermark is independent.
	if res, err = cursync.Push(ctx, local, remote, config.EnvProd); err != nil || res.Stories != 1 {
		t.Fatalf("prod push = %+v, %v; want 1 story", res, err)
	}
}

func TestPushTombstones(t *testing.T) {
	ctx := t.Context()
	local, remote := setup(t)
	addStory(t, local, "Keep", "security", "https://a.example")
	addStory(t, local, "Dup", "security", "https://b.example")
	if _, err := cursync.Push(ctx, local, remote, config.EnvStaging); err != nil {
		t.Fatal(err)
	}
	exec(t, remote, `INSERT INTO users (firebase_uid) VALUES ('u1'), ('u2')`)
	exec(t, remote, `INSERT INTO user_bookmarks (user_id, story_id) SELECT u.id, s.id FROM users u, stories s WHERE s.title = 'Dup'`)
	exec(t, remote, `INSERT INTO user_bookmarks (user_id, story_id) SELECT u.id, s.id FROM users u, stories s WHERE s.title = 'Keep' AND u.firebase_uid = 'u1'`)
	exec(t, remote, `INSERT INTO user_story_state (user_id, story_id, read_at) SELECT u.id, s.id, now() FROM users u, stories s WHERE s.title = 'Dup' AND u.firebase_uid = 'u2'`)

	exec(t, local, `INSERT INTO story_tombstones (uid, merged_into_uid)
		SELECT d.uid, k.uid FROM stories d, stories k WHERE d.title = 'Dup' AND k.title = 'Keep'`)
	// A tombstone pointing at a story the hosted DB lacks is skipped, keeping user state.
	exec(t, local, `INSERT INTO story_tombstones (uid, merged_into_uid) VALUES (gen_random_uuid(), gen_random_uuid())`)

	res, err := cursync.Push(ctx, local, remote, config.EnvStaging)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tombstones != 1 || res.SkippedTombstones != 1 {
		t.Errorf("result = %+v, want 1 pushed and 1 skipped tombstone", res)
	}
	if n := count(t, remote, `SELECT count(*) FROM stories WHERE title = 'Dup'`); n != 0 {
		t.Errorf("duplicate story not deleted")
	}
	if n := count(t, remote, `SELECT count(*) FROM user_bookmarks b JOIN stories s ON s.id = b.story_id WHERE s.title = 'Keep'`); n != 2 {
		t.Errorf("bookmarks on kept story = %d, want 2", n)
	}
	if n := count(t, remote, `SELECT count(*) FROM user_story_state st JOIN stories s ON s.id = st.story_id WHERE s.title = 'Keep'`); n != 1 {
		t.Errorf("read state not moved")
	}

	// Pushed tombstones are not pushed again; the skipped one is retried.
	res, err = cursync.Push(ctx, local, remote, config.EnvStaging)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tombstones != 0 || res.SkippedTombstones != 1 {
		t.Errorf("second result = %+v, want only the skipped tombstone retried", res)
	}
}

func TestPushDecisions(t *testing.T) {
	ctx := t.Context()
	local, remote := setup(t)
	exec(t, remote, `INSERT INTO users (firebase_uid) VALUES ('u1')`)
	exec(t, remote, `INSERT INTO topic_requests (id, user_id, text) OVERRIDING SYSTEM VALUE
		SELECT n, u.id, 'req' || n FROM users u, generate_series(1, 3) n`)
	exec(t, local, `INSERT INTO request_inbox (env, remote_id, text, created_at, status, topic_slug, note, resolved_at) VALUES
		('staging', 1, 'req1', now(), 'merged', 'security', 'covered', now()),
		('staging', 2, 'req2', now(), 'rejected', NULL, 'out of scope', now()),
		('staging', 3, 'req3', now(), 'pending', NULL, NULL, NULL),
		('prod', 1, 'req1', now(), 'rejected', NULL, 'prod only', now())`)

	res, err := cursync.Push(ctx, local, remote, config.EnvStaging)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decisions != 2 {
		t.Errorf("decisions = %d, want 2", res.Decisions)
	}
	if n := count(t, remote, `SELECT count(*) FROM topic_requests r JOIN topics t ON t.id = r.topic_id WHERE r.id = 1 AND r.status = 'merged' AND t.slug = 'security' AND r.note = 'covered'`); n != 1 {
		t.Errorf("merged decision not applied")
	}
	if n := count(t, remote, `SELECT count(*) FROM topic_requests WHERE id = 2 AND status = 'rejected' AND topic_id IS NULL`); n != 1 {
		t.Errorf("rejected decision not applied")
	}
	if n := count(t, remote, `SELECT count(*) FROM topic_requests WHERE status = 'pending'`); n != 1 {
		t.Errorf("pending requests = %d, want the undecided one", n)
	}
	if n := count(t, local, `SELECT count(*) FROM request_inbox WHERE pushed_at IS NOT NULL`); n != 2 {
		t.Errorf("pushed inbox rows = %d, want 2 (staging decisions only)", n)
	}
	if res, err = cursync.Push(ctx, local, remote, config.EnvStaging); err != nil || res.Decisions != 0 {
		t.Errorf("second push = %+v, %v; want no decisions", res, err)
	}
}

func TestNotify(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Method + " " + r.URL.Path + " " + r.Header.Get("Authorization")
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"sent":2}`))
	}))
	defer srv.Close()

	n, err := cursync.Notify(t.Context(), srv.Client(), config.Remote{APIBaseURL: srv.URL, NotifySecret: "s3cret"})
	if err != nil || n != 2 {
		t.Fatalf("Notify = %d, %v; want 2, nil", n, err)
	}
	if want := "POST /internal/notify Bearer s3cret"; gotAuth != want {
		t.Errorf("request = %q, want %q", gotAuth, want)
	}
	if _, err := cursync.Notify(t.Context(), srv.Client(), config.Remote{APIBaseURL: srv.URL, NotifySecret: "bad"}); err == nil {
		t.Error("wrong secret: want an error")
	}
	if _, err := cursync.Notify(t.Context(), srv.Client(), config.Remote{}); err == nil {
		t.Error("unset config: want an error")
	}
}
