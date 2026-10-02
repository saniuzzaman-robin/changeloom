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
	// UnfollowedInterval is the least time between fetches of a topic nobody follows
	// (CURATOR_UNFOLLOWED_INTERVAL_HOURS). Followed topics are fetched on every run.
	UnfollowedInterval time.Duration
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
		ItemMaxAge:         time.Duration(envInt(&errs, "CURATOR_ITEM_MAX_AGE_DAYS", 7)) * 24 * time.Hour,
		MergeWindow:        time.Duration(envInt(&errs, "CURATOR_MERGE_WINDOW_DAYS", 14)) * 24 * time.Hour,
		UnfollowedInterval: time.Duration(envInt(&errs, "CURATOR_UNFOLLOWED_INTERVAL_HOURS", 24)) * time.Hour,
	}

	for _, env := range Envs {
		cfg.Remotes[env] = Remote{
			DatabaseURL:  getenv("REMOTE_DATABASE_URL_"+env.Suffix(), ""),
			APIBaseURL:   strings.TrimRight(getenv("API_BASE_URL_"+env.Suffix(), ""), "/"),
			NotifySecret: getenv("NOTIFY_SECRET_"+env.Suffix(), ""),
		}
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
