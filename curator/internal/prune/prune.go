// Package prune deletes old stories from a hosted DB to keep it small. The local DB
// keeps everything, so dedupe and "already covered" still work.
package prune

import (
	"context"
	"fmt"
	"time"

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
