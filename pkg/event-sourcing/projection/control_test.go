package projection

import (
	"context"
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestStatusOwnsExplicitStateAndOptionalCheckpoint(t *testing.T) {
	t.Parallel()

	running := mustStatus(t, StatusInput{State: StateRunning})
	paused := mustStatus(t, StatusInput{
		State:         StatePaused,
		Checkpoint:    7,
		HasCheckpoint: true,
	})
	checkpoint, hasCheckpoint := paused.Checkpoint()
	if !running.Valid() ||
		running.State() != StateRunning ||
		StateRunning.String() != "running" ||
		!paused.Valid() ||
		paused.State() != StatePaused ||
		StatePaused.String() != "paused" ||
		!hasCheckpoint ||
		checkpoint != 7 ||
		(RunState(99)).String() != "unknown" {
		t.Fatalf(
			"running=%#v paused=%#v checkpoint=%d/%t",
			running,
			paused,
			checkpoint,
			hasCheckpoint,
		)
	}
	if checkpoint, exists := running.Checkpoint(); exists || checkpoint != 0 {
		t.Fatalf("running checkpoint = %d/%t", checkpoint, exists)
	}
	if (Status{}).Valid() {
		t.Fatal("zero status is valid")
	}

	tests := map[string]StatusInput{
		"unknown state": {
			State: RunState(99),
		},
		"checkpoint omitted": {
			State:      StateRunning,
			Checkpoint: 1,
		},
		"zero checkpoint": {
			State:         StateRunning,
			HasCheckpoint: true,
		},
	}
	for name, input := range tests {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status, err := NewStatus(input)
			if status.Valid() ||
				!errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("NewStatus() = %#v, %v", status, err)
			}
		})
	}
}

func TestCheckpointConflictErrorPreservesStableCategory(t *testing.T) {
	t.Parallel()

	err := &CheckpointConflictError{
		Expected:     4,
		Actual:       5,
		ActualExists: true,
	}
	if err.Error() != ErrCheckpointConflict.Error() ||
		!errors.Is(err, ErrCheckpointConflict) {
		t.Fatalf("CheckpointConflictError = %v", err)
	}
}

func TestControllerForwardsExplicitLifecycleOperations(t *testing.T) {
	t.Parallel()

	running := mustStatus(t, StatusInput{State: StateRunning})
	paused := mustStatus(t, StatusInput{
		State:         StatePaused,
		Checkpoint:    9,
		HasCheckpoint: true,
	})
	var calls []string
	store := controlStoreStub{
		status: func(context.Context, string) (Status, error) {
			calls = append(calls, "status")

			return running, nil
		},
		pause: func(context.Context, string) (Status, error) {
			calls = append(calls, "pause")

			return paused, nil
		},
		resume: func(context.Context, string) (Status, error) {
			calls = append(calls, "resume")

			return running, nil
		},
		reset: func(
			_ context.Context,
			_ string,
			expected eventsourcing.GlobalPosition,
		) (Status, error) {
			if expected != 9 {
				t.Fatalf("expected checkpoint = %d", expected)
			}
			calls = append(calls, "reset")

			return mustStatus(t, StatusInput{State: StatePaused}), nil
		},
	}
	controller, err := NewController("account-summary", store)
	if err != nil {
		t.Fatal(err)
	}

	status, err := controller.Status(context.Background())
	if err != nil || status.State() != StateRunning {
		t.Fatalf("Status() = %#v, %v", status, err)
	}
	status, err = controller.Pause(context.Background())
	if err != nil || status.State() != StatePaused {
		t.Fatalf("Pause() = %#v, %v", status, err)
	}
	status, err = controller.Resume(context.Background())
	if err != nil || status.State() != StateRunning {
		t.Fatalf("Resume() = %#v, %v", status, err)
	}
	status, err = controller.ResetCheckpoint(context.Background(), 9)
	if err != nil ||
		status.State() != StatePaused ||
		len(calls) != 4 ||
		calls[0] != "status" ||
		calls[1] != "pause" ||
		calls[2] != "resume" ||
		calls[3] != "reset" {
		t.Fatalf("ResetCheckpoint() = %#v, %v calls=%v", status, err, calls)
	}
}

