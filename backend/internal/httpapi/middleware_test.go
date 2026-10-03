package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/auth"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/logging"
)

// captureLogs routes the default logger to a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(logging.NewHandler(&buf, slog.LevelDebug, "")))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) Error {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json (body %q)", ct, rec.Body.String())
	}
	var e Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return e
}

func TestPanicBecomesJSON500(t *testing.T) {
	logs := captureLogs(t)
	h := withTrace(observe(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })))
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/timeline", nil)
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if e := decodeError(t, rec); e.Code != "internal" {
		t.Errorf("error code = %q, want internal", e.Code)
	}
	out := logs.String()
	for _, want := range []string{`"message":"panic: boom"`, `"severity":"ERROR"`, `"stack_trace":"goroutine`,
		`"trace_id":"0af7651916cd43dd8448eb211c80319c"`, `"message":"request"`, `"status":500`} {
		if !strings.Contains(out, want) {
			t.Errorf("logs missing %s:\n%s", want, out)
		}
	}
}

func TestPanicAfterWriteAborts(t *testing.T) {
	captureLogs(t)
	h := observe(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		panic("late")
	}))
	defer func() {
		if v := recover(); !isAbort(v) {
			t.Errorf("recovered %v, want http.ErrAbortHandler", v)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
}

func TestAccessLogHasNoQueryOrAuth(t *testing.T) {
	logs := captureLogs(t)
	h := observe(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/search?q=secret-term", nil)
	req.Header.Set("Authorization", "Bearer dev:alice")
	h.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	if !strings.Contains(out, `"path":"/v1/search"`) || !strings.Contains(out, `"bytes":2`) || !strings.Contains(out, `"status":200`) {
		t.Errorf("access log missing fields:\n%s", out)
	}
	if strings.Contains(out, "secret-term") || strings.Contains(out, "alice") {
		t.Errorf("access log leaks the query or token:\n%s", out)
	}
}

func TestTimeoutSetsDeadline(t *testing.T) {
	var deadline time.Time
	h := withTimeout(time.Minute, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		deadline, _ = r.Context().Deadline()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if left := time.Until(deadline); left <= 0 || left > time.Minute {
		t.Errorf("deadline %s from now, want within a minute", left)
	}
}

func TestUnknownRoutesAreJSON(t *testing.T) {
	captureLogs(t)
	h := NewHandler(NewServer(nil, Options{}), auth.DevVerifier{})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound || decodeError(t, rec).Code != "not_found" {
		t.Errorf("GET /nope: %d %q, want JSON 404", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/health", nil))
	if rec.Code != http.StatusMethodNotAllowed || decodeError(t, rec).Code != "method_not_allowed" {
		t.Errorf("POST /health: %d %q, want JSON 405", rec.Code, rec.Body.String())
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Errorf("Allow = %q, want it to list GET", allow)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /health: %d, want 200", rec.Code)
	}
}

func TestBadParamNamesParameterOnly(t *testing.T) {
	rec := httptest.NewRecorder()
	badParam(rec, nil, &InvalidParamFormatError{ParamName: "limit", Err: errors.New("strconv.ParseInt: parsing \"x\": invalid syntax")})
	e := decodeError(t, rec)
	if rec.Code != http.StatusBadRequest || e.Message != `invalid value for parameter "limit"` {
		t.Errorf("badParam = %d %q", rec.Code, e.Message)
	}
}
