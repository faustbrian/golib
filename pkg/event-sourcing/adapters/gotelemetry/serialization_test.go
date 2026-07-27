package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type serializationContextKey struct{}

type telemetryPayloadCodec struct {
	encode        func(eventsourcing.DecodedEvent) (eventsourcing.EncodedEvent, error)
	decode        func(eventsourcing.EncodedEvent) (eventsourcing.DecodedEvent, error)
	encodeContext func(context.Context, eventsourcing.DecodedEvent) (eventsourcing.EncodedEvent, error)
	decodeContext func(context.Context, eventsourcing.EncodedEvent) (eventsourcing.DecodedEvent, error)
}

func (codec telemetryPayloadCodec) Encode(
	event eventsourcing.DecodedEvent,
) (eventsourcing.EncodedEvent, error) {
	return codec.encode(event)
}

func (codec telemetryPayloadCodec) Decode(
	event eventsourcing.EncodedEvent,
) (eventsourcing.DecodedEvent, error) {
	return codec.decode(event)
}

func (codec telemetryPayloadCodec) EncodeContext(
	ctx context.Context,
	event eventsourcing.DecodedEvent,
) (eventsourcing.EncodedEvent, error) {
	return codec.encodeContext(ctx, event)
}

func (codec telemetryPayloadCodec) DecodeContext(
	ctx context.Context,
	event eventsourcing.EncodedEvent,
) (eventsourcing.DecodedEvent, error) {
	return codec.decodeContext(ctx, event)
}

type telemetryUpcaster struct {
	upcast        func(eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error)
	upcastContext func(context.Context, eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error)
}

func (upcaster telemetryUpcaster) Upcast(
	event eventsourcing.UpcastEvent,
) ([]eventsourcing.UpcastEvent, error) {
	return upcaster.upcast(event)
}

func (upcaster telemetryUpcaster) UpcastContext(
	ctx context.Context,
	event eventsourcing.UpcastEvent,
) ([]eventsourcing.UpcastEvent, error) {
	return upcaster.upcastContext(ctx, event)
}

type legacyTelemetryPayloadCodec struct {
	decoded     eventsourcing.DecodedEvent
	encoded     eventsourcing.EncodedEvent
	encodeCalls int
	decodeCalls int
}

func (codec *legacyTelemetryPayloadCodec) Encode(
	eventsourcing.DecodedEvent,
) (eventsourcing.EncodedEvent, error) {
	codec.encodeCalls++

	return codec.encoded, nil
}

func (codec *legacyTelemetryPayloadCodec) Decode(
	eventsourcing.EncodedEvent,
) (eventsourcing.DecodedEvent, error) {
	codec.decodeCalls++

	return codec.decoded, nil
}

type legacyTelemetryUpcaster func(
	eventsourcing.UpcastEvent,
) ([]eventsourcing.UpcastEvent, error)

func (upcaster legacyTelemetryUpcaster) Upcast(
	event eventsourcing.UpcastEvent,
) ([]eventsourcing.UpcastEvent, error) {
	return upcaster(event)
}

