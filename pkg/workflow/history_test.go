package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestReplayDeterministicallyReconstructsPersistedLifecycle(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	now := time.Date(2026, 8, 9, 5, 0, 0, 123, time.FixedZone("EEST", 3*60*60))
	input := []byte("order-42")
	events := []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
			OccurredAt: now, Definition: definition.Reference(), Data: input,
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused,
			OccurredAt: now.Add(time.Second),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstanceResumed,
			OccurredAt: now.Add(2 * time.Second),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventInstanceCompleted,
			OccurredAt: now.Add(3 * time.Second), Data: []byte("complete"),
		}),
	}
	input[0] = 'X'

	first, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("first replay: %v", err)
	}
	second, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("second replay: %v", err)
	}

	if first.ID() != "instance-1" || first.Status() != workflow.StatusCompleted || first.Sequence() != 4 {
		t.Fatalf("unexpected state: id=%q status=%v sequence=%d", first.ID(), first.Status(), first.Sequence())
	}
	if first.Definition() != definition.Reference() {
		t.Fatal("instance lost its exact pinned definition")
	}
	if string(first.Input()) != "order-42" || string(first.Result()) != "complete" {
		t.Fatalf("unexpected persisted values: input=%q result=%q", first.Input(), first.Result())
	}
	if first.SnapshotDigest() != second.SnapshotDigest() {
		t.Fatal("the same persisted decisions produced different snapshots")
	}
	result := first.Result()
	result[0] = 'X'
	if string(first.Result()) != "complete" {
		t.Fatal("instance returned caller-mutable persisted state")
	}
	if !first.StartedAt().Equal(now.UTC()) || !first.UpdatedAt().Equal(now.Add(3*time.Second).UTC()) {
		t.Fatalf("timestamps were not canonicalized: %s %s", first.StartedAt(), first.UpdatedAt())
	}
}

func TestReplayRejectsUnpinnedOrConflictingHistory(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	now := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	start := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(), Data: []byte("input"),
	})

	tests := map[string]struct {
		events []workflow.HistoryEvent
		want   error
	}{
		"empty history": {want: workflow.ErrEmptyHistory},
		"first event is not start": {
			events: []workflow.HistoryEvent{mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now,
			})},
			want: workflow.ErrInvalidTransition,
		},
		"sequence gap": {
			events: []workflow.HistoryEvent{start, mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second),
			})},
			want: workflow.ErrHistoryConflict,
		},
		"wrong instance": {
			events: []workflow.HistoryEvent{start, mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: "instance-2", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second),
			})},
			want: workflow.ErrHistoryConflict,
		},
		"time moves backward": {
			events: []workflow.HistoryEvent{start, mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(-time.Second),
			})},
			want: workflow.ErrHistoryConflict,
		},
		"duplicate start": {
			events: []workflow.HistoryEvent{start, mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
				OccurredAt: now.Add(time.Second), Definition: definition.Reference(),
			})},
			want: workflow.ErrInvalidTransition,
		},
		"start sequence is not one": {
			events: []workflow.HistoryEvent{mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
				OccurredAt: now, Definition: definition.Reference(),
			})},
			want: workflow.ErrHistoryConflict,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := workflow.Replay(registry, test.events)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}

	missing, err := workflow.CompileDefinitions()
	if err != nil {
		t.Fatalf("compile empty registry: %v", err)
	}
	if _, err := workflow.Replay(missing, []workflow.HistoryEvent{start}); !errors.Is(err, workflow.ErrDefinitionNotFound) {
		t.Fatalf("missing definition error = %v", err)
	}

	other := mustDefinition(t, "orders", "1")
	ref, err := workflow.NewDefinitionReference(other.Name(), other.Version(), "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatalf("construct mismatched reference: %v", err)
	}
	mismatch := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: ref,
	})
	if _, err := workflow.Replay(registry, []workflow.HistoryEvent{mismatch}); !errors.Is(err, workflow.ErrDefinitionMismatch) {
		t.Fatalf("fingerprint mismatch error = %v", err)
	}
}

