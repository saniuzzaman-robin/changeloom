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

func hours(h ...int) [5]time.Duration {
	var out [5]time.Duration
	for i, n := range h {
		out[i] = time.Duration(n) * time.Hour
	}
	return out
}

func TestPlan(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	never := time.Unix(0, 0)
	settings := fetch.PlanSettings{
		TopicsPerCall: 3, MaxCalls: 2, StoriesPerTopic: 5, HotMinViews: 1,
		WarmInterval: 24 * time.Hour, PriorityIntervals: hours(48, 96, 168, 336, 672), MaxAge: 14 * 24 * time.Hour,
	}

	// No priority set: every topic counts as the default, 3 (cold interval 168h).
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
		// Cold: due only after the priority's interval, oldest first.
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
	groups, _ := fetch.Plan(topics, fetch.PlanSettings{TopicsPerCall: 2, MaxCalls: 10, HotMinViews: 1, WarmInterval: time.Hour, PriorityIntervals: hours(1, 1, 1, 1, 1), MaxAge: time.Hour}, now)
	if len(groups) != 3 || !slices.Equal(slugsOf(groups[2]), []string{"a/5"}) || groups[1].Slug != "a" {
		t.Fatalf("groups = %+v", groups)
	}
	// Hot topics fetched a moment ago still start from their last fetch.
	if !groups[0].Since.Equal(now) {
		t.Errorf("since = %v, want %v", groups[0].Since, now)
	}
}

func TestPlanPriority(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	never := time.Unix(0, 0)
	settings := fetch.PlanSettings{
		TopicsPerCall: 1, MaxCalls: 3, StoriesPerTopic: 5, HotMinViews: 1,
		WarmInterval: 100 * time.Hour, PriorityIntervals: hours(48, 96, 168, 336, 672), MaxAge: 14 * 24 * time.Hour,
	}
	topics := []fetch.Topic{
		// Cold: each priority has its own interval; within the tier, priority beats age.
		{Slug: "cold3", Priority: 3, LastFetchedAt: never},
		{Slug: "cold1", Priority: 1, LastFetchedAt: now.Add(-50 * time.Hour)},
		{Slug: "cold2", Priority: 2, LastFetchedAt: now.Add(-50 * time.Hour)},  // not due: 96h
		{Slug: "cold4", Priority: 4, LastFetchedAt: now.Add(-300 * time.Hour)}, // not due: 336h
		{Slug: "cold5", Priority: 5, LastFetchedAt: now.Add(-700 * time.Hour)},
		// Hot beats every cold topic, whatever its priority.
		{Slug: "hot5", Priority: 5, Views7d: 1, LastFetchedAt: now.Add(-time.Hour)},
		// Warm: the shorter of the warm interval and the priority's.
		{Slug: "warm1", Priority: 1, Followers: 1, LastFetchedAt: now.Add(-50 * time.Hour)}, // 48h: due
		{Slug: "warm3", Priority: 3, Followers: 1, LastFetchedAt: now.Add(-50 * time.Hour)}, // 100h: not due
	}
	groups, deferred := fetch.Plan(topics, settings, now)
	var got []string
	for _, g := range groups {
		got = append(got, slugsOf(g)...)
	}
	if !slices.Equal(got, []string{"hot5", "warm1", "cold1"}) {
		t.Errorf("planned = %v", got)
	}
	if !slices.Equal(deferred, []string{"cold3", "cold5"}) {
		t.Errorf("deferred = %v", deferred)
	}
}

func TestPlanFamilyTakesBestPriority(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	never := time.Unix(0, 0)
	settings := fetch.PlanSettings{
		TopicsPerCall: 2, MaxCalls: 1, HotMinViews: 1,
		WarmInterval: 24 * time.Hour, PriorityIntervals: hours(48, 96, 168, 336, 672), MaxAge: 14 * 24 * time.Hour,
	}
	topics := []fetch.Topic{
		{Slug: "a", HasChildren: true},
		{Slug: "a/x", ParentSlug: "a", Priority: 4, LastFetchedAt: never},
		{Slug: "a/y", ParentSlug: "a", Priority: 1, LastFetchedAt: now.Add(-50 * time.Hour)},
		{Slug: "b", Priority: 2, LastFetchedAt: never},
	}
	// Family a ranks as priority 1 (a/y), so it goes before b.
	groups, deferred := fetch.Plan(topics, settings, now)
	if len(groups) != 1 || !slices.Equal(slugsOf(groups[0]), []string{"a/x", "a/y"}) || !slices.Equal(deferred, []string{"b"}) {
		t.Errorf("groups %+v, deferred %v", groups, deferred)
	}
}
