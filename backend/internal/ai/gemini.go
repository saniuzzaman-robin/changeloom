package ai

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/genai"
)

func init() {
	RegisterProvider("gemini", func(ctx context.Context, c ProviderConfig) (Provider, error) {
		return NewGeminiClient(ctx, c.APIKey, c.Model, c.MaxTokens)
	})
}

// GeminiClient implements Client and Searcher with google.golang.org/genai.
//
// Gemini has no Message Batches equivalent, so SubmitBatch answers every request
// synchronously and keeps the results in memory until BatchResults reads them. A batch
// unknown to this process (e.g. after a restart) reports as ended with no results, so the
// pipeline counts each of its items as a failed attempt and resubmits them.
type GeminiClient struct {
	c         *genai.Client
	model     string
	maxTokens int32

	mu      sync.Mutex
	batches map[string][]Result
	nextID  int
}

// NewGeminiClient creates a client for the given model.
func NewGeminiClient(ctx context.Context, apiKey, model string, maxTokens int64) (*GeminiClient, error) {
	if apiKey == "" || model == "" {
		return nil, errors.New("gemini client needs an API key and a model")
	}
	c, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}
	return &GeminiClient{c: c, model: model, maxTokens: int32(min(maxTokens, 1<<31-1)), batches: map[string][]Result{}}, nil //nolint:gosec // clamped
}

func (g *GeminiClient) config(system string, schema map[string]any) *genai.GenerateContentConfig {
	cfg := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(system, genai.RoleUser),
		MaxOutputTokens:   g.maxTokens,
	}
	if schema != nil {
		cfg.ResponseMIMEType = "application/json"
		cfg.ResponseJsonSchema = schema
	}
	return cfg
}

func (g *GeminiClient) generate(ctx context.Context, user string, cfg *genai.GenerateContentConfig) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	resp, err := g.c.Models.GenerateContent(ctx, g.model, genai.Text(user), cfg)
	if err != nil {
		return Result{}, fmt.Errorf("generate content: %w", err)
	}
	return convertGeminiResponse(resp), nil
}

// convertGeminiResponse maps Gemini finish/block reasons onto the stop reasons the
// pipeline checks: "refusal" and "max_tokens".
func convertGeminiResponse(resp *genai.GenerateContentResponse) Result {
	res := Result{Outcome: OutcomeSucceeded, Text: resp.Text()}
	if u := resp.UsageMetadata; u != nil {
		res.Usage = Usage{
			Input:     int64(u.PromptTokenCount - u.CachedContentTokenCount),
			Output:    int64(u.CandidatesTokenCount + u.ThoughtsTokenCount),
			CacheRead: int64(u.CachedContentTokenCount),
		}
	}
	if pf := resp.PromptFeedback; pf != nil && pf.BlockReason != "" {
		res.StopReason = "refusal"
		res.RefusalCategory = string(pf.BlockReason)
		return res
	}
	if len(resp.Candidates) == 0 {
		res.Outcome = OutcomeErrored
		res.Error = "gemini returned no candidates"
		return res
	}
	fr := resp.Candidates[0].FinishReason
	res.StopReason = string(fr)
	switch fr {
	case genai.FinishReasonMaxTokens:
		res.StopReason = "max_tokens"
	case genai.FinishReasonStop, genai.FinishReasonUnspecified:
	default:
		// SAFETY, PROHIBITED_CONTENT, BLOCKLIST, RECITATION, ... all mean no usable answer.
		res.StopReason = "refusal"
		res.RefusalCategory = string(fr)
	}
	return res
}

// SubmitBatch implements Client.
func (g *GeminiClient) SubmitBatch(ctx context.Context, reqs []Request) (string, error) {
	results := make([]Result, 0, len(reqs))
	for _, r := range reqs {
		res, err := g.generate(ctx, r.User, g.config(r.System, r.Schema))
		if err != nil {
			res = Result{Outcome: OutcomeErrored, Error: err.Error()}
		}
		res.CustomID = r.CustomID
		results = append(results, res)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nextID++
	id := fmt.Sprintf("gemini-sync-%d", g.nextID)
	g.batches[id] = results
	return id, nil
}

// GetBatch implements Client. Every known batch is already complete.
func (g *GeminiClient) GetBatch(_ context.Context, _ string) (BatchStatus, error) {
	return BatchStatus{Status: "ended", Ended: true}, nil
}

// BatchResults implements Client. Results are released once read.
func (g *GeminiClient) BatchResults(_ context.Context, batchID string) ([]Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	res := g.batches[batchID]
	delete(g.batches, batchID)
	return res, nil
}

// Complete implements Client.
func (g *GeminiClient) Complete(ctx context.Context, req Request) (Result, error) {
	res, err := g.generate(ctx, req.User, g.config(req.System, req.Schema))
	if err != nil {
		return Result{}, err
	}
	res.CustomID = req.CustomID
	return res, nil
}

// Search implements Searcher with Google Search grounding. Gemini has no per-request
// search cap, so req.MaxSearches is not enforced. Structured output is not combined with
// grounding; the prompt asks for JSON and the caller parses it.
func (g *GeminiClient) Search(ctx context.Context, req SearchRequest) (Result, error) {
	cfg := g.config(req.System, nil)
	cfg.Tools = []*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}}
	return g.generate(ctx, req.User, cfg)
}
