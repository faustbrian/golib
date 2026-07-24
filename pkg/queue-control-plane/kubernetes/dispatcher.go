package kubernetes

import (
	"context"
	"errors"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
)

var (
	// ErrInvalidDispatcher reports a missing tenant-to-cluster resolver.
	ErrInvalidDispatcher = errors.New("kubernetes: invalid dispatcher")
	// ErrUnsupportedCommand reports a non-scaling command at this boundary.
	ErrUnsupportedCommand = errors.New("kubernetes: unsupported command")
)

// Scaler is the scale-only workload mutation exposed to the dispatcher.
type Scaler interface {
	Scale(context.Context, string, uint32) (ScaleResult, error)
}

// TenantResolver maps a tenant to its namespace-scoped Kubernetes adapter.
type TenantResolver interface {
	ResolveTenant(context.Context, string) (Scaler, error)
}

// ScaleDispatcher sends validated scaling commands to a tenant-scoped
// Kubernetes adapter.
type ScaleDispatcher struct {
	resolver TenantResolver
}

// NewScaleDispatcher creates a scale-only Kubernetes command dispatcher.
func NewScaleDispatcher(resolver TenantResolver) (*ScaleDispatcher, error) {
	if nilInterface(resolver) {
		return nil, ErrInvalidDispatcher
	}

	return &ScaleDispatcher{resolver: resolver}, nil
}

// Dispatch resolves the tenant and updates only the workload scale
// subresource.
func (dispatcher *ScaleDispatcher) Dispatch(ctx context.Context, command controlplane.Command) error {
	if err := command.Validate(); err != nil {
		return err
	}
	if command.Action != controlplane.ActionScale {
		return ErrUnsupportedCommand
	}

	scaler, err := dispatcher.resolver.ResolveTenant(ctx, command.TenantID)
	if err != nil {
		return err
	}
	if nilInterface(scaler) {
		return ErrInvalidDispatcher
	}
	_, err = scaler.Scale(ctx, command.Target.Name, command.Scale.Replicas)

	return err
}
