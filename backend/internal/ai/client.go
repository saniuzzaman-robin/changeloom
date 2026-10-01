package ai

import (
	"context"
	"time"
)

const requestTimeout = 5 * time.Minute

// Request is one item to process. System must be identical across requests so that it
// can be prompt-cached.
type Request struct {
	CustomID string
	System   string
	User     string
	Schema   map[string]any
}

// Outcome is how one request in a batch ended.
type Outcome string

// Batch request outcomes, one per batch-result type.
const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeErrored   Outcome = "errored"
	OutcomeCanceled  Outcome = "canceled"
	OutcomeExpired   Outcome = "expired"
)

// Usage counts tokens for one or more requests.
type Usage struct {
	Input, Output, CacheRead, CacheCreation int64
}

// Add accumulates another usage into u.
func (u *Usage) Add(o Usage) {
	u.Input += o.Input
	u.Output += o.Output
	u.CacheRead += o.CacheRead
	u.CacheCreation += o.CacheCreation
}

// Result is the answer to one Request.
type Result struct {
	CustomID string
	Outcome  Outcome
	// Text is the concatenated text content of a succeeded response.
	Text       string
	StopReason string
	// RefusalCategory is set when StopReason is "refusal".
	RefusalCategory string
	// Error describes an errored, canceled or expired request.
	Error string
	Usage Usage
}

// BatchStatus is the state of a submitted batch.
type BatchStatus struct {
	Status string
	Ended  bool
}

// Client is the subset of an AI provider API the pipeline uses.
type Client interface {
	SubmitBatch(ctx context.Context, reqs []Request) (batchID string, err error)
	GetBatch(ctx context.Context, batchID string) (BatchStatus, error)
	BatchResults(ctx context.Context, batchID string) ([]Result, error)
	// Complete runs one request synchronously at full price (prompt tuning only).
	Complete(ctx context.Context, req Request) (Result, error)
}

// SearchRequest is one web-search-enabled request. Unlike Request it has no output schema:
// structured outputs may not combine with web search, so the prompt asks
// for JSON and the caller parses it.
type SearchRequest struct {
	System string
	User   string
	// MaxSearches caps web search uses in the request.
	MaxSearches int64
}

// Searcher runs a request with the provider's web search tool.
type Searcher interface {
	Search(ctx context.Context, req SearchRequest) (Result, error)
}
