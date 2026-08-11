package gotelemetry

import (
	"context"
	"errors"
	"math"
	"strconv"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	// ErrProjectionNameInvalid reports an empty or non-canonical projection
	// instrumentation name.
	ErrProjectionNameInvalid = errors.New(
		"event-sourcing/gotelemetry: projection name is invalid",
	)
	// ErrProjectionRunnerRequired reports a missing downstream projection
	// runner.
	ErrProjectionRunnerRequired = errors.New(
		"event-sourcing/gotelemetry: projection runner is required",
	)
	// ErrProjectionLagInvalid reports reversed or unrepresentable projection
	// progress.
	ErrProjectionLagInvalid = errors.New(
		"event-sourcing/gotelemetry: projection lag is invalid",
	)
)

// ProjectionRunner is the consumer-owned surface required to instrument one
// bounded projection replay batch.
type ProjectionRunner interface {
	RunBatch(context.Context) (projection.BatchResult, error)
}

// WrapProjectionRunner instruments bounded replay progress and termination
// without recording event, stream, tenant, payload, metadata, or failure data.
//
// Name becomes an operator-facing span attribute. Applications must use a
// bounded static projection name and must not place tenant or customer
// identity in it.
func (instrumentation *Instrumentation) WrapProjectionRunner(
	name string,
	next ProjectionRunner,
) (ProjectionRunner, error) {
	if instrumentation == nil {
		return nil, ErrRuntimeRequired
	}
	if !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if !validTelemetryProjectionName(name) {
		return nil, ErrProjectionNameInvalid
	}
	if next == nil {
		return nil, ErrProjectionRunnerRequired
	}

	return projectionRunner{
		instrumentation: instrumentation,
		name:            name,
		next:            next,
	}, nil
}

type projectionRunner struct {
	instrumentation *Instrumentation
	name            string
	next            ProjectionRunner
}

func (runner projectionRunner) RunBatch(
	ctx context.Context,
) (
	result projection.BatchResult,
	operationErr error,
) {
	if ctx == nil {
		return projection.BatchResult{}, ErrContextRequired
	}
	ctx, span := runner.instrumentation.tracer.Start(
		ctx,
		"event_sourcing.projection.run_batch",
		trace.WithAttributes(
			attribute.String(
				"event_sourcing.operation",
				"projection_run_batch",
			),
			attribute.String(
				"event_sourcing.projection.name",
				runner.name,
			),
		),
	)
	started := time.Now()
	defer func() {
		panicValue := recover()
		outcome := operationOutcome(operationErr, panicValue)
		termination := projectionTermination(result, operationErr, panicValue)
		span.SetAttributes(
			attribute.String("event_sourcing.outcome", outcome),
			attribute.String(
				"event_sourcing.replay.termination",
				termination,
			),
			attribute.Int64(
				"event_sourcing.projection.scanned",
				int64(result.Scanned()),
			),
			attribute.Int64(
				"event_sourcing.projection.handled",
				int64(result.Handled()),
			),
			attribute.Int64(
				"event_sourcing.projection.filtered",
				int64(result.Filtered()),
			),
			attribute.Int64(
				"event_sourcing.projection.skipped",
				int64(result.Skipped()),
			),
			attribute.Int64(
				"event_sourcing.projection.checkpointed",
				int64(result.Checkpointed()),
			),
			attribute.String(
				"event_sourcing.projection.checkpoint",
				strconv.FormatUint(uint64(result.Checkpoint()), 10),
			),
		)
		if outcome != "success" {
			span.SetStatus(codes.Error, "event-sourcing projection batch failed")
		}
		runner.instrumentation.recordOperation(
			ctx,
			"projection_run_batch",
			outcome,
			time.Since(started),
		)
		runner.instrumentation.recordProjectionResult(
			ctx,
			runner.name,
			result,
			outcome,
		)
		span.End()
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	return runner.next.RunBatch(ctx)
}

func projectionTermination(
	result projection.BatchResult,
	operationErr error,
	panicValue any,
) string {
	if panicValue != nil {
		return "panic"
	}
	if operationErr != nil {
		return "error"
	}
	if result.Scanned() == 0 {
		return "terminated"
	}

	return "progress"
}

// RecordProjectionLag records caller-observed lag without reading a store or
// discovering a high watermark implicitly.
//
// Current and highWatermark are exact durable global positions supplied by the
// caller. The observation also decorates the current span. Values that cannot
// be represented by OpenTelemetry's signed 64-bit metric API are rejected.
func (instrumentation *Instrumentation) RecordProjectionLag(
	ctx context.Context,
	name string,
	current eventsourcing.GlobalPosition,
	highWatermark eventsourcing.GlobalPosition,
) error {
	if instrumentation == nil {
		return ErrRuntimeRequired
	}
	if !instrumentation.valid() {
		return ErrRuntimeRequired
	}
	if ctx == nil {
		return ErrContextRequired
	}
	if !validTelemetryProjectionName(name) {
		return ErrProjectionNameInvalid
	}
	if highWatermark < current {
		return ErrProjectionLagInvalid
	}
	lag := uint64(highWatermark - current)
	if lag > math.MaxInt64 {
		return ErrProjectionLagInvalid
	}
	attributes := []attribute.KeyValue{
		attribute.String("event_sourcing.projection.name", name),
		attribute.Int64("event_sourcing.projection.lag", int64(lag)),
	}
	isolateTelemetry(func() {
		trace.SpanFromContext(ctx).SetAttributes(attributes...)
	})
	instrumentation.projectionLag.Record(
		ctx,
		int64(lag),
	)

	return nil
}

func (instrumentation *Instrumentation) recordProjectionResult(
	ctx context.Context,
	name string,
	result projection.BatchResult,
	outcome string,
) {
	instrumentation.recordProjectionMessageCount(
		ctx,
		name,
		"scanned",
		outcome,
		int64(result.Scanned()),
	)
	instrumentation.recordProjectionMessageCount(
		ctx,
		name,
		"handled",
		outcome,
		int64(result.Handled()),
	)
	instrumentation.recordProjectionMessageCount(
		ctx,
		name,
		"filtered",
		outcome,
		int64(result.Filtered()),
	)
	instrumentation.recordProjectionMessageCount(
		ctx,
		name,
		"skipped",
		outcome,
		int64(result.Skipped()),
	)
	instrumentation.recordProjectionMessageCount(
		ctx,
		name,
		"checkpointed",
		outcome,
		int64(result.Checkpointed()),
	)
}

func (instrumentation *Instrumentation) recordProjectionMessageCount(
	ctx context.Context,
	_ string,
	result string,
	outcome string,
	count int64,
) {
	if count == 0 {
		return
	}
	instrumentation.projectionMessages.Add(
		ctx,
		count,
		metric.WithAttributes(
			attribute.String("event_sourcing.projection.result", result),
			attribute.String("event_sourcing.outcome", outcome),
		),
	)
}

func validTelemetryProjectionName(name string) bool {
	_, err := eventsourcing.NewStreamID("projection", name)

	return err == nil
}

var _ ProjectionRunner = projectionRunner{}
