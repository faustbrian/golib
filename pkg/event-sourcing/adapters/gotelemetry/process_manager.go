package gotelemetry

import (
	"context"
	"errors"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/processmanager"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	// ErrProcessManagerNameInvalid reports a non-canonical process-manager
	// telemetry name.
	ErrProcessManagerNameInvalid = errors.New(
		"event-sourcing/gotelemetry: process manager name is invalid",
	)
	// ErrProcessManagerRequired reports a missing downstream process manager.
	ErrProcessManagerRequired = errors.New(
		"event-sourcing/gotelemetry: process manager is required",
	)
)

// ProcessManager is the consumer-owned planning surface implemented by
// *processmanager.Manager.
type ProcessManager[Command any] interface {
	Plan(
		context.Context,
		eventsourcing.Delivery,
	) (processmanager.PlanResult[Command], error)
}

// WrapProcessManager instruments one statically named process manager without
// recording message identity, event data, command data, errors, or panic
// values.
//
// It returns ErrRuntimeRequired, ErrProcessManagerNameInvalid, or
// ErrProcessManagerRequired for invalid construction. Calls require a non-nil
// context and preserve downstream delivery, context, results, errors, and
// panics. The returned manager inherits concurrency safety from next and
// starts no goroutines.
//
// WrapProcessManager is a function rather than an Instrumentation method
// because Go does not support methods with their own type parameters.
func WrapProcessManager[Command any](
	instrumentation *Instrumentation,
	name string,
	next ProcessManager[Command],
) (ProcessManager[Command], error) {
	if !instrumentation.valid() {
		return nil, ErrRuntimeRequired
	}
	if !validTelemetryProcessManagerName(name) {
		return nil, ErrProcessManagerNameInvalid
	}
	if next == nil {
		return nil, ErrProcessManagerRequired
	}

	return processManager[Command]{
		instrumentation: instrumentation,
		name:            name,
		next:            next,
	}, nil
}

type processManager[Command any] struct {
	instrumentation *Instrumentation
	name            string
	next            ProcessManager[Command]
}

func (manager processManager[Command]) Plan(
	ctx context.Context,
	delivery eventsourcing.Delivery,
) (
	result processmanager.PlanResult[Command],
	operationErr error,
) {
	if ctx == nil {
		return processmanager.PlanResult[Command]{}, ErrContextRequired
	}
	ctx, span := manager.instrumentation.tracer.Start(
		ctx,
		"event_sourcing.process_manager.plan",
		trace.WithAttributes(
			attribute.String(
				"event_sourcing.process_manager.name",
				manager.name,
			),
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
				"event-sourcing process-manager planning failed",
			)
		} else {
			span.SetAttributes(attribute.Int(
				"event_sourcing.process_manager.command_count",
				result.CommandCount(),
			))
		}
		manager.instrumentation.recordOperation(
			ctx,
			"process_manager_plan",
			outcome,
			time.Since(started),
		)
		manager.instrumentation.recordDeliveries(
			ctx,
			[]eventsourcing.Delivery{delivery},
			outcome,
		)
		span.End()
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	return manager.next.Plan(ctx, delivery)
}

func validTelemetryProcessManagerName(name string) bool {
	_, err := eventsourcing.NewEventName(name)

	return err == nil
}

var _ ProcessManager[struct{}] = processManager[struct{}]{}
