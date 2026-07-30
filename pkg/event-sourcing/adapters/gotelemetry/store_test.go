package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestStoreInstrumentationMeasuresCompleteOperations(t *testing.T) {
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
	streamIterator := &telemetryIterator{
		messages: []eventsourcing.Message{{}, {}},
	}
	store := &telemetryStore{
		appended: []eventsourcing.Message{{}},
		iterator: streamIterator,
	}
	wrappedStore, err := instrumentation.WrapEventStore(store)
	if err != nil {
		t.Fatalf("WrapEventStore() error = %v", err)
	}
	parentCtx, parent := instrumentation.tracer.Start(
		context.Background(),
		"parent",
	)
	appended, err := wrappedStore.Append(
		parentCtx,
		eventsourcing.StreamID{},
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{{}, {}},
	)
	if err != nil || len(appended) != 1 {
		t.Fatalf("Append() = %d, %v", len(appended), err)
	}
	iterator, err := wrappedStore.ReadStream(
		parentCtx,
		eventsourcing.StreamID{},
		eventsourcing.ReadStreamOptions{},
	)
	if err != nil {
		t.Fatalf("ReadStream() error = %v", err)
	}
	for iterator.Next(parentCtx) {
		_ = iterator.Message()
	}
	if streamIterator.nextSpan.TraceID() != parent.SpanContext().TraceID() {
		t.Fatal("stream iterator did not receive the read span context")
	}
	if err := iterator.Err(); err != nil {
		t.Fatalf("iterator error = %v", err)
	}
	if err := iterator.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	globalStore := &telemetryGlobalReader{
		iterator: &telemetryIterator{messages: []eventsourcing.Message{{}}},
	}
	wrappedGlobal, err := instrumentation.WrapGlobalReader(globalStore)
	if err != nil {
		t.Fatalf("WrapGlobalReader() error = %v", err)
	}
	global, err := wrappedGlobal.ReadGlobal(
		parentCtx,
		eventsourcing.ReadGlobalOptions{},
	)
	if err != nil {
		t.Fatalf("ReadGlobal() error = %v", err)
	}
	if !global.Next(parentCtx) {
		t.Fatal("global iterator ended before message")
	}
	_ = global.Message()
	if err := global.Close(); err != nil {
		t.Fatalf("global Close() error = %v", err)
	}
	parent.End()

	spans := recorder.Ended()
	if len(spans) != 4 {
		t.Fatalf("span count = %d", len(spans))
	}
	assertStoreSpan(t, spans[0], "event_sourcing.store.append", "append", 2)
	assertStoreSpan(
		t,
		spans[1],
		"event_sourcing.store.read_stream",
		"read_stream",
		2,
	)
	assertStoreSpan(
		t,
		spans[2],
		"event_sourcing.store.read_global",
		"read_global",
		1,
	)
	if strings.Contains(fmt.Sprint(spans), "secret") {
		t.Fatal("store spans disclosed data")
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertOperationMetric(t, metrics, "append", "success", 1)
	assertOperationMetric(t, metrics, "read_stream", "success", 1)
	assertOperationMetric(t, metrics, "read_global", "success", 1)
}

func TestStoreInstrumentationPreservesFailuresAndPanics(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	instrumentation, err := New(testRuntime{
		tracer: sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(recorder),
		),
		meter:      sdkmetric.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	want := errors.New("secret database failure")
	wrappedStore, err := instrumentation.WrapEventStore(&telemetryStore{
		appendErr: want,
		readErr:   want,
	})
	if err != nil {
		t.Fatalf("WrapEventStore() error = %v", err)
	}
	if _, err := wrappedStore.Append(
		context.Background(),
		eventsourcing.StreamID{},
		eventsourcing.ExpectedVersion{},
		nil,
	); !errors.Is(err, want) {
		t.Fatalf("Append() error = %v", err)
	}
	if _, err := wrappedStore.ReadStream(
		context.Background(),
		eventsourcing.StreamID{},
		eventsourcing.ReadStreamOptions{},
	); !errors.Is(err, want) {
		t.Fatalf("ReadStream() error = %v", err)
	}

	panicStore, err := instrumentation.WrapEventStore(&telemetryStore{
		panicValue: "secret panic",
	})
	if err != nil {
		t.Fatalf("WrapEventStore() error = %v", err)
	}
	assertStorePanic(t, "secret panic", func() {
		_, _ = panicStore.Append(
			context.Background(),
			eventsourcing.StreamID{},
			eventsourcing.ExpectedVersion{},
			nil,
		)
	})
	panicRead, err := instrumentation.WrapEventStore(&telemetryStore{
		readPanic: "read panic",
	})
	if err != nil {
		t.Fatalf("WrapEventStore() error = %v", err)
	}
	assertStorePanic(t, "read panic", func() {
		_, _ = panicRead.ReadStream(
			context.Background(),
			eventsourcing.StreamID{},
			eventsourcing.ReadStreamOptions{},
		)
	})
	if strings.Contains(fmt.Sprint(recorder.Ended()), "secret") {
		t.Fatal("store failure telemetry disclosed diagnostics")
	}
}

func TestInstrumentedIteratorPreservesErrorsCloseAndPanics(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	instrumentation, err := New(testRuntime{
		tracer: sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(recorder),
		),
		meter:      sdkmetric.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	want := errors.New("iterator failure")
	next := &telemetryIterator{err: want, closeErr: want}
	store, err := instrumentation.WrapEventStore(&telemetryStore{iterator: next})
	if err != nil {
		t.Fatalf("WrapEventStore() error = %v", err)
	}
	iterator, err := store.ReadStream(
		context.Background(),
		eventsourcing.StreamID{},
		eventsourcing.ReadStreamOptions{},
	)
	if err != nil {
		t.Fatalf("ReadStream() error = %v", err)
	}
	if iterator.Next(context.Background()) {
		t.Fatal("Next() = true")
	}
	assertAllSpansError(t, recorder.Ended())
	if !errors.Is(iterator.Err(), want) {
		t.Fatalf("Err() = %v", iterator.Err())
	}
	if err := iterator.Close(); !errors.Is(err, want) {
		t.Fatalf("Close() error = %v", err)
	}

	panicIterator := &telemetryIterator{panicNext: "next panic"}
	store, err = instrumentation.WrapEventStore(
		&telemetryStore{iterator: panicIterator},
	)
	if err != nil {
		t.Fatalf("WrapEventStore() error = %v", err)
	}
	iterator, err = store.ReadStream(
		context.Background(),
		eventsourcing.StreamID{},
		eventsourcing.ReadStreamOptions{},
	)
	if err != nil {
		t.Fatalf("ReadStream() error = %v", err)
	}
	assertStorePanic(t, "next panic", func() {
		iterator.Next(context.Background())
	})

	closeIterator := &telemetryIterator{panicClose: "close panic"}
	store, err = instrumentation.WrapEventStore(
		&telemetryStore{iterator: closeIterator},
	)
	if err != nil {
		t.Fatalf("WrapEventStore() error = %v", err)
	}
	iterator, err = store.ReadStream(
		context.Background(),
		eventsourcing.StreamID{},
		eventsourcing.ReadStreamOptions{},
	)
	if err != nil {
		t.Fatalf("ReadStream() error = %v", err)
	}
	assertStorePanic(t, "close panic", func() {
		_ = iterator.Close()
	})

	for name, test := range map[string]struct {
		iterator *telemetryIterator
		call     func(eventsourcing.MessageIterator)
		want     any
	}{
		"message": {
			iterator: &telemetryIterator{panicMessage: "message panic"},
			call: func(iterator eventsourcing.MessageIterator) {
				_ = iterator.Message()
			},
			want: "message panic",
		},
		"error": {
			iterator: &telemetryIterator{panicErr: "error panic"},
			call: func(iterator eventsourcing.MessageIterator) {
				_ = iterator.Err()
			},
			want: "error panic",
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, err := instrumentation.WrapEventStore(
				&telemetryStore{iterator: test.iterator},
			)
			if err != nil {
				t.Fatalf("WrapEventStore() error = %v", err)
			}
			iterator, err := store.ReadStream(
				context.Background(),
				eventsourcing.StreamID{},
				eventsourcing.ReadStreamOptions{},
			)
			if err != nil {
				t.Fatalf("ReadStream() error = %v", err)
			}
			assertStorePanic(t, test.want, func() {
				test.call(iterator)
			})
		})
	}
}

func TestStoreInstrumentationValidatesDependenciesAndContexts(t *testing.T) {
	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	if _, err := instrumentation.WrapEventStore(nil); !errors.Is(err, ErrEventStoreRequired) {
		t.Fatalf("nil store error = %v", err)
	}
	if _, err := instrumentation.WrapGlobalReader(nil); !errors.Is(err, ErrGlobalReaderRequired) {
		t.Fatalf("nil global reader error = %v", err)
	}
	var missing *Instrumentation
	if _, err := missing.WrapEventStore(&telemetryStore{}); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("nil instrumentation error = %v", err)
	}
	if _, err := missing.WrapGlobalReader(&telemetryGlobalReader{}); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("nil global instrumentation error = %v", err)
	}
	store, err := instrumentation.WrapEventStore(&telemetryStore{
		iterator: &telemetryIterator{},
	})
	if err != nil {
		t.Fatalf("WrapEventStore() error = %v", err)
	}
	if _, err := store.Append(
		nilContext(),
		eventsourcing.StreamID{},
		eventsourcing.ExpectedVersion{},
		nil,
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil append context error = %v", err)
	}
	if _, err := store.ReadStream(
		nilContext(),
		eventsourcing.StreamID{},
		eventsourcing.ReadStreamOptions{},
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil read context error = %v", err)
	}
	iterator, err := store.ReadStream(
		context.Background(),
		eventsourcing.StreamID{},
		eventsourcing.ReadStreamOptions{},
	)
	if err != nil {
		t.Fatalf("ReadStream() error = %v", err)
	}
	if iterator.Next(nilContext()) {
		t.Fatal("Next(nil) = true")
	}
	if !errors.Is(iterator.Err(), ErrContextRequired) {
		t.Fatalf("Err() = %v", iterator.Err())
	}

	nilIteratorStore, err := instrumentation.WrapEventStore(&telemetryStore{})
	if err != nil {
		t.Fatalf("WrapEventStore() error = %v", err)
	}
	if _, err := nilIteratorStore.ReadStream(
		context.Background(),
		eventsourcing.StreamID{},
		eventsourcing.ReadStreamOptions{},
	); !errors.Is(err, ErrMessageIteratorRequired) {
		t.Fatalf("nil iterator error = %v", err)
	}
}

