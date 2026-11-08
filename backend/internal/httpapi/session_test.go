package httpapi_test

import (
	"maps"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/auth"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/httpapi"
)

const carolToken = auth.DevTokenPrefix + "carol"

type sessionPage struct {
	Items       []httpapi.StorySummary `json:"items"`
	NextCursor  *string                `json:"next_cursor"`
	UnseenCount *int64                 `json:"unseen_count"`
}

func (e *env) sessionPage(token string, query url.Values) sessionPage {
	e.t.Helper()
	var page sessionPage
	if code := e.do(http.MethodGet, "/v1/timeline?"+query.Encode(), token, nil, &page); code != http.StatusOK {
		e.t.Fatalf("timeline: status %d", code)
	}
	return page
}

func (e *env) recordViews(token string, ids ...int64) {
	e.t.Helper()
	if code := e.do(http.MethodPost, "/v1/stories/views", token, map[string]any{"ids": ids}, nil); code != http.StatusNoContent {
		e.t.Fatalf("record views: status %d", code)
	}
}

func seenByID(items []httpapi.StorySummary) map[int64]bool {
	out := make(map[int64]bool, len(items))
	for _, it := range items {
		if it.Seen == nil {
			continue
		}
		out[it.Id] = *it.Seen
	}
	return out
}

