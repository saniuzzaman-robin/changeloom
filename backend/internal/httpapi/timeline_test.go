package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/auth"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/dbtest"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/topics"
	"github.com/saniuzzaman-robin/changeloom/backend/seed"
)

// Scores fall as stories age, so later pages must be ranked at the first page's time: otherwise
// the stories already shown score below the cursor again and repeat.
func TestTimelinePagesKeepFirstPageTime(t *testing.T) {
	pool := dbtest.New(t)
	if _, err := topics.Sync(t.Context(), pool, seed.TopicsYAML); err != nil {
		t.Fatalf("sync topics: %v", err)
	}
	now := time.Now().Truncate(time.Second)
	srv := NewServer(pool, Options{
		TimelineWindow: 60 * 24 * time.Hour,
		TimelineScore:  TimelineScore{TierWeight: 2, ImportanceWeight: 1, AgeDecay: 24 * time.Hour, SeenPenalty: 3, SeenGrace: 12 * time.Hour},
	})
	srv.now = func() time.Time { return now }
	ts := httptest.NewServer(NewHandler(srv, auth.DevVerifier{}))
	t.Cleanup(ts.Close)

	var want []int64
	for i := range 4 {
		var id int64
		err := pool.QueryRow(t.Context(), `
			INSERT INTO stories (title, summary, body_md, kind, importance, published_at, model, prompt_version)
			VALUES ('story', 'summary', 'body', 'release', 3, $1, 'test-model', 'v0') RETURNING id`,
			now.Add(-time.Duration(i+1)*time.Hour)).Scan(&id)
		if err != nil {
			t.Fatalf("insert story: %v", err)
		}
		if _, err := pool.Exec(t.Context(),
			`INSERT INTO story_topics (story_id, topic_id) SELECT $1, id FROM topics WHERE slug = 'languages/go'`, id); err != nil {
			t.Fatalf("tag story: %v", err)
		}
		want = append(want, id)
	}

	// Only stories of a followed topic reach the timeline.
	put, err := http.NewRequestWithContext(t.Context(), http.MethodPut, ts.URL+"/v1/me/topics", strings.NewReader(`{"topics":["languages/go"]}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	put.Header.Set("Authorization", "Bearer "+auth.DevTokenPrefix+"alice")
	put.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(put)
	if err != nil {
		t.Fatalf("follow: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("follow: status %d", resp.StatusCode)
	}

	page := func(cursor string) (items []int64, next string) {
		t.Helper()
		q := url.Values{"limit": {"2"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/v1/timeline?"+q.Encode(), nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+auth.DevTokenPrefix+"alice")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("timeline: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("timeline: status %d", resp.StatusCode)
		}
		var body struct {
			Items      []StorySummary `json:"items"`
			NextCursor *string        `json:"next_cursor"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode timeline: %v", err)
		}
		for _, it := range body.Items {
			items = append(items, it.Id)
		}
		if body.NextCursor != nil {
			next = *body.NextCursor
		}
		return items, next
	}

	first, cursor := page("")
	now = now.Add(48 * time.Hour) // every score drops by two points
	second, _ := page(cursor)
	if got := append(first, second...); !slices.Equal(got, want) {
		t.Fatalf("paged order = %v, want %v", got, want)
	}
}
