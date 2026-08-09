package workflow_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestOperatorLifecycleCommandPersistsAuditBeforeAction(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	instance, err := workflow.Replay(registry, []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
			OccurredAt: now, Definition: definition.Reference(),
		}),
	})
	if err != nil {
		t.Fatalf("replay instance: %v", err)
	}
	transition, err := workflow.NewOperatorLifecycleCommand(workflow.OperatorLifecycleCommandSpec{
		CommandID: "operator-command-1", Instance: instance, Action: workflow.OperatorPause,
		Actor: "operator-1", Reason: "maintenance", OccurredAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("construct operator command: %v", err)
	}
	events := transition.Events()
	if transition.ID() != "operator-command-1" || transition.ExpectedSequence() != 1 ||
		len(events) != 2 || events[0].Kind() != workflow.EventOperatorCommandRecorded ||
		events[0].IdempotencyKey() != "operator-command-1" ||
		events[1].Kind() != workflow.EventInstancePaused {
		t.Fatalf("operator transition = %#v", transition)
	}
	history := append([]workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
			OccurredAt: now, Definition: definition.Reference(),
		}),
	}, events...)
	replayed, err := workflow.Replay(registry, history)
	if err != nil {
		t.Fatalf("replay operator command: %v", err)
	}
	actions := replayed.OperatorActions()
	if replayed.Status() != workflow.StatusPaused || len(actions) != 1 ||
		actions[0].CommandID() != "operator-command-1" ||
		actions[0].Action() != workflow.OperatorPause || actions[0].Actor() != "operator-1" ||
		actions[0].Reason() != "maintenance" || actions[0].OccurredAt() != now.Add(time.Second) {
		t.Fatalf("operator replay = %#v actions %#v", replayed, actions)
	}
	if replayed.SnapshotDigest() == instance.SnapshotDigest() {
		t.Fatal("operator audit was omitted from replay diagnostics")
	}
}

func TestOperatorLifecycleCommandsEnforceCurrentStateAndBoundedAudit(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	started := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})
	running, err := workflow.Replay(registry, []workflow.HistoryEvent{started})
	if err != nil {
		t.Fatalf("replay running: %v", err)
	}
	paused, err := workflow.Replay(registry, []workflow.HistoryEvent{
		started,
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second)}),
	})
	if err != nil {
		t.Fatalf("replay paused: %v", err)
	}
	cancelling, err := workflow.Replay(registry, []workflow.HistoryEvent{
		started,
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventCancellationRequested, OccurredAt: now.Add(time.Second)}),
	})
	if err != nil {
		t.Fatalf("replay cancelling: %v", err)
	}
	valid := workflow.OperatorLifecycleCommandSpec{
		CommandID: "operator-command-1", Instance: running, Action: workflow.OperatorPause,
		Actor: "operator-1", Reason: "maintenance", OccurredAt: now.Add(2 * time.Second),
	}
	invalid := []workflow.OperatorLifecycleCommandSpec{
		{},
		func() workflow.OperatorLifecycleCommandSpec {
			value := valid
			value.CommandID = " spaces "
			return value
		}(),
		func() workflow.OperatorLifecycleCommandSpec { value := valid; value.Actor = " spaces "; return value }(),
		func() workflow.OperatorLifecycleCommandSpec { value := valid; value.Reason = " spaces "; return value }(),
		func() workflow.OperatorLifecycleCommandSpec { value := valid; value.Action = 0; return value }(),
		func() workflow.OperatorLifecycleCommandSpec {
			value := valid
			value.OccurredAt = time.Time{}
			return value
		}(),
		func() workflow.OperatorLifecycleCommandSpec {
			value := valid
			value.OccurredAt = now.Add(-time.Second)
			return value
		}(),
		func() workflow.OperatorLifecycleCommandSpec {
			value := valid
			value.Action = workflow.OperatorResume
			return value
		}(),
		func() workflow.OperatorLifecycleCommandSpec { value := valid; value.Instance = paused; return value }(),
	}
	for _, spec := range invalid {
		if _, err := workflow.NewOperatorLifecycleCommand(spec); !errors.Is(err, workflow.ErrInvalidOperatorCommand) {
			t.Fatalf("invalid operator command error = %v for %#v", err, spec)
		}
	}
	for _, action := range []workflow.OperatorAction{
		workflow.OperatorPause, workflow.OperatorCancel, workflow.OperatorTerminate,
	} {
		spec := valid
		spec.CommandID += action.String()
		spec.Action = action
		if _, err := workflow.NewOperatorLifecycleCommand(spec); err != nil {
			t.Fatalf("running action %s: %v", action, err)
		}
	}
	resume := valid
	resume.CommandID = "operator-resume-1"
	resume.Instance = paused
	resume.Action = workflow.OperatorResume
	if _, err := workflow.NewOperatorLifecycleCommand(resume); err != nil {
		t.Fatalf("resume paused instance: %v", err)
	}
	for _, test := range []struct {
		name     string
		instance workflow.Instance
		action   workflow.OperatorAction
	}{
		{name: "cancel paused", instance: paused, action: workflow.OperatorCancel},
		{name: "terminate paused", instance: paused, action: workflow.OperatorTerminate},
		{name: "terminate cancelling", instance: cancelling, action: workflow.OperatorTerminate},
	} {
		spec := valid
		spec.CommandID = "operator-" + test.name
		spec.CommandID = strings.ReplaceAll(spec.CommandID, " ", "-")
		spec.Instance = test.instance
		spec.Action = test.action
		if _, err := workflow.NewOperatorLifecycleCommand(spec); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
	if workflow.OperatorAction(255).String() != "" {
		t.Fatal("invalid operator action has a persisted name")
	}
}