func TestGlobalReaderInstrumentationPreservesFailuresAndContexts(t *testing.T) {
	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	want := errors.New("global failure")
	reader, err := instrumentation.WrapGlobalReader(&telemetryGlobalReader{
		err: want,
	})
	if err != nil {
		t.Fatalf("WrapGlobalReader() error = %v", err)
	}
	if _, err := reader.ReadGlobal(
		context.Background(),
		eventsourcing.ReadGlobalOptions{},
	); !errors.Is(err, want) {
		t.Fatalf("ReadGlobal() error = %v", err)
	}
	if _, err := reader.ReadGlobal(
		nilContext(),
		eventsourcing.ReadGlobalOptions{},
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil context error = %v", err)
	}

	reader, err = instrumentation.WrapGlobalReader(&telemetryGlobalReader{})
	if err != nil {
		t.Fatalf("WrapGlobalReader() error = %v", err)
	}
	if _, err := reader.ReadGlobal(
		context.Background(),
		eventsourcing.ReadGlobalOptions{},
	); !errors.Is(err, ErrMessageIteratorRequired) {
		t.Fatalf("nil iterator error = %v", err)
	}

	reader, err = instrumentation.WrapGlobalReader(&telemetryGlobalReader{
		panicValue: "global panic",
	})
	if err != nil {
		t.Fatalf("WrapGlobalReader() error = %v", err)
	}
	assertStorePanic(t, "global panic", func() {
		_, _ = reader.ReadGlobal(
			context.Background(),
			eventsourcing.ReadGlobalOptions{},
		)
	})
}

