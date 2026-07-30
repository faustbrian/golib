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
	// ErrProjectionControllerRequired reports a missing downstream projection
	// controller.
	ErrProjectionControllerRequired = errors.New(
		"event-sourcing/gotelemetry: projection controller is required",
	)
)

// ProjectionController is the consumer-owned operational control surface
// implemented by *projection.Controller.
type ProjectionController interface {
	Status(context.Context) (projection.Status, error)
	Pause(context.Context) (projection.Status, error)
	Resume(context.Context) (projection.Status, error)
	ResetCheckpoint(
		context.Context,
		eventsourcing.GlobalPosition,
	) (projection.Status, error)
}

// WrapProjectionController instruments one statically named projection
// controller without changing pause, drain, resume, reset, or conflict
// behavior.
//
// It returns ErrRuntimeRequired, ErrProjectionNameInvalid, or
// ErrProjectionControllerRequired for invalid construction. Calls require a
// non-nil context, preserve downstream results, errors, and panics, start no
// work, and inherit concurrency safety from next.
func (instrumentation *Instrumentation) WrapProjectionController(
	name string,
	next ProjectionController,
) (ProjectionController, error) {
	if !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if !validTelemetryProjectionName(name) {
		return nil, ErrProjectionNameInvalid
	}
	if next == nil {
		return nil, ErrProjectionControllerRequired
	}

	return projectionController{
		instrumentation: instrumentation,
		name:            name,
		next:            next,
	}, nil
}

type projectionController struct {
	instrumentation *Instrumentation
	name            string
	next            ProjectionController
}

func (controller projectionController) Status(
	ctx context.Context,
) (projection.Status, error) {
	return controller.run(ctx, "status", nil, controller.next.Status)
}

func (controller projectionController) Pause(
	ctx context.Context,
) (projection.Status, error) {
	return controller.run(ctx, "pause", nil, controller.next.Pause)
}

func (controller projectionController) Resume(
	ctx context.Context,
) (projection.Status, error) {
	return controller.run(ctx, "resume", nil, controller.next.Resume)
}

func (controller projectionController) ResetCheckpoint(
	ctx context.Context,
	expected eventsourcing.GlobalPosition,
) (projection.Status, error) {
	return controller.run(
		ctx,
		"reset",
		&expected,
		func(ctx context.Context) (projection.Status, error) {
			return controller.next.ResetCheckpoint(ctx, expected)
		},
	)
}

func (controller projectionController) run(
	ctx context.Context,
	operation string,
	expected *eventsourcing.GlobalPosition,
	next func(context.Context) (projection.Status, error),
) (status projection.Status, operationErr error) {
	if ctx == nil {
		return projection.Status{}, ErrContextRequired
	}
	attributes := []attribute.KeyValue{
		attribute.String("event_sourcing.projection.name", controller.name),
	}
	if expected != nil {
		attributes = append(
			attributes,
			attribute.String(
				"event_sourcing.projection.expected_checkpoint",
				strconv.FormatUint(uint64(*expected), 10),
			),
		)
	}
	ctx, span := controller.instrumentation.tracer.Start(
		ctx,
		"event_sourcing.projection.control."+operation,
		trace.WithAttributes(attributes...),
	)
	started := time.Now()
	defer func() {
		panicValue := recover()
		outcome := operationOutcome(operationErr, panicValue)
		if outcome != "success" {
			span.SetStatus(
				codes.Error,
				"event-sourcing projection control failed",
			)
		}
		if outcome == "success" {
			recordProjectionStatus(span, status)
		}
		controller.instrumentation.recordOperation(
			ctx,
			"projection_control_"+operation,
			outcome,
			time.Since(started),
		)
		span.End()
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	return next(ctx)
}

var _ ProjectionController = projectionController{}
