// Package config loads curator configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Env is a hosted environment the curator pushes content to.
type Env string

// Hosted environments.
const (
	EnvStaging Env = "staging"
	EnvProd    Env = "prod"
)

// Envs lists the hosted environments.
var Envs = []Env{EnvStaging, EnvProd}

// ParseEnv validates a hosted environment name.
func ParseEnv(s string) (Env, error) {
	for _, env := range Envs {
		if s == string(env) {
			return env, nil
		}
	}
	return "", fmt.Errorf("environment must be %q or %q, got %q", EnvStaging, EnvProd, s)
}

// Suffix is the suffix of the env's variables, e.g. REMOTE_DATABASE_URL_STAGING.
func (e Env) Suffix() string { return strings.ToUpper(string(e)) }

// Remote holds one hosted environment's settings, from the variables ending in _<ENV>.
type Remote struct {
	// DatabaseURL is the hosted Postgres (REMOTE_DATABASE_URL_<ENV>): Neon's direct, non-pooled URL.
	DatabaseURL string
	// APIBaseURL is the hosted api, called on POST /internal/notify after a sync (API_BASE_URL_<ENV>).
	APIBaseURL string
	// NotifySecret is the bearer token for POST /internal/notify (NOTIFY_SECRET_<ENV>).
	NotifySecret string
}

// Config holds the curator settings. Database URLs are validated where they are used, since
// not every command needs them all.
type Config struct {
	// LocalDatabaseURL is the curator's own Postgres (LOCAL_DATABASE_URL).
	LocalDatabaseURL string
	// Remotes holds the settings of each hosted environment.
	Remotes map[Env]Remote
	// BackendMigrationsDir holds the backend's goose migrations (BACKEND_MIGRATIONS_DIR).
	BackendMigrationsDir string
	LogLevel             slog.Level

	Claude Claude

	// MaxCallsPerRun caps the Claude fetch calls per run (CURATOR_MAX_CALLS_PER_RUN).
	MaxCallsPerRun int
	// TopicsPerCall is the most topics fetched in one Claude call (CURATOR_TOPICS_PER_CALL).
	TopicsPerCall int
	// MaxNewTopicsPerRun caps the topics created from requests per run (CURATOR_MAX_NEW_TOPICS_PER_RUN).
	MaxNewTopicsPerRun int
	// ItemMaxAge drops stories published longer ago than this (CURATOR_ITEM_MAX_AGE_DAYS).
	ItemMaxAge time.Duration
	// MergeWindow is how far back a new story about the same CVE or project+version is merged
	// into an existing one (CURATOR_MERGE_WINDOW_DAYS).
	MergeWindow time.Duration
	// HotMinViews is how many distinct viewers of a topic's stories in the last week make the topic
	// hot, fetched on every run (CURATOR_HOT_MIN_VIEWS).
	HotMinViews int
	// WarmInterval is the least time between fetches of a topic that is followed or serves a user's
	// profession but is not hot (CURATOR_WARM_INTERVAL_HOURS).
	WarmInterval time.Duration
	// ColdInterval is the same for a topic nobody follows or sees (CURATOR_COLD_INTERVAL_HOURS).
	ColdInterval time.Duration
	// StoriesPerTopic is the most stories asked for per topic in one fetch call
	// (CURATOR_STORIES_PER_TOPIC).
	StoriesPerTopic int
	// Concurrency is how many Claude calls run at once (CURATOR_CONCURRENCY).
	Concurrency int

	// DealsMaxCountries is the most countries deal topics are fetched for per run, the ones with the
	// most users (CURATOR_DEALS_MAX_COUNTRIES).
	DealsMaxCountries int
	// DealsMaxCallsPerRun caps the per-country deal calls per run, on top of MaxCallsPerRun
	// (CURATOR_DEALS_MAX_CALLS_PER_RUN).
	DealsMaxCallsPerRun int
	// DealsMaxAge drops deals published longer ago than this, and prunes unsaved ones older than it
	// (CURATOR_DEALS_MAX_AGE_DAYS).
	DealsMaxAge time.Duration

	// BackfillTarget is the story count per topic that backfill aims for (CURATOR_BACKFILL_TARGET).
	BackfillTarget int
	// BackfillTopicsPerCall is the topics per backfill call (CURATOR_BACKFILL_TOPICS_PER_CALL).
	BackfillTopicsPerCall int
	// BackfillCallsPerRun is the backfill calls `curator run` makes (CURATOR_BACKFILL_CALLS_PER_RUN).
	BackfillCallsPerRun int

	// RunEnvs are the hosted envs `curator run` pulls from, syncs to and prunes (CURATOR_RUN_ENVS,
	// comma-separated; default prod).
	RunEnvs []Env
	// RunMinGap is the least time since the last successful fetch for `curator run --if-due` to run
	// (CURATOR_RUN_MIN_GAP_HOURS).
	RunMinGap time.Duration

	// PruneInterval is how often `curator run` prunes a hosted DB (CURATOR_PRUNE_INTERVAL_DAYS).
	PruneInterval time.Duration
	// PruneMaxAge: unsaved stories published longer ago than this are deleted
	// (CURATOR_PRUNE_MAX_AGE_DAYS). Sync also skips stories older than this.
	PruneMaxAge time.Duration
	// PruneGrace: unsaved stories older than this are deleted when fewer than PruneMinViews
	// distinct users saw them (CURATOR_PRUNE_GRACE_DAYS).
	PruneGrace time.Duration
	// PruneMinViews is the distinct viewers a story older than PruneGrace needs to be kept
	// (CURATOR_PRUNE_MIN_VIEWS).
	PruneMinViews int
}

