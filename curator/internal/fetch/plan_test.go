package fetch_test

import (
	"slices"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/fetch"
)

func slugsOf(g fetch.Group) []string {
	out := make([]string, len(g.Topics))
	for i, t := range g.Topics {
		out[i] = t.Slug
	}
	return out
}

func TestPlan(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	never := time.Unix(0, 0)
	settings := fetch.PlanSettings{TopicsPerCall: 3, MaxCalls: 2, UnfollowedInterval: 24 * time.Hour, MaxAge: 7 * 24 * time.Hour}

	topics := []fetch.Topic{
		// Parent with children: never fetched itself, but its followers count for the children.
		{Slug: "lang", HasChildren: true, Followers: 4},
		{Slug: "lang/go", ParentSlug: "lang", LastFetchedAt: now.Add(-time.Hour)},
		{Slug: "lang/rust", ParentSlug: "lang", LastFetchedAt: now.Add(-2 * time.Hour)},
		// Unfollowed and fetched recently: not due.
		{Slug: "web", HasChildren: true},
		{Slug: "web/react", ParentSlug: "web", LastFetchedAt: now.Add(-time.Hour)},
		// Unfollowed, stale: due.
		{Slug: "web/vue", ParentSlug: "web", LastFetchedAt: now.Add(-30 * time.Hour)},
		// Root leaves, never fetched: due, oldest first.
		{Slug: "db", LastFetchedAt: never},
		{Slug: "cloud", LastFetchedAt: never},
		{Slug: "ops", LastFetchedAt: never},
	}
	groups, deferred := fetch.Plan(topics, settings, now)

	// Families by priority: lang (followed), then cloud, db, ops (never fetched, by key), then web.
	// First fit with 3 per call: [lang/go lang/rust cloud] [db ops web/vue].
	if len(groups) != 2 {
		t.Fatalf("got %d groups: %+v", len(groups), groups)
	}
	if got := slugsOf(groups[0]); !slices.Equal(got, []string{"lang/go", "lang/rust", "cloud"}) {
		t.Errorf("group 0 = %v", got)
	}
	if got := slugsOf(groups[1]); !slices.Equal(got, []string{"db", "ops", "web/vue"}) {
		t.Errorf("group 1 = %v", got)
	}
	if groups[0].Slug != "lang+cloud" {
		t.Errorf("group 0 slug = %q", groups[0].Slug)
	}
	if len(deferred) != 0 {
		t.Errorf("deferred = %v", deferred)
	}
	// Since is the oldest fetch in the group, floored at the max age.
	if want := now.Add(-settings.MaxAge); !groups[0].Since.Equal(want) {
		t.Errorf("group 0 since = %v, want %v", groups[0].Since, want)
	}

	settings.MaxCalls = 1
	groups, deferred = fetch.Plan(topics, settings, now)
	if len(groups) != 1 || !slices.Equal(deferred, []string{"db", "ops", "web/vue"}) {
		t.Errorf("with one call: groups %v, deferred %v", groups, deferred)
	}
}

func TestPlanSplitsLargeFamilies(t *testing.T) {
	now := time.Now()
	var topics []fetch.Topic
	for _, s := range []string{"a/1", "a/2", "a/3", "a/4", "a/5"} {
		topics = append(topics, fetch.Topic{Slug: s, ParentSlug: "a", Followers: 1, LastFetchedAt: now})
	}
	groups, _ := fetch.Plan(topics, fetch.PlanSettings{TopicsPerCall: 2, MaxCalls: 10, UnfollowedInterval: time.Hour, MaxAge: time.Hour}, now)
	if len(groups) != 3 || !slices.Equal(slugsOf(groups[2]), []string{"a/5"}) || groups[1].Slug != "a" {
		t.Fatalf("groups = %+v", groups)
	}
	// Followed topics fetched a moment ago still start from their last fetch.
	if !groups[0].Since.Equal(now) {
		t.Errorf("since = %v, want %v", groups[0].Since, now)
	}
}
