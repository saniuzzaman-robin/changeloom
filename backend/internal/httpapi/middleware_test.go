package httpapi

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

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

func TestRequestSpanIsNamedAfterRoute(t *testing.T) {
	captureLogs(t)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	h := otelhttp.NewHandler(NewHandler(NewServer(nil, Options{}), auth.DevVerifier{}), "request", otelhttp.WithTracerProvider(tp))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil))

	spans := rec.Ended()
	if len(spans) != 2 || spans[0].Name() != "GET /health" || spans[1].Name() != "GET" {
		names := make([]string, len(spans))
		for i, s := range spans {
			names[i] = s.Name()
		}
		t.Fatalf("span names = %q, want [GET /health GET]", names)
	}
}

func TestLogsUseSampledSpan(t *testing.T) {
	// With a project the handler logs the span ID, which tells the two cases apart.
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(logging.NewHandler(&logs, slog.LevelInfo, "proj")))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := withTrace(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		slog.InfoContext(r.Context(), "inside")
	}))
	traceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	for _, tc := range []struct {
		name     string
		flags    trace.TraceFlags
		wantSpan string
	}{
		{"sampled span", trace.FlagsSampled, "00f067aa0ba902b7"},
		{"unsampled span falls back to the header", 0, "b7ad6b7169203331"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs.Reset()
			sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: tc.flags})
			req := httptest.NewRequestWithContext(trace.ContextWithSpanContext(t.Context(), sc), http.MethodGet, "/", nil)
			req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
			h.ServeHTTP(httptest.NewRecorder(), req)
			if want := `"logging.googleapis.com/spanId":"` + tc.wantSpan + `"`; !strings.Contains(logs.String(), want) {
				t.Errorf("log lacks %s:\n%s", want, logs.String())
			}
		})
	}
}

func TestCompress(t *testing.T) {
	large := `{"items":"` + strings.Repeat("x", gzipMinBytes) + `"}`
	small := `{"items":"x"}`
	for _, tc := range []struct {
		name, acceptEncoding, contentType, contentEncoding, etag, body string
		status                                                         int
		wantGzip                                                       bool
		wantETag                                                       string
	}{
		{name: "large json", acceptEncoding: "gzip", contentType: "application/json", etag: `"a"`, body: large, status: http.StatusOK, wantGzip: true, wantETag: `W/"a"`},
		{name: "large json, error status", acceptEncoding: "br, GZIP;q=0.5", contentType: "application/json; charset=utf-8", body: large, status: http.StatusBadRequest, wantGzip: true},
		{name: "small json", acceptEncoding: "gzip", contentType: "application/json", etag: `"a"`, body: small, status: http.StatusCreated, wantETag: `W/"a"`},
		{name: "no gzip accepted", acceptEncoding: "", contentType: "application/json", etag: `"a"`, body: large, status: http.StatusOK, wantETag: `"a"`},
		{name: "gzip refused", acceptEncoding: "gzip;q=0", contentType: "application/json", body: large, status: 200},
		{name: "not json", acceptEncoding: "gzip", contentType: "text/plain", body: large, status: 200},
		{name: "already encoded", acceptEncoding: "gzip", contentType: "application/json", contentEncoding: "br", body: large, status: 200},
		{name: "not modified", acceptEncoding: "gzip", etag: `W/"a"`, status: http.StatusNotModified, wantETag: `W/"a"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := compress(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				if tc.contentEncoding != "" {
					w.Header().Set("Content-Encoding", tc.contentEncoding)
				}
				if tc.etag != "" {
					w.Header().Set("ETag", tc.etag)
				}
				w.WriteHeader(tc.status)
				// Small writes, so the body crosses gzipMinBytes part way through one.
				for b := []byte(tc.body); len(b) > 0; {
					n := min(len(b), 300)
					if _, err := w.Write(b[:n]); err != nil {
						t.Errorf("write: %v", err)
					}
					b = b[n:]
				}
			}))
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/timeline", nil)
			if tc.acceptEncoding != "" {
				req.Header.Set("Accept-Encoding", tc.acceptEncoding)
			}
			h.ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Errorf("status = %d, want %d", rec.Code, tc.status)
			}
			if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
				t.Errorf("Vary = %q, want Accept-Encoding", got)
			}
			if got := rec.Header().Get("ETag"); got != tc.wantETag {
				t.Errorf("ETag = %q, want %q", got, tc.wantETag)
			}
			body := rec.Body.String()
			if tc.wantGzip {
				if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
					t.Fatalf("Content-Encoding = %q, want gzip", got)
				}
				zr, err := gzip.NewReader(rec.Body)
				if err != nil {
					t.Fatalf("gzip reader: %v", err)
				}
				b, err := io.ReadAll(zr)
				if err != nil {
					t.Fatalf("gunzip body: %v", err)
				}
				body = string(b)
			} else if got := rec.Header().Get("Content-Encoding"); got != tc.contentEncoding {
				t.Errorf("Content-Encoding = %q, want %q", got, tc.contentEncoding)
			}
			if body != tc.body {
				t.Errorf("body = %q (%d bytes), want %d bytes", body[:min(len(body), 40)], len(body), len(tc.body))
			}
		})
	}
}

func TestCompressedHandlerPanics(t *testing.T) {
	captureLogs(t)
	// A panic while the body is still held still gets the JSON 500, uncompressed.
	h := observe(compress(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"items": "partial"})
		panic("boom")
	})))
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/timeline", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("status %d, Content-Encoding %q; want 500 uncompressed", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	if e := decodeError(t, rec); e.Code != "internal" {
		t.Errorf("error code = %q, want internal", e.Code)
	}

	// Once compressed bytes are out, the connection is aborted instead.
	h = observe(compress(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"items": strings.Repeat("x", 2*gzipMinBytes)})
		panic("boom")
	})))
	defer func() {
		if v := recover(); !isAbort(v) {
			t.Errorf("recovered %v, want http.ErrAbortHandler", v)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), req)
}
