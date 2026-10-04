// Package ai picks the backend that answers the curator's calls (Claude or a local Ollama model)
// and spaces the calls out so a provider's rate limit is not hit.
package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/ollama"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/web"
)

// Runner makes one call; fetch.Runner and requests.Runner are satisfied by it.
type Runner interface {
	Run(ctx context.Context, req claude.Request) (claude.Result, error)
}

// New returns the runner for cfg.Provider, wrapped to leave cfg.CallDelay between call starts.
func New(cfg config.Config) (Runner, error) {
	var r Runner
	switch cfg.Provider {
	case config.ProviderClaude:
		r = claude.New(cfg.Claude)
	case config.ProviderOllama:
		r = ollama.New(cfg.Ollama, web.New(cfg.SearxngURL))
	default:
		return nil, fmt.Errorf("unknown CURATOR_AI_PROVIDER %q", cfg.Provider)
	}
	return Spaced(r, cfg.CallDelay), nil
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
