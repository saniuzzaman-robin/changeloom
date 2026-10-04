package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
)

type stub struct {
	mu     sync.Mutex
	starts []time.Time
}

func (s *stub) Run(context.Context, claude.Request) (claude.Result, error) {
	s.mu.Lock()
	s.starts = append(s.starts, time.Now())
	s.mu.Unlock()
	return claude.Result{}, nil
}

func TestSpacedKeepsGapBetweenCalls(t *testing.T) {
	inner := &stub{}
	r := Spaced(inner, 50*time.Millisecond)

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.Run(context.Background(), claude.Request{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if len(inner.starts) != 3 {
		t.Fatalf("calls = %d, want 3", len(inner.starts))
	}
	first, last := inner.starts[0], inner.starts[0]
	for _, s := range inner.starts {
		if s.Before(first) {
			first = s
		}
		if s.After(last) {
			last = s
		}
	}
	if got := last.Sub(first); got < 90*time.Millisecond {
		t.Fatalf("three calls started within %s, want at least two 50ms gaps", got)
	}
}

func TestSpacedHonorsCancellation(t *testing.T) {
	r := Spaced(&stub{}, time.Hour)
	if _, err := r.Run(context.Background(), claude.Request{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := r.Run(ctx, claude.Request{}); err == nil {
		t.Fatal("want a context error while waiting for the gap")
	}
}

func TestSpacedZeroDelayReturnsInner(t *testing.T) {
	inner := &stub{}
	if Spaced(inner, 0) != Runner(inner) {
		t.Fatal("zero delay should not wrap the runner")
	}
}

func TestNewRejectsUnknownProvider(t *testing.T) {
	if _, err := New(config.Config{Provider: "gpt"}); err == nil {
		t.Fatal("want an error")
	}
	if _, err := New(config.Config{Provider: config.ProviderOllama}); err != nil {
		t.Fatal(err)
	}
}
