// Package testtelemetry provides deterministic in-memory providers for tests.
package testtelemetry

import (
	"context"
	"encoding/binary"
	"errors"
	"sync/atomic"

	metricapi "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	traceapi "go.opentelemetry.io/otel/trace"
)

// Harness owns deterministic trace and metric providers and their readers.
type Harness struct {
	tracer   *sdktrace.TracerProvider
	recorder *tracetest.SpanRecorder
	meter    *sdkmetric.MeterProvider
	reader   *sdkmetric.ManualReader
}

// New constructs an isolated in-memory telemetry harness.
func New() *Harness {
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(
		sdktrace.WithIDGenerator(&sequentialIDGenerator{}),
		sdktrace.WithSpanProcessor(recorder),
	)
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	return &Harness{tracer: tracer, recorder: recorder, meter: meter, reader: reader}
}

// TracerProvider returns a standard OpenTelemetry tracer provider.
func (h *Harness) TracerProvider() traceapi.TracerProvider {
	return h.tracer
}

// MeterProvider returns a standard OpenTelemetry meter provider.
func (h *Harness) MeterProvider() metricapi.MeterProvider {
	return h.meter
}

// Spans returns immutable snapshots of all ended spans.
func (h *Harness) Spans() tracetest.SpanStubs {
	return tracetest.SpanStubsFromReadOnlySpans(h.recorder.Ended())
}

// ResetSpans removes all recorded spans.
func (h *Harness) ResetSpans() {
	h.recorder.Reset()
}

// Metrics synchronously collects current metric data.
func (h *Harness) Metrics(ctx context.Context) (metricdata.ResourceMetrics, error) {
	var metrics metricdata.ResourceMetrics
	err := h.reader.Collect(ctx, &metrics)
	return metrics, err
}

// Shutdown releases both providers and aggregates failures.
func (h *Harness) Shutdown(ctx context.Context) error {
	return errors.Join(h.meter.Shutdown(ctx), h.tracer.Shutdown(ctx))
}

type sequentialIDGenerator struct {
	trace atomic.Uint64
	span  atomic.Uint64
}

func (g *sequentialIDGenerator) NewIDs(context.Context) (traceapi.TraceID, traceapi.SpanID) {
	var traceID traceapi.TraceID
	binary.BigEndian.PutUint64(traceID[8:], g.trace.Add(1))
	return traceID, g.newSpanID()
}

func (g *sequentialIDGenerator) NewSpanID(context.Context, traceapi.TraceID) traceapi.SpanID {
	return g.newSpanID()
}

func (g *sequentialIDGenerator) newSpanID() traceapi.SpanID {
	var spanID traceapi.SpanID
	binary.BigEndian.PutUint64(spanID[:], g.span.Add(1))
	return spanID
}
