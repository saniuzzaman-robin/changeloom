package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/auth"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/dbtest"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/httpapi"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/topics"
	"github.com/saniuzzaman-robin/changeloom/backend/seed"
)

const (
	testWindow = 60 * 24 * time.Hour
	aliceToken = auth.DevTokenPrefix + "alice"
	bobToken   = auth.DevTokenPrefix + "bob"
)

type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	srv  *httptest.Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := dbtest.New(t)
	if _, err := topics.Sync(t.Context(), pool, seed.TopicsYAML); err != nil {
		t.Fatalf("sync topics: %v", err)
	}
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.NewServer(pool, testWindow), auth.DevVerifier{}))
	t.Cleanup(srv.Close)
	return &env{t: t, pool: pool, srv: srv}
}

// do sends a request as token (empty for none) and decodes a JSON response into out (if non-nil).
func (e *env) do(method, path, token string, body, out any) int {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("marshal body: %v", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(e.t.Context(), method, e.srv.URL+path, rd)
	if err != nil {
		e.t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			e.t.Fatalf("%s %s: decode response: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

func (e *env) follow(token string, slugs ...string) {
	e.t.Helper()
	if code := e.do(http.MethodPut, "/v1/me/topics", token, map[string]any{"topics": slugs}, nil); code != http.StatusOK {
		e.t.Fatalf("follow %v: status %d", slugs, code)
	}
}

func (e *env) insertStory(title string, publishedAt time.Time, topicSlugs ...string) int64 {
	e.t.Helper()
	var id int64
	err := e.pool.QueryRow(e.t.Context(), `
		INSERT INTO stories (title, summary, body_md, kind, importance, published_at, model, prompt_version)
		VALUES ($1, 'summary', 'body', 'release', 3, $2, 'test-model', 'v0')
		RETURNING id`, title, publishedAt).Scan(&id)
	if err != nil {
		e.t.Fatalf("insert story: %v", err)
	}
	_, err = e.pool.Exec(e.t.Context(), `
		INSERT INTO story_topics (story_id, topic_id)
		SELECT $1, id FROM topics WHERE slug = ANY($2)`, id, topicSlugs)
	if err != nil {
		e.t.Fatalf("insert story topics: %v", err)
	}
	return id
}

type timelinePage struct {
	Items      []httpapi.StorySummary `json:"items"`
	NextCursor *string                `json:"next_cursor"`
}

func (e *env) timeline(token string, limit int, cursor string) timelinePage {
	e.t.Helper()
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var page timelinePage
	if code := e.do(http.MethodGet, "/v1/timeline?"+q.Encode(), token, nil, &page); code != http.StatusOK {
		e.t.Fatalf("timeline: status %d", code)
	}
	return page
}

func ids(items []httpapi.StorySummary) []int64 {
	out := make([]int64, len(items))
	for i, it := range items {
		out[i] = it.Id
	}
	return out
}

func TestTimelineOrderAndReadMoves(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	s1 := e.insertStory("newest", now.Add(-1*time.Hour), "languages/go")
	s2 := e.insertStory("middle", now.Add(-2*time.Hour), "languages/rust")
	s3 := e.insertStory("oldest", now.Add(-3*time.Hour), "languages")
	e.insertStory("other topic", now.Add(-30*time.Minute), "web/react")
	e.insertStory("outside window", now.Add(-testWindow-time.Hour), "languages/go")

	// Following a parent topic includes its descendants.
	e.follow(aliceToken, "languages")

	assertOrder := func(want ...int64) []httpapi.StorySummary {
		t.Helper()
		got := e.timeline(aliceToken, 50, "").Items
		if !slices.Equal(ids(got), want) {
			t.Fatalf("timeline order = %v, want %v", ids(got), want)
		}
		return got
	}

	assertOrder(s1, s2, s3)

	if code := e.do(http.MethodPut, "/v1/stories/"+strconv.FormatInt(s1, 10)+"/read", aliceToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("mark read: status %d", code)
	}
	items := assertOrder(s2, s3, s1)
	if !items[2].IsRead || items[2].ReadAt == nil || items[0].IsRead {
		t.Fatalf("read flags wrong after mark read: %+v", items)
	}

	// Read items stay newest-first within the read section.
	e.do(http.MethodPut, "/v1/stories/"+strconv.FormatInt(s3, 10)+"/read", aliceToken, nil, nil)
	assertOrder(s2, s1, s3)

	if code := e.do(http.MethodDelete, "/v1/stories/"+strconv.FormatInt(s1, 10)+"/read", aliceToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("mark unread: status %d", code)
	}
	assertOrder(s1, s2, s3)

	// Read state is per user.
	bob := auth.DevTokenPrefix + "bob"
	e.follow(bob, "languages")
	if got := ids(e.timeline(bob, 50, "").Items); !slices.Equal(got, []int64{s1, s2, s3}) {
		t.Fatalf("bob timeline = %v, want all unread", got)
	}
}

func TestTimelineCursorPagination(t *testing.T) {
	e := newEnv(t)
	e.follow(aliceToken, "languages/go")

	now := time.Now().Truncate(time.Second)
	var all []int64
	for i := range 9 {
		// Pairs share a published_at so the id tie-breaker is exercised.
		all = append(all, e.insertStory("story", now.Add(-time.Duration(i/2)*time.Hour), "languages/go"))
	}
	for _, id := range []int64{all[1], all[4], all[5]} {
		e.do(http.MethodPut, "/v1/stories/"+strconv.FormatInt(id, 10)+"/read", aliceToken, nil, nil)
	}

	full := ids(e.timeline(aliceToken, 100, "").Items)
	if len(full) != len(all) {
		t.Fatalf("full timeline has %d items, want %d", len(full), len(all))
	}

	for _, limit := range []int{1, 2, 4, 9, 10} {
		var walked []int64
		cursor := ""
		for pages := 0; ; pages++ {
			if pages > len(all) {
				t.Fatalf("limit %d: pagination did not terminate", limit)
			}
			page := e.timeline(aliceToken, limit, cursor)
			if len(page.Items) > limit {
				t.Fatalf("limit %d: page has %d items", limit, len(page.Items))
			}
			walked = append(walked, ids(page.Items)...)
			if page.NextCursor == nil {
				break
			}
			cursor = *page.NextCursor
		}
		if !slices.Equal(walked, full) {
			t.Fatalf("limit %d: paged order = %v, want %v", limit, walked, full)
		}
	}
}

func TestTimelineRejectsBadParams(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{"limit=0", "limit=101", "limit=abc", "cursor=not-a-cursor", "cursor=e30"} {
		if code := e.do(http.MethodGet, "/v1/timeline?"+q, aliceToken, nil, nil); code != http.StatusBadRequest {
			t.Errorf("GET /v1/timeline?%s: status %d, want 400", q, code)
		}
	}
}

func TestAuthRequired(t *testing.T) {
	e := newEnv(t)
	for _, token := range []string{"", "not-a-dev-token", auth.DevTokenPrefix} {
		if code := e.do(http.MethodGet, "/v1/me", token, nil, nil); code != http.StatusUnauthorized {
			t.Errorf("token %q: status %d, want 401", token, code)
		}
	}
	if code := e.do(http.MethodGet, "/healthz", "", nil, nil); code != http.StatusOK {
		t.Errorf("healthz: status %d, want 200", code)
	}
}

func TestMeAndTopics(t *testing.T) {
	e := newEnv(t)

	var list struct {
		Items []httpapi.Topic `json:"items"`
	}
	if code := e.do(http.MethodGet, "/v1/topics", aliceToken, nil, &list); code != http.StatusOK || len(list.Items) == 0 {
		t.Fatalf("list topics: status %d, %d items", code, len(list.Items))
	}

	var me httpapi.Me
	e.do(http.MethodGet, "/v1/me", aliceToken, nil, &me)
	if me.Id == 0 || len(me.Topics) != 0 {
		t.Fatalf("new user = %+v, want id and no topics", me)
	}

	if code := e.do(http.MethodPut, "/v1/me/topics", aliceToken, map[string]any{"topics": []string{"languages/go", "nope"}}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown topic: status %d, want 400", code)
	}

	e.do(http.MethodPut, "/v1/me/topics", aliceToken, map[string]any{"topics": []string{"security", "languages/go", "security"}}, &me)
	if want := []string{"languages/go", "security"}; !slices.Equal(me.Topics, want) {
		t.Fatalf("topics = %v, want %v", me.Topics, want)
	}
	// A rejected update leaves the previous set intact.
	e.do(http.MethodGet, "/v1/me", aliceToken, nil, &me)
	if len(me.Topics) != 2 {
		t.Fatalf("topics after reload = %v", me.Topics)
	}
}

func TestMeStats(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	a := e.insertStory("A", now.Add(-2*time.Hour), "languages/go")
	b := e.insertStory("B", now.Add(-1*time.Hour), "languages/go")
	path := func(id int64, action string) string { return "/v1/stories/" + strconv.FormatInt(id, 10) + "/" + action }

	var me httpapi.Me
	e.do(http.MethodGet, "/v1/me", aliceToken, nil, &me)
	if me.Stats != (httpapi.MeStats{}) {
		t.Fatalf("new user stats = %+v, want zero", me.Stats)
	}

	for _, req := range []struct{ method, path string }{
		{http.MethodPut, path(a, "bookmark")},
		{http.MethodPut, path(b, "bookmark")},
		{http.MethodPut, path(a, "read")},
		{http.MethodPut, path(a, "read")}, // idempotent
		{http.MethodPut, path(b, "bookmark")},
	} {
		if code := e.do(req.method, req.path, aliceToken, nil, nil); code != http.StatusNoContent {
			t.Fatalf("%s %s: status %d", req.method, req.path, code)
		}
	}
	e.do(http.MethodGet, "/v1/me", aliceToken, nil, &me)
	if want := (httpapi.MeStats{Saved: 2, Read: 1}); me.Stats != want {
		t.Fatalf("stats = %+v, want %+v", me.Stats, want)
	}

	// PUT /v1/me/topics returns the same stats, and other users' state doesn't count.
	e.do(http.MethodPut, "/v1/me/topics", aliceToken, map[string]any{"topics": []string{"languages/go"}}, &me)
	if want := (httpapi.MeStats{Saved: 2, Read: 1}); me.Stats != want {
		t.Fatalf("stats after put topics = %+v, want %+v", me.Stats, want)
	}
	var bob httpapi.Me
	e.do(http.MethodGet, "/v1/me", bobToken, nil, &bob)
	if bob.Stats != (httpapi.MeStats{}) {
		t.Fatalf("bob stats = %+v, want zero", bob.Stats)
	}
}

func TestStoryDetailAndNotFound(t *testing.T) {
	e := newEnv(t)
	id := e.insertStory("detail", time.Now(), "languages/go", "security")
	if _, err := e.pool.Exec(t.Context(),
		`INSERT INTO story_sources (story_id, url, source_name) VALUES ($1, 'https://go.dev/blog/x', 'Go Blog')`, id); err != nil {
		t.Fatalf("insert source: %v", err)
	}

	var story httpapi.Story
	if code := e.do(http.MethodGet, "/v1/stories/"+strconv.FormatInt(id, 10), aliceToken, nil, &story); code != http.StatusOK {
		t.Fatalf("get story: status %d", code)
	}
	if story.BodyMd != "body" || len(story.Sources) != 1 || !slices.Equal(story.Topics, []string{"languages/go", "security"}) {
		t.Fatalf("story = %+v", story)
	}

	missing := "/v1/stories/999999"
	for _, req := range []struct{ method, path string }{
		{http.MethodGet, missing},
		{http.MethodPut, missing + "/read"},
		{http.MethodDelete, missing + "/read"},
	} {
		if code := e.do(req.method, req.path, aliceToken, nil, nil); code != http.StatusNotFound {
			t.Errorf("%s %s: status %d, want 404", req.method, req.path, code)
		}
	}
}
