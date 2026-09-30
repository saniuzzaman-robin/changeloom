package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

const (
	// minContentChars is the length below which an item is treated as an excerpt
	// and the full article is fetched from its URL.
	minContentChars = 400
	// maxStoredContentBytes bounds the size of raw_items.content.
	maxStoredContentBytes = 200_000
)

// Stats summarizes one poll of a source.
type Stats struct {
	Fetched     int
	TooOld      int
	Invalid     int
	Existing    int
	Inserted    int
	NotModified bool
}

// Ingester polls sources and stores new items in raw_items.
type Ingester struct {
	pool      *pgxpool.Pool
	adapters  map[string]Adapter
	extractor Extractor
	maxAge    time.Duration
	now       func() time.Time
}

// New builds an Ingester with the built-in adapters. Items older than maxAge are skipped.
func New(pool *pgxpool.Pool, client *http.Client, maxAge time.Duration) *Ingester {
	return &Ingester{
		pool: pool,
		adapters: map[string]Adapter{
			KindRSS:        RSS{Client: client},
			KindGHAdvisory: GHAdvisory{Client: client},
			KindKEV:        KEV{Client: client},
			KindHN:         HN{Client: client},
		},
		extractor: ReadabilityExtractor{Client: client},
		maxAge:    maxAge,
		now:       time.Now,
	}
}

// PollSource fetches one source and stores its new items. The poll is recorded even
// when it fails, so a broken source is retried at its normal interval, not in a loop.
func (in *Ingester) PollSource(ctx context.Context, sourceID int64) (Stats, error) {
	q := db.New(in.pool)
	row, err := q.GetSource(ctx, sourceID)
	if err != nil {
		return Stats{}, fmt.Errorf("load source %d: %w", sourceID, err)
	}
	if !row.Enabled {
		return Stats{}, nil
	}
	src := Source{ID: row.ID, Name: row.Name, Kind: row.Kind, Config: row.Config, ETag: deref(row.Etag), LastModified: deref(row.LastModified)}

	stats, err := in.poll(ctx, q, src)
	if err != nil {
		// Detached context: the poll itself may have failed because ctx was cancelled.
		if markErr := q.MarkSourcePolled(context.WithoutCancel(ctx), src.ID); markErr != nil {
			err = errors.Join(err, fmt.Errorf("record failed poll: %w", markErr))
		}
		return stats, fmt.Errorf("poll source %q: %w", src.Name, err)
	}
	return stats, nil
}

func (in *Ingester) poll(ctx context.Context, q *db.Queries, src Source) (Stats, error) {
	adapter, ok := in.adapters[src.Kind]
	if !ok {
		return Stats{}, fmt.Errorf("no adapter for source kind %q", src.Kind)
	}
	result, err := adapter.Fetch(ctx, src)
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{Fetched: len(result.Items), NotModified: result.NotModified}
	if !result.NotModified {
		if err := in.store(ctx, q, src, result.Items, &stats); err != nil {
			return stats, err
		}
	}
	err = q.RecordSourcePoll(ctx, db.RecordSourcePollParams{
		ID:           src.ID,
		Etag:         nilIfEmpty(result.ETag),
		LastModified: nilIfEmpty(result.LastModified),
	})
	if err != nil {
		return stats, fmt.Errorf("record poll: %w", err)
	}
	slog.InfoContext(ctx, "source polled", "source", src.Name, "fetched", stats.Fetched, "inserted", stats.Inserted,
		"existing", stats.Existing, "too_old", stats.TooOld, "invalid", stats.Invalid, "not_modified", stats.NotModified)
	return stats, nil
}

// StoreItems stores items found outside a poll (the discovery agent) under src, with the same
// URL normalization, deduplication, age limit and article extraction as a poll.
func (in *Ingester) StoreItems(ctx context.Context, src Source, items []Item) (Stats, error) {
	stats := Stats{Fetched: len(items)}
	err := in.store(ctx, db.New(in.pool), src, items, &stats)
	return stats, err
}

type candidate struct {
	item Item
	hash []byte
}

func (in *Ingester) store(ctx context.Context, q *db.Queries, src Source, items []Item, stats *Stats) error {
	cutoff := in.now().Add(-in.maxAge)
	seen := map[string]bool{}
	var cands []candidate
	for _, it := range items {
		if it.PublishedAt != nil && it.PublishedAt.Before(cutoff) {
			stats.TooOld++
			continue
		}
		normalized, err := NormalizeURL(it.URL)
		if err != nil {
			slog.WarnContext(ctx, "skipping item with unusable url", "source", src.Name, "url", it.URL, "err", err)
			stats.Invalid++
			continue
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		it.URL = normalized
		cands = append(cands, candidate{item: it, hash: URLHash(normalized)})
	}
	if len(cands) == 0 {
		return nil
	}

	hashes := make([][]byte, len(cands))
	for i, c := range cands {
		hashes[i] = c.hash
	}
	existing, err := q.ExistingURLHashes(ctx, hashes)
	if err != nil {
		return fmt.Errorf("look up existing items: %w", err)
	}
	known := make(map[string]bool, len(existing))
	for _, h := range existing {
		known[string(h)] = true
	}

	for _, c := range cands {
		if known[string(c.hash)] {
			stats.Existing++
			continue
		}
		content := in.fullContent(ctx, src, c.item)
		if len(content) > maxStoredContentBytes {
			slog.WarnContext(ctx, "truncating stored content", "source", src.Name, "url", c.item.URL, "bytes", len(content), "limit", maxStoredContentBytes)
			content = content[:maxStoredContentBytes]
		}
		n, err := q.InsertRawItem(ctx, db.InsertRawItemParams{
			SourceID:    src.ID,
			ExternalID:  nilIfEmpty(c.item.ExternalID),
			Url:         c.item.URL,
			UrlHash:     c.hash,
			Title:       c.item.Title,
			Content:     nilIfEmpty(content),
			PublishedAt: c.item.PublishedAt,
		})
		if err != nil {
			return fmt.Errorf("insert item %q: %w", c.item.URL, err)
		}
		stats.Inserted += int(n)
	}
	return nil
}

// fullContent returns the item's content, fetching the article page when the feed only
// carried an excerpt. Only feed and link-aggregator items point at articles; advisory and
// KEV items already carry their full text. Extraction failures keep the excerpt and are logged.
func (in *Ingester) fullContent(ctx context.Context, src Source, it Item) string {
	articleLinks := src.Kind == KindRSS || src.Kind == KindHN || src.Kind == KindDiscovery
	if !articleLinks || len(it.Content) >= minContentChars || in.extractor == nil {
		return it.Content
	}
	text, err := in.extractor.Extract(ctx, it.URL)
	if err != nil {
		slog.WarnContext(ctx, "article extraction failed, keeping excerpt", "source", src.Name, "url", it.URL, "err", err)
		return it.Content
	}
	if len(text) > len(it.Content) {
		return text
	}
	return it.Content
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
