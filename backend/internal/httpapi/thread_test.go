package httpapi_test

import (
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/httpapi"
)

// setDedupe stores the curator's dedupe keys on a story.
func (e *env) setDedupe(id int64, keys string) {
	e.t.Helper()
	e.exec(`UPDATE stories SET dedupe_keys = $2::jsonb WHERE id = $1`, id, keys)
}

// story fetches one story as token.
func (e *env) story(token string, id int64) httpapi.Story {
	e.t.Helper()
	var story httpapi.Story
	if code := e.do(http.MethodGet, "/v1/stories/"+strconv.FormatInt(id, 10), token, nil, &story); code != http.StatusOK {
		e.t.Fatalf("get story %d: status %d", id, code)
	}
	if story.Thread == nil {
		e.t.Fatalf("story %d: thread missing", id)
	}
	return story
}

func threadIDs(thread []httpapi.StoryThreadItem) []int64 {
	out := make([]int64, len(thread))
	for i, s := range thread {
		out[i] = s.Id
	}
	return out
}

func TestStoryThreadByProject(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	current := e.insertStory("go 1.25", now.Add(-48*time.Hour), "languages/go")
	older := e.insertStory("go 1.24", now.Add(-90*24*time.Hour), "languages/go")
	newer := e.insertStory("go 1.26", now.Add(-1*time.Hour), "languages/go")
	expired := e.insertStory("go 1.10", now.Add(-200*24*time.Hour), "languages/go")
	other := e.insertStory("rust 1.80", now.Add(-2*time.Hour), "languages/rust")
	unversioned := e.insertStory("go survey", now.Add(-3*time.Hour), "languages/go")
	e.setDedupe(current, `{"project": "go", "version": "1.25.0", "cve_ids": []}`)
	e.setDedupe(older, `{"project": "go", "version": "1.24.0", "cve_ids": []}`)
	e.setDedupe(newer, `{"project": "go", "version": "1.26.0", "cve_ids": []}`)
	e.setDedupe(expired, `{"project": "go", "version": "1.10.0", "cve_ids": []}`)
	e.setDedupe(other, `{"project": "rust", "version": "1.80.0", "cve_ids": []}`)
	e.setDedupe(unversioned, `{"project": "go", "version": "", "cve_ids": []}`)

	story := e.story(aliceToken, current)
	if story.Version == nil || *story.Version != "1.25.0" {
		t.Fatalf("version = %v, want 1.25.0", story.Version)
	}
	thread := *story.Thread
	if got, want := threadIDs(thread), []int64{newer, unversioned, older}; !slices.Equal(got, want) {
		t.Fatalf("thread = %v, want %v (newest first, without the story itself, other projects or old stories)", got, want)
	}
	if thread[0].Version == nil || *thread[0].Version != "1.26.0" || thread[1].Version != nil {
		t.Fatalf("thread versions = %v, %v; want 1.26.0 and none", thread[0].Version, thread[1].Version)
	}
	if !slices.Equal(thread[0].Topics, []string{"languages/go"}) || thread[0].KeyPoints == nil || thread[0].ActionRequired == nil {
		t.Fatalf("thread item = %+v, want topics, key points and action", thread[0])
	}
}

func TestStoryThreadByCVE(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	current := e.insertStory("openssl advisory", now.Add(-5*time.Hour), "security")
	shared := e.insertStory("nginx patch", now.Add(-2*time.Hour), "security")
	unrelated := e.insertStory("other cve", now.Add(-1*time.Hour), "security")
	noKeys := e.insertStory("no keys", now.Add(-1*time.Hour), "security")
	e.setDedupe(current, `{"project": "", "version": "", "cve_ids": ["CVE-2026-0001", "CVE-2026-0002"]}`)
	e.setDedupe(shared, `{"project": "nginx", "version": "1.27.1", "cve_ids": ["CVE-2026-0002"]}`)
	e.setDedupe(unrelated, `{"project": "", "version": "", "cve_ids": ["CVE-2026-0009"]}`)

	if got, want := threadIDs(*e.story(aliceToken, current).Thread), []int64{shared}; !slices.Equal(got, want) {
		t.Fatalf("thread = %v, want %v", got, want)
	}
	// A story without a project or CVE (seed and older stories) has an empty thread, not every
	// other story without keys.
	if got := *e.story(aliceToken, noKeys).Thread; len(got) != 0 {
		t.Fatalf("thread of a story without keys = %v, want empty", threadIDs(got))
	}
}

func TestStoryThreadLeavesOutDismissedAndMuted(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	current := e.insertStory("react 19.1", now.Add(-5*time.Hour), "web/react")
	dismissed := e.insertStory("react 19.2", now.Add(-1*time.Hour), "web/react")
	mutedOnly := e.insertStory("react native", now.Add(-2*time.Hour), "mobile/react-native")
	mixed := e.insertStory("react and android", now.Add(-3*time.Hour), "mobile/android", "web/react")
	for _, id := range []int64{current, dismissed, mutedOnly, mixed} {
		e.setDedupe(id, `{"project": "react", "version": "", "cve_ids": []}`)
	}
	if code := e.do(http.MethodPut, "/v1/stories/"+strconv.FormatInt(dismissed, 10)+"/dismiss", aliceToken, nil, nil); code != http.StatusNoContent {
		t.Fatalf("dismiss: status %d", code)
	}
	if code := e.do(http.MethodPut, "/v1/me/muted-topics", aliceToken, map[string]any{"topics": []string{"mobile"}}, nil); code != http.StatusOK {
		t.Fatalf("mute: status %d", code)
	}

	if got, want := threadIDs(*e.story(aliceToken, current).Thread), []int64{mixed}; !slices.Equal(got, want) {
		t.Fatalf("alice's thread = %v, want %v", got, want)
	}
	// Dismissals and mutes are per user.
	if got, want := threadIDs(*e.story(bobToken, current).Thread), []int64{dismissed, mutedOnly, mixed}; !slices.Equal(got, want) {
		t.Fatalf("bob's thread = %v, want %v", got, want)
	}
}

func TestStoryThreadLimit(t *testing.T) {
	e := newEnvWith(t, func(o *httpapi.Options) { o.StoryThreadLimit = 2 })
	now := time.Now()
	var stories []int64
	for i := range 4 {
		id := e.insertStory("kotlin "+strconv.Itoa(i), now.Add(-time.Duration(i)*time.Hour), "mobile")
		e.setDedupe(id, `{"project": "kotlin", "version": "2.`+strconv.Itoa(i)+`.0", "cve_ids": []}`)
		stories = append(stories, id)
	}
	if got, want := threadIDs(*e.story(aliceToken, stories[3]).Thread), stories[:2]; !slices.Equal(got, want) {
		t.Fatalf("thread = %v, want the newest two %v", got, want)
	}
}
