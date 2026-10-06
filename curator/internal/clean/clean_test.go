package clean_test

import (
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/catalog"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/clean"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/dbtest"
)

const testCatalog = `
professions:
  - {slug: engineer, name: Engineer, topics: [databases, security], launch: true}
topics:
  - slug: databases
    name: Databases
    priority: 3
    children:
      - {slug: databases/postgres, name: PostgreSQL}
  - {slug: security, name: Security, priority: 3}
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

// withOrphans gives pool the catalog plus topics and professions that left it: old (old/a is
// followed, old/b isn't), gone and gone/x (unused), req (a request resolved to it), and the
// professions retired (unused) and picked (a user picked it).
func withOrphans(t *testing.T, pool *pgxpool.Pool, cat catalog.Catalog) {
	t.Helper()
	if _, err := catalog.Seed(t.Context(), pool, cat); err != nil {
		t.Fatal(err)
	}
	exec(t, pool, `INSERT INTO topics (slug, name) VALUES ('old', 'Old'), ('gone', 'Gone'), ('req', 'Requested')`)
	exec(t, pool, `INSERT INTO topics (slug, name, parent_id) SELECT 'old/a', 'A', id FROM topics WHERE slug = 'old'`)
	exec(t, pool, `INSERT INTO topics (slug, name, parent_id) SELECT 'old/b', 'B', id FROM topics WHERE slug = 'old'`)
	exec(t, pool, `INSERT INTO topics (slug, name, parent_id) SELECT 'gone/x', 'X', id FROM topics WHERE slug = 'gone'`)
	exec(t, pool, `INSERT INTO professions (slug, name) VALUES ('retired', 'Retired'), ('picked', 'Picked')`)
}

func story(t *testing.T, pool *pgxpool.Pool, title string, topics ...string) {
	t.Helper()
	exec(t, pool, `INSERT INTO stories (title, summary, body_md, kind, importance, published_at, model, prompt_version)
		VALUES ($1, 's', 'b', 'release', 3, now(), 'm', 'p')`, title)
	exec(t, pool, `INSERT INTO story_topics (story_id, topic_id)
		SELECT s.id, t.id FROM stories s, topics t WHERE s.title = $1 AND t.slug = ANY($2)`, title, topics)
}

func TestCleanKeepsWhatIsInUse(t *testing.T) {
	ctx := t.Context()
	cat, err := catalog.Parse([]byte(testCatalog))
	if err != nil {
		t.Fatal(err)
	}
	local, remote := dbtest.New(t), dbtest.New(t)
	withOrphans(t, local, cat)
	withOrphans(t, remote, cat)
	exec(t, local, `INSERT INTO request_inbox (env, remote_id, text, created_at, status, topic_slug)
		VALUES ('staging', 1, 'Requested', now(), 'accepted', 'req')`)
	exec(t, remote, `INSERT INTO users (firebase_uid) VALUES ('a'), ('b')`)
	exec(t, remote, `INSERT INTO user_topics SELECT u.id, t.id FROM users u, topics t WHERE u.firebase_uid = 'a' AND t.slug = 'old/a'`)
	exec(t, remote, `INSERT INTO user_professions SELECT u.id, p.id FROM users u, professions p WHERE u.firebase_uid = 'b' AND p.slug = 'picked'`)

	story(t, remote, "only gone", "gone/x")
	story(t, remote, "gone and databases", "gone/x", "databases")
	story(t, remote, "saved gone", "gone/x")
	exec(t, remote, `INSERT INTO user_bookmarks (user_id, story_id) SELECT u.id, s.id FROM users u, stories s WHERE u.firebase_uid = 'a' AND s.title = 'saved gone'`)
	story(t, remote, "only old/b", "old/b")
	story(t, remote, "only old/a", "old/a")

	inUse := clean.NewInUse()
	if err := inUse.AddLocal(ctx, local); err != nil {
		t.Fatal(err)
	}
	if err := inUse.AddHosted(ctx, remote); err != nil {
		t.Fatal(err)
	}

	plan, err := clean.PlanFor(ctx, remote, cat, inUse)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"gone", "gone/x", "old/b"}; !slices.Equal(plan.Topics, want) {
		t.Errorf("topics to delete = %v, want %v", plan.Topics, want)
	}
	if want := []string{"old", "old/a", "req"}; !slices.Equal(plan.KeptTopics, want) {
		t.Errorf("kept topics = %v, want %v (old stays as old/a's parent)", plan.KeptTopics, want)
	}
	if !slices.Equal(plan.Professions, []string{"retired"}) || !slices.Equal(plan.KeptProfessions, []string{"picked"}) {
		t.Errorf("professions: delete %v, keep %v; want [retired], [picked]", plan.Professions, plan.KeptProfessions)
	}
	if plan.Stories != 2 {
		t.Errorf("stories to delete = %d, want 2 (only gone, only old/b)", plan.Stories)
	}
	// Planning deletes nothing.
	if n := count(t, remote, `SELECT count(*) FROM topics WHERE slug = 'gone/x'`); n != 1 {
		t.Fatalf("plan deleted gone/x")
	}

	res, err := clean.Apply(ctx, remote, plan)
	if err != nil {
		t.Fatal(err)
	}
	if res != (clean.Result{Topics: 3, Professions: 1, Stories: 2}) {
		t.Errorf("result = %+v, want 3 topics, 1 profession, 2 stories", res)
	}
	var left []string
	rows, err := remote.Query(ctx, `SELECT title FROM stories ORDER BY title`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		left = append(left, s)
	}
	rows.Close()
	// A saved story stays (without the deleted topic), as does one with another topic.
	if want := []string{"gone and databases", "only old/a", "saved gone"}; !slices.Equal(left, want) {
		t.Errorf("stories left = %v, want %v", left, want)
	}
	if n := count(t, remote, `SELECT count(*) FROM user_topics`); n != 1 {
		t.Errorf("follows left = %d, want 1", n)
	}

	// The local DB loses the same orphans, and a second run finds nothing.
	localPlan, err := clean.PlanFor(ctx, local, cat, inUse)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clean.Apply(ctx, local, localPlan); err != nil {
		t.Fatal(err)
	}
	for _, pool := range []*pgxpool.Pool{local, remote} {
		again, err := clean.PlanFor(ctx, pool, cat, inUse)
		if err != nil {
			t.Fatal(err)
		}
		if !again.Empty() {
			t.Errorf("second plan = %+v, want nothing to delete", again)
		}
	}
	if n := count(t, local, `SELECT count(*) FROM topics WHERE slug IN ('databases', 'databases/postgres', 'security', 'req', 'old', 'old/a')`); n != 6 {
		t.Errorf("local kept topics = %d, want 6", n)
	}
}
