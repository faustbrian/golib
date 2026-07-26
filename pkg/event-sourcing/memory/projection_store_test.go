package memory_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
)

func TestProjectionStoreControlsCheckpointLifecycle(t *testing.T) {
	t.Parallel()

	store := memory.NewProjectionStore()
	controller, err := projection.NewController("account-summary", store)
	if err != nil {
		t.Fatal(err)
	}
	status, err := controller.Status(context.Background())
	if err != nil ||
		status.State() != projection.StateRunning ||
		statusHasCheckpoint(status) {
		t.Fatalf("initial Status() = %#v, %v", status, err)
	}
	if err := store.Save(
		context.Background(),
		"account-summary",
		0,
		7,
	); err != nil {
		t.Fatal(err)
	}
	status, err = controller.Pause(context.Background())
	if err != nil ||
		status.State() != projection.StatePaused ||
		statusCheckpoint(t, status) != 7 {
		t.Fatalf("Pause() = %#v, %v", status, err)
	}
	if err := store.Save(
		context.Background(),
		"account-summary",
		7,
		8,
	); !errors.Is(err, projection.ErrProjectionPaused) {
		t.Fatalf("Save(paused) error = %v", err)
	}
	status, err = controller.Pause(context.Background())
	if err != nil || status.State() != projection.StatePaused {
		t.Fatalf("idempotent Pause() = %#v, %v", status, err)
	}
	status, err = controller.ResetCheckpoint(context.Background(), 7)
	if err != nil ||
		status.State() != projection.StatePaused ||
		statusHasCheckpoint(status) {
		t.Fatalf("ResetCheckpoint() = %#v, %v", status, err)
	}
	status, err = controller.ResetCheckpoint(context.Background(), 0)
	if err != nil || statusHasCheckpoint(status) {
		t.Fatalf("idempotent ResetCheckpoint() = %#v, %v", status, err)
	}
	if _, err := store.Load(
		context.Background(),
		"account-summary",
	); !errors.Is(err, projection.ErrCheckpointNotFound) {
		t.Fatalf("Load(reset) error = %v", err)
	}
	status, err = controller.Resume(context.Background())
	if err != nil ||
		status.State() != projection.StateRunning ||
		statusHasCheckpoint(status) {
		t.Fatalf("Resume() = %#v, %v", status, err)
	}
	status, err = controller.Resume(context.Background())
	if err != nil || status.State() != projection.StateRunning {
		t.Fatalf("idempotent Resume() = %#v, %v", status, err)
	}
	if err := store.Save(
		context.Background(),
		"account-summary",
		0,
		1,
	); err != nil {
		t.Fatalf("Save(resumed) error = %v", err)
	}
}

func TestProjectionStoreEnforcesOptimisticMonotonicProgress(t *testing.T) {
	t.Parallel()

	store := memory.NewProjectionStore()
	if err := store.Save(
		context.Background(),
		"account-summary",
		0,
		5,
	); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.Load(
		context.Background(),
		"account-summary",
	); err != nil || loaded != 5 {
		t.Fatalf("Load() = %d, %v", loaded, err)
	}
	for name, values := range map[string][2]eventsourcing.GlobalPosition{
		"stale expected": {4, 6},
		"new expected":   {0, 6},
	} {
		values := values
		t.Run(name, func(t *testing.T) {
			err := store.Save(
				context.Background(),
				"account-summary",
				values[0],
				values[1],
			)
			if !errors.Is(err, projection.ErrCheckpointConflict) {
				t.Fatalf("Save() error = %v", err)
			}
			var conflict *projection.CheckpointConflictError
			if !errors.As(err, &conflict) ||
				conflict.Expected != values[0] ||
				conflict.Actual != 5 ||
				!conflict.ActualExists {
				t.Fatalf("CheckpointConflictError = %#v", conflict)
			}
		})
	}
	for _, values := range [][2]eventsourcing.GlobalPosition{
		{5, 5},
		{5, 4},
		{0, 0},
	} {
		if err := store.Save(
			context.Background(),
			"account-summary",
			values[0],
			values[1],
		); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
			t.Fatalf("Save(%d, %d) error = %v", values[0], values[1], err)
		}
	}

	controller, err := projection.NewController("account-summary", store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.ResetCheckpoint(
		context.Background(),
		5,
	); !errors.Is(err, projection.ErrProjectionRunning) {
		t.Fatalf("ResetCheckpoint(running) error = %v", err)
	}
	if _, err := controller.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.ResetCheckpoint(
		context.Background(),
		4,
	); !errors.Is(err, projection.ErrCheckpointConflict) {
		t.Fatalf("ResetCheckpoint(stale) error = %v", err)
	}
	if loaded, err := store.Load(
		context.Background(),
		"account-summary",
	); err != nil || loaded != 5 {
		t.Fatalf("Load(after rejected reset) = %d, %v", loaded, err)
	}
}

