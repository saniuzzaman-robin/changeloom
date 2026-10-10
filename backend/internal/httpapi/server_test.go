package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	testWindow     = 60 * 24 * time.Hour
	testMaxPending = 3
	aliceToken     = auth.DevTokenPrefix + "alice"
	bobToken       = auth.DevTokenPrefix + "bob"
)

// testScore is the api's default ranking: with stories of equal importance a few hours apart,
// tiers decide the order.
var testScore = httpapi.TimelineScore{
	TierWeight: 2, ImportanceWeight: 1, SeverityWeight: 0.5, AgeDecay: 24 * time.Hour, SeenPenalty: 3, SeenGrace: 12 * time.Hour,
}

type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	srv  *httptest.Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	return newEnvWith(t, func(*httpapi.Options) {})
}

// newEnvWith is newEnv with test defaults adjusted by configure.
func newEnvWith(t *testing.T, configure func(*httpapi.Options)) *env {
	t.Helper()
	pool := dbtest.New(t)
	if _, err := topics.Sync(t.Context(), pool, seed.TopicsYAML); err != nil {
		t.Fatalf("sync topics: %v", err)
	}
	opts := httpapi.Options{
		TimelineWindow: testWindow, TopicRequestMaxPending: testMaxPending,
		TimelineScore: testScore,
	}
	configure(&opts)
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.NewServer(pool, opts), auth.DevVerifier{}))
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

// relate stores a symmetric relation between two topics.
func (e *env) relate(a, b string) {
	e.t.Helper()
	_, err := e.pool.Exec(e.t.Context(), `
		INSERT INTO topic_relations (topic_id, related_id)
		SELECT least(x.id, y.id), greatest(x.id, y.id) FROM topics x, topics y WHERE x.slug = $1 AND y.slug = $2`, a, b)
	if err != nil {
		e.t.Fatalf("relate %s and %s: %v", a, b, err)
	}
}

