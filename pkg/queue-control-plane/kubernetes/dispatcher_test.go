package kubernetes

import (
	"context"
	"errors"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
)

func TestNewScaleDispatcherRequiresResolver(t *testing.T) {
	t.Parallel()

	var typedNil *resolverStub
	for _, resolver := range []TenantResolver{nil, typedNil} {
		dispatcher, err := NewScaleDispatcher(resolver)
		if dispatcher != nil || !errors.Is(err, ErrInvalidDispatcher) {
			t.Fatalf("NewScaleDispatcher(nil) = (%v, %v)", dispatcher, err)
		}
	}
}

func TestScaleDispatcherResolvesTenantAndScalesWorkload(t *testing.T) {
	t.Parallel()

	scaler := &scalerStub{result: ScaleResult{DesiredReplicas: 5}}
	resolver := &resolverStub{scaler: scaler}
	dispatcher, err := NewScaleDispatcher(resolver)
	if err != nil {
		t.Fatalf("NewScaleDispatcher() error = %v", err)
	}
	command := scaleCommand()

	if err := dispatcher.Dispatch(context.Background(), command); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if resolver.tenant != command.TenantID || scaler.name != command.Target.Name || scaler.replicas != 5 {
		t.Fatalf("dispatch = tenant %q workload %q replicas %d", resolver.tenant, scaler.name, scaler.replicas)
	}
}

func TestScaleDispatcherFailsClosed(t *testing.T) {
	t.Parallel()

	resolverErr := errors.New("tenant cluster unavailable")
	scaleErr := errors.New("scale conflict")
	tests := map[string]struct {
		command  controlplane.Command
		resolver *resolverStub
		wantErr  error
	}{
		"invalid command": {
			command: func() controlplane.Command {
				command := scaleCommand()
				command.Actor = ""

				return command
			}(),
			resolver: &resolverStub{},
		},
		"unsupported command": {
			command: func() controlplane.Command {
				command := scaleCommand()
				command.Action = controlplane.ActionDrain
				command.Target = controlplane.Target{Kind: controlplane.TargetWorkerGroup, Name: "billing"}
				command.Scale = nil

				return command
			}(),
			resolver: &resolverStub{},
			wantErr:  ErrUnsupportedCommand,
		},
		"resolver failure": {
			command: scaleCommand(), resolver: &resolverStub{err: resolverErr}, wantErr: resolverErr,
		},
		"missing scaler": {
			command: scaleCommand(), resolver: &resolverStub{}, wantErr: ErrInvalidDispatcher,
		},
		"scale failure": {
			command: scaleCommand(), resolver: &resolverStub{scaler: &scalerStub{err: scaleErr}}, wantErr: scaleErr,
		},
	}

	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dispatcher, err := NewScaleDispatcher(test.resolver)
			if err != nil {
				t.Fatalf("NewScaleDispatcher() error = %v", err)
			}
			err = dispatcher.Dispatch(context.Background(), test.command)
			if test.wantErr == nil {
				var validationError *controlplane.ValidationError
				if !errors.As(err, &validationError) {
					t.Fatalf("Dispatch() error = %v, want ValidationError", err)
				}
			} else if !errors.Is(err, test.wantErr) {
				t.Fatalf("Dispatch() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func scaleCommand() controlplane.Command {
	return controlplane.Command{
		IdempotencyKey: "request-1",
		TenantID:       "tenant-1",
		Actor:          "operator-1",
		Reason:         "Increase event capacity",
		Action:         controlplane.ActionScale,
		Target:         controlplane.Target{Kind: controlplane.TargetWorkload, Name: "billing-workers"},
		RequestedAt:    time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
		Scale:          &controlplane.Scale{Replicas: 5},
	}
}

type resolverStub struct {
	scaler Scaler
	err    error
	tenant string
}

func (resolver *resolverStub) ResolveTenant(_ context.Context, tenant string) (Scaler, error) {
	resolver.tenant = tenant

	return resolver.scaler, resolver.err
}

type scalerStub struct {
	result   ScaleResult
	err      error
	name     string
	replicas uint32
}

func (scaler *scalerStub) Scale(_ context.Context, name string, replicas uint32) (ScaleResult, error) {
	scaler.name = name
	scaler.replicas = replicas

	return scaler.result, scaler.err
}
