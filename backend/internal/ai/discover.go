package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/ingest"
)

// DiscoverySourceName is the (disabled, never polled) source that discovered items belong to.
const DiscoverySourceName = "Web discovery"

const (
	// discoveryMaxItems bounds how many items one run stores.
	discoveryMaxItems = 20
	// discoveryLookbackDays is how far back the agent is asked to look.
	discoveryLookbackDays = 3
)

const discoverySystemPrompt = `You are the discovery agent for a personal developer news timeline.
Use web search to find notable, recent news for the given topics that regular RSS feeds are likely to miss: releases, breaking changes, security advisories and deprecations of libraries, languages, frameworks and platforms.
Rules:
- Only include items published within the requested time window.
- Prefer primary sources (project blogs, release notes, changelogs, security advisories) over commentary.
- Skip tutorials, opinion pieces, marketing, and anything you cannot tie to a concrete URL.
- Never invent a URL. Use only URLs that appeared in search results.
When you are done, reply with ONLY one JSON object, no prose and no code fence:
{"items":[{"title":"...","url":"https://...","published":"YYYY-MM-DD or empty","summary":"1-3 sentences on what happened"}]}
Return {"items":[]} when nothing qualifies.`

// DiscoverySettings configures the discovery agent.
type DiscoverySettings struct {
	// MaxSearches caps web_search uses per run.
	MaxSearches int64
	// MinInterval is the minimum time between runs.
	MinInterval time.Duration
	// MaxItemAge is passed on to ingestion and bounds accepted publication dates.
	MaxItemAge time.Duration
}

// Discoverer finds stories no feed covers by asking the AI provider to search the web, then stores
// them as raw items for the normal processing pipeline.
type Discoverer struct {
	pool     *pgxpool.Pool
	searcher Searcher
	ingester *ingest.Ingester
	settings DiscoverySettings
	now      func() time.Time
}

// NewDiscoverer creates a Discoverer.
func NewDiscoverer(pool *pgxpool.Pool, searcher Searcher, ingester *ingest.Ingester, settings DiscoverySettings) *Discoverer {
	return &Discoverer{pool: pool, searcher: searcher, ingester: ingester, settings: settings, now: time.Now}
}

type discovered struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Published string `json:"published"`
	Summary   string `json:"summary"`
}

// Run performs one discovery pass unless one already ran within MinInterval. It returns
// the number of new raw items stored.
func (d *Discoverer) Run(ctx context.Context) (int, error) {
	q := db.New(d.pool)
	srcID, err := q.UpsertSource(ctx, db.UpsertSourceParams{
		Name: DiscoverySourceName, Kind: ingest.KindDiscovery, Config: json.RawMessage(`{}`), DefaultTopicIds: []int64{},
		PollSeconds: int64(d.settings.MinInterval / time.Second), Enabled: false,
	})
	if err != nil {
		return 0, fmt.Errorf("ensure discovery source: %w", err)
	}
	last, err := q.GetSourceLastPolledAt(ctx, srcID)
	if err != nil {
		return 0, fmt.Errorf("read last discovery run: %w", err)
	}
	if last != nil && d.now().Sub(*last) < d.settings.MinInterval {
		return 0, nil
	}

	topics, err := q.ListFollowedTopics(ctx)
	if err != nil {
		return 0, fmt.Errorf("list followed topics: %w", err)
	}
	if len(topics) == 0 {
		slog.InfoContext(ctx, "discovery skipped: no user follows a topic yet")
		return 0, nil
	}

	res, err := d.searcher.Search(ctx, SearchRequest{
		System: discoverySystemPrompt, User: discoveryUser(topics, d.now()), MaxSearches: d.settings.MaxSearches,
	})
	if err != nil {
		return 0, err
	}
	// Record the run before parsing: a malformed answer must not be retried every cycle at full price.
	if err := q.MarkSourcePolled(context.WithoutCancel(ctx), srcID); err != nil {
		return 0, fmt.Errorf("record discovery run: %w", err)
	}
	slog.InfoContext(ctx, "discovery search finished", "input_tokens", res.Usage.Input, "output_tokens", res.Usage.Output, "stop_reason", res.StopReason)
	if res.Outcome != OutcomeSucceeded || res.StopReason == "refusal" {
		return 0, fmt.Errorf("discovery request did not complete: outcome %q, stop reason %q, %s", res.Outcome, res.StopReason, res.Error)
	}

	items, err := parseDiscovered(res.Text, d.now())
	if err != nil {
		return 0, err
	}
	stats, err := d.ingester.StoreItems(ctx, ingest.Source{ID: srcID, Name: DiscoverySourceName, Kind: ingest.KindDiscovery}, items)
	if err != nil {
		return 0, fmt.Errorf("store discovered items: %w", err)
	}
	slog.InfoContext(ctx, "discovery stored items", "found", len(items), "inserted", stats.Inserted, "existing", stats.Existing, "too_old", stats.TooOld, "invalid", stats.Invalid)
	return stats.Inserted, nil
}

func discoveryUser(topics []db.ListFollowedTopicsRow, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Today is %s. Look for items published in the last %d days (since %s).\nTopics:\n",
		now.UTC().Format("2006-01-02"), discoveryLookbackDays, now.AddDate(0, 0, -discoveryLookbackDays).UTC().Format("2006-01-02"))
	for _, t := range topics {
		fmt.Fprintf(&b, "- %s (%s): %s\n", t.Name, t.Slug, t.Description)
	}
	fmt.Fprintf(&b, "Return at most %d items.", discoveryMaxItems)
	return b.String()
}

// parseDiscovered extracts the JSON object from the model's final text (which may carry
// prose around it) and converts it to ingest items. Items without an http(s) URL or a
// title are dropped.
func parseDiscovered(text string, now time.Time) ([]ingest.Item, error) {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("discovery answer has no JSON object: %q", truncateForLog(text))
	}
	var out struct {
		Items []discovered `json:"items"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("parse discovery answer: %w (answer: %q)", err, truncateForLog(text))
	}
	var items []ingest.Item
	for _, d := range out.Items {
		url := strings.TrimSpace(d.URL)
		title := strings.TrimSpace(d.Title)
		if title == "" || (!strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://")) {
			continue
		}
		it := ingest.Item{ExternalID: url, URL: url, Title: title, Content: strings.TrimSpace(d.Summary)}
		if t, err := time.Parse("2006-01-02", strings.TrimSpace(d.Published)); err == nil && !t.After(now) {
			it.PublishedAt = &t
		}
		items = append(items, it)
		if len(items) == discoveryMaxItems {
			break
		}
	}
	return items, nil
}

func truncateForLog(s string) string {
	const limit = 200
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
