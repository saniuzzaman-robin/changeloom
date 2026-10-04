// Command curator fetches stories with Claude into a local Postgres and syncs them to the
// hosted Postgres.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/catalog"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/fetch"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/migrate"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/prune"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/requests"
	cursync "github.com/saniuzzaman-robin/changeloom/curator/internal/sync"
	"github.com/saniuzzaman-robin/changeloom/curator/seed"
)

const connectTimeout = 10 * time.Second

// Unattended runs start at login, before Docker has brought Postgres up, so `run` retries the local connection.
const (
	localConnectWait  = 2 * time.Minute
	localConnectRetry = 5 * time.Second
)

const usage = `usage: curator <command> [flags]

commands:
  migrate [--remote [--env ENV]]   apply the backend and curator migrations to the local DB;
                                   --remote applies only the backend migrations to the hosted DB
                                   of ENV (staging, the default, or prod): REMOTE_DATABASE_URL_<ENV>
  seed                             load the profession and topic catalog (seed/catalog/) into the local DB
  fetch [--all]                    fetch new stories with Claude into the local DB; --all repeats
                                   until every due topic has been fetched
  backfill [--max-calls N] [--all] fill topics that have fewer than CURATOR_BACKFILL_TARGET recent
                                   stories with Claude (default N: CURATOR_BACKFILL_CALLS_PER_RUN);
                                   --all repeats until no topic is short
  requests pull [--env ENV]        copy pending topic requests and follower counts from the hosted
                                   DB of ENV (staging, the default, or prod)
  requests group [--dry-run]       turn pending requests into topics with Claude
  sync [--env ENV]                 push content and request decisions to the hosted DB of ENV
                                   (staging, the default, or prod), then ask its api to notify
  prune [--env ENV]                delete old and unseen stories nobody saved from the hosted DB of
                                   ENV (staging, the default, or prod)
  run [--if-due | --full]          requests pull, requests group, fetch, backfill, then sync and
                                   (daily) prune, for each env in CURATOR_RUN_ENVS (default prod);
                                   --if-due skips the run when the last successful fetch was less
                                   than CURATOR_RUN_MIN_GAP_HOURS ago; --full is the one-shot
                                   version: fetch and backfill repeat until every topic is covered,
                                   and prune always runs

Configuration comes from the environment; see curator/.env.example.
`

// errUsage marks a bad command line; main prints the usage text for it.
var errUsage = errors.New("bad usage")

func main() {
	err := run(os.Args[1:])
	switch {
	case errors.Is(err, errUsage):
		fmt.Fprintf(os.Stderr, "%v\n\n%s", err, usage)
		os.Exit(2)
	case err != nil:
		slog.Error("curator failed", "err", err)
		if len(os.Args) > 1 && os.Args[1] == "run" {
			alertFailure(err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: missing command", errUsage)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "migrate":
		fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
		remote := fs.Bool("remote", false, "apply only the backend migrations to the hosted DB of --env")
		envName := fs.String("env", string(config.EnvStaging), "hosted environment for --remote: staging or prod")
		if err := parse(fs, rest); err != nil {
			return err
		}
		env, err := config.ParseEnv(*envName)
		if err != nil {
			return fmt.Errorf("%w: migrate --env: %w", errUsage, err)
		}
		return runMigrate(ctx, cfg, *remote, env)
	case "seed":
		if err := parse(flag.NewFlagSet("seed", flag.ContinueOnError), rest); err != nil {
			return err
		}
		return runSeed(ctx, cfg)
	case "requests":
		return runRequests(ctx, cfg, rest)
	case "fetch":
		fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
		all := fs.Bool("all", false, "repeat until every due topic has been fetched")
		if err := parse(fs, rest); err != nil {
			return err
		}
		return runFetch(ctx, cfg, *all)
	case "backfill":
		fs := flag.NewFlagSet("backfill", flag.ContinueOnError)
		maxCalls := fs.Int("max-calls", cfg.BackfillCallsPerRun, "most Claude calls to make per pass")
		all := fs.Bool("all", false, "repeat passes until no topic is short")
		if err := parse(fs, rest); err != nil {
			return err
		}
		if *maxCalls < 1 {
			return fmt.Errorf("%w: backfill --max-calls must be at least 1", errUsage)
		}
		return runBackfill(ctx, cfg, *maxCalls, *all)
	case "prune":
		fs := flag.NewFlagSet("prune", flag.ContinueOnError)
		envName := fs.String("env", string(config.EnvStaging), "hosted environment to prune: staging or prod")
		if err := parse(fs, rest); err != nil {
			return err
		}
		env, err := config.ParseEnv(*envName)
		if err != nil {
			return fmt.Errorf("%w: prune --env: %w", errUsage, err)
		}
		return runPrune(ctx, cfg, env)
	case "sync":
		fs := flag.NewFlagSet("sync", flag.ContinueOnError)
		envName := fs.String("env", string(config.EnvStaging), "hosted environment to push to: staging or prod")
		if err := parse(fs, rest); err != nil {
			return err
		}
		env, err := config.ParseEnv(*envName)
		if err != nil {
			return fmt.Errorf("%w: sync --env: %w", errUsage, err)
		}
		return runSync(ctx, cfg, env)
	case "run":
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		ifDue := fs.Bool("if-due", false, "skip the run when the last successful fetch was under CURATOR_RUN_MIN_GAP_HOURS ago")
		full := fs.Bool("full", false, "repeat fetch and backfill until every topic is covered, and always prune")
		if err := parse(fs, rest); err != nil {
			return err
		}
		if *ifDue && *full {
			return fmt.Errorf("%w: run --if-due and --full cannot be combined", errUsage)
		}
		return runAll(ctx, cfg, *ifDue, *full)
	default:
		return fmt.Errorf("%w: unknown command %q", errUsage, cmd)
	}
}

// parse parses a subcommand's flags and rejects positional arguments.
func parse(fs *flag.FlagSet, args []string) error {
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %s: %w", errUsage, fs.Name(), err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: %s: unexpected arguments %q", errUsage, fs.Name(), fs.Args())
	}
	return nil
}