func TestReplayEnforcesLifecycleStateTransitions(t *testing.T) {
	t.Parallel()

	definition := mustDefinition(t, "orders", "1")
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	now := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	start := workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	}

	tests := map[string][]workflow.HistoryEventSpec{
		"resume running": {start, {Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstanceResumed, OccurredAt: now.Add(time.Second)}},
		"pause twice": {start,
			{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second)},
			{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(2 * time.Second)}},
		"cancel without request": {start, {Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstanceCancelled, OccurredAt: now.Add(time.Second)}},
		"complete while paused": {start,
			{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second)},
			{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstanceCompleted, OccurredAt: now.Add(2 * time.Second)}},
		"fail while paused": {start,
			{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second)},
			{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstanceFailed, OccurredAt: now.Add(2 * time.Second)}},
		"advance terminal": {start,
			{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstanceFailed, OccurredAt: now.Add(time.Second)},
			{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstanceTerminated, OccurredAt: now.Add(2 * time.Second)}},
		"migrate while running": {start,
			{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventDefinitionMigrated, OccurredAt: now.Add(time.Second), Definition: definition.Reference()}},
		"continue while paused": {start,
			{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second)},
			{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventContinuedAsNew, OccurredAt: now.Add(2 * time.Second), Definition: definition.Reference(), SuccessorID: "instance-2"}},
		"request cancellation twice": {start,
			{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventCancellationRequested, OccurredAt: now.Add(time.Second)},
			{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventCancellationRequested, OccurredAt: now.Add(2 * time.Second)}},
	}

	for name, specs := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			events := make([]workflow.HistoryEvent, len(specs))
			for index, spec := range specs {
				events[index] = mustHistoryEvent(t, spec)
			}
			if _, err := workflow.Replay(registry, events); !errors.Is(err, workflow.ErrInvalidTransition) {
				t.Fatalf("error = %v, want ErrInvalidTransition", err)
			}
		})
	}

	for name, final := range map[string]struct {
		events []workflow.HistoryEventSpec
		status workflow.InstanceStatus
	}{
		"cancel": {events: []workflow.HistoryEventSpec{
			{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventCancellationRequested, OccurredAt: now.Add(time.Second)},
			{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstanceCancelled, OccurredAt: now.Add(2 * time.Second)},
		}, status: workflow.StatusCancelled},
		"terminate paused": {events: []workflow.HistoryEventSpec{
			{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second)},
			{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstanceTerminated, OccurredAt: now.Add(2 * time.Second)},
		}, status: workflow.StatusTerminated},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			events := []workflow.HistoryEvent{mustHistoryEvent(t, start)}
			for _, spec := range final.events {
				events = append(events, mustHistoryEvent(t, spec))
			}
			instance, err := workflow.Replay(registry, events)
			if err != nil {
				t.Fatalf("replay: %v", err)
			}
			if instance.Status() != final.status {
				t.Fatalf("status = %v, want %v", instance.Status(), final.status)
			}
		})
	}

	missingContinuation := []workflow.HistoryEvent{
		mustHistoryEvent(t, start),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventContinuedAsNew,
			OccurredAt:  now.Add(time.Second),
			Definition:  mustDefinitionReference(t, "orders", "2", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
			SuccessorID: "instance-2",
		}),
	}
	if _, err := workflow.Replay(registry, missingContinuation); !errors.Is(err, workflow.ErrDefinitionNotFound) {
		t.Fatalf("missing continuation definition error = %v", err)
	}
}

func TestReplayUsesPersistedMigrationDecisionWithoutReexecutingCode(t *testing.T) {
	t.Parallel()

	first := mustDefinition(t, "orders", "1")
	second := mustDefinition(t, "orders", "2")
	calls := 0
	registry, err := workflow.CompileRegistry(
		[]workflow.Definition{first, second},
		[]workflow.Migration{{Name: "orders", FromVersion: "1", ToVersion: "2", Apply: func(state workflow.MigrationState) (workflow.MigrationState, error) {
			calls++
			return state, nil
		}}},
	)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	now := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	events := []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: first.Reference(), Data: []byte("v1")}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventDefinitionMigrated, OccurredAt: now.Add(2 * time.Second), Definition: second.Reference(), Data: []byte("v2")}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventInstanceResumed, OccurredAt: now.Add(3 * time.Second)}),
	}

	instance, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("replay migrated history: %v", err)
	}
	if calls != 0 {
		t.Fatalf("replay re-executed migration code %d times", calls)
	}
	if instance.Definition() != second.Reference() || string(instance.Input()) != "v2" {
		t.Fatal("replay did not use the persisted migration decision")
	}

	other := mustDefinition(t, "customers", "2")
	crossNameRegistry, err := workflow.CompileRegistry(
		[]workflow.Definition{first, second, other},
		[]workflow.Migration{{Name: "orders", FromVersion: "1", ToVersion: "2", Apply: func(state workflow.MigrationState) (workflow.MigrationState, error) { return state, nil }}},
	)
	if err != nil {
		t.Fatalf("compile cross-name registry: %v", err)
	}
	crossNameEvents := []workflow.HistoryEvent{
		events[0], events[1],
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventDefinitionMigrated, OccurredAt: now.Add(2 * time.Second), Definition: other.Reference()}),
	}
	if _, err := workflow.Replay(crossNameRegistry, crossNameEvents); !errors.Is(err, workflow.ErrInvalidMigration) {
		t.Fatalf("cross-name migration error = %v", err)
	}

	withoutEdge, err := workflow.CompileDefinitions(first, second)
	if err != nil {
		t.Fatalf("compile registry without migration: %v", err)
	}
	if _, err := workflow.Replay(withoutEdge, events[:3]); !errors.Is(err, workflow.ErrMigrationNotFound) {
		t.Fatalf("missing migration edge error = %v", err)
	}

	badFingerprint := mustDefinitionReference(t, "orders", "2", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	badFingerprintEvents := []workflow.HistoryEvent{
		events[0], events[1],
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventDefinitionMigrated, OccurredAt: now.Add(2 * time.Second), Definition: badFingerprint}),
	}
	if _, err := workflow.Replay(registry, badFingerprintEvents); !errors.Is(err, workflow.ErrDefinitionMismatch) {
		t.Fatalf("migration fingerprint error = %v", err)
	}
}