func TestOperatorAuditEventRejectsMalformedOrUnboundedFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	valid := workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventOperatorCommandRecorded,
		OccurredAt: now, IdempotencyKey: "operator-command-1",
		Data: []byte(`{"action":1,"actor":"operator-1","reason":"maintenance"}`),
	}
	maximum := valid
	maximum.Data = append(append([]byte(nil), valid.Data...), make([]byte, workflow.MaxOperatorAuditBytes-len(valid.Data))...)
	for index := len(valid.Data); index < len(maximum.Data); index++ {
		maximum.Data[index] = ' '
	}
	if _, err := workflow.NewHistoryEvent(maximum); err != nil {
		t.Fatalf("maximum operator audit: %v", err)
	}
	invalid := []workflow.HistoryEventSpec{
		func() workflow.HistoryEventSpec { value := valid; value.IdempotencyKey = " spaces "; return value }(),
		func() workflow.HistoryEventSpec { value := valid; value.StepName = "reserve"; return value }(),
		func() workflow.HistoryEventSpec { value := valid; value.Attempt = 1; return value }(),
		func() workflow.HistoryEventSpec { value := valid; value.DueAt = now.Add(time.Second); return value }(),
		func() workflow.HistoryEventSpec { value := valid; value.Code = "unexpected"; return value }(),
		func() workflow.HistoryEventSpec { value := valid; value.Retryable = true; return value }(),
		func() workflow.HistoryEventSpec { value := valid; value.Data = nil; return value }(),
		func() workflow.HistoryEventSpec {
			value := valid
			value.Data = make([]byte, workflow.MaxOperatorAuditBytes+1)
			return value
		}(),
		func() workflow.HistoryEventSpec { value := valid; value.Data = []byte("bad"); return value }(),
		func() workflow.HistoryEventSpec {
			value := valid
			value.Data = append(append([]byte(nil), valid.Data...), []byte(`{}`)...)
			return value
		}(),
		func() workflow.HistoryEventSpec {
			value := valid
			value.Data = []byte(`{"action":1,"actor":"operator-1","reason":"maintenance","extra":true}`)
			return value
		}(),
		func() workflow.HistoryEventSpec {
			value := valid
			value.Data = []byte(`{"action":99,"actor":"operator-1","reason":"maintenance"}`)
			return value
		}(),
		func() workflow.HistoryEventSpec {
			value := valid
			value.Data = []byte(`{"action":1,"actor":" spaces ","reason":"maintenance"}`)
			return value
		}(),
		func() workflow.HistoryEventSpec {
			value := valid
			value.Data = []byte(`{"action":1,"actor":"operator-1","reason":" spaces "}`)
			return value
		}(),
	}
	for _, spec := range invalid {
		if _, err := workflow.NewHistoryEvent(spec); !errors.Is(err, workflow.ErrInvalidHistoryEvent) {
			t.Fatalf("invalid operator audit error = %v for %#v", err, spec)
		}
	}
}

func TestReplayRejectsOrphanedOrMismatchedOperatorAudit(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	started := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})
	audit := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventOperatorCommandRecorded,
		OccurredAt: now.Add(time.Second), IdempotencyKey: "operator-command-1",
		Data: []byte(`{"action":1,"actor":"operator-1","reason":"maintenance"}`),
	})
	for _, history := range [][]workflow.HistoryEvent{
		{started, audit},
		{started, audit, mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstanceTerminated,
			OccurredAt: now.Add(time.Second),
		})},
	} {
		if _, err := workflow.Replay(registry, history); !errors.Is(err, workflow.ErrInvalidTransition) {
			t.Fatalf("invalid operator audit replay error = %v", err)
		}
	}
}