func runMigrate(ctx context.Context, cfg config.Config, remote bool, env config.Env) error {
	key, url := "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL
	if remote {
		key, url = "REMOTE_DATABASE_URL_"+env.Suffix(), cfg.Remotes[env].DatabaseURL
		slog.Info("migrating the hosted DB", "env", env)
	}
	pool, err := openPool(ctx, key, url)
	if err != nil {
		return err
	}
	defer pool.Close()

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()
	backend := os.DirFS(cfg.BackendMigrationsDir)
	if remote {
		return migrate.Remote(ctx, sqlDB, backend)
	}
	return migrate.Local(ctx, sqlDB, backend)
}

func runSeed(ctx context.Context, cfg config.Config) error {
	cat, err := catalog.Load(seed.Catalog)
	if err != nil {
		return err
	}
	pool, err := openPool(ctx, "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	res, err := catalog.Seed(ctx, pool, cat)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "catalog seeded", "topics", res.Topics, "professions", res.Professions, "new_relations", res.Relations, "new_hints", res.Hints)
	return nil
}

func runRequests(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) == 0 || (args[0] != "pull" && args[0] != "group") {
		return fmt.Errorf("%w: requests needs pull or group", errUsage)
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("requests "+sub, flag.ContinueOnError)
	var (
		envName *string
		dryRun  *bool
	)
	if sub == "pull" {
		envName = fs.String("env", string(config.EnvStaging), "hosted environment to pull from: staging or prod")
	} else {
		dryRun = fs.Bool("dry-run", false, "print the proposed topics and decisions without applying them")
	}
	if err := parse(fs, rest); err != nil {
		return err
	}

	local, err := openPool(ctx, "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL)
	if err != nil {
		return err
	}
	defer local.Close()

	if sub == "group" {
		return runGroup(ctx, cfg, local, *dryRun)
	}
	env, err := config.ParseEnv(*envName)
	if err != nil {
		return fmt.Errorf("%w: requests pull --env: %w", errUsage, err)
	}
	return pullRequests(ctx, cfg, local, env)
}

func pullRequests(ctx context.Context, cfg config.Config, local *pgxpool.Pool, env config.Env) error {
	remote, err := openPool(ctx, "REMOTE_DATABASE_URL_"+env.Suffix(), cfg.Remotes[env].DatabaseURL)
	if err != nil {
		return err
	}
	defer remote.Close()
	res, err := requests.Pull(ctx, local, remote, env)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "requests pulled", "env", env, "pending", res.Requests, "new", res.New,
		"topics_with_demand", res.Topics, "unknown_topics", res.Unknown)
	return nil
}

func runGroup(ctx context.Context, cfg config.Config, local *pgxpool.Pool, dryRun bool) error {
	res, err := requests.New(local, claude.New(cfg.Claude), cfg).Run(ctx, dryRun)
	if res.Pending > 0 {
		slog.InfoContext(ctx, "requests grouped", "requests", res.Pending, "new_topics", len(res.Plan.NewTopics),
			"decisions", len(res.Plan.Decisions), "applied", res.Applied, "cost_usd", res.CostUSD, "account", res.Account)
	}
	if err != nil {
		return err
	}
	if dryRun && res.Pending > 0 {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res.Plan); err != nil {
			return fmt.Errorf("print plan: %w", err)
		}
	}
	return nil
}

