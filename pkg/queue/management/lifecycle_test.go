package management

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestWorkerLifecycleGatesAdmissionAndWaitsForSafeTransitions(t *testing.T) {
	t.Parallel()

	lifecycle := newTestLifecycle(t, 8)
	if !lifecycle.BeginAdmission() {
		t.Fatal("initial admission rejected")
	}
	pausedContext, cancelPause := context.WithCancel(context.Background())
	cancelPause()
	paused := lifecycleRecord(1, DesiredPaused, TargetQueue, "critical")
	if err := lifecycle.ApplyDesiredState(pausedContext, paused); !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyDesiredState(blocked pause) error = %v", err)
	}
	if err := lifecycle.EndAdmission(); err != nil {
		t.Fatalf("EndAdmission() error = %v", err)
	}
	if err := lifecycle.ApplyDesiredState(context.Background(), paused); err != nil {
		t.Fatalf("ApplyDesiredState(paused) error = %v", err)
	}
	if lifecycle.BeginAdmission() {
		t.Fatal("paused lifecycle admitted work")
	}
	assertLifecycleSnapshot(t, lifecycle.Snapshot(), WorkerPaused, DrainNotRequested, 0)

	if err := lifecycle.ApplyDesiredState(
		context.Background(), lifecycleRecord(2, DesiredActive, TargetQueue, "critical"),
	); err != nil {
		t.Fatalf("ApplyDesiredState(active) error = %v", err)
	}
	if !lifecycle.BeginAdmission() {
		t.Fatal("resumed lifecycle rejected admission")
	}
	if err := lifecycle.PromoteAdmissionToJob(); err != nil {
		t.Fatalf("PromoteAdmissionToJob() error = %v", err)
	}
	drainContext, cancelDrain := context.WithCancel(context.Background())
	cancelDrain()
	draining := lifecycleRecord(1, DesiredDraining, TargetWorkerGroup, "payments")
	if err := lifecycle.ApplyDesiredState(drainContext, draining); !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyDesiredState(blocked drain) error = %v", err)
	}
	if lifecycle.BeginAdmission() {
		t.Fatal("draining lifecycle admitted work")
	}
	assertLifecycleSnapshot(t, lifecycle.Snapshot(), WorkerDraining, DrainInProgress, 1)
	if err := lifecycle.EndJob(); err != nil {
		t.Fatalf("EndJob() error = %v", err)
	}
	if err := lifecycle.ApplyDesiredState(context.Background(), draining); err != nil {
		t.Fatalf("ApplyDesiredState(draining) error = %v", err)
	}
	assertLifecycleSnapshot(t, lifecycle.Snapshot(), WorkerDraining, DrainCompleted, 0)
}

