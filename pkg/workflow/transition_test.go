package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestTransitionOwnsAtomicHistoryAndDueWork(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	event := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled,
		OccurredAt: now, StepName: "execute", Data: []byte("input"),
	})
	payload := []byte("work-input")
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "work-1", Kind: workflow.WorkActivity, InstanceID: "instance-1",
		Sequence: 2, AvailableAt: now, Deadline: now.Add(time.Minute), Payload: payload,
		TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("construct pending work: %v", err)
	}
	payload[0] = 'X'

	transition, err := workflow.NewTransition(workflow.TransitionSpec{
		ID: "transition-1", InstanceID: "instance-1", ExpectedSequence: 1,
		Definition: definition.Reference(), Events: []workflow.HistoryEvent{event},
		Work: []workflow.PendingWork{work},
	})
	if err != nil {
		t.Fatalf("construct transition: %v", err)
	}
	if transition.ID() != "transition-1" || transition.InstanceID() != "instance-1" ||
		transition.ExpectedSequence() != 1 || transition.Definition() != definition.Reference() {
		t.Fatal("transition identity was not preserved")
	}
	events := transition.Events()
	workItems := transition.Work()
	if len(events) != 1 || events[0].Sequence() != 2 || len(workItems) != 1 {
		t.Fatal("transition batch was not preserved")
	}
	if workItems[0].ID() != "work-1" || workItems[0].Kind() != workflow.WorkActivity ||
		workItems[0].InstanceID() != "instance-1" || workItems[0].Sequence() != 2 ||
		!workItems[0].AvailableAt().Equal(now) || !workItems[0].Deadline().Equal(now.Add(time.Minute)) ||
		workItems[0].TenantID() != "tenant-1" || workItems[0].CorrelationID() != "correlation-1" ||
		string(workItems[0].Payload()) != "work-input" {
		t.Fatal("pending work metadata was not preserved")
	}
	gotPayload := workItems[0].Payload()
	gotPayload[0] = 'X'
	workItems[0] = workflow.PendingWork{}
	if string(transition.Work()[0].Payload()) != "work-input" {
		t.Fatal("transition returned caller-mutable work")
	}
}

func TestTransitionRejectsNonAtomicOrUnboundedPlans(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	event := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled,
		OccurredAt: now, StepName: "execute",
	})
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "work-1", Kind: workflow.WorkActivity, InstanceID: "instance-1",
		Sequence: 2, AvailableAt: now, Deadline: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct pending work: %v", err)
	}
	valid := workflow.TransitionSpec{
		ID: "transition-1", InstanceID: "instance-1", ExpectedSequence: 1,
		Definition: definition.Reference(), Events: []workflow.HistoryEvent{event},
		Work: []workflow.PendingWork{work},
	}
	tests := map[string]func() workflow.TransitionSpec{
		"missing id":       func() workflow.TransitionSpec { spec := valid; spec.ID = ""; return spec },
		"missing instance": func() workflow.TransitionSpec { spec := valid; spec.InstanceID = ""; return spec },
		"missing definition": func() workflow.TransitionSpec {
			spec := valid
			spec.Definition = workflow.DefinitionReference{}
			return spec
		},
		"empty events":      func() workflow.TransitionSpec { spec := valid; spec.Events = nil; return spec },
		"sequence conflict": func() workflow.TransitionSpec { spec := valid; spec.ExpectedSequence = 2; return spec },
		"mixed instance": func() workflow.TransitionSpec {
			spec := valid
			spec.InstanceID = "instance-2"
			return spec
		},
		"work outside event batch": func() workflow.TransitionSpec {
			spec := valid
			bad, workErr := workflow.NewPendingWork(workflow.PendingWorkSpec{
				ID: "work-2", Kind: workflow.WorkActivity, InstanceID: "instance-1",
				Sequence: 3, AvailableAt: now, Deadline: now.Add(time.Minute),
			})
			if workErr != nil {
				t.Fatalf("construct work: %v", workErr)
			}
			spec.Work = []workflow.PendingWork{bad}
			return spec
		},
		"duplicate work": func() workflow.TransitionSpec {
			spec := valid
			spec.Work = []workflow.PendingWork{work, work}
			return spec
		},
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := workflow.NewTransition(build()); !errors.Is(err, workflow.ErrInvalidTransitionPlan) {
				t.Fatalf("error = %v, want ErrInvalidTransitionPlan", err)
			}
		})
	}
}

