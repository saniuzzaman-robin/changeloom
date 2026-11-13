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

const replacingCatalog = `
professions:
  - {slug: engineer, name: Engineer, topics: [databases], launch: true}
topics:
  - slug: databases
    name: Databases
    priority: 3
    children:
      - {slug: databases/postgresql, name: PostgreSQL, replaces: [databases/postgres]}
`

func TestCleanMergesReplacedTopics(t *testing.T) {
	ctx := t.Context()
	cat, err := catalog.Parse([]byte(replacingCatalog))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.New(t)
	if _, err := catalog.Seed(ctx, pool, cat); err != nil {
		t.Fatal(err)
	}
	exec(t, pool, `INSERT INTO topics (slug, name, parent_id) SELECT 'databases/postgres', 'PostgreSQL', id FROM topics WHERE slug = 'databases'`)
	exec(t, pool, `INSERT INTO users (firebase_uid) VALUES ('old'), ('both'), ('muter')`)
	exec(t, pool, `INSERT INTO user_topics SELECT u.id, t.id FROM users u, topics t
		WHERE (u.firebase_uid IN ('old', 'both') AND t.slug = 'databases/postgres') OR (u.firebase_uid = 'both' AND t.slug = 'databases/postgresql')`)
	exec(t, pool, `INSERT INTO user_topic_mutes SELECT u.id, t.id FROM users u, topics t WHERE u.firebase_uid = 'muter' AND t.slug = 'databases/postgres'`)
	exec(t, pool, `INSERT INTO topic_requests (user_id, text, status, topic_id)
		SELECT u.id, 'Postgres', 'accepted', t.id FROM users u, topics t WHERE u.firebase_uid = 'old' AND t.slug = 'databases/postgres'`)
	story(t, pool, "only old", "databases/postgres")
	story(t, pool, "old and new", "databases/postgres", "databases/postgresql")

	inUse := clean.NewInUse()
	if err := inUse.AddHosted(ctx, pool); err != nil {
		t.Fatal(err)
	}
	plan, err := clean.PlanFor(ctx, pool, cat, inUse)
	if err != nil {
		t.Fatal(err)
	}
	if want := []clean.Merge{{From: "databases/postgres", To: "databases/postgresql"}}; !slices.Equal(plan.Merges, want) {
		t.Errorf("merges = %v, want %v", plan.Merges, want)
	}
	// In use, but replaced: its uses move, then it goes, and its stories stay with the replacement.
	if want := []string{"databases/postgres"}; !slices.Equal(plan.Topics, want) || plan.Stories != 0 {
		t.Errorf("delete topics %v and %d stories, want %v and 0", plan.Topics, plan.Stories, want)
	}

	res, err := clean.Apply(ctx, pool, plan)
	if err != nil {
		t.Fatal(err)
	}
	// One follow (the other user already followed both), one mute, one story tag, one request.
	if res != (clean.Result{Moved: 4, Topics: 1}) {
		t.Errorf("result = %+v, want 4 moved, 1 topic", res)
	}
	for sql, want := range map[string]int{
		`SELECT count(*) FROM topics WHERE slug = 'databases/postgres'`:                                                      0,
		`SELECT count(*) FROM user_topics ut JOIN topics t ON t.id = ut.topic_id WHERE t.slug = 'databases/postgresql'`:      2,
		`SELECT count(*) FROM user_topic_mutes um JOIN topics t ON t.id = um.topic_id WHERE t.slug = 'databases/postgresql'`: 1,
		`SELECT count(*) FROM story_topics st JOIN topics t ON t.id = st.topic_id WHERE t.slug = 'databases/postgresql'`:     2,
		`SELECT count(*) FROM topic_requests r JOIN topics t ON t.id = r.topic_id WHERE t.slug = 'databases/postgresql'`:     1,
		`SELECT count(*) FROM stories`: 2,
	} {
		if n := count(t, pool, sql); n != want {
			t.Errorf("%s = %d, want %d", sql, n, want)
		}
	}

	again, err := clean.PlanFor(ctx, pool, cat, inUse)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Empty() {
		t.Errorf("second plan = %+v, want nothing to do", again)
	}
}
