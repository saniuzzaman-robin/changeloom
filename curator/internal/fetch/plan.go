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
	ParentSlug  string
	Followers   int
	HasChildren bool
	Hints       []string
	// LastFetchedAt is when a call covering the topic last succeeded (the Unix epoch if never).
	LastFetchedAt time.Time
}

// Group is the topics fetched in one Claude call.
type Group struct {
	// Slug labels the call in fetch_runs: the topic families it covers, joined by "+".
	Slug   string
	Topics []Topic
	// Since is the oldest publish time asked for.
	Since time.Time
}

// PlanSettings bound a plan.
type PlanSettings struct {
	TopicsPerCall      int
	MaxCalls           int
	UnfollowedInterval time.Duration
	MaxAge             time.Duration
}

type family struct {
	key       string
	topics    []Topic
	followers int
	oldest    time.Time
}

// Plan picks the topics due for a fetch and packs them into at most s.MaxCalls groups of at most
// s.TopicsPerCall topics. Only leaf topics are fetched; a parent's stories come from its
// children. A topic is due when someone follows it (or its parent) or when it has not been
// fetched for s.UnfollowedInterval. Siblings stay together; followed families come first, then
// the least recently fetched. It returns the due topics that did not fit as deferred.
func Plan(topics []Topic, s PlanSettings, now time.Time) (groups []Group, deferred []string) {
	followers := make(map[string]int, len(topics))
	for _, t := range topics {
		followers[t.Slug] = t.Followers
	}

	byKey := map[string]*family{}
	var families []*family
	for _, t := range topics {
		if t.HasChildren {
			continue
		}
		reach := t.Followers + followers[t.ParentSlug]
		if reach == 0 && now.Sub(t.LastFetchedAt) < s.UnfollowedInterval {
			continue
		}
		key := cmp.Or(t.ParentSlug, t.Slug)
		f := byKey[key]
		if f == nil {
			f = &family{key: key, oldest: t.LastFetchedAt}
			byKey[key] = f
			families = append(families, f)
		}
		f.topics = append(f.topics, t)
		f.followers = max(f.followers, reach)
		if t.LastFetchedAt.Before(f.oldest) {
			f.oldest = t.LastFetchedAt
		}
	}
	slices.SortFunc(families, func(a, b *family) int {
		return cmp.Or(
			cmp.Compare(b.followers, a.followers),
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
	}
	return groups, deferred
}
