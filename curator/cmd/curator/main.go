// Command curator fetches stories with Claude into a local Postgres and syncs them to the
// hosted Postgres.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
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
	"github.com/saniuzzaman-robin/changeloom/curator/seed"
)

const connectTimeout = 10 * time.Second

const usage = `usage: curator <command> [flags]

commands:
  migrate [--remote]               apply the backend and curator migrations to the local DB;
                                   --remote applies only the backend migrations to REMOTE_DATABASE_URL
  seed                             load the topic catalog (seed/catalog.yaml) into the local DB
  fetch                            fetch new stories with Claude into the local DB
  requests pull                    copy pending topic requests and follower counts from the hosted DB
  requests group [--dry-run]       turn pending requests into topics with Claude
  sync                             push content and request decisions to the hosted DB
  run                              requests pull, requests group, fetch, then sync

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
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "migrate":
		fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
		remote := fs.Bool("remote", false, "apply only the backend migrations to REMOTE_DATABASE_URL")
		if err := parse(fs, rest); err != nil {
			return err
		}
		return runMigrate(ctx, cfg, *remote)
	case "seed":
		if err := parse(flag.NewFlagSet("seed", flag.ContinueOnError), rest); err != nil {
			return err
		}
		return runSeed(ctx, cfg)
	case "requests":
		if len(rest) == 0 || (rest[0] != "pull" && rest[0] != "group") {
			return fmt.Errorf("%w: requests needs pull or group", errUsage)
		}
		return fmt.Errorf("curator requests %s is not implemented yet", rest[0])
	case "fetch":
		if err := parse(flag.NewFlagSet("fetch", flag.ContinueOnError), rest); err != nil {
			return err
		}
		return runFetch(ctx, cfg)
	case "sync", "run":
		return fmt.Errorf("curator %s is not implemented yet", cmd)
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

func runMigrate(ctx context.Context, cfg config.Config, remote bool) error {
	key, url := "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL
	if remote {
		key, url = "REMOTE_DATABASE_URL", cfg.RemoteDatabaseURL
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
	nodes, err := catalog.Parse(seed.CatalogYAML)
	if err != nil {
		return err
	}
	pool, err := openPool(ctx, "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	res, err := catalog.Seed(ctx, pool, nodes)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "catalog seeded", "topics", res.Topics, "new_relations", res.Relations, "new_hints", res.Hints)
	return nil
}

func runFetch(ctx context.Context, cfg config.Config) error {
	pool, err := openPool(ctx, "LOCAL_DATABASE_URL", cfg.LocalDatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	sum, err := fetch.New(pool, claude.New(cfg.Claude), cfg).Run(ctx)
	slog.InfoContext(ctx, "fetch finished", "calls", sum.Calls, "failed", sum.Failed, "added", sum.Added,
		"merged", sum.Merged, "rejected", sum.Rejected, "deferred_topics", len(sum.Deferred), "cost_usd", sum.CostUSD)
	return err
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
