// Package requests turns the topic requests of hosted users into topics: it pulls pending
// requests and follower counts from a hosted DB, and groups the requests into new topics with
// Claude.
package requests

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/db"
)

// viewWindow is how far back viewers of a topic's stories are counted.
const viewWindow = 7 * 24 * time.Hour

// PullResult counts what Pull copied.
type PullResult struct {
	// Requests are the hosted pending requests; New are those not yet in the inbox.
	Requests int
	New      int
	// Topics are the topics with demand that were stored; Unknown are hosted topics missing
	// from the local catalog, whose demand is skipped.
	Topics  int
	Unknown int
}

// Pull copies env's pending topic requests into the inbox and replaces env's per-topic demand
// (followers, profession users, recent viewers). remote is the hosted DB of env. Only request ids,
// texts and times and per-topic counts are read, never users.
func Pull(ctx context.Context, local *pgxpool.Pool, remote db.DBTX, env config.Env) (PullResult, error) {
	rq := db.New(remote)
	pending, err := rq.ListHostedPendingRequests(ctx)
	if err != nil {
		return PullResult{}, fmt.Errorf("read pending topic requests from %s: %w", env, err)
	}
	counts, err := rq.ListHostedTopicDemand(ctx, time.Now().Add(-viewWindow))
	if err != nil {
		return PullResult{}, fmt.Errorf("read topic demand from %s: %w", env, err)
	}

	res := PullResult{Requests: len(pending)}
	err = pgx.BeginFunc(ctx, local, func(tx pgx.Tx) error {
		q := db.New(tx)
		for _, r := range pending {
			added, err := q.UpsertInboxRequest(ctx, db.UpsertInboxRequestParams{
				Env: string(env), RemoteID: r.ID, Text: r.Text, CreatedAt: r.CreatedAt,
			})
			if err != nil {
				return fmt.Errorf("store request %d: %w", r.ID, err)
			}
			res.New += int(added)
		}
		if err := q.DeleteTopicStats(ctx, string(env)); err != nil {
			return fmt.Errorf("clear follower counts: %w", err)
		}
		for _, c := range counts {
			added, err := q.AddTopicStat(ctx, db.AddTopicStatParams{
				Env: string(env), Followers: c.Followers, ProfessionUsers: c.ProfessionUsers, Views7d: c.Views, Slug: c.Slug,
			})
			if err != nil {
				return fmt.Errorf("store demand of %q: %w", c.Slug, err)
			}
			if added == 0 {
				res.Unknown++
				continue
			}
			res.Topics++
		}
		return nil
	})
	if err != nil {
		return PullResult{}, fmt.Errorf("pull from %s: %w", env, err)
	}
	return res, nil
}
