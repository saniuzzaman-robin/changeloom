// Package config loads process configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// Env is the deployment environment.
type Env string

// Supported environments.
const (
	EnvDev     Env = "dev"
	EnvStaging Env = "staging"
	EnvProd    Env = "prod"
)

// Config holds the api settings.
type Config struct {
	Env Env
	// HTTPAddr is HTTP_ADDR, or ":$PORT" when PORT is set (the Cloud Run convention).
	HTTPAddr    string
	DatabaseURL string
	LogLevel    slog.Level
	// TimelineWindow hides stories published longer ago than this from the timeline.
	TimelineWindow time.Duration
	// FirebaseProjectID is the Firebase project whose ID tokens the api accepts (required unless ENV=dev).
	FirebaseProjectID string
	// PushEnabled turns on FCM push notifications (FCM_ENABLED). It needs
	// FIREBASE_PROJECT_ID and Application Default Credentials (GOOGLE_APPLICATION_CREDENTIALS).
	PushEnabled bool
	// NotifySecret is the bearer token for POST /internal/notify (NOTIFY_SECRET); empty
	// disables the route.
	NotifySecret string
	// TopicRequestMaxPending caps the pending topic requests per user (TOPIC_REQUEST_MAX_PENDING).
	TopicRequestMaxPending int
	// DBMaxConns caps the database pool per instance (DB_MAX_CONNS). Keep it at or below the
	// connections Neon allows divided by the max Cloud Run instances.
	DBMaxConns int32
	// RequestTimeout bounds each request, including its database queries (REQUEST_TIMEOUT, a Go
	// duration). It must leave room for POST /internal/notify, which sends every pending push.
	RequestTimeout time.Duration
}

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	var errs []error

	// No default: a missing ENV must not silently enable dev auth.
	env := Env(getenv("ENV", ""))
	if env == "" {
		errs = append(errs, fmt.Errorf("ENV is required: %q, %q or %q (see .env.example)", EnvDev, EnvStaging, EnvProd))
	} else if env != EnvDev && env != EnvStaging && env != EnvProd {
		errs = append(errs, fmt.Errorf("ENV must be %q, %q or %q, got %q", EnvDev, EnvStaging, EnvProd, env))
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

	httpAddr := getenv("HTTP_ADDR", ":8080")
	if port := getenv("PORT", ""); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			errs = append(errs, fmt.Errorf("PORT must be a port number, got %q", port))
		}
		httpAddr = ":" + port
	}

	firebaseProjectID := getenv("FIREBASE_PROJECT_ID", "")
	if (env == EnvStaging || env == EnvProd) && firebaseProjectID == "" {
		errs = append(errs, fmt.Errorf("FIREBASE_PROJECT_ID is required when ENV=%s (see deploy/%s.env.example)", env, env))
	}

	pushEnabled := envBool(&errs, "FCM_ENABLED")
	topicRequestMaxPending := envInt(&errs, "TOPIC_REQUEST_MAX_PENDING", 10)
	dbMaxConns := envInt(&errs, "DB_MAX_CONNS", 10)
	if dbMaxConns > math.MaxInt32 {
		errs = append(errs, fmt.Errorf("DB_MAX_CONNS is too large, got %d", dbMaxConns))
		dbMaxConns = 1
	}
	requestTimeout := envDuration(&errs, "REQUEST_TIMEOUT", 30*time.Second)
	if pushEnabled && firebaseProjectID == "" {
		errs = append(errs, errors.New("FIREBASE_PROJECT_ID is required when FCM_ENABLED=true (see .env.example)"))
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}

	return Config{
		Env:            env,
		HTTPAddr:       httpAddr,
		DatabaseURL:    dbURL,
		LogLevel:       level,
		TimelineWindow: time.Duration(windowDays) * 24 * time.Hour,

		FirebaseProjectID: firebaseProjectID,
		PushEnabled:       pushEnabled,

		NotifySecret:           strings.TrimSpace(os.Getenv("NOTIFY_SECRET")),
		TopicRequestMaxPending: topicRequestMaxPending,
		DBMaxConns:             int32(dbMaxConns),
		RequestTimeout:         requestTimeout,
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

// envDuration reads a positive Go duration setting (e.g. "30s"), appending to errs when it is invalid.
func envDuration(errs *[]error, key string, fallback time.Duration) time.Duration {
	raw := getenv(key, fallback.String())
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be a positive duration like 30s, got %q", key, raw))
		return fallback
	}
	return d
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
