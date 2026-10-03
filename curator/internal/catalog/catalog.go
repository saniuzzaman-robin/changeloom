// Package catalog loads the curator's topic catalog: the topic tree, topic relations and
// source hints.
package catalog

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
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

// Profession is a profession users can pick. Topics are root topic slugs in display order.
type Profession struct {
	Slug        string   `yaml:"slug"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Topics      []string `yaml:"topics"`
}

// Catalog is the professions and the topic tree.
type Catalog struct {
	Professions []Profession `yaml:"professions"`
	Topics      []Node       `yaml:"topics"`
}

// slugPart is one lowercase, hyphenated slug segment, e.g. "react-native".
var slugPart = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const (
	professionsFile = "professions.yaml"
	topicsDir       = "topics"
)

// Load reads professions.yaml and every topics/*.yaml (each a list of root topics) from fsys, in
// file name order, and validates the result.
func Load(fsys fs.FS) (Catalog, error) {
	var c Catalog
	data, err := fs.ReadFile(fsys, professionsFile)
	if err != nil {
		return Catalog{}, fmt.Errorf("read catalog: %w", err)
	}
	if err := decode(data, &c.Professions); err != nil {
		return Catalog{}, fmt.Errorf("parse %s: %w", professionsFile, err)
	}
	files, err := fs.Glob(fsys, topicsDir+"/*.yaml")
	if err != nil {
		return Catalog{}, fmt.Errorf("read catalog: %w", err)
	}
	for _, f := range files {
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			return Catalog{}, fmt.Errorf("read catalog: %w", err)
		}
		var nodes []Node
		if err := decode(data, &nodes); err != nil {
			return Catalog{}, fmt.Errorf("parse %s: %w", f, err)
		}
		c.Topics = append(c.Topics, nodes...)
	}
	if err := c.Validate(); err != nil {
		return Catalog{}, fmt.Errorf("invalid catalog: %w", err)
	}
	return c, nil
}

// Parse decodes and validates one YAML document with professions and topics keys.
func Parse(data []byte) (Catalog, error) {
	var c Catalog
	if err := decode(data, &c); err != nil {
		return Catalog{}, fmt.Errorf("parse catalog yaml: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Catalog{}, fmt.Errorf("invalid catalog yaml: %w", err)
	}
	return c, nil
}

// decode rejects unknown keys to catch typos.
func decode(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	return dec.Decode(v)
}

// Validate checks the topic tree, relations, hints and professions.
func (c Catalog) Validate() error {
	seen := map[string]bool{}
	if err := validateTree(c.Topics, "", seen); err != nil {
		return err
	}
	if err := validateLinks(c.Topics, seen); err != nil {
		return err
	}
	return c.validateProfessions()
}

func (c Catalog) validateProfessions() error {
	roots := map[string]bool{}
	for _, n := range c.Topics {
		roots[n.Slug] = true
	}
	mapped := map[string]bool{}
	seen := map[string]bool{}
	for _, p := range c.Professions {
		switch {
		case p.Name == "":
			return fmt.Errorf("profession %q needs a name", p.Slug)
		case !slugPart.MatchString(p.Slug):
			return fmt.Errorf("profession slug %q must be lowercase letters, digits and single hyphens", p.Slug)
		case seen[p.Slug]:
			return fmt.Errorf("duplicate profession slug %q", p.Slug)
		}
		seen[p.Slug] = true
		inProfession := map[string]bool{}
		for _, t := range p.Topics {
			switch {
			case !roots[t]:
				return fmt.Errorf("profession %q lists %q, which is not a root topic", p.Slug, t)
			case inProfession[t]:
				return fmt.Errorf("profession %q lists %q twice", p.Slug, t)
			}
			inProfession[t] = true
			mapped[t] = true
		}
	}
	for _, n := range c.Topics {
		if !mapped[n.Slug] {
			return fmt.Errorf("root topic %q belongs to no profession", n.Slug)
		}
	}
	return nil
}

// validateTree checks names and slugs. The tree is at most two levels deep because the
// Android topic picker shows two, and a child's slug is "<parent>/<segment>".
func validateTree(nodes []Node, parent string, seen map[string]bool) error {
	for _, n := range nodes {
		if n.Slug == "" || n.Name == "" {
			return fmt.Errorf("topic under %q needs both slug and name", parent)
		}
		if err := CheckSlug(n.Slug, parent); err != nil {
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

// CheckSlug checks a topic slug: a root slug, or "<parent>/<segment>" when parent is set.
func CheckSlug(slug, parent string) error {
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
			if err := CheckHint(h); err != nil {
				return fmt.Errorf("topic %q: %w", n.Slug, err)
			}
		}
		if err := validateLinks(n.Children, slugs); err != nil {
			return err
		}
	}
	return nil
}

// CheckHint checks that a source hint is an absolute http(s) URL.
func CheckHint(h string) error {
	u, err := url.Parse(h)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("hint %q must be an absolute http(s) URL", h)
	}
	return nil
}

// Result counts what Seed wrote. Topics and Professions count every upserted row; Relations and
// Hints count only rows that were new.
type Result struct {
	Topics      int
	Professions int
	Relations   int64
	Hints       int64
}

// Seed writes the catalog into the local DB in one transaction: the topics by slug with their
// relations and hints, then the professions. Nothing is ever deleted.
func Seed(ctx context.Context, pool *pgxpool.Pool, c Catalog) (Result, error) {
	var res Result
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		ids := map[string]int64{}
		if err := upsertTopics(ctx, q, c.Topics, nil, ids); err != nil {
			return err
		}
		res.Topics = len(ids)
		if err := addLinks(ctx, q, c.Topics, ids, &res); err != nil {
			return err
		}
		for i, p := range c.Professions {
			if err := UpsertProfession(ctx, tx, p, i); err != nil {
				return err
			}
		}
		res.Professions = len(c.Professions)
		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("seed catalog: %w", err)
	}
	return res, nil
}

// UpsertProfession upserts a profession by slug and replaces its topic list. Topics that are not
// in the DB are skipped.
func UpsertProfession(ctx context.Context, tx pgx.Tx, p Profession, position int) error {
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO professions (slug, name, description, position) VALUES ($1, $2, $3, $4)
		ON CONFLICT (slug) DO UPDATE
		SET name = EXCLUDED.name, description = EXCLUDED.description, position = EXCLUDED.position,
		    updated_at = CASE
		        WHEN (professions.name, professions.description, professions.position)
		            IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.description, EXCLUDED.position)
		        THEN now() ELSE professions.updated_at END
		RETURNING id`, p.Slug, p.Name, p.Description, position).Scan(&id)
	if err != nil {
		return fmt.Errorf("upsert profession %q: %w", p.Slug, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM profession_topics WHERE profession_id = $1`, id); err != nil {
		return fmt.Errorf("clear topics of profession %q: %w", p.Slug, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO profession_topics (profession_id, topic_id, position)
		SELECT $1, t.id, x.pos - 1
		FROM unnest($2::text[]) WITH ORDINALITY AS x(slug, pos) JOIN topics t ON t.slug = x.slug`, id, p.Topics); err != nil {
		return fmt.Errorf("link topics of profession %q: %w", p.Slug, err)
	}
	return nil
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
