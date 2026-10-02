// Package catalog loads the curator's topic catalog: the topic tree, topic relations and
// source hints.
package catalog

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.yaml.in/yaml/v3"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/db"
)

// Node is one topic in the catalog tree.
type Node struct {
	Slug        string `yaml:"slug"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// Related are slugs of topics whose stories also suit followers of this topic.
	Related []string `yaml:"related"`
	// Hints are source URLs and feeds Claude should check for this topic.
	Hints    []string `yaml:"hints"`
	Children []Node   `yaml:"children"`
}

// slugPart is one lowercase, hyphenated slug segment, e.g. "react-native".
var slugPart = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Parse decodes and validates a YAML catalog. Unknown keys are rejected to catch typos.
func Parse(data []byte) ([]Node, error) {
	var nodes []Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&nodes); err != nil {
		return nil, fmt.Errorf("parse catalog yaml: %w", err)
	}
	seen := map[string]bool{}
	if err := validateTree(nodes, "", seen); err != nil {
		return nil, fmt.Errorf("invalid catalog yaml: %w", err)
	}
	if err := validateLinks(nodes, seen); err != nil {
		return nil, fmt.Errorf("invalid catalog yaml: %w", err)
	}
	return nodes, nil
}

// validateTree checks names and slugs. The tree is at most two levels deep because the
// Android topic picker shows two, and a child's slug is "<parent>/<segment>".
func validateTree(nodes []Node, parent string, seen map[string]bool) error {
	for _, n := range nodes {
		if n.Slug == "" || n.Name == "" {
			return fmt.Errorf("topic under %q needs both slug and name", parent)
		}
		if err := checkSlug(n.Slug, parent); err != nil {
			return err
		}
		if seen[n.Slug] {
			return fmt.Errorf("duplicate topic slug %q", n.Slug)
		}
		seen[n.Slug] = true
		if parent != "" && len(n.Children) > 0 {
			return fmt.Errorf("topic %q has children, but topics nest at most two levels deep", n.Slug)
		}
		if err := validateTree(n.Children, n.Slug, seen); err != nil {
			return err
		}
	}
	return nil
}

func checkSlug(slug, parent string) error {
	if parent == "" {
		if !slugPart.MatchString(slug) {
			return fmt.Errorf("root topic slug %q must be lowercase letters, digits and single hyphens", slug)
		}
		return nil
	}
	rest, ok := strings.CutPrefix(slug, parent+"/")
	if !ok || !slugPart.MatchString(rest) {
		return fmt.Errorf("topic slug %q must be %q followed by lowercase letters, digits and single hyphens", slug, parent+"/")
	}
	return nil
}

// validateLinks checks that related slugs exist and hints are absolute http(s) URLs.
func validateLinks(nodes []Node, slugs map[string]bool) error {
	for _, n := range nodes {
		for _, r := range n.Related {
			switch {
			case r == n.Slug:
				return fmt.Errorf("topic %q lists itself as related", n.Slug)
			case !slugs[r]:
				return fmt.Errorf("topic %q: related topic %q is not in the catalog", n.Slug, r)
			}
		}
		for _, h := range n.Hints {
			u, err := url.Parse(h)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return fmt.Errorf("topic %q: hint %q must be an absolute http(s) URL", n.Slug, h)
			}
		}
		if err := validateLinks(n.Children, slugs); err != nil {
			return err
		}
	}
	return nil
}

// Result counts what Seed wrote. Topics counts every upserted topic; Relations and Hints count
// only rows that were new.
type Result struct {
	Topics    int
	Relations int64
	Hints     int64
}

// Seed upserts the catalog into the local DB in one transaction: topics by slug, then the
// relations and hints. Nothing is ever deleted.
func Seed(ctx context.Context, pool *pgxpool.Pool, nodes []Node) (Result, error) {
	var res Result
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		ids := map[string]int64{}
		if err := upsertTopics(ctx, q, nodes, nil, ids); err != nil {
			return err
		}
		res.Topics = len(ids)
		return addLinks(ctx, q, nodes, ids, &res)
	})
	if err != nil {
		return Result{}, fmt.Errorf("seed catalog: %w", err)
	}
	return res, nil
}

func upsertTopics(ctx context.Context, q *db.Queries, nodes []Node, parentID *int64, ids map[string]int64) error {
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
		ids[n.Slug] = id
		if err := upsertTopics(ctx, q, n.Children, &id, ids); err != nil {
			return err
		}
	}
	return nil
}

func addLinks(ctx context.Context, q *db.Queries, nodes []Node, ids map[string]int64, res *Result) error {
	for _, n := range nodes {
		for _, r := range n.Related {
			added, err := q.AddTopicRelation(ctx, db.AddTopicRelationParams{A: ids[n.Slug], B: ids[r]})
			if err != nil {
				return fmt.Errorf("relate %q to %q: %w", n.Slug, r, err)
			}
			res.Relations += added
		}
		for _, h := range n.Hints {
			added, err := q.AddTopicHint(ctx, db.AddTopicHintParams{TopicID: ids[n.Slug], Url: h})
			if err != nil {
				return fmt.Errorf("add hint %q to %q: %w", h, n.Slug, err)
			}
			res.Hints += added
		}
		if err := addLinks(ctx, q, n.Children, ids, res); err != nil {
			return err
		}
	}
	return nil
}
