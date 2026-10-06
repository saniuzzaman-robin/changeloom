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
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/ai"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/catalog"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/clean"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/fetch"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/migrate"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/prune"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/requests"
	cursync "github.com/saniuzzaman-robin/changeloom/curator/internal/sync"
	"github.com/saniuzzaman-robin/changeloom/curator/seed"
)

const connectTimeout = 10 * time.Second

const usage = `usage: curator <command> [flags]

commands:
  migrate [--remote [--env ENV]]   apply the backend and curator migrations to the local DB;
                                   --remote applies only the backend migrations to the hosted DB
                                   of ENV (staging, the default, or prod): REMOTE_DATABASE_URL_<ENV>
  seed                             load the profession and topic catalog (seed/catalog/) into the local DB
  fetch [--all] [--rank FROM-TO] [--least-first]
                                   fetch new stories with Claude into the local DB; --all repeats
                                   until every due topic has been fetched; --rank limits it to the
                                   topics ranked FROM-TO (e.g. 1-100, or 401- for the rest) by
                                   priority, then demand; --least-first takes the least important
                                   topics first (default: the most important)
  requests pull [--env ENV]        copy pending topic requests and follower counts from the hosted
                                   DB of ENV (staging, the default, or prod)
  requests group [--dry-run]       turn pending requests into topics with Claude
  sync [--env ENV]                 push content and request decisions to the hosted DB of ENV
                                   (staging, the default, or prod), then ask its api to notify
  prune [--env ENV]                delete old and unseen stories nobody saved from the hosted DB of
                                   ENV (staging, the default, or prod)
  clean [--envs ENVS] [--apply]    delete topics and professions no longer in seed/catalog/, and the
                                   unsaved stories only in those topics, from the local DB and the
                                   hosted DBs of ENVS (comma-separated, default staging,prod; list
                                   every env that has users). Anything a user follows, muted, picked
                                   or requested is kept. Without --apply it only reports.
  run [--full] [--rank FROM-TO] [--least-first]
                                   pull demand and fetch, for each env in CURATOR_RUN_ENVS
                                   (default prod); --full repeats fetch until every topic is covered. It neither syncs nor prunes: use sync and
                                   prune. User topic requests are not handled: use requests pull,
                                   requests group, then sync. --rank and --least-first as in fetch

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
		rank := fs.String("rank", "", rankUsage)
		leastFirst := fs.Bool("least-first", false, leastFirstUsage)
		if err := parse(fs, rest); err != nil {
			return err
		}
		band, err := parseBand(*rank, *leastFirst)
		if err != nil {
			return err
		}
		return runFetch(ctx, cfg, *all, band)
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
	case "clean":
		fs := flag.NewFlagSet("clean", flag.ContinueOnError)
		envNames := fs.String("envs", string(config.EnvStaging)+","+string(config.EnvProd), "hosted environments to clean and check for use")
		apply := fs.Bool("apply", false, "delete; without it, only report what would be deleted")
		if err := parse(fs, rest); err != nil {
			return err
		}
		var envs []config.Env
		for _, name := range strings.Split(*envNames, ",") {
			env, err := config.ParseEnv(strings.TrimSpace(name))
			if err != nil {
				return fmt.Errorf("%w: clean --envs: %w", errUsage, err)
			}
			if !slices.Contains(envs, env) {
				envs = append(envs, env)
			}
		}
		return runClean(ctx, cfg, envs, *apply)
	case "run":
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		full := fs.Bool("full", false, "repeat fetch until every topic is covered")
		rank := fs.String("rank", "", rankUsage)
		leastFirst := fs.Bool("least-first", false, leastFirstUsage)
		if err := parse(fs, rest); err != nil {
			return err
		}
		band, err := parseBand(*rank, *leastFirst)
		if err != nil {
			return err
		}
		return runAll(ctx, cfg, *full, band)
	default:
		return fmt.Errorf("%w: unknown command %q", errUsage, cmd)
	}
}

const leastFirstUsage = "take the least important topics first (lowest priority, least engaged) instead of the most important"

const rankUsage = "only the topics ranked inside FROM-TO (1-100) or FROM- (401-), by priority then demand; default every topic"

// parseBand reads the --rank and --least-first flags.
func parseBand(value string, leastFirst bool) (fetch.Band, error) {
	band, err := fetch.ParseBand(value)
	if err != nil {
		return fetch.Band{}, fmt.Errorf("%w: --rank: %w", errUsage, err)
	}
	band.LeastFirst = leastFirst
	return band, nil
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

// pullDemand refreshes env's per-topic and per-country demand, which fetch plans with.
func pullDemand(ctx context.Context, cfg config.Config, local *pgxpool.Pool, env config.Env) error {
	remote, err := openPool(ctx, "REMOTE_DATABASE_URL_"+env.Suffix(), cfg.Remotes[env].DatabaseURL)
	if err != nil {
		return err
	}
	defer remote.Close()
	res, err := requests.PullDemand(ctx, local, remote, env)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "demand pulled", "env", env, "topics_with_demand", res.Topics, "unknown_topics", res.Unknown)
	return nil
}

func runGroup(ctx context.Context, cfg config.Config, local *pgxpool.Pool, dryRun bool) error {
	runner, err := ai.New(cfg)
	if err != nil {
		return err
	}
	res, err := requests.New(local, runner, cfg).Run(ctx, dryRun)
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

func runFetch(ctx context.Context, cfg config.Config, all bool, band fetch.Band) error {
	pool, err := openPool(ctx, "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	runner, err := ai.New(cfg)
	if err != nil {
		return err
	}
	f := fetch.New(pool, runner, cfg).WithBand(band)
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
	return pruneEnv(ctx, cfg, env)
}

// pruneEnv deletes the stories env's hosted DB no longer needs.
func pruneEnv(ctx context.Context, cfg config.Config, env config.Env) error {
	remote, err := openPool(ctx, "REMOTE_DATABASE_URL_"+env.Suffix(), cfg.Remotes[env].DatabaseURL)
	if err != nil {
		return err
	}
	defer remote.Close()

	deleted, err := prune.Run(ctx, remote, prune.Settings{
		MaxAge: cfg.PruneMaxAge,
	}, time.Now())
	slog.InfoContext(ctx, "pruned", "env", env, "deleted_stories", deleted)
	return err
}

// runClean deletes what left the catalog from the local DB, then from each hosted DB in envs, keeping
// whatever any of them still uses. Local goes first: sync pushes every local topic, so one left
// locally would come back to the hosted DBs.
func runClean(ctx context.Context, cfg config.Config, envs []config.Env, apply bool) error {
	cat, err := catalog.Load(seed.Catalog)
	if err != nil {
		return err
	}
	local, err := openPool(ctx, "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL)
	if err != nil {
		return err
	}
	defer local.Close()

	type target struct {
		name string
		pool *pgxpool.Pool
	}
	targets := []target{{"local", local}}
	inUse := clean.NewInUse()
	if err := inUse.AddLocal(ctx, local); err != nil {
		return err
	}
	for _, env := range envs {
		remote, err := openPool(ctx, "REMOTE_DATABASE_URL_"+env.Suffix(), cfg.Remotes[env].DatabaseURL)
		if err != nil {
			return err
		}
		defer remote.Close()
		if err := inUse.AddHosted(ctx, remote); err != nil {
			return fmt.Errorf("%s: %w", env, err)
		}
		targets = append(targets, target{string(env), remote})
	}

	for _, t := range targets {
		plan, err := clean.PlanFor(ctx, t.pool, cat, inUse)
		if err != nil {
			return fmt.Errorf("%s: %w", t.name, err)
		}
		slog.InfoContext(ctx, "clean plan", "db", t.name,
			"delete_topics", plan.Topics, "delete_professions", plan.Professions, "delete_stories", plan.Stories,
			"kept_in_use_topics", plan.KeptTopics, "kept_in_use_professions", plan.KeptProfessions)
		if !apply || plan.Empty() {
			continue
		}
		res, err := clean.Apply(ctx, t.pool, plan)
		slog.InfoContext(ctx, "cleaned", "db", t.name, "topics", res.Topics, "professions", res.Professions, "stories", res.Stories)
		if err != nil {
			return fmt.Errorf("%s: %w", t.name, err)
		}
	}
	if !apply {
		slog.InfoContext(ctx, "dry run: nothing deleted; rerun with --apply (make curator-clean APPLY=1) to delete")
	}
	return nil
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

// runAll is a demand pull and fetch. With full, fetch repeats until every topic is covered. The pull runs for each hosted env with a database URL. Sync and prune are separate
// commands. A failing step is logged and the run continues where that is safe; the error returned
// lists every failed step.
func runAll(ctx context.Context, cfg config.Config, full bool, band fetch.Band) error {
	local, err := openPool(ctx, "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL)
	if err != nil {
		return err
	}
	defer local.Close()

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
	// Demand only: user topic requests are handled apart (`requests pull`, `requests group`, then sync).
	for _, env := range envs {
		step("demand pull "+string(env), func() error { return pullDemand(ctx, cfg, local, env) })
	}
	step("fetch", func() error { return runFetch(ctx, cfg, full, band) })
	return errors.Join(errs...)
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
