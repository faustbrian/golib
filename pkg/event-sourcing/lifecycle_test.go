package eventsourcing_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestLifecycleRecordsAppliesAndAcknowledgesExactlyOnce(t *testing.T) {
	event, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    "account.opened",
		Version: 1,
		Value:   accountOpened{Owner: "customer-9"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var lifecycle eventsourcing.Lifecycle
	var owner string
	err = lifecycle.Record(event, func(applied eventsourcing.DecodedEvent) error {
		owner = applied.Value().(accountOpened).Owner

		return nil
	})
	if err != nil {
		t.Fatalf("record event: %v", err)
	}

	if owner != "customer-9" {
		t.Fatalf("event was not applied immediately: owner=%q", owner)
	}
	if lifecycle.CommittedVersion() != 0 || lifecycle.Version() != 1 {
		t.Fatalf(
			"versions = committed %d, current %d",
			lifecycle.CommittedVersion(),
			lifecycle.Version(),
		)
	}

	changes, err := lifecycle.Changes()
	if err != nil {
		t.Fatalf("get changes: %v", err)
	}
	if changes.BaseVersion() != 0 || changes.Len() != 1 {
		t.Fatalf("changes = base %d, count %d", changes.BaseVersion(), changes.Len())
	}
	events := changes.Events()
	if len(events) != 1 ||
		events[0].Name().String() != "account.opened" ||
		events[0].Version() != 1 {
		t.Fatalf("unexpected changes: %v", events)
	}
	events[0] = eventsourcing.DecodedEvent{}
	if changes.Events()[0].Name().String() != "account.opened" {
		t.Fatal("change set exposed its event slice")
	}

	pending, message := persistedLifecycleMessage(t, "account.opened", 1, 1, []byte("{}"))
	if err := lifecycle.Acknowledge(
		changes,
		[]eventsourcing.PendingMessage{pending},
		[]eventsourcing.Message{message},
	); err != nil {
		t.Fatalf("acknowledge changes: %v", err)
	}
	if lifecycle.CommittedVersion() != 1 || lifecycle.Version() != 1 {
		t.Fatalf(
			"acknowledged versions = committed %d, current %d",
			lifecycle.CommittedVersion(),
			lifecycle.Version(),
		)
	}
	remaining, err := lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if !remaining.Empty() || remaining.Len() != 0 {
		t.Fatalf("remaining changes = %d", remaining.Len())
	}

	if err := lifecycle.Acknowledge(
		changes,
		[]eventsourcing.PendingMessage{pending},
		[]eventsourcing.Message{message},
	); !errors.Is(err, eventsourcing.ErrInvalidChangeSet) {
		t.Fatalf("second acknowledgement error = %v, want ErrInvalidChangeSet", err)
	}
}

func TestLifecyclePoisonedAfterApplyFailure(t *testing.T) {
	t.Parallel()

	event := lifecycleEvent(t, "account.email-changed", 1)
	sentinel := errors.New("apply rejected event")
	var lifecycle eventsourcing.Lifecycle

	err := lifecycle.Record(event, func(eventsourcing.DecodedEvent) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Record() error = %v, want wrapped sentinel", err)
	}
	if !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("Record() error = %v, want ErrLifecyclePoisoned", err)
	}
	if !lifecycle.Poisoned() {
		t.Fatal("Poisoned() = false, want true")
	}
	if lifecycle.Version() != 0 || lifecycle.CommittedVersion() != 0 {
		t.Fatalf(
			"versions = (%d, %d), want (0, 0)",
			lifecycle.Version(),
			lifecycle.CommittedVersion(),
		)
	}
	if _, changesErr := lifecycle.Changes(); !errors.Is(changesErr, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("Changes() error = %v, want ErrLifecyclePoisoned", changesErr)
	}
	if recordErr := lifecycle.Record(event, func(eventsourcing.DecodedEvent) error {
		t.Fatal("apply called after lifecycle was poisoned")

		return nil
	}); !errors.Is(recordErr, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("second Record() error = %v, want ErrLifecyclePoisoned", recordErr)
	}
}

func TestLifecycleContainsApplyPanicWithoutDisclosingValue(t *testing.T) {
	t.Parallel()

	event := lifecycleEvent(t, "account.email-changed", 1)
	secret := "credential-secret"
	var lifecycle eventsourcing.Lifecycle

	err := lifecycle.Record(event, func(eventsourcing.DecodedEvent) error {
		panic(secret)
	})
	if err == nil {
		t.Fatal("Record() error = nil, want contained panic")
	}
	if !errors.Is(err, eventsourcing.ErrApplyPanic) {
		t.Fatalf("Record() error = %v, want ErrApplyPanic", err)
	}
	if !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("Record() error = %v, want ErrLifecyclePoisoned", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Record() error disclosed panic value: %q", err)
	}
}

