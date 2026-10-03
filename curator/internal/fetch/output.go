package fetch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/urlnorm"
)

const (
	maxTitleRunes   = 100
	maxSummaryRunes = 280
)

// Allowed values, mirrored in the stories table's CHECK constraints ("none" means no severity).
var (
	kinds      = []string{"release", "breaking", "security", "deprecation", "announcement", "article", "research", "policy"}
	severities = []string{"none", "low", "medium", "high", "critical"}
)

// Output is Claude's structured answer for one call.
type Output struct {
	Stories []StoryOutput `json:"stories"`
}

// StoryOutput is one story as Claude returns it.
type StoryOutput struct {
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	BodyMD      string   `json:"body_md"`
	Kind        string   `json:"kind"`
	Severity    string   `json:"severity"`
	Importance  int      `json:"importance"`
	PublishedAt string   `json:"published_at"`
	Topics      []string `json:"topics"`
	Sources     []Source `json:"sources"`
	Dedupe      Dedupe   `json:"dedupe"`
}

// Source is a page a story is based on.
type Source struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

// Dedupe holds the keys used to merge stories about the same event.
type Dedupe struct {
	Project string   `json:"project"`
	Version string   `json:"version"`
	CVEIDs  []string `json:"cve_ids"`
}

// Story is a validated story, ready to store.
type Story struct {
	Title       string
	Summary     string
	BodyMD      string
	Kind        string
	Severity    *string
	Importance  int16
	PublishedAt time.Time
	// Topics are known slugs, without duplicates.
	Topics []string
	// Sources have normalized URLs, without duplicates.
	Sources []Source
	Dedupe  Dedupe
}

// ParseOutput decodes Claude's answer and validates each story against the known topics. A bad
// story is reported in rejected and skipped; only an undecodable answer is an error. Stories
// published before now-maxAge are rejected; ones dated in the future are clamped to now.
func ParseOutput(raw []byte, validTopics map[string]bool, now time.Time, maxAge time.Duration) (stories []Story, rejected []error, err error) {
	var out Output
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, nil, fmt.Errorf("decode claude output: %w", err)
	}
	for i, so := range out.Stories {
		s, err := validateStory(so, validTopics, now, maxAge)
		if err != nil {
			rejected = append(rejected, fmt.Errorf("story %d %q: %w", i, so.Title, err))
			continue
		}
		stories = append(stories, s)
	}
	return stories, rejected, nil
}

func validateStory(so StoryOutput, validTopics map[string]bool, now time.Time, maxAge time.Duration) (Story, error) {
	s := Story{
		Title:   strings.TrimSpace(so.Title),
		Summary: strings.TrimSpace(so.Summary),
		BodyMD:  strings.TrimSpace(so.BodyMD),
		Kind:    so.Kind,
		Dedupe:  normalizeDedupe(so.Dedupe),
	}
	if !slices.Contains(kinds, so.Kind) {
		return Story{}, fmt.Errorf("invalid kind %q", so.Kind)
	}
	if !slices.Contains(severities, so.Severity) {
		return Story{}, fmt.Errorf("invalid severity %q", so.Severity)
	}
	if so.Severity != "none" {
		s.Severity = &so.Severity
	}
	if so.Importance < 1 || so.Importance > 5 {
		return Story{}, fmt.Errorf("importance %d out of range 1-5", so.Importance)
	}
	s.Importance = int16(so.Importance)
	if s.Title == "" || s.Summary == "" || s.BodyMD == "" {
		return Story{}, errors.New("title, summary and body_md must be non-empty")
	}
	if n := utf8.RuneCountInString(s.Title); n > maxTitleRunes {
		return Story{}, fmt.Errorf("title is %d characters, limit is %d", n, maxTitleRunes)
	}
	if n := utf8.RuneCountInString(s.Summary); n > maxSummaryRunes {
		return Story{}, fmt.Errorf("summary is %d characters, limit is %d", n, maxSummaryRunes)
	}

	published, err := parseTime(so.PublishedAt)
	if err != nil {
		return Story{}, err
	}
	if published.Before(now.Add(-maxAge)) {
		return Story{}, fmt.Errorf("published_at %s is older than %s", so.PublishedAt, maxAge)
	}
	if published.After(now) {
		published = now
	}
	s.PublishedAt = published

	for _, slug := range so.Topics {
		if validTopics[slug] && !slices.Contains(s.Topics, slug) {
			s.Topics = append(s.Topics, slug)
		}
	}
	if len(s.Topics) == 0 {
		return Story{}, fmt.Errorf("no valid topics in %v", so.Topics)
	}

	for _, src := range so.Sources {
		u, err := urlnorm.Normalize(src.URL)
		if err != nil || slices.ContainsFunc(s.Sources, func(x Source) bool { return x.URL == u }) {
			continue
		}
		name := strings.TrimSpace(src.Name)
		if name == "" {
			parsed, _ := url.Parse(u)
			name = parsed.Hostname()
		}
		s.Sources = append(s.Sources, Source{URL: u, Name: name})
	}
	if len(s.Sources) == 0 {
		return Story{}, fmt.Errorf("no valid source URL in %v", so.Sources)
	}
	return s, nil
}

// parseTime accepts RFC 3339 or a bare date (midnight UTC).
func parseTime(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.DateOnly, v); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("published_at %q is not an RFC 3339 time", v)
}

// normalizeDedupe canonicalizes the keys so equal events compare equal.
func normalizeDedupe(d Dedupe) Dedupe {
	out := Dedupe{
		Project: strings.ToLower(strings.TrimSpace(d.Project)),
		Version: strings.TrimPrefix(strings.ToLower(strings.TrimSpace(d.Version)), "v"),
		CVEIDs:  []string{},
	}
	for _, c := range d.CVEIDs {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c != "" && !slices.Contains(out.CVEIDs, c) {
			out.CVEIDs = append(out.CVEIDs, c)
		}
	}
	return out
}
