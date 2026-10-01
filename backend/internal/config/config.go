// Package config loads process configuration from environment variables.
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

// Env is the deployment environment.
type Env string

// Supported environments.
const (
	EnvDev  Env = "dev"
	EnvProd Env = "prod"
)

// Config holds settings shared by the api and worker binaries.
type Config struct {
	Env         Env
	HTTPAddr    string
	DatabaseURL string
	LogLevel    slog.Level
	// TimelineWindow hides stories published longer ago than this from the timeline.
	TimelineWindow time.Duration
	// IngestMaxItemAge is how old a feed item may be and still be ingested.
	IngestMaxItemAge time.Duration
	// FirebaseProjectID is the Firebase project whose ID tokens the api accepts (required when ENV=prod).
	FirebaseProjectID string
	// PushEnabled turns on FCM push notifications in the worker (FCM_ENABLED). It needs
	// FIREBASE_PROJECT_ID and Application Default Credentials (GOOGLE_APPLICATION_CREDENTIALS).
	PushEnabled bool
	// AI configures AI processing. It is optional at load time (the api does not
	// need it); AI.Validate is called by the worker before it starts AI jobs.
	AI AIConfig
}

// AIConfig holds the settings for the AI processing pipeline.
type AIConfig struct {
	// Provider names the AI service (AI_PROVIDER); see ai.NewProvider for the registered ones.
	Provider string
	// APIKey is the provider credential (AI_API_KEY).
	APIKey string
	// Model is the model id used for all processing (AI_MODEL).
	Model string
	// DailyTokenBudget caps tokens submitted in any rolling 24 hours.
	DailyTokenBudget int64
	BatchMaxItems    int
	MaxAttempts      int
	// InputMaxChars trims raw item content sent to the model.
	InputMaxChars   int
	MaxOutputTokens int64
	MergeWindow     time.Duration
	// DedupeMaxPerRun bounds the stories checked for near-duplicates per run (AI_DEDUPE_MAX_PER_RUN).
	DedupeMaxPerRun int
	// DiscoveryEnabled turns on the daily web discovery agent (AI_DISCOVERY_ENABLED). It
	// costs web searches plus tokens on every run, so it is off by default.
	DiscoveryEnabled     bool
	DiscoveryMaxSearches int
	DiscoveryInterval    time.Duration
}

// DefaultAIProvider is used when AI_PROVIDER is unset.
const DefaultAIProvider = "gemini"

// Validate reports whether the settings needed to call the AI provider are present.
func (a AIConfig) Validate() error {
	var errs []error
	if a.APIKey == "" {
		errs = append(errs, errors.New("AI_API_KEY is required for AI processing"))
	}
	if a.Model == "" {
		errs = append(errs, errors.New("AI_MODEL is required for AI processing (see .env.example)"))
	}
	return errors.Join(errs...)
}

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	var errs []error

	env := Env(getenv("ENV", string(EnvDev)))
	if env != EnvDev && env != EnvProd {
		errs = append(errs, fmt.Errorf("ENV must be %q or %q, got %q", EnvDev, EnvProd, env))
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(getenv("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required (see .env.example)"))
	}

	windowDays, err := strconv.Atoi(getenv("TIMELINE_WINDOW_DAYS", "60"))
	if err != nil || windowDays < 1 {
		errs = append(errs, fmt.Errorf("TIMELINE_WINDOW_DAYS must be a positive integer, got %q", os.Getenv("TIMELINE_WINDOW_DAYS")))
	}

	maxAgeDays, err := strconv.Atoi(getenv("INGEST_MAX_ITEM_AGE_DAYS", "30"))
	if err != nil || maxAgeDays < 1 {
		errs = append(errs, fmt.Errorf("INGEST_MAX_ITEM_AGE_DAYS must be a positive integer, got %q", os.Getenv("INGEST_MAX_ITEM_AGE_DAYS")))
	}

	firebaseProjectID := getenv("FIREBASE_PROJECT_ID", "")
	if env == EnvProd && firebaseProjectID == "" {
		errs = append(errs, errors.New("FIREBASE_PROJECT_ID is required when ENV=prod (see .env.example)"))
	}

	budget := envInt(&errs, "AI_DAILY_TOKEN_BUDGET", 2_000_000)
	batchMax := envInt(&errs, "AI_BATCH_MAX_ITEMS", 100)
	maxAttempts := envInt(&errs, "AI_MAX_ATTEMPTS", 3)
	inputMax := envInt(&errs, "AI_INPUT_MAX_CHARS", 24_000)
	outputMax := envInt(&errs, "AI_MAX_OUTPUT_TOKENS", 8_000)
	mergeDays := envInt(&errs, "AI_MERGE_WINDOW_DAYS", 7)
	dedupeMax := envInt(&errs, "AI_DEDUPE_MAX_PER_RUN", 10)
	discoverySearches := envInt(&errs, "AI_DISCOVERY_MAX_SEARCHES", 8)
	discoveryHours := envInt(&errs, "AI_DISCOVERY_INTERVAL_HOURS", 24)
	discoveryEnabled := envBool(&errs, "AI_DISCOVERY_ENABLED")
	pushEnabled := envBool(&errs, "FCM_ENABLED")
	if pushEnabled && firebaseProjectID == "" {
		errs = append(errs, errors.New("FIREBASE_PROJECT_ID is required when FCM_ENABLED=true (see .env.example)"))
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}

	return Config{
		Env:            env,
		HTTPAddr:       getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:    dbURL,
		LogLevel:       level,
		TimelineWindow: time.Duration(windowDays) * 24 * time.Hour,

		IngestMaxItemAge:  time.Duration(maxAgeDays) * 24 * time.Hour,
		FirebaseProjectID: firebaseProjectID,
		PushEnabled:       pushEnabled,
		AI: AIConfig{
			Provider:         getenv("AI_PROVIDER", DefaultAIProvider),
			APIKey:           strings.TrimSpace(os.Getenv("AI_API_KEY")),
			Model:            getenv("AI_MODEL", ""),
			DailyTokenBudget: int64(budget),
			BatchMaxItems:    batchMax,
			MaxAttempts:      maxAttempts,
			InputMaxChars:    inputMax,
			MaxOutputTokens:  int64(outputMax),
			MergeWindow:      time.Duration(mergeDays) * 24 * time.Hour,
			DedupeMaxPerRun:  dedupeMax,

			DiscoveryEnabled:     discoveryEnabled,
			DiscoveryMaxSearches: discoverySearches,
			DiscoveryInterval:    time.Duration(discoveryHours) * time.Hour,
		},
	}, nil
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

// envBool reads a boolean setting (default false), appending to errs when it is invalid.
func envBool(errs *[]error, key string) bool {
	raw := getenv(key, "false")
	b, err := strconv.ParseBool(raw)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be true or false, got %q", key, raw))
	}
	return b
}
