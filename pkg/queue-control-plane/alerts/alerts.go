// Package alerts derives bounded alert inputs from validated operational
// snapshots without delivering notifications or storing time series.
package alerts

import (
	"errors"
	"strings"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

const (
	// MaxWorkers bounds one evaluation pass.
	MaxWorkers = 10_000
	// MaxCommands bounds one evaluation pass.
	MaxCommands = 1_000
)

var (
	// ErrInvalidInput reports malformed or unbounded operational input.
	ErrInvalidInput = errors.New("alerts: invalid input")
	// ErrInvalidPolicy reports an impossible alert threshold.
	ErrInvalidPolicy = errors.New("alerts: invalid policy")
)

// Kind identifies a stable platform alert input.
type Kind string

const (
	KindQueueWait       Kind = "queue_wait"
	KindFailureCount    Kind = "failure_count"
	KindStaleWorker     Kind = "stale_worker"
	KindDeadLetterCount Kind = "dead_letter_count"
	KindCommandFailure  Kind = "command_failure"
)

// Alert is a backend-neutral threshold breach suitable for telemetry export.
type Alert struct {
	Kind       Kind      `json:"kind"`
	TenantID   string    `json:"tenant_id"`
	Resource   string    `json:"resource"`
	Value      float64   `json:"value"`
	Threshold  float64   `json:"threshold"`
	ObservedAt time.Time `json:"observed_at"`
}

// Policy enables alert categories and declares strict upper thresholds.
// Zero count or duration thresholds disable the corresponding category.
type Policy struct {
	MaxQueueWait    time.Duration
	MaxFailures     uint64
	MaxDeadLetters  uint64
	StaleWorkers    bool
	CommandFailures bool
}

// Input is one tenant's bounded current operational snapshot.
type Input struct {
	TenantID    string
	ObservedAt  time.Time
	Queues      queue.QueueStatusPage
	Workers     fleet.RegistrySnapshot
	Failures    queue.Measurement[uint64]
	DeadLetters queue.Measurement[uint64]
	Commands    []controlplane.CommandResult
}

// Evaluate returns deterministic alert inputs for enabled threshold breaches.
func Evaluate(input Input, policy Policy) ([]Alert, error) {
	if policy.MaxQueueWait < 0 {
		return nil, ErrInvalidPolicy
	}
	if invalidIdentity(input.TenantID) || input.ObservedAt.IsZero() ||
		len(input.Workers.Workers) > MaxWorkers || len(input.Commands) > MaxCommands {
		return nil, ErrInvalidInput
	}
	if err := input.Queues.Validate(); err != nil {
		return nil, ErrInvalidInput
	}
	for _, worker := range input.Workers.Workers {
		if worker.TenantID != input.TenantID || worker.Validate() != nil ||
			!validWorkerState(worker.State) {
			return nil, ErrInvalidInput
		}
	}
	for _, command := range input.Commands {
		if command.TenantID != input.TenantID || command.Validate() != nil {
			return nil, ErrInvalidInput
		}
	}

	alerts := make([]Alert, 0)
	if policy.MaxQueueWait > 0 {
		for _, status := range input.Queues.Items {
			measurement := status.Metrics.OldestAge
			if measurement.Supported && measurement.Value > policy.MaxQueueWait {
				alerts = append(alerts, Alert{
					Kind: KindQueueWait, TenantID: input.TenantID, Resource: status.Queue,
					Value: measurement.Value.Seconds(), Threshold: policy.MaxQueueWait.Seconds(),
					ObservedAt: status.ObservedAt.UTC(),
				})
			}
		}
	}
	if policy.MaxFailures > 0 && input.Failures.Supported &&
		input.Failures.Value > policy.MaxFailures {
		alerts = append(alerts, countAlert(
			KindFailureCount, input.TenantID, "failures", input.Failures.Value,
			policy.MaxFailures, input.ObservedAt,
		))
	}
	if policy.StaleWorkers {
		for _, worker := range input.Workers.Workers {
			if worker.State == fleet.StateStale {
				alerts = append(alerts, Alert{
					Kind: KindStaleWorker, TenantID: input.TenantID,
					Resource: worker.WorkerID, Value: 1, ObservedAt: input.ObservedAt.UTC(),
				})
			}
		}
	}
	if policy.MaxDeadLetters > 0 && input.DeadLetters.Supported &&
		input.DeadLetters.Value > policy.MaxDeadLetters {
		alerts = append(alerts, countAlert(
			KindDeadLetterCount, input.TenantID, "dead-letters", input.DeadLetters.Value,
			policy.MaxDeadLetters, input.ObservedAt,
		))
	}
	if policy.CommandFailures {
		for _, command := range input.Commands {
			if command.Status == controlplane.CommandFailed ||
				command.Status == controlplane.CommandUnknown {
				alerts = append(alerts, Alert{
					Kind: KindCommandFailure, TenantID: input.TenantID,
					Resource: command.IdempotencyKey, Value: 1,
					ObservedAt: command.CompletedAt.UTC(),
				})
			}
		}
	}

	return alerts, nil
}

func countAlert(
	kind Kind,
	tenant string,
	resource string,
	value uint64,
	threshold uint64,
	observedAt time.Time,
) Alert {
	return Alert{
		Kind: kind, TenantID: tenant, Resource: resource,
		Value: float64(value), Threshold: float64(threshold), ObservedAt: observedAt.UTC(),
	}
}

func validWorkerState(state fleet.State) bool {
	switch state {
	case fleet.StateRunning, fleet.StatePaused, fleet.StateDraining,
		fleet.StateStopped, fleet.StateStale, fleet.StateUnknown:
		return true
	default:
		return false
	}
}

func invalidIdentity(value string) bool {
	return strings.TrimSpace(value) == "" || len(value) > fleet.MaxIdentityBytes
}
