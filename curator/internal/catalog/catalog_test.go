package catalog_test

import (
	"strings"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/catalog"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/dbtest"
	"github.com/saniuzzaman-robin/changeloom/curator/seed"
)

func TestParseEmbeddedCatalog(t *testing.T) {
	nodes, err := catalog.Parse(seed.CatalogYAML)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
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
		{"child without parent prefix", `[{slug: a, name: A, children: [{slug: b/c, name: C}]}]`, `must be "a/"`},
		{"child with two segments", `[{slug: a, name: A, children: [{slug: a/b/c, name: C}]}]`, `must be "a/"`},
		{"duplicate", `[{slug: a, name: A}, {slug: a, name: B}]`, "duplicate topic slug"},
		{"too deep", `[{slug: a, name: A, children: [{slug: a/b, name: B, children: [{slug: a/b-c, name: C}]}]}]`, "at most two levels"},
		{"unknown related", `[{slug: a, name: A, related: [b]}]`, `related topic "b" is not in the catalog`},
		{"self related", `[{slug: a, name: A, related: [a]}]`, "lists itself"},
		{"relative hint", `[{slug: a, name: A, hints: [/feed.xml]}]`, "absolute http(s) URL"},
		{"non-http hint", `[{slug: a, name: A, hints: ["ftp://example.com/x"]}]`, "absolute http(s) URL"},
		{"unknown key", `[{slug: a, name: A, hint: [https://example.com]}]`, "field hint not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := catalog.Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestSeed(t *testing.T) {
	pool := dbtest.New(t)
	ctx := t.Context()

	nodes, err := catalog.Parse([]byte(`
- slug: a
  name: A
  children:
    - {slug: a/x, name: X, related: [b/y], hints: [https://example.com/x.xml]}
- slug: b
  name: B
  children:
    - {slug: b/y, name: Y, related: [a/x]}
`))
	if err != nil {
		t.Fatal(err)
	}

	res, err := catalog.Seed(ctx, pool, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if want := (catalog.Result{Topics: 4, Relations: 1, Hints: 1}); res != want {
		t.Fatalf("first seed = %+v, want %+v", res, want)
	}

	var parent string
	if err := pool.QueryRow(ctx, `SELECT p.slug FROM topics t JOIN topics p ON p.id = t.parent_id WHERE t.slug = 'a/x'`).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != "a" {
		t.Fatalf("parent of a/x = %q, want a", parent)
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
	res, err = catalog.Seed(ctx, pool, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if want := (catalog.Result{Topics: 4}); res != want {
		t.Fatalf("second seed = %+v, want %+v", res, want)
	}
	if got := updatedAt("a/x"); !got.Equal(before) {
		t.Fatalf("unchanged topic updated_at moved from %v to %v", before, got)
	}

	// A changed name bumps updated_at; topics missing from the catalog are kept.
	renamed, err := catalog.Parse([]byte(`[{slug: a, name: A, children: [{slug: a/x, name: X2}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Seed(ctx, pool, renamed); err != nil {
		t.Fatal(err)
	}
	if got := updatedAt("a/x"); !got.After(before) {
		t.Fatalf("renamed topic updated_at = %v, want after %v", got, before)
	}
	var topics, relations int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM topics), (SELECT count(*) FROM topic_relations)`).Scan(&topics, &relations); err != nil {
		t.Fatal(err)
	}
	if topics != 4 || relations != 1 {
		t.Fatalf("after partial seed: %d topics, %d relations; want 4 and 1", topics, relations)
	}
}
