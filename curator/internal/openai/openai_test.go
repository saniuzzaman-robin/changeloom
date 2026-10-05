package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/openai"
)

// server replies with each canned response in order and records the requests.
type server struct {
	*httptest.Server
	mu       sync.Mutex
	requests []map[string]any
	auth     []string
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
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		if r.URL.Path != "/v1/chat/completions" || len(s.requests) > len(replies) {
			t.Errorf("unexpected request %d to %s", len(s.requests), r.URL.Path)
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

func client(url, key string) *openai.Client {
	c := openai.New(config.OpenAI{BaseURL: url + "/v1", APIKey: key, Model: "m", Timeout: 5 * time.Second})
	c.SetRetryDelay(time.Millisecond)
	return c
}

var schema = map[string]any{"type": "object", "required": []string{"stories"}}

func ok(content string) reply {
	b, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}},
		"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
	})
	return reply{http.StatusOK, string(b)}
}

func TestRunAnswersInSchema(t *testing.T) {
	srv := newServer(t, ok(`{"stories":[]}`))

	res, err := client(srv.URL, "sk-test").Run(context.Background(), claude.Request{Prompt: "find", Schema: schema, Tools: []string{"WebSearch", "WebFetch"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Output) != `{"stories":[]}` || res.Model != "m" || res.Account != openai.Account(srv.URL+"/v1") || res.CostUSD != 0 {
		t.Fatalf("result = %+v", res)
	}
	if res.Usage.InputTokens != 10 || res.Usage.OutputTokens != 5 || res.Turns != 1 {
		t.Fatalf("usage/turns = %+v / %d", res.Usage, res.Turns)
	}
	if srv.auth[0] != "Bearer sk-test" {
		t.Errorf("authorization = %q", srv.auth[0])
	}

	// One request: the schema as response_format and no tools, since the model searches on its own.
	req := srv.requests[0]
	if rf, _ := req["response_format"].(map[string]any); rf["type"] != "json_schema" || req["tools"] != nil {
		t.Errorf("request = %v", req)
	}
	msgs, _ := req["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" || msgs[1].(map[string]any)["content"] != "find" {
		t.Errorf("messages = %v", msgs)
	}
}

func TestRunRetriesInvalidAnswer(t *testing.T) {
	srv := newServer(t, ok("not json"), ok(`{"other":1}`), ok(`{"stories":[1]}`))
	res, err := client(srv.URL, "").Run(context.Background(), claude.Request{Prompt: "p", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Output) != `{"stories":[1]}` || res.Turns != 3 || res.Usage.InputTokens != 30 {
		t.Fatalf("result = %+v", res)
	}
	// Each retry carries the rejected answer and the reason.
	msgs, _ := srv.requests[1]["messages"].([]any)
	if len(msgs) != 4 || msgs[2].(map[string]any)["content"] != "not json" ||
		!strings.Contains(msgs[3].(map[string]any)["content"].(string), "That answer was invalid") {
		t.Errorf("retry messages = %v", msgs)
	}
}

func TestRunGivesUpOnInvalidAnswer(t *testing.T) {
	srv := newServer(t, ok("x"), ok("y"), ok("z"))
	if _, err := client(srv.URL, "").Run(context.Background(), claude.Request{Prompt: "p", Schema: schema}); err == nil ||
		!strings.Contains(err.Error(), "no valid structured output") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunRejectsUnknownTool(t *testing.T) {
	srv := newServer(t)
	if _, err := client(srv.URL, "").Run(context.Background(), claude.Request{Prompt: "p", Schema: schema, Tools: []string{"Bash"}}); err == nil ||
		len(srv.requests) != 0 {
		t.Fatalf("err = %v after %d requests", err, len(srv.requests))
	}
}

func TestRunRetriesRateLimits(t *testing.T) {
	srv := newServer(t, reply{http.StatusTooManyRequests, "slow down"}, reply{http.StatusBadGateway, "oops"}, ok(`{"stories":[]}`))
	if _, err := client(srv.URL, "").Run(context.Background(), claude.Request{Prompt: "p", Schema: schema}); err != nil {
		t.Fatal(err)
	}
	if srv.auth[0] != "" {
		t.Errorf("authorization sent without a key: %q", srv.auth[0])
	}
}

func TestRunAuthErrorHint(t *testing.T) {
	srv := newServer(t, reply{http.StatusUnauthorized, `{"error":{"message":"bad key"}}`})
	if _, err := client(srv.URL, "k").Run(context.Background(), claude.Request{Prompt: "p", Schema: schema}); err == nil ||
		!strings.Contains(err.Error(), "OPENAI_API_KEY") || len(srv.requests) != 1 {
		t.Fatalf("err = %v after %d requests", err, len(srv.requests))
	}
}