func (e *env) markRead(token string, id int64) {
	e.t.Helper()
	if code := e.do(http.MethodPut, "/v1/stories/"+strconv.FormatInt(id, 10)+"/read", token, nil, nil); code != http.StatusNoContent {
		e.t.Fatalf("mark %d read: status %d", id, code)
	}
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

	// Following a parent topic includes its descendants; other topics don't show up.
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

func TestTimelineTiers(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	followed := e.insertStory("followed", now.Add(-3*time.Hour), "languages/go")
	child := e.insertStory("followed child", now.Add(-4*time.Hour), "cloud/docker")
	mixed := e.insertStory("followed and other", now.Add(-6*time.Hour), "web/react", "languages/go")
	e.insertStory("relation neighbour", now.Add(-2*time.Hour), "databases/postgres")
	e.insertStory("ancestor", now.Add(-5*time.Hour), "languages")
	e.insertStory("sibling", now.Add(-1*time.Hour), "languages/rust")

	e.follow(aliceToken, "languages/go", "cloud")
	e.relate("databases/postgres", "languages/go")

	// Only stories of a followed topic (or a descendant) show up: neighbours, ancestors and siblings don't.
	got := e.timeline(aliceToken, 50, "").Items
	want := []int64{followed, child, mixed}
	if !slices.Equal(ids(got), want) {
		t.Fatalf("timeline order = %v, want %v", ids(got), want)
	}
	for _, it := range got {
		if it.Match == nil || *it.Match != httpapi.StorySummaryMatchFollowed {
			t.Errorf("story %d (%s): match = %v, want followed", it.Id, it.Title, it.Match)
		}
	}

	e.markRead(aliceToken, followed)
	if got := ids(e.timeline(aliceToken, 50, "").Items); !slices.Equal(got, []int64{child, mixed, followed}) {
		t.Fatalf("timeline after read = %v", got)
	}
}

func TestTimelineWithoutInterestsIsEmpty(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	e.insertStory("go", now.Add(-2*time.Hour), "languages/go")
	e.insertStory("react", now.Add(-1*time.Hour), "web/react")

	if got := e.timeline(aliceToken, 50, ""); len(got.Items) != 0 || got.NextCursor != nil {
		t.Fatalf("timeline = %v, want empty", ids(got.Items))
	}
}

func TestTimelineCursorPagination(t *testing.T) {
	e := newEnv(t)
	e.follow(aliceToken, "languages/go")
	// data-scientist maps to ai, databases and languages.
	if code := e.do(http.MethodPut, "/v1/me/professions", aliceToken, map[string]any{"professions": []string{"data-scientist"}}, nil); code != http.StatusOK {
		t.Fatalf("put professions: status %d", code)
	}

	now := time.Now().Truncate(time.Second)
	tierTopics := []string{"languages/go", "databases/postgres"}
	tierOf := map[int64]int{}
	var all []int64
	for i := range 15 {
		// Pairs share a published_at so the id tie-breaker is exercised, and tiers interleave
		// by time so every page boundary can fall inside or between tiers.
		id := e.insertStory("story", now.Add(-time.Duration(i/2)*time.Hour), tierTopics[i%2])
		tierOf[id] = i % 2
		all = append(all, id)
	}
	read := map[int64]bool{}
	for _, id := range []int64{all[1], all[4], all[5], all[9]} {
		e.markRead(aliceToken, id)
		read[id] = true
	}

	full := e.timeline(aliceToken, 100, "").Items
	if len(full) != len(all) {
		t.Fatalf("full timeline has %d items, want %d", len(full), len(all))
	}
	// Expected order: unread first, then tier (equal importance and only hours apart, so the score
	// follows the tier), then newest, then highest id; read ones newest first.
	want := slices.Clone(all)
	pub := map[int64]time.Time{}
	for _, it := range full {
		pub[it.Id] = it.PublishedAt
	}
	slices.SortFunc(want, func(a, b int64) int {
		switch {
		case read[a] != read[b]:
			if read[a] {
				return 1
			}
			return -1
		case !read[a] && tierOf[a] != tierOf[b]:
			return tierOf[a] - tierOf[b]
		case !pub[a].Equal(pub[b]):
			return pub[b].Compare(pub[a])
		default:
			return int(b - a)
		}
	})
	if !slices.Equal(ids(full), want) {
		t.Fatalf("full timeline = %v, want %v", ids(full), want)
	}

	for _, limit := range []int{1, 2, 4, 9, 15, 16} {
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
		if !slices.Equal(walked, want) {
			t.Fatalf("limit %d: paged order = %v, want %v", limit, walked, want)
		}
	}
}

func TestTimelineKindAndReadFilters(t *testing.T) {
	e := newEnv(t)
	now := time.Now().Truncate(time.Second)
	rel1 := e.insertStory("release 1", now.Add(-1*time.Hour), "languages/go")
	sec1 := e.insertStory("security 1", now.Add(-2*time.Hour), "languages/go")
	dep1 := e.insertStory("deprecation 1", now.Add(-3*time.Hour), "web/react")
	sec2 := e.insertStory("security 2", now.Add(-4*time.Hour), "web/react")
	for id, kind := range map[int64]string{sec1: "security", sec2: "security", dep1: "deprecation"} {
		if _, err := e.pool.Exec(t.Context(), `UPDATE stories SET kind = $2 WHERE id = $1`, id, kind); err != nil {
			t.Fatalf("set kind: %v", err)
		}
	}
	e.follow(aliceToken, "languages/go", "web/react")
	e.markRead(aliceToken, sec1)
	e.markRead(aliceToken, rel1)

	get := func(query string) []int64 {
		t.Helper()
		var page timelinePage
		if code := e.do(http.MethodGet, "/v1/timeline?"+query, aliceToken, nil, &page); code != http.StatusOK {
			t.Fatalf("GET /v1/timeline?%s: status %d", query, code)
		}
		return ids(page.Items)
	}
	for _, tc := range []struct {
		query string
		want  []int64
	}{
		{"", []int64{dep1, sec2, rel1, sec1}},
		{"kind=security", []int64{sec2, sec1}},
		{"kind=security&kind=deprecation", []int64{dep1, sec2, sec1}},
		{"read=true", []int64{rel1, sec1}},
		{"read=false", []int64{dep1, sec2}},
		{"kind=security&read=false", []int64{sec2}},
		{"kind=security&read=true", []int64{sec1}},
		{"kind=policy", []int64{}},
	} {
		if got := get(tc.query); !slices.Equal(got, tc.want) {
			t.Errorf("GET /v1/timeline?%s = %v, want %v", tc.query, got, tc.want)
		}
	}

	// Paging keeps the filter: the cursor only carries the position.
	var paged []int64
	cursor := ""
	for range 5 {
		q := url.Values{"limit": {"1"}, "kind": {"security"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var page timelinePage
		if code := e.do(http.MethodGet, "/v1/timeline?"+q.Encode(), aliceToken, nil, &page); code != http.StatusOK {
			t.Fatalf("paged: status %d", code)
		}
		paged = append(paged, ids(page.Items)...)
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if !slices.Equal(paged, []int64{sec2, sec1}) {
		t.Fatalf("paged security = %v, want %v", paged, []int64{sec2, sec1})
	}

	for _, q := range []string{"kind=bogus", "read=maybe", "kind=release&kind=release&kind=release&kind=release&kind=release&kind=release&kind=release&kind=release&kind=release&kind=release"} {
		if code := e.do(http.MethodGet, "/v1/timeline?"+q, aliceToken, nil, nil); code != http.StatusBadRequest {
			t.Errorf("GET /v1/timeline?%s: status %d, want 400", q, code)
		}
	}
}

func TestTimelineRejectsBadParams(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{"limit=0", "limit=101", "limit=abc", "cursor=not-a-cursor", "cursor=e30",
		// A tier cursor from before scores: {"r":false,"t":1,"p":"2026-01-01T00:00:00Z","i":1}.
		"cursor=eyJyIjpmYWxzZSwidCI6MSwicCI6IjIwMjYtMDEtMDFUMDA6MDA6MDBaIiwiaSI6MX0",
		// A scored cursor without its ranking time: {"v":2,"r":false,"s":1,"p":"2026-01-01T00:00:00Z","i":1}.
		"cursor=eyJ2IjoyLCJyIjpmYWxzZSwicyI6MSwicCI6IjIwMjYtMDEtMDFUMDA6MDA6MDBaIiwiaSI6MX0",
	} {
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
	for _, path := range []string{"/healthz", "/health", "/ready"} {
		if code := e.do(http.MethodGet, path, "", nil, nil); code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", path, code)
		}
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
	e.follow(aliceToken, "languages/go")
	e.follow(bobToken, "languages/go")
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

func TestTopicRequests(t *testing.T) {
	e := newEnv(t)
	create := func(token, text string) (int, httpapi.TopicRequest) {
		t.Helper()
		var tr httpapi.TopicRequest
		code := e.do(http.MethodPost, "/v1/topic-requests", token, map[string]any{"text": text}, &tr)
		return code, tr
	}

	for _, text := range []string{"", " x ", strings.Repeat("a", 101)} {
		if code, _ := create(aliceToken, text); code != http.StatusBadRequest {
			t.Errorf("text %q: status %d, want 400", text, code)
		}
	}
	if code := e.do(http.MethodPost, "/v1/topic-requests", aliceToken, map[string]any{"text": "Zig", "extra": 1}, nil); code != http.StatusBadRequest {
		t.Errorf("unknown field: status %d, want 400", code)
	}

	code, zig := create(aliceToken, "  Zig  ")
	if code != http.StatusCreated || zig.Id == 0 || zig.Text != "Zig" || zig.Status != httpapi.Pending || zig.Topic != nil {
		t.Fatalf("create: status %d, %+v", code, zig)
	}
	// The same text is a duplicate while pending, case-insensitively, but only for the same user.
	if code, _ := create(aliceToken, "zig"); code != http.StatusConflict {
		t.Fatalf("duplicate: status %d, want 409", code)
	}
	if code, _ := create(bobToken, "Zig"); code != http.StatusCreated {
		t.Fatalf("other user: status %d, want 201", code)
	}
	// Once resolved, the same text can be requested again.
	if _, err := e.pool.Exec(t.Context(), `
		UPDATE topic_requests SET status = 'merged', note = 'see Go',
			topic_id = (SELECT id FROM topics WHERE slug = 'languages/go'), resolved_at = now()
		WHERE id = $1`, zig.Id); err != nil {
		t.Fatalf("resolve request: %v", err)
	}
	if code, _ := create(aliceToken, "zig"); code != http.StatusCreated {
		t.Fatalf("after resolve: status %d, want 201", code)
	}

	// The pending cap counts only pending requests.
	for i := 2; i <= testMaxPending; i++ {
		if code, _ := create(aliceToken, "topic "+strconv.Itoa(i)); code != http.StatusCreated {
			t.Fatalf("request %d: status %d, want 201", i, code)
		}
	}
	if code, _ := create(aliceToken, "one too many"); code != http.StatusTooManyRequests {
		t.Fatalf("over cap: status %d, want 429", code)
	}

	var list struct {
		Items []httpapi.TopicRequest `json:"items"`
	}
	if code := e.do(http.MethodGet, "/v1/topic-requests", aliceToken, nil, &list); code != http.StatusOK {
		t.Fatalf("list: status %d", code)
	}
	if len(list.Items) != testMaxPending+1 {
		t.Fatalf("list has %d items, want %d", len(list.Items), testMaxPending+1)
	}
	merged := list.Items[len(list.Items)-1]
	if merged.Id != zig.Id || merged.Status != httpapi.Merged || merged.Topic == nil || *merged.Topic != "languages/go" ||
		merged.Note == nil || *merged.Note != "see Go" || merged.ResolvedAt == nil {
		t.Fatalf("resolved request = %+v", merged)
	}
	if list.Items[0].Text != "topic "+strconv.Itoa(testMaxPending) {
		t.Fatalf("list not newest first: %+v", list.Items[0])
	}
}

type fakeNotifier struct {
	sent, remaining int
	err             error
	runs            int
}

func (f *fakeNotifier) Run(context.Context) (int, int, error) {
	f.runs++
	return f.sent, f.remaining, f.err
}

func TestNotify(t *testing.T) {
	const secret = "s3cret"
	notify := func(e *env, token string) (int, map[string]int) {
		t.Helper()
		var out map[string]int
		return e.do(http.MethodPost, "/internal/notify", token, nil, &out), out
	}

	disabled := newEnv(t)
	if code, _ := notify(disabled, secret); code != http.StatusNotFound {
		t.Errorf("no secret configured: status %d, want 404", code)
	}

	fake := &fakeNotifier{sent: 2, remaining: 5}
	e := newEnvWith(t, func(o *httpapi.Options) { o.NotifySecret, o.Notifier = secret, fake })
	for _, token := range []string{"", "wrong", secret + "x", aliceToken} {
		if code, _ := notify(e, token); code != http.StatusUnauthorized {
			t.Errorf("token %q: status %d, want 401", token, code)
		}
	}
	if fake.runs != 0 {
		t.Fatalf("notifier ran %d times on rejected requests", fake.runs)
	}
	if code, out := notify(e, secret); code != http.StatusOK || out["sent"] != 2 || out["remaining"] != 5 || fake.runs != 1 {
		t.Fatalf("notify: status %d, body %v, runs %d", code, out, fake.runs)
	}
	if code := e.do(http.MethodGet, "/internal/notify", secret, nil, nil); code != http.StatusMethodNotAllowed {
		t.Errorf("GET: status %d, want 405", code)
	}

	fake.err = errors.New("fcm down")
	if code, _ := notify(e, secret); code != http.StatusInternalServerError {
		t.Errorf("notifier error: status %d, want 500", code)
	}

	pushOff := newEnvWith(t, func(o *httpapi.Options) { o.NotifySecret = secret })
	if code, out := notify(pushOff, secret); code != http.StatusOK || out["sent"] != 0 {
		t.Fatalf("push disabled: status %d, body %v", code, out)
	}
}

func TestTopicRequestCapHoldsUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	const attempts = 3 * testMaxPending
	codes := make(chan int, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Go(func() {
			codes <- e.do(http.MethodPost, "/v1/topic-requests", aliceToken, map[string]any{"text": "topic " + strconv.Itoa(i)}, nil)
		})
	}
	wg.Wait()
	close(codes)
	created := 0
	for code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusTooManyRequests:
		default:
			t.Errorf("status %d, want 201 or 429", code)
		}
	}
	if created != testMaxPending {
		t.Fatalf("created %d requests in parallel, want the cap of %d", created, testMaxPending)
	}
}

// emailVerifier accepts any token as the UID and returns the email set for it.
type emailVerifier struct {
	mu     sync.Mutex
	emails map[string]string
}

func (v *emailVerifier) Verify(_ context.Context, token string) (auth.Identity, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	id := auth.Identity{UID: token}
	if email, ok := v.emails[token]; ok {
		id.Email = &email
	}
	return id, nil
}

func TestMeRefreshesEmail(t *testing.T) {
	pool := dbtest.New(t)
	verifier := &emailVerifier{emails: map[string]string{"u": "old@example.com"}}
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.NewServer(pool, httpapi.Options{TimelineWindow: testWindow}), verifier))
	t.Cleanup(srv.Close)
	e := &env{t: t, pool: pool, srv: srv}

	email := func() string {
		t.Helper()
		var me httpapi.Me
		if code := e.do(http.MethodGet, "/v1/me", "u", nil, &me); code != http.StatusOK || me.Email == nil {
			t.Fatalf("get me: status %d, %+v", code, me)
		}
		return *me.Email
	}
	if got := email(); got != "old@example.com" {
		t.Fatalf("email = %q, want old@example.com", got)
	}
	verifier.mu.Lock()
	verifier.emails["u"] = "new@example.com"
	verifier.mu.Unlock()
	if got := email(); got != "new@example.com" {
		t.Fatalf("email after change = %q, want new@example.com", got)
	}
	verifier.mu.Lock()
	delete(verifier.emails, "u")
	verifier.mu.Unlock()
	if got := email(); got != "new@example.com" {
		t.Fatalf("email from a token without one = %q, want the stored new@example.com", got)
	}
}

func TestDeleteMe(t *testing.T) {
	e := newEnv(t)
	e.follow(aliceToken, "languages/go")
	e.follow(bobToken, "languages/go")
	story := e.insertStory("Go 2", time.Now(), "languages/go")
	e.markRead(aliceToken, story)
	path := "/v1/stories/" + strconv.FormatInt(story, 10) + "/bookmark"
	if code := e.do(http.MethodPut, path, aliceToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("bookmark: status %d", code)
	}
	if code := e.do(http.MethodPost, "/v1/topic-requests", aliceToken, map[string]any{"text": "Zig"}, nil); code != http.StatusCreated {
		t.Fatalf("topic request: status %d", code)
	}
	var aliceID int64
	if err := e.pool.QueryRow(t.Context(), `SELECT id FROM users WHERE firebase_uid = $1`, aliceToken).Scan(&aliceID); err != nil {
		t.Fatalf("alice id: %v", err)
	}

	if code := e.do(http.MethodDelete, "/v1/me", aliceToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete me: status %d, want 204", code)
	}
	var left int
	err := e.pool.QueryRow(t.Context(), `
		SELECT (SELECT count(*) FROM users WHERE id = $1)
			+ (SELECT count(*) FROM user_topics WHERE user_id = $1)
			+ (SELECT count(*) FROM user_story_state WHERE user_id = $1)
			+ (SELECT count(*) FROM user_bookmarks WHERE user_id = $1)
			+ (SELECT count(*) FROM topic_requests WHERE user_id = $1)`, aliceID).Scan(&left)
	if err != nil || left != 0 {
		t.Fatalf("rows left for deleted user = %d (%v), want 0", left, err)
	}
	var me httpapi.Me
	if e.do(http.MethodGet, "/v1/me", bobToken, nil, &me); len(me.Topics) != 1 {
		t.Fatalf("other user's topics = %v, want untouched", me.Topics)
	}
}

func TestCacheHeaders(t *testing.T) {
	e := newEnv(t)
	get := func(path, ifNoneMatch string) (int, http.Header) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e.srv.URL+path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+aliceToken)
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		resp, err := e.srv.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode, resp.Header
	}

	code, header := get("/v1/topics", "")
	etag := header.Get("ETag")
	if code != http.StatusOK || etag == "" || !strings.HasPrefix(header.Get("Cache-Control"), "private, max-age=") {
		t.Fatalf("topics: status %d, ETag %q, Cache-Control %q", code, etag, header.Get("Cache-Control"))
	}
	for _, inm := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
		if code, header := get("/v1/topics", inm); code != http.StatusNotModified || header.Get("ETag") != etag {
			t.Errorf("If-None-Match %q: status %d, ETag %q; want 304 with the same ETag", inm, code, header.Get("ETag"))
		}
	}
	if code, _ := get("/v1/topics", `"stale"`); code != http.StatusOK {
		t.Errorf("stale If-None-Match: status %d, want 200", code)
	}

	for _, path := range []string{"/v1/me", "/v1/timeline", "/v1/nope"} {
		if _, header := get(path, ""); header.Get("Cache-Control") != "private, no-store" {
			t.Errorf("%s: Cache-Control %q, want private, no-store", path, header.Get("Cache-Control"))
		}
	}
}

