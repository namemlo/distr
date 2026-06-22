package svc

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestCreateTracerUsesNoopProviderWhenObservabilityTracingDisabled(t *testing.T) {
	g := NewWithT(t)
	registry := &Registry{logger: zap.NewNop()}

	tracers, err := registry.createTracer(context.Background(), false)

	g.Expect(err).NotTo(HaveOccurred())
	_, span := tracers.Default().Tracer("test").Start(context.Background(), "disabled")
	g.Expect(span.SpanContext().IsValid()).To(BeFalse())
}
