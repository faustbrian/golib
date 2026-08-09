package workflow

import (
	"errors"
	"testing"
	"time"
)

func TestOperatorInternalsRejectCorruptAuditAndSequenceOverflow(t *testing.T) {
	t.Parallel()

	if operatorEventKind(OperatorAction(255)) != 0 {
		t.Fatal("invalid operator action mapped to an event")
	}
	instance := Instance{}
	if err := instance.applyOperator(HistoryEvent{data: []byte("bad")}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("corrupt operator audit error = %v", err)
	}
	instance.pendingOperator = OperatorPause
	if err := instance.applyOperator(HistoryEvent{data: []byte(`{"action":1,"actor":"operator-1","reason":"maintenance"}`)}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("nested operator audit error = %v", err)
	}
	if operatorActionSnapshots(nil) != nil {
		t.Fatal("nil operator audit snapshots did not remain nil")
	}
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	definition, err := NewDefinition(DefinitionSpec{
		Name: "orders", Version: "1", Mode: Orchestration,
		Steps: []StepSpec{{
			Name: "execute", Kind: StepActivity, Target: "orders.execute",
			Timeout: time.Second, InputLimit: 1, ResultLimit: 1,
			Retry: RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct definition: %v", err)
	}
	corruptReplay := Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 1, updatedAt: now,
	}
	if err := corruptReplay.apply(nil, HistoryEvent{
		sequence: 2, instanceID: "instance-1", kind: EventOperatorCommandRecorded,
		occurredAt: now, data: []byte("bad"),
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("corrupt replayed operator audit error = %v", err)
	}
	overflow := Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: ^uint64(0) - 1, updatedAt: now,
	}
	if _, err := NewOperatorLifecycleCommand(OperatorLifecycleCommandSpec{
		CommandID: "operator-command-1", Instance: overflow, Action: OperatorPause,
		Actor: "operator-1", Reason: "maintenance", OccurredAt: now,
	}); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("overflow operator command error = %v", err)
	}
	maximum := overflow
	maximum.sequence = ^uint64(0) - 2
	if _, err := NewOperatorLifecycleCommand(OperatorLifecycleCommandSpec{
		CommandID: "operator-command-maximum", Instance: maximum, Action: OperatorPause,
		Actor: "operator-1", Reason: "maintenance", OccurredAt: now,
	}); err != nil {
		t.Fatalf("maximum operator sequence: %v", err)
	}
	if validOperatorLifecycleState(InstanceStatus(255), OperatorTerminate) {
		t.Fatal("invalid instance status allowed termination")
	}
	invalidInstance := overflow
	invalidInstance.sequence = 1
	invalidInstance.id = " spaces "
	if _, err := NewOperatorLifecycleCommand(OperatorLifecycleCommandSpec{
		CommandID: "operator-command-1", Instance: invalidInstance, Action: OperatorPause,
		Actor: "operator-1", Reason: "maintenance", OccurredAt: now,
	}); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("invalid instance operator command error = %v", err)
	}
}