func TestRateLimits(t *testing.T) {
	e := newEnvWith(t, func(o *httpapi.Options) { o.RateLimitPerUser = 2 })
	for i := range 2 {
		if code := e.do(http.MethodGet, "/v1/me", aliceToken, nil, nil); code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, code)
		}
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e.srv.URL+"/v1/me", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /v1/me: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("over the user limit: status %d, Retry-After %q; want 429 with Retry-After", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if code := e.do(http.MethodGet, "/v1/me", bobToken, nil, nil); code != http.StatusOK {
		t.Fatalf("another user: status %d, want 200", code)
	}

	ipLimited := newEnvWith(t, func(o *httpapi.Options) { o.RateLimitPerIP = 1 })
	if code := ipLimited.do(http.MethodGet, "/v1/me", aliceToken, nil, nil); code != http.StatusOK {
		t.Fatalf("first request: status %d", code)
	}
	if code := ipLimited.do(http.MethodGet, "/v1/me", bobToken, nil, nil); code != http.StatusTooManyRequests {
		t.Fatalf("over the IP limit: status %d, want 429", code)
	}
	if code := ipLimited.do(http.MethodGet, "/health", "", nil, nil); code != http.StatusOK {
		t.Fatalf("health check over the IP limit: status %d, want 200", code)
	}
}