func runFetch(ctx context.Context, cfg config.Config, all bool) error {
	pool, err := openPool(ctx, "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	f := fetch.New(pool, claude.New(cfg.Claude), cfg)
	var errs []error
	for pass := 1; ; pass++ {
		sum, err := f.Run(ctx)
		slog.InfoContext(ctx, "fetch finished", "pass", pass, "calls", sum.Calls, "failed", sum.Failed, "added", sum.Added,
			"merged", sum.Merged, "rejected", sum.Rejected, "deferred_topics", len(sum.Deferred), "cost_usd", sum.CostUSD)
		if err != nil {
			errs = append(errs, err)
		}
		if !all || len(sum.Deferred) == 0 || noProgress(ctx, sum) {
			break
		}
	}
	return errors.Join(errs...)
}

func runBackfill(ctx context.Context, cfg config.Config, maxCalls int, all bool) error {
	pool, err := openPool(ctx, "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	f := fetch.New(pool, claude.New(cfg.Claude), cfg)
	var errs []error
	for pass := 1; ; pass++ {
		sum, err := f.Backfill(ctx, maxCalls)
		slog.InfoContext(ctx, "backfill finished", "pass", pass, "calls", sum.Calls, "failed", sum.Failed, "added", sum.Added,
			"merged", sum.Merged, "rejected", sum.Rejected, "cost_usd", sum.CostUSD)
		if err != nil {
			errs = append(errs, err)
		}
		if !all || sum.Calls == 0 || noProgress(ctx, sum) {
			break
		}
	}
	return errors.Join(errs...)
}

// noProgress reports whether a repeating pass should stop: it was cancelled, or every call failed, so
// another pass would plan the same topics again.
func noProgress(ctx context.Context, sum fetch.Summary) bool {
	if ctx.Err() != nil {
		return true
	}
	if sum.Calls > 0 && sum.Failed == sum.Calls {
		slog.WarnContext(ctx, "stopping: every call of the last pass failed")
		return true
	}
	return false
}

func runPrune(ctx context.Context, cfg config.Config, env config.Env) error {
	local, err := openPool(ctx, "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL)
	if err != nil {
		return err
	}
	defer local.Close()
	return pruneEnv(ctx, cfg, local, env)
}

// pruneEnv deletes the stories env's hosted DB no longer needs and records when it did.
func pruneEnv(ctx context.Context, cfg config.Config, local *pgxpool.Pool, env config.Env) error {
	remote, err := openPool(ctx, "REMOTE_DATABASE_URL_"+env.Suffix(), cfg.Remotes[env].DatabaseURL)
	if err != nil {
		return err
	}
	defer remote.Close()

	now := time.Now()
	deleted, err := prune.Run(ctx, remote, prune.Settings{
		MaxAge: cfg.PruneMaxAge,
	}, now)
	slog.InfoContext(ctx, "pruned", "env", env, "deleted_stories", deleted)
	if err != nil {
		return err
	}
	return prune.MarkDone(ctx, local, env, now)
}

// pruneIfDue prunes env when CURATOR_PRUNE_INTERVAL_DAYS have passed since the last prune.
func pruneIfDue(ctx context.Context, cfg config.Config, local *pgxpool.Pool, env config.Env) error {
	due, err := prune.Due(ctx, local, env, cfg.PruneInterval, time.Now())
	if err != nil || !due {
		return err
	}
	return pruneEnv(ctx, cfg, local, env)
}

func runSync(ctx context.Context, cfg config.Config, env config.Env) error {
	local, err := openPool(ctx, "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL)
	if err != nil {
		return err
	}
	defer local.Close()
	return syncEnv(ctx, cfg, local, env)
}

// syncEnv pushes to env's hosted DB, then asks its api to notify. A failed notify is logged only:
// the sync stands, and the next notify picks up what was missed.
func syncEnv(ctx context.Context, cfg config.Config, local *pgxpool.Pool, env config.Env) error {
	remote, err := openPool(ctx, "REMOTE_DATABASE_URL_"+env.Suffix(), cfg.Remotes[env].DatabaseURL)
	if err != nil {
		return err
	}
	defer remote.Close()

	res, err := cursync.Push(ctx, local, remote, env, time.Now().Add(-cfg.PruneMaxAge))
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "synced", "env", env, "topics", res.Topics, "stories", res.Stories,
		"tombstones", res.Tombstones, "skipped_tombstones", res.SkippedTombstones, "decisions", res.Decisions)

	sent, err := cursync.Notify(ctx, http.DefaultClient, cfg.Remotes[env])
	if err != nil {
		slog.WarnContext(ctx, "notify failed; the next sync retries", "env", env, "err", err)
		return nil
	}
	slog.InfoContext(ctx, "notified", "env", env, "sent", sent)
	return nil
}

// runAll is requests pull, requests group, fetch, backfill, then sync and a prune. With full, fetch
// and backfill repeat until every topic is covered and the prune is not skipped. Pull,
// sync and prune run for each hosted env with a database URL. A failing step is logged and the run continues where that is safe, so a
// failed fetch still pushes request decisions; the error returned lists every failed step.
func runAll(ctx context.Context, cfg config.Config, ifDue, full bool) error {
	local, err := openPoolWait(ctx, "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL, localConnectWait)
	if err != nil {
		return err
	}
	defer local.Close()

	if ifDue {
		var last *time.Time
		if err := local.QueryRow(ctx, `SELECT max(started_at) FROM fetch_runs WHERE status = 'succeeded'`).Scan(&last); err != nil {
			return fmt.Errorf("read the last successful fetch: %w", err)
		}
		if last != nil && time.Since(*last) < cfg.RunMinGap {
			slog.InfoContext(ctx, "skipping run: last successful fetch is recent", "last", *last, "min_gap", cfg.RunMinGap)
			return nil
		}
	}

	var envs []config.Env
	for _, env := range cfg.RunEnvs {
		if cfg.Remotes[env].DatabaseURL == "" {
			slog.InfoContext(ctx, "skipping env without REMOTE_DATABASE_URL", "env", env)
			continue
		}
		envs = append(envs, env)
	}
	if len(envs) == 0 {
		return errors.New("no hosted env to run for: set REMOTE_DATABASE_URL_<ENV> for each env in CURATOR_RUN_ENVS (see curator/.env.example)")
	}

	var errs []error
	step := func(name string, fn func() error) {
		if ctx.Err() != nil {
			return
		}
		if err := fn(); err != nil {
			slog.ErrorContext(ctx, "step failed", "step", name, "err", err)
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	for _, env := range envs {
		step("requests pull "+string(env), func() error { return pullRequests(ctx, cfg, local, env) })
	}
	step("requests group", func() error { return runGroup(ctx, cfg, local, false) })
	step("fetch", func() error { return runFetch(ctx, cfg, full) })
	step("backfill", func() error { return runBackfill(ctx, cfg, cfg.BackfillCallsPerRun, full) })
	for _, env := range envs {
		step("sync "+string(env), func() error { return syncEnv(ctx, cfg, local, env) })
	}
	for _, env := range envs {
		step("prune "+string(env), func() error {
			if full {
				return pruneEnv(ctx, cfg, local, env)
			}
			return pruneIfDue(ctx, cfg, local, env)
		})
	}
	return errors.Join(errs...)
}

// openPoolWait is openPool, retried every localConnectRetry until wait has passed. A missing URL fails at once.
func openPoolWait(ctx context.Context, key, url string, wait time.Duration) (*pgxpool.Pool, error) {
	deadline := time.Now().Add(wait)
	for {
		pool, err := openPool(ctx, key, url)
		if err == nil || url == "" || time.Now().Add(localConnectRetry).After(deadline) {
			return pool, err
		}
		slog.Warn("database not reachable yet, retrying", "key", key, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(localConnectRetry):
		}
	}
}

// openPool connects to the database in the env var key and checks it is reachable.
func openPool(ctx context.Context, key, url string) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, fmt.Errorf("%s is not set (see curator/.env.example)", key)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("configure %s pool: %w", key, err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to %s (is the database running and created? try `make db-up curator-db`): %w", key, err)
	}
	return pool, nil
}
