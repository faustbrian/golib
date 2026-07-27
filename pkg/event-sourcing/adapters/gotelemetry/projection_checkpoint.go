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
	// ErrProjectionCheckpointStoreRequired reports a missing downstream
	// projection checkpoint store.
	ErrProjectionCheckpointStoreRequired = errors.New(
		"event-sourcing/gotelemetry: projection checkpoint store is required",
	)
)

// WrapProjectionCheckpointStore instruments projection checkpoint status and
// persistence without changing optimistic concurrency or pause behavior.
//
// It returns ErrRuntimeRequired or ErrProjectionCheckpointStoreRequired for
// invalid construction. Calls require a non-nil context, preserve downstream
// errors and panics, start no work, and inherit concurrency safety from next.
func (instrumentation *Instrumentation) WrapProjectionCheckpointStore(
	next projection.CheckpointStore,
) (projection.CheckpointStore, error) {
	if instrumentation == nil || !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if next == nil {
		return nil, ErrProjectionCheckpointStoreRequired
	}

	return projectionCheckpointStore{
		instrumentation: instrumentation,
		next:            next,
	}, nil
}

type projectionCheckpointStore struct {
	instrumentation *Instrumentation
	next            projection.CheckpointStore
}

func (store projectionCheckpointStore) Status(
	ctx context.Context,
	name string,
) (status projection.Status, operationErr error) {
	if ctx == nil {
		return projection.Status{}, ErrContextRequired
	}
	ctx, span := store.instrumentation.tracer.Start(
		ctx,
		"event_sourcing.projection.checkpoint.status",
		trace.WithAttributes(
			attribute.String(
				"event_sourcing.projection.name",
				telemetryProjectionName(name),
			),
		),
	)
	started := time.Now()
	defer func() {
		panicValue := recover()
		outcome := operationOutcome(operationErr, panicValue)
		if outcome != "success" {
			span.SetStatus(
				codes.Error,
				"event-sourcing projection checkpoint status failed",
			)
		}
		if outcome == "success" {
			recordProjectionStatus(span, status)
		}
		store.instrumentation.recordOperation(
			ctx,
			"projection_checkpoint_status",
			outcome,
			time.Since(started),
		)
		span.End()
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	return store.next.Status(ctx, name)
}

func (store projectionCheckpointStore) Save(
	ctx context.Context,
	name string,
	expected eventsourcing.GlobalPosition,
	next eventsourcing.GlobalPosition,
) (operationErr error) {
	if ctx == nil {
		return ErrContextRequired
	}
	ctx, span := store.instrumentation.tracer.Start(
		ctx,
		"event_sourcing.projection.checkpoint.save",
		trace.WithAttributes(
			attribute.String(
				"event_sourcing.projection.name",
				telemetryProjectionName(name),
			),
			attribute.String(
				"event_sourcing.projection.expected_checkpoint",
				strconv.FormatUint(uint64(expected), 10),
			),
			attribute.String(
				"event_sourcing.projection.checkpoint",
				strconv.FormatUint(uint64(next), 10),
			),
		),
	)
	started := time.Now()
	defer func() {
		panicValue := recover()
		outcome := operationOutcome(operationErr, panicValue)
		if outcome != "success" {
			span.SetStatus(
				codes.Error,
				"event-sourcing projection checkpoint save failed",
			)
		}
		store.instrumentation.recordOperation(
			ctx,
			"projection_checkpoint_save",
			outcome,
			time.Since(started),
		)
		span.End()
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	return store.next.Save(ctx, name, expected, next)
}

func recordProjectionStatus(span trace.Span, status projection.Status) {
	state := "unknown"
	if status.Valid() {
		state = status.State().String()
	}
	attributes := []attribute.KeyValue{
		attribute.String("event_sourcing.projection.state", state),
	}
	if checkpoint, ok := status.Checkpoint(); ok {
		attributes = append(
			attributes,
			attribute.String(
				"event_sourcing.projection.checkpoint",
				strconv.FormatUint(uint64(checkpoint), 10),
			),
		)
	}
	span.SetAttributes(attributes...)
}

func telemetryProjectionName(name string) string {
	if !validTelemetryProjectionName(name) {
		return "invalid"
	}

	return name
}

var _ projection.CheckpointStore = projectionCheckpointStore{}
