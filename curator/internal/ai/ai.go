// Package ai picks the backend that answers the curator's calls (Claude, an OpenAI-compatible
// API or Gemini) and spaces the calls out so a provider's rate limit is not hit.
package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/gemini"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/openai"
)

// Runner makes one call; fetch.Runner and requests.Runner are satisfied by it.
type Runner interface {
	Run(ctx context.Context, req claude.Request) (claude.Result, error)
}

// providers builds the runner of each provider in config.Providers.
var providers = map[config.Provider]func(cfg config.Config) Runner{
	config.ProviderClaude: func(cfg config.Config) Runner { return claude.New(cfg.Claude) },
	config.ProviderOpenAI: func(cfg config.Config) Runner { return openai.New(cfg.OpenAI) },
	config.ProviderGemini: func(cfg config.Config) Runner { return gemini.New(cfg.Gemini) },
}

// New returns the runner for cfg.Provider, wrapped to leave cfg.CallDelay between call starts.
func New(cfg config.Config) (Runner, error) {
	build, ok := providers[cfg.Provider]
	if !ok {
		return nil, fmt.Errorf("unknown CURATOR_AI_PROVIDER %q", cfg.Provider)
	}
	return Spaced(build(cfg), cfg.CallDelay), nil
}

// Spaced returns r with at least delay between the starts of two calls, also across concurrent
// callers. A zero delay returns r as is.
func Spaced(r Runner, delay time.Duration) Runner {
	if delay <= 0 {
		return r
	}
	return &spaced{inner: r, delay: delay, now: time.Now}
}

type spaced struct {
	inner Runner
	delay time.Duration
	now   func() time.Time

	mu   sync.Mutex
	next time.Time
}

func (s *spaced) Run(ctx context.Context, req claude.Request) (claude.Result, error) {
	s.mu.Lock()
	start := s.now()
	if s.next.After(start) {
		start = s.next
	}
	s.next = start.Add(s.delay)
	s.mu.Unlock()

	if wait := start.Sub(s.now()); wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return claude.Result{}, fmt.Errorf("waiting between AI calls: %w", ctx.Err())
		case <-t.C:
		}
	}
	return s.inner.Run(ctx, req)
}