func TestRunnerStopsAtPausedAtomicStatus(t *testing.T) {
	t.Parallel()

	paused := mustStatus(t, StatusInput{
		State:         StatePaused,
		Checkpoint:    7,
		HasCheckpoint: true,
	})
	config := internalRunnerConfig()
	config.Checkpoints = internalCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			t.Fatal("Load() called instead of atomic Status()")

			return 0, nil
		},
		status: func(context.Context, string) (Status, error) {
			return paused, nil
		},
		save: func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
			eventsourcing.GlobalPosition,
		) error {
			t.Fatal("Save() called while paused")

			return nil
		},
	}
	config.Reader = internalGlobalReader{
		read: func(
			context.Context,
			eventsourcing.ReadGlobalOptions,
		) (eventsourcing.MessageIterator, error) {
			t.Fatal("ReadGlobal() called while paused")

			return nil, nil
		},
	}
	config.Handler = func(
		context.Context,
		eventsourcing.Delivery,
	) error {
		t.Fatal("handler called while paused")

		return nil
	}
	runner := internalRunner(t, config)

	result, err := runner.RunBatch(context.Background())
	if !errors.Is(err, ErrProjectionPaused) ||
		result.Checkpoint() != 7 ||
		result.Scanned() != 0 {
		t.Fatalf("RunBatch() = %#v, %v", result, err)
	}
}

func TestControllerValidatesCompositionCallsAndStoreResults(t *testing.T) {
	t.Parallel()

	validStore := successfulControlStore(t)
	tests := map[string]struct {
		name  string
		store ControlStore
	}{
		"name": {
			store: validStore,
		},
		"store": {
			name: "account-summary",
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			controller, err := NewController(testCase.name, testCase.store)
			if controller != nil ||
				!errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("NewController() = %#v, %v", controller, err)
			}
		})
	}

	controller, err := NewController("account-summary", validStore)
	if err != nil {
		t.Fatal(err)
	}
	var nilController *Controller
	var nilContext context.Context
	operations := map[string]func(*Controller) (Status, error){
		"status": func(target *Controller) (Status, error) {
			return target.Status(nilContext)
		},
		"pause": func(target *Controller) (Status, error) {
			return target.Pause(nilContext)
		},
		"resume": func(target *Controller) (Status, error) {
			return target.Resume(nilContext)
		},
		"reset": func(target *Controller) (Status, error) {
			return target.ResetCheckpoint(nilContext, 0)
		},
	}
	for name, operation := range operations {
		operation := operation
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if status, callErr := operation(
				controller,
			); status.Valid() ||
				!errors.Is(callErr, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("operation(nil context) = %#v, %v", status, callErr)
			}
			if status, callErr := operation(
				nilController,
			); status.Valid() ||
				!errors.Is(callErr, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("operation(nil controller) = %#v, %v", status, callErr)
			}
		})
	}

	storeFailure := errors.New("control store failed")
	for name, mutate := range map[string]func(*controlStoreStub){
		"status error": func(store *controlStoreStub) {
			store.status = func(context.Context, string) (Status, error) {
				return Status{}, storeFailure
			}
		},
		"pause error": func(store *controlStoreStub) {
			store.pause = func(context.Context, string) (Status, error) {
				return Status{}, storeFailure
			}
		},
		"resume error": func(store *controlStoreStub) {
			store.resume = func(context.Context, string) (Status, error) {
				return Status{}, storeFailure
			}
		},
		"reset error": func(store *controlStoreStub) {
			store.reset = func(
				context.Context,
				string,
				eventsourcing.GlobalPosition,
			) (Status, error) {
				return Status{}, storeFailure
			}
		},
		"status corrupt": func(store *controlStoreStub) {
			store.status = func(context.Context, string) (Status, error) {
				return Status{}, nil
			}
		},
		"pause corrupt": func(store *controlStoreStub) {
			store.pause = func(context.Context, string) (Status, error) {
				return Status{}, nil
			}
		},
		"resume corrupt": func(store *controlStoreStub) {
			store.resume = func(context.Context, string) (Status, error) {
				return Status{}, nil
			}
		},
		"reset corrupt": func(store *controlStoreStub) {
			store.reset = func(
				context.Context,
				string,
				eventsourcing.GlobalPosition,
			) (Status, error) {
				return Status{}, nil
			}
		},
	} {
		mutate := mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := successfulControlStore(t)
			mutate(&store)
			controller, controllerErr := NewController(
				"account-summary",
				store,
			)
			if controllerErr != nil {
				t.Fatal(controllerErr)
			}
			var status Status
			var callErr error
			switch name {
			case "status error", "status corrupt":
				status, callErr = controller.Status(context.Background())
			case "pause error", "pause corrupt":
				status, callErr = controller.Pause(context.Background())
			case "resume error", "resume corrupt":
				status, callErr = controller.Resume(context.Background())
			default:
				status, callErr = controller.ResetCheckpoint(
					context.Background(),
					0,
				)
			}
			want := storeFailure
			if name == "status corrupt" ||
				name == "pause corrupt" ||
				name == "resume corrupt" ||
				name == "reset corrupt" {
				want = ErrCheckpointCorrupt
			}
			if status.Valid() || !errors.Is(callErr, want) {
				t.Fatalf("operation() = %#v, %v", status, callErr)
			}
		})
	}
}

