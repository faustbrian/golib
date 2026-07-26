package memory

import (
	"context"
	"sync"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
)

// ProjectionStore is a concurrency-safe in-memory checkpoint and projection
// control store.
//
// It provides process-local durability only. Its zero value is invalid;
// construct it with NewProjectionStore.
type ProjectionStore struct {
	mutex       sync.RWMutex
	checkpoints map[string]eventsourcing.GlobalPosition
	states      map[string]projection.RunState
}

// NewProjectionStore constructs an empty running projection store.
func NewProjectionStore() *ProjectionStore {
	return &ProjectionStore{
		checkpoints: make(map[string]eventsourcing.GlobalPosition),
		states:      make(map[string]projection.RunState),
	}
}

// Load returns one checkpoint or ErrCheckpointNotFound.
func (store *ProjectionStore) Load(
	ctx context.Context,
	name string,
) (eventsourcing.GlobalPosition, error) {
	if err := store.validate(ctx, name); err != nil {
		return 0, err
	}

	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	checkpoint, exists := store.checkpoints[name]
	if !exists {
		return 0, projection.ErrCheckpointNotFound
	}

	return checkpoint, nil
}

// Status returns one atomic run-state and checkpoint snapshot.
func (store *ProjectionStore) Status(
	ctx context.Context,
	name string,
) (projection.Status, error) {
	if err := store.validate(ctx, name); err != nil {
		return projection.Status{}, err
	}

	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if err := ctx.Err(); err != nil {
		return projection.Status{}, err
	}

	return store.status(name), nil
}

// Save atomically advances one running projection checkpoint.
func (store *ProjectionStore) Save(
	ctx context.Context,
	name string,
	expected eventsourcing.GlobalPosition,
	next eventsourcing.GlobalPosition,
) error {
	if err := store.validate(ctx, name); err != nil {
		return err
	}
	if next == 0 || next <= expected {
		return eventsourcing.ErrInvalidArgument
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if store.state(name) == projection.StatePaused {
		return projection.ErrProjectionPaused
	}
	actual, exists := store.checkpoints[name]
	if exists != (expected != 0) || actual != expected {
		return checkpointConflict(expected, actual, exists)
	}
	store.checkpoints[name] = next

	return nil
}

// Pause idempotently prevents new batches and checkpoint advancement.
func (store *ProjectionStore) Pause(
	ctx context.Context,
	name string,
) (projection.Status, error) {
	if err := store.validate(ctx, name); err != nil {
		return projection.Status{}, err
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return projection.Status{}, err
	}
	store.states[name] = projection.StatePaused

	return store.status(name), nil
}

// Resume idempotently permits new batches and checkpoint advancement.
func (store *ProjectionStore) Resume(
	ctx context.Context,
	name string,
) (projection.Status, error) {
	if err := store.validate(ctx, name); err != nil {
		return projection.Status{}, err
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return projection.Status{}, err
	}
	delete(store.states, name)

	return store.status(name), nil
}

// ResetCheckpoint atomically removes the expected checkpoint while paused.
func (store *ProjectionStore) ResetCheckpoint(
	ctx context.Context,
	name string,
	expected eventsourcing.GlobalPosition,
) (projection.Status, error) {
	if err := store.validate(ctx, name); err != nil {
		return projection.Status{}, err
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return projection.Status{}, err
	}
	if store.state(name) != projection.StatePaused {
		return projection.Status{}, projection.ErrProjectionRunning
	}
	actual, exists := store.checkpoints[name]
	if exists != (expected != 0) || actual != expected {
		return projection.Status{},
			checkpointConflict(expected, actual, exists)
	}
	delete(store.checkpoints, name)

	return store.status(name), nil
}

func (store *ProjectionStore) validate(
	ctx context.Context,
	name string,
) error {
	if store == nil ||
		store.checkpoints == nil ||
		store.states == nil ||
		ctx == nil ||
		!validProjectionName(name) {
		return eventsourcing.ErrInvalidArgument
	}

	return ctx.Err()
}

func (store *ProjectionStore) status(name string) projection.Status {
	checkpoint, exists := store.checkpoints[name]
	status, _ := projection.NewStatus(projection.StatusInput{
		State:         store.state(name),
		Checkpoint:    checkpoint,
		HasCheckpoint: exists,
	})

	return status
}

func (store *ProjectionStore) state(name string) projection.RunState {
	if state, exists := store.states[name]; exists {
		return state
	}

	return projection.StateRunning
}

func checkpointConflict(
	expected eventsourcing.GlobalPosition,
	actual eventsourcing.GlobalPosition,
	exists bool,
) error {
	return &projection.CheckpointConflictError{
		Expected:     expected,
		Actual:       actual,
		ActualExists: exists,
	}
}

func validProjectionName(name string) bool {
	_, err := eventsourcing.NewStreamID("projection", name)

	return err == nil
}

var _ projection.CheckpointStore = (*ProjectionStore)(nil)
var _ projection.ControlStore = (*ProjectionStore)(nil)
