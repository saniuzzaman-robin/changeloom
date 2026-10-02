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

// Config holds the curator settings. Database URLs are validated where they are used, since
// not every command needs both.
type Config struct {
	// LocalDatabaseURL is the curator's own Postgres (LOCAL_DATABASE_URL).
	LocalDatabaseURL string
	// RemoteDatabaseURL is the hosted Postgres (REMOTE_DATABASE_URL): Neon's direct, non-pooled URL.
	RemoteDatabaseURL string
	// APIBaseURL is the hosted api, called on POST /internal/notify after a sync (API_BASE_URL).
	APIBaseURL string
	// NotifySecret is the bearer token for POST /internal/notify (NOTIFY_SECRET).
	NotifySecret string
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
		RemoteDatabaseURL:    getenv("REMOTE_DATABASE_URL", ""),
		APIBaseURL:           strings.TrimRight(getenv("API_BASE_URL", ""), "/"),
		NotifySecret:         getenv("NOTIFY_SECRET", ""),
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
