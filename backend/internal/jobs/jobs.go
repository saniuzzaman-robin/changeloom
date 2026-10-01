// Package jobs defines the River background jobs run by the worker binary.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/ai"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/ingest"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/push"
)

const (
	dueCheckInterval = time.Minute
	aiSubmitInterval = 30 * time.Minute
	aiPollInterval   = 5 * time.Minute
	dedupeInterval   = 10 * time.Minute
	pushInterval     = 5 * time.Minute
	// discoveryCheckInterval is how often the discovery job wakes up; the agent itself
	// enforces its configured minimum interval between real runs.
	discoveryCheckInterval = 6 * time.Hour
	pollWorkers            = 5
	jobTimeout             = 10 * time.Minute
)

// unfinishedStates makes uniqueness apply only to jobs that have not finished, so a
// completed poll never blocks the next one.
var unfinishedStates = []rivertype.JobState{
	rivertype.JobStateAvailable,
	rivertype.JobStatePending,
	rivertype.JobStateRunning,
	rivertype.JobStateRetryable,
	rivertype.JobStateScheduled,
}

// PollSourceArgs polls one source. Failed polls are not retried: the source is polled
// again at its next interval.
type PollSourceArgs struct {
	SourceID int64 `json:"source_id"`
}

// Kind implements river.JobArgs.
func (PollSourceArgs) Kind() string { return "poll_source" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (PollSourceArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: unfinishedStates}}
}

// PollSourceWorker runs PollSourceArgs jobs.
type PollSourceWorker struct {
	river.WorkerDefaults[PollSourceArgs]
	Ingester *ingest.Ingester
}

// Work implements river.Worker.
func (w *PollSourceWorker) Work(ctx context.Context, job *river.Job[PollSourceArgs]) error {
	_, err := w.Ingester.PollSource(ctx, job.Args.SourceID)
	return err
}

// EnqueueDueArgs enqueues a poll job for every source that is due.
type EnqueueDueArgs struct{}

// Kind implements river.JobArgs.
func (EnqueueDueArgs) Kind() string { return "enqueue_due_sources" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (EnqueueDueArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: unfinishedStates}}
}

// EnqueueDueWorker runs EnqueueDueArgs jobs.
type EnqueueDueWorker struct {
	river.WorkerDefaults[EnqueueDueArgs]
	Pool *pgxpool.Pool
}

// Work implements river.Worker.
func (w *EnqueueDueWorker) Work(ctx context.Context, _ *river.Job[EnqueueDueArgs]) error {
	n, err := EnqueueDue(ctx, w.Pool, river.ClientFromContext[pgx.Tx](ctx))
	if err != nil {
		return err
	}
	if n > 0 {
		slog.InfoContext(ctx, "enqueued due sources", "count", n)
	}
	return nil
}

// EnqueueDue inserts a poll job for each enabled source whose poll interval has elapsed.
// Sources that already have an unfinished poll job are skipped by job uniqueness.
func EnqueueDue(ctx context.Context, pool *pgxpool.Pool, client *river.Client[pgx.Tx]) (int, error) {
	ids, err := db.New(pool).ListDueSourceIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list due sources: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	params := make([]river.InsertManyParams, len(ids))
	for i, id := range ids {
		params[i] = river.InsertManyParams{Args: PollSourceArgs{SourceID: id}}
	}
	res, err := client.InsertMany(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("enqueue poll jobs: %w", err)
	}
	return len(res), nil
}

// SubmitBatchArgs submits pending raw items to the AI provider as one batch.
type SubmitBatchArgs struct{}

// Kind implements river.JobArgs.
func (SubmitBatchArgs) Kind() string { return "ai_submit_batch" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (SubmitBatchArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: unfinishedStates}}
}

// SubmitBatchWorker runs SubmitBatchArgs jobs.
type SubmitBatchWorker struct {
	river.WorkerDefaults[SubmitBatchArgs]
	Processor *ai.Processor
}

// Work implements river.Worker.
func (w *SubmitBatchWorker) Work(ctx context.Context, _ *river.Job[SubmitBatchArgs]) error {
	_, err := w.Processor.Submit(ctx)
	return err
}

// PollBatchesArgs polls open AI batches and applies finished ones.
type PollBatchesArgs struct{}

// Kind implements river.JobArgs.
func (PollBatchesArgs) Kind() string { return "ai_poll_batches" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (PollBatchesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: unfinishedStates}}
}

// PollBatchesWorker runs PollBatchesArgs jobs.
type PollBatchesWorker struct {
	river.WorkerDefaults[PollBatchesArgs]
	Processor *ai.Processor
}

// Work implements river.Worker.
func (w *PollBatchesWorker) Work(ctx context.Context, _ *river.Job[PollBatchesArgs]) error {
	return w.Processor.Poll(ctx)
}

// DedupeStoriesArgs merges near-duplicate stories using the AI provider.
type DedupeStoriesArgs struct{}

