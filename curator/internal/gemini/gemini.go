// Package gemini runs the curator's calls on the Gemini API (Google AI Studio key) with Google
// Search and URL context as tools. It needs a Gemini 3 model: earlier ones cannot combine tools
// with structured output.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
)

const (
	// maxAttempts bounds one request on a transient server error or rate limit.
	maxAttempts  = 3
	maxErrorBody = 300
	// maxFormatRetries bounds the retries when the answer is not valid JSON for the schema.
	maxFormatRetries = 2
	// maxRetryWait caps how long a rate-limit reply may make a retry wait.
	maxRetryWait = 90 * time.Second
)

// tools maps the Claude tool names a request may ask for to Gemini's built-in tools.
var tools = map[string]string{"WebSearch": "google_search", "WebFetch": "url_context"}

// Client makes calls through the Gemini API; it satisfies fetch.Runner and requests.Runner.
type Client struct {
	cfg        config.Gemini
	http       *http.Client
	retryDelay time.Duration
	now        func() time.Time
}

// New returns a client for the API and model in cfg.
func New(cfg config.Gemini) *Client {
	return &Client{cfg: cfg, http: &http.Client{}, retryDelay: 5 * time.Second, now: time.Now}
}

// Account is the label recorded for calls to baseURL, in place of a Claude account email.
func Account(baseURL string) string {
	host := baseURL
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		host = u.Host
	}
	return "gemini (" + host + ")"
}

type part struct {
	Text    string `json:"text,omitempty"`
	Thought bool   `json:"thought,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type generationConfig struct {
	ResponseMimeType   string          `json:"responseMimeType"`
	ResponseJSONSchema json.RawMessage `json:"responseJsonSchema"`
}

type request struct {
	SystemInstruction *content          `json:"systemInstruction,omitempty"`
	Contents          []content         `json:"contents"`
	Tools             []map[string]any  `json:"tools,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
}

