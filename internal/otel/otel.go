// Package otel wires OpenTelemetry traces, metrics and logs over OTLP gRPC.
// On the k3s cluster the endpoint is the otel-lgtm service in `monitoring`;
// in dev it is off unless configs/dev.local.pkl turns it on.
package otel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config mirrors the Otel block of the Pkl schema as plain values.
type Config struct {
	Enabled          bool
	Endpoint         string
	Insecure         bool
	ServiceName      string
	Environment      string
	TraceSampleRatio float64
	MetricInterval   time.Duration
}

// Telemetry holds the configured providers. Nil when telemetry is disabled.
type Telemetry struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	LoggerProvider *sdklog.LoggerProvider
}

// Setup initialises global providers. It returns nil and a no-op shutdown when
// cfg.Enabled is false. Instruments created through the global otel.Meter
// before Setup are no-ops, so packages may declare counters unconditionally.
func Setup(ctx context.Context, cfg Config, version string) (*Telemetry, func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if !cfg.Enabled {
		return nil, noop, nil
	}
	if cfg.Endpoint == "" {
		return nil, noop, errors.New("otel: endpoint is required when enabled")
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", version),
		attribute.String("service.namespace", cfg.ServiceName),
		attribute.String("deployment.environment.name", cfg.Environment),
	))
	if err != nil {
		return nil, noop, fmt.Errorf("otel resource: %w", err)
	}

	traceOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
	metricOpts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.Endpoint)}
	logOpts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		traceOpts = append(traceOpts, otlptracegrpc.WithInsecure())
		metricOpts = append(metricOpts, otlpmetricgrpc.WithInsecure())
		logOpts = append(logOpts, otlploggrpc.WithInsecure())
	}
	traceExp, err := otlptracegrpc.New(ctx, traceOpts...)
	if err != nil {
		return nil, noop, fmt.Errorf("otel trace exporter: %w", err)
	}
	metricExp, err := otlpmetricgrpc.New(ctx, metricOpts...)
	if err != nil {
		return nil, noop, fmt.Errorf("otel metric exporter: %w", err)
	}
	logExp, err := otlploggrpc.New(ctx, logOpts...)
	if err != nil {
		return nil, noop, fmt.Errorf("otel log exporter: %w", err)
	}
	interval := cfg.MetricInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	t := &Telemetry{
		TracerProvider: sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExp),
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TraceSampleRatio))),
		),
		MeterProvider: sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(interval))),
		),
		LoggerProvider: sdklog.NewLoggerProvider(
			sdklog.WithResource(res),
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		),
	}
	otel.SetTracerProvider(t.TracerProvider)
	otel.SetMeterProvider(t.MeterProvider)
	global.SetLoggerProvider(t.LoggerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	shutdown := func(ctx context.Context) error {
		return errors.Join(t.TracerProvider.Shutdown(ctx), t.MeterProvider.Shutdown(ctx), t.LoggerProvider.Shutdown(ctx))
	}
	return t, shutdown, nil
}
