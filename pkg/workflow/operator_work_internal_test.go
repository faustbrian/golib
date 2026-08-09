package workflow

import (
	"errors"
	"testing"
	"time"
)

func TestOperatorWorkCommandsRejectInvalidAuditAndActionBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := NewOperatorActivityRetry(OperatorActivityRetrySpec{}); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("zero activity retry error = %v", err)
	}
	if _, err := NewOperatorCompensation(OperatorCompensationSpec{}); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("zero compensation error = %v", err)
	}
	if _, err := NewOperatorCompensationResolution(OperatorCompensationResolutionSpec{}); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("zero resolution error = %v", err)
	}
	if _, err := NewOperatorApproval(OperatorApprovalSpec{}); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("zero approval error = %v", err)
	}

	now := time.Date(2026, 8, 10, 17, 0, 0, 0, time.UTC)
	activityDefinition := internalActivityTransitionDefinition(t)
	failedActivity := internalActivityInstance(activityDefinition, now, ActivityProgressFailed, 1, true)
	if _, err := NewOperatorActivityRetry(OperatorActivityRetrySpec{
		CommandID: "operator-retry", Instance: failedActivity, Definition: activityDefinition,
		StepName: "execute", IdempotencyKey: "attempt-2", Actor: "operator-1",
		Reason: "manual-retry", OccurredAt: now.Add(time.Second), Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("invalid activity action error = %v", err)
	}

	compensationDefinition := internalCompensationTransitionDefinition(t)
	readyCompensation := internalCompensationProcessorReadyInstance(compensationDefinition, now)
	if _, err := NewOperatorCompensation(OperatorCompensationSpec{
		CommandID: "operator-compensate", Instance: readyCompensation, Definition: compensationDefinition,
		StepName: "reserve", Attempt: 1, IdempotencyKey: "key-1",
		Actor: "operator-1", Reason: "manual-compensation",
		OccurredAt: now.Add(5 * time.Second), Deadline: now.Add(time.Hour),
	}); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("invalid compensation action error = %v", err)
	}

	failedCompensation := internalCompensationProcessorFailedInstance(compensationDefinition, now)
	if _, err := NewOperatorCompensationResolution(OperatorCompensationResolutionSpec{
		CommandID: "operator-resolution", Instance: failedCompensation, Definition: compensationDefinition,
		StepName: "reserve", Actor: "operator-1", Reason: "manual-resolution",
		OccurredAt: now.Add(7 * time.Second),
	}); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("invalid resolution action error = %v", err)
	}
	if _, err := NewOperatorApproval(OperatorApprovalSpec{
		CommandID: "operator-approval", Instance: failedCompensation, Definition: compensationDefinition,
		StepName: "missing", Actor: "operator-1", Reason: "manual-approval",
		OccurredAt: now.Add(7 * time.Second),
	}); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("invalid approval action error = %v", err)
	}
}

func TestOperatorWorkHelpersRejectInvalidAuditDocumentAndTransition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	definition := internalActivityTransitionDefinition(t)
	instance := internalActivityInstance(definition, now, ActivityProgressFailed, 1, true)
	if _, _, err := newOperatorWorkAudit(
		"operator-command", instance, OperatorAction(99), "operator-1", "manual-action", now.Add(time.Second),
	); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("invalid audit document error = %v", err)
	}
	audit, _, err := newOperatorWorkAudit(
		"operator-command", instance, OperatorRetryActivity, "operator-1", "manual-action", now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("construct audit: %v", err)
	}
	if _, err := newOperatorWorkTransition(" spaces ", instance, audit, Transition{}); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("invalid combined transition error = %v", err)
	}
}