func TestWorkerLifecycleDesiredStateIsTargetedMonotonicAndRetryable(t *testing.T) {
	t.Parallel()

	lifecycle := newTestLifecycle(t, 8)
	if err := lifecycle.ApplyDesiredState(context.Background(), DesiredRecord{}); !errors.Is(err, ErrInvalidDesiredStateOutput) {
		t.Fatalf("invalid desired output error = %v", err)
	}
	tests := []struct {
		record  DesiredRecord
		wantErr error
	}{
		{record: lifecycleRecord(1, DesiredPaused, TargetQueue, "other"), wantErr: ErrDesiredStateTargetMismatch},
		{record: lifecycleRecord(1, DesiredPaused, TargetWorker, "worker-1"), wantErr: ErrInvalidDesiredStateTransition},
		{record: lifecycleRecord(1, DesiredDraining, TargetQueue, "critical"), wantErr: ErrInvalidDesiredStateTransition},
		{record: lifecycleRecord(1, DesiredActive, TargetWorkerGroup, "payments"), wantErr: nil},
	}
	for _, tt := range tests {
		err := lifecycle.ApplyDesiredState(context.Background(), tt.record)
		if !errors.Is(err, tt.wantErr) {
			t.Fatalf("ApplyDesiredState(%+v) error = %v, want %v", tt.record, err, tt.wantErr)
		}
	}

	paused := lifecycleRecord(2, DesiredPaused, TargetQueue, "critical")
	if err := lifecycle.ApplyDesiredState(context.Background(), paused); err != nil {
		t.Fatalf("ApplyDesiredState(paused) error = %v", err)
	}
	if err := lifecycle.ApplyDesiredState(context.Background(), paused); err != nil {
		t.Fatalf("ApplyDesiredState(duplicate) error = %v", err)
	}
	regression := lifecycleRecord(1, DesiredActive, TargetQueue, "critical")
	if err := lifecycle.ApplyDesiredState(context.Background(), regression); !errors.Is(err, ErrDesiredStateRegression) {
		t.Fatalf("regression error = %v", err)
	}
	conflict := lifecycleRecord(2, DesiredActive, TargetQueue, "critical")
	if err := lifecycle.ApplyDesiredState(context.Background(), conflict); !errors.Is(err, ErrDesiredStateConflict) {
		t.Fatalf("conflict error = %v", err)
	}

	if !lifecycle.BeginAdmission() {
		// Resume before exercising timeout retryability.
		if err := lifecycle.ApplyDesiredState(
			context.Background(), lifecycleRecord(3, DesiredActive, TargetQueue, "critical"),
		); err != nil {
			t.Fatalf("resume error = %v", err)
		}
		if !lifecycle.BeginAdmission() {
			t.Fatal("resumed lifecycle rejected admission")
		}
	}
	if err := lifecycle.PromoteAdmissionToJob(); err != nil {
		t.Fatalf("PromoteAdmissionToJob() error = %v", err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	cancel()
	draining := lifecycleRecord(2, DesiredDraining, TargetWorkerGroup, "payments")
	if err := lifecycle.ApplyDesiredState(ctx, draining); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled drain error = %v", err)
	}
	assertLifecycleSnapshot(t, lifecycle.Snapshot(), WorkerDraining, DrainTimedOut, 1)
	if err := lifecycle.EndJob(); err != nil {
		t.Fatalf("EndJob() error = %v", err)
	}
	if err := lifecycle.ApplyDesiredState(context.Background(), draining); err != nil {
		t.Fatalf("retried drain error = %v", err)
	}
}

func TestWorkerLifecycleExecutesEveryLifecycleActionAndFailureOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		action CommandAction
		kind   TargetKind
		name   string
	}{
		{action: CommandResume, kind: TargetWorkerGroup, name: "payments"},
		{action: CommandDrain, kind: TargetWorker, name: "worker-1"},
		{action: CommandTerminate, kind: TargetWorker, name: "worker-1"},
	}
	for _, tt := range tests {
		lifecycle := newTestLifecycle(t, 8)
		command := lifecycleCommand(string(tt.action)+"-1", tt.action, tt.kind, tt.name)
		result, err := lifecycle.Execute(context.Background(), command)
		if err != nil || result.Status != CommandAcknowledged {
			t.Fatalf("Execute(%s) = (%+v, %v)", tt.action, result, err)
		}
	}

	protocol := newTestLifecycle(t, 8)
	command := lifecycleCommand("protocol-1", CommandPause, TargetQueue, "critical")
	command.Protocol.Minor = 1
	result, err := protocol.Execute(context.Background(), command)
	if err != nil || result.Status != CommandUnsupported || result.FailureCode != "protocol_mismatch" {
		t.Fatalf("Execute(protocol mismatch) = (%+v, %v)", result, err)
	}

	expired := newTestLifecycle(t, 8)
	command = lifecycleCommand("expired-1", CommandPause, TargetQueue, "critical")
	command.RequestedAt = time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	command.Deadline = command.RequestedAt.Add(time.Minute)
	result, err = expired.Execute(context.Background(), command)
	if err != nil || result.Status != CommandTimedOut || result.FailureCode != "deadline_exceeded" {
		t.Fatalf("Execute(expired) = (%+v, %v)", result, err)
	}

	terminating := newTestLifecycle(t, 8)
	if err := terminating.ApplyDesiredState(
		context.Background(), lifecycleRecord(1, DesiredTerminating, TargetWorker, "worker-1"),
	); err != nil {
		t.Fatalf("terminate state error = %v", err)
	}
	command = lifecycleCommand("resume-terminated", CommandResume, TargetWorkerGroup, "payments")
	result, err = terminating.Execute(context.Background(), command)
	if err != nil || result.Status != CommandRejected || result.FailureCode != "invalid_transition" {
		t.Fatalf("Execute(resume terminated) = (%+v, %v)", result, err)
	}

	canceled := newTestLifecycle(t, 8)
	if !canceled.BeginAdmission() {
		t.Fatal("canceled lifecycle admission rejected")
	}
	if err := canceled.PromoteAdmissionToJob(); err != nil {
		t.Fatalf("PromoteAdmissionToJob() error = %v", err)
	}
	command = lifecycleCommand("canceled-drain", CommandDrain, TargetWorker, "worker-1")
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = canceled.Execute(canceledContext, command)
	if err != nil || result.Status != CommandTimedOut || result.FailureCode != "deadline_exceeded" {
		t.Fatalf("Execute(canceled drain) = (%+v, %v)", result, err)
	}
	if err := canceled.EndJob(); err != nil {
		t.Fatalf("EndJob() error = %v", err)
	}
}

