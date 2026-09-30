package ingest

import (
	"context"

	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

type rssConfig struct {
	URL string `json:"url"`
}

func (c rssConfig) validate() error { return requireHTTPURL("url", c.URL) }

// RSS reads RSS, Atom and JSON feeds (blogs, GitHub releases.atom).
type RSS struct{ Client *http.Client }

// Fetch implements Adapter.
func (a RSS) Fetch(ctx context.Context, src Source) (Result, error) {
	cfg, err := parseConfig[rssConfig](src.Config)
	if err != nil {
		return Result{}, err
	}
	resp, err := get(ctx, a.Client, cfg.URL, &src, nil)
	if err != nil {
		return Result{}, err
	}
	if resp.notModified {
		return Result{NotModified: true}, nil
	}
	feed, err := gofeed.NewParser().ParseString(string(resp.body))
	if err != nil {
		return Result{}, fmt.Errorf("parse feed %s: %w", cfg.URL, err)
	}

	items := make([]Item, 0, len(feed.Items))
	for _, it := range feed.Items {
		link := strings.TrimSpace(it.Link)
		if link == "" || strings.TrimSpace(it.Title) == "" {
			continue
		}
		content := it.Content
		if content == "" {
			content = it.Description
		}
		published := it.PublishedParsed
		if published == nil {
			published = it.UpdatedParsed
		}
		items = append(items, Item{
			ExternalID:  it.GUID,
			URL:         link,
			Title:       strings.TrimSpace(it.Title),
			Content:     htmlToText(content),
			PublishedAt: utc(published),
		})
	}
	return Result{Items: items, ETag: resp.etag, LastModified: resp.lastModified}, nil
}

func utc(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}
