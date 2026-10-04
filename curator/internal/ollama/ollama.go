// Package ollama runs the curator's calls on a local open-source model through Ollama's chat API,
// giving it web search and page fetching through SearXNG (package web).
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/web"
)

const (
	// Account is the label recorded for calls, in place of a Claude account email.
	Account = "ollama (local)"

	// maxAttempts bounds one chat request on a transient server error.
	maxAttempts = 3
	// maxFormatRetries bounds the retries when the final answer is not valid JSON for the schema.
	maxFormatRetries = 2
	keepAlive        = "30m"
	temperature      = 0.2
	maxErrorBody     = 300
	maxToolResult    = 16000

	// Claude's built-in tool names, which callers put in claude.Request.Tools.
	toolWebSearch = "WebSearch"
	toolWebFetch  = "WebFetch"
)

// Web is the browsing the model is given.
type Web interface {
	Search(ctx context.Context, query string) ([]web.SearchResult, error)
	FetchPage(ctx context.Context, rawURL string) (string, error)
}

// Client makes calls through Ollama.
type Client struct {
	cfg        config.Ollama
	web        Web
	http       *http.Client
	now        func() time.Time
	retryDelay time.Duration
}

// New returns a client for the Ollama server and model in cfg.
func New(cfg config.Ollama, w Web) *Client {
	return &Client{cfg: cfg, web: w, http: &http.Client{}, now: time.Now, retryDelay: 5 * time.Second}
}

type message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"`
}

type toolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

type chatRequest struct {
	Model     string         `json:"model"`
	Messages  []message      `json:"messages"`
	Tools     []toolDef      `json:"tools,omitempty"`
	Format    map[string]any `json:"format,omitempty"`
	Stream    bool           `json:"stream"`
	KeepAlive string         `json:"keep_alive"`
	Options   map[string]any `json:"options"`
}

type chatResponse struct {
	Message         message `json:"message"`
	PromptEvalCount int     `json:"prompt_eval_count"`
	EvalCount       int     `json:"eval_count"`
	Error           string  `json:"error"`
}

type toolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

func defineTool(name, description, param, paramDescription string) toolDef {
	var d toolDef
	d.Type = "function"
	d.Function.Name = name
	d.Function.Description = description
	d.Function.Parameters = map[string]any{
		"type":       "object",
		"properties": map[string]any{param: map[string]any{"type": "string", "description": paramDescription}},
		"required":   []string{param},
	}
	return d
}

// toolDefs maps the Claude tool names a request asks for to Ollama tools.
func toolDefs(names []string) ([]toolDef, error) {
	var defs []toolDef
	for _, name := range names {
		switch name {
		case toolWebSearch:
			defs = append(defs, defineTool("web_search", "Search the web. Returns titles, URLs and snippets.", "query", "the search query"))
		case toolWebFetch:
			defs = append(defs, defineTool("web_fetch", "Fetch a web page and return its readable text.", "url", "an absolute http(s) URL taken from a search result"))
		default:
			return nil, fmt.Errorf("ollama provider does not support tool %q", name)
		}
	}
	return defs, nil
}

