// Package topics keeps the topics table in sync with the seed topic tree.
package topics

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.yaml.in/yaml/v3"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

// Node is one topic in the seed tree.
type Node struct {
	Slug        string `yaml:"slug"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Children    []Node `yaml:"children"`
}

// Profession is one profession in the seed file; Topics are root topic slugs in display order.
type Profession struct {
	Slug        string   `yaml:"slug"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Topics      []string `yaml:"topics"`
}

// Seed is the decoded seed file: professions and the topic tree.
type Seed struct {
	Professions []Profession `yaml:"professions"`
	Topics      []Node       `yaml:"topics"`
}

// Parse decodes and validates a YAML seed file.
func Parse(data []byte) (Seed, error) {
	var seed Seed
	if err := yaml.Unmarshal(data, &seed); err != nil {
		return Seed{}, fmt.Errorf("parse topics yaml: %w", err)
	}
	if err := validate(seed.Topics, "", map[string]bool{}); err != nil {
		return Seed{}, fmt.Errorf("invalid topics yaml: %w", err)
	}
	roots := map[string]bool{}
	for _, n := range seed.Topics {
		roots[n.Slug] = true
	}
	seen := map[string]bool{}
	for _, p := range seed.Professions {
		if p.Slug == "" || p.Name == "" {
			return Seed{}, errors.New("invalid topics yaml: profession needs both slug and name")
		}
		if seen[p.Slug] {
			return Seed{}, fmt.Errorf("invalid topics yaml: duplicate profession slug %q", p.Slug)
		}
		seen[p.Slug] = true
		for _, t := range p.Topics {
			if !roots[t] {
				return Seed{}, fmt.Errorf("invalid topics yaml: profession %q lists %q, which is not a root topic", p.Slug, t)
			}
		}
	}
	return seed, nil
}

func validate(nodes []Node, parent string, seen map[string]bool) error {
	for _, n := range nodes {
		switch {
		case n.Slug == "" || n.Name == "":
			return fmt.Errorf("topic under %q needs both slug and name", parent)
		case seen[n.Slug]:
			return fmt.Errorf("duplicate topic slug %q", n.Slug)
		case parent != "" && !strings.HasPrefix(n.Slug, parent+"/"):
			return fmt.Errorf("topic %q must start with parent slug %q/", n.Slug, parent)
		case parent == "" && strings.Contains(n.Slug, "/"):
			return fmt.Errorf("root topic %q must not contain '/'", n.Slug)
		}
		seen[n.Slug] = true
		if err := validate(n.Children, n.Slug, seen); err != nil {
			return err
		}
	}
	return nil
}

// Sync upserts every topic and profession in the YAML file by slug in one transaction.
// Entries missing from the file are left untouched.
func Sync(ctx context.Context, pool *pgxpool.Pool, data []byte) (int, error) {
	seed, err := Parse(data)
	if err != nil {
		return 0, err
	}
	count := 0
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		if err := upsert(ctx, q, seed.Topics, nil, &count); err != nil {
			return err
		}
		return upsertProfessions(ctx, q, seed.Professions)
	})
	if err != nil {
		return 0, fmt.Errorf("sync topics: %w", err)
	}
	return count, nil
}

func upsertProfessions(ctx context.Context, q *db.Queries, professions []Profession) error {
	for i, p := range professions {
		id, err := q.UpsertProfession(ctx, db.UpsertProfessionParams{
			Slug:        p.Slug,
			Name:        p.Name,
			Description: p.Description,
			Position:    int16(i),
		})
		if err != nil {
			return fmt.Errorf("upsert profession %q: %w", p.Slug, err)
		}
		if err := q.DeleteProfessionTopics(ctx, id); err != nil {
			return fmt.Errorf("clear topics of profession %q: %w", p.Slug, err)
		}
		if err := q.InsertProfessionTopics(ctx, db.InsertProfessionTopicsParams{ProfessionID: id, TopicSlugs: p.Topics}); err != nil {
			return fmt.Errorf("link topics of profession %q: %w", p.Slug, err)
		}
	}
	return nil
}

func upsert(ctx context.Context, q *db.Queries, nodes []Node, parentID *int64, count *int) error {
	for _, n := range nodes {
		id, err := q.UpsertTopic(ctx, db.UpsertTopicParams{
			Slug:        n.Slug,
			Name:        n.Name,
			ParentID:    parentID,
			Description: n.Description,
		})
		if err != nil {
			return fmt.Errorf("upsert topic %q: %w", n.Slug, err)
		}
		*count++
		if err := upsert(ctx, q, n.Children, &id, count); err != nil {
			return err
		}
	}
	return nil
}
