package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestSnapshotStoreInstrumentationMeasuresLifecycleOutcomes(t *testing.T) {
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
	snapshot := telemetrySnapshot(t)
	next := &telemetrySnapshotStore{loaded: snapshot}
	store, err := instrumentation.WrapSnapshotStore(next)
	if err != nil {
		t.Fatalf("WrapSnapshotStore() error = %v", err)
	}

	parentCtx, parent := instrumentation.tracer.Start(
		context.Background(),
		"parent",
	)
	loaded, err := store.Load(parentCtx, snapshot.Stream())
	if err != nil || !loaded.Equal(snapshot) {
		t.Fatalf("Load(hit) = %#v, %v", loaded, err)
	}
	next.loadErr = eventsourcing.ErrSnapshotNotFound
	if _, err := store.Load(parentCtx, snapshot.Stream()); !errors.Is(
		err,
		eventsourcing.ErrSnapshotNotFound,
	) {
		t.Fatalf("Load(miss) error = %v", err)
	}
	next.loadErr = nil
	if err := store.Save(parentCtx, snapshot); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	next.saveErr = eventsourcing.ErrSnapshotStale
	if err := store.Save(parentCtx, snapshot); !errors.Is(
		err,
		eventsourcing.ErrSnapshotStale,
	) {
		t.Fatalf("Save(stale) error = %v", err)
	}
	if err := store.Delete(parentCtx, snapshot.Stream()); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	parent.End()

	if len(next.contexts) != 5 {
		t.Fatalf("downstream context count = %d", len(next.contexts))
	}
	for _, spanContext := range next.contexts {
		if spanContext.TraceID() != parent.SpanContext().TraceID() {
			t.Fatal("downstream snapshot call did not receive operation context")
		}
	}

	spans := recorder.Ended()
	if len(spans) != 6 {
		t.Fatalf("span count = %d", len(spans))
	}
	wantNames := []string{
		"event_sourcing.snapshot.load",
		"event_sourcing.snapshot.load",
		"event_sourcing.snapshot.save",
		"event_sourcing.snapshot.save",
		"event_sourcing.snapshot.delete",
	}
	for index, want := range wantNames {
		if spans[index].Name() != want ||
			spans[index].Parent().TraceID() != parent.SpanContext().TraceID() {
			t.Fatalf("span %d = %q", index, spans[index].Name())
		}
	}
	if telemetry := fmt.Sprint(spans); strings.Contains(
		telemetry,
		snapshot.Stream().AggregateID(),
	) {
		t.Fatal("snapshot spans disclosed aggregate identity")
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertOperationMetric(t, metrics, "snapshot_load", "hit", 1)
	assertOperationMetric(t, metrics, "snapshot_load", "miss", 1)
	assertOperationMetric(t, metrics, "snapshot_save", "success", 1)
	assertOperationMetric(t, metrics, "snapshot_save", "stale", 1)
	assertOperationMetric(t, metrics, "snapshot_delete", "success", 1)
}

func TestSnapshotStoreInstrumentationPreservesErrorsAndPanics(t *testing.T) {
	t.Parallel()

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
	secret := errors.New("snapshot secret")
	snapshot := telemetrySnapshot(t)
	next := &telemetrySnapshotStore{
		loadErr:    secret,
		saveErr:    secret,
		deleteErr:  secret,
		panicValue: secret,
	}
	store, err := instrumentation.WrapSnapshotStore(next)
	if err != nil {
		t.Fatalf("WrapSnapshotStore() error = %v", err)
	}
	if _, err := store.Load(
		context.Background(),
		snapshot.Stream(),
	); !errors.Is(err, secret) {
		t.Fatalf("Load() error = %v", err)
	}
	if err := store.Save(context.Background(), snapshot); !errors.Is(err, secret) {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Delete(
		context.Background(),
		snapshot.Stream(),
	); !errors.Is(err, secret) {
		t.Fatalf("Delete() error = %v", err)
	}

	for _, operation := range []string{"load", "save", "delete"} {
		next.loadErr = nil
		next.saveErr = nil
		next.deleteErr = nil
		next.panicOperation = operation
		assertPanicPreserved(t, secret, func() {
			switch operation {
			case "load":
				_, _ = store.Load(context.Background(), snapshot.Stream())
			case "save":
				_ = store.Save(context.Background(), snapshot)
			case "delete":
				_ = store.Delete(context.Background(), snapshot.Stream())
			}
		})
	}
	if telemetry := fmt.Sprint(recorder.Ended()); strings.Contains(
		telemetry,
		secret.Error(),
	) {
		t.Fatal("snapshot telemetry disclosed failure diagnostics")
	}
}

func TestSnapshotStoreInstrumentationRejectsInvalidCalls(t *testing.T) {
	t.Parallel()

	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	if _, err := instrumentation.WrapSnapshotStore(nil); !errors.Is(
		err,
		ErrSnapshotStoreRequired,
	) {
		t.Fatalf("nil store error = %v", err)
	}
	var nilInstrumentation *Instrumentation
	if _, err := nilInstrumentation.WrapSnapshotStore(
		&telemetrySnapshotStore{},
	); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("nil instrumentation error = %v", err)
	}
	store, err := instrumentation.WrapSnapshotStore(&telemetrySnapshotStore{})
	if err != nil {
		t.Fatalf("WrapSnapshotStore() error = %v", err)
	}
	if _, err := store.Load(nilContext(), eventsourcing.StreamID{}); !errors.Is(
		err,
		ErrContextRequired,
	) {
		t.Fatalf("Load(nil) error = %v", err)
	}
	if err := store.Save(nilContext(), eventsourcing.Snapshot{}); !errors.Is(
		err,
		ErrContextRequired,
	) {
		t.Fatalf("Save(nil) error = %v", err)
	}
	if err := store.Delete(nilContext(), eventsourcing.StreamID{}); !errors.Is(
		err,
		ErrContextRequired,
	) {
		t.Fatalf("Delete(nil) error = %v", err)
	}
}

type telemetrySnapshotStore struct {
	loaded         eventsourcing.Snapshot
	loadErr        error
	saveErr        error
	deleteErr      error
	panicOperation string
	panicValue     any
	contexts       []trace.SpanContext
}

func (store *telemetrySnapshotStore) Load(
	ctx context.Context,
	_ eventsourcing.StreamID,
) (eventsourcing.Snapshot, error) {
	store.contexts = append(store.contexts, trace.SpanContextFromContext(ctx))
	if store.panicOperation == "load" {
		panic(store.panicValue)
	}

	return store.loaded, store.loadErr
}

func (store *telemetrySnapshotStore) Save(
	ctx context.Context,
	_ eventsourcing.Snapshot,
) error {
	store.contexts = append(store.contexts, trace.SpanContextFromContext(ctx))
	if store.panicOperation == "save" {
		panic(store.panicValue)
	}

	return store.saveErr
}

func (store *telemetrySnapshotStore) Delete(
	ctx context.Context,
	_ eventsourcing.StreamID,
) error {
	store.contexts = append(store.contexts, trace.SpanContextFromContext(ctx))
	if store.panicOperation == "delete" {
		panic(store.panicValue)
	}

	return store.deleteErr
}

func telemetrySnapshot(t testing.TB) eventsourcing.Snapshot {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "secret-aggregate")
	if err != nil {
		t.Fatalf("NewStreamID() error = %v", err)
	}
	snapshot, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           stream,
		AggregateVersion: 7,
		SchemaVersion:    2,
		State:            []byte(`{"secret":"state"}`),
		Metadata:         map[string]string{"secret": "metadata"},
		CreatedAt:        time.Date(2026, time.July, 27, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	return snapshot
}
