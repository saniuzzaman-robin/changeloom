// Package clean deletes topics and professions that left the catalog, with the stories that were
// only in those topics, from the local DB and the hosted DBs. Seed and sync never delete, so
// removed or renamed catalog entries otherwise stay forever. Anything a hosted user still uses is
// kept, unless a catalog topic replaces it: then its follows, mutes, stories and requests move to
// that topic first.
package clean

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/catalog"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/db"
)

// batchSize bounds one story delete statement.
const batchSize = 500

// InUse are the slugs no DB may lose: topics a user follows or muted, or that a topic request
// resolved to, and professions a user picked.
type InUse struct {
	Topics      map[string]bool
	Professions map[string]bool
}

// NewInUse returns an empty set.
func NewInUse() InUse {
	return InUse{Topics: map[string]bool{}, Professions: map[string]bool{}}
}

// AddLocal adds the topics the local request inbox resolved requests to.
func (u InUse) AddLocal(ctx context.Context, local db.DBTX) error {
	slugs, err := db.New(local).ListRequestedTopicSlugs(ctx)
	if err != nil {
		return fmt.Errorf("read requested topics: %w", err)
	}
	for _, s := range slugs {
		u.Topics[s] = true
	}
	return nil
}

// AddHosted adds what a hosted DB's users use.
func (u InUse) AddHosted(ctx context.Context, remote db.DBTX) error {
	q := db.New(remote)
	topics, err := q.ListHostedTopicsInUse(ctx)
	if err != nil {
		return fmt.Errorf("read topics in use: %w", err)
	}
	professions, err := q.ListHostedProfessionsInUse(ctx)
	if err != nil {
		return fmt.Errorf("read professions in use: %w", err)
	}
	for _, s := range topics {
		u.Topics[s] = true
	}
	for _, s := range professions {
		u.Professions[s] = true
	}
	return nil
}

// Merge moves a replaced topic's follows, mutes, stories and requests to the catalog topic replacing it.
type Merge struct {
	From, To string
}

// Plan is what Clean merges and deletes in one DB, and the unused-by-the-catalog entries it keeps.
type Plan struct {
	Merges          []Merge
	Topics          []string
	Professions     []string
	KeptTopics      []string
	KeptProfessions []string
	// Stories only in Topics that nobody saved.
	Stories int64
}

// Result counts what Clean merged and deleted.
type Result struct {
	// Moved counts the follows, mutes, story tags and requests merges moved.
	Moved       int64
	Topics      int64
	Professions int64
	Stories     int64
}

// PlanFor works out what to merge and delete in pool: its topics and professions not in cat, minus
// those in use. A topic stays when it is in use, when its parent is (following or muting a topic
// covers its descendants), or when one of its children stays (a parent can't go before its
// children). A topic a catalog topic in pool replaces is merged into it, so its own use doesn't count.
func PlanFor(ctx context.Context, pool db.DBTX, cat catalog.Catalog, inUse InUse) (Plan, error) {
	q := db.New(pool)
	tree, err := q.ListTopicTree(ctx)
	if err != nil {
		return Plan{}, fmt.Errorf("read topics: %w", err)
	}
	professions, err := q.ListAllProfessionSlugs(ctx)
	if err != nil {
		return Plan{}, fmt.Errorf("read professions: %w", err)
	}
	inCatalog := catalogTopics(cat)

	var plan Plan
	parent := make(map[string]string, len(tree))
	inPool := make(map[string]bool, len(tree))
	for _, t := range tree {
		inPool[t.Slug] = true
		if t.ParentSlug != nil {
			parent[t.Slug] = *t.ParentSlug
		}
	}
	merged := map[string]bool{}
	for from, to := range cat.Replacements() {
		if inPool[from] && inPool[to] {
			merged[from] = true
			plan.Merges = append(plan.Merges, Merge{From: from, To: to})
		}
	}
	slices.SortFunc(plan.Merges, func(a, b Merge) int { return strings.Compare(a.From, b.From) })
	used := func(slug string) bool { return slug != "" && inUse.Topics[slug] && !merged[slug] }
	keep := map[string]bool{}
	for _, t := range tree {
		if inCatalog[t.Slug] || used(t.Slug) || used(parent[t.Slug]) {
			keep[t.Slug] = true
		}
	}
	for _, t := range tree {
		if !keep[t.Slug] {
			continue
		}
		for p, ok := parent[t.Slug]; ok; p, ok = parent[p] {
			keep[p] = true
		}
	}

	for _, t := range tree {
		switch {
		case !keep[t.Slug]:
			plan.Topics = append(plan.Topics, t.Slug)
		case !inCatalog[t.Slug]:
			plan.KeptTopics = append(plan.KeptTopics, t.Slug)
		}
	}
	catProfessions := map[string]bool{}
	for _, p := range cat.Professions {
		catProfessions[p.Slug] = true
	}
	for _, p := range professions {
		switch {
		case catProfessions[p]:
		case inUse.Professions[p]:
			plan.KeptProfessions = append(plan.KeptProfessions, p)
		default:
			plan.Professions = append(plan.Professions, p)
		}
	}
	// A merged topic's stories move to its replacement, so only the other deleted topics lose stories.
	if gone := slices.DeleteFunc(slices.Clone(plan.Topics), func(s string) bool { return merged[s] }); len(gone) > 0 {
		if plan.Stories, err = q.CountStoriesOnlyIn(ctx, gone); err != nil {
			return Plan{}, fmt.Errorf("count stories to delete: %w", err)
		}
	}
	return plan, nil
}

