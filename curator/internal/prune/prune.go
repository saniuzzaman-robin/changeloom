// Package prune deletes old stories from a hosted DB to keep it small. The local DB
// keeps everything, so dedupe and "already covered" still work.
package prune

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/db"
)

// batchSize bounds one delete statement.
const batchSize = 500

// Settings are the pruning rules.
type Settings struct {
	// MaxAge: unsaved stories published longer ago than this are deleted.
	MaxAge time.Duration
}

// Run deletes from remote, in batches, every story that no user saved and that is older than
// s.MaxAge. Sources, topics, views and read state go with it. It returns how many stories were deleted.
func Run(ctx context.Context, remote db.DBTX, s Settings, now time.Time) (int64, error) {
	q := db.New(remote)
	var total int64
	for {
		n, err := q.PruneHostedStories(ctx, db.PruneHostedStoriesParams{
			MaxAgeBefore: now.Add(-s.MaxAge), MaxRows: batchSize,
		})
		if err != nil {
			return total, fmt.Errorf("delete old stories: %w", err)
		}
		total += n
		if n < batchSize {
			return total, nil
		}
	}
}

func watermarkKey(env config.Env) string { return string(env) + ":pruned" }

// Due reports whether env was last pruned more than interval ago, or never.
func Due(ctx context.Context, local *pgxpool.Pool, env config.Env, interval time.Duration, now time.Time) (bool, error) {
	var last time.Time
	err := local.QueryRow(ctx, `SELECT value FROM sync_state WHERE key = $1`, watermarkKey(env)).Scan(&last)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read prune watermark: %w", err)
	}
	return now.Sub(last) >= interval, nil
}

// MarkDone records that env was pruned at now.
func MarkDone(ctx context.Context, local *pgxpool.Pool, env config.Env, now time.Time) error {
	_, err := local.Exec(ctx, `
		INSERT INTO sync_state (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, watermarkKey(env), now)
	if err != nil {
		return fmt.Errorf("record prune watermark: %w", err)
	}
	return nil
}
