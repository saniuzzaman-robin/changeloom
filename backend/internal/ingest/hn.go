package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultHNURL   = "https://hn.algolia.com/api/v1/search_by_date"
	defaultHNLimit = 50
	maxHNLimit     = 100
	hnItemURL      = "https://news.ycombinator.com/item?id="
)

type hnConfig struct {
	URL       string `json:"url"`
	Query     string `json:"query"`
	MinPoints int    `json:"min_points"`
	Limit     int    `json:"limit"`
}

func (c hnConfig) validate() error {
	if c.URL != "" {
		if err := requireHTTPURL("url", c.URL); err != nil {
			return err
		}
	}
	if c.MinPoints < 1 {
		return fmt.Errorf("min_points must be at least 1, got %d", c.MinPoints)
	}
	if c.Limit < 0 || c.Limit > maxHNLimit {
		return fmt.Errorf("limit must be between 1 and %d, got %d", maxHNLimit, c.Limit)
	}
	return nil
}

// HN reads Hacker News stories above a points threshold through the Algolia API.
type HN struct{ Client *http.Client }

type hnResponse struct {
	Hits []struct {
		ObjectID  string    `json:"objectID"`
		Title     string    `json:"title"`
		URL       string    `json:"url"`
		StoryText string    `json:"story_text"`
		Points    int       `json:"points"`
		Comments  int       `json:"num_comments"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"hits"`
}

// Fetch implements Adapter.
func (a HN) Fetch(ctx context.Context, src Source) (Result, error) {
	cfg, err := parseConfig[hnConfig](src.Config)
	if err != nil {
		return Result{}, err
	}
	limit := cfg.Limit
	if limit == 0 {
		limit = defaultHNLimit
	}
	q := url.Values{
		"tags":           {"story"},
		"numericFilters": {"points>=" + strconv.Itoa(cfg.MinPoints)},
		"hitsPerPage":    {strconv.Itoa(limit)},
	}
	if cfg.Query != "" {
		q.Set("query", cfg.Query)
	}
	resp, err := get(ctx, a.Client, cmpOr(cfg.URL, defaultHNURL)+"?"+q.Encode(), nil, nil)
	if err != nil {
		return Result{}, err
	}

	var decoded hnResponse
	if err := json.Unmarshal(resp.body, &decoded); err != nil {
		return Result{}, fmt.Errorf("decode hacker news response: %w", err)
	}
	items := make([]Item, 0, len(decoded.Hits))
	for _, h := range decoded.Hits {
		if h.Title == "" {
			continue
		}
		link := h.URL
		if link == "" {
			link = hnItemURL + h.ObjectID
		}
		created := h.CreatedAt.UTC()
		items = append(items, Item{
			ExternalID:  h.ObjectID,
			URL:         link,
			Title:       h.Title,
			Content:     htmlToText(h.StoryText),
			PublishedAt: utc(&created),
		})
	}
	return Result{Items: items}, nil
}
