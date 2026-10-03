package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
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