// seen and unseen_count are fixed by the first page's time: a story put on screen while the user
// pages stays new on later pages, and only the next first page counts it as seen.
func TestTimelineSeenAndUnseenCount(t *testing.T) {
	e := newEnv(t)
	now := time.Now().Truncate(time.Second)
	e.follow(aliceToken, "languages/go")
	seen := e.insertStory("seen", now.Add(-3*time.Hour), "languages/go")
	fresh := e.insertStory("fresh", now.Add(-2*time.Hour), "languages/go")
	readUnseen := e.insertStory("read", now.Add(-1*time.Hour), "languages/go")
	security := e.insertStory("security", now.Add(-4*time.Hour), "languages/go")
	e.exec(`UPDATE stories SET kind = 'security' WHERE id = $1`, security)
	e.markRead(aliceToken, readUnseen)
	e.recordViews(aliceToken, seen)
	e.recordViews(bobToken, fresh, security)

	all := e.sessionPage(aliceToken, url.Values{"limit": {"50"}})
	if all.UnseenCount == nil || *all.UnseenCount != 2 {
		t.Fatalf("unseen_count = %v, want 2 (fresh and security; seen is seen, read is read, bob's views don't count)", all.UnseenCount)
	}
	want := map[int64]bool{seen: true, fresh: false, readUnseen: false, security: false}
	if got := seenByID(all.Items); !maps.Equal(got, want) {
		t.Fatalf("seen = %v, want %v", got, want)
	}

	filtered := e.sessionPage(aliceToken, url.Values{"limit": {"50"}, "kind": {"security"}})
	if filtered.UnseenCount == nil || *filtered.UnseenCount != 1 {
		t.Fatalf("unseen_count with kind=security = %v, want 1", filtered.UnseenCount)
	}

	first := e.sessionPage(aliceToken, url.Values{"limit": {"1"}})
	if first.UnseenCount == nil || *first.UnseenCount != 2 || first.NextCursor == nil {
		t.Fatalf("first page = unseen %v, cursor %v; want 2 and a cursor", first.UnseenCount, first.NextCursor)
	}
	e.recordViews(aliceToken, fresh, security)
	rest := map[int64]bool{}
	cursor := *first.NextCursor
	for {
		page := e.sessionPage(aliceToken, url.Values{"limit": {"1"}, "cursor": {cursor}})
		if page.UnseenCount != nil {
			t.Fatalf("later page unseen_count = %d, want absent", *page.UnseenCount)
		}
		maps.Copy(rest, seenByID(page.Items))
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	maps.Copy(rest, seenByID(first.Items))
	if !maps.Equal(rest, want) {
		t.Fatalf("paged seen = %v, want %v (as of the first page)", rest, want)
	}

	again := e.sessionPage(aliceToken, url.Values{"limit": {"50"}})
	if again.UnseenCount == nil || *again.UnseenCount != 0 {
		t.Fatalf("unseen_count after viewing = %v, want 0", again.UnseenCount)
	}
	if got := seenByID(again.Items); !got[fresh] || !got[security] {
		t.Fatalf("seen after viewing = %v, want fresh and security seen", got)
	}

	empty := e.sessionPage(bobToken, url.Values{})
	if empty.UnseenCount == nil || *empty.UnseenCount != 0 || len(empty.Items) != 0 {
		t.Fatalf("bob without topics = %d items, unseen %v; want none and 0", len(empty.Items), empty.UnseenCount)
	}
}

func (e *env) suggestions(token string) []string {
	e.t.Helper()
	var body struct {
		Items []httpapi.Topic `json:"items"`
	}
	if code := e.do(http.MethodGet, "/v1/me/topic-suggestions", token, nil, &body); code != http.StatusOK {
		e.t.Fatalf("topic suggestions: status %d", code)
	}
	if body.Items == nil {
		e.t.Fatal("topic suggestions: items missing")
	}
	out := make([]string, len(body.Items))
	for i, t := range body.Items {
		out[i] = t.Slug
	}
	return out
}

func (e *env) mute(token string, slugs ...string) {
	e.t.Helper()
	if code := e.do(http.MethodPut, "/v1/me/muted-topics", token, map[string]any{"topics": slugs}, nil); code != http.StatusOK {
		e.t.Fatalf("mute %v: status %d", slugs, code)
	}
}

func TestTopicSuggestions(t *testing.T) {
	e := newEnv(t)
	now := time.Now().Truncate(time.Second)
	e.follow(aliceToken, "languages/go", "mobile/android", "web")
	e.mute(aliceToken, "languages/kotlin", "cloud")
	e.relate("languages/go", "databases/postgres")
	e.relate("languages/go", "web/react")      // covered by the followed root
	e.relate("languages/go", "cloud/docker")   // covered by the muted root
	e.relate("mobile/android", "languages/go") // followed itself
	e.follow(bobToken, "languages/python")
	e.mute(bobToken, "languages/rust")

	recent := now.Add(-24 * time.Hour)
	for _, slug := range []string{
		"languages/python", "languages/python", "languages/rust", "languages/kotlin", "web/react",
		"cloud/docker", "databases/postgres", "mobile/ios", "languages/go", "mobile/android",
	} {
		e.insertStory(slug, recent, slug)
	}
	e.insertStory("old java", now.Add(-30*24*time.Hour), "languages/java")

	// Android opened, Go skipped: Android's neighbours come first.
	for range 2 {
		e.markRead(aliceToken, e.insertStory("old android", now.Add(-48*time.Hour), "mobile/android"))
		e.recordViews(aliceToken, e.insertStory("old go", now.Add(-48*time.Hour), "languages/go"))
	}

	got := e.suggestions(aliceToken)
	want := []string{"mobile/ios", "languages/python", "databases/postgres", "languages/rust"}
	if !slices.Equal(got, want) {
		t.Fatalf("alice's suggestions = %v, want %v", got, want)
	}

	// Following a suggestion removes it; muting one removes it too.
	e.follow(aliceToken, "languages/go", "mobile/android", "web", "mobile/ios")
	e.mute(aliceToken, "languages/kotlin", "cloud", "languages/python")
	if got, want := e.suggestions(aliceToken), []string{"databases/postgres", "languages/rust"}; !slices.Equal(got, want) {
		t.Fatalf("after follow and mute = %v, want %v", got, want)
	}

	if got := e.suggestions(carolToken); len(got) != 0 {
		t.Fatalf("user without topics = %v, want none", got)
	}
	if code := e.do(http.MethodGet, "/v1/me/topic-suggestions", "", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("without auth: status %d, want 401", code)
	}
}

func TestTopicSuggestionsLimit(t *testing.T) {
	e := newEnvWith(t, func(o *httpapi.Options) { o.TopicSuggestionLimit = 2 })
	now := time.Now()
	e.follow(aliceToken, "languages/go")
	for i, slug := range []string{"languages/rust", "languages/python", "languages/java"} {
		for range i + 1 {
			e.insertStory(slug, now.Add(-time.Hour), slug)
		}
	}
	if got, want := e.suggestions(aliceToken), []string{"languages/java", "languages/python"}; !slices.Equal(got, want) {
		t.Fatalf("suggestions = %v, want %v (most recent stories first, at most 2)", got, want)
	}
}
