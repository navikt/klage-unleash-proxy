package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/navikt/klage-unleash-proxy/env"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

// Config holds the OpenTelemetry configuration
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string
}

// ConfigFromEnv creates a Config from environment variables
func ConfigFromEnv() Config {
	serviceName := env.OtelServiceName
	if serviceName == "" {
		serviceName = env.NaisAppName
	}
	if serviceName == "" {
		serviceName = env.DefaultServiceName
	}

	serviceVersion := env.OtelServiceVersion
	if serviceVersion == "" {
		serviceVersion = "unknown"
	}

	environment := env.NaisClusterName
	if environment == "" {
		environment = env.UnleashServerAPIEnv
	}
	if environment == "" {
		environment = "development"
	}

	otlpEndpoint := env.OtelExporterOTLPEndpoint

	return Config{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Environment:    environment,
		OTLPEndpoint:   otlpEndpoint,
	}
}

// Telemetry holds the OpenTelemetry providers
type Telemetry struct {
	TracerProvider *trace.TracerProvider
	MeterProvider  *metric.MeterProvider
}

// Shutdown gracefully shuts down the telemetry providers
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var err error
	if t.TracerProvider != nil {
		if e := t.TracerProvider.Shutdown(ctx); e != nil {
			err = e
			slog.Error("Failed to shutdown tracer provider", slog.String("error", e.Error()))
		}
	}
	if t.MeterProvider != nil {
		if e := t.MeterProvider.Shutdown(ctx); e != nil {
			err = e
			slog.Error("Failed to shutdown meter provider", slog.String("error", e.Error()))
		}
	}
	return err
}

// Initialize sets up OpenTelemetry with tracing and metrics
func Initialize(ctx context.Context, cfg Config) (*Telemetry, error) {
	logger := slog.Default()

	// If no OTLP endpoint is configured, return nil (telemetry disabled)
	if cfg.OTLPEndpoint == "" {
		logger.Info("OpenTelemetry disabled: OTEL_EXPORTER_OTLP_ENDPOINT not set")
		return nil, nil
	}

	logger.Info("Initializing OpenTelemetry",
		slog.String("service_name", cfg.ServiceName),
		slog.String("service_version", cfg.ServiceVersion),
		slog.String("environment", cfg.Environment),
		slog.String("otlp_endpoint", cfg.OTLPEndpoint),
	)

	// Create resource with service information
	// Using resource.New with WithSchemaURL avoids schema URL conflicts
	// that occur when merging resources with different semconv versions
	res, err := resource.New(ctx,
		resource.WithSchemaURL(SchemaURL),
		resource.WithAttributes(
			ServiceName(cfg.ServiceName),
			ServiceVersion(cfg.ServiceVersion),
			DeploymentEnvironment(cfg.Environment),
		),
	)
	if err != nil {
		return nil, err
	}

	telemetry := &Telemetry{}

	// Set up trace exporter
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	// Create tracer provider
	telemetry.TracerProvider = trace.NewTracerProvider(
		trace.WithBatcher(traceExporter,
			trace.WithBatchTimeout(5*time.Second),
		),
		trace.WithResource(res),
		trace.WithSampler(trace.AlwaysSample()),
	)

	// Set global tracer provider
	otel.SetTracerProvider(telemetry.TracerProvider)

	// Set up propagator for trace context propagation
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Set up metrics exporter
	metricExporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return telemetry, err
	}

	// Create meter provider
	telemetry.MeterProvider = metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(metricExporter,
			metric.WithInterval(30*time.Second),
		)),
	)

	// Set global meter provider
	otel.SetMeterProvider(telemetry.MeterProvider)

	logger.Info("OpenTelemetry initialized successfully")

	return telemetry, nil
}
