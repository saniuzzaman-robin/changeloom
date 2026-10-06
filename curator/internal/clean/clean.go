// Package clean deletes topics and professions that left the catalog, with the stories that were
// only in those topics, from the local DB and the hosted DBs. Seed and sync never delete, so
// removed or renamed catalog entries otherwise stay forever. Anything a hosted user still uses is
// kept.
package clean

import (
	"context"
	"fmt"

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

// Plan is what Clean deletes from one DB, and the unused-by-the-catalog entries it keeps.
type Plan struct {
	Topics          []string
	Professions     []string
	KeptTopics      []string
	KeptProfessions []string
	// Stories only in Topics that nobody saved.
	Stories int64
}

// Result counts what Clean deleted.
type Result struct {
	Topics      int64
	Professions int64
	Stories     int64
}

// PlanFor works out what to delete from pool: its topics and professions not in cat, minus those
// in use. A topic stays when it is in use, when its parent is (following or muting a topic covers
// its descendants), or when one of its children stays (a parent can't go before its children).
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

	parent := make(map[string]string, len(tree))
	for _, t := range tree {
		if t.ParentSlug != nil {
			parent[t.Slug] = *t.ParentSlug
		}
	}
	keep := map[string]bool{}
	for _, t := range tree {
		if inCatalog[t.Slug] || inUse.Topics[t.Slug] || inUse.Topics[parent[t.Slug]] {
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

	var plan Plan
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
	if len(plan.Topics) > 0 {
		if plan.Stories, err = q.CountStoriesOnlyIn(ctx, plan.Topics); err != nil {
			return Plan{}, fmt.Errorf("count stories to delete: %w", err)
		}
	}
	return plan, nil
}

// Apply deletes plan from pool: first the stories (in batches, so a rerun after an interruption
// finishes the job), then the professions and topics in one transaction. Saved stories stay, without
// the deleted topics.
func Apply(ctx context.Context, pool *pgxpool.Pool, plan Plan) (Result, error) {
	var res Result
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

// Empty reports whether the plan deletes nothing.
func (p Plan) Empty() bool {
	return len(p.Topics) == 0 && len(p.Professions) == 0
}
