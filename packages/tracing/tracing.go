// Package tracing wires OpenTelemetry — ADR-011's stack decision,
// docs/16-observability.md section 5. Scoped to what's actually built:
// apps/api's HTTP requests get real spans, exported via OTLP to the
// otel-collector and on to Tempo. The full pipeline trace docs/16 section
// 5 describes (one trace per job, spanning collector → brain → notifier,
// trace_id propagated through the River job payload) is not implemented
// — see this package's own comment on why that's a real, documented gap
// rather than a silent omission.
package tracing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Shutdown flushes and stops the tracer provider — call via defer from
// main after Setup succeeds.
type Shutdown func(ctx context.Context) error

// Setup configures the global TracerProvider for serviceName, exporting
// to SCOUT_OTEL_COLLECTOR_ENDPOINT (a bare host:port, e.g.
// "otel-collector:4318" — otlptracehttp appends its own /v1/traces path).
// A no-op provider is installed when that variable is unset, so a service
// runs identically with or without the observability stack present —
// same "monitoring absence must never affect the thing being monitored"
// posture as apps/collector/internal/heartbeat.Pinger's own Enabled().
func Setup(ctx context.Context, serviceName string, log *slog.Logger) (Shutdown, error) {
	endpoint := os.Getenv("SCOUT_OTEL_COLLECTOR_ENDPOINT")
	if endpoint == "" {
		log.Info("tracing disabled: SCOUT_OTEL_COLLECTOR_ENDPOINT is not set")
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(), // internal docker network only, no public ingress (ADR-014)
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: build OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: build resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// docs/16 section 5's sampling table (100% errors/notifications, 10%
		// normal runs) needs per-span error/notification awareness this
		// simple ratio sampler doesn't have. 10% is the closest single
		// number available without that logic — a documented
		// simplification, not the full spec.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))),
	)
	otel.SetTracerProvider(tp)
	log.Info("tracing armed", "service", serviceName, "collector_endpoint", endpoint)

	return func(shutdownCtx context.Context) error {
		shutdownCtx, cancel := context.WithTimeout(shutdownCtx, 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("tracing: shutdown: %w", err)
		}
		return nil
	}, nil
}
