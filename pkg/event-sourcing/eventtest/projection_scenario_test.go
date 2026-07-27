package eventtest_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/eventtest"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
)

type projectionRunnerFunc func(
	context.Context,
) (projection.BatchResult, error)

func (function projectionRunnerFunc) RunBatch(
	ctx context.Context,
) (projection.BatchResult, error) {
	return function(ctx)
}

type scenarioCheckpointStore struct {
	checkpoint eventsourcing.GlobalPosition
	has        bool
}

func (store *scenarioCheckpointStore) Status(
	context.Context,
	string,
) (projection.Status, error) {
	return projection.NewStatus(projection.StatusInput{
		State:         projection.StateRunning,
		Checkpoint:    store.checkpoint,
		HasCheckpoint: store.has,
	})
}

func (store *scenarioCheckpointStore) Save(
	_ context.Context,
	_ string,
	expected eventsourcing.GlobalPosition,
	next eventsourcing.GlobalPosition,
) error {
	if expected != store.checkpoint {
		return projection.ErrCheckpointConflict
	}
	store.checkpoint = next
	store.has = true

	return nil
}

func TestCheckProjectionScenarioRunsRealProjectionAndChecksState(
	t *testing.T,
) {
	t.Parallel()

	ctx := context.Background()
	store := memory.NewStore()
	stream, pending := projectionScenarioHistory(t)
	if _, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	); err != nil {
		t.Fatal(err)
	}
	checkpoints := &scenarioCheckpointStore{}
	projected := make([]string, 0, 2)
	runner, err := projection.NewRunner(projection.RunnerConfig{
		Name:        "eventtest-summary",
		Reader:      store,
		Checkpoints: checkpoints,
		Handler: func(
			_ context.Context,
			delivery eventsourcing.Delivery,
		) error {
			projected = append(
				projected,
				delivery.Message().Event().Name().String(),
			)

			return nil
		},
		Guard:     projection.PermitReplay,
		BatchSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = eventtest.CheckProjectionScenario(
		ctx,
		eventtest.ProjectionScenario{
			Runner: runner,
			Expected: eventtest.ExpectedProjectionBatch{
				Scanned:      2,
				Handled:      2,
				Checkpointed: 2,
				Checkpoint:   2,
			},
			State: func() bool {
				return len(projected) == 2 &&
					projected[0] == "account.opened" &&
					projected[1] == "account.renamed"
			},
		},
	)
	if err != nil {
		t.Fatalf("CheckProjectionScenario() error = %v", err)
	}
}

func TestCheckProjectionScenarioSupportsExpectedPartialFailure(
	t *testing.T,
) {
	t.Parallel()

	ctx := context.Background()
	store := memory.NewStore()
	stream, pending := projectionScenarioHistory(t)
	if _, err := store.Append(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		pending,
	); err != nil {
		t.Fatal(err)
	}
	secretFailure := errors.New("secret projection state")
	attempted := 0
	runner, err := projection.NewRunner(projection.RunnerConfig{
		Name:        "eventtest-failure",
		Reader:      store,
		Checkpoints: &scenarioCheckpointStore{},
		Handler: func(context.Context, eventsourcing.Delivery) error {
			attempted++

			return secretFailure
		},
		Guard:     projection.PermitReplay,
		BatchSize: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = eventtest.CheckProjectionScenario(
		ctx,
		eventtest.ProjectionScenario{
			Runner: runner,
			Expected: eventtest.ExpectedProjectionBatch{
				Scanned: 1,
			},
			WantError: secretFailure,
			State:     func() bool { return attempted == 1 },
		},
	)
	if err != nil {
		t.Fatalf("CheckProjectionScenario() error = %v", err)
	}
}

func TestCheckProjectionScenarioValidatesAndRedactsMismatches(t *testing.T) {
	t.Parallel()

	zeroRunner := projectionRunnerFunc(func(
		context.Context,
	) (projection.BatchResult, error) {
		return projection.BatchResult{}, nil
	})
	var nilContext context.Context
	if err := eventtest.CheckProjectionScenario(
		nilContext,
		eventtest.ProjectionScenario{Runner: zeroRunner},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil context error = %v", err)
	}
	if err := eventtest.CheckProjectionScenario(
		context.Background(),
		eventtest.ProjectionScenario{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil runner error = %v", err)
	}

	secretFailure := errors.New("secret projection error")
	tests := map[string]eventtest.ProjectionScenario{
		"scanned": {
			Runner: zeroRunner,
			Expected: eventtest.ExpectedProjectionBatch{
				Scanned: 1,
			},
		},
		"handled": {
			Runner: zeroRunner,
			Expected: eventtest.ExpectedProjectionBatch{
				Handled: 1,
			},
		},
		"filtered": {
			Runner: zeroRunner,
			Expected: eventtest.ExpectedProjectionBatch{
				Filtered: 1,
			},
		},
		"skipped": {
			Runner: zeroRunner,
			Expected: eventtest.ExpectedProjectionBatch{
				Skipped: 1,
			},
		},
		"checkpointed": {
			Runner: zeroRunner,
			Expected: eventtest.ExpectedProjectionBatch{
				Checkpointed: 1,
			},
		},
		"checkpoint": {
			Runner: zeroRunner,
			Expected: eventtest.ExpectedProjectionBatch{
				Checkpoint: 1,
			},
		},
		"state": {
			Runner: zeroRunner,
			State:  func() bool { return false },
		},
		"wrong error": {
			Runner:    zeroRunner,
			WantError: secretFailure,
		},
	}
	for name, scenario := range tests {
		scenario := scenario
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := eventtest.CheckProjectionScenario(
				context.Background(),
				scenario,
			)
			if !errors.Is(err, eventtest.ErrConformance) ||
				strings.Contains(err.Error(), "secret") {
				t.Fatalf("CheckProjectionScenario() error = %v", err)
			}
		})
	}
	failingRunner := projectionRunnerFunc(func(
		context.Context,
	) (projection.BatchResult, error) {
		return projection.BatchResult{}, secretFailure
	})
	if err := eventtest.CheckProjectionScenario(
		context.Background(),
		eventtest.ProjectionScenario{Runner: failingRunner},
	); !errors.Is(err, secretFailure) {
		t.Fatalf("unexpected runner failure = %v", err)
	}
}

func projectionScenarioHistory(
	t testing.TB,
) (eventsourcing.StreamID, []eventsourcing.PendingMessage) {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "projection-scenario")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"account.opened", "account.renamed"}
	messageIDs := []string{"projection-message-1", "projection-message-2"}
	pending := make([]eventsourcing.PendingMessage, len(names))
	for index, name := range names {
		event, err := eventsourcing.NewEncodedEvent(
			eventsourcing.EncodedEventInput{
				Name:        name,
				Version:     1,
				ContentType: "application/json",
				Payload:     []byte("{}"),
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		pending[index], err = eventsourcing.NewPendingMessage(
			eventsourcing.PendingMessageInput{
				ID:     messageIDs[index],
				Stream: stream,
				Event:  event,
				RecordedAt: time.Date(
					2026,
					7,
					26,
					13,
					index,
					0,
					0,
					time.UTC,
				),
			},
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	return stream, pending
}