func TestProjectionStorePausesRunnerBeforeReadingOrHandling(t *testing.T) {
	t.Parallel()

	store := memory.NewProjectionStore()
	controller, err := projection.NewController("account-summary", store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	reader := &failIfReadGlobal{t: t}
	runner, err := projection.NewRunner(projection.RunnerConfig{
		Name:        "account-summary",
		Reader:      reader,
		Checkpoints: store,
		Handler: func(context.Context, eventsourcing.Delivery) error {
			t.Fatal("handler ran while projection was paused")

			return nil
		},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.RunBatch(context.Background())
	if !errors.Is(err, projection.ErrProjectionPaused) ||
		result.Scanned() != 0 ||
		result.Handled() != 0 ||
		result.Checkpointed() != 0 {
		t.Fatalf("RunBatch(paused) = %#v, %v", result, err)
	}
}

func TestProjectionStoreRejectsCheckpointFromHandlerPausedInFlight(
	t *testing.T,
) {
	t.Parallel()

	checkpoints := memory.NewProjectionStore()
	controller, err := projection.NewController(
		"account-summary",
		checkpoints,
	)
	if err != nil {
		t.Fatal(err)
	}
	events := memory.NewStore()
	stream, err := eventsourcing.NewStreamID("account", "account-1")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.changed",
			Version:     1,
			ContentType: eventsourcing.JSONContentType,
			Payload:     []byte("{}"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:     "message-1",
			Stream: stream,
			Event:  event,
			RecordedAt: time.Date(
				2026,
				time.July,
				25,
				15,
				0,
				0,
				0,
				time.UTC,
			),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := events.Append(
		context.Background(),
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	); err != nil {
		t.Fatal(err)
	}

	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	runner, err := projection.NewRunner(projection.RunnerConfig{
		Name:        "account-summary",
		Reader:      events,
		Checkpoints: checkpoints,
		Handler: func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			close(handlerStarted)
			<-releaseHandler

			return nil
		},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	type runOutcome struct {
		result projection.BatchResult
		err    error
	}
	finished := make(chan runOutcome, 1)
	go func() {
		result, runErr := runner.RunBatch(context.Background())
		finished <- runOutcome{result: result, err: runErr}
	}()
	released := false
	completed := false
	defer func() {
		if !released {
			close(releaseHandler)
		}
		if !completed {
			<-finished
		}
	}()
	<-handlerStarted
	status, err := controller.Pause(context.Background())
	if err != nil || status.State() != projection.StatePaused {
		t.Fatalf("Pause() = %#v, %v", status, err)
	}
	close(releaseHandler)
	released = true
	outcome := <-finished
	completed = true
	if !errors.Is(outcome.err, projection.ErrProjectionPaused) ||
		outcome.result.Handled() != 1 ||
		outcome.result.Checkpointed() != 0 ||
		statusHasCheckpoint(outcomeStatus(t, controller)) {
		t.Fatalf(
			"RunBatch() = %#v, %v",
			outcome.result,
			outcome.err,
		)
	}
}

func TestProjectionStoreAllowsOnlyOneConcurrentCheckpointWinner(
	t *testing.T,
) {
	t.Parallel()

	store := memory.NewProjectionStore()
	const writers = 32
	start := make(chan struct{})
	var successes atomic.Uint32
	var conflicts atomic.Uint32
	var wait sync.WaitGroup
	wait.Add(writers)
	for range writers {
		go func() {
			defer wait.Done()
			<-start
			err := store.Save(
				context.Background(),
				"account-summary",
				0,
				1,
			)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, projection.ErrCheckpointConflict):
				conflicts.Add(1)
			default:
				t.Errorf("Save() error = %v", err)
			}
		}()
	}
	close(start)
	wait.Wait()
	if successes.Load() != 1 || conflicts.Load() != writers-1 {
		t.Fatalf(
			"successes=%d conflicts=%d",
			successes.Load(),
			conflicts.Load(),
		)
	}
}

func TestProjectionStoreValidatesReceiverContextAndNames(t *testing.T) {
	t.Parallel()

	store := memory.NewProjectionStore()
	var nilStore *memory.ProjectionStore
	var zeroStore memory.ProjectionStore
	var nilContext context.Context
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, target := range map[string]*memory.ProjectionStore{
		"nil":  nilStore,
		"zero": &zeroStore,
	} {
		target := target
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assertProjectionStoreInvalid(t, target, context.Background(), "valid")
		})
	}
	assertProjectionStoreInvalid(t, store, nilContext, "valid")
	assertProjectionStoreInvalid(t, store, context.Background(), "")

	if _, err := store.Status(
		cancelled,
		"account-summary",
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Status(cancelled) error = %v", err)
	}
	if err := store.Save(
		cancelled,
		"account-summary",
		0,
		1,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save(cancelled) error = %v", err)
	}
	if _, err := store.Pause(
		cancelled,
		"account-summary",
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Pause(cancelled) error = %v", err)
	}
	if _, err := store.Resume(
		cancelled,
		"account-summary",
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resume(cancelled) error = %v", err)
	}
	if _, err := store.ResetCheckpoint(
		cancelled,
		"account-summary",
		0,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("ResetCheckpoint(cancelled) error = %v", err)
	}
}

func assertProjectionStoreInvalid(
	t *testing.T,
	store *memory.ProjectionStore,
	ctx context.Context,
	name string,
) {
	t.Helper()

	if _, err := store.Status(
		ctx,
		name,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Status() error = %v", err)
	}
	if _, err := store.Load(
		ctx,
		name,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Load() error = %v", err)
	}
	if err := store.Save(
		ctx,
		name,
		0,
		1,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := store.Pause(
		ctx,
		name,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Pause() error = %v", err)
	}
	if _, err := store.Resume(
		ctx,
		name,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Resume() error = %v", err)
	}
	if _, err := store.ResetCheckpoint(
		ctx,
		name,
		0,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ResetCheckpoint() error = %v", err)
	}
}

func statusCheckpoint(
	t *testing.T,
	status projection.Status,
) eventsourcing.GlobalPosition {
	t.Helper()

	checkpoint, exists := status.Checkpoint()
	if !exists {
		t.Fatal("status has no checkpoint")
	}

	return checkpoint
}

func statusHasCheckpoint(status projection.Status) bool {
	_, exists := status.Checkpoint()

	return exists
}

func outcomeStatus(
	t *testing.T,
	controller *projection.Controller,
) projection.Status {
	t.Helper()

	status, err := controller.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	return status
}

type failIfReadGlobal struct {
	t *testing.T
}

func (reader *failIfReadGlobal) ReadGlobal(
	context.Context,
	eventsourcing.ReadGlobalOptions,
) (eventsourcing.MessageIterator, error) {
	reader.t.Fatal("global reader called while projection was paused")

	return nil, nil
}
