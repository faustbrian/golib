package gotelemetry

import (
	"context"
	"errors"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	// ErrProjectionHandlerRequired reports a missing downstream projection
	// handler.
	ErrProjectionHandlerRequired = errors.New(
		"event-sourcing/gotelemetry: projection handler is required",
	)
)

// WrapProjectionHandler instruments one statically named projection handler
// without recording message identity, event data, errors, or panic values.
//
// It returns ErrRuntimeRequired, ErrProjectionNameInvalid, or
// ErrProjectionHandlerRequired for invalid construction. Calls require a
// non-nil context and preserve downstream delivery, context, errors, and
// panics. The returned function inherits concurrency safety from next.
func (instrumentation *Instrumentation) WrapProjectionHandler(
	name string,
	next projection.Handler,
) (projection.Handler, error) {
	if !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if !validTelemetryProjectionName(name) {
		return nil, ErrProjectionNameInvalid
	}
	if next == nil {
		return nil, ErrProjectionHandlerRequired
	}

	return projectionHandler{
		instrumentation: instrumentation,
		name:            name,
		next:            next,
	}.handle, nil
}

type projectionHandler struct {
	instrumentation *Instrumentation
	name            string
	next            projection.Handler
}

func (handler projectionHandler) handle(
	ctx context.Context,
	delivery eventsourcing.Delivery,
) (operationErr error) {
	if ctx == nil {
		return ErrContextRequired
	}
	ctx, span := handler.instrumentation.tracer.Start(
		ctx,
		"event_sourcing.projection.handle",
		trace.WithAttributes(
			attribute.String("event_sourcing.projection.name", handler.name),
			attribute.String(
				"event_sourcing.delivery.mode",
				delivery.Mode().String(),
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
				"event-sourcing projection handler failed",
			)
		}
		handler.instrumentation.recordOperation(
			ctx,
			"projection_handle",
			outcome,
			time.Since(started),
		)
		handler.instrumentation.recordDeliveries(
			ctx,
			[]eventsourcing.Delivery{delivery},
			outcome,
		)
		span.End()
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	return handler.next(ctx, delivery)
}
