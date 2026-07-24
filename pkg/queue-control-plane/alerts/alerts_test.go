package alerts

import (
	"errors"
	"strings"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestEvaluateReturnsOnlyHonestThresholdBreaches(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	input := Input{
		TenantID:   "tenant-1",
		ObservedAt: now,
		Queues: queue.QueueStatusPage{Items: []queue.QueueStatus{
			{
				Backend: "valkey-streams", Queue: "critical", ObservedAt: now,
				Metrics: queue.QueueMetrics{
					OldestAge: queue.Measurement[time.Duration]{Value: 2 * time.Minute, Supported: true},
				},
			},
			{
				Backend: "redis-streams", Queue: "unsupported", ObservedAt: now,
				Metrics: queue.QueueMetrics{
					OldestAge: queue.Measurement[time.Duration]{Value: time.Hour},
				},
			},
		}},
		Workers: fleet.RegistrySnapshot{Workers: []fleet.WorkerSnapshot{
			{Heartbeat: alertHeartbeat("tenant-1", "worker-1", now), State: fleet.StateStale},
			{Heartbeat: alertHeartbeat("tenant-1", "worker-2", now), State: fleet.StateRunning},
		}},
		Failures:    queue.Measurement[uint64]{Value: 12, Supported: true},
		DeadLetters: queue.Measurement[uint64]{Value: 3, Supported: true},
		Commands: []controlplane.CommandResult{
			alertResult("tenant-1", "failed-1", controlplane.CommandFailed, now),
			alertResult("tenant-1", "unknown-1", controlplane.CommandUnknown, now),
			alertResult("tenant-1", "ok-1", controlplane.CommandSucceeded, now),
		},
	}
	policy := Policy{
		MaxQueueWait:    time.Minute,
		MaxFailures:     10,
		MaxDeadLetters:  2,
		StaleWorkers:    true,
		CommandFailures: true,
	}

	got, err := Evaluate(input, policy)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	want := []Alert{
		{Kind: KindQueueWait, TenantID: "tenant-1", Resource: "critical", Value: 120, Threshold: 60, ObservedAt: now},
		{Kind: KindFailureCount, TenantID: "tenant-1", Resource: "failures", Value: 12, Threshold: 10, ObservedAt: now},
		{Kind: KindStaleWorker, TenantID: "tenant-1", Resource: "worker-1", Value: 1, Threshold: 0, ObservedAt: now},
		{Kind: KindDeadLetterCount, TenantID: "tenant-1", Resource: "dead-letters", Value: 3, Threshold: 2, ObservedAt: now},
		{Kind: KindCommandFailure, TenantID: "tenant-1", Resource: "failed-1", Value: 1, Threshold: 0, ObservedAt: now},
		{Kind: KindCommandFailure, TenantID: "tenant-1", Resource: "unknown-1", Value: 1, Threshold: 0, ObservedAt: now},
	}
	if len(got) != len(want) {
		t.Fatalf("Evaluate() = %+v, want %+v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("Evaluate()[%d] = %+v, want %+v", index, got[index], want[index])
		}
	}
}

func TestEvaluateSkipsDisabledAndUnsupportedInputs(t *testing.T) {
	t.Parallel()

	now := time.Unix(1, 0).UTC()
	got, err := Evaluate(Input{
		TenantID: "tenant-1", ObservedAt: now,
		Failures:    queue.Measurement[uint64]{Value: 100},
		DeadLetters: queue.Measurement[uint64]{Value: 100},
	}, Policy{})
	if err != nil || len(got) != 0 {
		t.Fatalf("Evaluate() = (%+v, %v), want no alerts", got, err)
	}
}

func TestEvaluateRejectsMalformedInputs(t *testing.T) {
	t.Parallel()

	now := time.Unix(1, 0).UTC()
	tests := map[string]Input{
		"tenant": {ObservedAt: now},
		"tenant length": {
			TenantID: strings.Repeat("x", fleet.MaxIdentityBytes+1), ObservedAt: now,
		},
		"observation": {TenantID: "tenant-1"},
		"queue": {
			TenantID: "tenant-1", ObservedAt: now,
			Queues: queue.QueueStatusPage{Items: []queue.QueueStatus{{Queue: "invalid"}}},
		},
		"worker tenant": {
			TenantID: "tenant-1", ObservedAt: now,
			Workers: fleet.RegistrySnapshot{Workers: []fleet.WorkerSnapshot{
				{Heartbeat: alertHeartbeat("tenant-2", "worker-1", now), State: fleet.StateStale},
			}},
		},
		"worker heartbeat": {
			TenantID: "tenant-1", ObservedAt: now,
			Workers: fleet.RegistrySnapshot{Workers: []fleet.WorkerSnapshot{
				{Heartbeat: fleet.Heartbeat{TenantID: "tenant-1"}, State: fleet.StateStale},
			}},
		},
		"worker state": {
			TenantID: "tenant-1", ObservedAt: now,
			Workers: fleet.RegistrySnapshot{Workers: []fleet.WorkerSnapshot{
				{Heartbeat: alertHeartbeat("tenant-1", "worker-1", now), State: fleet.State("wedged")},
			}},
		},
		"worker bound": {
			TenantID: "tenant-1", ObservedAt: now,
			Workers: fleet.RegistrySnapshot{Workers: make([]fleet.WorkerSnapshot, MaxWorkers+1)},
		},
		"command tenant": {
			TenantID: "tenant-1", ObservedAt: now,
			Commands: []controlplane.CommandResult{
				alertResult("tenant-2", "failed-1", controlplane.CommandFailed, now),
			},
		},
		"command invalid": {
			TenantID: "tenant-1", ObservedAt: now,
			Commands: []controlplane.CommandResult{{TenantID: "tenant-1"}},
		},
		"command bound": {
			TenantID: "tenant-1", ObservedAt: now,
			Commands: make([]controlplane.CommandResult, MaxCommands+1),
		},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if alerts, err := Evaluate(input, Policy{}); alerts != nil || !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Evaluate() = (%+v, %v)", alerts, err)
			}
		})
	}
	if alerts, err := Evaluate(Input{TenantID: "tenant-1", ObservedAt: now}, Policy{MaxQueueWait: -1}); alerts != nil || !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("invalid policy = (%+v, %v)", alerts, err)
	}
}

func alertResult(
	tenant string,
	key string,
	status controlplane.CommandStatus,
	completedAt time.Time,
) controlplane.CommandResult {
	result := controlplane.CommandResult{
		IdempotencyKey: key, TenantID: tenant, Status: status, CompletedAt: completedAt,
	}
	if status == controlplane.CommandFailed || status == controlplane.CommandUnknown {
		result.Failure = controlplane.FailureDispatch
	}

	return result
}

func alertHeartbeat(tenant, worker string, observedAt time.Time) fleet.Heartbeat {
	return fleet.Heartbeat{
		TenantID: tenant, WorkerID: worker, Version: "v1.0.0",
		StartedAt: observedAt.Add(-time.Hour), ObservedAt: observedAt,
		Queues: []string{"critical"}, Concurrency: 1, State: fleet.StateRunning,
		DrainStatus: fleet.DrainNotRequested, Backend: "valkey-streams",
		Protocol: fleet.ProtocolVersion{Major: 1},
	}
}
