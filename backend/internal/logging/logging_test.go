package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func logOne(ctx context.Context, t *testing.T, project string, level slog.Level) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	slog.New(NewHandler(&buf, slog.LevelDebug, project)).Log(ctx, level, "hello", "k", "v")
	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decode %q: %v", buf.String(), err)
	}
	return entry
}

func TestSeverityAndMessage(t *testing.T) {
	for level, want := range map[slog.Level]string{
		slog.LevelDebug: "DEBUG",
		slog.LevelInfo:  "INFO",
		slog.LevelWarn:  "WARNING",
		slog.LevelError: "ERROR",
	} {
		entry := logOne(context.Background(), t, "", level)
		if entry["severity"] != want || entry["message"] != "hello" || entry["k"] != "v" {
			t.Errorf("level %s: entry %v, want severity %s and message hello", level, entry, want)
		}
		if _, ok := entry["level"]; ok {
			t.Errorf("level %s: entry still has slog's level key: %v", level, entry)
		}
		_, reported := entry["@type"]
		if reported != (level == slog.LevelError) {
			t.Errorf("level %s: @type present = %v, want %v", level, reported, level == slog.LevelError)
		}
	}
}

func TestTraceFields(t *testing.T) {
	ctx := WithTrace(context.Background(), Trace{TraceID: "0af7651916cd43dd8448eb211c80319c", SpanID: "b7ad6b7169203331"})

	hosted := logOne(ctx, t, "my-proj", slog.LevelInfo)
	if got := hosted[traceKey]; got != "projects/my-proj/traces/0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("trace = %v", got)
	}
	if got := hosted[spanKey]; got != "b7ad6b7169203331" {
		t.Errorf("span = %v", got)
	}

	local := logOne(ctx, t, "", slog.LevelInfo)
	if got := local["trace_id"]; got != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("local trace_id = %v", got)
	}
	if _, ok := local[traceKey]; ok {
		t.Errorf("local entry has a Cloud Trace name: %v", local)
	}
}

func TestTraceFromRequest(t *testing.T) {
	cases := []struct {
		name, header, value string
		want                Trace
	}{
		{"traceparent", "traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
			Trace{TraceID: "0af7651916cd43dd8448eb211c80319c", SpanID: "b7ad6b7169203331"}},
		{"cloud trace context", "X-Cloud-Trace-Context", "105445AA7843BC8BF206B12000100000/1;o=1",
			Trace{TraceID: "105445aa7843bc8bf206b12000100000", SpanID: "0000000000000001"}},
		{"cloud trace without span", "X-Cloud-Trace-Context", "105445aa7843bc8bf206b12000100000",
			Trace{TraceID: "105445aa7843bc8bf206b12000100000"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			r.Header.Set(tc.header, tc.value)
			if got := TraceFromRequest(r); got != tc.want {
				t.Errorf("TraceFromRequest() = %+v, want %+v", got, tc.want)
			}
		})
	}

	for _, bad := range []string{"", "garbage", "00-00000000000000000000000000000000-b7ad6b7169203331-01", "00-xyz-b7ad6b7169203331-01"} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.Header.Set("traceparent", bad)
		got := TraceFromRequest(r)
		if len(got.TraceID) != 32 || got.SpanID != "" {
			t.Errorf("traceparent %q: got %+v, want a fresh 32-char trace", bad, got)
		}
	}
}