// Claude configures the `claude -p` calls.
type Claude struct {
	// Bin is the claude CLI to run (CLAUDE_BIN).
	Bin string
	// Model is passed to --model (CLAUDE_MODEL).
	Model string
	// Timeout bounds one call (CLAUDE_TIMEOUT, a Go duration).
	Timeout time.Duration
}

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	var errs []error

	var level slog.Level
	if err := level.UnmarshalText([]byte(getenv("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}

	timeout, err := time.ParseDuration(getenv("CLAUDE_TIMEOUT", "10m"))
	if err != nil || timeout <= 0 {
		errs = append(errs, fmt.Errorf("CLAUDE_TIMEOUT must be a positive Go duration such as 10m, got %q", os.Getenv("CLAUDE_TIMEOUT")))
	}

	cfg := Config{
		LocalDatabaseURL:     getenv("LOCAL_DATABASE_URL", ""),
		Remotes:              make(map[Env]Remote, len(Envs)),
		BackendMigrationsDir: getenv("BACKEND_MIGRATIONS_DIR", "../backend/migrations"),
		LogLevel:             level,
		Claude: Claude{
			Bin:     getenv("CLAUDE_BIN", "claude"),
			Model:   getenv("CLAUDE_MODEL", "sonnet"),
			Timeout: timeout,
		},
		MaxCallsPerRun:     envInt(&errs, "CURATOR_MAX_CALLS_PER_RUN", 8),
		TopicsPerCall:      envInt(&errs, "CURATOR_TOPICS_PER_CALL", 5),
		MaxNewTopicsPerRun: envInt(&errs, "CURATOR_MAX_NEW_TOPICS_PER_RUN", 5),
		ItemMaxAge:         time.Duration(envInt(&errs, "CURATOR_ITEM_MAX_AGE_DAYS", 14)) * 24 * time.Hour,
		MergeWindow:        time.Duration(envInt(&errs, "CURATOR_MERGE_WINDOW_DAYS", 14)) * 24 * time.Hour,
		HotMinViews:        envInt(&errs, "CURATOR_HOT_MIN_VIEWS", 1),
		WarmInterval:       time.Duration(envInt(&errs, "CURATOR_WARM_INTERVAL_HOURS", 24)) * time.Hour,
		ColdInterval:       time.Duration(envInt(&errs, "CURATOR_COLD_INTERVAL_HOURS", 168)) * time.Hour,
		StoriesPerTopic:    envInt(&errs, "CURATOR_STORIES_PER_TOPIC", 5),
		Concurrency:        envInt(&errs, "CURATOR_CONCURRENCY", 2),

		DealsMaxCountries:   envInt(&errs, "CURATOR_DEALS_MAX_COUNTRIES", 5),
		DealsMaxCallsPerRun: envInt(&errs, "CURATOR_DEALS_MAX_CALLS_PER_RUN", 4),
		DealsMaxAge:         time.Duration(envInt(&errs, "CURATOR_DEALS_MAX_AGE_DAYS", 7)) * 24 * time.Hour,

		BackfillTarget:        envInt(&errs, "CURATOR_BACKFILL_TARGET", 20),
		BackfillTopicsPerCall: envInt(&errs, "CURATOR_BACKFILL_TOPICS_PER_CALL", 2),
		BackfillCallsPerRun:   envInt(&errs, "CURATOR_BACKFILL_CALLS_PER_RUN", 2),

		RunMinGap: time.Duration(envInt(&errs, "CURATOR_RUN_MIN_GAP_HOURS", 5)) * time.Hour,

		PruneInterval: time.Duration(envInt(&errs, "CURATOR_PRUNE_INTERVAL_DAYS", 7)) * 24 * time.Hour,
		PruneMaxAge:   time.Duration(envInt(&errs, "CURATOR_PRUNE_MAX_AGE_DAYS", 14)) * 24 * time.Hour,
		PruneGrace:    time.Duration(envInt(&errs, "CURATOR_PRUNE_GRACE_DAYS", 7)) * 24 * time.Hour,
		PruneMinViews: envInt(&errs, "CURATOR_PRUNE_MIN_VIEWS", 10),
	}

	for _, env := range Envs {
		cfg.Remotes[env] = Remote{
			DatabaseURL:  getenv("REMOTE_DATABASE_URL_"+env.Suffix(), ""),
			APIBaseURL:   strings.TrimRight(getenv("API_BASE_URL_"+env.Suffix(), ""), "/"),
			NotifySecret: getenv("NOTIFY_SECRET_"+env.Suffix(), ""),
		}
	}

	for _, name := range strings.Split(getenv("CURATOR_RUN_ENVS", string(EnvProd)), ",") {
		env, err := ParseEnv(strings.TrimSpace(name))
		if err != nil {
			errs = append(errs, fmt.Errorf("CURATOR_RUN_ENVS: %w", err))
			continue
		}
		cfg.RunEnvs = append(cfg.RunEnvs, env)
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// envInt reads a positive integer setting, appending to errs when it is invalid.
func envInt(errs *[]error, key string, fallback int) int {
	raw := getenv(key, strconv.Itoa(fallback))
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		*errs = append(*errs, fmt.Errorf("%s must be a positive integer, got %q", key, raw))
		return fallback
	}
	return n
}
