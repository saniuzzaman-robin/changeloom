package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
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

// Batch request outcomes, matching the Message Batches API result types.
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

// Client is the subset of the Anthropic API the pipeline uses.
type Client interface {
	SubmitBatch(ctx context.Context, reqs []Request) (batchID string, err error)
	GetBatch(ctx context.Context, batchID string) (BatchStatus, error)
	BatchResults(ctx context.Context, batchID string) ([]Result, error)
	// Complete runs one request synchronously at full price (prompt tuning only).
	Complete(ctx context.Context, req Request) (Result, error)
}

// SearchRequest is one web-search-enabled request. Unlike Request it has no output schema:
// structured outputs do not combine with the citations web search adds, so the prompt asks
// for JSON and the caller parses it.
type SearchRequest struct {
	System string
	User   string
	// MaxSearches caps web_search tool uses in the request.
	MaxSearches int64
}

// Searcher runs a request with Claude's web search tool.
type Searcher interface {
	Search(ctx context.Context, req SearchRequest) (Result, error)
}

// AnthropicClient implements Client with anthropic-sdk-go.
type AnthropicClient struct {
	c         anthropic.Client
	model     string
	effort    string
	maxTokens int64
}

// NewAnthropicClient creates a client for the given model. effort is an output_config
// effort level. Thinking is left at the model default; effort controls its depth.
func NewAnthropicClient(apiKey, model, effort string, maxTokens int64) (*AnthropicClient, error) {
	if apiKey == "" || model == "" {
		return nil, errors.New("anthropic client needs an API key and a model")
	}
	return &AnthropicClient{
		c:         anthropic.NewClient(option.WithAPIKey(apiKey), option.WithRequestTimeout(requestTimeout)),
		model:     model,
		effort:    effort,
		maxTokens: maxTokens,
	}, nil
}

func (a *AnthropicClient) system(text string) []anthropic.TextBlockParam {
	return []anthropic.TextBlockParam{{Text: text, CacheControl: anthropic.NewCacheControlEphemeralParam()}}
}

func (a *AnthropicClient) outputConfig(schema map[string]any) anthropic.OutputConfigParam {
	return anthropic.OutputConfigParam{
		Effort: anthropic.OutputConfigEffort(a.effort),
		Format: anthropic.JSONOutputFormatParam{Schema: schema},
	}
}

// SubmitBatch implements Client.
func (a *AnthropicClient) SubmitBatch(ctx context.Context, reqs []Request) (string, error) {
	items := make([]anthropic.MessageBatchNewParamsRequest, len(reqs))
	for i, r := range reqs {
		items[i] = anthropic.MessageBatchNewParamsRequest{
			CustomID: r.CustomID,
			Params: anthropic.MessageBatchNewParamsRequestParams{
				Model:        a.model,
				MaxTokens:    a.maxTokens,
				System:       a.system(r.System),
				Messages:     []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(r.User))},
				OutputConfig: a.outputConfig(r.Schema),
			},
		}
	}
	batch, err := a.c.Messages.Batches.New(ctx, anthropic.MessageBatchNewParams{Requests: items})
	if err != nil {
		return "", fmt.Errorf("create message batch: %w", err)
	}
	return batch.ID, nil
}

// GetBatch implements Client.
func (a *AnthropicClient) GetBatch(ctx context.Context, batchID string) (BatchStatus, error) {
	b, err := a.c.Messages.Batches.Get(ctx, batchID, anthropic.MessageBatchGetParams{})
	if err != nil {
		return BatchStatus{}, fmt.Errorf("get message batch %s: %w", batchID, err)
	}
	return BatchStatus{Status: string(b.ProcessingStatus), Ended: b.ProcessingStatus == anthropic.MessageBatchProcessingStatusEnded}, nil
}

// BatchResults implements Client.
func (a *AnthropicClient) BatchResults(ctx context.Context, batchID string) ([]Result, error) {
	stream := a.c.Messages.Batches.ResultsStreaming(ctx, batchID, anthropic.MessageBatchResultsParams{})
	defer func() { _ = stream.Close() }()
	var out []Result
	for stream.Next() {
		out = append(out, convertBatchResult(stream.Current()))
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("read results of message batch %s: %w", batchID, err)
	}
	return out, nil
}

func convertBatchResult(r anthropic.MessageBatchIndividualResponse) Result {
	res := Result{CustomID: r.CustomID}
	switch v := r.Result.AsAny().(type) {
	case anthropic.MessageBatchSucceededResult:
		res = convertMessage(v.Message)
		res.CustomID = r.CustomID
	case anthropic.MessageBatchErroredResult:
		res.Outcome = OutcomeErrored
		res.Error = v.Error.Error.Message
	case anthropic.MessageBatchCanceledResult:
		res.Outcome = OutcomeCanceled
		res.Error = "batch request canceled"
	case anthropic.MessageBatchExpiredResult:
		res.Outcome = OutcomeExpired
		res.Error = "batch request expired before processing"
	default:
		res.Outcome = OutcomeErrored
		res.Error = fmt.Sprintf("unknown batch result type %q", r.Result.Type)
	}
	return res
}

func convertMessage(m anthropic.Message) Result {
	var text strings.Builder
	for _, block := range m.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}
	res := Result{
		Outcome:    OutcomeSucceeded,
		Text:       text.String(),
		StopReason: string(m.StopReason),
		Usage: Usage{
			Input:         m.Usage.InputTokens,
			Output:        m.Usage.OutputTokens,
			CacheRead:     m.Usage.CacheReadInputTokens,
			CacheCreation: m.Usage.CacheCreationInputTokens,
		},
	}
	if m.StopReason == anthropic.StopReasonRefusal {
		res.RefusalCategory = string(m.StopDetails.Category)
	}
	return res
}

// Complete implements Client.
func (a *AnthropicClient) Complete(ctx context.Context, req Request) (Result, error) {
	msg, err := a.c.Messages.New(ctx, anthropic.MessageNewParams{
		Model:        a.model,
		MaxTokens:    a.maxTokens,
		System:       a.system(req.System),
		Messages:     []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.User))},
		OutputConfig: a.outputConfig(req.Schema),
	})
	if err != nil {
		return Result{}, fmt.Errorf("create message: %w", err)
	}
	res := convertMessage(*msg)
	res.CustomID = req.CustomID
	return res, nil
}

// maxPauseContinuations bounds how often a paused turn (long-running server tools) is resumed.
const maxPauseContinuations = 3

// Search implements Searcher. The server may pause a long turn; it is resumed up to
// maxPauseContinuations times and the usage of all calls is summed.
func (a *AnthropicClient) Search(ctx context.Context, req SearchRequest) (Result, error) {
	messages := []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.User))}
	var usage Usage
	for range maxPauseContinuations + 1 {
		msg, err := a.c.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     a.model,
			MaxTokens: a.maxTokens,
			System:    a.system(req.System),
			Messages:  messages,
			Tools: []anthropic.ToolUnionParam{{OfWebSearchTool20260209: &anthropic.WebSearchTool20260209Param{
				MaxUses: param.NewOpt(req.MaxSearches),
			}}},
			OutputConfig: anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffort(a.effort)},
		})
		if err != nil {
			return Result{}, fmt.Errorf("create message with web search: %w", err)
		}
		res := convertMessage(*msg)
		usage.Add(res.Usage)
		if msg.StopReason != anthropic.StopReasonPauseTurn {
			res.Usage = usage
			return res, nil
		}
		messages = append(messages, msg.ToParam())
	}
	return Result{}, fmt.Errorf("web search turn still paused after %d continuations", maxPauseContinuations)
}