func TestTopicsIncludeProfessions(t *testing.T) {
	e := newEnv(t)
	var got struct {
		Professions []httpapi.Profession `json:"professions"`
	}
	if code := e.do(http.MethodGet, "/v1/topics", aliceToken, nil, &got); code != http.StatusOK {
		t.Fatalf("topics: status %d", code)
	}
	if len(got.Professions) != 2 || got.Professions[0].Slug != "data-scientist" || got.Professions[1].Slug != "software-engineer" {
		t.Fatalf("professions = %+v, want data-scientist then software-engineer (alphabetical)", got.Professions)
	}
	if want := []string{"web", "mobile", "cloud", "languages", "devtools"}; !slices.Equal(got.Professions[1].Topics, want) {
		t.Fatalf("software-engineer topics = %v, want %v", got.Professions[0].Topics, want)
	}
}

func TestPutMyProfessions(t *testing.T) {
	e := newEnv(t)
	put := func(slugs ...string) (int, httpapi.Me) {
		var me httpapi.Me
		code := e.do(http.MethodPut, "/v1/me/professions", aliceToken, map[string]any{"professions": slugs}, &me)
		return code, me
	}

	if code, me := put("software-engineer", "data-scientist"); code != http.StatusOK || !slices.Equal(me.Professions, []string{"software-engineer", "data-scientist"}) {
		t.Fatalf("put: status %d, professions %v", code, me.Professions)
	}
	// The set is replaced, not merged.
	if code, me := put("data-scientist"); code != http.StatusOK || !slices.Equal(me.Professions, []string{"data-scientist"}) {
		t.Fatalf("replace: status %d, professions %v", code, me.Professions)
	}
	var me httpapi.Me
	if code := e.do(http.MethodGet, "/v1/me", aliceToken, nil, &me); code != http.StatusOK || !slices.Equal(me.Professions, []string{"data-scientist"}) {
		t.Fatalf("get me: status %d, professions %v", code, me.Professions)
	}

	// A rejected request leaves the saved professions alone.
	if code, _ := put("software-engineer", "nope"); code != http.StatusBadRequest {
		t.Fatalf("unknown profession: status %d, want 400", code)
	}
	if code := e.do(http.MethodGet, "/v1/me", aliceToken, nil, &me); code != http.StatusOK || !slices.Equal(me.Professions, []string{"data-scientist"}) {
		t.Fatalf("after rejected puts: status %d, professions %v", code, me.Professions)
	}
	if code, me := put([]string{}...); code != http.StatusOK || len(me.Professions) != 0 {
		t.Fatalf("clear: status %d, professions %v", code, me.Professions)
	}
}

