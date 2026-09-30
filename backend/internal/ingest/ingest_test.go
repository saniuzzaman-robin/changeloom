package ingest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/dbtest"
)

type fakeExtractor struct {
	text  string
	err   error
	calls int
}

func (f *fakeExtractor) Extract(context.Context, string) (string, error) {
	f.calls++
	return f.text, f.err
}

func newSource(t *testing.T, pool *pgxpool.Pool, url string) int64 {
	t.Helper()
	id, err := db.New(pool).UpsertSource(t.Context(), db.UpsertSourceParams{
		Name:            "Example",
		Kind:            KindRSS,
		Config:          cfg(t, map[string]any{"url": url}),
		DefaultTopicIds: []int64{},
		PollSeconds:     3600,
		Enabled:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newIngester(pool *pgxpool.Pool, srv *httptest.Server, ex Extractor) *Ingester {
	in := New(pool, srv.Client(), 30*24*time.Hour)
	in.extractor = ex
	in.now = func() time.Time { return time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC) }
	return in
}

func TestPollSource(t *testing.T) {
	pool := dbtest.New(t)
	feed, err := os.ReadFile("testdata/feed.atom")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write(feed)
	}))
	defer srv.Close()

	id := newSource(t, pool, srv.URL)
	ex := &fakeExtractor{text: "The full article text, much longer than the excerpt."}
	in := newIngester(pool, srv, ex)

	stats, err := in.PollSource(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Fetched != 2 || stats.Inserted != 1 || stats.TooOld != 1 {
		t.Errorf("first poll stats = %+v, want fetched=2 inserted=1 too_old=1", stats)
	}
	if ex.calls != 1 {
		t.Errorf("extractor calls = %d, want 1 (excerpt only)", ex.calls)
	}
	var url, content string
	err = pool.QueryRow(t.Context(), "SELECT url, content FROM raw_items").Scan(&url, &content)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://example.com/blog/release-2" || content != ex.text {
		t.Errorf("stored url=%q content=%q", url, content)
	}

	// Second poll sends If-None-Match and gets 304: nothing fetched, no new extraction.
	stats, err = in.PollSource(t.Context(), id)
	if err != nil || !stats.NotModified || stats.Inserted != 0 {
		t.Fatalf("second poll: stats=%+v err=%v", stats, err)
	}

	// Without the stored etag the feed is re-read but the item is recognised, not re-fetched.
	if _, err := pool.Exec(t.Context(), "UPDATE sources SET etag = NULL"); err != nil {
		t.Fatal(err)
	}
	stats, err = in.PollSource(t.Context(), id)
	if err != nil || stats.Existing != 1 || stats.Inserted != 0 || ex.calls != 1 {
		t.Fatalf("third poll: stats=%+v calls=%d err=%v", stats, ex.calls, err)
	}
}

func TestPollSourceKeepsExcerptWhenExtractionFails(t *testing.T) {
	pool := dbtest.New(t)
	feed, err := os.ReadFile("testdata/feed.atom")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(feed) }))
	defer srv.Close()

	id := newSource(t, pool, srv.URL)
	in := newIngester(pool, srv, &fakeExtractor{err: errors.New("boom")})
	if _, err := in.PollSource(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	var content string
	if err := pool.QueryRow(t.Context(), "SELECT content FROM raw_items").Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "Short excerpt." {
		t.Errorf("content = %q, want the feed excerpt", content)
	}
}

func TestPollSourceFailureIsRecorded(t *testing.T) {
	pool := dbtest.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()

	id := newSource(t, pool, srv.URL)
	if _, err := newIngester(pool, srv, nil).PollSource(t.Context(), id); err == nil {
		t.Fatal("want error from failing source")
	}
	due, err := db.New(pool).ListDueSourceIDs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Errorf("failed source should wait for its next interval, but is due: %v", due)
	}
}
