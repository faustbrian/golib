package eventsourcing_test

import (
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestLifecycleReconstitutesStoredEventsIncrementally(t *testing.T) {
	t.Parallel()

	var lifecycle eventsourcing.Lifecycle
	var applied []string
	apply := func(event eventsourcing.DecodedEvent) error {
		applied = append(applied, event.Name().String())

		return nil
	}
	first := historicalEvent(t, 1, 0, 2, "account.opened")
	second := historicalEvent(t, 1, 1, 2, "account.owner-set")
	if err := lifecycle.ReconstituteNext(1, []eventsourcing.HistoricalEvent{
		first,
		second,
	}, apply); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ReconstituteNext(2, nil, apply); err != nil {
		t.Fatal(err)
	}
	if lifecycle.CommittedVersion() != 2 {
		t.Fatalf("CommittedVersion() = %d, want 2", lifecycle.CommittedVersion())
	}
	if len(applied) != 2 ||
		applied[0] != "account.opened" ||
		applied[1] != "account.owner-set" {
		t.Fatalf("applied = %v", applied)
	}
	changes, err := lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if !changes.Empty() || changes.BaseVersion() != 2 {
		t.Fatalf("Changes() = base %d length %d", changes.BaseVersion(), changes.Len())
	}
}

func TestLifecycleIncrementalHistoryRejectsCorruption(t *testing.T) {
	t.Parallel()

	event := lifecycleEvent(t, "account.opened", 1)
	cases := map[string]struct {
		sourceVersion uint64
		history       []eventsourcing.HistoricalEvent
	}{
		"zero source": {},
		"gap": {
			sourceVersion: 2,
		},
		"wrong history source": {
			sourceVersion: 1,
			history: []eventsourcing.HistoricalEvent{
				historicalEvent(t, 2, 0, 1, event.Name().String()),
			},
		},
	}
	for name, testCase := range cases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var lifecycle eventsourcing.Lifecycle
			err := lifecycle.ReconstituteNext(
				testCase.sourceVersion,
				testCase.history,
				func(eventsourcing.DecodedEvent) error { return nil },
			)
			if !errors.Is(err, eventsourcing.ErrCorruptHistory) {
				t.Fatalf("ReconstituteNext() error = %v", err)
			}
			if !lifecycle.Poisoned() {
				t.Fatal("corrupt incremental history did not poison lifecycle")
			}
			if err := lifecycle.ReconstituteNext(
				1,
				nil,
				func(eventsourcing.DecodedEvent) error { return nil },
			); !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
				t.Fatalf("poisoned ReconstituteNext() error = %v", err)
			}
		})
	}

	var crossed eventsourcing.Lifecycle
	err := crossed.ReconstituteNext(
		1,
		[]eventsourcing.HistoricalEvent{
			historicalEvent(t, 1, 0, 1, "account.opened"),
			historicalEvent(t, 2, 0, 1, "account.owner-set"),
		},
		func(eventsourcing.DecodedEvent) error { return nil },
	)
	if !errors.Is(err, eventsourcing.ErrCorruptHistory) || !crossed.Poisoned() {
		t.Fatalf("crossed ReconstituteNext() error = %v, poisoned %v", err, crossed.Poisoned())
	}
}

func TestLifecycleIncrementalHistoryValidatesStateAndApply(t *testing.T) {
	t.Parallel()

	var lifecycle eventsourcing.Lifecycle
	if err := lifecycle.ReconstituteNext(1, nil, nil); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("ReconstituteNext(nil apply) error = %v", err)
	}
	event := lifecycleEvent(t, "account.opened", 1)
	if err := lifecycle.Record(
		event,
		func(eventsourcing.DecodedEvent) error { return nil },
	); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ReconstituteNext(
		1,
		nil,
		func(eventsourcing.DecodedEvent) error { return nil },
	); !errors.Is(err, eventsourcing.ErrInvalidLifecycleState) {
		t.Fatalf("ReconstituteNext(pending) error = %v", err)
	}

	var failing eventsourcing.Lifecycle
	applyFailure := errors.New("apply failed")
	historical := historicalEvent(t, 1, 0, 1, event.Name().String())
	if err := failing.ReconstituteNext(
		1,
		[]eventsourcing.HistoricalEvent{historical},
		func(eventsourcing.DecodedEvent) error { return applyFailure },
	); !errors.Is(err, applyFailure) {
		t.Fatalf("ReconstituteNext(apply failure) error = %v", err)
	}
	if !failing.Poisoned() {
		t.Fatal("failed incremental apply did not poison lifecycle")
	}

	var overflow eventsourcing.Lifecycle
	if err := overflow.Reconstitute(
		^uint64(0),
		nil,
		func(eventsourcing.DecodedEvent) error { return nil },
	); err != nil {
		t.Fatal(err)
	}
	if err := overflow.ReconstituteNext(
		1,
		nil,
		func(eventsourcing.DecodedEvent) error { return nil },
	); !errors.Is(err, eventsourcing.ErrVersionOverflow) {
		t.Fatalf("ReconstituteNext(overflow) error = %v", err)
	}
}