func TestRecordStoryViews(t *testing.T) {
	e := newEnv(t)
	s1 := e.insertStory("one", time.Now(), "languages/go")
	s2 := e.insertStory("two", time.Now(), "web/react")
	count := func() int {
		var n int
		if err := e.pool.QueryRow(t.Context(), `SELECT count(*) FROM story_views`).Scan(&n); err != nil {
			t.Fatalf("count views: %v", err)
		}
		return n
	}

	body := map[string]any{"ids": []int64{s1, s2, 999999}}
	for range 2 {
		if code := e.do(http.MethodPost, "/v1/stories/views", aliceToken, body, nil); code != http.StatusNoContent {
			t.Fatalf("record views: status %d", code)
		}
	}
	if got := count(); got != 2 {
		t.Fatalf("views after repeats = %d, want 2 (idempotent, unknown id ignored)", got)
	}
	if code := e.do(http.MethodPost, "/v1/stories/views", bobToken, map[string]any{"ids": []int64{s1}}, nil); code != http.StatusNoContent {
		t.Fatalf("bob views: status %d", code)
	}
	if got := count(); got != 3 {
		t.Fatalf("views with a second viewer = %d, want 3", got)
	}

	tooMany := make([]int64, 101)
	for i := range tooMany {
		tooMany[i] = s1
	}
	if code := e.do(http.MethodPost, "/v1/stories/views", aliceToken, map[string]any{"ids": tooMany}, nil); code != http.StatusBadRequest {
		t.Fatalf("101 ids: status %d, want 400", code)
	}
	if code := e.do(http.MethodPost, "/v1/stories/views", aliceToken, map[string]any{}, nil); code != http.StatusBadRequest {
		t.Fatalf("missing ids: status %d, want 400", code)
	}
}

func TestProfessionEndpointsRequireAuth(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/v1/topics", nil},
		{http.MethodPut, "/v1/me/professions", map[string]any{"professions": []string{}}},
		{http.MethodPost, "/v1/stories/views", map[string]any{"ids": []int64{1}}},
	} {
		for _, token := range []string{"", "not-a-valid-token"} {
			if code := e.do(tc.method, tc.path, token, tc.body, nil); code != http.StatusUnauthorized {
				t.Errorf("%s %s with token %q: status %d, want 401", tc.method, tc.path, token, code)
			}
		}
	}
}

// exec runs a statement against the test database.
func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(e.t.Context(), sql, args...); err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

