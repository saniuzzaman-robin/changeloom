package httpapi_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestBookmarks(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	a := e.insertStory("A", now.Add(-3*time.Hour), "languages/go")
	b := e.insertStory("B", now.Add(-2*time.Hour), "languages/go")
	c := e.insertStory("C", now.Add(-1*time.Hour), "languages/go")
	e.follow(aliceToken, "languages/go")

	for _, id := range []int64{a, c, b} { // bookmark order: a, c, b
		if code := e.do(http.MethodPut, "/v1/stories/"+strconv.FormatInt(id, 10)+"/bookmark", aliceToken, nil, nil); code != http.StatusNoContent {
			t.Fatalf("bookmark %d: status %d", id, code)
		}
		time.Sleep(5 * time.Millisecond) // distinct created_at values
	}
	// Idempotent.
	e.do(http.MethodPut, "/v1/stories/"+strconv.FormatInt(a, 10)+"/bookmark", aliceToken, nil, nil)

	var page timelinePage
	if code := e.do(http.MethodGet, "/v1/bookmarks?limit=2", aliceToken, nil, &page); code != http.StatusOK {
		t.Fatalf("bookmarks: status %d", code)
	}
	if got, want := ids(page.Items), []int64{b, c}; !slices.Equal(got, want) {
		t.Fatalf("first page = %v, want %v (most recently bookmarked first)", got, want)
	}
	if page.NextCursor == nil {
		t.Fatal("expected a next cursor")
	}
	var page2 timelinePage
	e.do(http.MethodGet, "/v1/bookmarks?limit=2&cursor="+url.QueryEscape(*page.NextCursor), aliceToken, nil, &page2)
	if got, want := ids(page2.Items), []int64{a}; !slices.Equal(got, want) || page2.NextCursor != nil {
		t.Fatalf("second page = %v (next %v), want %v and no cursor", got, page2.NextCursor, want)
	}

	tl := e.timeline(aliceToken, 10, "")
	for _, it := range tl.Items {
		if !it.IsBookmarked {
			t.Errorf("timeline story %d: is_bookmarked = false", it.Id)
		}
	}

	if code := e.do(http.MethodDelete, "/v1/stories/"+strconv.FormatInt(a, 10)+"/bookmark", aliceToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("unbookmark: status %d", code)
	}
	var after timelinePage
	e.do(http.MethodGet, "/v1/bookmarks", aliceToken, nil, &after)
	if got, want := ids(after.Items), []int64{b, c}; !slices.Equal(got, want) {
		t.Fatalf("after remove = %v, want %v", got, want)
	}
	if code := e.do(http.MethodPut, "/v1/stories/999999/bookmark", aliceToken, nil, nil); code != http.StatusNotFound {
		t.Errorf("bookmark unknown story: status %d, want 404", code)
	}
	var other timelinePage
	e.do(http.MethodGet, "/v1/bookmarks", bobToken, nil, &other)
	if len(other.Items) != 0 {
		t.Errorf("another user sees %d bookmarks, want 0", len(other.Items))
	}
}

func TestSearch(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	e.insertStory("Go 1.30 released with new iterators", now.Add(-2*time.Hour), "languages/go")
	rust := e.insertStory("Rust 2.0 borrow checker overhaul", now.Add(-1*time.Hour), "languages/rust")
	e.follow(aliceToken, "languages/go") // search covers stories outside followed topics

	var page timelinePage
	q := url.Values{"q": {"borrow checker"}}
	if code := e.do(http.MethodGet, "/v1/search?"+q.Encode(), aliceToken, nil, &page); code != http.StatusOK {
		t.Fatalf("search: status %d", code)
	}
	if got, want := ids(page.Items), []int64{rust}; !slices.Equal(got, want) {
		t.Fatalf("search = %v, want %v", got, want)
	}

	var none timelinePage
	e.do(http.MethodGet, "/v1/search?q=nonexistentterm", aliceToken, nil, &none)
	if len(none.Items) != 0 {
		t.Errorf("search for unknown term returned %d items", len(none.Items))
	}
	for _, bad := range []string{"/v1/search", "/v1/search?q=%20", "/v1/search?q=go&limit=0"} {
		if code := e.do(http.MethodGet, bad, aliceToken, nil, nil); code != http.StatusBadRequest {
			t.Errorf("GET %s: status %d, want 400", bad, code)
		}
	}
}

func TestDeviceTokens(t *testing.T) {
	e := newEnv(t)
	body := map[string]string{"token": "tok-1", "platform": "android"}
	if code := e.do(http.MethodPut, "/v1/me/devices", aliceToken, body, nil); code != http.StatusNoContent {
		t.Fatalf("register: status %d", code)
	}
	// Same token from another user moves it.
	if code := e.do(http.MethodPut, "/v1/me/devices", bobToken, body, nil); code != http.StatusNoContent {
		t.Fatalf("re-register: status %d", code)
	}
	var owner int64
	if err := e.pool.QueryRow(t.Context(), `SELECT user_id FROM device_tokens WHERE token = 'tok-1'`).Scan(&owner); err != nil {
		t.Fatalf("look up token: %v", err)
	}
	var me struct {
		ID int64 `json:"id"`
	}
	e.do(http.MethodGet, "/v1/me", bobToken, nil, &me)
	if owner != me.ID {
		t.Errorf("token owner = %d, want bob (%d)", owner, me.ID)
	}
	// Alice cannot unregister bob's token.
	e.do(http.MethodDelete, "/v1/me/devices?token=tok-1", aliceToken, nil, nil)
	var n int
	_ = e.pool.QueryRow(t.Context(), `SELECT count(*) FROM device_tokens`).Scan(&n)
	if n != 1 {
		t.Fatalf("token count after foreign delete = %d, want 1", n)
	}
	if code := e.do(http.MethodDelete, "/v1/me/devices?token=tok-1", bobToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("unregister: status %d", code)
	}
	_ = e.pool.QueryRow(t.Context(), `SELECT count(*) FROM device_tokens`).Scan(&n)
	if n != 0 {
		t.Errorf("token count after delete = %d, want 0", n)
	}
	for _, bad := range []map[string]string{{"token": "", "platform": "android"}, {"token": "x", "platform": "web"}} {
		if code := e.do(http.MethodPut, "/v1/me/devices", aliceToken, bad, nil); code != http.StatusBadRequest {
			t.Errorf("register %v: status %d, want 400", bad, code)
		}
	}
}
