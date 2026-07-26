package projection

import (
	"context"
	"errors"
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

var (
	// ErrProjectionPaused reports a projection that cannot advance while
	// explicitly paused.
	ErrProjectionPaused = errors.New("projection is paused")
	// ErrProjectionRunning reports an operation that requires a paused
	// projection.
	ErrProjectionRunning = errors.New("projection is running")
)

// RunState identifies whether a projection runner may start a new batch.
type RunState uint8

const (
	// StateRunning permits a runner to start a new batch.
	StateRunning RunState = iota + 1
	// StatePaused prevents a runner from starting or checkpointing a batch.
	StatePaused
)

// String returns a stable diagnostic state.
func (state RunState) String() string {
	switch state {
	case StateRunning:
		return "running"
	case StatePaused:
		return "paused"
	default:
		return "unknown"
	}
}

// StatusInput supplies one immutable projection status.
type StatusInput struct {
	State         RunState
	Checkpoint    eventsourcing.GlobalPosition
	HasCheckpoint bool
}

// Status is one immutable projection run-state and checkpoint snapshot.
//
// Its zero value is invalid.
type Status struct {
	state         RunState
	checkpoint    eventsourcing.GlobalPosition
	hasCheckpoint bool
	valid         bool
}

// NewStatus validates one projection status snapshot.
func NewStatus(input StatusInput) (Status, error) {
	if input.State != StateRunning && input.State != StatePaused {
		return Status{}, invalidControl("status state is unknown")
	}
	if input.HasCheckpoint != (input.Checkpoint != 0) {
		return Status{}, invalidControl(
			"status checkpoint presence is inconsistent",
		)
	}

	return Status{
		state:         input.State,
		checkpoint:    input.Checkpoint,
		hasCheckpoint: input.HasCheckpoint,
		valid:         true,
	}, nil
}

// State returns whether a runner may start new work.
func (status Status) State() RunState {
	return status.state
}

// Checkpoint returns the optional durable global position.
func (status Status) Checkpoint() (
	eventsourcing.GlobalPosition,
	bool,
) {
	return status.checkpoint, status.hasCheckpoint
}

// Valid reports whether the status was constructed successfully.
func (status Status) Valid() bool {
	return status.valid
}

// ControlStore extends checkpoint persistence with explicit operational
// controls.
//
// Pause and Resume are idempotent. ResetCheckpoint requires a paused
// projection and atomically removes only the expected checkpoint. A reset
// never modifies application read-model state.
type ControlStore interface {
	CheckpointStore
	Pause(context.Context, string) (Status, error)
	Resume(context.Context, string) (Status, error)
	ResetCheckpoint(
		context.Context,
		string,
		eventsourcing.GlobalPosition,
	) (Status, error)
}

// Controller binds one validated projection name to explicit operational
// controls.
type Controller struct {
	name  string
	store ControlStore
}

// NewController validates one projection-control composition.
func NewController(name string, store ControlStore) (*Controller, error) {
	if !validProjectionName(name) || store == nil {
		return nil, invalidControl(
			"controller name and store must be assigned",
		)
	}

	return &Controller{name: name, store: store}, nil
}

// Status returns one atomic run-state and checkpoint snapshot.
func (controller *Controller) Status(ctx context.Context) (Status, error) {
	if controller == nil || ctx == nil {
		return Status{}, eventsourcing.ErrInvalidArgument
	}

	return validateStoredStatus(controller.store.Status(ctx, controller.name))
}

// Pause prevents new runner batches and future checkpoint advancement.
//
// An already executing handler is not interrupted. Callers must drain
// in-flight work before changing application read-model state.
func (controller *Controller) Pause(ctx context.Context) (Status, error) {
	if controller == nil || ctx == nil {
		return Status{}, eventsourcing.ErrInvalidArgument
	}

	return validateStoredStatus(controller.store.Pause(ctx, controller.name))
}

// Resume permits new runner batches.
func (controller *Controller) Resume(ctx context.Context) (Status, error) {
	if controller == nil || ctx == nil {
		return Status{}, eventsourcing.ErrInvalidArgument
	}

	return validateStoredStatus(controller.store.Resume(ctx, controller.name))
}

// ResetCheckpoint atomically removes the expected durable checkpoint while
// paused. It does not reset application read-model state.
func (controller *Controller) ResetCheckpoint(
	ctx context.Context,
	expected eventsourcing.GlobalPosition,
) (Status, error) {
	if controller == nil || ctx == nil {
		return Status{}, eventsourcing.ErrInvalidArgument
	}

	return validateStoredStatus(
		controller.store.ResetCheckpoint(
			ctx,
			controller.name,
			expected,
		),
	)
}

// CheckpointConflictError reports the actual progress that rejected a stale
// expected checkpoint.
type CheckpointConflictError struct {
	Expected     eventsourcing.GlobalPosition
	Actual       eventsourcing.GlobalPosition
	ActualExists bool
}

// Error implements error without exposing application data.
func (*CheckpointConflictError) Error() string {
	return ErrCheckpointConflict.Error()
}

// Unwrap exposes the stable checkpoint-conflict category.
func (*CheckpointConflictError) Unwrap() error {
	return ErrCheckpointConflict
}

func validateStoredStatus(status Status, err error) (Status, error) {
	if err != nil {
		return Status{}, err
	}
	if !status.Valid() {
		return Status{}, ErrCheckpointCorrupt
	}

	return status, nil
}

func validProjectionName(name string) bool {
	_, err := eventsourcing.NewStreamID("projection", name)

	return err == nil
}

func invalidControl(reason string) error {
	return fmt.Errorf("%w: projection %s", eventsourcing.ErrInvalidArgument, reason)
}

var _ error = (*CheckpointConflictError)(nil)