type response struct {
	Candidates []struct {
		Content      content `json:"content"`
		FinishReason string  `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	UsageMetadata struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
	} `json:"usageMetadata"`
}

// Run makes one call: the model researches with Google Search and URL context and answers in the
// request's schema. An answer that does not match the schema is sent back for correction a bounded
// number of times. The call, retries included, is bounded by the configured timeout.
func (c *Client) Run(ctx context.Context, req claude.Request) (claude.Result, error) {
	var toolDecls []map[string]any
	for _, name := range req.Tools {
		tool, ok := tools[name]
		if !ok {
			return claude.Result{}, fmt.Errorf("the gemini provider does not support tool %q", name)
		}
		toolDecls = append(toolDecls, map[string]any{tool: map[string]any{}})
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	body := request{
		SystemInstruction: &content{Parts: []part{{Text: fmt.Sprintf("Today is %s. You are a careful news researcher. "+
			"Search the web for real, recent sources; never invent facts or URLs. Every URL you report must come from a page you found.",
			c.now().UTC().Format("2006-01-02"))}}},
		Contents: []content{{Role: "user", Parts: []part{{Text: req.Prompt}}}},
		Tools:    toolDecls,
	}
	if req.Schema != nil {
		schema, err := geminiSchema(req.Schema)
		if err != nil {
			return claude.Result{}, fmt.Errorf("encode json schema: %w", err)
		}
		body.GenerationConfig = &generationConfig{ResponseMimeType: "application/json", ResponseJSONSchema: schema}
	}

	start := time.Now()
	var usage claude.Usage
	var lastErr error
	for turn := 1; turn <= 1+maxFormatRetries; turn++ {
		resp, err := c.generate(ctx, body)
		if err != nil {
			return claude.Result{}, err
		}
		usage.InputTokens += resp.UsageMetadata.PromptTokenCount
		usage.OutputTokens += resp.UsageMetadata.CandidatesTokenCount + resp.UsageMetadata.ThoughtsTokenCount
		usage.CacheReadInputTokens += resp.UsageMetadata.CachedContentTokenCount
		text, err := answerText(resp)
		if err != nil {
			return claude.Result{}, err
		}
		raw := json.RawMessage(stripFence(text))
		slog.DebugContext(ctx, "gemini answer", "answer", string(raw))
		if lastErr = checkAnswer(raw, req.Schema); lastErr == nil {
			return claude.Result{
				Output:   raw,
				Model:    c.cfg.Model,
				Account:  Account(c.cfg.BaseURL),
				Usage:    usage,
				Turns:    turn,
				Duration: time.Since(start),
			}, nil
		}
		slog.WarnContext(ctx, "model answer did not match the schema; retrying", "model", c.cfg.Model, "error", lastErr)
		body.Contents = append(body.Contents,
			content{Role: "model", Parts: []part{{Text: text}}},
			content{Role: "user", Parts: []part{{Text: "That answer was invalid: " + lastErr.Error() + ". Reply again with only the corrected JSON."}}})
	}
	return claude.Result{}, fmt.Errorf("model %s returned no valid structured output after %d tries: %w", c.cfg.Model, 1+maxFormatRetries, lastErr)
}

// answerText joins the first candidate's answer text (thought summaries left out). A reply that was
// blocked or cut off is an error: truncated JSON is never worth parsing.
func answerText(resp response) (string, error) {
	if reason := resp.PromptFeedback.BlockReason; reason != "" {
		return "", fmt.Errorf("gemini blocked the prompt (%s)", reason)
	}
	if len(resp.Candidates) == 0 {
		return "", errors.New("gemini returned no candidates")
	}
	cand := resp.Candidates[0]
	if cand.FinishReason != "" && cand.FinishReason != "STOP" {
		return "", fmt.Errorf("gemini stopped early (finish reason %s)", cand.FinishReason)
	}
	var b strings.Builder
	for _, p := range cand.Content.Parts {
		if !p.Thought {
			b.WriteString(p.Text)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

var fence = regexp.MustCompile("(?s)^```(?:json)?\\s*(.*?)\\s*```$")

// stripFence removes a Markdown code fence a model may wrap its JSON in.
func stripFence(s string) string {
	if m := fence.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return s
}

// checkAnswer verifies raw is a JSON object carrying every top-level key the schema requires.
func checkAnswer(raw json.RawMessage, schema map[string]any) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("not a JSON object (%w)", err)
	}
	var required []string
	switch r := schema["required"].(type) {
	case []string:
		required = r
	case []any:
		for _, v := range r {
			if s, ok := v.(string); ok {
				required = append(required, s)
			}
		}
	}
	for _, key := range required {
		if _, ok := obj[key]; !ok {
			return fmt.Errorf("missing required key %q", key)
		}
	}
	return nil
}

// generate posts one request, retrying transient failures.
func (c *Client) generate(ctx context.Context, req request) (response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return response{}, fmt.Errorf("encode gemini request: %w", err)
	}

	var lastErr error
	wait := time.Duration(0)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return response{}, fmt.Errorf("gemini call timed out after %s (raise GEMINI_TIMEOUT): %w", c.cfg.Timeout, ctx.Err())
			case <-time.After(max(wait, c.retryDelay*time.Duration(attempt-1))):
			}
		}
		resp, retryAfter, retry, err := c.post(ctx, body)
		if err == nil {
			return resp, nil
		}
		lastErr, wait = err, min(retryAfter, maxRetryWait)
		if !retry {
			break
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return response{}, fmt.Errorf("gemini call timed out after %s (raise GEMINI_TIMEOUT): %w", c.cfg.Timeout, lastErr)
	}
	return response{}, lastErr
}

// post makes one request; retry reports whether the failure may be transient, and retryAfter how
// long the API asked to wait.
func (c *Client) post(ctx context.Context, body []byte) (resp response, retryAfter time.Duration, retry bool, err error) {
	endpoint := c.cfg.BaseURL + "/models/" + url.PathEscape(c.cfg.Model) + ":generateContent"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return response{}, 0, false, fmt.Errorf("build gemini request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// A header, not a query parameter, so the key never lands in a logged URL or error.
	httpReq.Header.Set("x-goog-api-key", c.cfg.APIKey)
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return response{}, 0, ctx.Err() == nil, fmt.Errorf("call gemini API at %s: %w", c.cfg.BaseURL, err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return response{}, 0, ctx.Err() == nil, fmt.Errorf("read gemini response: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if len(msg) > maxErrorBody {
			msg = msg[:maxErrorBody]
		}
		hint := ""
		switch httpResp.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
			hint = " (check GEMINI_API_KEY and that GEMINI_MODEL is a Gemini 3 model your key can use)"
		case http.StatusNotFound:
			hint = fmt.Sprintf(" (check GEMINI_MODEL %s)", c.cfg.Model)
		case http.StatusTooManyRequests:
			hint = " (free-tier quota; lower CURATOR_CONCURRENCY, raise CURATOR_CALL_DELAY, or enable billing)"
		}
		return response{}, retryDelay(data), httpResp.StatusCode >= 500 || httpResp.StatusCode == http.StatusTooManyRequests,
			fmt.Errorf("gemini API returned HTTP %d%s: %s", httpResp.StatusCode, hint, msg)
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return response{}, 0, false, fmt.Errorf("decode gemini response: %w", err)
	}
	return resp, 0, false, nil
}

// retryDelay reads the wait a rate-limit error asks for (a RetryInfo detail such as "23s"), or zero.
func retryDelay(errBody []byte) time.Duration {
	var e struct {
		Error struct {
			Details []struct {
				RetryDelay string `json:"retryDelay"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(errBody, &e) != nil {
		return 0
	}
	for _, d := range e.Error.Details {
		if d.RetryDelay == "" {
			continue
		}
		if dur, err := time.ParseDuration(d.RetryDelay); err == nil {
			return dur
		}
		if secs, err := strconv.ParseFloat(strings.TrimSuffix(d.RetryDelay, "s"), 64); err == nil {
			return time.Duration(secs * float64(time.Second))
		}
	}
	return 0
}

// geminiSchema encodes schema for Gemini, whose schemas reject non-string enums. A numeric enum
// becomes minimum/maximum when its values are consecutive integers, or else a note in the
// description. The curator still validates every value.
func geminiSchema(schema map[string]any) ([]byte, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	dropNumericEnums(tree)
	return json.Marshal(tree)
}

func dropNumericEnums(node any) {
	switch n := node.(type) {
	case map[string]any:
		if values, ok := n["enum"].([]any); ok && len(values) > 0 {
			if nums, ok := numbers(values); ok {
				delete(n, "enum")
				if consecutive(nums) {
					n["minimum"], n["maximum"] = nums[0], nums[len(nums)-1]
				} else {
					note := "One of: " + joinNumbers(nums) + "."
					if d, _ := n["description"].(string); d != "" {
						note = d + " " + note
					}
					n["description"] = note
				}
			}
		}
		for _, child := range n {
			dropNumericEnums(child)
		}
	case []any:
		for _, child := range n {
			dropNumericEnums(child)
		}
	}
}

// numbers returns values as numbers, or false when any is not one.
func numbers(values []any) ([]float64, bool) {
	nums := make([]float64, len(values))
	for i, v := range values {
		f, ok := v.(float64)
		if !ok {
			return nil, false
		}
		nums[i] = f
	}
	return nums, true
}

func consecutive(nums []float64) bool {
	for i := 1; i < len(nums); i++ {
		if nums[i] != nums[i-1]+1 {
			return false
		}
	}
	return true
}

func joinNumbers(nums []float64) string {
	parts := make([]string, len(nums))
	for i, f := range nums {
		parts[i] = strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strings.Join(parts, ", ")
}