func TestTimelineScore(t *testing.T) {
	e := newEnv(t)
	now := time.Now().Truncate(time.Second)
	e.follow(aliceToken, "web/react")
	// data-scientist maps to ai, databases and languages.
	if code := e.do(http.MethodPut, "/v1/me/professions", aliceToken, map[string]any{"professions": []string{"data-scientist"}}, nil); code != http.StatusOK {
		t.Fatalf("put professions: status %d", code)
	}

	// A stale, minor followed story still comes before a fresh, important profession one.
	stale := e.insertStory("stale followed", now.Add(-13*24*time.Hour), "web/react")
	fresh := e.insertStory("fresh profession", now.Add(-1*time.Hour), "databases/postgres")
	e.exec(`UPDATE stories SET importance = 1 WHERE id = $1`, stale)
	e.exec(`UPDATE stories SET importance = 5 WHERE id = $1`, fresh)

	// Severity lifts an otherwise equal story (the id tie-break alone would put plain first).
	critical := e.insertStory("critical", now.Add(-2*time.Hour), "databases/postgres")
	plain := e.insertStory("plain", now.Add(-2*time.Hour), "databases/postgres")
	e.exec(`UPDATE stories SET kind = 'security', severity = 'critical' WHERE id = $1`, critical)

	// A story seen long ago but never opened or saved sinks, unless it is of a followed topic;
	// recent views and saved stories don't.
	unseen := e.insertStory("unseen", now.Add(-4*time.Hour), "web/react")
	seenLongAgo := e.insertStory("seen long ago", now.Add(-3*time.Hour), "web/react")
	seenRecently := e.insertStory("seen recently", now.Add(-3*time.Hour), "web/react")
	seenSaved := e.insertStory("seen long ago and saved", now.Add(-3*time.Hour), "web/react")
	if code := e.do(http.MethodPost, "/v1/stories/views", aliceToken, map[string]any{"ids": []int64{seenLongAgo, seenRecently, seenSaved}}, nil); code != http.StatusNoContent {
		t.Fatalf("record views: status %d", code)
	}
	// Outside the followed topics the same long-ago view sinks a story below an unseen twin.
	profUnseen := e.insertStory("profession unseen", now.Add(-5*time.Hour), "databases/postgres")
	profSeen := e.insertStory("profession seen long ago", now.Add(-5*time.Hour), "databases/postgres")
	if code := e.do(http.MethodPost, "/v1/stories/views", aliceToken, map[string]any{"ids": []int64{profSeen}}, nil); code != http.StatusNoContent {
		t.Fatalf("record views: status %d", code)
	}
	e.exec(`UPDATE story_views SET seen_at = $2 WHERE story_id = ANY($1)`, []int64{seenLongAgo, seenSaved, profSeen}, now.Add(-testScore.SeenGrace-time.Hour))
	if code := e.do(http.MethodPut, "/v1/stories/"+strconv.FormatInt(seenSaved, 10)+"/bookmark", aliceToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("bookmark: status %d", code)
	}

	// Unread followed stories first, by score: seen recently, seen long ago (no penalty for a
	// followed topic) and saved 3-3/24 (tie: highest id first); unseen 3-4/24; stale 1-13. Then
	// the profession ones: fresh 5-2-1/24; critical 3-2+1.5-2/24; plain 3-2-2/24; profession unseen
	// 3-2-5/24; profession seen long ago the same -3.
	want := []int64{seenSaved, seenRecently, seenLongAgo, unseen, stale, fresh, critical, plain, profUnseen, profSeen}
	if got := ids(e.timeline(aliceToken, 50, "").Items); !slices.Equal(got, want) {
		t.Fatalf("timeline order = %v, want %v", got, want)
	}

	// Bob's views don't penalize Alice's stories (Alice never saw this one).
	e.follow(bobToken, "web/react")
	if code := e.do(http.MethodPost, "/v1/stories/views", bobToken, map[string]any{"ids": []int64{unseen}}, nil); code != http.StatusNoContent {
		t.Fatalf("record bob's views: status %d", code)
	}
	e.exec(`UPDATE story_views SET seen_at = $1 WHERE story_id = $2`, now.Add(-testScore.SeenGrace-time.Hour), unseen)
	if got := ids(e.timeline(aliceToken, 50, "").Items); !slices.Equal(got, want) {
		t.Fatalf("timeline order after bob's view = %v, want %v", got, want)
	}

	// Pages of every size walk the same order.
	for _, limit := range []int{1, 2, 3} {
		var walked []int64
		cursor := ""
		for range len(want) + 1 {
			page := e.timeline(aliceToken, limit, cursor)
			walked = append(walked, ids(page.Items)...)
			if page.NextCursor == nil {
				break
			}
			cursor = *page.NextCursor
		}
		if !slices.Equal(walked, want) {
			t.Fatalf("limit %d: paged order = %v, want %v", limit, walked, want)
		}
	}
}

