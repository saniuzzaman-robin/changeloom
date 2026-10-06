package fetch

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	topiccatalog "github.com/saniuzzaman-robin/changeloom/curator/internal/catalog"
)

// Band is an inclusive, 1-based range of topic ranks (see Rank); To 0 leaves the end open. The zero
// Band is every topic. LeastFirst makes a run take the least important topics first instead of the
// most important ones.
type Band struct {
	From, To   int
	LeastFirst bool
}

// ParseBand reads "FROM-TO" ("1-100") or "FROM-" ("401-"); empty is the zero Band.
func ParseBand(s string) (Band, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Band{}, nil
	}
	from, to, ok := strings.Cut(s, "-")
	if !ok {
		return Band{}, fmt.Errorf("rank band %q must look like 1-100 or 401-", s)
	}
	var b Band
	var err error
	if b.From, err = strconv.Atoi(strings.TrimSpace(from)); err != nil || b.From < 1 {
		return Band{}, fmt.Errorf("rank band %q: the start must be a number from 1", s)
	}
	if to = strings.TrimSpace(to); to != "" {
		if b.To, err = strconv.Atoi(to); err != nil || b.To < b.From {
			return Band{}, fmt.Errorf("rank band %q: the end must be a number no smaller than the start", s)
		}
	}
	return b, nil
}

// IsZero reports whether b covers every topic.
func (b Band) IsZero() bool { return b.From == 0 && b.To == 0 }

func (b Band) String() string {
	if b.IsZero() {
		return "all"
	}
	if b.To == 0 {
		return fmt.Sprintf("%d-", b.From)
	}
	return fmt.Sprintf("%d-%d", b.From, b.To)
}

// Slugs returns the slugs of the leaf topics ranked inside b, best first.
func (b Band) Slugs(topics []Topic) []string {
	ranked := Rank(topics)
	if b.IsZero() {
		return ranked
	}
	from := b.From - 1
	if from >= len(ranked) {
		return nil
	}
	to := len(ranked)
	if b.To > 0 {
		to = min(b.To, len(ranked))
	}
	return ranked[from:to]
}

// Rank orders the leaf topics, best first: editorial priority (1 first), then the users who opened
// or saved one of its stories in the last week, then its viewers, then its slug. A topic counts its
// parent's users and viewers too, as Plan does. Unlike a plan it ignores whether the topic is due,
// so a topic keeps its rank, and so its place in a band, from run to run.
func Rank(topics []Topic) []string {
	bySlug := make(map[string]Topic, len(topics))
	for _, t := range topics {
		bySlug[t.Slug] = t
	}
	type ranked struct {
		slug                    string
		priority, engaged, view int
	}
	var rows []ranked
	for _, t := range topics {
		if t.HasChildren {
			continue
		}
		parent := bySlug[t.ParentSlug]
		p := t.Priority
		if p < topiccatalog.MinPriority || p > topiccatalog.MaxPriority {
			p = topiccatalog.DefaultPriority
		}
		rows = append(rows, ranked{t.Slug, p, t.Engaged7d + parent.Engaged7d, t.Views7d + parent.Views7d})
	}
	slices.SortFunc(rows, func(a, b ranked) int {
		return cmp.Or(
			cmp.Compare(a.priority, b.priority),
			cmp.Compare(b.engaged, a.engaged),
			cmp.Compare(b.view, a.view),
			cmp.Compare(a.slug, b.slug),
		)
	})
	slugs := make([]string, len(rows))
	for i, r := range rows {
		slugs[i] = r.slug
	}
	return slugs
}
