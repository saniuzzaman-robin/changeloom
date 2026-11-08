package httpapi

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/logging"
)

// withTrace stores the request's trace in its context, so every log entry for it carries the trace.
// When tracing sampled this request, entries point at its server span so they show under it in
// Cloud Trace; otherwise they use the trace headers Cloud Run sent.
func withTrace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := logging.TraceFromRequest(r)
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() && sc.IsSampled() {
			t = logging.Trace{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String()}
		}
		next.ServeHTTP(w, r.WithContext(logging.WithTrace(r.Context(), t)))
	})
}

// observe logs one entry per request and turns a panic into a JSON 500 (or, once the response has
// started, an aborted connection) with the stack logged for Error Reporting. It logs no user data.
func observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w}
		defer func() {
			v := recover()
			// Once the response has started there is no way to report the failure, so the connection is
			// aborted instead. http.ErrAbortHandler is net/http's own signal to do that quietly.
			abort := v != nil && (isAbort(v) || rec.status != 0)
			if v != nil && !isAbort(v) {
				slog.ErrorContext(r.Context(), fmt.Sprintf("panic: %v", v),
					"method", r.Method, "path", r.URL.Path, "stack_trace", string(debug.Stack()))
				if !abort {
					writeError(rec, http.StatusInternalServerError, "internal", "internal server error")
				}
			}
			slog.InfoContext(r.Context(), "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.statusCode(),
				"bytes", rec.bytes,
				"latency_ms", time.Since(start).Milliseconds(),
			)
			if abort {
				panic(http.ErrAbortHandler)
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

func isAbort(v any) bool {
	err, ok := v.(error)
	return ok && errors.Is(err, http.ErrAbortHandler)
}

// recorder captures the status and size of a response.
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *recorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// withTimeout bounds each request's context, which also cancels its in-flight database queries.
func withTimeout(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// jsonFallback answers requests that match no route with the API's JSON error instead of the mux's
// plain-text 404 and 405 (keeping the Allow header). It names the request's span after the matched
// route, which keeps span names few (no IDs from the path); without tracing the span is a no-op.
func jsonFallback(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern == "" {
			h.ServeHTTP(&fallbackWriter{ResponseWriter: w}, r)
			return
		}
		span := trace.SpanFromContext(r.Context())
		span.SetName(pattern)
		span.SetAttributes(attribute.String("http.route", pattern))
		mux.ServeHTTP(w, r)
	})
}

// fallbackWriter replaces a 404 or 405 body with the JSON error and passes anything else (such as
// the mux's trailing-slash redirects) through.
type fallbackWriter struct {
	http.ResponseWriter
	replaced bool
}

func (f *fallbackWriter) WriteHeader(code int) {
	switch code {
	case http.StatusNotFound:
		f.replaced = true
		writeError(f.ResponseWriter, code, "not_found", "no such endpoint")
	case http.StatusMethodNotAllowed:
		f.replaced = true
		writeError(f.ResponseWriter, code, "method_not_allowed", "method not allowed for this endpoint")
	default:
		f.ResponseWriter.WriteHeader(code)
	}
}

func (f *fallbackWriter) Write(b []byte) (int, error) {
	if f.replaced {
		return len(b), nil
	}
	return f.ResponseWriter.Write(b)
}

// gzipMinBytes is the smallest JSON body worth compressing: below it gzip's header and trailer
// outweigh the saving.
const gzipMinBytes = 1024

var gzipWriters = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

// compress gzips JSON responses of at least gzipMinBytes when the client accepts gzip. Every
// response carries Vary: Accept-Encoding. For gzip clients a strong ETag is made weak, since the
// bytes on the wire differ by encoding; handlers compare If-None-Match weakly, so it still matches.
// A response that already has a Content-Encoding, or has no body, passes through unchanged.
func compress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if r.Method == http.MethodHead || !acceptsGzip(strings.Join(r.Header.Values("Accept-Encoding"), ",")) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipWriter{ResponseWriter: w}
		next.ServeHTTP(gw, r)
		// Not deferred: after a panic the held body must be dropped, so observe can still answer 500.
		gw.finish()
	})
}

// acceptsGzip reports whether an Accept-Encoding header lists gzip without q=0.
func acceptsGzip(header string) bool {
	for part := range strings.SplitSeq(header, ",") {
		coding, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(coding), "gzip") {
			continue
		}
		name, value, _ := strings.Cut(strings.TrimSpace(params), "=")
		if strings.EqualFold(strings.TrimSpace(name), "q") {
			q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			return err == nil && q > 0
		}
		return true
	}
	return false
}

// gzipWriter holds a JSON body until it reaches gzipMinBytes, then compresses it; a shorter body
// is sent as is when the handler returns. Anything else passes straight through.
type gzipWriter struct {
	http.ResponseWriter
	status  int
	through bool         // the status was sent uncompressed: writes go straight to ResponseWriter
	held    []byte       // body written before deciding
	gz      *gzip.Writer // non-nil once compressing
}

func (g *gzipWriter) WriteHeader(code int) {
	if code < http.StatusOK {
		// Informational: the final status is still to come.
		g.ResponseWriter.WriteHeader(code)
		return
	}
	if g.status != 0 {
		return
	}
	g.status = code
	h := g.Header()
	if etag := h.Get("ETag"); etag != "" && !strings.HasPrefix(etag, "W/") {
		h.Set("ETag", "W/"+etag)
	}
	mediaType, _, _ := strings.Cut(h.Get("Content-Type"), ";")
	if code == http.StatusNoContent || code == http.StatusNotModified ||
		h.Get("Content-Encoding") != "" || !strings.EqualFold(strings.TrimSpace(mediaType), "application/json") {
		g.through = true
		g.ResponseWriter.WriteHeader(code)
	}
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if g.status == 0 {
		g.WriteHeader(http.StatusOK)
	}
	switch {
	case g.through:
		return g.ResponseWriter.Write(b)
	case g.gz != nil:
		return g.gz.Write(b)
	}
	g.held = append(g.held, b...)
	if len(g.held) < gzipMinBytes {
		return len(b), nil
	}
	h := g.Header()
	h.Set("Content-Encoding", "gzip")
	h.Del("Content-Length")
	g.ResponseWriter.WriteHeader(g.status)
	g.gz = gzipWriters.Get().(*gzip.Writer)
	g.gz.Reset(g.ResponseWriter)
	held := g.held
	g.held = nil
	if _, err := g.gz.Write(held); err != nil {
		return 0, err
	}
	return len(b), nil
}

// finish sends a held short body, or ends the gzip stream.
func (g *gzipWriter) finish() {
	switch {
	case g.gz != nil:
		if err := g.gz.Close(); err != nil {
			slog.Error("write gzip response", "err", err)
		}
		gzipWriters.Put(g.gz)
	case g.status != 0 && !g.through:
		g.ResponseWriter.WriteHeader(g.status)
		if _, err := g.ResponseWriter.Write(g.held); err != nil {
			slog.Error("write json response", "err", err)
		}
	}
}
