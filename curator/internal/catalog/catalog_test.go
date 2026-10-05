package catalog_test

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/catalog"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/dbtest"
	"github.com/saniuzzaman-robin/changeloom/curator/seed"
)

func TestParseEmbeddedCatalog(t *testing.T) {
	c, err := catalog.Load(seed.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Topics) == 0 || len(c.Professions) == 0 {
		t.Fatal("embedded catalog is empty")
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name, yaml, want string
	}{
		{"missing name", `[{slug: a}]`, "needs both slug and name"},
		{"bad root slug", `[{slug: A_b, name: A}]`, "root topic slug"},
		{"root with slash", `[{slug: a/b, name: A}]`, "root topic slug"},
		{"child without parent prefix", `[{slug: a, name: A, priority: 3, children: [{slug: b/c, name: C}]}]`, `must be "a/"`},
		{"child with two segments", `[{slug: a, name: A, priority: 3, children: [{slug: a/b/c, name: C}]}]`, `must be "a/"`},
		{"duplicate", `[{slug: a, name: A, priority: 3}, {slug: a, name: B}]`, "duplicate topic slug"},
		{"too deep", `[{slug: a, name: A, priority: 3, children: [{slug: a/b, name: B, children: [{slug: a/b-c, name: C}]}]}]`, "at most two levels"},
		{"unknown related", `[{slug: a, name: A, priority: 3, related: [b]}]`, `related topic "b" is not in the catalog`},
		{"self related", `[{slug: a, name: A, priority: 3, related: [a]}]`, "lists itself"},
		{"relative hint", `[{slug: a, name: A, priority: 3, hints: [/feed.xml]}]`, "absolute http(s) URL"},
		{"non-http hint", `[{slug: a, name: A, priority: 3, hints: ["ftp://example.com/x"]}]`, "absolute http(s) URL"},
		{"profession unknown topic", `professions: [{slug: p, name: P, topics: [zz]}]
topics: [{slug: a, name: A, priority: 3}]`, "not a root topic"},
		{"profession child topic", `professions: [{slug: p, name: P, topics: [a, a/x]}]
topics: [{slug: a, name: A, priority: 3, children: [{slug: a/x, name: X}]}]`, "not a root topic"},
		{"profession duplicate", `professions: [{slug: p, name: P, topics: [a]}, {slug: p, name: Q, topics: [a]}]
topics: [{slug: a, name: A, priority: 3}]`, "duplicate profession"},
		{"profession bad slug", `professions: [{slug: P_1, name: P, topics: [a]}]
topics: [{slug: a, name: A, priority: 3}]`, "profession slug"},
		{"unmapped root", `professions: [{slug: p, name: P, topics: [a]}]
topics: [{slug: a, name: A, priority: 3}, {slug: b, name: B, priority: 3}]`, `root topic "b" belongs to no profession`},
		{"root without priority", `[{slug: z, name: Z}]`, `topic "z": priority 0 must be 1 (highest) to 5`},
		{"root priority too high", `[{slug: z, name: Z, priority: 6}]`, `topic "z": priority 6`},
		{"child priority out of range", `[{slug: z, name: Z, priority: 2, children: [{slug: z/x, name: X, priority: -1}]}]`, `topic "z/x": priority -1`},
		{"unknown key", `[{slug: a, name: A, priority: 3, hint: [https://example.com]}]`, "field hint not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			yaml := tc.yaml
			if !strings.Contains(yaml, "professions:") {
				yaml = "professions: [{slug: p, name: P, topics: [a]}]\ntopics: " + yaml
			}
			_, err := catalog.Parse([]byte(yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

const seedCatalog = `
professions:
  - {slug: p, name: P, topics: [a, b]}
  - {slug: q, name: Q, topics: [b]}
topics:
  - slug: a
    name: A
    priority: 2
    children:
      - {slug: a/x, name: X, related: [b/y], hints: [https://example.com/x.xml]}
  - slug: b
    name: B
    priority: 4
    children:
      - {slug: b/y, name: Y, priority: 1, related: [a/x]}
`

func TestSeed(t *testing.T) {
	pool := dbtest.New(t)
	ctx := t.Context()

	c, err := catalog.Parse([]byte(seedCatalog))
	if err != nil {
		t.Fatal(err)
	}

	res, err := catalog.Seed(ctx, pool, c)
	if err != nil {
		t.Fatal(err)
	}
	if want := (catalog.Result{Topics: 4, Professions: 2, Relations: 1, Hints: 1}); res != want {
		t.Fatalf("first seed = %+v, want %+v", res, want)
	}
	var links int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM profession_topics`).Scan(&links); err != nil || links != 3 {
		t.Fatalf("profession_topics = %d, %v; want 3", links, err)
	}

	var parent string
	if err := pool.QueryRow(ctx, `SELECT p.slug FROM topics t JOIN topics p ON p.id = t.parent_id WHERE t.slug = 'a/x'`).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != "a" {
		t.Fatalf("parent of a/x = %q, want a", parent)
	}

	priorities := func() map[string]int {
		t.Helper()
		rows, err := pool.Query(ctx, `SELECT t.slug, p.priority FROM topic_priority p JOIN topics t ON t.id = p.topic_id`)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]int{}
		for rows.Next() {
			var slug string
			var p int
			if err := rows.Scan(&slug, &p); err != nil {
				t.Fatal(err)
			}
			got[slug] = p
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return got
	}
	// a/x inherits its root's priority; b/y sets its own.
	if got, want := priorities(), map[string]int{"a": 2, "a/x": 2, "b": 4, "b/y": 1}; !maps.Equal(got, want) {
		t.Fatalf("priorities = %v, want %v", got, want)
	}

	updatedAt := func(slug string) time.Time {
		t.Helper()
		var ts time.Time
		if err := pool.QueryRow(ctx, `SELECT updated_at FROM topics WHERE slug = $1`, slug).Scan(&ts); err != nil {
			t.Fatal(err)
		}
		return ts
	}
	before := updatedAt("a/x")

	// Re-seeding is idempotent and leaves unchanged topics' updated_at alone.
	res, err = catalog.Seed(ctx, pool, c)
	if err != nil {
		t.Fatal(err)
	}
	if want := (catalog.Result{Topics: 4, Professions: 2}); res != want {
		t.Fatalf("second seed = %+v, want %+v", res, want)
	}
	if got := updatedAt("a/x"); !got.Equal(before) {
		t.Fatalf("unchanged topic updated_at moved from %v to %v", before, got)
	}

	// A changed name bumps updated_at; topics missing from the catalog are kept, and a
	// profession's topic list is replaced.
	renamed, err := catalog.Parse([]byte(`
professions: [{slug: p, name: P, topics: [a]}]
topics: [{slug: a, name: A, priority: 3, children: [{slug: a/x, name: X2}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Seed(ctx, pool, renamed); err != nil {
		t.Fatal(err)
	}
	if got := updatedAt("a/x"); !got.After(before) {
		t.Fatalf("renamed topic updated_at = %v, want after %v", got, before)
	}
	// A reseed replaces catalog priorities and leaves topics missing from the catalog alone.
	if got, want := priorities(), map[string]int{"a": 3, "a/x": 3, "b": 4, "b/y": 1}; !maps.Equal(got, want) {
		t.Fatalf("priorities after reseed = %v, want %v", got, want)
	}
	var topics, relations int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM topics), (SELECT count(*) FROM topic_relations)`).Scan(&topics, &relations); err != nil {
		t.Fatal(err)
	}
	if topics != 4 || relations != 1 {
		t.Fatalf("after partial seed: %d topics, %d relations; want 4 and 1", topics, relations)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM profession_topics pt JOIN professions p ON p.id = pt.profession_id WHERE p.slug = 'p'`).Scan(&links); err != nil || links != 1 {
		t.Fatalf("profession p has %d topics, %v; want 1", links, err)
	}
}
