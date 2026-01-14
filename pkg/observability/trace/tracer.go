// Package trace provides OpenTelemetry tracing for the Marionette server.
// It supports OTLP, Jaeger, and Zipkin exporters for distributed tracing.
package trace

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Config holds configuration for the tracer.
type Config struct {
	// Enabled controls whether tracing is enabled.
	Enabled bool

	// Exporter specifies the exporter type: "otlp", "stdout", or "noop".
	Exporter string

	// Endpoint is the collector endpoint (for OTLP exporter).
	// Example: "localhost:4317" for gRPC, "localhost:4318" for HTTP.
	Endpoint string

	// ServiceName is the name of the service for tracing.
	ServiceName string

	// ServiceVersion is the version of the service.
	ServiceVersion string

	// Environment is the deployment environment (e.g., "production", "staging").
	Environment string

	// SampleRate is the sampling rate (0.0 to 1.0).
	// 1.0 means all traces are sampled, 0.1 means 10% of traces.
	SampleRate float64

	// Insecure disables TLS for the OTLP exporter.
	Insecure bool
}

// DefaultConfig returns the default tracer configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:        false,
		Exporter:       "otlp",
		Endpoint:       "localhost:4317",
		ServiceName:    "marionette-server",
		ServiceVersion: "unknown",
		Environment:    "development",
		SampleRate:     0.1,
		Insecure:       true,
	}
}

// Provider wraps the OpenTelemetry TracerProvider with lifecycle management.
type Provider struct {
	provider *sdktrace.TracerProvider
	logger   *zap.Logger
}

// NewProvider creates a new tracer provider based on the configuration.
func NewProvider(ctx context.Context, cfg Config, logger *zap.Logger) (*Provider, error) {
	if !cfg.Enabled {
		// Return a no-op provider when tracing is disabled
		return &Provider{
			provider: nil,
			logger:   logger,
		}, nil
	}

	// Create resource with service information
	// Note: We create resource directly without merging with Default() to avoid
	// schema URL conflicts between different OpenTelemetry package versions
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
		resource.WithHost(),
		resource.WithProcess(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating resource: %w", err)
	}

	// Create exporter based on configuration
	var exporter sdktrace.SpanExporter
	switch cfg.Exporter {
	case "otlp":
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.Endpoint),
		}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exp, err := otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("creating OTLP exporter: %w", err)
		}
		exporter = exp

	case "stdout":
		exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("creating stdout exporter: %w", err)
		}
		exporter = exp

	case "noop", "":
		// No exporter, traces are discarded
		logger.Info("tracing enabled but no exporter configured")

	default:
		return nil, fmt.Errorf("unknown exporter type: %s", cfg.Exporter)
	}

	// Create sampler
	var sampler sdktrace.Sampler
	if cfg.SampleRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if cfg.SampleRate <= 0.0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}

	// Build tracer provider options
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	}
	if exporter != nil {
		opts = append(opts, sdktrace.WithBatcher(exporter))
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(opts...)

	// Set global tracer provider and propagator
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger.Info("tracing initialized",
		zap.String("exporter", cfg.Exporter),
		zap.String("endpoint", cfg.Endpoint),
		zap.String("service_name", cfg.ServiceName),
		zap.Float64("sample_rate", cfg.SampleRate),
	)

	return &Provider{
		provider: tp,
		logger:   logger,
	}, nil
}

// Tracer returns a tracer with the given name.
func (p *Provider) Tracer(name string) trace.Tracer {
	if p.provider == nil {
		return otel.Tracer(name)
	}
	return p.provider.Tracer(name)
}

// Shutdown gracefully shuts down the tracer provider.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.provider == nil {
		return nil
	}
	p.logger.Info("shutting down tracer provider")
	return p.provider.Shutdown(ctx)
}

// IsEnabled returns whether tracing is enabled.
func (p *Provider) IsEnabled() bool {
	return p.provider != nil
}