func TestWorkerLifecyclePendingDuplicateHonorsCancellation(t *testing.T) {
	t.Parallel()

	lifecycle := newTestLifecycle(t, 8)
	if !lifecycle.BeginAdmission() {
		t.Fatal("admission rejected")
	}
	if err := lifecycle.PromoteAdmissionToJob(); err != nil {
		t.Fatalf("PromoteAdmissionToJob() error = %v", err)
	}
	command := lifecycleCommand("drain-1", CommandDrain, TargetWorker, "worker-1")
	done := make(chan CommandResult, 1)
	go func() {
		result, _ := lifecycle.Execute(context.Background(), command)
		done <- result
	}()
	waitForLifecycleState(t, lifecycle, DesiredDraining)
	duplicateContext, cancel := context.WithCancel(context.Background())
	cancel()
	duplicate, err := lifecycle.Execute(duplicateContext, command)
	if err != nil || duplicate.Status != CommandUnknown {
		t.Fatalf("Execute(pending duplicate) = (%+v, %v)", duplicate, err)
	}
	if err := lifecycle.EndJob(); err != nil {
		t.Fatalf("EndJob() error = %v", err)
	}
	if result := <-done; result.Status != CommandAcknowledged {
		t.Fatalf("initial result = %+v", result)
	}
}

func TestWorkerLifecycleExecutesAndCachesBoundedLifecycleCommands(t *testing.T) {
	t.Parallel()

	lifecycle := newTestLifecycle(t, 2)
	pause := lifecycleCommand("pause-1", CommandPause, TargetQueue, "critical")
	result, err := lifecycle.Execute(context.Background(), pause)
	if err != nil || result.Status != CommandAcknowledged {
		t.Fatalf("Execute(pause) = (%+v, %v)", result, err)
	}
	duplicate, err := lifecycle.Execute(context.Background(), pause)
	if err != nil || !reflect.DeepEqual(duplicate, result) {
		t.Fatalf("Execute(duplicate) = (%+v, %v), want %+v", duplicate, err, result)
	}
	conflict := pause
	conflict.Action = CommandResume
	conflicted, err := lifecycle.Execute(context.Background(), conflict)
	if err != nil || conflicted.Status != CommandRejected || conflicted.FailureCode != "idempotency_conflict" {
		t.Fatalf("Execute(conflict) = (%+v, %v)", conflicted, err)
	}

	unsupported := lifecycleCommand("retry-1", CommandRetry, TargetFailure, "failure-1")
	result, err = lifecycle.Execute(context.Background(), unsupported)
	if err != nil || result.Status != CommandUnsupported || result.FailureCode != "unsupported_action" {
		t.Fatalf("Execute(unsupported) = (%+v, %v)", result, err)
	}
	full := lifecycleCommand("pause-2", CommandPause, TargetQueue, "critical")
	result, err = lifecycle.Execute(context.Background(), full)
	if err != nil || result.Status != CommandRejected || result.FailureCode != "idempotency_capacity" {
		t.Fatalf("Execute(full) = (%+v, %v)", result, err)
	}
}

