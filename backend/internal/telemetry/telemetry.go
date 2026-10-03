// Package telemetry exports OpenTelemetry traces to Google Cloud Trace over OTLP.
package telemetry

import (
	"context"
	"fmt"
	"strings"

	"github.com/exaring/otelpgx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/oauth"
)

// cloudPlatformScope lets Application Default Credentials from a user login (local runs) mint
// tokens too; on Cloud Run the metadata server ignores it.
const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// Setup installs a global tracer provider that samples ratio of traces and exports them with OTLP to
// OTEL_EXPORTER_OTLP_ENDPOINT, authenticated with Application Default Credentials, and the W3C
// trace-context propagator that Cloud Run uses. The returned shutdown flushes buffered spans.
func Setup(ctx context.Context, projectID, serviceName string, ratio float64) (shutdown func(context.Context) error, err error) {
	creds, err := oauth.NewApplicationDefault(ctx, cloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("load Application Default Credentials for trace export: %w", err)
	}
	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithDialOption(grpc.WithPerRPCCredentials(creds)))
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	// Cloud Trace files spans under gcp.project_id.
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("gcp.project_id", projectID),
		attribute.String("service.name", serviceName),
	))
	if err != nil {
		return nil, fmt.Errorf("build trace resource: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		// Ratio sampling on the trace ID, ignoring the caller's flag: Cloud Run samples its own
		// spans far more sparsely, and following it would trace almost nothing.
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(ratio)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp.Shutdown, nil
}

// NewPgxTracer traces each query as a span named after its sqlc query, with the SQL statement but
// not its parameters, which can hold user data.
func NewPgxTracer() *otelpgx.Tracer {
	return otelpgx.NewTracer(otelpgx.WithSpanNameCtxFunc(func(_ context.Context, stmt string) string {
		return QueryName(stmt)
	}))
}

// QueryName returns X for sqlc's "-- name: X :kind" header, or else the statement's first word.
func QueryName(stmt string) string {
	if rest, ok := strings.CutPrefix(strings.TrimSpace(stmt), "-- name: "); ok {
		header, _, _ := strings.Cut(rest, "\n")
		if fields := strings.Fields(header); len(fields) > 0 {
			return fields[0]
		}
	}
	for word := range strings.FieldsSeq(stmt) {
		return strings.ToUpper(word)
	}
	return "UNKNOWN"
}