func TestLifecycleReconstitutesOrderedAndSplitHistory(t *testing.T) {
	t.Parallel()

	history := []eventsourcing.HistoricalEvent{
		historicalEvent(t, 1, 0, 1, "account.opened"),
		historicalEvent(t, 2, 0, 2, "account.owner-renamed"),
		historicalEvent(t, 2, 1, 2, "account.contact-renamed"),
		historicalEvent(t, 3, 0, 1, "account.email-changed"),
	}
	var lifecycle eventsourcing.Lifecycle
	var applied []string

	err := lifecycle.Reconstitute(0, history, func(event eventsourcing.DecodedEvent) error {
		applied = append(applied, event.Name().String())

		return nil
	})
	if err != nil {
		t.Fatalf("Reconstitute() error = %v", err)
	}
	want := []string{
		"account.opened",
		"account.owner-renamed",
		"account.contact-renamed",
		"account.email-changed",
	}
	if !slices.Equal(applied, want) {
		t.Fatalf("applied = %v, want %v", applied, want)
	}
	if lifecycle.CommittedVersion() != 3 || lifecycle.Version() != 3 {
		t.Fatalf(
			"versions = (%d, %d), want (3, 3)",
			lifecycle.CommittedVersion(),
			lifecycle.Version(),
		)
	}
	changes, err := lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if !changes.Empty() {
		t.Fatalf("Changes().Len() = %d, want 0", changes.Len())
	}
}

func TestLifecycleRejectsCorruptHistoryBeforeApplication(t *testing.T) {
	t.Parallel()

	tests := map[string][]eventsourcing.HistoricalEvent{
		"missing source version": {
			historicalEvent(t, 2, 0, 1, "account.opened"),
		},
		"missing split segment": {
			historicalEvent(t, 1, 0, 2, "account.opened"),
		},
		"segment starts after zero": {
			historicalEvent(t, 1, 1, 2, "account.opened"),
		},
		"segment count changes": {
			historicalEvent(t, 1, 0, 2, "account.opened"),
			historicalEvent(t, 1, 1, 3, "account.owner-renamed"),
		},
		"source repeats after completion": {
			historicalEvent(t, 1, 0, 1, "account.opened"),
			historicalEvent(t, 1, 0, 1, "account.owner-renamed"),
		},
	}

	for name, history := range tests {
		history := history
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var lifecycle eventsourcing.Lifecycle
			applied := false
			err := lifecycle.Reconstitute(0, history, func(eventsourcing.DecodedEvent) error {
				applied = true

				return nil
			})
			if !errors.Is(err, eventsourcing.ErrCorruptHistory) {
				t.Fatalf("Reconstitute() error = %v, want ErrCorruptHistory", err)
			}
			if applied {
				t.Fatal("apply called for structurally corrupt history")
			}
			if !lifecycle.Poisoned() {
				t.Fatal("Poisoned() = false, want true")
			}
		})
	}
}

