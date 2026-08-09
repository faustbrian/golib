package audit_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/faustbrian/golib/pkg/audit/memory"
)

func TestRecorderEmitsIdentifierFreePanicContainedObservations(t *testing.T) {
	t.Parallel()
	var nilObserver audit.ObserverFunc
	nilObserver.Observe(context.Background(), audit.Observation{})
	for index := 0; index < reflect.TypeOf(audit.Observation{}).NumField(); index++ {
		name := strings.ToLower(reflect.TypeOf(audit.Observation{}).Field(index).Name)
		for _, prohibited := range []string{"actor", "subject", "tenant", "record", "identifier", "id"} {
			if strings.Contains(name, prohibited) {
				t.Fatalf("observation field %q permits unbounded identity", name)
			}
		}
	}

	sink, err := memory.New(memory.Config{MaxRecords: 2, MaxBytes: 1 << 20, MaxBatchRecords: 2})
	if err != nil {
		t.Fatal(err)
	}
	redactor, err := audit.NewRedactor(audit.RedactionRules{AllowedChanges: []string{"status"}})
	if err != nil {
		t.Fatal(err)
	}
	observed := make([]audit.ObservationKind, 0, 2)
	recorder, err := audit.NewRecorder(audit.RecorderConfig{
		Sink: sink, Redactor: redactor, Mode: audit.DeliveryFailClosed,
		Clock:          func() time.Time { return time.Date(2026, time.August, 9, 12, 0, 2, 0, time.UTC) },
		DelayThreshold: time.Second,
		Observer: audit.ObserverFunc(func(_ context.Context, observation audit.Observation) {
			observed = append(observed, observation.Kind)
			if observation.Kind == audit.ObservationAccepted {
				panic("observer failure")
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := recorder.Submit(context.Background(), deliveryRecord(t, "observed-record"))
	if err != nil || result.Disposition != audit.DeliveryPersisted {
		t.Fatalf("Submit() = %#v, %v", result, err)
	}
	if len(observed) != 2 || observed[0] != audit.ObservationDelayed || observed[1] != audit.ObservationAccepted {
		t.Fatalf("observations = %#v", observed)
	}
}

type exporterFunc func(context.Context, audit.Query, func(audit.Record) error) error

func (export exporterFunc) Export(ctx context.Context, query audit.Query, consume func(audit.Record) error) error {
	return export(ctx, query, consume)
}

func TestObservedExporterReportsOnlyBoundedCounts(t *testing.T) {
	t.Parallel()
	store, err := memory.New(memory.Config{MaxRecords: 2, MaxBytes: 1 << 20, MaxBatchRecords: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendBatch(context.Background(), []audit.Record{
		deliveryRecord(t, "export-1"), deliveryRecord(t, "export-2"),
	}); err != nil {
		t.Fatal(err)
	}
	var observation audit.Observation
	exporter, err := audit.NewObservedExporter(store, audit.ObserverFunc(func(_ context.Context, value audit.Observation) {
		observation = value
	}))
	if err != nil {
		t.Fatal(err)
	}
	query, err := audit.NewQuery(audit.QueryInput{Tenant: audit.NoTenant(), Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := exporter.Export(context.Background(), query, func(audit.Record) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	if count != 2 || observation.Kind != audit.ObservationExported || observation.Count != 2 {
		t.Fatalf("export count/observation = %d, %#v", count, observation)
	}
}

func TestObservedExporterReportsPartialFailuresAndValidatesCalls(t *testing.T) {
	t.Parallel()

	observer := audit.ObserverFunc(func(context.Context, audit.Observation) {})
	if _, err := audit.NewObservedExporter(nil, observer); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("NewObservedExporter(nil) error = %v", err)
	}
	store, _ := memory.New(memory.Config{MaxRecords: 1, MaxBytes: 1 << 20, MaxBatchRecords: 1})
	if _, err := audit.NewObservedExporter(store, nil); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("NewObservedExporter(nil observer) error = %v", err)
	}

	record := deliveryRecord(t, "partial-export")
	exportFailure := errors.New("source failed")
	var observation audit.Observation
	decorated, err := audit.NewObservedExporter(exporterFunc(func(ctx context.Context, query audit.Query, consume func(audit.Record) error) error {
		if err := consume(record); err != nil {
			return err
		}
		return exportFailure
	}), audit.ObserverFunc(func(_ context.Context, value audit.Observation) { observation = value }))
	if err != nil {
		t.Fatal(err)
	}
	query, _ := audit.NewQuery(audit.QueryInput{Tenant: audit.NoTenant(), Limit: 1})
	if err := decorated.Export(context.Background(), query, func(audit.Record) error { return nil }); !errors.Is(err, exportFailure) {
		t.Fatalf("failed Export() error = %v", err)
	}
	if observation.Kind != audit.ObservationFailed || observation.Count != 1 {
		t.Fatalf("failure observation = %#v", observation)
	}
	callbackFailure := errors.New("consumer failed")
	if err := decorated.Export(context.Background(), query, func(audit.Record) error { return callbackFailure }); !errors.Is(err, callbackFailure) {
		t.Fatalf("callback-failed Export() error = %v", err)
	}
	if observation.Kind != audit.ObservationFailed || observation.Count != 0 {
		t.Fatalf("callback failure observation = %#v", observation)
	}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if err := decorated.Export(nil, query, func(audit.Record) error { return nil }); !errors.Is(err, audit.ErrInvalidArgument) { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("nil-context Export() error = %v", err)
	}
	if err := decorated.Export(context.Background(), query, nil); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil-callback Export() error = %v", err)
	}
}