func TestLifecycleMarksUnknownPersistenceForReconciliation(t *testing.T) {
	t.Parallel()

	var lifecycle eventsourcing.Lifecycle
	event := lifecycleEvent(t, "account.opened", 1)
	if err := lifecycle.Record(
		event,
		func(eventsourcing.DecodedEvent) error { return nil },
	); err != nil {
		t.Fatal(err)
	}
	changes, err := lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.MarkPersistenceUnknown(changes); err != nil {
		t.Fatal(err)
	}
	if !lifecycle.Poisoned() {
		t.Fatal("unknown persistence did not poison lifecycle")
	}
	if _, err := lifecycle.Changes(); !errors.Is(
		err,
		eventsourcing.ErrLifecyclePoisoned,
	) {
		t.Fatalf("Changes() error = %v", err)
	}
	if err := lifecycle.MarkPersistenceUnknown(changes); !errors.Is(
		err,
		eventsourcing.ErrLifecyclePoisoned,
	) {
		t.Fatalf("poisoned MarkPersistenceUnknown() error = %v", err)
	}

	var other eventsourcing.Lifecycle
	if err := other.MarkPersistenceUnknown(changes); !errors.Is(
		err,
		eventsourcing.ErrInvalidChangeSet,
	) {
		t.Fatalf("MarkPersistenceUnknown(foreign) error = %v", err)
	}
	if other.Poisoned() {
		t.Fatal("foreign change set poisoned unrelated lifecycle")
	}

	var transitioning eventsourcing.Lifecycle
	empty, err := transitioning.Changes()
	if err != nil {
		t.Fatal(err)
	}
	var transitionErr error
	if err := transitioning.Record(
		event,
		func(eventsourcing.DecodedEvent) error {
			transitionErr = transitioning.MarkPersistenceUnknown(empty)

			return nil
		},
	); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(transitionErr, eventsourcing.ErrInvalidLifecycleState) {
		t.Fatalf("transition MarkPersistenceUnknown() error = %v", transitionErr)
	}
}

func TestLifecycleRestoresSnapshotVersionWithoutHistory(t *testing.T) {
	t.Parallel()

	var lifecycle eventsourcing.Lifecycle
	if err := lifecycle.RestoreSnapshotVersion(7); err != nil {
		t.Fatal(err)
	}
	if lifecycle.CommittedVersion() != 7 || lifecycle.Version() != 7 {
		t.Fatalf(
			"versions = committed %d current %d",
			lifecycle.CommittedVersion(),
			lifecycle.Version(),
		)
	}
	changes, err := lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if changes.BaseVersion() != 7 || !changes.Empty() {
		t.Fatalf("changes = base %d length %d", changes.BaseVersion(), changes.Len())
	}

	if err := lifecycle.RestoreSnapshotVersion(8); !errors.Is(
		err,
		eventsourcing.ErrInvalidLifecycleState,
	) {
		t.Fatalf("second RestoreSnapshotVersion() error = %v", err)
	}
	var zero eventsourcing.Lifecycle
	if err := zero.RestoreSnapshotVersion(0); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("RestoreSnapshotVersion(0) error = %v", err)
	}

	var poisoned eventsourcing.Lifecycle
	event := lifecycleEvent(t, "account.opened", 1)
	if err := poisoned.Record(
		event,
		func(eventsourcing.DecodedEvent) error {
			return errors.New("apply failed")
		},
	); err == nil {
		t.Fatal("Record() unexpectedly succeeded")
	}
	if err := poisoned.RestoreSnapshotVersion(1); !errors.Is(
		err,
		eventsourcing.ErrLifecyclePoisoned,
	) {
		t.Fatalf("poisoned RestoreSnapshotVersion() error = %v", err)
	}
}
