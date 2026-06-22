package svc

import (
	"context"
	"fmt"

	"github.com/distr-sh/distr/internal/env"
	"github.com/distr-sh/distr/internal/tracers"
	sentryotlp "github.com/getsentry/sentry-go/otel/otlp"
	"github.com/go-logr/zapr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func (r *Registry) GetTracers() *tracers.Tracers {
	return r.tracers
}

func (reg *Registry) createTracer(ctx context.Context, enabled bool) (*tracers.Tracers, error) {
	otel.SetLogger(zapr.NewLogger(reg.logger))

	if !enabled {
		provider := oteltrace.NewNoopTracerProvider()
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
		return &tracers.Tracers{
			DefaultProvider:  provider,
			AlwaysProvider:   provider,
			AgentProvider:    provider,
			RegistryProvider: provider,
		}, nil
	}

	tpopts := []sdktrace.TracerProviderOption{}
	tmps := []propagation.TextMapPropagator{propagation.TraceContext{}, propagation.Baggage{}}

	if env.OtelExporterOtlpEnabled() {
		if exp, err := otlptracegrpc.New(ctx); err != nil {
			return nil, err
		} else {
			tpopts = append(tpopts, sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exp)))
		}
	}

	if env.OtelExporterSentryEnabled() {
		if exp, err := sentryotlp.NewTraceExporter(ctx, env.SentryDSN()); err != nil {
			return nil, err
		} else {
			tpopts = append(tpopts, sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exp)))
		}
	}

	tracers := tracers.Tracers{
		DefaultProvider: sdktrace.NewTracerProvider(tpopts...),
		AlwaysProvider:  sdktrace.NewTracerProvider(append(tpopts, sdktrace.WithSampler(sdktrace.AlwaysSample()))...),
	}

	otel.SetTracerProvider(tracers.DefaultProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(tmps...))

	if cfg := env.OtelAgentSampler(); cfg != nil {
		tracers.AgentProvider = sdktrace.NewTracerProvider(append(
			tpopts,
			sdktrace.WithSampler(samplerFromConfig(cfg)),
		)...)
	}

	if cfg := env.OtelRegistrySampler(); cfg != nil {
		tracers.RegistryProvider = sdktrace.NewTracerProvider(append(
			tpopts,
			sdktrace.WithSampler(samplerFromConfig(cfg)),
		)...)
	}

	return &tracers, nil
}

func samplerFromConfig(cfg *env.SamplerConfig) sdktrace.Sampler {
	switch cfg.Sampler {
	case env.SamplerAlwaysOn:
		return sdktrace.AlwaysSample()
	case env.SamplerAlwaysOff:
		return sdktrace.NeverSample()
	case env.SamplerTraceIDRatio:
		return sdktrace.TraceIDRatioBased(cfg.Arg)
	case env.SamplerParentBasedAlwaysOn:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	case env.SamplerParsedBasedAlwaysOff:
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case env.SamplerParentBasedTraceIDRatio:
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Arg))
	default:
		panic(fmt.Sprintf("invalid SamplerType: %v", cfg.Sampler))
	}
}
