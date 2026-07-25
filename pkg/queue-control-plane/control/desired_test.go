package control

import (
	"errors"
	"math"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
)

func TestNextDesiredStateBuildsMonotonicAttributedTransitions(t *testing.T) {
	t.Parallel()

	requestedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	pause := desiredCommand(controlplane.ActionPause, controlplane.TargetQueue, requestedAt)

	paused, err := NextDesiredState(nil, pause)
	if err != nil {
		t.Fatalf("NextDesiredState(pause) error = %v", err)
	}
	if paused.State != DesiredPaused || paused.Revision != 1 {
		t.Fatalf("paused state = %+v, want paused revision 1", paused)
	}
	if paused.CommandKey != pause.CommandID ||
		paused.TenantID != pause.TenantID ||
		paused.ChangedAt != requestedAt ||
		paused.Target != pause.Target {
		t.Fatalf("paused attribution = %+v, want command attribution", paused)
	}

	resume := desiredCommand(controlplane.ActionResume, controlplane.TargetQueue, requestedAt.Add(time.Second))
	active, err := NextDesiredState(&paused, resume)
	if err != nil {
		t.Fatalf("NextDesiredState(resume) error = %v", err)
	}
	if active.State != DesiredActive || active.Revision != 2 {
		t.Fatalf("active state = %+v, want active revision 2", active)
	}

	drain := desiredCommand(controlplane.ActionDrain, controlplane.TargetWorkerGroup, requestedAt)
	draining, err := NextDesiredState(nil, drain)
	if err != nil {
		t.Fatalf("NextDesiredState(drain) error = %v", err)
	}
	if draining.State != DesiredDraining {
		t.Fatalf("draining state = %q, want %q", draining.State, DesiredDraining)
	}

	terminate := desiredCommand(controlplane.ActionTerminate, controlplane.TargetWorkerGroup, requestedAt.Add(time.Second))
	terminating, err := NextDesiredState(&draining, terminate)
	if err != nil {
		t.Fatalf("NextDesiredState(terminate) error = %v", err)
	}
	if terminating.State != DesiredTerminating || terminating.Revision != 2 {
		t.Fatalf("terminating state = %+v, want terminating revision 2", terminating)
	}
}

func TestNextDesiredStateRejectsUnsafeTransitions(t *testing.T) {
	t.Parallel()

	now := time.Unix(1, 0)
	queue := controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"}
	workerGroup := controlplane.Target{Kind: controlplane.TargetWorkerGroup, Name: "payments"}

	tests := map[string]struct {
		current *DesiredRecord
		command controlplane.Command
		wantErr error
	}{
		"non desired action": {
			command: desiredCommand(controlplane.ActionRetry, controlplane.TargetFailure, now),
			wantErr: ErrNotDesiredStateAction,
		},
		"record target mismatch": {
			current: &DesiredRecord{TenantID: "tenant-1", Target: queue, State: DesiredPaused, Revision: 1},
			command: desiredCommand(controlplane.ActionResume, controlplane.TargetWorkerGroup, now),
			wantErr: ErrDesiredTargetMismatch,
		},
		"record tenant mismatch": {
			current: &DesiredRecord{TenantID: "tenant-2", Target: queue, State: DesiredPaused, Revision: 1},
			command: desiredCommand(controlplane.ActionResume, controlplane.TargetQueue, now),
			wantErr: ErrDesiredTenantMismatch,
		},
		"termination is irreversible": {
			current: &DesiredRecord{TenantID: "tenant-1", Target: workerGroup, State: DesiredTerminating, Revision: 1},
			command: desiredCommand(controlplane.ActionDrain, controlplane.TargetWorkerGroup, now),
			wantErr: ErrInvalidDesiredTransition,
		},
		"unknown current state": {
			current: &DesiredRecord{TenantID: "tenant-1", Target: queue, State: DesiredState("lost"), Revision: 1},
			command: desiredCommand(controlplane.ActionResume, controlplane.TargetQueue, now),
			wantErr: ErrInvalidDesiredTransition,
		},
		"revision exhausted": {
			current: &DesiredRecord{TenantID: "tenant-1", Target: queue, State: DesiredPaused, Revision: math.MaxUint64},
			command: desiredCommand(controlplane.ActionResume, controlplane.TargetQueue, now),
			wantErr: ErrDesiredRevisionExhausted,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NextDesiredState(tt.current, tt.command)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NextDesiredState() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNextDesiredStateRejectsInvalidCommandTarget(t *testing.T) {
	t.Parallel()

	command := desiredCommand(controlplane.ActionPause, controlplane.TargetFailure, time.Unix(1, 0))
	_, err := NextDesiredState(nil, command)
	var validationError *controlplane.ValidationError
	if !errors.As(err, &validationError) || validationError.Field != "target.kind" {
		t.Fatalf("NextDesiredState() error = %v, want target.kind validation", err)
	}
}

func TestDesiredStateForRejectsInvalidTarget(t *testing.T) {
	t.Parallel()

	if _, err := desiredStateFor(controlplane.ActionPause, controlplane.TargetFailure); !errors.Is(err, ErrInvalidDesiredTarget) {
		t.Fatalf("desiredStateFor() error = %v, want %v", err, ErrInvalidDesiredTarget)
	}
}

func TestNextDesiredStateAllowsRepeatedTerminalRequest(t *testing.T) {
	t.Parallel()

	now := time.Unix(1, 0)
	target := controlplane.Target{Kind: controlplane.TargetWorker, Name: "worker-1"}
	current := DesiredRecord{TenantID: "tenant-1", Target: target, State: DesiredTerminating, Revision: 4}
	command := desiredCommand(controlplane.ActionTerminate, controlplane.TargetWorker, now)
	command.Target = target

	next, err := NextDesiredState(&current, command)
	if err != nil {
		t.Fatalf("NextDesiredState() error = %v", err)
	}
	if next.State != DesiredTerminating || next.Revision != 5 {
		t.Fatalf("NextDesiredState() = %+v, want terminating revision 5", next)
	}
}

func TestNextDesiredStateRejectsInvalidCommandEnvelope(t *testing.T) {
	t.Parallel()

	command := desiredCommand(controlplane.ActionPause, controlplane.TargetQueue, time.Unix(1, 0))
	command.Actor = ""

	_, err := NextDesiredState(nil, command)
	var validationError *controlplane.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("NextDesiredState() error = %v, want ValidationError", err)
	}
}

func desiredCommand(action controlplane.Action, kind controlplane.TargetKind, requestedAt time.Time) controlplane.Command {
	return controlplane.Command{
		CommandID:      "command-1",
		IdempotencyKey: "request-1",
		TenantID:       "tenant-1",
		Actor:          "operator@example.test",
		Reason:         "Incident mitigation",
		Action:         action,
		Target:         controlplane.Target{Kind: kind, Name: targetName(kind)},
		RequestedAt:    requestedAt,
	}
}

func targetName(kind controlplane.TargetKind) string {
	if kind == controlplane.TargetQueue {
		return "critical"
	}

	return "payments"
}
