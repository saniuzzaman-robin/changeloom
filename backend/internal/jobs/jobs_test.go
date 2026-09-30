package jobs

import (
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/dbtest"
)

func TestEnqueueDue(t *testing.T) {
	pool := dbtest.New(t)
	ctx := t.Context()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	client, err := NewInsertClient(pool)
	if err != nil {
		t.Fatal(err)
	}

	q := db.New(pool)
	add := func(name string, enabled bool) int64 {
		id, err := q.UpsertSource(ctx, db.UpsertSourceParams{
			Name: name, Kind: "kev", Config: []byte("{}"), DefaultTopicIds: []int64{}, PollSeconds: 3600, Enabled: enabled,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	never := add("never polled", true)
	recent := add("polled recently", true)
	stale := add("polled long ago", true)
	_ = add("disabled", false)
	if _, err := pool.Exec(ctx, "UPDATE sources SET last_polled_at = now() WHERE id = $1", recent); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE sources SET last_polled_at = now() - interval '2 hours' WHERE id = $1", stale); err != nil {
		t.Fatal(err)
	}

	n, err := EnqueueDue(ctx, pool, client)
	if err != nil || n != 2 {
		t.Fatalf("first EnqueueDue = %d, %v; want 2", n, err)
	}
	assertQueued(t, pool, []int64{never, stale})

	// Jobs still queued must not be enqueued twice.
	if _, err := EnqueueDue(ctx, pool, client); err != nil {
		t.Fatal(err)
	}
	assertQueued(t, pool, []int64{never, stale})
}

// assertQueued checks that poll_source jobs exist for exactly the wanted source ids.
func assertQueued(t *testing.T, pool *pgxpool.Pool, want []int64) {
	t.Helper()
	rows, err := pool.Query(t.Context(), "SELECT (args->>'source_id')::bigint FROM river_job WHERE kind = 'poll_source' ORDER BY 1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("queued poll_source jobs for sources %v, want %v", got, want)
	}
}
