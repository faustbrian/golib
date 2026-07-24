package control

import (
	"errors"
	"math"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
)

var (
	// ErrNotDesiredStateAction identifies mutations that do not alter durable
	// worker or queue operational state.
	ErrNotDesiredStateAction = errors.New("control: action has no desired state")
	// ErrInvalidDesiredTarget identifies an action/resource combination that
	// workers cannot enforce through desired state.
	ErrInvalidDesiredTarget = errors.New("control: invalid desired-state target")
	// ErrDesiredTargetMismatch prevents a record from being reused for another
	// resource.
	ErrDesiredTargetMismatch = errors.New("control: desired-state target mismatch")
	// ErrDesiredTenantMismatch prevents cross-tenant state reuse.
	ErrDesiredTenantMismatch = errors.New("control: desired-state tenant mismatch")
	// ErrInvalidDesiredTransition identifies corrupt or irreversible state.
	ErrInvalidDesiredTransition = errors.New("control: invalid desired-state transition")
	// ErrDesiredRevisionExhausted prevents revision wraparound.
	ErrDesiredRevisionExhausted = errors.New("control: desired-state revision exhausted")
)

// DesiredState is the durable operational state workers converge toward.
type DesiredState string

const (
	DesiredActive      DesiredState = "active"
	DesiredPaused      DesiredState = "paused"
	DesiredDraining    DesiredState = "draining"
	DesiredTerminating DesiredState = "terminating"
)

// DesiredRecord is one revisioned durable state for a queue or worker scope.
type DesiredRecord struct {
	TenantID   string
	Target     controlplane.Target
	State      DesiredState
	Revision   uint64
	CommandKey string
	ChangedAt  time.Time
}

// NextDesiredState validates and plans the next monotonic desired-state
// revision. It performs no persistence and sends no worker command.
func NextDesiredState(
	current *DesiredRecord,
	command controlplane.Command,
) (DesiredRecord, error) {
	if err := command.Validate(); err != nil {
		return DesiredRecord{}, err
	}

	nextState, err := desiredStateFor(command.Action, command.Target.Kind)
	if err != nil {
		return DesiredRecord{}, err
	}

	revision := uint64(1)
	if current != nil {
		if current.TenantID != command.TenantID {
			return DesiredRecord{}, ErrDesiredTenantMismatch
		}
		if current.Target != command.Target {
			return DesiredRecord{}, ErrDesiredTargetMismatch
		}
		if !current.State.valid() {
			return DesiredRecord{}, ErrInvalidDesiredTransition
		}
		if current.State == DesiredTerminating && nextState != DesiredTerminating {
			return DesiredRecord{}, ErrInvalidDesiredTransition
		}
		if current.Revision == math.MaxUint64 {
			return DesiredRecord{}, ErrDesiredRevisionExhausted
		}

		revision = current.Revision + 1
	}

	return DesiredRecord{
		TenantID:   command.TenantID,
		Target:     command.Target,
		State:      nextState,
		Revision:   revision,
		CommandKey: command.CommandID,
		ChangedAt:  command.RequestedAt,
	}, nil
}

func desiredStateFor(
	action controlplane.Action,
	target controlplane.TargetKind,
) (DesiredState, error) {
	switch action {
	case controlplane.ActionPause:
		if target == controlplane.TargetQueue || target == controlplane.TargetWorkerGroup {
			return DesiredPaused, nil
		}
	case controlplane.ActionResume:
		if target == controlplane.TargetQueue || target == controlplane.TargetWorkerGroup {
			return DesiredActive, nil
		}
	case controlplane.ActionDrain:
		if target == controlplane.TargetWorker || target == controlplane.TargetWorkerGroup {
			return DesiredDraining, nil
		}
	case controlplane.ActionTerminate:
		if target == controlplane.TargetWorker || target == controlplane.TargetWorkerGroup {
			return DesiredTerminating, nil
		}
	default:
		return "", ErrNotDesiredStateAction
	}

	return "", ErrInvalidDesiredTarget
}

func (s DesiredState) valid() bool {
	switch s {
	case DesiredActive, DesiredPaused, DesiredDraining, DesiredTerminating:
		return true
	default:
		return false
	}
}