// Run makes one call: the model researches with its tools, then answers in the request's schema.
// The call, tool turns included, is bounded by the configured timeout.
func (c *Client) Run(ctx context.Context, req claude.Request) (claude.Result, error) {
	tools, err := toolDefs(req.Tools)
	if err != nil {
		return claude.Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	start := time.Now()
	var usage claude.Usage
	turns := 0
	msgs := []message{
		{Role: "system", Content: fmt.Sprintf("Today is %s. You are a careful news researcher. Use the tools to find real, recent "+
			"sources; never invent facts or URLs. Every URL you report must come from a search result or page you opened.",
			c.now().UTC().Format("2006-01-02"))},
		{Role: "user", Content: req.Prompt},
	}

	// Research: tool calls cannot be combined with a constrained answer format, so the schema is
	// applied in a separate final call.
	for turn := 0; len(tools) > 0 && turn < c.cfg.MaxTurns; turn++ {
		resp, err := c.chat(ctx, chatRequest{Model: c.cfg.Model, Messages: msgs, Tools: tools}, &usage)
		if err != nil {
			return claude.Result{}, err
		}
		turns++
		msgs = append(msgs, resp.Message)
		if len(resp.Message.ToolCalls) == 0 {
			break
		}
		for _, tc := range resp.Message.ToolCalls {
			msgs = append(msgs, message{Role: "tool", ToolName: tc.Function.Name, Content: c.runTool(ctx, tc)})
		}
	}

	msgs = append(msgs, message{Role: "user", Content: "Now give your final answer as JSON matching the required schema, " +
		"using only what you found above. If you found nothing usable, return an empty list instead of guessing."})
	var lastErr error
	for range 1 + maxFormatRetries {
		resp, err := c.chat(ctx, chatRequest{Model: c.cfg.Model, Messages: msgs, Format: req.Schema}, &usage)
		if err != nil {
			return claude.Result{}, err
		}
		turns++
		raw := json.RawMessage(strings.TrimSpace(resp.Message.Content))
		if lastErr = checkAnswer(raw, req.Schema); lastErr == nil {
			return claude.Result{
				Output:   raw,
				Model:    c.cfg.Model,
				Account:  Account,
				Usage:    usage,
				Turns:    turns,
				Duration: time.Since(start),
			}, nil
		}
		slog.WarnContext(ctx, "ollama answer did not match the schema; retrying", "error", lastErr)
		msgs = append(msgs, resp.Message, message{Role: "user", Content: "That answer was invalid: " + lastErr.Error() + ". Reply again with only the corrected JSON."})
	}
	return claude.Result{}, fmt.Errorf("ollama model %s returned no valid structured output after %d tries: %w", c.cfg.Model, 1+maxFormatRetries, lastErr)
}

// runTool executes one tool call. Failures go back to the model as text so it can try something else.
func (c *Client) runTool(ctx context.Context, tc toolCall) string {
	arg := func(key string) string {
		s, _ := tc.Function.Arguments[key].(string)
		return s
	}
	var out string
	var err error
	switch tc.Function.Name {
	case "web_search":
		var results []web.SearchResult
		if results, err = c.web.Search(ctx, arg("query")); err == nil {
			var b []byte
			if b, err = json.Marshal(results); err == nil {
				out = string(b)
			}
		}
	case "web_fetch":
		out, err = c.web.FetchPage(ctx, arg("url"))
	default:
		err = fmt.Errorf("unknown tool %q", tc.Function.Name)
	}
	if err != nil {
		slog.DebugContext(ctx, "ollama tool call failed", "tool", tc.Function.Name, "error", err)
		return "error: " + err.Error()
	}
	if r := []rune(out); len(r) > maxToolResult {
		out = string(r[:maxToolResult])
	}
	return out
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

// chat posts one request, retrying transient failures, and adds the token counts to usage.
func (c *Client) chat(ctx context.Context, req chatRequest, usage *claude.Usage) (chatResponse, error) {
	req.Stream = false
	req.KeepAlive = keepAlive
	req.Options = map[string]any{"temperature": temperature, "num_ctx": c.cfg.ContextTokens}
	body, err := json.Marshal(req)
	if err != nil {
		return chatResponse{}, fmt.Errorf("encode ollama request: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return chatResponse{}, fmt.Errorf("ollama call timed out after %s (raise OLLAMA_TIMEOUT): %w", c.cfg.Timeout, ctx.Err())
			case <-time.After(c.retryDelay * time.Duration(attempt-1)):
			}
		}
		resp, retry, err := c.post(ctx, body)
		if err == nil {
			usage.InputTokens += resp.PromptEvalCount
			usage.OutputTokens += resp.EvalCount
			return resp, nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return chatResponse{}, fmt.Errorf("ollama call timed out after %s (raise OLLAMA_TIMEOUT): %w", c.cfg.Timeout, lastErr)
	}
	return chatResponse{}, lastErr
}

// post makes one request; retry reports whether the failure may be transient.
func (c *Client) post(ctx context.Context, body []byte) (resp chatResponse, retry bool, err error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return chatResponse{}, false, fmt.Errorf("build ollama request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return chatResponse{}, ctx.Err() == nil, fmt.Errorf("call ollama at %s (is it running? `ollama serve`): %w", c.cfg.URL, err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return chatResponse{}, ctx.Err() == nil, fmt.Errorf("read ollama response: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if len(msg) > maxErrorBody {
			msg = msg[:maxErrorBody]
		}
		hint := ""
		if httpResp.StatusCode == http.StatusNotFound {
			hint = fmt.Sprintf(" (pull the model: ollama pull %s)", c.cfg.Model)
		}
		return chatResponse{}, httpResp.StatusCode >= 500 || httpResp.StatusCode == http.StatusTooManyRequests,
			fmt.Errorf("ollama returned HTTP %d%s: %s", httpResp.StatusCode, hint, msg)
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return chatResponse{}, false, fmt.Errorf("decode ollama response: %w", err)
	}
	if resp.Error != "" {
		return chatResponse{}, false, fmt.Errorf("ollama error: %s", resp.Error)
	}
	return resp, false, nil
}