func TestTopicsMarkHeadlinesAndLaunches(t *testing.T) {
	e := newEnv(t)
	e.exec(`UPDATE topics SET headline = true WHERE slug = 'security'`)
	e.exec(`UPDATE professions SET launched = false WHERE slug = 'data-scientist'`)
	var got struct {
		Items       []httpapi.Topic      `json:"items"`
		Professions []httpapi.Profession `json:"professions"`
	}
	if code := e.do(http.MethodGet, "/v1/topics", aliceToken, nil, &got); code != http.StatusOK {
		t.Fatalf("topics: status %d", code)
	}
	for _, it := range got.Items {
		// Sent only when true.
		if want := it.Slug == "security"; (it.Headline != nil) != want || (want && !*it.Headline) {
			t.Errorf("topic %s: headline = %v, want %v", it.Slug, it.Headline, want)
		}
	}
	for _, p := range got.Professions {
		if want := p.Slug != "data-scientist"; p.Launched == nil || *p.Launched != want {
			t.Errorf("profession %s: launched = %v, want %v", p.Slug, p.Launched, want)
		}
	}
	// A profession that is not launched stays valid for users who pick it (older apps, earlier picks).
	if code := e.do(http.MethodPut, "/v1/me/professions", aliceToken, map[string]any{"professions": []string{"data-scientist"}}, nil); code != http.StatusOK {
		t.Fatalf("put unlaunched profession: status %d", code)
	}
}