func TestPendingWorkRejectsInvalidOrUnboundedMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	valid := workflow.PendingWorkSpec{
		ID: "work-1", Kind: workflow.WorkActivity, InstanceID: "instance-1",
		Sequence: 1, AvailableAt: now, Deadline: now.Add(time.Second), Payload: []byte("input"),
	}
	tests := map[string]func() workflow.PendingWorkSpec{
		"id":             func() workflow.PendingWorkSpec { spec := valid; spec.ID = ""; return spec },
		"kind low":       func() workflow.PendingWorkSpec { spec := valid; spec.Kind = 0; return spec },
		"kind high":      func() workflow.PendingWorkSpec { spec := valid; spec.Kind = 99; return spec },
		"instance":       func() workflow.PendingWorkSpec { spec := valid; spec.InstanceID = ""; return spec },
		"sequence":       func() workflow.PendingWorkSpec { spec := valid; spec.Sequence = 0; return spec },
		"available time": func() workflow.PendingWorkSpec { spec := valid; spec.AvailableAt = time.Time{}; return spec },
		"deadline":       func() workflow.PendingWorkSpec { spec := valid; spec.Deadline = now; return spec },
		"payload": func() workflow.PendingWorkSpec {
			spec := valid
			spec.Payload = make([]byte, workflow.MaxPayloadBytes+1)
			return spec
		},
		"tenant":      func() workflow.PendingWorkSpec { spec := valid; spec.TenantID = " spaces "; return spec },
		"correlation": func() workflow.PendingWorkSpec { spec := valid; spec.CorrelationID = " spaces "; return spec },
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := workflow.NewPendingWork(build()); !errors.Is(err, workflow.ErrInvalidPendingWork) {
				t.Fatalf("error = %v, want ErrInvalidPendingWork", err)
			}
		})
	}
}

func TestPendingWorkAcceptsEveryKindAndExactPayloadLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	kinds := []workflow.WorkKind{
		workflow.WorkActivity, workflow.WorkTimer, workflow.WorkChild,
		workflow.WorkPublication, workflow.WorkReconciliation, workflow.WorkCompensation,
	}
	for _, kind := range kinds {
		work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
			ID: "work-1", Kind: kind, InstanceID: "instance-1", Sequence: 1,
			AvailableAt: now, Deadline: now.Add(time.Nanosecond),
			Payload: make([]byte, workflow.MaxPayloadBytes),
		})
		if err != nil {
			t.Fatalf("kind %d exact boundary rejected: %v", kind, err)
		}
		if work.Kind() != kind || len(work.Payload()) != workflow.MaxPayloadBytes {
			t.Fatal("exact pending-work boundary was not preserved")
		}
	}
}

