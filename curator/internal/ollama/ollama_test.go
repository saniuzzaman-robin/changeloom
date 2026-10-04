package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/claude"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/ollama"
	"github.com/saniuzzaman-robin/changeloom/curator/internal/web"
)

type fakeWeb struct {
	searches, fetches []string
}

func (f *fakeWeb) Search(_ context.Context, q string) ([]web.SearchResult, error) {
	f.searches = append(f.searches, q)
	return []web.SearchResult{{Title: "T", URL: "https://a.example"}}, nil
}

func (f *fakeWeb) FetchPage(_ context.Context, u string) (string, error) {
	f.fetches = append(f.fetches, u)
	return "", errors.New("boom")
}

// server replies with each canned response in order and records the requests.
type server struct {
	*httptest.Server
	mu       sync.Mutex
	requests []map[string]any
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
		if r.URL.Path != "/api/chat" || len(s.requests) > len(replies) {
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

func client(url string, w ollama.Web) *ollama.Client {
	c := ollama.New(config.Ollama{URL: url, Model: "m", Timeout: 5 * time.Second, MaxTurns: 3, ContextTokens: 4096}, w)
	c.SetRetryDelay(time.Millisecond)
	return c
}

var schema = map[string]any{"type": "object", "required": []string{"stories"}}

func ok(content string) reply {
	b, _ := json.Marshal(map[string]any{"message": map[string]any{"role": "assistant", "content": content}, "prompt_eval_count": 10, "eval_count": 5})
	return reply{http.StatusOK, string(b)}
}

func TestRunResearchesThenAnswersInSchema(t *testing.T) {
	toolCall := reply{http.StatusOK, `{"message":{"role":"assistant","content":"","tool_calls":[
		{"function":{"name":"web_search","arguments":{"query":"go news"}}},
		{"function":{"name":"web_fetch","arguments":{"url":"https://a.example"}}}]},"prompt_eval_count":10,"eval_count":5}`}
	srv := newServer(t, toolCall, ok("notes"), ok(`{"stories":[]}`))
	w := &fakeWeb{}

	res, err := client(srv.URL, w).Run(context.Background(), claude.Request{Prompt: "find", Schema: schema, Tools: []string{"WebSearch", "WebFetch"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Output) != `{"stories":[]}` || res.Model != "m" || res.Account != ollama.Account || res.CostUSD != 0 {
		t.Fatalf("result = %+v", res)
	}
	if res.Usage.InputTokens != 30 || res.Usage.OutputTokens != 15 || res.Turns != 3 {
		t.Fatalf("usage/turns = %+v / %d", res.Usage, res.Turns)
	}
	if len(w.searches) != 1 || w.searches[0] != "go news" || len(w.fetches) != 1 {
		t.Fatalf("tool calls = %v %v", w.searches, w.fetches)
	}

	// Research calls carry tools and no format; the final call carries the schema and no tools. A
	// failed tool reaches the model as text.
	if srv.requests[0]["format"] != nil || srv.requests[0]["tools"] == nil {
		t.Errorf("research request = %v", srv.requests[0])
	}
	if srv.requests[2]["format"] == nil || srv.requests[2]["tools"] != nil {
		t.Errorf("final request = %v", srv.requests[2])
	}
	msgs, _ := json.Marshal(srv.requests[1]["messages"])
	if !strings.Contains(string(msgs), "error: boom") {
		t.Errorf("failed tool result not sent back: %s", msgs)
	}
}

func TestRunRetriesInvalidAnswer(t *testing.T) {
	srv := newServer(t, ok("not json"), ok(`{"other":1}`), ok(`{"stories":[1]}`))
	res, err := client(srv.URL, &fakeWeb{}).Run(context.Background(), claude.Request{Prompt: "p", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Output) != `{"stories":[1]}` || len(srv.requests) != 3 {
		t.Fatalf("output %s after %d requests", res.Output, len(srv.requests))
	}
}

func TestRunGivesUpOnInvalidAnswer(t *testing.T) {
	srv := newServer(t, ok("x"), ok("y"), ok("z"))
	if _, err := client(srv.URL, &fakeWeb{}).Run(context.Background(), claude.Request{Prompt: "p", Schema: schema}); err == nil ||
		!strings.Contains(err.Error(), "no valid structured output") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunStopsAfterMaxTurns(t *testing.T) {
	loop := reply{http.StatusOK, `{"message":{"role":"assistant","tool_calls":[{"function":{"name":"web_search","arguments":{"query":"q"}}}]}}`}
	srv := newServer(t, loop, loop, loop, ok(`{"stories":[]}`))
	if _, err := client(srv.URL, &fakeWeb{}).Run(context.Background(), claude.Request{Prompt: "p", Schema: schema, Tools: []string{"WebSearch"}}); err != nil {
		t.Fatal(err)
	}
	if len(srv.requests) != 4 {
		t.Fatalf("requests = %d, want 3 research turns + 1 final", len(srv.requests))
	}
}

func TestRunRetriesServerErrors(t *testing.T) {
	srv := newServer(t, reply{http.StatusServiceUnavailable, "loading"}, ok(`{"stories":[]}`))
	if _, err := client(srv.URL, &fakeWeb{}).Run(context.Background(), claude.Request{Prompt: "p", Schema: schema}); err != nil {
		t.Fatal(err)
	}
}

func TestRunMissingModelHint(t *testing.T) {
	srv := newServer(t, reply{http.StatusNotFound, `{"error":"model not found"}`})
	if _, err := client(srv.URL, &fakeWeb{}).Run(context.Background(), claude.Request{Prompt: "p", Schema: schema}); err == nil ||
		!strings.Contains(err.Error(), "ollama pull m") || len(srv.requests) != 1 {
		t.Fatalf("err = %v after %d requests", err, len(srv.requests))
	}
}

func TestRunRejectsUnknownTool(t *testing.T) {
	if _, err := client("http://unused", &fakeWeb{}).Run(context.Background(), claude.Request{Prompt: "p", Tools: []string{"Bash"}}); err == nil {
		t.Fatal("want an error")
	}
}
