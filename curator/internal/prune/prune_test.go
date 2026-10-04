package prune_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/dbtest"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/prune"
)

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func TestRun(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.New(t)
	exec(t, pool, `INSERT INTO users (firebase_uid) VALUES ('a'), ('b'), ('c')`)

	addStory := func(title string, age time.Duration, viewers int, saved bool) {
		t.Helper()
		exec(t, pool, `INSERT INTO stories (title, summary, body_md, kind, importance, published_at, model, prompt_version)
			VALUES ($1, 's', 'b', 'release', 2, now() - $2::interval, 'm', 'p')`, title, age.String())
		exec(t, pool, `INSERT INTO story_views (user_id, story_id)
			SELECT u.id, s.id FROM (SELECT id FROM users ORDER BY id LIMIT $2) u, stories s WHERE s.title = $1`, title, viewers)
		if saved {
			exec(t, pool, `INSERT INTO user_bookmarks (user_id, story_id) SELECT u.id, s.id FROM users u, stories s WHERE u.firebase_uid = 'a' AND s.title = $1`, title)
		}
	}
	day := 24 * time.Hour
	addStory("fresh unseen", day, 0, false)
	addStory("old popular", 20*day, 3, false)
	addStory("old saved", 20*day, 0, true)
	addStory("week old popular", 10*day, 3, false)
	addStory("week old saved", 10*day, 0, true)
	exec(t, pool, `INSERT INTO stories (title, summary, body_md, kind, importance, published_at, model, prompt_version)
		VALUES ('week old deal', 's', 'b', 'deal', 2, now() - interval '10 days', 'm', 'p'), ('old deal', 's', 'b', 'deal', 2, now() - interval '20 days', 'm', 'p')`)
	// More than one batch of old stories.
	exec(t, pool, `INSERT INTO stories (title, summary, body_md, kind, importance, published_at, model, prompt_version)
		SELECT 'bulk ' || g, 's', 'b', 'release', 2, now() - interval '30 days', 'm', 'p' FROM generate_series(1, 1100) g`)

	deleted, err := prune.Run(ctx, pool, prune.Settings{MaxAge: 14 * day}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1100+2 {
		t.Errorf("deleted = %d, want 1102", deleted)
	}
	rows, err := pool.Query(ctx, `SELECT title FROM stories ORDER BY title`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var kept []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatal(err)
		}
		kept = append(kept, title)
	}
	want := []string{"fresh unseen", "old saved", "week old deal", "week old popular", "week old saved"}
	if len(kept) != len(want) {
		t.Fatalf("kept %v, want %v", kept, want)
	}
	for i := range want {
		if kept[i] != want[i] {
			t.Errorf("kept %v, want %v", kept, want)
			break
		}
	}
}

func TestDue(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.New(t)
	now := time.Now()
	interval := 7 * 24 * time.Hour

	due, err := prune.Due(ctx, pool, config.EnvStaging, interval, now)
	if err != nil || !due {
		t.Fatalf("never pruned: due %v, err %v", due, err)
	}
	if err := prune.MarkDone(ctx, pool, config.EnvStaging, now.Add(-6*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if due, err = prune.Due(ctx, pool, config.EnvStaging, interval, now); err != nil || due {
		t.Fatalf("pruned 6 days ago: due %v, err %v", due, err)
	}
	if due, err = prune.Due(ctx, pool, config.EnvProd, interval, now); err != nil || !due {
		t.Fatalf("prod never pruned: due %v, err %v", due, err)
	}
	if due, err = prune.Due(ctx, pool, config.EnvStaging, interval, now.Add(2*24*time.Hour)); err != nil || !due {
		t.Fatalf("pruned 8 days ago: due %v, err %v", due, err)
	}
}
