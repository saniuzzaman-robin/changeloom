// Package claude runs one-shot `claude -p` calls with structured JSON output, using the Claude
// subscription the CLI is logged in with.
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
)

// maxErrorOutput caps how much CLI output is quoted in an error.
const maxErrorOutput = 2000

// Client runs the claude CLI.
type Client struct {
	cfg config.Claude
}

// New returns a client for the CLI and model in cfg.
func New(cfg config.Claude) *Client {
	return &Client{cfg: cfg}
}

// Request is one call.
type Request struct {
	// Prompt is sent on stdin.
	Prompt string
	// Schema is the JSON schema the answer must match (--json-schema).
	Schema map[string]any
	// Tools are the only built-in tools available, all pre-approved (e.g. WebSearch, WebFetch).
	Tools []string
}

// Result is a successful call's answer.
type Result struct {
	// Output is the structured answer matching Request.Schema.
	Output json.RawMessage
	// Model is the model that produced most of the output, or the configured model.
	Model   string
	CostUSD float64
	Usage   Usage
	Turns   int
	// Duration is the wall time of the call.
	Duration time.Duration
}

// Usage is the token usage reported by the CLI.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// envelope is the CLI's --output-format json result.
type envelope struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	TotalCostUSD     float64         `json:"total_cost_usd"`
	NumTurns         int             `json:"num_turns"`
	Usage            Usage           `json:"usage"`
	ModelUsage       map[string]struct {
		OutputTokens int `json:"outputTokens"`
	} `json:"modelUsage"`
}

// Run makes one call, bounded by the configured timeout.
func (c *Client) Run(ctx context.Context, req Request) (Result, error) {
	schema, err := json.Marshal(req.Schema)
	if err != nil {
		return Result{}, fmt.Errorf("encode json schema: %w", err)
	}
	tools := strings.Join(req.Tools, ",")
	args := []string{
		"-p",
		"--output-format", "json",
		"--json-schema", string(schema),
		"--tools", tools,
		"--model", c.cfg.Model,
		"--no-session-persistence",
		// No MCP servers: the call needs only the built-in tools above.
		"--strict-mcp-config",
	}
	if tools != "" {
		args = append(args, "--allowedTools", tools)
	}

	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.cfg.Bin, args...) //nolint:gosec // binary and args come from local config
	// Run outside the repo so no project CLAUDE.md or settings leak into the call.
	cmd.Dir = os.TempDir()
	if c.cfg.ConfigDir != "" {
		// Later duplicates win, so this overrides any CLAUDE_CONFIG_DIR inherited from the shell.
		cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+c.cfg.ConfigDir)
	}
	cmd.Stdin = strings.NewReader(req.Prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	if ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Result{}, fmt.Errorf("claude call timed out after %s (raise CLAUDE_TIMEOUT)", c.cfg.Timeout)
	}
	if runErr != nil {
		var execErr *exec.Error
		// A bare name not on PATH is an *exec.Error; a missing path fails at fork/exec.
		if errors.As(runErr, &execErr) || errors.Is(runErr, fs.ErrNotExist) || errors.Is(runErr, fs.ErrPermission) {
			return Result{}, fmt.Errorf("run %q (install Claude Code or set CLAUDE_BIN): %w", c.cfg.Bin, runErr)
		}
	}

	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil || env.Type != "result" {
		out := clip(strings.TrimSpace(stderr.String() + "\n" + stdout.String()))
		if runErr != nil {
			return Result{}, fmt.Errorf("claude failed (%w)%s: %s", runErr, authHint(out), out)
		}
		return Result{}, fmt.Errorf("claude returned no result envelope: %s", out)
	}
	if env.IsError || runErr != nil {
		msg := env.Result
		if msg == "" {
			msg = strings.TrimSpace(stderr.String())
		}
		return Result{}, fmt.Errorf("claude call failed (%s)%s: %s", env.Subtype, authHint(msg), clip(msg))
	}
	if len(env.StructuredOutput) == 0 || string(env.StructuredOutput) == "null" {
		return Result{}, fmt.Errorf("claude returned no structured output (%s): %s", env.Subtype, clip(env.Result))
	}
	res := Result{
		Output:   env.StructuredOutput,
		Model:    mainModel(env, c.cfg.Model),
		CostUSD:  env.TotalCostUSD,
		Usage:    env.Usage,
		Turns:    env.NumTurns,
		Duration: time.Since(start),
	}
	return res, nil
}

// mainModel is the model with the most output tokens; side calls (such as fetch summaries) may
// use a smaller one.
func mainModel(env envelope, fallback string) string {
	best, most := fallback, -1
	for name, u := range env.ModelUsage {
		if u.OutputTokens > most || (u.OutputTokens == most && name < best) {
			best, most = name, u.OutputTokens
		}
	}
	return best
}

// authHint adds a next step when the CLI looks logged out.
func authHint(out string) string {
	lower := strings.ToLower(out)
	for _, s := range []string{"login", "log in", "not authenticated", "api key", "unauthorized", "oauth"} {
		if strings.Contains(lower, s) {
			return " (run `claude` once to log in)"
		}
	}
	return ""
}

func clip(s string) string {
	if len(s) > maxErrorOutput {
		return s[:maxErrorOutput] + "…"
	}
	return s
}
