// Package logging writes slog records as Cloud Logging structured JSON: `severity` and `message`
// instead of slog's `level` and `msg`, the request's trace so entries group under it in the Logs
// Explorer, and the Error Reporting type on errors so they show up there.
package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

// Cloud Logging field names; see https://cloud.google.com/logging/docs/structured-logging.
const (
	traceKey  = "logging.googleapis.com/trace"
	spanKey   = "logging.googleapis.com/spanId"
	errorType = "type.googleapis.com/google.devtools.clouderrorreporting.v1beta1.ReportedErrorEvent"
)

// NewHandler returns a JSON handler for w. project is the GCP project that traces belong to; when it
// is empty (local dev) the trace ID is logged as `trace_id` instead of a Cloud Trace resource name.
func NewHandler(w io.Writer, level slog.Leveler, project string) slog.Handler {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) > 0 {
				return a
			}
			switch a.Key {
			case slog.LevelKey:
				return slog.String("severity", severity(a.Value.Any().(slog.Level)))
			case slog.MessageKey:
				return slog.Attr{Key: "message", Value: a.Value}
			}
			return a
		},
	})
	return &handler{Handler: h, project: project}
}

func severity(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARNING"
	case l >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

type handler struct {
	slog.Handler
	project string
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if t, ok := ctx.Value(traceCtxKey{}).(Trace); ok {
		if h.project != "" {
			r.AddAttrs(slog.String(traceKey, "projects/"+h.project+"/traces/"+t.TraceID))
			if t.SpanID != "" {
				r.AddAttrs(slog.String(spanKey, t.SpanID))
			}
		} else {
			r.AddAttrs(slog.String("trace_id", t.TraceID))
		}
	}
	if r.Level >= slog.LevelError {
		r.AddAttrs(slog.String("@type", errorType))
	}
	return h.Handler.Handle(ctx, r)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{Handler: h.Handler.WithAttrs(attrs), project: h.project}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{Handler: h.Handler.WithGroup(name), project: h.project}
}

// Trace identifies the request a log entry belongs to.
type Trace struct {
	// TraceID is 32 hex characters.
	TraceID string
	// SpanID is 16 hex characters, or empty when the caller sent none.
	SpanID string
}

type traceCtxKey struct{}

// WithTrace returns ctx carrying t, which the handler adds to every entry logged with ctx.
func WithTrace(ctx context.Context, t Trace) context.Context {
	return context.WithValue(ctx, traceCtxKey{}, t)
}

// TraceFrom returns the trace stored by WithTrace.
func TraceFrom(ctx context.Context) (Trace, bool) {
	t, ok := ctx.Value(traceCtxKey{}).(Trace)
	return t, ok
}

// TraceFromRequest reads the W3C `traceparent` header, falling back to Google's
// `X-Cloud-Trace-Context` (both are set by Cloud Run). Without either it starts a new trace, so
// local requests can still be correlated.
func TraceFromRequest(r *http.Request) Trace {
	if t, ok := parseTraceparent(r.Header.Get("traceparent")); ok {
		return t
	}
	if t, ok := parseCloudTraceContext(r.Header.Get("X-Cloud-Trace-Context")); ok {
		return t
	}
	return Trace{TraceID: randomHex(16)}
}

// parseTraceparent parses "00-<32 hex trace>-<16 hex span>-<2 hex flags>".
func parseTraceparent(v string) (Trace, bool) {
	parts := strings.Split(v, "-")
	if len(parts) != 4 || !isHex(parts[1], 32) || !isHex(parts[2], 16) {
		return Trace{}, false
	}
	return Trace{TraceID: parts[1], SpanID: parts[2]}, true
}

// parseCloudTraceContext parses "<32 hex trace>/<decimal span>;o=<options>".
func parseCloudTraceContext(v string) (Trace, bool) {
	v, _, _ = strings.Cut(v, ";")
	traceID, span, _ := strings.Cut(v, "/")
	if !isHex(traceID, 32) {
		return Trace{}, false
	}
	t := Trace{TraceID: strings.ToLower(traceID)}
	if n, err := strconv.ParseUint(span, 10, 64); err == nil && n != 0 {
		t.SpanID = strconv.FormatUint(n, 16)
		t.SpanID = strings.Repeat("0", 16-len(t.SpanID)) + t.SpanID
	}
	return t, true
}

func isHex(s string, n int) bool {
	if len(s) != n || strings.Trim(s, "0") == "" {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error.
	return hex.EncodeToString(b)
}
