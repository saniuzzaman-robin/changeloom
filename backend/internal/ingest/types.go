// Package ingest polls configured sources and stores new items as raw_items.
package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Source kinds with an adapter. They match sources.kind in the database.
const (
	KindRSS        = "rss"
	KindGHAdvisory = "gh_advisory"
	KindKEV        = "kev"
	KindHN         = "hn"
	// KindDiscovery marks the source that holds items found by the web discovery agent.
	// It has no adapter and is never polled.
	KindDiscovery = "discovery"
)

// Source is the part of a sources row an adapter needs.
type Source struct {
	ID           int64
	Name         string
	Kind         string
	Config       json.RawMessage
	ETag         string
	LastModified string
}

// Item is one entry fetched from a source, before it is stored.
type Item struct {
	ExternalID  string
	URL         string
	Title       string
	Content     string
	PublishedAt *time.Time
}

// Result is what one poll of a source returned.
type Result struct {
	Items        []Item
	NotModified  bool
	ETag         string
	LastModified string
}

// Adapter fetches the current items of one kind of source.
type Adapter interface {
	Fetch(ctx context.Context, src Source) (Result, error)
}

// ValidateConfig checks that raw is a valid config for a source of the given kind.
func ValidateConfig(kind string, raw json.RawMessage) error {
	switch kind {
	case KindRSS:
		_, err := parseConfig[rssConfig](raw)
		return err
	case KindGHAdvisory:
		_, err := parseConfig[ghAdvisoryConfig](raw)
		return err
	case KindKEV:
		_, err := parseConfig[kevConfig](raw)
		return err
	case KindHN:
		_, err := parseConfig[hnConfig](raw)
		return err
	default:
		return fmt.Errorf("unsupported source kind %q", kind)
	}
}
