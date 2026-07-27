package projection_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
)

func TestRunnerProcessesReplayInGlobalOrderAndCheckpointsEachMessage(
	t *testing.T,
) {
	t.Parallel()

	reader := projectionReader(t, 3)
	var saves [][2]eventsourcing.GlobalPosition
	checkpoints := projectionCheckpointStore{
		load: func(context.Context, string) (eventsourcing.GlobalPosition, error) {
			return 0, projection.ErrCheckpointNotFound
		},
		save: func(
			_ context.Context,
			name string,
			expected eventsourcing.GlobalPosition,
			next eventsourcing.GlobalPosition,
		) error {
			if name != "account-summary" {
				t.Fatalf("checkpoint name = %q", name)
			}
			saves = append(saves, [2]eventsourcing.GlobalPosition{expected, next})

			return nil
		},
	}
	var handled []string
	runner := projectionRunner(t, reader, checkpoints, 2, func(
		_ context.Context,
		delivery eventsourcing.Delivery,
	) error {
		if delivery.Mode() != eventsourcing.DeliveryReplay {
			t.Fatalf("delivery mode = %s", delivery.Mode())
		}
		handled = append(handled, delivery.Message().ID().String())

		return nil
	})

	result, err := runner.RunBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Handled() != 2 ||
		result.Checkpointed() != 2 ||
		result.Checkpoint() != 2 ||
		len(handled) != 2 ||
		handled[0] != "message-1" ||
		handled[1] != "message-2" ||
		len(saves) != 2 ||
		saves[0] != [2]eventsourcing.GlobalPosition{0, 1} ||
		saves[1] != [2]eventsourcing.GlobalPosition{1, 2} {
		t.Fatalf("RunBatch() = %#v, handled=%v saves=%v", result, handled, saves)
	}
}

