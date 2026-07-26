package projection_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
)

func TestControllerRebuildsReadModelAndCheckpointWhileRemainingPaused(
	t *testing.T,
) {
	t.Parallel()

	store := memory.NewProjectionStore()
	if err := store.Save(
		context.Background(),
		"account-summary",
		0,
		3,
	); err != nil {
		t.Fatal(err)
	}
	controller, err := projection.NewController("account-summary", store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = controller.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}

	resetCalled := false
	status, err := controller.Rebuild(
		context.Background(),
		3,
		func(ctx context.Context) error {
			resetCalled = true
			current, statusErr := controller.Status(ctx)
			if statusErr != nil ||
				current.State() != projection.StatePaused {
				t.Fatalf(
					"status during reset = %#v, %v",
					current,
					statusErr,
				)
			}

			return nil
		},
	)
	if err != nil ||
		!resetCalled ||
		status.State() != projection.StatePaused {
		t.Fatalf(
			"Rebuild() = %#v, %v reset=%t",
			status,
			err,
			resetCalled,
		)
	}
	if checkpoint, exists := status.Checkpoint(); exists || checkpoint != 0 {
		t.Fatalf("checkpoint after rebuild = %d, %t", checkpoint, exists)
	}
	if _, err = store.Load(
		context.Background(),
		"account-summary",
	); !errors.Is(err, projection.ErrCheckpointNotFound) {
		t.Fatalf("Load() = %v", err)
	}
}

func TestControllerRebuildRequiresPausedProjection(t *testing.T) {
	t.Parallel()

	store := memory.NewProjectionStore()
	controller, err := projection.NewController("account-summary", store)
	if err != nil {
		t.Fatal(err)
	}

	status, err := controller.Rebuild(
		context.Background(),
		0,
		func(context.Context) error {
			t.Fatal("read model reset while projection was running")

			return nil
		},
	)
	if !errors.Is(err, projection.ErrProjectionRunning) ||
		status.State() != projection.StateRunning {
		t.Fatalf("Rebuild() = %#v, %v", status, err)
	}
}

func TestControllerRebuildPreservesStatusFailure(t *testing.T) {
	t.Parallel()

	statusFailure := errors.New("status failure")
	store := rebuildControlStore{
		ProjectionStore: memory.NewProjectionStore(),
		status: func(
			context.Context,
			string,
		) (projection.Status, error) {
			return projection.Status{}, statusFailure
		},
	}
	controller, err := projection.NewController("account-summary", store)
	if err != nil {
		t.Fatal(err)
	}

	status, err := controller.Rebuild(
		context.Background(),
		0,
		func(context.Context) error {
			t.Fatal("read model reset after status failure")

			return nil
		},
	)
	if !errors.Is(err, statusFailure) || status.Valid() {
		t.Fatalf("Rebuild() = %#v, %v", status, err)
	}
}

func TestControllerReportsPartialRebuildAndKeepsCheckpoint(t *testing.T) {
	t.Parallel()

	resetFailure := errors.New("secret reset failure")
	checkpointFailure := errors.New("checkpoint failure")
	tests := map[string]struct {
		reset func(context.Context) error
		store projection.ControlStore
		want  error
		phase projection.RebuildPhase
	}{
		"reset failure": {
			reset: func(context.Context) error {
				return resetFailure
			},
			want:  resetFailure,
			phase: projection.RebuildReadModel,
		},
		"reset panic": {
			reset: func(context.Context) error {
				panic("secret read model state")
			},
			want:  projection.ErrReadModelResetPanic,
			phase: projection.RebuildReadModel,
		},
		"checkpoint failure": {
			reset: func(context.Context) error {
				return nil
			},
			store: rebuildControlStore{
				ProjectionStore: pausedProjectionStore(t, 3),
				reset: func(
					context.Context,
					string,
					eventsourcing.GlobalPosition,
				) (projection.Status, error) {
					return projection.Status{}, checkpointFailure
				},
			},
			want:  checkpointFailure,
			phase: projection.RebuildCheckpoint,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := testCase.store
			if store == nil {
				store = pausedProjectionStore(t, 3)
			}
			controller, err := projection.NewController(
				"account-summary",
				store,
			)
			if err != nil {
				t.Fatal(err)
			}

			status, rebuildErr := controller.Rebuild(
				context.Background(),
				3,
				testCase.reset,
			)
			var structured *projection.RebuildError
			if !errors.Is(rebuildErr, projection.ErrRebuildPartial) ||
				!errors.Is(rebuildErr, testCase.want) ||
				!errors.As(rebuildErr, &structured) ||
				!errors.Is(structured.Cause, testCase.want) ||
				structured.Phase != testCase.phase ||
				rebuildErr.Error() != "projection rebuild did not complete" ||
				strings.Contains(rebuildErr.Error(), "secret") ||
				status.State() != projection.StatePaused {
				t.Fatalf(
					"Rebuild() = %#v, %v structured=%#v",
					status,
					rebuildErr,
					structured,
				)
			}
			checkpoint, exists := status.Checkpoint()
			if !exists || checkpoint != 3 {
				t.Fatalf("last known checkpoint = %d, %t", checkpoint, exists)
			}
		})
	}
}

