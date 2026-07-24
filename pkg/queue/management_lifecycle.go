package queue

import (
	"context"
	"errors"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/management"
)

var (
	// ErrInvalidManagementLifecycle reports a lifecycle paired with a worker
	// that cannot provide native worker and queue status.
	ErrInvalidManagementLifecycle = errors.New("queue: invalid management lifecycle")
	// ErrManagementLifecycleDisabled reports management calls on a queue that
	// was not explicitly configured for them.
	ErrManagementLifecycleDisabled = errors.New("queue: management lifecycle disabled")
)

// Execute applies one queue-owned lifecycle command. Backend record and queue
// mutation actions remain explicitly unsupported by WorkerLifecycle.
func (q *Queue) Execute(
	ctx context.Context,
	command management.Command,
) (management.CommandResult, error) {
	if q.lifecycle == nil {
		return management.CommandResult{}, ErrManagementLifecycleDisabled
	}
	if nativeMutationAction(command.Action) {
		if controller, ok := q.worker.(management.Controller); ok {
			return controller.Execute(ctx, command)
		}
	}
	result, err := q.lifecycle.Execute(ctx, command)
	if err != nil {
		return management.CommandResult{}, err
	}
	if result.Status == management.CommandAcknowledged {
		switch command.Action {
		case management.CommandResume:
			q.schedule()
		case management.CommandTerminate:
			q.Shutdown()
		default:
		}
	}

	return result, nil
}

func nativeMutationAction(action management.CommandAction) bool {
	switch action {
	case management.CommandRetry,
		management.CommandBulkRetry,
		management.CommandDelete,
		management.CommandPurge,
		management.CommandReplay:
		return true
	default:
		return false
	}
}

// ApplyDesiredState converges queue admission and graceful shutdown to one
// durable revision.
func (q *Queue) ApplyDesiredState(
	ctx context.Context,
	record management.DesiredRecord,
) error {
	if q.lifecycle == nil {
		return ErrManagementLifecycleDisabled
	}
	if err := q.lifecycle.ApplyDesiredState(ctx, record); err != nil {
		return err
	}
	snapshot := q.lifecycle.Snapshot()
	if snapshot.Terminating && snapshot.DrainStatus == management.DrainCompleted {
		q.Shutdown()
	} else if snapshot.State == management.WorkerRunning {
		q.schedule()
	}

	return nil
}

// ObserveWorker overlays queue-owned admission and in-flight state on the
// backend worker's native status.
func (q *Queue) ObserveWorker(ctx context.Context) (management.WorkerStatus, error) {
	if q.lifecycle == nil {
		return management.WorkerStatus{}, ErrManagementLifecycleDisabled
	}
	provider := q.worker.(management.StatusProvider)
	status, err := provider.ObserveWorker(ctx)
	if err != nil {
		return management.WorkerStatus{}, err
	}

	return q.lifecycle.DecorateWorkerStatus(status)
}

// ObserveQueue forwards honest backend-native queue measurements.
func (q *Queue) ObserveQueue(ctx context.Context) (management.QueueStatus, error) {
	if q.lifecycle == nil {
		return management.QueueStatus{}, ErrManagementLifecycleDisabled
	}
	provider := q.worker.(management.StatusProvider)

	return provider.ObserveQueue(ctx)
}

func workerProvidesStatus(worker core.Worker) bool {
	_, ok := worker.(management.StatusProvider)
	return ok
}

var (
	_ management.Controller          = (*Queue)(nil)
	_ management.DesiredStateApplier = (*Queue)(nil)
	_ management.StatusProvider      = (*Queue)(nil)
)
