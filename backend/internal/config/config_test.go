package config

import (
	"strings"
	"testing"
	"time"
)

// setBase sets a minimal valid dev environment; `make test` exports .env, so every key the tests
// depend on is set explicitly.
func setBase(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"ENV":                       "dev",
		"DATABASE_URL":              "postgres://localhost/test",
		"PORT":                      "",
		"FIREBASE_PROJECT_ID":       "",
		"FCM_ENABLED":               "",
		"DB_MAX_CONNS":              "",
		"REQUEST_TIMEOUT":           "",
		"TIMELINE_WINDOW_DAYS":      "",
		"TOPIC_REQUEST_MAX_PENDING": "",
		"LOG_LEVEL":                 "",
	} {
		t.Setenv(k, v)
	}
}

func TestLoadRequiresEnv(t *testing.T) {
	setBase(t)
	t.Setenv("ENV", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "ENV is required") {
		t.Fatalf("Load() error = %v, want ENV is required", err)
	}
}

func TestLoadRejectsUnknownEnv(t *testing.T) {
	setBase(t)
	t.Setenv("ENV", "production")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ENV must be") {
		t.Fatalf("Load() error = %v, want ENV must be", err)
	}
}

func TestLoadDefaults(t *testing.T) {
	setBase(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Env != EnvDev || cfg.DBMaxConns != 10 || cfg.RequestTimeout != 30*time.Second {
		t.Errorf("Load() = env %q, max conns %d, timeout %s; want dev, 10, 30s", cfg.Env, cfg.DBMaxConns, cfg.RequestTimeout)
	}
}

func TestLoadPoolAndTimeout(t *testing.T) {
	setBase(t)
	t.Setenv("DB_MAX_CONNS", "25")
	t.Setenv("REQUEST_TIMEOUT", "45s")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DBMaxConns != 25 || cfg.RequestTimeout != 45*time.Second {
		t.Errorf("Load() = max conns %d, timeout %s; want 25, 45s", cfg.DBMaxConns, cfg.RequestTimeout)
	}
}

func TestLoadRejectsBadPoolAndTimeout(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"DB_MAX_CONNS", "0"},
		{"DB_MAX_CONNS", "99999999999"},
		{"REQUEST_TIMEOUT", "30"},
		{"REQUEST_TIMEOUT", "-1s"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			setBase(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Errorf("Load() error = %v, want one naming %s", err, tc.key)
			}
		})
	}
}

func TestLoadHostedNeedsFirebaseProject(t *testing.T) {
	setBase(t)
	t.Setenv("ENV", "prod")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "FIREBASE_PROJECT_ID") {
		t.Fatalf("Load() error = %v, want FIREBASE_PROJECT_ID required", err)
	}
}
