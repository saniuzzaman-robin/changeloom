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
	opts := httpapi.Options{TimelineWindow: testWindow, TopicRequestMaxPending: testMaxPending}
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
	other := e.insertStory("other topic", now.Add(-30*time.Minute), "web/react")
	e.insertStory("outside window", now.Add(-testWindow-time.Hour), "languages/go")

	// Following a parent topic includes its descendants; other topics rank after them.
	e.follow(aliceToken, "languages")

	assertOrder := func(want ...int64) []httpapi.StorySummary {
		t.Helper()
		got := e.timeline(aliceToken, 50, "").Items
		if !slices.Equal(ids(got), want) {
			t.Fatalf("timeline order = %v, want %v", ids(got), want)
		}
		return got
	}

	assertOrder(s1, s2, s3, other)

	if code := e.do(http.MethodPut, "/v1/stories/"+strconv.FormatInt(s1, 10)+"/read", aliceToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("mark read: status %d", code)
	}
	items := assertOrder(s2, s3, other, s1)
	if !items[3].IsRead || items[3].ReadAt == nil || items[0].IsRead {
		t.Fatalf("read flags wrong after mark read: %+v", items)
	}

	// Read items stay newest-first within the read section.
	e.do(http.MethodPut, "/v1/stories/"+strconv.FormatInt(s3, 10)+"/read", aliceToken, nil, nil)
	assertOrder(s2, other, s1, s3)

	if code := e.do(http.MethodDelete, "/v1/stories/"+strconv.FormatInt(s1, 10)+"/read", aliceToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("mark unread: status %d", code)
	}
	assertOrder(s1, s2, other, s3)

	// Read state is per user.
	bob := auth.DevTokenPrefix + "bob"
	e.follow(bob, "languages")
	if got := ids(e.timeline(bob, 50, "").Items); !slices.Equal(got, []int64{s1, s2, s3, other}) {
		t.Fatalf("bob timeline = %v, want all unread", got)
	}
}

func TestTimelineTiers(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	followed := e.insertStory("followed", now.Add(-3*time.Hour), "languages/go")
	child := e.insertStory("followed child", now.Add(-4*time.Hour), "cloud/docker")
	neighbour := e.insertStory("relation neighbour", now.Add(-2*time.Hour), "databases/postgres")
	ancestor := e.insertStory("ancestor", now.Add(-5*time.Hour), "languages")
	sibling := e.insertStory("sibling", now.Add(-1*time.Hour), "languages/rust")
	mixed := e.insertStory("followed and explore", now.Add(-6*time.Hour), "web/react", "languages/go")

	e.follow(aliceToken, "languages/go", "cloud")
	// Relations are symmetric: the pair is stored once and matches from either side.
	e.relate("databases/postgres", "languages/go")

	got := e.timeline(aliceToken, 50, "").Items
	want := []int64{followed, child, mixed, neighbour, ancestor, sibling}
	if !slices.Equal(ids(got), want) {
		t.Fatalf("timeline order = %v, want %v", ids(got), want)
	}
	wantMatch := []httpapi.StorySummaryMatch{
		httpapi.StorySummaryMatchFollowed, httpapi.StorySummaryMatchFollowed, httpapi.StorySummaryMatchFollowed,
		httpapi.StorySummaryMatchRelated, httpapi.StorySummaryMatchRelated, httpapi.StorySummaryMatchExplore,
	}
	for i, it := range got {
		if it.Match == nil || *it.Match != wantMatch[i] {
			t.Errorf("story %d (%s): match = %v, want %s", it.Id, it.Title, it.Match, wantMatch[i])
		}
	}

	// Unread stories of every tier come before read ones.
	e.markRead(aliceToken, followed)
	if got := ids(e.timeline(aliceToken, 50, "").Items); !slices.Equal(got, []int64{child, mixed, neighbour, ancestor, sibling, followed}) {
		t.Fatalf("timeline after read = %v", got)
	}
}

func TestTimelineWithoutFollowsIsRecencyFeed(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	older := e.insertStory("older", now.Add(-2*time.Hour), "languages/go")
	newer := e.insertStory("newer", now.Add(-1*time.Hour), "web/react")

	got := e.timeline(aliceToken, 50, "").Items
	if !slices.Equal(ids(got), []int64{newer, older}) {
		t.Fatalf("timeline = %v, want %v", ids(got), []int64{newer, older})
	}
	for _, it := range got {
		if it.Match == nil || *it.Match != httpapi.StorySummaryMatchExplore {
			t.Errorf("story %d: match = %v, want explore", it.Id, it.Match)
		}
	}
}

func TestTimelineCursorPagination(t *testing.T) {
	e := newEnv(t)
	e.follow(aliceToken, "languages/go")
	e.relate("languages/go", "web/react")

	now := time.Now().Truncate(time.Second)
	tierTopics := []string{"languages/go", "web/react", "cloud/aws"}
	tierOf := map[int64]int{}
	var all []int64
	for i := range 15 {
		// Pairs share a published_at so the id tie-breaker is exercised, and tiers interleave
		// by time so every page boundary can fall inside or between tiers.
		id := e.insertStory("story", now.Add(-time.Duration(i/2)*time.Hour), tierTopics[i%3])
		tierOf[id] = i % 3
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
	// Expected order: unread first, then tier, then newest, then highest id.
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
		case tierOf[a] != tierOf[b]:
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

func TestTimelineRejectsBadParams(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{"limit=0", "limit=101", "limit=abc", "cursor=not-a-cursor", "cursor=e30",
		// Tier 3 is out of range.
		"cursor=eyJyIjpmYWxzZSwidCI6MywicCI6IjIwMjYtMDEtMDFUMDA6MDA6MDBaIiwiaSI6MX0",
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
