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