func TestWorkerLifecycleReportsTargetsTimeoutsAndStatusHonestly(t *testing.T) {
	t.Parallel()

	lifecycle := newTestLifecycle(t, 8)
	wrong := lifecycleCommand("wrong-1", CommandPause, TargetQueue, "other")
	result, err := lifecycle.Execute(context.Background(), wrong)
	if err != nil || result.Status != CommandRejected || result.FailureCode != "target_mismatch" {
		t.Fatalf("Execute(wrong target) = (%+v, %v)", result, err)
	}
	expired := lifecycleCommand("expired-1", CommandPause, TargetQueue, "critical")
	expired.Deadline = expired.RequestedAt
	result, err = lifecycle.Execute(context.Background(), expired)
	if err == nil {
		t.Fatalf("Execute(invalid deadline) = (%+v, %v)", result, err)
	}

	status := WorkerStatus{
		ID: "worker-1", Version: "v1.0.0", StartedAt: time.Unix(1, 0).UTC(),
		HeartbeatAt: time.Unix(2, 0).UTC(), Queues: []string{"critical"},
		Concurrency: 4, State: WorkerRunning, DrainStatus: DrainNotRequested,
		Backend: "valkey-streams", Protocol: ProtocolVersion{Major: 1},
		Capabilities: []Capability{CapabilityQueueStatus},
	}
	decorated, err := lifecycle.DecorateWorkerStatus(status)
	if err != nil || decorated.CurrentJobs != 0 || decorated.State != WorkerRunning ||
		!reflect.DeepEqual(decorated.Capabilities, []Capability{
			CapabilityDrain, CapabilityPause, CapabilityQueueStatus,
			CapabilityResume, CapabilityTerminate,
		}) {
		t.Fatalf("DecorateWorkerStatus() = (%+v, %v)", decorated, err)
	}
	status.ID = "other"
	if _, err := lifecycle.DecorateWorkerStatus(status); !errors.Is(err, ErrDesiredStateTargetMismatch) {
		t.Fatalf("mismatched status error = %v", err)
	}
	status = WorkerStatus{}
	if _, err := lifecycle.DecorateWorkerStatus(status); !errors.Is(err, ErrDesiredStateTargetMismatch) {
		t.Fatalf("invalid status error = %v", err)
	}
	status = WorkerStatus{
		ID: "worker-1", Version: "v1.0.0", StartedAt: time.Unix(1, 0).UTC(),
		HeartbeatAt: time.Unix(2, 0).UTC(), Queues: []string{"other"},
		Concurrency: 4, State: WorkerRunning, DrainStatus: DrainNotRequested,
		Backend: "valkey-streams", Protocol: ProtocolVersion{Major: 1},
	}
	if _, err := lifecycle.DecorateWorkerStatus(status); !errors.Is(err, ErrDesiredStateTargetMismatch) {
		t.Fatalf("queue mismatch error = %v", err)
	}
	status.Queues = []string{"critical"}
	status.Capabilities = make([]Capability, MaxCapabilitiesPerWorker)
	for index := range status.Capabilities {
		status.Capabilities[index] = Capability("capability-" + string(rune('a'+index)))
	}
	if _, err := lifecycle.DecorateWorkerStatus(status); !errors.Is(err, ErrInvalidStatusProviderOutput) {
		t.Fatalf("capability overflow error = %v", err)
	}
}

