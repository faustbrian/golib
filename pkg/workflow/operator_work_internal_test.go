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

func TestOperatorWorkAuditValidatesEachBoundaryIndependently(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 15, 0, 0, 0, time.UTC)
	definition := internalActivityTransitionDefinition(t)
	base := internalActivityInstance(definition, now, ActivityProgressFailed, 1, true)
	for name, mutate := range map[string]func(*string, *Instance, *time.Time){
		"command id": func(commandID *string, _ *Instance, _ *time.Time) { *commandID = " spaces " },
		"zero time":  func(_ *string, _ *Instance, occurredAt *time.Time) { *occurredAt = time.Time{} },
		"old time":   func(_ *string, _ *Instance, occurredAt *time.Time) { *occurredAt = now.Add(-time.Second) },
		"sequence":   func(_ *string, instance *Instance, _ *time.Time) { instance.sequence = ^uint64(0) - 1 },
		"instance id": func(_ *string, instance *Instance, _ *time.Time) {
			instance.id = " spaces "
		},
		"definition": func(_ *string, instance *Instance, _ *time.Time) {
			instance.definition = DefinitionReference{}
		},
	} {
		commandID := "operator-command"
		instance := base
		occurredAt := now.Add(time.Second)
		mutate(&commandID, &instance, &occurredAt)
		if _, _, err := newOperatorWorkAudit(
			commandID, instance, OperatorRetryActivity, "operator-1", "manual-action", occurredAt,
		); !errors.Is(err, ErrInvalidOperatorCommand) {
			t.Fatalf("%s audit error = %v", name, err)
		}
	}

	maximum := base
	maximum.sequence = ^uint64(0) - 2
	audit, commandInstance, err := newOperatorWorkAudit(
		"operator-command", maximum, OperatorRetryActivity,
		"operator-1", "manual-action", now.Add(time.Second),
	)
	if err != nil || audit.Sequence() != ^uint64(0)-1 || commandInstance.Sequence() != ^uint64(0)-1 {
		t.Fatalf("maximum sequence audit = %#v, instance = %#v, error %v", audit, commandInstance, err)
	}
}

func TestOperatorCompensationResolutionValidatesEachBoundaryIndependently(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 16, 0, 0, 0, time.UTC)
	definition := internalCompensationTransitionDefinition(t)
	instance := internalCompensationProcessorFailedInstance(definition, now)
	valid := OperatorCompensationResolutionSpec{
		CommandID: "operator-resolution", Instance: instance, Definition: definition,
		StepName: "reserve", Actor: "operator-1", Reason: "manual-resolution",
		Code: "accepted-loss", OccurredAt: now.Add(7 * time.Second),
	}
	step, _ := definitionActivityStep(definition, "reserve")
	valid.Evidence = make([]byte, step.Compensation.ResultLimit)
	if _, err := NewOperatorCompensationResolution(valid); err != nil {
		t.Fatalf("valid resolution error = %v", err)
	}

	invalidAudit := valid
	invalidAudit.CommandID = " spaces "
	wrongDefinition := valid
	wrongDefinition.Definition = internalActivityTransitionDefinition(t)
	missingStep := valid
	missingStep.StepName = "missing"
	withoutCompensation := valid
	withoutCompensation.Definition = internalCompensationTransitionDefinition(t)
	withoutCompensation.Instance.definition = withoutCompensation.Definition.Reference()
	withoutCompensation.Definition.spec.Steps[0].Compensation = nil
	missingProgress := valid
	missingProgress.Instance.compensations = map[string]CompensationProgress{}
	wrongStatus := valid
	wrongStatus.Instance.compensations = map[string]CompensationProgress{
		"reserve": {stepName: "reserve", status: CompensationReady},
	}
	invalidCode := valid
	invalidCode.Code = " spaces "
	oversized := valid
	oversized.Evidence = make([]byte, step.Compensation.ResultLimit+1)
	for name, spec := range map[string]OperatorCompensationResolutionSpec{
		"audit": invalidAudit, "definition": wrongDefinition, "step": missingStep,
		"compensation": withoutCompensation, "progress": missingProgress,
		"status": wrongStatus, "code": invalidCode, "evidence": oversized,
	} {
		if _, err := NewOperatorCompensationResolution(spec); !errors.Is(err, ErrInvalidOperatorCommand) {
			t.Fatalf("%s resolution error = %v", name, err)
		}
	}
}

func TestOperatorApprovalAcceptsTheExactPayloadLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 17, 0, 0, 0, time.UTC)
	definition, err := NewDefinition(DefinitionSpec{
		Name: "approval", Version: "1", Mode: Orchestration,
		Steps: []StepSpec{{
			Name: "approve", Kind: StepApproval, Target: "finance.approval",
			Timeout: time.Minute, InputLimit: 4,
		}},
	})
	if err != nil {
		t.Fatalf("construct approval definition: %v", err)
	}
	instance := Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 1, startedAt: now, updatedAt: now,
	}
	valid := OperatorApprovalSpec{
		CommandID: "approval-command", Instance: instance, Definition: definition,
		StepName: "approve", Actor: "operator-1", Reason: "manual-approval",
		OccurredAt: now.Add(time.Second), Payload: []byte("1234"),
	}
	transition, err := NewOperatorApproval(valid)
	if err != nil || len(transition.Events()) != 2 {
		t.Fatalf("boundary approval = %#v, error %v", transition, err)
	}
	valid.Definition = internalActivityTransitionDefinition(t)
	if _, err := NewOperatorApproval(valid); !errors.Is(err, ErrInvalidOperatorCommand) {
		t.Fatalf("mismatched approval definition error = %v", err)
	}
}
