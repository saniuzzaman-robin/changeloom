// Package config loads curator configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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

	// Provider selects the AI backend for fetch and request-grouping calls (CURATOR_AI_PROVIDER).
	Provider Provider
	Claude   Claude
	OpenAI   OpenAI
	Gemini   Gemini
	// PromptStyle selects the prompt wording (CURATOR_PROMPT_STYLE; defaults to frontier for claude
	// and compact for openai).
	PromptStyle PromptStyle
	// CallDelay is the minimum gap between the starts of two AI calls (CURATOR_CALL_DELAY, a Go
	// duration; 0 disables it).
	CallDelay time.Duration

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
	// HotMinEngaged is how many distinct users who opened or saved one of a topic's stories in the
	// last week make the topic hot, fetched on every run (CURATOR_HOT_MIN_ENGAGED).
	HotMinEngaged int
	// WarmInterval is the least time between fetches of a topic that is followed or serves a user's
	// profession but is not hot (CURATOR_WARM_INTERVAL_HOURS).
	WarmInterval time.Duration
	// PriorityIntervals[p-1] is the least time between fetches of a priority p topic nobody follows
	// or sees; a warm topic uses it too when it is shorter than WarmInterval
	// (CURATOR_PRIORITY_INTERVAL_HOURS, five comma-separated hours for priorities 1 to 5).
	PriorityIntervals [5]time.Duration
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
	// DealsMaxAge drops deals published longer ago than this (CURATOR_DEALS_MAX_AGE_DAYS).
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

	// PruneInterval is how often `curator run` prunes a hosted DB (CURATOR_PRUNE_INTERVAL_DAYS).
	PruneInterval time.Duration
	// PruneMaxAge: unsaved stories published longer ago than this are deleted
	// (CURATOR_PRUNE_MAX_AGE_DAYS). Sync also skips stories older than this.
	PruneMaxAge time.Duration
}

// Provider is an AI backend.
type Provider string

// The supported providers.
const (
	ProviderClaude Provider = "claude"
	ProviderOpenAI Provider = "openai"
	// ProviderGemini calls the Gemini API with a Google AI Studio key.
	ProviderGemini Provider = "gemini"
)

// PromptStyle is a wording of the curator's prompts.
type PromptStyle string

// The supported prompt styles: frontier for large models, compact (shorter, with explicit research
// steps) for smaller models.
const (
	PromptFrontier PromptStyle = "frontier"
	PromptCompact  PromptStyle = "compact"
)

// PromptStyles lists the supported prompt styles.
var PromptStyles = []PromptStyle{PromptFrontier, PromptCompact}

// Providers lists the supported providers.
var Providers = []Provider{ProviderClaude, ProviderOpenAI, ProviderGemini}

// OpenAI configures calls to an OpenAI-compatible /chat/completions API. The curator gives the
// model no tools, so the model must search the web on its own.
type OpenAI struct {
	// BaseURL is the API root, e.g. https://api.openai.com/v1 (OPENAI_BASE_URL).
	BaseURL string
	// APIKey is sent as a Bearer token; empty sends none, for local servers (OPENAI_API_KEY).
	APIKey string
	// Model is the model to call (OPENAI_MODEL); required with this provider.
	Model string
	// Timeout bounds one call, including its format retries (OPENAI_TIMEOUT, a Go duration).
	Timeout time.Duration
}

// Gemini configures calls to the Gemini API, with Google Search and URL context as tools.
type Gemini struct {
	// BaseURL is the API root (GEMINI_BASE_URL).
	BaseURL string
	// APIKey is a Google AI Studio key, sent in the x-goog-api-key header (GEMINI_API_KEY); required.
	APIKey string
	// Model is the model to call (GEMINI_MODEL); a Gemini 3 model, which can use tools with structured output.
	Model string
	// Timeout bounds one call, including its retries (GEMINI_TIMEOUT, a Go duration).
	Timeout time.Duration
}