func TestControllerRebuildStopsAfterCallbackCancellation(t *testing.T) {
	t.Parallel()

	store := pausedProjectionStore(t, 2)
	controller, err := projection.NewController("account-summary", store)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	status, err := controller.Rebuild(
		ctx,
		2,
		func(context.Context) error {
			cancel()

			return nil
		},
	)
	if !errors.Is(err, projection.ErrRebuildPartial) ||
		!errors.Is(err, context.Canceled) ||
		rebuildPhase(err) != projection.RebuildCheckpoint ||
		status.State() != projection.StatePaused {
		t.Fatalf("Rebuild() = %#v, %v", status, err)
	}
	if checkpoint, exists := status.Checkpoint(); !exists || checkpoint != 2 {
		t.Fatalf("last known checkpoint = %d, %t", checkpoint, exists)
	}
}

func TestRebuildPhaseDiagnosticsAreStable(t *testing.T) {
	t.Parallel()

	if projection.RebuildReadModel.String() != "read_model" ||
		projection.RebuildCheckpoint.String() != "checkpoint" ||
		projection.RebuildPhase(99).String() != "unknown" {
		t.Fatal("rebuild phase diagnostics are unstable")
	}
}

func TestControllerRebuildRejectsInvalidCalls(t *testing.T) {
	t.Parallel()

	store := pausedProjectionStore(t, 1)
	controller, err := projection.NewController("account-summary", store)
	if err != nil {
		t.Fatal(err)
	}
	var nilController *projection.Controller
	var nilContext context.Context
	tests := map[string]func() (projection.Status, error){
		"nil controller": func() (projection.Status, error) {
			return nilController.Rebuild(
				context.Background(),
				1,
				func(context.Context) error { return nil },
			)
		},
		"nil context": func() (projection.Status, error) {
			return controller.Rebuild(
				nilContext,
				1,
				func(context.Context) error { return nil },
			)
		},
		"nil reset": func() (projection.Status, error) {
			return controller.Rebuild(context.Background(), 1, nil)
		},
	}
	for name, operation := range tests {
		operation := operation
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status, operationErr := operation()
			if !errors.Is(
				operationErr,
				eventsourcing.ErrInvalidArgument,
			) || status.Valid() {
				t.Fatalf("Rebuild() = %#v, %v", status, operationErr)
			}
		})
	}
}

func pausedProjectionStore(
	t *testing.T,
	checkpoint eventsourcing.GlobalPosition,
) *memory.ProjectionStore {
	t.Helper()

	store := memory.NewProjectionStore()
	if err := store.Save(
		context.Background(),
		"account-summary",
		0,
		checkpoint,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pause(
		context.Background(),
		"account-summary",
	); err != nil {
		t.Fatal(err)
	}

	return store
}

type rebuildControlStore struct {
	*memory.ProjectionStore
	status func(
		context.Context,
		string,
	) (projection.Status, error)
	reset func(
		context.Context,
		string,
		eventsourcing.GlobalPosition,
	) (projection.Status, error)
}

func rebuildPhase(err error) projection.RebuildPhase {
	var rebuildErr *projection.RebuildError
	if !errors.As(err, &rebuildErr) {
		return 0
	}

	return rebuildErr.Phase
}

func (store rebuildControlStore) Status(
	ctx context.Context,
	name string,
) (projection.Status, error) {
	if store.status != nil {
		return store.status(ctx, name)
	}

	return store.ProjectionStore.Status(ctx, name)
}

func (store rebuildControlStore) ResetCheckpoint(
	ctx context.Context,
	name string,
	expected eventsourcing.GlobalPosition,
) (projection.Status, error) {
	return store.reset(ctx, name, expected)
}