func TestHistoricalEventRejectsInvalidSourceCoordinates(t *testing.T) {
	t.Parallel()

	event := lifecycleEvent(t, "account.opened", 1)
	tests := map[string]eventsourcing.HistoricalEventInput{
		"zero source version": {Event: event, SegmentCount: 1},
		"zero segment count":  {Event: event, SourceVersion: 1},
		"segment out of range": {
			Event:         event,
			SourceVersion: 1,
			SegmentIndex:  1,
			SegmentCount:  1,
		},
		"segment count over limit": {
			Event:         event,
			SourceVersion: 1,
			SegmentCount:  eventsourcing.MaxUpcastSegments + 1,
		},
		"zero event": {SourceVersion: 1, SegmentCount: 1},
	}

	for name, input := range tests {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := eventsourcing.NewHistoricalEvent(input); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("NewHistoricalEvent() error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func TestDecodedEventRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := map[string]eventsourcing.DecodedEventInput{
		"invalid name":    {Name: "AccountOpened", Version: 1, Value: accountOpened{}},
		"zero version":    {Name: "account.opened", Value: accountOpened{}},
		"nil event value": {Name: "account.opened", Version: 1},
	}
	for name, input := range tests {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := eventsourcing.NewDecodedEvent(input); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("NewDecodedEvent() error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func TestHistoricalEventExposesCoordinates(t *testing.T) {
	t.Parallel()

	historical := historicalEvent(t, 9, 1, 3, "account.owner-renamed")
	if historical.SourceVersion() != 9 ||
		historical.SegmentIndex() != 1 ||
		historical.SegmentCount() != 3 ||
		historical.Event().Name().String() != "account.owner-renamed" {
		t.Fatalf(
			"historical coordinates = (%d, %d, %d, %s)",
			historical.SourceVersion(),
			historical.SegmentIndex(),
			historical.SegmentCount(),
			historical.Event().Name(),
		)
	}
}

func TestLifecycleRejectsInvalidRecordInputsWithoutPoisoning(t *testing.T) {
	t.Parallel()

	var lifecycle eventsourcing.Lifecycle
	event := lifecycleEvent(t, "account.opened", 1)

	if err := lifecycle.Record(eventsourcing.DecodedEvent{}, func(eventsourcing.DecodedEvent) error {
		return nil
	}); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Record(zero) error = %v, want ErrInvalidArgument", err)
	}
	if err := lifecycle.Record(event, nil); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Record(nil) error = %v, want ErrInvalidArgument", err)
	}
	if lifecycle.Poisoned() {
		t.Fatal("invalid programmer input poisoned lifecycle")
	}
}

func TestLifecycleReconstitutionFailurePoisonsLifecycle(t *testing.T) {
	t.Parallel()

	event := historicalEvent(t, 1, 0, 1, "account.opened")
	sentinel := errors.New("historical event rejected")
	tests := map[string]func(eventsourcing.DecodedEvent) error{
		"error": func(eventsourcing.DecodedEvent) error {
			return sentinel
		},
		"panic": func(eventsourcing.DecodedEvent) error {
			panic("private-panic-value")
		},
	}

	for name, apply := range tests {
		apply := apply
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var lifecycle eventsourcing.Lifecycle
			err := lifecycle.Reconstitute(0, []eventsourcing.HistoricalEvent{event}, apply)
			if !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
				t.Fatalf("Reconstitute() error = %v, want ErrLifecyclePoisoned", err)
			}
			if name == "error" && !errors.Is(err, sentinel) {
				t.Fatalf("Reconstitute() error = %v, want sentinel", err)
			}
			if name == "panic" && !errors.Is(err, eventsourcing.ErrApplyPanic) {
				t.Fatalf("Reconstitute() error = %v, want ErrApplyPanic", err)
			}
			if strings.Contains(err.Error(), "private-panic-value") {
				t.Fatalf("Reconstitute() disclosed panic value: %q", err)
			}
			if !lifecycle.Poisoned() {
				t.Fatal("Poisoned() = false, want true")
			}
		})
	}
}

func TestLifecycleReconstitutionRequiresFreshLifecycleAndApply(t *testing.T) {
	t.Parallel()

	var lifecycle eventsourcing.Lifecycle
	if err := lifecycle.Reconstitute(7, nil, func(eventsourcing.DecodedEvent) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if lifecycle.CommittedVersion() != 7 {
		t.Fatalf("CommittedVersion() = %d, want 7", lifecycle.CommittedVersion())
	}
	if err := lifecycle.Reconstitute(7, nil, func(eventsourcing.DecodedEvent) error {
		return nil
	}); !errors.Is(err, eventsourcing.ErrInvalidLifecycleState) {
		t.Fatalf("second Reconstitute() error = %v, want ErrInvalidLifecycleState", err)
	}

	var empty eventsourcing.Lifecycle
	if err := empty.Reconstitute(0, nil, nil); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Reconstitute(nil) error = %v, want ErrInvalidArgument", err)
	}

	var poisoned eventsourcing.Lifecycle
	event := lifecycleEvent(t, "account.opened", 1)
	_ = poisoned.Record(event, func(eventsourcing.DecodedEvent) error {
		return errors.New("poison")
	})
	if err := poisoned.Reconstitute(0, nil, func(eventsourcing.DecodedEvent) error {
		return nil
	}); !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("poisoned Reconstitute() error = %v, want ErrLifecyclePoisoned", err)
	}

	var overflow eventsourcing.Lifecycle
	err := overflow.Reconstitute(
		^uint64(0),
		[]eventsourcing.HistoricalEvent{historicalEvent(t, 1, 0, 1, "account.opened")},
		func(eventsourcing.DecodedEvent) error { return nil },
	)
	if !errors.Is(err, eventsourcing.ErrCorruptHistory) {
		t.Fatalf("overflow Reconstitute() error = %v, want ErrCorruptHistory", err)
	}
}

