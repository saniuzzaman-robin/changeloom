package ai_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/ai"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/ingest"
)

type fakeSearcher struct {
	text  string
	calls int
	last  ai.SearchRequest
}

func (f *fakeSearcher) Search(_ context.Context, req ai.SearchRequest) (ai.Result, error) {
	f.calls++
	f.last = req
	return ai.Result{Outcome: ai.OutcomeSucceeded, Text: f.text, StopReason: "end_turn"}, nil
}

// notFound makes article extraction fail fast without network access.
type notFound struct{}

func (notFound) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
}

// newDiscoverer returns a Discoverer over a fresh database; when followed is true a user
// follows languages/go.
func newDiscoverer(t *testing.T, s ai.Searcher, minInterval time.Duration, followed bool) (*ai.Discoverer, *pgxpool.Pool) {
	t.Helper()
	pool, _ := setup(t)
	if followed {
		for _, q := range []string{
			`INSERT INTO users (firebase_uid) VALUES ('u')`,
			`INSERT INTO user_topics SELECT u.id, t.id FROM users u, topics t WHERE t.slug = 'languages/go'`,
		} {
			if _, err := pool.Exec(t.Context(), q); err != nil {
				t.Fatalf("follow topic: %v", err)
			}
		}
	}
	ing := ingest.New(pool, &http.Client{Transport: notFound{}}, 30*24*time.Hour)
	return ai.NewDiscoverer(pool, s, ing, ai.DiscoverySettings{MaxSearches: 4, MinInterval: minInterval, MaxItemAge: 30 * 24 * time.Hour}), pool
}

func TestDiscoveryStoresItemsAsPendingRawItems(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	answer := fmt.Sprintf(`Here is what I found:
{"items":[
 {"title":"Foo 3.0 released","url":"https://foo.dev/blog/3.0","published":%q,"summary":"Foo 3.0 ships a new engine."},
 {"title":"No url","url":"","published":"","summary":"dropped"},
 {"title":"Bad scheme","url":"javascript:alert(1)","published":"","summary":"dropped"}
]}`, today)
	s := &fakeSearcher{text: answer}
	d, pool := newDiscoverer(t, s, 20*time.Hour, true)

	n, err := d.Run(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("Run = (%d, %v), want (1, nil)", n, err)
	}
	if got := count(t, pool, `SELECT count(*) FROM raw_items r JOIN sources s ON s.id = r.source_id WHERE s.name = 'Web discovery' AND s.kind = 'discovery' AND NOT s.enabled AND r.status = 'pending' AND r.url = 'https://foo.dev/blog/3.0'`); got != 1 {
		t.Fatalf("discovered raw item not stored under the discovery source (matched %d)", got)
	}
	if s.last.MaxSearches != 4 || !strings.Contains(s.last.User, "languages/go") {
		t.Errorf("search request = %+v, want max searches 4 and the followed topic", s.last)
	}

	// Within MinInterval the agent does not run again.
	if n, err := d.Run(t.Context()); err != nil || n != 0 || s.calls != 1 {
		t.Fatalf("second Run = (%d, %v), calls %d, want no new search", n, err, s.calls)
	}
}

func TestDiscoverySkipsWithoutFollowedTopics(t *testing.T) {
	s := &fakeSearcher{text: `{"items":[]}`}
	d, _ := newDiscoverer(t, s, time.Hour, false)
	if n, err := d.Run(t.Context()); err != nil || n != 0 || s.calls != 0 {
		t.Fatalf("Run = (%d, %v), calls %d, want skipped", n, err, s.calls)
	}
}

func TestDiscoveryRejectsAnswerWithoutJSON(t *testing.T) {
	s := &fakeSearcher{text: "I could not find anything."}
	d, _ := newDiscoverer(t, s, time.Hour, true)
	if _, err := d.Run(t.Context()); err == nil {
		t.Fatal("expected an error for an answer without JSON")
	}
}
