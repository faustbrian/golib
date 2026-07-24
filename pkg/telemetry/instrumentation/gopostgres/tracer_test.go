package gopostgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/telemetry/testtelemetry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
)

func TestTracerNeverRecordsSQLOrArguments(t *testing.T) {
	t.Parallel()

	harness := testtelemetry.New()
	tracer, err := New(Config{
		TracerProvider: harness.TracerProvider(),
		MeterProvider:  harness.MeterProvider(),
		Operations:     []string{"users.by_id"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := ContextWithOperation(context.Background(), "users.by_id")
	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{
		SQL:  "SELECT * FROM users WHERE password = 'secret'",
		Args: []any{"secret-argument"},
	})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 1")})

	spans := harness.Spans()
	if len(spans) != 1 || spans[0].Name != "users.by_id" {
		t.Fatalf("spans = %+v, want one named query span", spans)
	}
	text := fmt.Sprint(spans[0])
	if strings.Contains(text, "secret") || strings.Contains(text, "SELECT *") {
		t.Fatalf("span leaked SQL or arguments: %s", text)
	}
}

func TestTracerBoundsUnknownOperationAndDatabaseErrors(t *testing.T) {
	t.Parallel()

	harness := testtelemetry.New()
	tracer, err := New(Config{
		TracerProvider: harness.TracerProvider(),
		Operations:     []string{"users.by_id"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := ContextWithOperation(context.Background(), "attacker-secret-id")
	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "secret SQL"})
	queryErr := &pgconn.PgError{Code: "23505", Message: "secret value already exists"}
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: queryErr})

	span := harness.Spans()[0]
	if span.Name != "postgresql.query" || span.Status.Code != codes.Error {
		t.Fatalf("span name/status = %q/%v, want bounded fallback/error", span.Name, span.Status.Code)
	}
	text := fmt.Sprint(span)
	if strings.Contains(text, "secret") || !strings.Contains(text, "23505") {
		t.Fatalf("span must retain SQLSTATE without leaking error details: %s", text)
	}
}

func TestTracerPreservesOriginalErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("secret database error")
	harness := testtelemetry.New()
	tracer, err := New(Config{TracerProvider: harness.TracerProvider()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: want})
	if strings.Contains(fmt.Sprint(harness.Spans()[0]), want.Error()) {
		t.Fatal("span recorded raw database error")
	}
}

func TestConfigRejectsUnboundedOperationContracts(t *testing.T) {
	t.Parallel()

	for _, config := range []Config{
		{Operations: []string{"invalid operation"}},
		{Operations: []string{"duplicate", "duplicate"}},
		{Operations: make([]string, 129)},
	} {
		if _, err := New(config); err == nil {
			t.Fatalf("New(%+v) error = nil, want validation error", config)
		}
	}
}

func TestTracerUsesNoopProvidersAndToleratesMissingStartState(t *testing.T) {
	t.Parallel()

	tracer, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tracer.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{})
}

func TestTraceQueryEndOnlyEndsItsOwnedSpan(t *testing.T) {
	t.Parallel()

	harness := testtelemetry.New()
	tracer, err := New(Config{TracerProvider: harness.TracerProvider()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	parentCtx, parent := harness.TracerProvider().Tracer("test").Start(context.Background(), "parent")
	tracer.TraceQueryEnd(parentCtx, nil, pgx.TraceQueryEndData{})
	if spans := harness.Spans(); len(spans) != 0 {
		t.Fatalf("missing query state ended application span: %+v", spans)
	}

	queryCtx := tracer.TraceQueryStart(parentCtx, nil, pgx.TraceQueryStartData{})
	childCtx, child := harness.TracerProvider().Tracer("test").Start(queryCtx, "child")
	tracer.TraceQueryEnd(childCtx, nil, pgx.TraceQueryEndData{})
	spans := harness.Spans()
	if len(spans) != 1 || spans[0].Name != defaultOperation {
		t.Fatalf("ended spans = %+v, want only owned query span", spans)
	}

	child.End()
	parent.End()
}

func TestNewReportsInstrumentFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("instrument failed")
	provider := errorMeterProvider{MeterProvider: metricnoop.NewMeterProvider(), meter: errorMeter{
		Meter:        metricnoop.NewMeterProvider().Meter("test"),
		histogramErr: want,
	}}
	if _, err := New(Config{MeterProvider: provider}); !errors.Is(err, want) {
		t.Fatalf("New() histogram error = %v, want %v", err, want)
	}
	provider.meter = errorMeter{Meter: metricnoop.NewMeterProvider().Meter("test"), counterErr: want}
	if _, err := New(Config{MeterProvider: provider}); !errors.Is(err, want) {
		t.Fatalf("New() counter error = %v, want %v", err, want)
	}
}

type errorMeterProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

func (provider errorMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return provider.meter
}

type errorMeter struct {
	metric.Meter
	histogramErr error
	counterErr   error
}

func (meter errorMeter) Float64Histogram(string, ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if meter.histogramErr != nil {
		return nil, meter.histogramErr
	}
	return meter.Meter.Float64Histogram("ok")
}

func (meter errorMeter) Int64Counter(string, ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if meter.counterErr != nil {
		return nil, meter.counterErr
	}
	return meter.Meter.Int64Counter("ok")
}

var _ pgx.QueryTracer = (*Tracer)(nil)
