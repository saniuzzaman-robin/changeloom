// Package requests turns the topic requests of hosted users into topics: it pulls pending
// requests from a hosted DB and groups them into new topics with Claude. It also pulls each hosted
// DB's demand (follower and engagement counts), which fetch plans with.
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

// Pull copies env's pending topic requests into the inbox ([PullRequests]) and replaces env's demand
// ([PullDemand]). remote is the hosted DB of env.
func Pull(ctx context.Context, local *pgxpool.Pool, remote db.DBTX, env config.Env) (PullResult, error) {
	res, err := PullRequests(ctx, local, remote, env)
	if err != nil {
		return res, err
	}
	demand, err := PullDemand(ctx, local, remote, env)
	res.Topics, res.Unknown = demand.Topics, demand.Unknown
	return res, err
}

// PullRequests copies env's pending topic requests into the inbox; requests already there keep
// their local decision. Only request ids, texts and times are read, never users. Only Requests and
// New are set in the result.
func PullRequests(ctx context.Context, local *pgxpool.Pool, remote db.DBTX, env config.Env) (PullResult, error) {
	pending, err := db.New(remote).ListHostedPendingRequests(ctx)
	if err != nil {
		return PullResult{}, fmt.Errorf("read pending topic requests from %s: %w", env, err)
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
		return nil
	})
	if err != nil {
		return PullResult{}, fmt.Errorf("pull requests from %s: %w", env, err)
	}
	return res, nil
}

// PullDemand replaces env's per-topic demand (followers, profession users, engaged users, recent
// viewers) and users per country, which decide what fetch fetches. Only aggregate counts are read,
// never users. Only Topics and Unknown are set in the result.
func PullDemand(ctx context.Context, local *pgxpool.Pool, remote db.DBTX, env config.Env) (PullResult, error) {
	rq := db.New(remote)
	counts, err := rq.ListHostedTopicDemand(ctx, time.Now().Add(-viewWindow))
	if err != nil {
		return PullResult{}, fmt.Errorf("read topic demand from %s: %w", env, err)
	}
	countries, err := rq.ListHostedCountryDemand(ctx)
	if err != nil {
		return PullResult{}, fmt.Errorf("read country demand from %s: %w", env, err)
	}

	var res PullResult
	err = pgx.BeginFunc(ctx, local, func(tx pgx.Tx) error {
		q := db.New(tx)
		if err := q.DeleteTopicStats(ctx, string(env)); err != nil {
			return fmt.Errorf("clear follower counts: %w", err)
		}
		for _, c := range counts {
			added, err := q.AddTopicStat(ctx, db.AddTopicStatParams{
				Env: string(env), Followers: c.Followers, ProfessionUsers: c.ProfessionUsers, Engaged7d: c.Engaged, Views7d: c.Views, Slug: c.Slug,
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
		if err := q.DeleteCountryStats(ctx, string(env)); err != nil {
			return fmt.Errorf("clear country counts: %w", err)
		}
		for _, c := range countries {
			if err := q.AddCountryStat(ctx, db.AddCountryStatParams{Env: string(env), Country: c.Country, Users: c.Users}); err != nil {
				return fmt.Errorf("store users of country %q: %w", c.Country, err)
			}
		}
		return nil
	})
	if err != nil {
		return PullResult{}, fmt.Errorf("pull demand from %s: %w", env, err)
	}
	return res, nil
}