// Claude configures the `claude -p` calls.
type Claude struct {
	// Bin is the claude CLI to run (CLAUDE_BIN).
	Bin string
	// Model is passed to --model (CLAUDE_MODEL).
	Model string
	// Timeout bounds one call (CLAUDE_TIMEOUT, a Go duration).
	Timeout time.Duration
	// ConfigDir is the Claude account directory passed as CLAUDE_CONFIG_DIR to every call
	// (CURATOR_CLAUDE_CONFIG_DIR, default ~/.claude-personal), so a CLAUDE_CONFIG_DIR exported in
	// the calling shell (e.g. the work account) never leaks in. Empty leaves the environment as is.
	ConfigDir string
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

	callDelay, err := time.ParseDuration(getenv("CURATOR_CALL_DELAY", "0s"))
	if err != nil || callDelay < 0 {
		errs = append(errs, fmt.Errorf("CURATOR_CALL_DELAY must be a non-negative Go duration such as 5s, got %q", os.Getenv("CURATOR_CALL_DELAY")))
	}

	openaiTimeout, err := time.ParseDuration(getenv("OPENAI_TIMEOUT", "10m"))
	if err != nil || openaiTimeout <= 0 {
		errs = append(errs, fmt.Errorf("OPENAI_TIMEOUT must be a positive Go duration such as 10m, got %q", os.Getenv("OPENAI_TIMEOUT")))
	}

	geminiTimeout, err := time.ParseDuration(getenv("GEMINI_TIMEOUT", "10m"))
	if err != nil || geminiTimeout <= 0 {
		errs = append(errs, fmt.Errorf("GEMINI_TIMEOUT must be a positive Go duration such as 10m, got %q", os.Getenv("GEMINI_TIMEOUT")))
	}

	provider := Provider(getenv("CURATOR_AI_PROVIDER", string(ProviderClaude)))
	if !slices.Contains(Providers, provider) {
		errs = append(errs, fmt.Errorf("CURATOR_AI_PROVIDER must be one of %v, got %q", Providers, provider))
	}

	configDir, err := claudeConfigDir()
	if err != nil {
		errs = append(errs, err)
	}

	cfg := Config{
		LocalDatabaseURL:     getenv("LOCAL_DATABASE_URL", ""),
		Remotes:              make(map[Env]Remote, len(Envs)),
		BackendMigrationsDir: getenv("BACKEND_MIGRATIONS_DIR", "../backend/migrations"),
		LogLevel:             level,
		Provider:             provider,
		OpenAI: OpenAI{
			BaseURL: strings.TrimRight(getenv("OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"),
			APIKey:  getenv("OPENAI_API_KEY", ""),
			Model:   getenv("OPENAI_MODEL", ""),
			Timeout: openaiTimeout,
		},
		Gemini: Gemini{
			BaseURL: strings.TrimRight(getenv("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta"), "/"),
			APIKey:  getenv("GEMINI_API_KEY", ""),
			Model:   getenv("GEMINI_MODEL", "gemini-3.8-flash"),
			Timeout: geminiTimeout,
		},
		CallDelay: callDelay,
		Claude: Claude{
			Bin:     getenv("CLAUDE_BIN", "claude"),
			Model:   getenv("CLAUDE_MODEL", "sonnet"),
			Timeout: timeout,

			ConfigDir: configDir,
		},
		MaxCallsPerRun:     envInt(&errs, "CURATOR_MAX_CALLS_PER_RUN", 8),
		TopicsPerCall:      envInt(&errs, "CURATOR_TOPICS_PER_CALL", 5),
		MaxNewTopicsPerRun: envInt(&errs, "CURATOR_MAX_NEW_TOPICS_PER_RUN", 5),
		ItemMaxAge:         time.Duration(envInt(&errs, "CURATOR_ITEM_MAX_AGE_DAYS", 14)) * 24 * time.Hour,
		MergeWindow:        time.Duration(envInt(&errs, "CURATOR_MERGE_WINDOW_DAYS", 14)) * 24 * time.Hour,
		HotMinEngaged:      envInt(&errs, "CURATOR_HOT_MIN_ENGAGED", 1),
		WarmInterval:       time.Duration(envInt(&errs, "CURATOR_WARM_INTERVAL_HOURS", 24)) * time.Hour,
		PriorityIntervals:  envHours(&errs, "CURATOR_PRIORITY_INTERVAL_HOURS", [5]int{48, 96, 168, 336, 672}),
		StoriesPerTopic:    envInt(&errs, "CURATOR_STORIES_PER_TOPIC", 5),
		Concurrency:        envInt(&errs, "CURATOR_CONCURRENCY", 2),

		DealsMaxCountries:   envInt(&errs, "CURATOR_DEALS_MAX_COUNTRIES", 5),
		DealsMaxCallsPerRun: envInt(&errs, "CURATOR_DEALS_MAX_CALLS_PER_RUN", 4),
		DealsMaxAge:         time.Duration(envInt(&errs, "CURATOR_DEALS_MAX_AGE_DAYS", 7)) * 24 * time.Hour,

		BackfillTarget:        envInt(&errs, "CURATOR_BACKFILL_TARGET", 20),
		BackfillTopicsPerCall: envInt(&errs, "CURATOR_BACKFILL_TOPICS_PER_CALL", 2),
		BackfillCallsPerRun:   envInt(&errs, "CURATOR_BACKFILL_CALLS_PER_RUN", 2),

		PruneInterval: time.Duration(envInt(&errs, "CURATOR_PRUNE_INTERVAL_DAYS", 1)) * 24 * time.Hour,
		PruneMaxAge:   time.Duration(envInt(&errs, "CURATOR_PRUNE_MAX_AGE_DAYS", 14)) * 24 * time.Hour,
	}

	for _, env := range Envs {
		cfg.Remotes[env] = Remote{
			DatabaseURL:  getenv("REMOTE_DATABASE_URL_"+env.Suffix(), ""),
			APIBaseURL:   strings.TrimRight(getenv("API_BASE_URL_"+env.Suffix(), ""), "/"),
			NotifySecret: getenv("NOTIFY_SECRET_"+env.Suffix(), ""),
		}
	}

	// CURATOR_AI_MODEL overrides the selected provider's model, e.g. for a one-off run.
	if model := getenv("CURATOR_AI_MODEL", ""); model != "" {
		switch provider {
		case ProviderClaude:
			cfg.Claude.Model = model
		case ProviderOpenAI:
			cfg.OpenAI.Model = model
		case ProviderGemini:
			cfg.Gemini.Model = model
		}
	}
	if provider == ProviderOpenAI && cfg.OpenAI.Model == "" {
		errs = append(errs, errors.New("OPENAI_MODEL (or CURATOR_AI_MODEL) is required with CURATOR_AI_PROVIDER=openai"))
	}

	if provider == ProviderGemini && cfg.Gemini.APIKey == "" {
		errs = append(errs, errors.New("GEMINI_API_KEY (a Google AI Studio key) is required with CURATOR_AI_PROVIDER=gemini"))
	}

	defaultStyle := PromptCompact
	if provider == ProviderClaude {
		defaultStyle = PromptFrontier
	}
	cfg.PromptStyle = PromptStyle(getenv("CURATOR_PROMPT_STYLE", string(defaultStyle)))
	if !slices.Contains(PromptStyles, cfg.PromptStyle) {
		errs = append(errs, fmt.Errorf("CURATOR_PROMPT_STYLE must be one of %v, got %q", PromptStyles, cfg.PromptStyle))
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

// envHours reads exactly five comma-separated positive hour counts, appending to errs when they are
// invalid.
func envHours(errs *[]error, key string, fallback [5]int) [5]time.Duration {
	var out [5]time.Duration
	for i, h := range fallback {
		out[i] = time.Duration(h) * time.Hour
	}
	raw := getenv(key, "")
	if raw == "" {
		return out
	}
	parts := strings.Split(raw, ",")
	if len(parts) != len(out) {
		*errs = append(*errs, fmt.Errorf("%s must be %d comma-separated positive integers, got %q", key, len(out), raw))
		return out
	}
	var parsed [5]time.Duration
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 1 {
			*errs = append(*errs, fmt.Errorf("%s must be %d comma-separated positive integers, got %q", key, len(out), raw))
			return out
		}
		parsed[i] = time.Duration(n) * time.Hour
	}
	return parsed
}

// claudeConfigDir returns CURATOR_CLAUDE_CONFIG_DIR, defaulting to ~/.claude-personal.
func claudeConfigDir() (string, error) {
	if dir := os.Getenv("CURATOR_CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve default Claude config dir (set CURATOR_CLAUDE_CONFIG_DIR): %w", err)
	}
	return filepath.Join(home, ".claude-personal"), nil
}