func TestTimelineProfessionTier(t *testing.T) {
	e := newEnv(t)
	now := time.Now().Truncate(time.Second)
	// data-scientist maps to ai, databases and languages. Newer stories sit in lower tiers, so
	// the expected order can only come from the tiers.
	followed := e.insertStory("followed", now.Add(-4*time.Hour), "web/react")
	profession := e.insertStory("profession", now.Add(-3*time.Hour), "databases/postgres")
	e.insertStory("related", now.Add(-2*time.Hour), "cloud/aws")
	e.insertStory("outside", now.Add(-1*time.Hour), "security/advisories")

	e.follow(aliceToken, "web/react")
	e.relate("web/react", "cloud/aws")
	if code := e.do(http.MethodPut, "/v1/me/professions", aliceToken, map[string]any{"professions": []string{"data-scientist"}}, nil); code != http.StatusOK {
		t.Fatalf("put professions: status %d", code)
	}

	// Related and unrelated topics don't show up.
	want := []int64{followed, profession}
	wantMatch := []httpapi.StorySummaryMatch{httpapi.StorySummaryMatchFollowed, httpapi.StorySummaryMatchProfession}
	got := e.timeline(aliceToken, 50, "").Items
	if !slices.Equal(ids(got), want) {
		t.Fatalf("timeline order = %v, want %v", ids(got), want)
	}
	for i, it := range got {
		if it.Match == nil || *it.Match != wantMatch[i] {
			t.Errorf("story %d (%s): match = %v, want %s", it.Id, it.Title, it.Match, wantMatch[i])
		}
	}

	// The cursor carries the profession tier across page boundaries.
	var paged []int64
	cursor := ""
	for range len(want) + 1 {
		page := e.timeline(aliceToken, 1, cursor)
		paged = append(paged, ids(page.Items)...)
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if !slices.Equal(paged, want) {
		t.Fatalf("paged order = %v, want %v", paged, want)
	}
}

func TestDismissStory(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	a := e.insertStory("A", now.Add(-2*time.Hour), "languages/go")
	b := e.insertStory("B", now.Add(-1*time.Hour), "languages/go")
	e.follow(aliceToken, "languages/go")
	e.follow(bobToken, "languages/go")
	path := "/v1/stories/" + strconv.FormatInt(a, 10) + "/dismiss"

	if code := e.do(http.MethodPut, path, "", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("dismiss without auth: status %d, want 401", code)
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		if code := e.do(method, "/v1/stories/999999/dismiss", aliceToken, nil, nil); code != http.StatusNotFound {
			t.Fatalf("%s unknown story: status %d, want 404", method, code)
		}
	}
	for range 2 { // idempotent
		if code := e.do(http.MethodPut, path, aliceToken, nil, nil); code != http.StatusNoContent {
			t.Fatalf("dismiss: status %d, want 204", code)
		}
	}
	if got := ids(e.timeline(aliceToken, 50, "").Items); !slices.Equal(got, []int64{b}) {
		t.Fatalf("timeline after dismiss = %v, want [%d]", got, b)
	}
	if got := ids(e.timeline(bobToken, 50, "").Items); !slices.Equal(got, []int64{b, a}) {
		t.Fatalf("bob's timeline = %v, want [%d %d]", got, b, a)
	}
	// A dismissed story is still a story: it opens and stays saved.
	if code := e.do(http.MethodGet, "/v1/stories/"+strconv.FormatInt(a, 10), aliceToken, nil, nil); code != http.StatusOK {
		t.Fatalf("get dismissed story: status %d, want 200", code)
	}

	for range 2 {
		if code := e.do(http.MethodDelete, path, aliceToken, nil, nil); code != http.StatusNoContent {
			t.Fatalf("undismiss: status %d, want 204", code)
		}
	}
	if got := ids(e.timeline(aliceToken, 50, "").Items); !slices.Equal(got, []int64{b, a}) {
		t.Fatalf("timeline after undismiss = %v, want [%d %d]", got, b, a)
	}
}

func TestPutMyMutedTopics(t *testing.T) {
	e := newEnv(t)
	put := func(token string, body any) (httpapi.Me, int) {
		t.Helper()
		var me httpapi.Me
		code := e.do(http.MethodPut, "/v1/me/muted-topics", token, body, &me)
		return me, code
	}

	if _, code := put("", map[string]any{"topics": []string{"web"}}); code != http.StatusUnauthorized {
		t.Fatalf("without auth: status %d, want 401", code)
	}
	for _, body := range []any{map[string]any{}, map[string]any{"topics": []string{"web", "nope"}}, map[string]any{"topic": "web"}} {
		if _, code := put(aliceToken, body); code != http.StatusBadRequest {
			t.Fatalf("body %v: status %d, want 400", body, code)
		}
	}
	var me httpapi.Me
	e.do(http.MethodGet, "/v1/me", aliceToken, nil, &me)
	if me.MutedTopics != nil {
		t.Fatalf("new user muted = %v, want absent", *me.MutedTopics)
	}

	// Muting a followed topic unfollows it, and following it again unmutes it.
	e.follow(aliceToken, "web", "languages/go")
	me, code := put(aliceToken, map[string]any{"topics": []string{"languages/go", "cloud", "cloud"}})
	if code != http.StatusOK || me.MutedTopics == nil || !slices.Equal(*me.MutedTopics, []string{"cloud", "languages/go"}) {
		t.Fatalf("mute: status %d, muted %v", code, me.MutedTopics)
	}
	if !slices.Equal(me.Topics, []string{"web"}) {
		t.Fatalf("topics after mute = %v, want [web]", me.Topics)
	}
	e.do(http.MethodPut, "/v1/me/topics", aliceToken, map[string]any{"topics": []string{"web", "languages/go"}}, &me)
	if me.MutedTopics == nil || !slices.Equal(*me.MutedTopics, []string{"cloud"}) {
		t.Fatalf("muted after follow = %v, want [cloud]", me.MutedTopics)
	}

	// A rejected update keeps the previous set, and other users' mutes are their own.
	put(aliceToken, map[string]any{"topics": []string{"nope"}})
	e.do(http.MethodGet, "/v1/me", aliceToken, nil, &me)
	if me.MutedTopics == nil || !slices.Equal(*me.MutedTopics, []string{"cloud"}) {
		t.Fatalf("muted after rejected update = %v, want [cloud]", me.MutedTopics)
	}
	var bob httpapi.Me
	e.do(http.MethodGet, "/v1/me", bobToken, nil, &bob)
	if bob.MutedTopics != nil {
		t.Fatalf("bob muted = %v, want absent", *bob.MutedTopics)
	}

	if me, _ = put(aliceToken, map[string]any{"topics": []string{}}); me.MutedTopics != nil {
		t.Fatalf("muted after clearing = %v, want absent", *me.MutedTopics)
	}
}

func TestTimelineMutedTopics(t *testing.T) {
	e := newEnv(t)
	now := time.Now().Truncate(time.Second)
	react := e.insertStory("react", now.Add(-1*time.Hour), "web/react")
	vue := e.insertStory("vue", now.Add(-2*time.Hour), "web/vue")
	e.insertStory("go", now.Add(-3*time.Hour), "languages/go")
	mixed := e.insertStory("go and aws", now.Add(-4*time.Hour), "languages/go", "cloud/aws")
	aws := e.insertStory("aws", now.Add(-5*time.Hour), "cloud/aws")
	e.insertStory("gcp", now.Add(-6*time.Hour), "cloud/gcp")
	e.insertStory("untagged", now.Add(-7*time.Hour))
	mute := func(token string, slugs ...string) {
		t.Helper()
		if code := e.do(http.MethodPut, "/v1/me/muted-topics", token, map[string]any{"topics": slugs}, nil); code != http.StatusOK {
			t.Fatalf("mute %v: status %d", slugs, code)
		}
	}

	// A muted descendant of a followed topic stays muted; a muted root covers its children; a story
	// needs a followed or profession topic that isn't muted, so the rest are left out.
	e.follow(aliceToken, "web")
	mute(aliceToken, "web/vue", "languages")
	if got, want := ids(e.timeline(aliceToken, 50, "").Items), []int64{react}; !slices.Equal(got, want) {
		t.Fatalf("alice's timeline = %v, want %v", got, want)
	}

	// A followed descendant of a muted topic stays followed.
	e.follow(bobToken, "cloud/aws")
	mute(bobToken, "cloud")
	if got, want := ids(e.timeline(bobToken, 50, "").Items), []int64{mixed, aws}; !slices.Equal(got, want) {
		t.Fatalf("bob's timeline = %v, want %v", got, want)
	}

	// Mutes are per user.
	carol := auth.DevTokenPrefix + "carol"
	e.follow(carol, "web")
	if got, want := ids(e.timeline(carol, 50, "").Items), []int64{react, vue}; !slices.Equal(got, want) {
		t.Fatalf("carol's timeline = %v, want %v", got, want)
	}
}

func TestTimelineMix(t *testing.T) {
	e := newEnvWith(t, func(o *httpapi.Options) { o.TimelineMix = httpapi.TimelineMix{MaxTopicRun: 2} })
	now := time.Now().Truncate(time.Second)
	web1 := e.insertStory("web 1", now.Add(-1*time.Hour), "web/react")
	web2 := e.insertStory("web 2", now.Add(-2*time.Hour), "web/vue")
	web3 := e.insertStory("web 3", now.Add(-3*time.Hour), "web/react")
	cloud := e.insertStory("cloud", now.Add(-4*time.Hour), "cloud/aws")
	e.follow(aliceToken, "web", "cloud")

	if got, want := ids(e.timeline(aliceToken, 50, "").Items), []int64{web1, web2, cloud, web3}; !slices.Equal(got, want) {
		t.Fatalf("timeline = %v, want %v", got, want)
	}
	// Pages are mixed one at a time, and the cursor still follows the ranked order.
	first := e.timeline(aliceToken, 3, "")
	if got, want := ids(first.Items), []int64{web1, web2, web3}; !slices.Equal(got, want) || first.NextCursor == nil {
		t.Fatalf("first page = %v (next %v), want %v and a cursor", got, first.NextCursor, want)
	}
	if got := ids(e.timeline(aliceToken, 3, *first.NextCursor).Items); !slices.Equal(got, []int64{cloud}) {
		t.Fatalf("second page = %v, want [%d]", got, cloud)
	}
}
