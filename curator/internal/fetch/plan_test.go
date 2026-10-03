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
	settings := fetch.PlanSettings{
		TopicsPerCall: 3, MaxCalls: 2, StoriesPerTopic: 5, HotMinViews: 1,
		WarmInterval: 24 * time.Hour, ColdInterval: 168 * time.Hour, MaxAge: 14 * 24 * time.Hour,
	}

	topics := []fetch.Topic{
		// Hot through the parent's viewers: due on every run, even fetched an hour ago.
		{Slug: "web", HasChildren: true, Views7d: 2},
		{Slug: "web/react", ParentSlug: "web", LastFetchedAt: now.Add(-time.Hour)},
		{Slug: "web/vue", ParentSlug: "web", LastFetchedAt: never},
		// Warm through the parent's profession users: due only after the warm interval.
		{Slug: "lang", HasChildren: true, ProfessionUsers: 1},
		{Slug: "lang/go", ParentSlug: "lang", LastFetchedAt: now.Add(-30 * time.Hour)},
		{Slug: "lang/rust", ParentSlug: "lang", LastFetchedAt: now.Add(-2 * time.Hour)},
		// Warm through followers, fetched within the warm interval: not due.
		{Slug: "sec", Followers: 1, LastFetchedAt: now.Add(-23 * time.Hour)},
		// Cold: due only after the cold interval, oldest first.
		{Slug: "db", LastFetchedAt: now.Add(-100 * time.Hour)},
		{Slug: "cloud", LastFetchedAt: now.Add(-200 * time.Hour)},
		{Slug: "ops", LastFetchedAt: never},
	}
	groups, deferred := fetch.Plan(topics, settings, now)

	// Families: web (hot), lang (warm), then cold by age: ops, cloud. First fit with 3 per call:
	// [web/react web/vue lang/go] [ops cloud].
	if len(groups) != 2 {
		t.Fatalf("got %d groups: %+v", len(groups), groups)
	}
	if got := slugsOf(groups[0]); !slices.Equal(got, []string{"web/react", "web/vue", "lang/go"}) {
		t.Errorf("group 0 = %v", got)
	}
	if got := slugsOf(groups[1]); !slices.Equal(got, []string{"ops", "cloud"}) {
		t.Errorf("group 1 = %v", got)
	}
	if groups[0].Slug != "web+lang" {
		t.Errorf("group 0 slug = %q", groups[0].Slug)
	}
	if groups[0].PerTopic != settings.StoriesPerTopic {
		t.Errorf("group 0 per topic = %d", groups[0].PerTopic)
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
	if len(groups) != 1 || !slices.Equal(deferred, []string{"ops", "cloud"}) {
		t.Errorf("with one call: groups %v, deferred %v", groups, deferred)
	}
}

func TestPlanSplitsLargeFamilies(t *testing.T) {
	now := time.Now()
	var topics []fetch.Topic
	for _, s := range []string{"a/1", "a/2", "a/3", "a/4", "a/5"} {
		topics = append(topics, fetch.Topic{Slug: s, ParentSlug: "a", Views7d: 1, LastFetchedAt: now})
	}
	groups, _ := fetch.Plan(topics, fetch.PlanSettings{TopicsPerCall: 2, MaxCalls: 10, HotMinViews: 1, WarmInterval: time.Hour, ColdInterval: time.Hour, MaxAge: time.Hour}, now)
	if len(groups) != 3 || !slices.Equal(slugsOf(groups[2]), []string{"a/5"}) || groups[1].Slug != "a" {
		t.Fatalf("groups = %+v", groups)
	}
	// Hot topics fetched a moment ago still start from their last fetch.
	if !groups[0].Since.Equal(now) {
		t.Errorf("since = %v, want %v", groups[0].Since, now)
	}
}