func TestWorkerLifecycleRejectsUnsafeConfigurationAndCounterUse(t *testing.T) {
	t.Parallel()

	valid := WorkerLifecycleConfig{
		Metadata: StatusMetadata{
			ID: "worker-1", Version: "v1.0.0", Concurrency: 4,
			Protocol: ProtocolVersion{Major: 1},
		},
		WorkerGroup: "payments", Queue: "critical", MaxCommandResults: 8,
		Now: func() time.Time { return time.Unix(10, 0).UTC() },
	}
	tests := []WorkerLifecycleConfig{
		{},
		func() WorkerLifecycleConfig { value := valid; value.Metadata = StatusMetadata{}; return value }(),
		func() WorkerLifecycleConfig { value := valid; value.WorkerGroup = ""; return value }(),
		func() WorkerLifecycleConfig { value := valid; value.Queue = ""; return value }(),
		func() WorkerLifecycleConfig { value := valid; value.MaxCommandResults = 0; return value }(),
		func() WorkerLifecycleConfig {
			value := valid
			value.MaxCommandResults = MaxLifecycleCommandResults + 1
			return value
		}(),
		func() WorkerLifecycleConfig { value := valid; value.Now = nil; return value }(),
	}
	for _, config := range tests {
		lifecycle, err := NewWorkerLifecycle(config)
		if lifecycle != nil || !errors.Is(err, ErrInvalidWorkerLifecycleConfiguration) {
			t.Fatalf("NewWorkerLifecycle(%+v) = (%v, %v)", config, lifecycle, err)
		}
	}
	lifecycle, err := NewWorkerLifecycle(valid)
	if err != nil {
		t.Fatalf("NewWorkerLifecycle() error = %v", err)
	}
	if err := lifecycle.EndAdmission(); !errors.Is(err, ErrInvalidLifecycleCounter) {
		t.Fatalf("EndAdmission() error = %v", err)
	}
	if err := lifecycle.PromoteAdmissionToJob(); !errors.Is(err, ErrInvalidLifecycleCounter) {
		t.Fatalf("PromoteAdmissionToJob() error = %v", err)
	}
	if err := lifecycle.EndJob(); !errors.Is(err, ErrInvalidLifecycleCounter) {
		t.Fatalf("EndJob() error = %v", err)
	}
	//lint:ignore SA1012 Public boundary must reject nil context safely.
	//nolint:staticcheck // Public boundary must reject nil context safely.
	if err := lifecycle.ApplyDesiredState(nil, lifecycleRecord(1, DesiredPaused, TargetQueue, "critical")); !errors.Is(err, ErrInvalidDesiredStateContext) {
		t.Fatalf("ApplyDesiredState(nil) error = %v", err)
	}
	//lint:ignore SA1012 Public boundary must reject nil context safely.
	//nolint:staticcheck // Public boundary must reject nil context safely.
	if _, err := lifecycle.Execute(nil, lifecycleCommand("pause-1", CommandPause, TargetQueue, "critical")); !errors.Is(err, ErrInvalidWorkerLifecycleContext) {
		t.Fatalf("Execute(nil) error = %v", err)
	}
}

func newTestLifecycle(t *testing.T, capacity int) *WorkerLifecycle {
	t.Helper()
	lifecycle, err := NewWorkerLifecycle(WorkerLifecycleConfig{
		Metadata: StatusMetadata{
			ID: "worker-1", Version: "v1.0.0", Concurrency: 4,
			Protocol: ProtocolVersion{Major: 1},
		},
		WorkerGroup: "payments", Queue: "critical", MaxCommandResults: capacity,
		Now: func() time.Time { return time.Date(2026, 7, 16, 10, 0, 1, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewWorkerLifecycle() error = %v", err)
	}
	return lifecycle
}

func lifecycleRecord(
	revision uint64,
	state DesiredState,
	kind TargetKind,
	name string,
) DesiredRecord {
	return DesiredRecord{
		Target: Target{Kind: kind, Name: name}, State: state, Revision: revision,
		ChangedAt: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
		CommandID: "command-1",
	}
}

func lifecycleCommand(
	id string,
	action CommandAction,
	kind TargetKind,
	name string,
) Command {
	requested := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	return Command{
		ID: id, IdempotencyKey: id, Actor: "operator-1", Reason: "maintenance",
		Protocol: ProtocolVersion{Major: 1}, Action: action,
		Target: Target{Kind: kind, Name: name}, RequestedAt: requested,
		Deadline: requested.Add(time.Minute),
	}
}

func assertLifecycleSnapshot(
	t *testing.T,
	snapshot WorkerLifecycleSnapshot,
	state WorkerState,
	drain DrainState,
	jobs uint32,
) {
	t.Helper()
	if snapshot.State != state || snapshot.DrainStatus != drain || snapshot.CurrentJobs != jobs {
		t.Fatalf("snapshot = %+v, want state=%s drain=%s jobs=%d", snapshot, state, drain, jobs)
	}
}

func waitForLifecycleState(t *testing.T, lifecycle *WorkerLifecycle, wanted DesiredState) {
	t.Helper()
	for {
		lifecycle.mu.Lock()
		state := lifecycle.state
		changed := lifecycle.changed
		lifecycle.mu.Unlock()
		if state == wanted {
			return
		}
		select {
		case <-changed:
		case <-t.Context().Done():
			t.Fatalf("waiting for lifecycle state %q: %v", wanted, t.Context().Err())
		}
	}
}