// Kind implements river.JobArgs.
func (DedupeStoriesArgs) Kind() string { return "ai_dedupe_stories" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (DedupeStoriesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: unfinishedStates}}
}

// DedupeStoriesWorker runs DedupeStoriesArgs jobs.
type DedupeStoriesWorker struct {
	river.WorkerDefaults[DedupeStoriesArgs]
	Processor *ai.Processor
}

// Work implements river.Worker.
func (w *DedupeStoriesWorker) Work(ctx context.Context, _ *river.Job[DedupeStoriesArgs]) error {
	_, err := w.Processor.DedupeStories(ctx)
	return err
}

// DiscoverArgs runs the web discovery agent.
type DiscoverArgs struct{}

// Kind implements river.JobArgs.
func (DiscoverArgs) Kind() string { return "ai_discover" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (DiscoverArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: unfinishedStates}}
}

// DiscoverWorker runs DiscoverArgs jobs.
type DiscoverWorker struct {
	river.WorkerDefaults[DiscoverArgs]
	Discoverer *ai.Discoverer
}

// Work implements river.Worker.
func (w *DiscoverWorker) Work(ctx context.Context, _ *river.Job[DiscoverArgs]) error {
	_, err := w.Discoverer.Run(ctx)
	return err
}

// PushNotifyArgs sends pending push notifications.
type PushNotifyArgs struct{}

// Kind implements river.JobArgs.
func (PushNotifyArgs) Kind() string { return "push_notify" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (PushNotifyArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: unfinishedStates}}
}

// PushNotifyWorker runs PushNotifyArgs jobs.
type PushNotifyWorker struct {
	river.WorkerDefaults[PushNotifyArgs]
	Notifier *push.Notifier
}

// Work implements river.Worker.
func (w *PushNotifyWorker) Work(ctx context.Context, _ *river.Job[PushNotifyArgs]) error {
	_, err := w.Notifier.Run(ctx)
	return err
}

// Migrate applies River's own schema migrations. It is idempotent.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("create river migrator: %w", err)
	}
	if _, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("migrate river schema: %w", err)
	}
	return nil
}

// NewInsertClient returns a client that can enqueue jobs but does not work them.
func NewInsertClient(pool *pgxpool.Pool) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{})
}

// Optional holds the components behind the jobs that need configuration. A nil field
// disables its jobs.
type Optional struct {
	// Processor enables AI batch processing and near-duplicate merging (needs an API key).
	Processor *ai.Processor
	// Discoverer enables the daily web discovery agent.
	Discoverer *ai.Discoverer
	// Notifier enables push notifications.
	Notifier *push.Notifier
}

// NewWorkerClient returns a client that works jobs and schedules the periodic jobs.
func NewWorkerClient(pool *pgxpool.Pool, ingester *ingest.Ingester, opt Optional) (*river.Client[pgx.Tx], error) {
	processor := opt.Processor
	workers := river.NewWorkers()
	river.AddWorker(workers, &PollSourceWorker{Ingester: ingester})
	river.AddWorker(workers, &EnqueueDueWorker{Pool: pool})

	periodic := []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(dueCheckInterval),
			func() (river.JobArgs, *river.InsertOpts) { return EnqueueDueArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
	}
	if processor != nil {
		river.AddWorker(workers, &SubmitBatchWorker{Processor: processor})
		river.AddWorker(workers, &PollBatchesWorker{Processor: processor})
		periodic = append(periodic,
			river.NewPeriodicJob(
				river.PeriodicInterval(aiSubmitInterval),
				func() (river.JobArgs, *river.InsertOpts) { return SubmitBatchArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(aiPollInterval),
				func() (river.JobArgs, *river.InsertOpts) { return PollBatchesArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
		)
	}

	if processor != nil {
		river.AddWorker(workers, &DedupeStoriesWorker{Processor: processor})
		periodic = append(periodic, river.NewPeriodicJob(
			river.PeriodicInterval(dedupeInterval),
			func() (river.JobArgs, *river.InsertOpts) { return DedupeStoriesArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		))
	}
	if opt.Discoverer != nil {
		river.AddWorker(workers, &DiscoverWorker{Discoverer: opt.Discoverer})
		periodic = append(periodic, river.NewPeriodicJob(
			river.PeriodicInterval(discoveryCheckInterval),
			func() (river.JobArgs, *river.InsertOpts) { return DiscoverArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		))
	}
	if opt.Notifier != nil {
		river.AddWorker(workers, &PushNotifyWorker{Notifier: opt.Notifier})
		periodic = append(periodic, river.NewPeriodicJob(
			river.PeriodicInterval(pushInterval),
			func() (river.JobArgs, *river.InsertOpts) { return PushNotifyArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		))
	}

	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		JobTimeout:   jobTimeout,
		Queues:       map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: pollWorkers}},
		Workers:      workers,
		PeriodicJobs: periodic,
	})
}
