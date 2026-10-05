package config_test

import (
	"strings"
	"testing"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
)

func TestPromptStyle(t *testing.T) {
	for _, tc := range []struct {
		provider, style string
		want            config.PromptStyle
	}{
		{provider: "claude", want: config.PromptFrontier},
		{provider: "openai", want: config.PromptCompact},
		{provider: "openai", style: "frontier", want: config.PromptFrontier},
		{provider: "claude", style: "compact", want: config.PromptCompact},
	} {
		t.Setenv("CURATOR_AI_PROVIDER", tc.provider)
		t.Setenv("CURATOR_PROMPT_STYLE", tc.style)
		t.Setenv("OPENAI_MODEL", "gpt-test")
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
