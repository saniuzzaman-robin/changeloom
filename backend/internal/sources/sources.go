// Package sources keeps the sources table in sync with the seed source list.
package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.yaml.in/yaml/v3"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/ingest"
)

// Entry is one source in the seed file.
type Entry struct {
	Name         string         `yaml:"name"`
	Kind         string         `yaml:"kind"`
	PollInterval string         `yaml:"poll_interval"`
	Topics       []string       `yaml:"topics"`
	Disabled     bool           `yaml:"disabled"`
	Config       map[string]any `yaml:"config"`

	pollEvery time.Duration
	config    json.RawMessage
}

// Parse decodes and validates the YAML source list.
func Parse(data []byte) ([]Entry, error) {
	var entries []Entry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse sources yaml: %w", err)
	}
	seen := map[string]bool{}
	for i := range entries {
		e := &entries[i]
		if e.Name == "" {
			return nil, fmt.Errorf("invalid sources yaml: entry %d has no name", i+1)
		}
		if seen[e.Name] {
			return nil, fmt.Errorf("invalid sources yaml: duplicate source name %q", e.Name)
		}
		seen[e.Name] = true

		d, err := time.ParseDuration(e.PollInterval)
		if err != nil || d < time.Minute {
			return nil, fmt.Errorf("invalid sources yaml: source %q needs poll_interval of at least 1m (e.g. \"1h\"), got %q", e.Name, e.PollInterval)
		}
		e.pollEvery = d

		if e.Config == nil {
			e.Config = map[string]any{}
		}
		if e.config, err = json.Marshal(e.Config); err != nil {
			return nil, fmt.Errorf("invalid sources yaml: source %q config: %w", e.Name, err)
		}
		if err := ingest.ValidateConfig(e.Kind, e.config); err != nil {
			return nil, fmt.Errorf("invalid sources yaml: source %q: %w", e.Name, err)
		}
	}
	return entries, nil
}

// Sync upserts every source in the YAML list by name in one transaction. Topics must
// already exist. Sources missing from the list are left untouched.
func Sync(ctx context.Context, pool *pgxpool.Pool, data []byte) (int, error) {
	entries, err := Parse(data)
	if err != nil {
		return 0, err
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		for _, e := range entries {
			topicIDs, err := topicIDs(ctx, q, e)
			if err != nil {
				return err
			}
			_, err = q.UpsertSource(ctx, db.UpsertSourceParams{
				Name:            e.Name,
				Kind:            e.Kind,
				Config:          e.config,
				DefaultTopicIds: topicIDs,
				PollSeconds:     int64(e.pollEvery / time.Second),
				Enabled:         !e.Disabled,
			})
			if err != nil {
				return fmt.Errorf("upsert source %q: %w", e.Name, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("sync sources: %w", err)
	}
	return len(entries), nil
}

func topicIDs(ctx context.Context, q *db.Queries, e Entry) ([]int64, error) {
	rows, err := q.GetTopicIDsBySlugs(ctx, e.Topics)
	if err != nil {
		return nil, fmt.Errorf("look up topics for source %q: %w", e.Name, err)
	}
	ids := make(map[string]int64, len(rows))
	for _, r := range rows {
		ids[r.Slug] = r.ID
	}
	out := make([]int64, 0, len(e.Topics))
	for _, slug := range e.Topics {
		id, ok := ids[slug]
		if !ok {
			return nil, fmt.Errorf("source %q references unknown topic %q", e.Name, slug)
		}
		out = append(out, id)
	}
	return out, nil
}
