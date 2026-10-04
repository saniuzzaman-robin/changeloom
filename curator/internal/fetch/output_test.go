package fetch_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/fetch"
)

var testNow = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func validStory() map[string]any {
	return map[string]any{
		"title":        "Go 1.27 released",
		"summary":      "Go 1.27 adds generic methods.",
		"body_md":      "## What changed\nGeneric methods.",
		"kind":         "release",
		"severity":     "none",
		"importance":   3,
		"published_at": "2026-10-01T10:00:00Z",
		"topics":       []string{"languages/go", "unknown/slug"},
		"sources": []map[string]string{
			{"url": "https://Go.dev/blog/go1.27?utm_source=x", "name": "Go Blog"},
			{"url": "https://go.dev/blog/go1.27/", "name": "dup"},
			{"url": "not a url", "name": "bad"},
			{"url": "https://github.com/golang/go/releases/tag/go1.27", "name": ""},
		},
		"dedupe": map[string]any{"project": " Go ", "version": "v1.27.0", "cve_ids": []string{"cve-2026-1", "CVE-2026-1"}},
	}
}

func parse(t *testing.T, stories ...map[string]any) ([]fetch.Story, []error) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"stories": stories})
	if err != nil {
		t.Fatal(err)
	}
	out, rejected, err := fetch.ParseOutput(raw, map[string]bool{"languages/go": true}, testNow, 7*24*time.Hour, "")
	if err != nil {
		t.Fatal(err)
	}
	return out, rejected
}

func TestParseOutputNormalizes(t *testing.T) {
	out, rejected := parse(t, validStory())
	if len(rejected) != 0 || len(out) != 1 {
		t.Fatalf("out %v, rejected %v", out, rejected)
	}
	s := out[0]
	if s.Severity != nil || s.Importance != 3 || len(s.Topics) != 1 || s.Topics[0] != "languages/go" {
		t.Errorf("story = %+v", s)
	}
	if len(s.Sources) != 2 || s.Sources[0].URL != "https://go.dev/blog/go1.27" || s.Sources[1].Name != "github.com" {
		t.Errorf("sources = %+v", s.Sources)
	}
	if s.Dedupe.Project != "go" || s.Dedupe.Version != "1.27.0" || len(s.Dedupe.CVEIDs) != 1 || s.Dedupe.CVEIDs[0] != "CVE-2026-1" {
		t.Errorf("dedupe = %+v", s.Dedupe)
	}
}

func TestParseOutputClampsFuture(t *testing.T) {
	st := validStory()
	st["published_at"] = "2026-10-05"
	out, _ := parse(t, st)
	if len(out) != 1 || !out[0].PublishedAt.Equal(testNow) {
		t.Fatalf("out = %+v", out)
	}
}

func TestParseOutputRejects(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value any
		want  string
	}{
		{"kind", "kind", "rumour", "invalid kind"},
		{"severity", "severity", "huge", "invalid severity"},
		{"importance", "importance", 9, "out of range"},
		{"empty title", "title", " ", "must be non-empty"},
		{"long summary", "summary", strings.Repeat("x", 281), "summary is 281"},
		{"old", "published_at", "2026-09-01T00:00:00Z", "older than"},
		{"bad time", "published_at", "yesterday", "not an RFC 3339"},
		{"no topics", "topics", []string{"unknown/slug"}, "no valid topics"},
		{"no sources", "sources", []map[string]string{{"url": "/relative", "name": "x"}}, "no valid source"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := validStory()
			st[tc.field] = tc.value
			out, rejected := parse(t, st, validStory())
			if len(out) != 1 || len(rejected) != 1 || !strings.Contains(rejected[0].Error(), tc.want) {
				t.Fatalf("out %d, rejected %v; want one rejection containing %q", len(out), rejected, tc.want)
			}
		})
	}
}

func TestParseOutputUndecodable(t *testing.T) {
	if _, _, err := fetch.ParseOutput([]byte(`{"stories":[],"extra":1}`), nil, testNow, time.Hour, ""); err == nil {
		t.Fatal("unknown field should fail")
	}
}