func TestRunnerRejectsCheckpointAheadOfRestoredHistory(t *testing.T) {
	t.Parallel()

	checkpoints := projectionCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			return 2, nil
		},
		save: func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
			eventsourcing.GlobalPosition,
		) error {
			t.Fatal("checkpoint saved while history was behind")

			return nil
		},
	}
	runner, err := projection.NewRunner(projection.RunnerConfig{
		Name:        "account-summary",
		Reader:      projectionReader(t, 1),
		Checkpoints: checkpoints,
		BatchSize:   10,
		Handler: func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			t.Fatal("handler ran while history was behind")

			return nil
		},
		AfterReplay: func(context.Context) error {
			t.Fatal("terminal hook ran while history was behind")

			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := runner.RunBatch(context.Background())
	if !errors.Is(err, projection.ErrCheckpointAheadOfHistory) ||
		!errors.Is(err, projection.ErrCheckpointCorrupt) ||
		result.Scanned() != 0 ||
		result.Checkpoint() != 2 {
		t.Fatalf("RunBatch() = %#v, %v", result, err)
	}
}

func TestRunnerReplaysUpcastLogicalEventsBeforeCheckpointingSource(t *testing.T) {
	t.Parallel()

	store := memory.NewStore()
	stream, err := eventsourcing.NewStreamID("account", "account-legacy")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "legacy.account-created",
		Version:     1,
		ContentType: eventsourcing.JSONContentType,
		Payload:     []byte(`{"owner":"Ada"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "legacy-message",
			Stream:     stream,
			Event:      legacy,
			Metadata:   map[string]string{"source": "legacy"},
			RecordedAt: time.Date(2026, time.July, 25, 14, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	); err != nil {
		t.Fatal(err)
	}
	rule, err := eventsourcing.NewUpcastRule(
		"legacy.account-created",
		1,
		func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			metadata := input.Metadata()
			metadata["migrated"] = "true"

			return []eventsourcing.UpcastEvent{
				projectionUpcastEvent(t, "account.opened", input.Event().Payload(), metadata),
				projectionUpcastEvent(t, "account.owner-set", input.Event().Payload(), metadata),
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	upcasters, err := eventsourcing.NewUpcasterChain(rule)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[projectionAccountOpened]("account.opened", 1),
		eventsourcing.JSONEvent[projectionOwnerSet]("account.owner-set", 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := eventsourcing.NewEventDecoder(codec, upcasters)
	if err != nil {
		t.Fatal(err)
	}
	var logicalNames []string
	var checkpointed eventsourcing.GlobalPosition
	runner := projectionRunner(
		t,
		store,
		projectionCheckpointStore{
			load: func(context.Context, string) (eventsourcing.GlobalPosition, error) {
				return 0, projection.ErrCheckpointNotFound
			},
			save: func(
				_ context.Context,
				_ string,
				_ eventsourcing.GlobalPosition,
				next eventsourcing.GlobalPosition,
			) error {
				if len(logicalNames) != 2 {
					t.Fatalf("checkpoint before logical events = %v", logicalNames)
				}
				checkpointed = next

				return nil
			},
		},
		1,
		func(_ context.Context, delivery eventsourcing.Delivery) error {
			if delivery.Mode() != eventsourcing.DeliveryReplay {
				t.Fatalf("delivery mode = %s", delivery.Mode())
			}
			logical, decodeErr := decoder.Decode(delivery.Message())
			if decodeErr != nil {
				return decodeErr
			}
			for _, event := range logical {
				if event.Metadata()["migrated"] != "true" {
					t.Fatalf("logical metadata = %#v", event.Metadata())
				}
				logicalNames = append(logicalNames, event.Event().Name().String())
			}

			return nil
		},
	)

	result, err := runner.RunBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Handled() != 1 ||
		result.Checkpointed() != 1 ||
		checkpointed != 1 ||
		len(logicalNames) != 2 ||
		logicalNames[0] != "account.opened" ||
		logicalNames[1] != "account.owner-set" {
		t.Fatalf(
			"RunBatch() = %#v, checkpoint=%d logical=%v",
			result,
			checkpointed,
			logicalNames,
		)
	}
}

func TestRunnerResumesAfterCheckpointAndReturnsEmptyEnd(t *testing.T) {
	t.Parallel()

	reader := projectionReader(t, 2)
	position := eventsourcing.GlobalPosition(1)
	checkpoints := projectionCheckpointStore{
		load: func(context.Context, string) (eventsourcing.GlobalPosition, error) {
			return position, nil
		},
		save: func(
			_ context.Context,
			_ string,
			expected eventsourcing.GlobalPosition,
			next eventsourcing.GlobalPosition,
		) error {
			if expected != position {
				t.Fatalf("expected checkpoint = %d", expected)
			}
			position = next

			return nil
		},
	}
	runner := projectionRunner(t, reader, checkpoints, 10, func(
		context.Context,
		eventsourcing.Delivery,
	) error {
		return nil
	})

	result, err := runner.RunBatch(context.Background())
	if err != nil || result.Checkpoint() != 2 || result.Handled() != 1 {
		t.Fatalf("first RunBatch() = %#v, %v", result, err)
	}
	result, err = runner.RunBatch(context.Background())
	if err != nil ||
		result.Checkpoint() != 2 ||
		result.Handled() != 0 ||
		result.Checkpointed() != 0 {
		t.Fatalf("second RunBatch() = %#v, %v", result, err)
	}
}

func TestRunnerReportsHandlerAndCheckpointPartialSuccess(t *testing.T) {
	t.Parallel()

	handlerFailure := errors.New("handler failed")
	checkpointFailure := errors.New("checkpoint failed")
	tests := map[string]struct {
		handle func(context.Context, eventsourcing.Delivery) error
		save   func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
			eventsourcing.GlobalPosition,
		) error
		want         error
		handled      uint32
		checkpointed uint32
	}{
		"handler": {
			handle: func(context.Context, eventsourcing.Delivery) error {
				return handlerFailure
			},
			save: func(
				context.Context,
				string,
				eventsourcing.GlobalPosition,
				eventsourcing.GlobalPosition,
			) error {
				t.Fatal("checkpoint saved after handler failure")

				return nil
			},
			want: handlerFailure,
		},
		"checkpoint": {
			handle: func(context.Context, eventsourcing.Delivery) error {
				return nil
			},
			save: func(
				context.Context,
				string,
				eventsourcing.GlobalPosition,
				eventsourcing.GlobalPosition,
			) error {
				return checkpointFailure
			},
			want:    checkpointFailure,
			handled: 1,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			checkpoints := projectionCheckpointStore{
				load: func(
					context.Context,
					string,
				) (eventsourcing.GlobalPosition, error) {
					return 0, projection.ErrCheckpointNotFound
				},
				save: testCase.save,
			}
			runner := projectionRunner(
				t,
				projectionReader(t, 1),
				checkpoints,
				1,
				testCase.handle,
			)
			result, err := runner.RunBatch(context.Background())
			if !errors.Is(err, testCase.want) ||
				result.Handled() != testCase.handled ||
				result.Checkpointed() != testCase.checkpointed {
				t.Fatalf("RunBatch() = %#v, %v", result, err)
			}
		})
	}
}

func TestRunnerRetriesIdempotentlyAfterCheckpointFailure(t *testing.T) {
	t.Parallel()

	checkpointFailure := errors.New("checkpoint failed")
	var checkpoint eventsourcing.GlobalPosition
	hasCheckpoint := false
	saveAttempts := 0
	checkpoints := projectionCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			if !hasCheckpoint {
				return 0, projection.ErrCheckpointNotFound
			}

			return checkpoint, nil
		},
		save: func(
			_ context.Context,
			_ string,
			expected eventsourcing.GlobalPosition,
			next eventsourcing.GlobalPosition,
		) error {
			saveAttempts++
			if saveAttempts == 1 {
				return checkpointFailure
			}
			if expected != 0 || next != 1 {
				t.Fatalf("checkpoint transition = %d -> %d", expected, next)
			}
			checkpoint = next
			hasCheckpoint = true

			return nil
		},
	}
	handlerCalls := 0
	transitions := 0
	applied := make(map[string]struct{}, 1)
	runner := projectionRunner(
		t,
		projectionReader(t, 1),
		checkpoints,
		1,
		func(_ context.Context, delivery eventsourcing.Delivery) error {
			handlerCalls++
			messageID := delivery.Message().ID().String()
			if _, duplicate := applied[messageID]; duplicate {
				return nil
			}
			applied[messageID] = struct{}{}
			transitions++

			return nil
		},
	)

	first, err := runner.RunBatch(context.Background())
	if !errors.Is(err, checkpointFailure) ||
		first.Handled() != 1 ||
		first.Checkpointed() != 0 {
		t.Fatalf("first RunBatch() = %#v, %v", first, err)
	}
	second, err := runner.RunBatch(context.Background())
	if err != nil ||
		second.Handled() != 1 ||
		second.Checkpointed() != 1 ||
		second.Checkpoint() != 1 {
		t.Fatalf("second RunBatch() = %#v, %v", second, err)
	}
	third, err := runner.RunBatch(context.Background())
	if err != nil || third.Scanned() != 0 || third.Checkpoint() != 1 {
		t.Fatalf("third RunBatch() = %#v, %v", third, err)
	}
	if handlerCalls != 2 || transitions != 1 || saveAttempts != 2 {
		t.Fatalf(
			"calls/transitions/saves = %d/%d/%d",
			handlerCalls,
			transitions,
			saveAttempts,
		)
	}
}

func TestRunnerContainsHandlerPanicWithoutDisclosingValue(t *testing.T) {
	t.Parallel()

	checkpoints := missingProjectionCheckpointStore()
	runner := projectionRunner(
		t,
		projectionReader(t, 1),
		checkpoints,
		1,
		func(context.Context, eventsourcing.Delivery) error {
			panic("secret projection state")
		},
	)

	result, err := runner.RunBatch(context.Background())
	if !errors.Is(err, projection.ErrHandlerPanic) ||
		strings.Contains(err.Error(), "secret") ||
		result.Handled() != 0 {
		t.Fatalf("RunBatch() = %#v, %v", result, err)
	}
}

func projectionRunner(
	t *testing.T,
	reader eventsourcing.GlobalReader,
	checkpoints projection.CheckpointStore,
	batchSize uint32,
	handler projection.Handler,
) *projection.Runner {
	t.Helper()

	runner, err := projection.NewRunner(projection.RunnerConfig{
		Name:        "account-summary",
		Reader:      reader,
		Checkpoints: checkpoints,
		BatchSize:   batchSize,
		Handler:     handler,
	})
	if err != nil {
		t.Fatal(err)
	}

	return runner
}

func projectionReader(t *testing.T, count int) eventsourcing.GlobalReader {
	t.Helper()

	store := memory.NewStore()
	stream, err := eventsourcing.NewStreamID("account", "account-1")
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < count; index++ {
		event, eventErr := eventsourcing.NewEncodedEvent(
			eventsourcing.EncodedEventInput{
				Name:        "account.changed",
				Version:     1,
				ContentType: "application/json",
				Payload:     []byte("{}"),
			},
		)
		if eventErr != nil {
			t.Fatal(eventErr)
		}
		pending, pendingErr := eventsourcing.NewPendingMessage(
			eventsourcing.PendingMessageInput{
				ID:         "message-" + string(rune('1'+index)),
				Stream:     stream,
				Event:      event,
				RecordedAt: time.Date(2026, time.July, 25, 14, 0, index, 0, time.UTC),
			},
		)
		if pendingErr != nil {
			t.Fatal(pendingErr)
		}
		expected := eventsourcing.ExpectNewStream()
		if index != 0 {
			expected = eventsourcing.ExpectExactVersion(uint64(index))
		}
		if _, appendErr := store.Append(
			context.Background(),
			stream,
			expected,
			[]eventsourcing.PendingMessage{pending},
		); appendErr != nil {
			t.Fatal(appendErr)
		}
	}

	return store
}

func projectionMessageAtPosition(
	t *testing.T,
	position eventsourcing.GlobalPosition,
) eventsourcing.Message {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-1")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.changed",
			Version:     1,
			ContentType: "application/json",
			Payload:     []byte("{}"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "checkpoint-message",
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Date(2026, time.July, 25, 14, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  1,
		GlobalPosition: position,
	})
	if err != nil {
		t.Fatal(err)
	}

	return message
}

type projectionCheckpointStore struct {
	load func(context.Context, string) (eventsourcing.GlobalPosition, error)
	save func(
		context.Context,
		string,
		eventsourcing.GlobalPosition,
		eventsourcing.GlobalPosition,
	) error
}

func (store projectionCheckpointStore) Load(
	ctx context.Context,
	name string,
) (eventsourcing.GlobalPosition, error) {
	return store.load(ctx, name)
}

func (store projectionCheckpointStore) Status(
	ctx context.Context,
	name string,
) (projection.Status, error) {
	checkpoint, err := store.load(ctx, name)
	if errors.Is(err, projection.ErrCheckpointNotFound) {
		return projection.NewStatus(projection.StatusInput{
			State: projection.StateRunning,
		})
	}
	if err != nil {
		return projection.Status{}, err
	}
	if checkpoint == 0 {
		return projection.Status{}, nil
	}

	return projection.NewStatus(projection.StatusInput{
		State:         projection.StateRunning,
		Checkpoint:    checkpoint,
		HasCheckpoint: true,
	})
}

func (store projectionCheckpointStore) Save(
	ctx context.Context,
	name string,
	expected eventsourcing.GlobalPosition,
	next eventsourcing.GlobalPosition,
) error {
	return store.save(ctx, name, expected, next)
}

func missingProjectionCheckpointStore() projectionCheckpointStore {
	return projectionCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			return 0, projection.ErrCheckpointNotFound
		},
		save: func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
			eventsourcing.GlobalPosition,
		) error {
			return nil
		},
	}
}

type projectionAccountOpened struct {
	Owner string `json:"owner"`
}

type projectionOwnerSet struct {
	Owner string `json:"owner"`
}

func projectionUpcastEvent(
	t *testing.T,
	name string,
	payload []byte,
	metadata map[string]string,
) eventsourcing.UpcastEvent {
	t.Helper()

	encoded, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        name,
			Version:     1,
			ContentType: eventsourcing.JSONContentType,
			Payload:     payload,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewUpcastEvent(encoded, metadata)
	if err != nil {
		t.Fatal(err)
	}

	return event
}
