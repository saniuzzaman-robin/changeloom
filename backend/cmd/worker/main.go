// Command worker runs Changeloom's background jobs and maintenance commands.
//
// Usage:
//
//	worker                     run the job worker (ingestion)
//	worker sources sync        sync seed/sources.yaml into the database
//	worker sources poll NAME   poll one source once and print what was stored
//	worker process-one ID [--apply]
//	                           run one raw item through Claude synchronously and print
//	                           the result; with --apply, store it like a batch result
//	worker eval export ID      print a raw item as an eval fixture (JSON)
//	worker eval run [DIR]      run eval fixtures (default internal/ai/testdata/eval)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/ai"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/config"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/ingest"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/jobs"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/push"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/sources"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/topics"
	"github.com/saniuzzaman-robin/changeloom/backend/seed"
)

const (
	startupDBTimeout = 10 * time.Second
	shutdownTimeout  = 30 * time.Second
	usage            = "usage: worker [sources sync | sources poll NAME | process-one ID [--apply] | eval export ID | eval run [DIR]]"
	defaultEvalDir   = "internal/ai/testdata/eval"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("worker exited with error", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch {
	case len(args) == 0:
		return runWorker(ctx, cfg, pool)
	case len(args) == 2 && args[0] == "sources" && args[1] == "sync":
		return syncSources(ctx, pool)
	case len(args) == 3 && args[0] == "sources" && args[1] == "poll":
		return pollSource(ctx, cfg, pool, args[2])
	case len(args) >= 2 && args[0] == "process-one":
		return processOne(ctx, cfg, pool, args[1:])
	case len(args) == 3 && args[0] == "eval" && args[1] == "export":
		return evalExport(ctx, cfg, pool, args[2])
	case len(args) >= 2 && args[0] == "eval" && args[1] == "run" && len(args) <= 3:
		return evalRun(ctx, cfg, pool, args[2:])
	default:
		return errors.New(usage)
	}
}

func connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("configure database pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, startupDBTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database (is it running? try `make db-up`): %w", err)
	}
	return pool, nil
}

func runWorker(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) error {
	if err := jobs.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("%w (have app migrations been applied? try `make migrate`)", err)
	}
	ingester := ingest.New(pool, ingest.NewHTTPClient(), cfg.IngestMaxItemAge)
	var opt jobs.Optional
	if err := cfg.AI.Validate(); err != nil {
		slog.Warn("AI processing disabled", "reason", err.Error())
	} else {
		var err error
		if opt.Processor, err = newProcessor(pool, cfg); err != nil {
			return err
		}
		if cfg.AI.DiscoveryEnabled {
			if opt.Discoverer, err = newDiscoverer(pool, ingester, cfg); err != nil {
				return err
			}
		}
	}
	if cfg.AI.DiscoveryEnabled && opt.Discoverer == nil {
		slog.Warn("discovery agent disabled: AI processing is not configured")
	}
	if cfg.PushEnabled {
		sender, err := push.NewFCMSender(ctx, cfg.FirebaseProjectID)
		if err != nil {
			return fmt.Errorf("push notifications: %w", err)
		}
		opt.Notifier = push.NewNotifier(pool, sender)
	}
	client, err := jobs.NewWorkerClient(pool, ingester, opt)
	if err != nil {
		return fmt.Errorf("create job client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start job client: %w", err)
	}
	slog.Info("worker started", "env", cfg.Env)

	<-ctx.Done()
	slog.Info("shutting down worker")
	stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return client.Stop(stopCtx)
}

func syncSources(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := topics.Sync(ctx, pool, seed.TopicsYAML); err != nil {
		return fmt.Errorf("%w (have migrations been applied? try `make migrate`)", err)
	}
	n, err := sources.Sync(ctx, pool, seed.SourcesYAML)
	if err != nil {
		return err
	}
	slog.Info("sources synced", "count", n)
	return nil
}

func pollSource(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, name string) error {
	row, err := db.New(pool).GetSourceByName(ctx, name)
	if err != nil {
		return fmt.Errorf("find source %q (run `worker sources sync` first): %w", name, err)
	}
	stats, err := ingest.New(pool, ingest.NewHTTPClient(), cfg.IngestMaxItemAge).PollSource(ctx, row.ID)
	if err != nil {
		return err
	}
	fmt.Printf("%s: fetched=%d inserted=%d existing=%d too_old=%d invalid=%d not_modified=%t\n",
		name, stats.Fetched, stats.Inserted, stats.Existing, stats.TooOld, stats.Invalid, stats.NotModified)
	return nil
}