func TestPayloadCodecInstrumentationPreservesCallsAndContext(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(testRuntime{
		tracer: sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(recorder),
		),
		meter:      sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	decoded, encoded := telemetrySerializationEvents(t)
	var encodeSpan, decodeSpan trace.SpanContext
	var encodeValue, decodeValue any
	pureEncodeCalls := 0
	pureDecodeCalls := 0
	contextEncodeCalls := 0
	contextDecodeCalls := 0
	codec, err := instrumentation.WrapPayloadCodec(telemetryPayloadCodec{
		encode: func(
			got eventsourcing.DecodedEvent,
		) (eventsourcing.EncodedEvent, error) {
			pureEncodeCalls++
			if got.Name() != decoded.Name() {
				t.Fatalf("Encode() event = %#v", got)
			}

			return encoded, nil
		},
		decode: func(
			got eventsourcing.EncodedEvent,
		) (eventsourcing.DecodedEvent, error) {
			pureDecodeCalls++
			if got.Name() != encoded.Name() {
				t.Fatalf("Decode() event = %#v", got)
			}

			return decoded, nil
		},
		encodeContext: func(
			ctx context.Context,
			got eventsourcing.DecodedEvent,
		) (eventsourcing.EncodedEvent, error) {
			contextEncodeCalls++
			encodeSpan = trace.SpanContextFromContext(ctx)
			encodeValue = ctx.Value(serializationContextKey{})
			if got.Name() != decoded.Name() {
				t.Fatalf("EncodeContext() event = %#v", got)
			}

			return encoded, nil
		},
		decodeContext: func(
			ctx context.Context,
			got eventsourcing.EncodedEvent,
		) (eventsourcing.DecodedEvent, error) {
			contextDecodeCalls++
			decodeSpan = trace.SpanContextFromContext(ctx)
			decodeValue = ctx.Value(serializationContextKey{})
			if got.Name() != encoded.Name() {
				t.Fatalf("DecodeContext() event = %#v", got)
			}

			return decoded, nil
		},
	})
	if err != nil {
		t.Fatalf("WrapPayloadCodec() error = %v", err)
	}
	if got, err := codec.Encode(decoded); err != nil ||
		got.Name() != encoded.Name() {
		t.Fatalf("Encode() = %#v, %v", got, err)
	}
	if got, err := codec.Decode(encoded); err != nil ||
		got.Name() != decoded.Name() {
		t.Fatalf("Decode() = %#v, %v", got, err)
	}
	parentCtx, parent := instrumentation.tracer.Start(
		context.WithValue(
			context.Background(),
			serializationContextKey{},
			"caller-value",
		),
		"parent",
	)
	if got, err := codec.EncodeContext(parentCtx, decoded); err != nil ||
		got.Name() != encoded.Name() {
		t.Fatalf("EncodeContext() = %#v, %v", got, err)
	}
	if got, err := codec.DecodeContext(parentCtx, encoded); err != nil ||
		got.Name() != decoded.Name() {
		t.Fatalf("DecodeContext() = %#v, %v", got, err)
	}
	parent.End()
	if pureEncodeCalls != 1 || pureDecodeCalls != 1 ||
		contextEncodeCalls != 1 || contextDecodeCalls != 1 {
		t.Fatalf(
			"codec calls = pure %d/%d, context %d/%d",
			pureEncodeCalls,
			pureDecodeCalls,
			contextEncodeCalls,
			contextDecodeCalls,
		)
	}
	if encodeSpan.TraceID() != parent.SpanContext().TraceID() ||
		decodeSpan.TraceID() != parent.SpanContext().TraceID() ||
		encodeValue != "caller-value" ||
		decodeValue != "caller-value" {
		t.Fatal("codec instrumentation did not preserve caller context")
	}
	spans := recorder.Ended()
	if len(spans) != 5 ||
		spans[0].Name() != "event_sourcing.codec.encode" ||
		spans[1].Name() != "event_sourcing.codec.decode" ||
		spans[2].Name() != "event_sourcing.codec.encode" ||
		spans[3].Name() != "event_sourcing.codec.decode" {
		t.Fatalf("codec spans = %#v", spans)
	}
	diagnostics := fmt.Sprint(spans)
	if strings.Contains(diagnostics, "secret.event") ||
		strings.Contains(diagnostics, "secret-value") ||
		strings.Contains(diagnostics, "secret-payload") {
		t.Fatal("codec telemetry disclosed event data")
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertOperationMetric(t, metrics, "codec_encode", "success", 2)
	assertOperationMetric(t, metrics, "codec_decode", "success", 2)
}

func TestPayloadCodecInstrumentationPreservesFailuresAndPanics(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(testRuntime{
		tracer: sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(recorder),
		),
		meter:      sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	decoded, encoded := telemetrySerializationEvents(t)
	secret := errors.New("secret codec failure")
	failing, err := instrumentation.WrapPayloadCodec(telemetryPayloadCodec{
		encode: func(
			eventsourcing.DecodedEvent,
		) (eventsourcing.EncodedEvent, error) {
			return eventsourcing.EncodedEvent{}, secret
		},
		decode: func(
			eventsourcing.EncodedEvent,
		) (eventsourcing.DecodedEvent, error) {
			return eventsourcing.DecodedEvent{}, secret
		},
		encodeContext: func(
			context.Context,
			eventsourcing.DecodedEvent,
		) (eventsourcing.EncodedEvent, error) {
			return eventsourcing.EncodedEvent{}, secret
		},
		decodeContext: func(
			context.Context,
			eventsourcing.EncodedEvent,
		) (eventsourcing.DecodedEvent, error) {
			return eventsourcing.DecodedEvent{}, secret
		},
	})
	if err != nil {
		t.Fatalf("WrapPayloadCodec(failing) error = %v", err)
	}
	if _, err := failing.Encode(decoded); !errors.Is(err, secret) {
		t.Fatalf("Encode() error = %v", err)
	}
	if _, err := failing.DecodeContext(
		context.Background(),
		encoded,
	); !errors.Is(err, secret) {
		t.Fatalf("DecodeContext() error = %v", err)
	}
	panicking, err := instrumentation.WrapPayloadCodec(telemetryPayloadCodec{
		encode: func(
			eventsourcing.DecodedEvent,
		) (eventsourcing.EncodedEvent, error) {
			panic(secret)
		},
		decode: func(
			eventsourcing.EncodedEvent,
		) (eventsourcing.DecodedEvent, error) {
			panic(secret)
		},
		encodeContext: func(
			context.Context,
			eventsourcing.DecodedEvent,
		) (eventsourcing.EncodedEvent, error) {
			panic(secret)
		},
		decodeContext: func(
			context.Context,
			eventsourcing.EncodedEvent,
		) (eventsourcing.DecodedEvent, error) {
			panic(secret)
		},
	})
	if err != nil {
		t.Fatalf("WrapPayloadCodec(panicking) error = %v", err)
	}
	assertPanicPreserved(t, secret, func() {
		_, _ = panicking.EncodeContext(context.Background(), decoded)
	})
	assertPanicPreserved(t, secret, func() {
		_, _ = panicking.Decode(encoded)
	})
	spans := recorder.Ended()
	if strings.Contains(fmt.Sprint(spans), secret.Error()) {
		t.Fatal("codec telemetry disclosed failure diagnostics")
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertOperationMetric(t, metrics, "codec_encode", "error", 1)
	assertOperationMetric(t, metrics, "codec_decode", "error", 1)
	assertOperationMetric(t, metrics, "codec_encode", "panic", 1)
	assertOperationMetric(t, metrics, "codec_decode", "panic", 1)
}

func TestUpcasterInstrumentationPreservesCallsAndContext(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(testRuntime{
		tracer: sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(recorder),
		),
		meter:      sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, encoded := telemetrySerializationEvents(t)
	input, err := eventsourcing.NewUpcastEvent(
		encoded,
		map[string]string{"secret": "metadata"},
	)
	if err != nil {
		t.Fatalf("NewUpcastEvent() error = %v", err)
	}
	var downstream trace.SpanContext
	var contextValue any
	pureCalls := 0
	contextCalls := 0
	wrapped, err := instrumentation.WrapUpcaster(telemetryUpcaster{
		upcast: func(
			event eventsourcing.UpcastEvent,
		) ([]eventsourcing.UpcastEvent, error) {
			pureCalls++

			return []eventsourcing.UpcastEvent{event}, nil
		},
		upcastContext: func(
			ctx context.Context,
			event eventsourcing.UpcastEvent,
		) ([]eventsourcing.UpcastEvent, error) {
			contextCalls++
			downstream = trace.SpanContextFromContext(ctx)
			contextValue = ctx.Value(serializationContextKey{})

			return []eventsourcing.UpcastEvent{event, event}, nil
		},
	})
	if err != nil {
		t.Fatalf("WrapUpcaster() error = %v", err)
	}
	if output, err := wrapped.Upcast(input); err != nil || len(output) != 1 {
		t.Fatalf("Upcast() = %#v, %v", output, err)
	}
	parentCtx, parent := instrumentation.tracer.Start(
		context.WithValue(
			context.Background(),
			serializationContextKey{},
			"caller-value",
		),
		"parent",
	)
	output, err := wrapped.UpcastContext(parentCtx, input)
	if err != nil || len(output) != 2 {
		t.Fatalf("UpcastContext() = %#v, %v", output, err)
	}
	parent.End()
	if pureCalls != 1 || contextCalls != 1 ||
		downstream.TraceID() != parent.SpanContext().TraceID() ||
		contextValue != "caller-value" {
		t.Fatal("upcaster instrumentation did not preserve calls or context")
	}
	spans := recorder.Ended()
	if len(spans) != 3 ||
		spans[0].Name() != "event_sourcing.upcast" ||
		projectionSpanInt64(
			spans[0],
			"event_sourcing.upcast.output_count",
		) != 1 ||
		spans[1].Name() != "event_sourcing.upcast" ||
		projectionSpanInt64(
			spans[1],
			"event_sourcing.upcast.output_count",
		) != 2 {
		t.Fatalf("upcaster spans = %#v", spans)
	}
	diagnostics := fmt.Sprint(spans)
	if strings.Contains(diagnostics, "secret.event") ||
		strings.Contains(diagnostics, "secret-payload") ||
		strings.Contains(diagnostics, "metadata") {
		t.Fatal("upcaster telemetry disclosed event data")
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertOperationMetric(t, metrics, "upcast", "success", 2)
}

func TestUpcasterInstrumentationPreservesFailuresAndPanics(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(testRuntime{
		tracer: sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(recorder),
		),
		meter:      sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, encoded := telemetrySerializationEvents(t)
	input, err := eventsourcing.NewUpcastEvent(encoded, nil)
	if err != nil {
		t.Fatalf("NewUpcastEvent() error = %v", err)
	}
	secret := errors.New("secret upcaster failure")
	failing, err := instrumentation.WrapUpcaster(telemetryUpcaster{
		upcast: func(
			eventsourcing.UpcastEvent,
		) ([]eventsourcing.UpcastEvent, error) {
			return nil, secret
		},
		upcastContext: func(
			context.Context,
			eventsourcing.UpcastEvent,
		) ([]eventsourcing.UpcastEvent, error) {
			return nil, secret
		},
	})
	if err != nil {
		t.Fatalf("WrapUpcaster(failing) error = %v", err)
	}
	if _, err := failing.Upcast(input); !errors.Is(err, secret) {
		t.Fatalf("Upcast() error = %v", err)
	}
	panicking, err := instrumentation.WrapUpcaster(telemetryUpcaster{
		upcast: func(
			eventsourcing.UpcastEvent,
		) ([]eventsourcing.UpcastEvent, error) {
			panic(secret)
		},
		upcastContext: func(
			context.Context,
			eventsourcing.UpcastEvent,
		) ([]eventsourcing.UpcastEvent, error) {
			panic(secret)
		},
	})
	if err != nil {
		t.Fatalf("WrapUpcaster(panicking) error = %v", err)
	}
	assertPanicPreserved(t, secret, func() {
		_, _ = panicking.UpcastContext(context.Background(), input)
	})
	spans := recorder.Ended()
	if strings.Contains(fmt.Sprint(spans), secret.Error()) {
		t.Fatal("upcaster telemetry disclosed failure diagnostics")
	}
	for _, span := range spans {
		if projectionSpanInt64(
			span,
			"event_sourcing.upcast.output_count",
		) != -1 {
			t.Fatal("failed upcast reported output count")
		}
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertOperationMetric(t, metrics, "upcast", "error", 1)
	assertOperationMetric(t, metrics, "upcast", "panic", 1)
}

func TestSerializationInstrumentationRejectsInvalidCalls(t *testing.T) {
	t.Parallel()

	instrumentation := newKafkaTestInstrumentation(
		t,
		propagation.TraceContext{},
	)
	codec := telemetryPayloadCodec{
		encode: func(
			eventsourcing.DecodedEvent,
		) (eventsourcing.EncodedEvent, error) {
			return eventsourcing.EncodedEvent{}, nil
		},
		decode: func(
			eventsourcing.EncodedEvent,
		) (eventsourcing.DecodedEvent, error) {
			return eventsourcing.DecodedEvent{}, nil
		},
		encodeContext: func(
			context.Context,
			eventsourcing.DecodedEvent,
		) (eventsourcing.EncodedEvent, error) {
			return eventsourcing.EncodedEvent{}, nil
		},
		decodeContext: func(
			context.Context,
			eventsourcing.EncodedEvent,
		) (eventsourcing.DecodedEvent, error) {
			return eventsourcing.DecodedEvent{}, nil
		},
	}
	upcaster := telemetryUpcaster{
		upcast: func(
			eventsourcing.UpcastEvent,
		) ([]eventsourcing.UpcastEvent, error) {
			return nil, nil
		},
		upcastContext: func(
			context.Context,
			eventsourcing.UpcastEvent,
		) ([]eventsourcing.UpcastEvent, error) {
			return nil, nil
		},
	}
	if _, err := instrumentation.WrapPayloadCodec(nil); !errors.Is(
		err,
		ErrPayloadCodecRequired,
	) {
		t.Fatalf("nil codec error = %v", err)
	}
	if _, err := instrumentation.WrapUpcaster(nil); !errors.Is(
		err,
		ErrUpcasterRequired,
	) {
		t.Fatalf("nil upcaster error = %v", err)
	}
	var nilInstrumentation *Instrumentation
	if _, err := nilInstrumentation.WrapPayloadCodec(codec); !errors.Is(
		err,
		ErrRuntimeRequired,
	) {
		t.Fatalf("nil instrumentation codec error = %v", err)
	}
	if _, err := nilInstrumentation.WrapUpcaster(upcaster); !errors.Is(
		err,
		ErrRuntimeRequired,
	) {
		t.Fatalf("nil instrumentation upcaster error = %v", err)
	}
	wrappedCodec, err := instrumentation.WrapPayloadCodec(codec)
	if err != nil {
		t.Fatalf("WrapPayloadCodec() error = %v", err)
	}
	if _, err := wrappedCodec.EncodeContext(
		nilContext(),
		eventsourcing.DecodedEvent{},
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil codec context error = %v", err)
	}
	if _, err := wrappedCodec.DecodeContext(
		nilContext(),
		eventsourcing.EncodedEvent{},
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil codec decode context error = %v", err)
	}
	wrappedUpcaster, err := instrumentation.WrapUpcaster(upcaster)
	if err != nil {
		t.Fatalf("WrapUpcaster() error = %v", err)
	}
	if _, err := wrappedUpcaster.UpcastContext(
		nilContext(),
		eventsourcing.UpcastEvent{},
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil upcaster context error = %v", err)
	}
}

func TestSerializationInstrumentationSupportsLegacyContracts(t *testing.T) {
	t.Parallel()

	instrumentation := newKafkaTestInstrumentation(
		t,
		propagation.TraceContext{},
	)
	decoded, encoded := telemetrySerializationEvents(t)
	nextCodec := &legacyTelemetryPayloadCodec{
		decoded: decoded,
		encoded: encoded,
	}
	codec, err := instrumentation.WrapPayloadCodec(nextCodec)
	if err != nil {
		t.Fatalf("WrapPayloadCodec() error = %v", err)
	}
	if got, err := codec.EncodeContext(
		context.Background(),
		decoded,
	); err != nil || got.Name() != encoded.Name() {
		t.Fatalf("EncodeContext() = %#v, %v", got, err)
	}
	if got, err := codec.DecodeContext(
		context.Background(),
		encoded,
	); err != nil || got.Name() != decoded.Name() {
		t.Fatalf("DecodeContext() = %#v, %v", got, err)
	}
	if nextCodec.encodeCalls != 1 || nextCodec.decodeCalls != 1 {
		t.Fatalf(
			"legacy codec calls = %d/%d",
			nextCodec.encodeCalls,
			nextCodec.decodeCalls,
		)
	}
	input, err := eventsourcing.NewUpcastEvent(encoded, nil)
	if err != nil {
		t.Fatalf("NewUpcastEvent() error = %v", err)
	}
	upcastCalls := 0
	wrapped, err := instrumentation.WrapUpcaster(
		legacyTelemetryUpcaster(func(
			event eventsourcing.UpcastEvent,
		) ([]eventsourcing.UpcastEvent, error) {
			upcastCalls++

			return []eventsourcing.UpcastEvent{event}, nil
		}),
	)
	if err != nil {
		t.Fatalf("WrapUpcaster() error = %v", err)
	}
	if output, err := wrapped.UpcastContext(
		context.Background(),
		input,
	); err != nil || len(output) != 1 || upcastCalls != 1 {
		t.Fatalf(
			"UpcastContext() = %#v, %v; calls = %d",
			output,
			err,
			upcastCalls,
		)
	}
}

func telemetrySerializationEvents(
	t testing.TB,
) (eventsourcing.DecodedEvent, eventsourcing.EncodedEvent) {
	t.Helper()

	decoded, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    "secret.event",
			Version: 1,
			Value:   struct{ Secret string }{Secret: "secret-value"},
		},
	)
	if err != nil {
		t.Fatalf("NewDecodedEvent() error = %v", err)
	}
	encoded, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "secret.event",
			Version:     1,
			ContentType: eventsourcing.JSONContentType,
			Payload:     []byte(`{"secret":"secret-payload"}`),
		},
	)
	if err != nil {
		t.Fatalf("NewEncodedEvent() error = %v", err)
	}

	return decoded, encoded
}
