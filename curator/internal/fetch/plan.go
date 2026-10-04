package fetch

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// Topic is a topic as fetch planning sees it.
type Topic struct {
	ID          int64
	Slug        string
	Name        string
	Description string
	// ParentSlug is empty for root topics.
	ParentSlug string
	// Demand pulled from the hosted DBs: followers, users whose profession maps to the topic's
	// root, and distinct viewers of its stories in the last week.
	Followers       int
	ProfessionUsers int
	Views7d         int
	HasChildren     bool
	Hints           []string
	// Professions are the names of the professions served by the topic's root.
	Professions []string
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
	// HotMinViews is the recent viewers that make a topic hot; WarmInterval and ColdInterval are
	// the least time between fetches of warm and cold topics.
	HotMinViews  int
	WarmInterval time.Duration
	ColdInterval time.Duration
	MaxAge       time.Duration
}

// tier ranks how much a topic is wanted; lower is fetched first.
type tier int

const (
	// tierHot topics have viewers: they are fetched on every run.
	tierHot tier = iota
	// tierWarm topics are followed or serve a user's profession, but nobody saw their stories.
	tierWarm
	// tierCold topics have no demand.
	tierCold
)

type family struct {
	key    string
	topics []Topic
	tier   tier
	oldest time.Time
}

// Plan picks the topics due for a fetch and packs them into at most s.MaxCalls groups of at most
// s.TopicsPerCall topics. Only leaf topics are fetched; a parent's stories come from its
// children. A topic's demand is its own plus its parent's. A hot topic (s.HotMinViews recent
// viewers) is always due; a warm one (followers or profession users) when it has not been fetched
// for s.WarmInterval; a cold one for s.ColdInterval. Siblings stay together; hot families come
// first, then warm, then cold, each the least recently fetched first. It returns the due topics
// that did not fit as deferred.
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
		var tr tier
		var interval time.Duration
		switch {
		case t.Views7d+parent.Views7d >= s.HotMinViews:
			tr = tierHot
		case t.Followers+parent.Followers+t.ProfessionUsers+parent.ProfessionUsers > 0:
			tr, interval = tierWarm, s.WarmInterval
		default:
			tr, interval = tierCold, s.ColdInterval
		}
		if now.Sub(t.LastFetchedAt) < interval {
			continue
		}
		key := cmp.Or(t.ParentSlug, t.Slug)
		f := byKey[key]
		if f == nil {
			f = &family{key: key, tier: tr, oldest: t.LastFetchedAt}
			byKey[key] = f
			families = append(families, f)
		}
		f.topics = append(f.topics, t)
		f.tier = min(f.tier, tr)
		if t.LastFetchedAt.Before(f.oldest) {
			f.oldest = t.LastFetchedAt
		}
	}
	slices.SortFunc(families, func(a, b *family) int {
		return cmp.Or(
			cmp.Compare(a.tier, b.tier),
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
