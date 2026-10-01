package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

// Fixture is a saved raw item plus optional expectations, used to check output quality
// after changing the prompt or model. Fixtures are exported from real items with
// ExportFixture; expectations are added by hand.
type Fixture struct {
	Name        string     `json:"name"`
	SourceName  string     `json:"source_name"`
	URL         string     `json:"url"`
	Title       string     `json:"title"`
	Content     string     `json:"content"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	Expect      *Expect    `json:"expect,omitempty"`
}

// Expect lists what a good answer for a fixture looks like. Unset fields are not checked.
type Expect struct {
	Relevant      *bool    `json:"relevant,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	TopicsInclude []string `json:"topics_include,omitempty"`
	MinImportance int      `json:"min_importance,omitempty"`
	MaxImportance int      `json:"max_importance,omitempty"`
}

// EvalResult is the outcome of running one fixture.
type EvalResult struct {
	Fixture  string
	Output   Output
	Problems []string
	Usage    Usage
}

// LoadFixtures reads every *.json file in dir, sorted by file name.
func LoadFixtures(dir string) ([]Fixture, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("list fixtures: %w", err)
	}
	slices.Sort(paths)
	fixtures := make([]Fixture, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path) //nolint:gosec // developer-supplied fixture directory
		if err != nil {
			return nil, fmt.Errorf("read fixture: %w", err)
		}
		var f Fixture
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("parse fixture %s: %w", path, err)
		}
		if f.Name == "" {
			f.Name = strings.TrimSuffix(filepath.Base(path), ".json")
		}
		fixtures = append(fixtures, f)
	}
	return fixtures, nil
}

// ExportFixture builds a fixture from a stored raw item.
func (p *Processor) ExportFixture(ctx context.Context, rawItemID int64) (Fixture, error) {
	row, err := db.New(p.pool).GetRawItem(ctx, rawItemID)
	if err != nil {
		return Fixture{}, fmt.Errorf("load raw item %d: %w", rawItemID, err)
	}
	f := Fixture{Name: fmt.Sprintf("item-%d", row.ID), SourceName: row.SourceName, URL: row.Url, Title: row.Title, PublishedAt: row.PublishedAt}
	if row.Content != nil {
		f.Content = *row.Content
	}
	return f, nil
}

// Eval runs fixtures through the AI provider synchronously and checks the answers. Nothing is stored.
func (p *Processor) Eval(ctx context.Context, fixtures []Fixture) ([]EvalResult, error) {
	pc, err := p.promptContext(ctx, db.New(p.pool))
	if err != nil {
		return nil, err
	}
	results := make([]EvalResult, 0, len(fixtures))
	for _, f := range fixtures {
		content := f.Content
		user, _, _ := buildUser(&content, f.Title, f.URL, f.SourceName, f.PublishedAt, p.settings.InputMaxChars)
		res, err := p.client.Complete(ctx, Request{CustomID: f.Name, System: pc.system, User: user, Schema: pc.schema})
		if err != nil {
			return results, fmt.Errorf("fixture %s: %w", f.Name, err)
		}
		er := EvalResult{Fixture: f.Name, Usage: res.Usage}
		switch res.StopReason {
		case "refusal":
			er.Problems = append(er.Problems, "model refused (category: "+orNone(res.RefusalCategory)+")")
		case "max_tokens":
			er.Problems = append(er.Problems, "output hit max tokens")
		default:
			out, err := ParseOutput(res.Text, pc.validTopics)
			if err != nil {
				er.Problems = append(er.Problems, err.Error())
			} else {
				er.Output = out
				er.Problems = append(er.Problems, check(out, f.Expect)...)
			}
		}
		results = append(results, er)
	}
	return results, nil
}

func check(out Output, e *Expect) []string {
	if e == nil {
		return nil
	}
	var problems []string
	if e.Relevant != nil && out.Relevant != *e.Relevant {
		problems = append(problems, fmt.Sprintf("relevant=%t, want %t", out.Relevant, *e.Relevant))
	}
	if !out.Relevant {
		return problems
	}
	if e.Kind != "" && out.Kind != e.Kind {
		problems = append(problems, fmt.Sprintf("kind=%s, want %s", out.Kind, e.Kind))
	}
	for _, want := range e.TopicsInclude {
		if !slices.Contains(out.Topics, want) {
			problems = append(problems, fmt.Sprintf("topics %v missing %s", out.Topics, want))
		}
	}
	if e.MinImportance > 0 && out.Importance < e.MinImportance {
		problems = append(problems, fmt.Sprintf("importance=%d, want >= %d", out.Importance, e.MinImportance))
	}
	if e.MaxImportance > 0 && out.Importance > e.MaxImportance {
		problems = append(problems, fmt.Sprintf("importance=%d, want <= %d", out.Importance, e.MaxImportance))
	}
	return problems
}