func TestLifecyclePersistenceMismatchPoisonsAggregate(t *testing.T) {
	t.Parallel()

	event := lifecycleEvent(t, "account.opened", 1)
	var lifecycle eventsourcing.Lifecycle
	if err := lifecycle.Record(event, func(eventsourcing.DecodedEvent) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	changes, err := lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}
	pending, message := persistedLifecycleMessage(t, "account.closed", 1, 1, []byte("{}"))

	err = lifecycle.Acknowledge(
		changes,
		[]eventsourcing.PendingMessage{pending},
		[]eventsourcing.Message{message},
	)
	if !errors.Is(err, eventsourcing.ErrPersistenceMismatch) {
		t.Fatalf("Acknowledge() error = %v, want ErrPersistenceMismatch", err)
	}
	if !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("Acknowledge() error = %v, want ErrLifecyclePoisoned", err)
	}
	if !lifecycle.Poisoned() {
		t.Fatal("Poisoned() = false, want true")
	}
}

func TestLifecyclePersistenceCountMismatchPoisonsAggregate(t *testing.T) {
	t.Parallel()

	event := lifecycleEvent(t, "account.opened", 1)
	var lifecycle eventsourcing.Lifecycle
	if err := lifecycle.Record(event, func(eventsourcing.DecodedEvent) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	changes, err := lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}

	err = lifecycle.Acknowledge(changes, nil, nil)
	if !errors.Is(err, eventsourcing.ErrPersistenceMismatch) ||
		!errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("Acknowledge() error = %v, want poisoned persistence mismatch", err)
	}
}

func TestLifecycleRejectsPersistedMessageThatDiffersFromPreparedMessage(t *testing.T) {
	t.Parallel()

	event := lifecycleEvent(t, "account.opened", 1)
	var lifecycle eventsourcing.Lifecycle
	if err := lifecycle.Record(event, func(eventsourcing.DecodedEvent) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	changes, err := lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}
	prepared, _ := persistedLifecycleMessage(t, "account.opened", 1, 1, []byte("{}"))
	_, persisted := persistedLifecycleMessage(
		t,
		"account.opened",
		1,
		1,
		[]byte(`{"owner":"different"}`),
	)

	err = lifecycle.Acknowledge(
		changes,
		[]eventsourcing.PendingMessage{prepared},
		[]eventsourcing.Message{persisted},
	)
	if !errors.Is(err, eventsourcing.ErrPersistenceMismatch) ||
		!errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("Acknowledge() error = %v, want poisoned persistence mismatch", err)
	}
}

func TestLifecycleRejectsVersionOverflowBeforeApplying(t *testing.T) {
	t.Parallel()

	var lifecycle eventsourcing.Lifecycle
	if err := lifecycle.Reconstitute(^uint64(0), nil, func(eventsourcing.DecodedEvent) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	applied := false
	err := lifecycle.Record(
		lifecycleEvent(t, "account.opened", 1),
		func(eventsourcing.DecodedEvent) error {
			applied = true

			return nil
		},
	)
	if !errors.Is(err, eventsourcing.ErrVersionOverflow) {
		t.Fatalf("Record() error = %v, want ErrVersionOverflow", err)
	}
	if applied {
		t.Fatal("apply called after stream version exhaustion")
	}
	if lifecycle.Poisoned() {
		t.Fatal("version exhaustion poisoned lifecycle")
	}
}

func TestLifecycleCannotAcknowledgeAfterItIsPoisoned(t *testing.T) {
	t.Parallel()

	var lifecycle eventsourcing.Lifecycle
	event := lifecycleEvent(t, "account.opened", 1)
	if err := lifecycle.Record(event, func(eventsourcing.DecodedEvent) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	changes, err := lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}
	_ = lifecycle.Record(event, func(eventsourcing.DecodedEvent) error {
		return errors.New("poison")
	})

	err = lifecycle.Acknowledge(
		changes,
		nil,
		nil,
	)
	if !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("Acknowledge() error = %v, want ErrLifecyclePoisoned", err)
	}
	if lifecycle.CommittedVersion() != 0 {
		t.Fatalf("CommittedVersion() = %d, want 0", lifecycle.CommittedVersion())
	}
}

