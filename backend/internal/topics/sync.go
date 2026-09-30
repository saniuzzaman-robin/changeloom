// Package topics keeps the topics table in sync with the seed topic tree.
package topics

import (
	"context"
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

// Parse decodes and validates a YAML topic tree.
func Parse(data []byte) ([]Node, error) {
	var nodes []Node
	if err := yaml.Unmarshal(data, &nodes); err != nil {
		return nil, fmt.Errorf("parse topics yaml: %w", err)
	}
	if err := validate(nodes, "", map[string]bool{}); err != nil {
		return nil, fmt.Errorf("invalid topics yaml: %w", err)
	}
	return nodes, nil
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

// Sync upserts every topic in the YAML tree by slug in one transaction.
// Topics missing from the tree are left untouched.
func Sync(ctx context.Context, pool *pgxpool.Pool, data []byte) (int, error) {
	nodes, err := Parse(data)
	if err != nil {
		return 0, err
	}
	count := 0
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return upsert(ctx, db.New(tx), nodes, nil, &count)
	})
	if err != nil {
		return 0, fmt.Errorf("sync topics: %w", err)
	}
	return count, nil
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