func TestReplayContinueAsNewClosesHistoryWithExplicitSuccessor(t *testing.T) {
	t.Parallel()

	first := mustDefinition(t, "orders", "1")
	second := mustDefinition(t, "orders", "2")
	registry, err := workflow.CompileDefinitions(first, second)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	now := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	events := []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: first.Reference()}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventContinuedAsNew, OccurredAt: now.Add(time.Second), Definition: second.Reference(), SuccessorID: "instance-2"}),
	}

	instance, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("replay continue-as-new: %v", err)
	}
	if instance.Status() != workflow.StatusContinuedAsNew || instance.SuccessorID() != "instance-2" {
		t.Fatalf("unexpected continuation: status=%v successor=%q", instance.Status(), instance.SuccessorID())
	}
}

func TestHistoryEventRejectsMalformedDurableRecords(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	tests := map[string]workflow.HistoryEventSpec{
		"zero sequence":                  {InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now},
		"invalid instance":               {Sequence: 1, InstanceID: " spaces ", Kind: workflow.EventInstancePaused, OccurredAt: now},
		"unknown kind":                   {Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventKind(99), OccurredAt: now},
		"missing time":                   {Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstancePaused},
		"oversized data":                 {Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now, Data: make([]byte, workflow.MaxPayloadBytes+1)},
		"start without definition":       {Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now},
		"migration without definition":   {Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventDefinitionMigrated, OccurredAt: now},
		"continuation without successor": {Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventContinuedAsNew, OccurredAt: now, Definition: mustDefinitionReference(t, "orders", "2", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")},
		"unexpected definition":          {Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now, Definition: mustDefinitionReference(t, "orders", "1", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")},
		"unexpected successor":           {Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now, SuccessorID: "instance-2"},
	}

	for name, spec := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := workflow.NewHistoryEvent(spec); !errors.Is(err, workflow.ErrInvalidHistoryEvent) {
				t.Fatalf("error = %v, want ErrInvalidHistoryEvent", err)
			}
		})
	}
}

func TestHistoryEventOwnsPersistedDataAndExposesCanonicalFields(t *testing.T) {
	t.Parallel()

	reference := mustDefinition(t, "orders", "1").Reference()
	now := time.Date(2026, 8, 9, 8, 0, 0, 123, time.FixedZone("EEST", 3*60*60))
	data := []byte("payload")
	event := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventContinuedAsNew,
		OccurredAt: now, Definition: reference, SuccessorID: "instance-2", Data: data,
	})
	data[0] = 'X'

	if event.Sequence() != 3 || event.InstanceID() != "instance-1" || event.Kind() != workflow.EventContinuedAsNew {
		t.Fatal("unexpected event identity")
	}
	if !event.OccurredAt().Equal(now.UTC()) || event.Definition() != reference || event.SuccessorID() != "instance-2" {
		t.Fatal("unexpected canonical event metadata")
	}
	got := event.Data()
	if string(got) != "payload" {
		t.Fatalf("event data = %q", got)
	}
	got[0] = 'X'
	if string(event.Data()) != "payload" {
		t.Fatal("event returned caller-mutable persisted data")
	}
}

func TestHistoryEventAcceptsExactPayloadLimit(t *testing.T) {
	t.Parallel()

	event, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstancePaused,
		OccurredAt: time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC),
		Data:       make([]byte, workflow.MaxPayloadBytes),
	})
	if err != nil {
		t.Fatalf("exact payload limit rejected: %v", err)
	}
	if len(event.Data()) != workflow.MaxPayloadBytes {
		t.Fatalf("payload size = %d", len(event.Data()))
	}
}

func TestDefinitionReferenceRejectsMalformedIdentity(t *testing.T) {
	t.Parallel()

	for name, input := range map[string][3]string{
		"name":        {" spaces ", "1", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
		"version":     {"orders", " spaces ", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
		"fingerprint": {"orders", "1", "not-a-fingerprint"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := workflow.NewDefinitionReference(input[0], input[1], input[2]); !errors.Is(err, workflow.ErrInvalidDefinitionReference) {
				t.Fatalf("error = %v, want ErrInvalidDefinitionReference", err)
			}
		})
	}
}

func mustHistoryEvent(t *testing.T, spec workflow.HistoryEventSpec) workflow.HistoryEvent {
	t.Helper()

	event, err := workflow.NewHistoryEvent(spec)
	if err != nil {
		t.Fatalf("construct history event: %v", err)
	}
	return event
}

func mustDefinitionReference(t *testing.T, name, version, fingerprint string) workflow.DefinitionReference {
	t.Helper()

	reference, err := workflow.NewDefinitionReference(name, version, fingerprint)
	if err != nil {
		t.Fatalf("construct definition reference: %v", err)
	}
	return reference
}