func mustStatus(t *testing.T, input StatusInput) Status {
	t.Helper()

	status, err := NewStatus(input)
	if err != nil {
		t.Fatal(err)
	}

	return status
}

func successfulControlStore(t *testing.T) controlStoreStub {
	t.Helper()

	running := mustStatus(t, StatusInput{State: StateRunning})

	return controlStoreStub{
		status: func(context.Context, string) (Status, error) {
			return running, nil
		},
		pause: func(context.Context, string) (Status, error) {
			return running, nil
		},
		resume: func(context.Context, string) (Status, error) {
			return running, nil
		},
		reset: func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
		) (Status, error) {
			return running, nil
		},
	}
}

type controlStoreStub struct {
	status func(context.Context, string) (Status, error)
	pause  func(context.Context, string) (Status, error)
	resume func(context.Context, string) (Status, error)
	reset  func(
		context.Context,
		string,
		eventsourcing.GlobalPosition,
	) (Status, error)
}

func (store controlStoreStub) Load(
	ctx context.Context,
	name string,
) (eventsourcing.GlobalPosition, error) {
	status, err := store.status(ctx, name)
	if err != nil {
		return 0, err
	}
	checkpoint, exists := status.Checkpoint()
	if !exists {
		return 0, ErrCheckpointNotFound
	}

	return checkpoint, nil
}

func (store controlStoreStub) Save(
	context.Context,
	string,
	eventsourcing.GlobalPosition,
	eventsourcing.GlobalPosition,
) error {
	return nil
}

func (store controlStoreStub) Status(
	ctx context.Context,
	name string,
) (Status, error) {
	return store.status(ctx, name)
}

func (store controlStoreStub) Pause(
	ctx context.Context,
	name string,
) (Status, error) {
	return store.pause(ctx, name)
}

func (store controlStoreStub) Resume(
	ctx context.Context,
	name string,
) (Status, error) {
	return store.resume(ctx, name)
}

func (store controlStoreStub) ResetCheckpoint(
	ctx context.Context,
	name string,
	expected eventsourcing.GlobalPosition,
) (Status, error) {
	return store.reset(ctx, name, expected)
}
