package fetch

import (
	"cmp"
	"slices"
	"strings"
	"time"

	topiccatalog "github.com/saniuzzaman-robin/changeloom/curator/internal/catalog"
)

// Topic is a topic as fetch planning sees it.
type Topic struct {
	ID          int64
	Slug        string
	Name        string
	Description string
	// ParentSlug is empty for root topics.
	ParentSlug string
	// Priority is the topic's fetch priority, topiccatalog.MinPriority (highest) to MaxPriority.
	Priority int
	// Demand pulled from the hosted DBs: followers, users whose profession maps to the topic's
	// root, distinct users who opened or saved one of its stories in the last week, and distinct
	// viewers of its stories in the last week.
	Followers       int
	ProfessionUsers int
	Engaged7d       int
	Views7d         int
	HasChildren     bool
	Hints           []string
	// Professions are the names of the professions served by the topic's root.
	Professions []string
	// Launched is true when one of those professions is launched: the topic is fetched without demand.
	Launched bool
	// Headline is true when the topic or its parent is a headline topic: it is fetched like a
	// followed one.
	Headline bool
	// LastFetchedAt is when a call covering the topic last succeeded (the Unix epoch if never).
	LastFetchedAt time.Time
}

// Group is the topics fetched in one Claude call.
type Group struct {
	// Slug labels the call in fetch_runs: the topic families it covers, joined by "+".
	Slug   string
	Topics []Topic
	// Country is the ISO code the call is for (deal topics); empty for a global call.
	Country string
	// Since is the oldest publish time asked for.
	Since time.Time
	// PerTopic is the most stories asked for per topic.
	PerTopic int
	// Also are topic slugs besides Topics that stories may be tagged with: their parents and
	// related topics.
	Also []string
}

// PlanSettings bound a plan.
type PlanSettings struct {
	TopicsPerCall int
	MaxCalls      int
	// StoriesPerTopic is copied to each group.
	StoriesPerTopic int
	// HotMinEngaged is the recent users who opened or saved a story that make a topic hot;
	// WarmInterval is the least time between fetches of a warm topic, and PriorityIntervals[p-1]
	// that of a cold topic of priority p.
	HotMinEngaged int
	// HotInterval is the least time between fetches of a hot topic, so repeated passes of one run
	// do not fetch it again.
	HotInterval time.Duration
	// Cooldown is the least time between fetches of any topic, whatever its tier: a topic fetched
	// more recently (even with no new stories) is skipped.
	Cooldown          time.Duration
	WarmInterval      time.Duration
	PriorityIntervals [topiccatalog.MaxPriority]time.Duration
	MaxAge            time.Duration
}

// tier ranks how much a topic is wanted; lower is fetched first.
type tier int

const (
	// tierHot topics have enough users opening or saving their stories: they are fetched on every run.
	tierHot tier = iota
	// tierWarm topics are followed, serve a user's profession, are headlines or have a few users
	// opening or saving their stories.
	tierWarm
	// tierCold topics have no demand but belong to a launched profession, or their stories were
	// seen but opened or saved by nobody.
	tierCold
)

type family struct {
	key      string
	topics   []Topic
	tier     tier
	priority int
	engaged  int
	views    int
	oldest   time.Time
}

