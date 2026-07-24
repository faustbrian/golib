package control

import (
	"context"
	"errors"
	"reflect"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
)

var (
	// ErrInvalidDispatcherConfiguration reports a missing dispatch boundary.
	ErrInvalidDispatcherConfiguration = errors.New("control: invalid dispatcher configuration")
	// ErrDataPlaneUnavailable reports that no queue controller is reachable.
	ErrDataPlaneUnavailable = errors.New("control: data plane unavailable")
)

// UnavailableDispatcher explicitly represents a disconnected data plane.
type UnavailableDispatcher struct{}

// Dispatch fails closed without issuing any backend operation.
func (UnavailableDispatcher) Dispatch(context.Context, controlplane.Command) error {
	return ErrDataPlaneUnavailable
}

// RoutingDispatcher keeps Kubernetes scaling separate from queue control
// commands.
type RoutingDispatcher struct {
	dataPlane Dispatcher
	workloads Dispatcher
}

// NewRoutingDispatcher creates an action router with explicit boundaries.
func NewRoutingDispatcher(dataPlane Dispatcher, workloads Dispatcher) (*RoutingDispatcher, error) {
	if nilDispatcher(dataPlane) || nilDispatcher(workloads) {
		return nil, ErrInvalidDispatcherConfiguration
	}

	return &RoutingDispatcher{dataPlane: dataPlane, workloads: workloads}, nil
}

func nilDispatcher(dispatcher Dispatcher) bool {
	if dispatcher == nil {
		return true
	}
	reflected := reflect.ValueOf(dispatcher)

	return reflected.Kind() == reflect.Pointer && reflected.IsNil()
}

// Dispatch routes scaling to Kubernetes and every data-plane command to its
// queue adapter.
func (dispatcher *RoutingDispatcher) Dispatch(ctx context.Context, command controlplane.Command) error {
	return dispatcher.selected(command).Dispatch(ctx, command)
}

// DispatchResult preserves structured queue acknowledgements while
// assigning an orchestration completion time to legacy workload adapters.
func (dispatcher *RoutingDispatcher) DispatchResult(
	ctx context.Context,
	command controlplane.Command,
) (DispatchOutcome, error) {
	selected := dispatcher.selected(command)
	if resultDispatcher, ok := selected.(ResultDispatcher); ok {
		return resultDispatcher.DispatchResult(ctx, command)
	}
	if err := selected.Dispatch(ctx, command); err != nil {
		return DispatchOutcome{}, err
	}

	return DispatchOutcome{
		Status:      controlplane.CommandSucceeded,
		CompletedAt: time.Now(),
	}, nil
}

func (dispatcher *RoutingDispatcher) selected(command controlplane.Command) Dispatcher {
	if command.Action == controlplane.ActionScale {
		return dispatcher.workloads
	}

	return dispatcher.dataPlane
}
