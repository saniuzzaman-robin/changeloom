package gemini_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/gemini"
)

// server replies with each canned response in order and records the requests.
type server struct {
	*httptest.Server
	mu       sync.Mutex
	requests []map[string]any
	keys     []string
}

type reply struct {
	status int
	body   string
}

func newServer(t *testing.T, replies ...reply) *server {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests = append(s.requests, req)
		s.keys = append(s.keys, r.Header.Get("x-goog-api-key"))
		if r.URL.Path != "/v1beta/models/m:generateContent" || r.URL.RawQuery != "" || len(s.requests) > len(replies) {
			t.Errorf("unexpected request %d to %s?%s", len(s.requests), r.URL.Path, r.URL.RawQuery)
			http.Error(w, "unexpected", http.StatusInternalServerError)
			return
		}
		rep := replies[len(s.requests)-1]
		w.WriteHeader(rep.status)
		_, _ = w.Write([]byte(rep.body))
	}))
	t.Cleanup(s.Close)
	return s
}

func client(url string) *gemini.Client {
	c := gemini.New(config.Gemini{BaseURL: url + "/v1beta", APIKey: "secret-key", Model: "m", Timeout: 5 * time.Second})
	c.SetRetryDelay(time.Millisecond)
	return c
}

func answer(text string) reply {
	body, _ := json.Marshal(map[string]any{
		"candidates": []any{map[string]any{
			"finishReason": "STOP",
			"content":      map[string]any{"parts": []any{map[string]any{"text": "thinking...", "thought": true}, map[string]any{"text": text}}},
		}},
		"usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 20, "thoughtsTokenCount": 5, "cachedContentTokenCount": 3},
	})
	return reply{http.StatusOK, string(body)}
}

var schema = map[string]any{
	"type":     "object",
	"required": []string{"stories"},
	"properties": map[string]any{
		"stories":    map[string]any{"type": "array"},
		"importance": map[string]any{"type": "integer", "enum": []int{1, 2, 3, 4, 5}},
		"priority":   map[string]any{"type": "integer", "enum": []int{1, 3}, "description": "How soon."},
		"kind":       map[string]any{"type": "string", "enum": []string{"a", "b"}},
	},
}

func TestRunSendsToolsSchemaAndKeyAndParsesTheAnswer(t *testing.T) {
	s := newServer(t, answer(`{"stories":[]}`))
	res, err := client(s.URL).Run(t.Context(), claude.Request{Prompt: "find news", Schema: schema, Tools: []string{"WebSearch", "WebFetch"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Output) != `{"stories":[]}` || res.Turns != 1 || res.Model != "m" || !strings.HasPrefix(res.Account, "gemini (") ||
		res.Usage.InputTokens != 10 || res.Usage.OutputTokens != 25 || res.Usage.CacheReadInputTokens != 3 {
		t.Fatalf("unexpected result %+v", res)
	}
	if s.keys[0] != "secret-key" {
		t.Errorf("api key header = %q", s.keys[0])
	}
	got, _ := json.Marshal(s.requests[0])
	for _, want := range []string{
		`"tools":[{"google_search":{}},{"url_context":{}}]`,
		`"responseMimeType":"application/json"`,
		`"importance":{"maximum":5,"minimum":1,"type":"integer"}`,
		`"priority":{"description":"How soon. One of: 1, 3.","type":"integer"}`,
		`"kind":{"enum":["a","b"],"type":"string"}`,
		`"text":"find news"`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("request %s missing %s", got, want)
		}
	}
	if _, ok := schema["properties"].(map[string]any)["importance"].(map[string]any)["enum"]; !ok {
		t.Error("the caller's schema was changed")
	}
}

func TestRunStripsACodeFence(t *testing.T) {
	s := newServer(t, answer("```json\n{\"stories\":[]}\n```"))
	res, err := client(s.URL).Run(t.Context(), claude.Request{Prompt: "x", Schema: schema})
	if err != nil || string(res.Output) != `{"stories":[]}` {
		t.Fatalf("res %q, err %v", res.Output, err)
	}
}

func TestRunSendsAnInvalidAnswerBackOnce(t *testing.T) {
	s := newServer(t, answer(`{"nope":1}`), answer(`{"stories":[]}`))
	res, err := client(s.URL).Run(t.Context(), claude.Request{Prompt: "x", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if res.Turns != 2 || res.Usage.InputTokens != 20 {
		t.Fatalf("turns %d, usage %+v", res.Turns, res.Usage)
	}
	retry, _ := json.Marshal(s.requests[1]["contents"])
	if !strings.Contains(string(retry), `missing required key \"stories\"`) {
		t.Errorf("retry contents %s", retry)
	}
}

func TestRunGivesUpAfterBoundedFormatRetries(t *testing.T) {
	s := newServer(t, answer("nope"), answer("nope"), answer("nope"))
	_, err := client(s.URL).Run(t.Context(), claude.Request{Prompt: "x", Schema: schema})
	if err == nil || !strings.Contains(err.Error(), "no valid structured output after 3 tries") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunRetriesARateLimitThenSucceeds(t *testing.T) {
	limited := reply{http.StatusTooManyRequests, `{"error":{"code":429,"details":[{"retryDelay":"0.001s"}]}}`}
	s := newServer(t, limited, answer(`{"stories":[]}`))
	if _, err := client(s.URL).Run(t.Context(), claude.Request{Prompt: "x", Schema: schema}); err != nil {
		t.Fatal(err)
	}
	if len(s.requests) != 2 {
		t.Errorf("requests = %d", len(s.requests))
	}
}

func TestRunErrors(t *testing.T) {
	tests := []struct {
		name  string
		reply reply
		calls int
		want  string
	}{
		{"bad key", reply{http.StatusForbidden, `{"error":{"message":"API key not valid"}}`}, 1, "check GEMINI_API_KEY"},
		{"unknown model", reply{http.StatusNotFound, `{}`}, 1, "check GEMINI_MODEL m"},
		{"quota", reply{http.StatusTooManyRequests, `{}`}, 3, "free-tier quota"},
		{"cut off", reply{http.StatusOK, `{"candidates":[{"finishReason":"MAX_TOKENS","content":{"parts":[{"text":"{\"sto"}]}}]}`}, 1, "finish reason MAX_TOKENS"},
		{"blocked", reply{http.StatusOK, `{"promptFeedback":{"blockReason":"SAFETY"}}`}, 1, "blocked the prompt (SAFETY)"},
		{"no candidates", reply{http.StatusOK, `{}`}, 1, "no candidates"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			replies := make([]reply, tc.calls)
			for i := range replies {
				replies[i] = tc.reply
			}
			s := newServer(t, replies...)
			_, err := client(s.URL).Run(t.Context(), claude.Request{Prompt: "x", Schema: schema})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "secret-key") {
				t.Errorf("error leaks the API key: %v", err)
			}
			if len(s.requests) != tc.calls {
				t.Errorf("requests = %d, want %d", len(s.requests), tc.calls)
			}
		})
	}
}

func TestRunRejectsAnUnsupportedTool(t *testing.T) {
	_, err := client("http://unused").Run(t.Context(), claude.Request{Prompt: "x", Tools: []string{"Bash"}})
	if err == nil || !strings.Contains(err.Error(), `"Bash"`) {
		t.Fatalf("err = %v", err)
	}
}
