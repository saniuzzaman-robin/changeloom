package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
)

func TestPromptStyle(t *testing.T) {
	for _, tc := range []struct {
		provider, style string
		want            config.PromptStyle
	}{
		{provider: "claude", want: config.PromptFrontier},
		{provider: "openai", want: config.PromptCompact},
		{provider: "gemini", want: config.PromptCompact},
		{provider: "openai", style: "frontier", want: config.PromptFrontier},
		{provider: "claude", style: "compact", want: config.PromptCompact},
	} {
		t.Setenv("CURATOR_AI_PROVIDER", tc.provider)
		t.Setenv("CURATOR_PROMPT_STYLE", tc.style)
		t.Setenv("OPENAI_MODEL", "gpt-test")
		t.Setenv("GEMINI_API_KEY", "key-test")
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("%s/%q: %v", tc.provider, tc.style, err)
		}
		if cfg.PromptStyle != tc.want {
			t.Errorf("%s/%q: style %q, want %q", tc.provider, tc.style, cfg.PromptStyle, tc.want)
		}
	}

	t.Setenv("CURATOR_PROMPT_STYLE", "tiny")
	if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "CURATOR_PROMPT_STYLE") {
		t.Fatalf("invalid style: err = %v", err)
	}
}

func TestPriorityIntervals(t *testing.T) {
	t.Setenv("CURATOR_AI_PROVIDER", "claude")
	t.Setenv("CURATOR_PRIORITY_INTERVAL_HOURS", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := [5]time.Duration{48 * time.Hour, 96 * time.Hour, 168 * time.Hour, 336 * time.Hour, 672 * time.Hour}; cfg.PriorityIntervals != want {
		t.Errorf("default = %v, want %v", cfg.PriorityIntervals, want)
	}

	t.Setenv("CURATOR_PRIORITY_INTERVAL_HOURS", "12, 24,48,96,192")
	if cfg, err = config.Load(); err != nil {
		t.Fatal(err)
	}
	if want := [5]time.Duration{12 * time.Hour, 24 * time.Hour, 48 * time.Hour, 96 * time.Hour, 192 * time.Hour}; cfg.PriorityIntervals != want {
		t.Errorf("set = %v, want %v", cfg.PriorityIntervals, want)
	}

	for _, bad := range []string{"1,2,3,4", "1,2,3,4,5,6", "1,2,0,4,5", "1,2,x,4,5", "1,2,-3,4,5"} {
		t.Setenv("CURATOR_PRIORITY_INTERVAL_HOURS", bad)
		if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "CURATOR_PRIORITY_INTERVAL_HOURS") {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

func TestGeminiNeedsAnAPIKey(t *testing.T) {
	t.Setenv("CURATOR_AI_PROVIDER", "gemini")
	t.Setenv("GEMINI_API_KEY", "")
	if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Fatalf("err = %v", err)
	}
	t.Setenv("GEMINI_API_KEY", "key-test")
	t.Setenv("CURATOR_AI_MODEL", "gemini-test")
	cfg, err := config.Load()
	if err != nil || cfg.Gemini.Model != "gemini-test" {
		t.Fatalf("model %q, err %v", cfg.Gemini.Model, err)
	}
}

func TestLocalNeedsAModel(t *testing.T) {
	t.Setenv("CURATOR_AI_PROVIDER", "local")
	t.Setenv("OLLAMA_MODEL", "")
	t.Setenv("CURATOR_AI_MODEL", "")
	if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "OLLAMA_MODEL") {
		t.Fatalf("err = %v", err)
	}
	t.Setenv("CURATOR_AI_MODEL", "model-test")
	cfg, err := config.Load()
	if err != nil || cfg.Ollama.Model != "model-test" || cfg.PromptStyle != config.PromptCompact {
		t.Fatalf("model %q, style %q, err %v", cfg.Ollama.Model, cfg.PromptStyle, err)
	}
}

func TestLocalSpeedDefaults(t *testing.T) {
	t.Setenv("CURATOR_AI_PROVIDER", "local")
	t.Setenv("CURATOR_AI_MODEL", "model-test")
	cfg, err := config.Load()
	if err != nil || cfg.Ollama.Think || cfg.Ollama.MaxTurns != 5 || cfg.SearchMaxResults != 5 || cfg.PageMaxChars != 6000 {
		t.Fatalf("think %v, turns %d, results %d, chars %d, err %v", cfg.Ollama.Think, cfg.Ollama.MaxTurns, cfg.SearchMaxResults, cfg.PageMaxChars, err)
	}
	t.Setenv("OLLAMA_THINK", "maybe")
	if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "OLLAMA_THINK") {
		t.Fatalf("err = %v", err)
	}
}
