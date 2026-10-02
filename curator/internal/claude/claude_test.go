package claude_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
)

// fakeCLI writes a stand-in claude binary that saves its args and stdin, prints out and exits
// with code.
func fakeCLI(t *testing.T, out string, code int) (bin, dir string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "out.json"), []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		`printf '%s\n' "$@" > "` + dir + `/args"` + "\n" +
		`cat > "` + dir + `/stdin"` + "\n" +
		`cat "` + dir + `/out.json"` + "\n" +
		"exit " + strconv.Itoa(code) + "\n"
	bin = filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil { //nolint:gosec // test executable
		t.Fatal(err)
	}
	return bin, dir
}

func client(bin string) *claude.Client {
	return claude.New(config.Claude{Bin: bin, Model: "sonnet", Timeout: 10 * time.Second})
}

func TestRunParsesStructuredOutput(t *testing.T) {
	bin, dir := fakeCLI(t, `{"type":"result","subtype":"success","is_error":false,"result":"{}",
		"structured_output":{"stories":[]},"total_cost_usd":0.25,"num_turns":3,
		"usage":{"input_tokens":10,"output_tokens":20},
		"modelUsage":{"claude-haiku-4-5":{"outputTokens":5},"claude-sonnet-5-5":{"outputTokens":900}}}`, 0)

	res, err := client(bin).Run(t.Context(), claude.Request{
		Prompt: "find news",
		Schema: map[string]any{"type": "object"},
		Tools:  []string{"WebSearch", "WebFetch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Output) != `{"stories":[]}` || res.CostUSD != 0.25 || res.Turns != 3 ||
		res.Model != "claude-sonnet-5-5" || res.Usage.OutputTokens != 20 {
		t.Fatalf("unexpected result %+v", res)
	}

	stdin, _ := os.ReadFile(filepath.Join(dir, "stdin")) //nolint:gosec // test temp dir
	if string(stdin) != "find news" {
		t.Errorf("stdin = %q", stdin)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args")) //nolint:gosec // test temp dir
	for _, want := range []string{"-p", "--output-format\njson", `--json-schema
{"type":"object"}`, "--tools\nWebSearch,WebFetch", "--allowedTools\nWebSearch,WebFetch", "--model\nsonnet", "--no-session-persistence"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("args %q missing %q", args, want)
		}
	}
}

func TestRunErrors(t *testing.T) {
	tests := []struct {
		name, out string
		code      int
		want      string
	}{
		{"logged out", `{"type":"result","subtype":"success","is_error":true,"result":"Invalid API key · Please run /login"}`, 1, "run `claude` once to log in"},
		{"error subtype", `{"type":"result","subtype":"error_max_turns","is_error":true,"result":""}`, 1, "error_max_turns"},
		{"no envelope", "boom", 1, "claude failed"},
		{"no structured output", `{"type":"result","subtype":"success","is_error":false,"result":"sorry"}`, 0, "no structured output"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bin, _ := fakeCLI(t, tc.out, tc.code)
			_, err := client(bin).Run(t.Context(), claude.Request{Prompt: "x", Schema: map[string]any{}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestRunMissingBinary(t *testing.T) {
	_, err := client(filepath.Join(t.TempDir(), "nope")).Run(t.Context(), claude.Request{Schema: map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "CLAUDE_BIN") {
		t.Fatalf("err = %v", err)
	}
}
