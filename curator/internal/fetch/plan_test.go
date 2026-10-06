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
		TopicsPerCall: 3, MaxCalls: 2, StoriesPerTopic: 5, HotMinEngaged: 1,
		WarmInterval: 24 * time.Hour, PriorityIntervals: hours(48, 96, 168, 336, 672), MaxAge: 14 * 24 * time.Hour,
	}

	// No priority set: every topic counts as the default, 3 (cold interval 168h).
	topics := []fetch.Topic{
		// Hot through the parent's engaged users: due on every run, even fetched an hour ago.
		{Slug: "web", HasChildren: true, Engaged7d: 2},
		{Slug: "web/react", ParentSlug: "web", LastFetchedAt: now.Add(-time.Hour)},
		{Slug: "web/vue", ParentSlug: "web", LastFetchedAt: never},
		// Warm through the parent's profession users: due only after the warm interval.
		{Slug: "lang", HasChildren: true, ProfessionUsers: 1},
		{Slug: "lang/go", ParentSlug: "lang", LastFetchedAt: now.Add(-30 * time.Hour)},
		{Slug: "lang/rust", ParentSlug: "lang", LastFetchedAt: now.Add(-2 * time.Hour)},
		// Warm through followers, fetched within the warm interval: not due.
		{Slug: "sec", Followers: 1, LastFetchedAt: now.Add(-23 * time.Hour)},
		// Cold (a launched profession's, no demand): due only after the priority's interval, oldest first.
		{Slug: "db", Launched: true, LastFetchedAt: now.Add(-100 * time.Hour)},
		{Slug: "cloud", Launched: true, LastFetchedAt: now.Add(-200 * time.Hour)},
		{Slug: "ops", Launched: true, LastFetchedAt: never},
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
		topics = append(topics, fetch.Topic{Slug: s, ParentSlug: "a", Engaged7d: 1, LastFetchedAt: now})
	}
	groups, _ := fetch.Plan(topics, fetch.PlanSettings{TopicsPerCall: 2, MaxCalls: 10, HotMinEngaged: 1, WarmInterval: time.Hour, PriorityIntervals: hours(1, 1, 1, 1, 1), MaxAge: time.Hour}, now)
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
		TopicsPerCall: 1, MaxCalls: 3, StoriesPerTopic: 5, HotMinEngaged: 1,
		WarmInterval: 100 * time.Hour, PriorityIntervals: hours(48, 96, 168, 336, 672), MaxAge: 14 * 24 * time.Hour,
	}
	topics := []fetch.Topic{
		// Cold: each priority has its own interval; within the tier, priority beats age.
		{Slug: "cold3", Priority: 3, Launched: true, LastFetchedAt: never},
		{Slug: "cold1", Priority: 1, Launched: true, LastFetchedAt: now.Add(-50 * time.Hour)},
		{Slug: "cold2", Priority: 2, Launched: true, LastFetchedAt: now.Add(-50 * time.Hour)},  // not due: 96h
		{Slug: "cold4", Priority: 4, Launched: true, LastFetchedAt: now.Add(-300 * time.Hour)}, // not due: 336h
		{Slug: "cold5", Priority: 5, Launched: true, LastFetchedAt: now.Add(-700 * time.Hour)},
		// Hot beats every cold topic, whatever its priority.
		{Slug: "hot5", Priority: 5, Engaged7d: 1, LastFetchedAt: now.Add(-time.Hour)},
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

func TestPlanLaunchAndHeadlines(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	never := time.Unix(0, 0)
	settings := fetch.PlanSettings{
		TopicsPerCall: 1, MaxCalls: 10, StoriesPerTopic: 5, HotMinEngaged: 1,
		WarmInterval: 24 * time.Hour, PriorityIntervals: hours(48, 96, 168, 336, 672), MaxAge: 14 * 24 * time.Hour,
	}
	topics := []fetch.Topic{
		// Headlines are warm without any demand: due after the warm interval, not the priority's.
		{Slug: "news/due", ParentSlug: "news", Priority: 5, Headline: true, LastFetchedAt: now.Add(-30 * time.Hour)},
		{Slug: "news/fresh", ParentSlug: "news", Priority: 5, Headline: true, LastFetchedAt: now.Add(-2 * time.Hour)},
		// Outside the launched professions: never due without demand, however stale.
		{Slug: "law/courts", ParentSlug: "law", Priority: 1, LastFetchedAt: never},
		// ...but demand still brings it in: followers make it warm, users who open its stories hot
		// (and the family goes first as hot, siblings in slug order).
		{Slug: "law/followed", ParentSlug: "law", Priority: 1, Followers: 1, LastFetchedAt: never},
		{Slug: "law/opened", ParentSlug: "law", Priority: 1, Engaged7d: 1, LastFetchedAt: now.Add(-time.Hour)},
		// Launched, no demand: cold.
		{Slug: "tech/x", ParentSlug: "tech", Priority: 1, Launched: true, LastFetchedAt: never},
	}
	groups, deferred := fetch.Plan(topics, settings, now)
	var got []string
	for _, g := range groups {
		got = append(got, slugsOf(g)...)
	}
	if want := []string{"law/followed", "law/opened", "news/due", "tech/x"}; !slices.Equal(got, want) {
		t.Errorf("planned = %v, want %v", got, want)
	}
	if len(deferred) != 0 {
		t.Errorf("deferred = %v", deferred)
	}
}

func TestPlanFamilyTakesBestPriority(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	never := time.Unix(0, 0)
	settings := fetch.PlanSettings{
		TopicsPerCall: 2, MaxCalls: 1, HotMinEngaged: 1,
		WarmInterval: 24 * time.Hour, PriorityIntervals: hours(48, 96, 168, 336, 672), MaxAge: 14 * 24 * time.Hour,
	}
	topics := []fetch.Topic{
		{Slug: "a", HasChildren: true},
		{Slug: "a/x", ParentSlug: "a", Priority: 4, Launched: true, LastFetchedAt: never},
		{Slug: "a/y", ParentSlug: "a", Priority: 1, Launched: true, LastFetchedAt: now.Add(-50 * time.Hour)},
		{Slug: "b", Priority: 2, Launched: true, LastFetchedAt: never},
	}
	// Family a ranks as priority 1 (a/y), so it goes before b.
	groups, deferred := fetch.Plan(topics, settings, now)
	if len(groups) != 1 || !slices.Equal(slugsOf(groups[0]), []string{"a/x", "a/y"}) || !slices.Equal(deferred, []string{"b"}) {
		t.Errorf("groups %+v, deferred %v", groups, deferred)
	}
}

func TestPlanHotMinInterval(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	settings := fetch.PlanSettings{
		TopicsPerCall: 1, MaxCalls: 10, StoriesPerTopic: 5, HotMinEngaged: 1, HotInterval: time.Hour,
		WarmInterval: 24 * time.Hour, PriorityIntervals: hours(48, 96, 168, 336, 672), MaxAge: 14 * 24 * time.Hour,
	}
	topics := []fetch.Topic{
		{Slug: "justfetched", Priority: 3, Engaged7d: 3, LastFetchedAt: now.Add(-3 * time.Minute)},
		{Slug: "due", Priority: 3, Engaged7d: 3, LastFetchedAt: now.Add(-2 * time.Hour)},
	}
	groups, _ := fetch.Plan(topics, settings, now)
	if len(groups) != 1 || groups[0].Topics[0].Slug != "due" {
		t.Fatalf("groups = %+v, want only the topic not fetched in the last hour", groups)
	}
}

func TestPlanEngagement(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	never := time.Unix(0, 0)
	settings := fetch.PlanSettings{
		TopicsPerCall: 1, MaxCalls: 10, StoriesPerTopic: 5, HotMinEngaged: 2,
		WarmInterval: 24 * time.Hour, PriorityIntervals: hours(48, 96, 168, 336, 672), MaxAge: 14 * 24 * time.Hour,
	}
	// Every topic is priority 3: cold interval 168h.
	topics := []fetch.Topic{
		// Hot: enough users opened or saved a story; the most engaged go first.
		{Slug: "hota", Priority: 3, Engaged7d: 2, Views7d: 9, LastFetchedAt: now.Add(-time.Hour)},
		{Slug: "hotb", Priority: 3, Engaged7d: 5, LastFetchedAt: now.Add(-time.Hour)},
		// Fewer engaged users than hot: warm, even without followers.
		{Slug: "warmfew", Priority: 3, Engaged7d: 1, Views7d: 3, LastFetchedAt: now.Add(-30 * time.Hour)},
		{Slug: "warmfresh", Priority: 3, Engaged7d: 1, LastFetchedAt: now.Add(-2 * time.Hour)},
		// Seen but opened or saved by nobody: the priority's interval, even when followed.
		{Slug: "ignoredfresh", Priority: 3, Followers: 3, Views7d: 4, LastFetchedAt: now.Add(-30 * time.Hour)},
		{Slug: "ignoredstale", Priority: 3, Followers: 3, Views7d: 4, LastFetchedAt: now.Add(-200 * time.Hour)},
		// Views alone no longer make a topic hot; outside the launched professions they keep it
		// cold, the most viewed first.
		{Slug: "seenonly", Priority: 3, Views7d: 1, LastFetchedAt: never},
		{Slug: "seenmore", Priority: 3, Views7d: 6, LastFetchedAt: never},
		{Slug: "seenfresh", Priority: 3, Views7d: 6, LastFetchedAt: now.Add(-30 * time.Hour)},
	}
	groups, deferred := fetch.Plan(topics, settings, now)
	var got []string
	for _, g := range groups {
		got = append(got, slugsOf(g)...)
	}
	if want := []string{"hotb", "hota", "warmfew", "seenmore", "ignoredstale", "seenonly"}; !slices.Equal(got, want) {
		t.Errorf("planned = %v, want %v", got, want)
	}
	if len(deferred) != 0 {
		t.Errorf("deferred = %v", deferred)
	}
}

func TestPlanLeastFirst(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	never := time.Unix(0, 0)
	settings := fetch.PlanSettings{
		TopicsPerCall: 1, MaxCalls: 2, StoriesPerTopic: 5, HotMinEngaged: 1, LeastFirst: true,
		WarmInterval: 24 * time.Hour, PriorityIntervals: hours(1, 1, 1, 1, 1), MaxAge: 14 * 24 * time.Hour,
	}
	topics := []fetch.Topic{
		{Slug: "hot", Priority: 1, Engaged7d: 4, LastFetchedAt: never},
		{Slug: "warm", Priority: 1, Followers: 2, LastFetchedAt: never},
		{Slug: "coldlow", Priority: 5, Views7d: 1, LastFetchedAt: never},
		{Slug: "coldhigh", Priority: 2, Views7d: 1, LastFetchedAt: never},
	}
	groups, deferred := fetch.Plan(topics, settings, now)
	var got []string
	for _, g := range groups {
		got = append(got, slugsOf(g)...)
	}
	if want := []string{"coldlow", "coldhigh"}; !slices.Equal(got, want) {
		t.Errorf("planned = %v, want %v", got, want)
	}
	if want := []string{"warm", "hot"}; !slices.Equal(deferred, want) {
		t.Errorf("deferred = %v, want %v", deferred, want)
	}
}