func newProcessor(pool *pgxpool.Pool, cfg config.Config) (*ai.Processor, error) {
	if err := cfg.AI.Validate(); err != nil {
		return nil, err
	}
	client, err := ai.NewAnthropicClient(cfg.AI.APIKey, cfg.AI.Model, cfg.AI.Effort, cfg.AI.MaxOutputTokens)
	if err != nil {
		return nil, err
	}
	return ai.NewProcessor(pool, client, ai.Settings{
		Model:            cfg.AI.Model,
		DailyTokenBudget: cfg.AI.DailyTokenBudget,
		BatchMaxItems:    cfg.AI.BatchMaxItems,
		MaxAttempts:      cfg.AI.MaxAttempts,
		InputMaxChars:    cfg.AI.InputMaxChars,
		MergeWindow:      cfg.AI.MergeWindow,
		DedupeMaxPerRun:  cfg.AI.DedupeMaxPerRun,
	}), nil
}

func newDiscoverer(pool *pgxpool.Pool, ingester *ingest.Ingester, cfg config.Config) (*ai.Discoverer, error) {
	client, err := ai.NewAnthropicClient(cfg.AI.APIKey, cfg.AI.Model, cfg.AI.Effort, cfg.AI.MaxOutputTokens)
	if err != nil {
		return nil, err
	}
	return ai.NewDiscoverer(pool, client, ingester, ai.DiscoverySettings{
		MaxSearches: int64(cfg.AI.DiscoveryMaxSearches),
		MinInterval: cfg.AI.DiscoveryInterval,
		MaxItemAge:  cfg.IngestMaxItemAge,
	}), nil
}

func processOne(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, args []string) error {
	apply := len(args) == 2 && args[1] == "--apply"
	if len(args) != 1 && !apply {
		return errors.New("usage: worker process-one ID [--apply]")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("raw item id must be a number, got %q", args[0])
	}
	proc, err := newProcessor(pool, cfg)
	if err != nil {
		return err
	}
	prev, err := proc.ProcessOne(ctx, id, apply)
	if err != nil {
		return err
	}
	fmt.Printf("stop_reason=%s tokens: input=%d output=%d cache_read=%d cache_creation=%d\n",
		prev.Result.StopReason, prev.Result.Usage.Input, prev.Result.Usage.Output, prev.Result.Usage.CacheRead, prev.Result.Usage.CacheCreation)
	if prev.ParseErr != nil {
		fmt.Printf("unusable output: %v\n%s\n", prev.ParseErr, prev.Result.Text)
	} else if prev.Result.Text != "" {
		fmt.Println(prev.Result.Text)
	}
	if apply {
		fmt.Println("applied")
	}
	return nil
}

func evalExport(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, idArg string) error {
	id, err := strconv.ParseInt(idArg, 10, 64)
	if err != nil {
		return fmt.Errorf("raw item id must be a number, got %q", idArg)
	}
	// Export only reads the database, so it does not need an API key.
	proc := ai.NewProcessor(pool, nil, ai.Settings{Model: cfg.AI.Model})
	f, err := proc.ExportFixture(ctx, id)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(f)
}

func evalRun(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, args []string) error {
	dir := defaultEvalDir
	if len(args) == 1 {
		dir = args[0]
	}
	fixtures, err := ai.LoadFixtures(dir)
	if err != nil {
		return err
	}
	if len(fixtures) == 0 {
		return fmt.Errorf("no fixtures in %s (create them with `worker eval export ID > %s/NAME.json`)", dir, dir)
	}
	proc, err := newProcessor(pool, cfg)
	if err != nil {
		return err
	}
	results, err := proc.Eval(ctx, fixtures)
	failed := 0
	for _, r := range results {
		status := "ok  "
		if len(r.Problems) > 0 {
			status = "FAIL"
			failed++
		}
		fmt.Printf("%s %s: relevant=%t kind=%s importance=%d topics=%v title=%q\n",
			status, r.Fixture, r.Output.Relevant, r.Output.Kind, r.Output.Importance, r.Output.Topics, r.Output.Title)
		for _, p := range r.Problems {
			fmt.Printf("       - %s\n", p)
		}
	}
	if err != nil {
		return err
	}
	fmt.Printf("%d fixtures, %d with problems\n", len(results), failed)
	if failed > 0 {
		return fmt.Errorf("%d fixtures have problems", failed)
	}
	return nil
}