type telemetryStore struct {
	appended   []eventsourcing.Message
	appendErr  error
	readErr    error
	iterator   eventsourcing.MessageIterator
	panicValue any
	readPanic  any
}

func (store *telemetryStore) Append(
	context.Context,
	eventsourcing.StreamID,
	eventsourcing.ExpectedVersion,
	[]eventsourcing.PendingMessage,
) ([]eventsourcing.Message, error) {
	if store.panicValue != nil {
		panic(store.panicValue)
	}
	return store.appended, store.appendErr
}

func (store *telemetryStore) ReadStream(
	context.Context,
	eventsourcing.StreamID,
	eventsourcing.ReadStreamOptions,
) (eventsourcing.MessageIterator, error) {
	if store.readPanic != nil {
		panic(store.readPanic)
	}
	return store.iterator, store.readErr
}

type telemetryGlobalReader struct {
	iterator   eventsourcing.MessageIterator
	err        error
	panicValue any
	read       func(
		context.Context,
		eventsourcing.ReadGlobalOptions,
	) (eventsourcing.MessageIterator, error)
}

func (reader *telemetryGlobalReader) ReadGlobal(
	ctx context.Context,
	options eventsourcing.ReadGlobalOptions,
) (eventsourcing.MessageIterator, error) {
	if reader.panicValue != nil {
		panic(reader.panicValue)
	}
	if reader.read != nil {
		return reader.read(ctx, options)
	}
	return reader.iterator, reader.err
}