// Plan picks the topics due for a fetch and packs them into at most s.MaxCalls groups of at most
// s.TopicsPerCall topics. Only leaf topics are fetched; a parent's stories come from its
// children. A topic's demand is its own plus its parent's. A hot topic (s.HotMinEngaged recent
// users who opened or saved a story) is always due; a topic whose stories were seen but opened or
// saved by nobody is cold, due for its priority's interval, even when followed; a warm one
// (followers, profession users, a headline or fewer engaged users than hot) when it has not been
// fetched for s.WarmInterval or its priority's interval, whichever is shorter; a cold one
// (launched, no demand) for its priority's interval. A topic with no demand outside the launched
// professions is never due. A priority outside topiccatalog.MinPriority to MaxPriority counts as
// DefaultPriority. Siblings stay together and a family takes the best priority and demand of its
// due topics. Hot families come first, then warm, then cold; within a tier the highest priority
// first, then the most engaged users, then the most viewers, then the least recently fetched. It
// returns the due topics that did not fit as deferred.
func Plan(topics []Topic, s PlanSettings, now time.Time) (groups []Group, deferred []string) {
	bySlug := make(map[string]Topic, len(topics))
	for _, t := range topics {
		bySlug[t.Slug] = t
	}

	byKey := map[string]*family{}
	var families []*family
	for _, t := range topics {
		if t.HasChildren {
			continue
		}
		parent := bySlug[t.ParentSlug]
		p := t.Priority
		if p < topiccatalog.MinPriority || p > topiccatalog.MaxPriority {
			p = topiccatalog.DefaultPriority
		}
		engaged, views := t.Engaged7d+parent.Engaged7d, t.Views7d+parent.Views7d
		var tr tier
		var interval time.Duration
		switch {
		case engaged >= s.HotMinEngaged:
			tr, interval = tierHot, s.HotInterval
		case views > 0 && engaged == 0:
			tr, interval = tierCold, s.PriorityIntervals[p-1]
		case engaged > 0 || t.Headline || t.Followers+parent.Followers+t.ProfessionUsers+parent.ProfessionUsers > 0:
			tr, interval = tierWarm, min(s.WarmInterval, s.PriorityIntervals[p-1])
		case t.Launched:
			tr, interval = tierCold, s.PriorityIntervals[p-1]
		default:
			continue
		}
		if now.Sub(t.LastFetchedAt) < max(interval, s.Cooldown) {
			continue
		}
		key := cmp.Or(t.ParentSlug, t.Slug)
		f := byKey[key]
		if f == nil {
			f = &family{key: key, tier: tr, priority: p, oldest: t.LastFetchedAt}
			byKey[key] = f
			families = append(families, f)
		}
		f.topics = append(f.topics, t)
		f.tier = min(f.tier, tr)
		f.priority = min(f.priority, p)
		f.engaged = max(f.engaged, engaged)
		f.views = max(f.views, views)
		if t.LastFetchedAt.Before(f.oldest) {
			f.oldest = t.LastFetchedAt
		}
	}
	slices.SortFunc(families, func(a, b *family) int {
		return cmp.Or(
			cmp.Compare(a.tier, b.tier),
			cmp.Compare(a.priority, b.priority),
			cmp.Compare(b.engaged, a.engaged),
			cmp.Compare(b.views, a.views),
			a.oldest.Compare(b.oldest),
			cmp.Compare(a.key, b.key),
		)
	})

	// First fit: a family's chunk joins the first group with room, so small families share calls.
	for _, f := range families {
		slices.SortFunc(f.topics, func(a, b Topic) int { return cmp.Compare(a.Slug, b.Slug) })
		for chunk := range slices.Chunk(f.topics, s.TopicsPerCall) {
			i := slices.IndexFunc(groups, func(g Group) bool { return len(g.Topics)+len(chunk) <= s.TopicsPerCall })
			if i < 0 {
				groups = append(groups, Group{})
				i = len(groups) - 1
			}
			g := &groups[i]
			g.Topics = append(g.Topics, chunk...)
			if !slices.Contains(strings.Split(g.Slug, "+"), f.key) {
				g.Slug = strings.TrimPrefix(g.Slug+"+"+f.key, "+")
			}
		}
	}

	if len(groups) > s.MaxCalls {
		for _, g := range groups[s.MaxCalls:] {
			for _, t := range g.Topics {
				deferred = append(deferred, t.Slug)
			}
		}
		groups = groups[:s.MaxCalls]
	}

	floor := now.Add(-s.MaxAge)
	for i := range groups {
		since := groups[i].Topics[0].LastFetchedAt
		for _, t := range groups[i].Topics[1:] {
			if t.LastFetchedAt.Before(since) {
				since = t.LastFetchedAt
			}
		}
		if since.Before(floor) {
			since = floor
		}
		groups[i].Since = since
		groups[i].PerTopic = s.StoriesPerTopic
	}
	return groups, deferred
}
