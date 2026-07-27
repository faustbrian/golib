package gotelemetry

import (
	"context"
	"errors"
	"strconv"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
	if instrumentation == nil || !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if _, err := eventsourcing.NewStreamID("projection", name); err != nil {
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

var _ ProjectionRunner = projectionRunner{}