type telemetryIterator struct {
	messages     []eventsourcing.Message
	index        int
	err          error
	closeErr     error
	closed       bool
	panicNext    any
	panicClose   any
	panicMessage any
	panicErr     any
	nextSpan     trace.SpanContext
}

func (iterator *telemetryIterator) Next(ctx context.Context) bool {
	iterator.nextSpan = trace.SpanContextFromContext(ctx)
	if iterator.panicNext != nil {
		panic(iterator.panicNext)
	}
	if iterator.index >= len(iterator.messages) {
		return false
	}
	iterator.index++
	return true
}

func (iterator *telemetryIterator) Message() eventsourcing.Message {
	if iterator.panicMessage != nil {
		panic(iterator.panicMessage)
	}
	if iterator.index == 0 || iterator.index > len(iterator.messages) {
		return eventsourcing.Message{}
	}
	return iterator.messages[iterator.index-1]
}

func (iterator *telemetryIterator) Err() error {
	if iterator.panicErr != nil {
		panic(iterator.panicErr)
	}
	return iterator.err
}

func (iterator *telemetryIterator) Close() error {
	if iterator.panicClose != nil {
		panic(iterator.panicClose)
	}
	iterator.closed = true
	return iterator.closeErr
}

func assertStoreSpan(
	t *testing.T,
	span sdktrace.ReadOnlySpan,
	name string,
	operation string,
	count int64,
) {
	t.Helper()
	if span.Name() != name {
		t.Fatalf("span name = %q", span.Name())
	}
	attributes := make(map[attribute.Key]attribute.Value)
	for _, item := range span.Attributes() {
		attributes[item.Key] = item.Value
	}
	if attributes["event_sourcing.operation"].AsString() != operation ||
		attributes["event_sourcing.message.count"].AsInt64() != count ||
		span.Status().Code.String() != "Unset" {
		t.Fatalf("span = %#v", span)
	}
}

func assertStorePanic(t *testing.T, want any, operation func()) {
	t.Helper()
	defer func() {
		if got := recover(); got != want {
			t.Fatalf("panic = %#v", got)
		}
	}()
	operation()
}