func TestLifecycleKeepsManyChangesUntilOneSuccessfulAcknowledgement(t *testing.T) {
	t.Parallel()

	var lifecycle eventsourcing.Lifecycle
	for _, name := range []string{"account.opened", "account.email-changed"} {
		if err := lifecycle.Record(
			lifecycleEvent(t, name, 1),
			func(eventsourcing.DecodedEvent) error { return nil },
		); err != nil {
			t.Fatal(err)
		}
	}

	first, err := lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}
	retry, err := lifecycle.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if first.BaseVersion() != 0 || retry.BaseVersion() != 0 ||
		first.Len() != 2 || retry.Len() != 2 {
		t.Fatalf(
			"change sets = first (%d, %d), retry (%d, %d)",
			first.BaseVersion(),
			first.Len(),
			retry.BaseVersion(),
			retry.Len(),
		)
	}

	firstPending, firstMessage := persistedLifecycleMessage(
		t,
		"account.opened",
		1,
		1,
		[]byte("{}"),
	)
	secondPending, secondMessage := persistedLifecycleMessage(
		t,
		"account.email-changed",
		1,
		2,
		[]byte("{}"),
	)
	if err := lifecycle.Acknowledge(
		retry,
		[]eventsourcing.PendingMessage{firstPending, secondPending},
		[]eventsourcing.Message{firstMessage, secondMessage},
	); err != nil {
		t.Fatal(err)
	}
	if lifecycle.CommittedVersion() != 2 || lifecycle.Version() != 2 {
		t.Fatalf(
			"versions = (%d, %d), want (2, 2)",
			lifecycle.CommittedVersion(),
			lifecycle.Version(),
		)
	}
	if err := lifecycle.Acknowledge(
		first,
		[]eventsourcing.PendingMessage{firstPending, secondPending},
		[]eventsourcing.Message{firstMessage, secondMessage},
	); !errors.Is(err, eventsourcing.ErrInvalidChangeSet) {
		t.Fatalf("old token error = %v, want ErrInvalidChangeSet", err)
	}
}

func TestLifecycleRejectsReentrantMutation(t *testing.T) {
	t.Parallel()

	var lifecycle eventsourcing.Lifecycle
	event := lifecycleEvent(t, "account.opened", 1)
	innerApplied := false
	var changesErr, acknowledgeErr, reconstituteErr error

	err := lifecycle.Record(event, func(eventsourcing.DecodedEvent) error {
		_, changesErr = lifecycle.Changes()
		acknowledgeErr = lifecycle.Acknowledge(eventsourcing.ChangeSet{}, nil, nil)
		reconstituteErr = lifecycle.Reconstitute(
			0,
			nil,
			func(eventsourcing.DecodedEvent) error { return nil },
		)

		return lifecycle.Record(event, func(eventsourcing.DecodedEvent) error {
			innerApplied = true

			return nil
		})
	})
	if !errors.Is(err, eventsourcing.ErrInvalidLifecycleState) {
		t.Fatalf("Record() error = %v, want ErrInvalidLifecycleState", err)
	}
	if !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("Record() error = %v, want ErrLifecyclePoisoned", err)
	}
	for operation, operationErr := range map[string]error{
		"acknowledge":  acknowledgeErr,
		"changes":      changesErr,
		"reconstitute": reconstituteErr,
	} {
		if !errors.Is(operationErr, eventsourcing.ErrInvalidLifecycleState) {
			t.Fatalf("%s error = %v, want ErrInvalidLifecycleState", operation, operationErr)
		}
	}
	if innerApplied {
		t.Fatal("inner event was applied")
	}
	if lifecycle.Version() != 0 {
		t.Fatalf("Version() = %d, want 0", lifecycle.Version())
	}
}

type accountOpened struct {
	Owner string
}

func lifecycleEvent(t *testing.T, name string, version eventsourcing.SchemaVersion) eventsourcing.DecodedEvent {
	t.Helper()

	event, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    name,
		Version: version,
		Value:   accountOpened{Owner: "customer-9"},
	})
	if err != nil {
		t.Fatal(err)
	}

	return event
}

func historicalEvent(
	t *testing.T,
	sourceVersion uint64,
	segmentIndex uint32,
	segmentCount uint32,
	name string,
) eventsourcing.HistoricalEvent {
	t.Helper()

	event, err := eventsourcing.NewHistoricalEvent(eventsourcing.HistoricalEventInput{
		SourceVersion: sourceVersion,
		SegmentIndex:  segmentIndex,
		SegmentCount:  segmentCount,
		Event:         lifecycleEvent(t, name, 1),
	})
	if err != nil {
		t.Fatal(err)
	}

	return event
}

func persistedLifecycleMessage(
	t *testing.T,
	name string,
	schemaVersion eventsourcing.SchemaVersion,
	streamVersion uint64,
	payload []byte,
) (eventsourcing.PendingMessage, eventsourcing.Message) {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("bank.account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        name,
		Version:     schemaVersion,
		ContentType: "application/json",
		Payload:     payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(eventsourcing.PendingMessageInput{
		ID:         "message-" + name,
		Stream:     stream,
		Event:      encoded,
		RecordedAt: time.Date(2026, time.July, 25, 9, 30, 45, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: streamVersion,
	})
	if err != nil {
		t.Fatal(err)
	}

	return pending, message
}