// Apply carries out plan in pool: first the merges in one transaction, then the stories (in batches,
// so a rerun after an interruption finishes the job), then the professions and topics in one
// transaction. Saved stories stay, without the deleted topics.
func Apply(ctx context.Context, pool *pgxpool.Pool, plan Plan) (Result, error) {
	var res Result
	if len(plan.Merges) > 0 {
		moved, err := merge(ctx, pool, plan.Merges)
		if err != nil {
			return res, err
		}
		res.Moved = moved
	}
	q := db.New(pool)
	if len(plan.Topics) > 0 {
		for {
			n, err := q.DeleteStoriesOnlyIn(ctx, db.DeleteStoriesOnlyInParams{Slugs: plan.Topics, MaxRows: batchSize})
			if err != nil {
				return res, fmt.Errorf("delete stories: %w", err)
			}
			res.Stories += n
			if n < batchSize {
				break
			}
		}
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		qt := db.New(tx)
		var err error
		if len(plan.Professions) > 0 {
			if res.Professions, err = qt.DeleteProfessionsBySlug(ctx, plan.Professions); err != nil {
				return fmt.Errorf("delete professions: %w", err)
			}
		}
		if len(plan.Topics) > 0 {
			children, err := qt.DeleteChildTopicsBySlug(ctx, plan.Topics)
			if err != nil {
				return fmt.Errorf("delete child topics: %w", err)
			}
			roots, err := qt.DeleteRootTopicsBySlug(ctx, plan.Topics)
			if err != nil {
				return fmt.Errorf("delete root topics: %w", err)
			}
			res.Topics = children + roots
		}
		return nil
	})
	return res, err
}

// merge moves each merge's follows, mutes, story tags and requests in one transaction and counts them.
func merge(ctx context.Context, pool *pgxpool.Pool, merges []Merge) (int64, error) {
	var moved int64
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		for _, m := range merges {
			steps := []struct {
				what string
				run  func() (int64, error)
			}{
				{"follows", func() (int64, error) {
					return q.MoveTopicFollows(ctx, db.MoveTopicFollowsParams{FromSlug: m.From, ToSlug: m.To})
				}},
				{"mutes", func() (int64, error) {
					return q.MoveTopicMutes(ctx, db.MoveTopicMutesParams{FromSlug: m.From, ToSlug: m.To})
				}},
				{"story topics", func() (int64, error) {
					return q.MoveStoryTopics(ctx, db.MoveStoryTopicsParams{FromSlug: m.From, ToSlug: m.To})
				}},
				{"requests", func() (int64, error) {
					return q.MoveTopicRequests(ctx, db.MoveTopicRequestsParams{FromSlug: m.From, ToSlug: m.To})
				}},
			}
			for _, s := range steps {
				n, err := s.run()
				if err != nil {
					return fmt.Errorf("merge %s into %s: move %s: %w", m.From, m.To, s.what, err)
				}
				moved += n
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return moved, nil
}

func catalogTopics(cat catalog.Catalog) map[string]bool {
	slugs := map[string]bool{}
	var walk func([]catalog.Node)
	walk = func(nodes []catalog.Node) {
		for _, n := range nodes {
			slugs[n.Slug] = true
			walk(n.Children)
		}
	}
	walk(cat.Topics)
	return slugs
}

// Empty reports whether the plan merges and deletes nothing.
func (p Plan) Empty() bool {
	return len(p.Merges) == 0 && len(p.Topics) == 0 && len(p.Professions) == 0
}