func TestTransitionValidatesCreateAndOrderedEventBatch(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	start := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})
	pause := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused,
		OccurredAt: now.Add(time.Second),
	})
	created, err := workflow.NewTransition(workflow.TransitionSpec{
		ID: "create-1", InstanceID: "instance-1", Definition: definition.Reference(),
		Events: []workflow.HistoryEvent{start, pause},
	})
	if err != nil {
		t.Fatalf("construct create transition: %v", err)
	}
	if created.ExpectedSequence() != 0 || len(created.Events()) != 2 || created.Work() != nil {
		t.Fatal("create transition was not preserved")
	}

	wrongDefinition := mustDefinition(t, "other", "1")
	tests := map[string]workflow.TransitionSpec{
		"create without start": {
			ID: "create-1", InstanceID: "instance-1", Definition: definition.Reference(),
			Events: []workflow.HistoryEvent{mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now})},
		},
		"create definition mismatch": {
			ID: "create-1", InstanceID: "instance-1", Definition: wrongDefinition.Reference(),
			Events: []workflow.HistoryEvent{start},
		},
		"start existing instance": {
			ID: "transition-2", InstanceID: "instance-1", ExpectedSequence: 1,
			Definition: definition.Reference(), Events: []workflow.HistoryEvent{
				mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
			},
		},
		"noncontiguous batch": {
			ID: "transition-2", InstanceID: "instance-1", ExpectedSequence: 1,
			Definition: definition.Reference(), Events: []workflow.HistoryEvent{
				pause,
				mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventInstanceResumed, OccurredAt: now.Add(2 * time.Second)}),
			},
		},
		"decreasing time": {
			ID: "transition-2", InstanceID: "instance-1", ExpectedSequence: 1,
			Definition: definition.Reference(), Events: []workflow.HistoryEvent{
				pause,
				mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstanceResumed, OccurredAt: now.Add(-time.Second)}),
			},
		},
	}
	for name, spec := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := workflow.NewTransition(spec); !errors.Is(err, workflow.ErrInvalidTransitionPlan) {
				t.Fatalf("error = %v, want ErrInvalidTransitionPlan", err)
			}
		})
	}
}

func TestTransitionEnforcesBatchAndWorkAdmissionBounds(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	events := make([]workflow.HistoryEvent, workflow.MaxTransitionEvents)
	for index := range events {
		events[index] = mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: uint64(index + 2), InstanceID: "instance-1", Kind: workflow.EventInstancePaused,
			OccurredAt: now.Add(time.Duration(index) * time.Nanosecond),
		})
	}
	work := make([]workflow.PendingWork, workflow.MaxTransitionWork)
	for index := range work {
		item, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
			ID:   "work/" + time.Unix(int64(index+1), 0).UTC().Format("150405.000000000"),
			Kind: workflow.WorkActivity, InstanceID: "instance-1", Sequence: 2,
			AvailableAt: now, Deadline: now.Add(time.Second),
		})
		if err != nil {
			t.Fatalf("construct work %d: %v", index, err)
		}
		work[index] = item
	}
	valid := workflow.TransitionSpec{
		ID: "transition-boundary", InstanceID: "instance-1", ExpectedSequence: 1,
		Definition: definition.Reference(), Events: events, Work: work,
	}
	if _, err := workflow.NewTransition(valid); err != nil {
		t.Fatalf("exact transition bounds rejected: %v", err)
	}

	tooManyEvents := valid
	tooManyEvents.Events = append(append([]workflow.HistoryEvent(nil), events...), events[0])
	if _, err := workflow.NewTransition(tooManyEvents); !errors.Is(err, workflow.ErrInvalidTransitionPlan) {
		t.Fatalf("event overflow error = %v", err)
	}
	tooMuchWork := valid
	tooMuchWork.Work = append(append([]workflow.PendingWork(nil), work...), work[0])
	if _, err := workflow.NewTransition(tooMuchWork); !errors.Is(err, workflow.ErrInvalidTransitionPlan) {
		t.Fatalf("work overflow error = %v", err)
	}
}

func TestTransitionBoundsAggregatePersistedPayload(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	event := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstanceFailed,
		OccurredAt: now, Data: make([]byte, workflow.MaxTransitionBytes),
	})
	valid := workflow.TransitionSpec{
		ID: "transition-payload", InstanceID: "instance-1", ExpectedSequence: 1,
		Definition: definition.Reference(), Events: []workflow.HistoryEvent{event},
	}
	if _, err := workflow.NewTransition(valid); err != nil {
		t.Fatalf("exact aggregate payload boundary rejected: %v", err)
	}
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "work-1", Kind: workflow.WorkActivity, InstanceID: "instance-1", Sequence: 2,
		AvailableAt: now, Deadline: now.Add(time.Second), Payload: []byte{1},
	})
	if err != nil {
		t.Fatalf("construct work: %v", err)
	}
	valid.Work = []workflow.PendingWork{work}
	if _, err := workflow.NewTransition(valid); !errors.Is(err, workflow.ErrInvalidTransitionPlan) {
		t.Fatalf("aggregate payload overflow error = %v", err)
	}
}
