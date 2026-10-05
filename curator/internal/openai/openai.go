// Package openai runs the curator's calls on any OpenAI-compatible /chat/completions API (OpenAI,
// OpenRouter, Groq, Perplexity, ...). The curator gives the model no tools: it needs a model that
// searches the web on its own, such as an OpenRouter ":online" model or a Perplexity Sonar model.
package openai

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
	"strings"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
)

const (
	// maxAttempts bounds one chat request on a transient server error or rate limit.
	maxAttempts  = 3
	maxErrorBody = 300
	// maxFormatRetries bounds the retries when the answer is not valid JSON for the schema.
	maxFormatRetries = 2
)

// webTools are Claude's built-in tools a request may ask for; the model's own web search stands in
// for them.
var webTools = map[string]bool{"WebSearch": true, "WebFetch": true}

// Client makes calls through an OpenAI-compatible API; it satisfies fetch.Runner and
// requests.Runner.
type Client struct {
	cfg        config.OpenAI
	http       *http.Client
	retryDelay time.Duration
	now        func() time.Time
}

// New returns a client for the API and model in cfg.
func New(cfg config.OpenAI) *Client {
	return &Client{cfg: cfg, http: &http.Client{}, retryDelay: 5 * time.Second, now: time.Now}
}

// Account is the label recorded for calls to baseURL, in place of a Claude account email.
func Account(baseURL string) string {
	host := baseURL
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		host = u.Host
	}
	return "openai (" + host + ")"
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type       string `json:"type"`
	JSONSchema struct {
		Name   string         `json:"name"`
		Schema map[string]any `json:"schema"`
		// Strict stays off: strict mode needs every property required and no additional ones,
		// which the curator's schemas do not promise. checkAnswer checks the answer instead.
		Strict bool `json:"strict"`
	} `json:"json_schema"`
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []message       `json:"messages"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Run makes one call: the model researches on its own and answers in the request's schema. An
// answer that does not match the schema is sent back for correction a bounded number of times.
// The call, retries included, is bounded by the configured timeout.
func (c *Client) Run(ctx context.Context, req claude.Request) (claude.Result, error) {
	for _, name := range req.Tools {
		if !webTools[name] {
			return claude.Result{}, fmt.Errorf("the openai provider does not support tool %q", name)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	start := time.Now()
	var usage claude.Usage
	chat := chatRequest{Model: c.cfg.Model, Messages: []message{
		{Role: "system", Content: fmt.Sprintf("Today is %s. You are a careful news researcher. Search the web for real, recent "+
			"sources; never invent facts or URLs. Every URL you report must come from a page you found.",
			c.now().UTC().Format("2006-01-02"))},
		{Role: "user", Content: req.Prompt},
	}}
	if req.Schema != nil {
		chat.ResponseFormat = &responseFormat{Type: "json_schema"}
		chat.ResponseFormat.JSONSchema.Name, chat.ResponseFormat.JSONSchema.Schema = "answer", req.Schema
	}

	var lastErr error
	for turn := 1; turn <= 1+maxFormatRetries; turn++ {
		resp, err := c.chat(ctx, chat)
		if err != nil {
			return claude.Result{}, err
		}
		usage.InputTokens += resp.Usage.PromptTokens
		usage.OutputTokens += resp.Usage.CompletionTokens
		if len(resp.Choices) == 0 {
			return claude.Result{}, errors.New("openai-compatible API returned no choices")
		}
		content := resp.Choices[0].Message.Content
		raw := json.RawMessage(strings.TrimSpace(content))
		slog.DebugContext(ctx, "openai answer", "answer", string(raw))
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
		chat.Messages = append(chat.Messages, message{Role: "assistant", Content: content},
			message{Role: "user", Content: "That answer was invalid: " + lastErr.Error() + ". Reply again with only the corrected JSON."})
	}
	return claude.Result{}, fmt.Errorf("model %s returned no valid structured output after %d tries: %w", c.cfg.Model, 1+maxFormatRetries, lastErr)
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

// chat posts one request, retrying transient failures.
func (c *Client) chat(ctx context.Context, req chatRequest) (chatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return chatResponse{}, fmt.Errorf("encode openai request: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return chatResponse{}, fmt.Errorf("openai call timed out after %s (raise OPENAI_TIMEOUT): %w", c.cfg.Timeout, ctx.Err())
			case <-time.After(c.retryDelay * time.Duration(attempt-1)):
			}
		}
		resp, retry, err := c.post(ctx, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return chatResponse{}, fmt.Errorf("openai call timed out after %s (raise OPENAI_TIMEOUT): %w", c.cfg.Timeout, lastErr)
	}
	return chatResponse{}, lastErr
}

// post makes one request; retry reports whether the failure may be transient.
func (c *Client) post(ctx context.Context, body []byte) (resp chatResponse, retry bool, err error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return chatResponse{}, false, fmt.Errorf("build openai request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return chatResponse{}, ctx.Err() == nil, fmt.Errorf("call openai-compatible API at %s: %w", c.cfg.BaseURL, err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return chatResponse{}, ctx.Err() == nil, fmt.Errorf("read openai response: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if len(msg) > maxErrorBody {
			msg = msg[:maxErrorBody]
		}
		hint := ""
		switch httpResp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			hint = " (check OPENAI_API_KEY)"
		case http.StatusNotFound:
			hint = fmt.Sprintf(" (check OPENAI_BASE_URL %s and OPENAI_MODEL %s)", c.cfg.BaseURL, c.cfg.Model)
		}
		return chatResponse{}, httpResp.StatusCode >= 500 || httpResp.StatusCode == http.StatusTooManyRequests,
			fmt.Errorf("openai-compatible API returned HTTP %d%s: %s", httpResp.StatusCode, hint, msg)
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return chatResponse{}, false, fmt.Errorf("decode openai response: %w", err)
	}
	return resp, false, nil
}
