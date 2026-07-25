// Package projection provides explicit bounded replay composition for read
// models without requiring CQRS.
package projection

import (
	"context"
	"errors"
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

var (
	// ErrCheckpointNotFound reports a projection without durable progress.
	ErrCheckpointNotFound = errors.New("projection checkpoint not found")
	// ErrCheckpointConflict reports concurrently advanced projection progress.
	ErrCheckpointConflict = errors.New("projection checkpoint conflict")
	// ErrCheckpointCorrupt reports an invalid durable checkpoint value.
	ErrCheckpointCorrupt = errors.New("projection checkpoint is corrupt")
	// ErrHandlerPanic reports a contained projection handler panic.
	ErrHandlerPanic = errors.New("projection handler panicked")
)

// CheckpointStore persists monotonic projection progress.
//
// Save atomically replaces expected with next. Expected zero creates the first
// checkpoint. Status returns one atomic run-state and optional checkpoint
// snapshot. Implementations must reject stale expectations with
// ErrCheckpointConflict and checkpoint advancement while paused with
// ErrProjectionPaused.
type CheckpointStore interface {
	Status(context.Context, string) (Status, error)
	Save(
		context.Context,
		string,
		eventsourcing.GlobalPosition,
		eventsourcing.GlobalPosition,
	) error
}

// Handler applies one explicit replay delivery to application-owned projection
// state. Handlers must be idempotent because a successful handler followed by
// checkpoint failure is retried.
type Handler func(context.Context, eventsourcing.Delivery) error

// RunnerConfig supplies every projection runner boundary.
type RunnerConfig struct {
	Name         string
	Reader       eventsourcing.GlobalReader
	Checkpoints  CheckpointStore
	Handler      Handler
	Filter       *ReplayFilter
	PoisonPolicy PoisonPolicy
	BatchSize    uint32
}

// Runner processes one bounded replay batch at a time.
//
// Runner starts no goroutines and owns no transactions. A Runner may be shared
// only when its reader, checkpoint store, and handler support that use.
type Runner struct {
	name         string
	reader       eventsourcing.GlobalReader
	checkpoints  CheckpointStore
	handler      Handler
	filter       ReplayFilter
	hasFilter    bool
	poisonPolicy PoisonPolicy
	batchSize    uint32
}

// NewRunner validates and owns projection composition.
func NewRunner(config RunnerConfig) (*Runner, error) {
	if !validProjectionName(config.Name) {
		return nil, fmt.Errorf(
			"%w: projection name must be canonical",
			eventsourcing.ErrInvalidArgument,
		)
	}
	if config.Reader == nil ||
		config.Checkpoints == nil ||
		config.Handler == nil {
		return nil, fmt.Errorf(
			"%w: projection dependencies must be assigned",
			eventsourcing.ErrInvalidArgument,
		)
	}
	if config.BatchSize == 0 ||
		config.BatchSize > eventsourcing.MaxReadMessages {
		return nil, fmt.Errorf(
			"%w: projection batch size must be bounded",
			eventsourcing.ErrInvalidArgument,
		)
	}
	if config.Filter != nil && !config.Filter.Valid() {
		return nil, fmt.Errorf(
			"%w: projection replay filter must be valid",
			eventsourcing.ErrInvalidArgument,
		)
	}

	runner := &Runner{
		name:         config.Name,
		reader:       config.Reader,
		checkpoints:  config.Checkpoints,
		handler:      config.Handler,
		poisonPolicy: config.PoisonPolicy,
		batchSize:    config.BatchSize,
	}
	if config.Filter != nil {
		runner.filter = *config.Filter
		runner.hasFilter = true
	}

	return runner, nil
}

// BatchResult describes handled application calls and durably checkpointed
// progress separately.
type BatchResult struct {
	scanned      uint32
	handled      uint32
	filtered     uint32
	skipped      uint32
	checkpointed uint32
	checkpoint   eventsourcing.GlobalPosition
}

// Scanned returns messages examined from the bounded global range.
func (result BatchResult) Scanned() uint32 {
	return result.scanned
}

// Handled returns successful handler calls, including a call whose checkpoint
// subsequently failed.
func (result BatchResult) Handled() uint32 {
	return result.handled
}

// Filtered returns scanned messages deliberately omitted from the handler.
func (result BatchResult) Filtered() uint32 {
	return result.filtered
}

// Skipped returns handler failures explicitly and durably skipped by the
// configured poison policy.
func (result BatchResult) Skipped() uint32 {
	return result.skipped
}

// Checkpointed returns scanned messages followed by successful checkpoint
// saves. This includes filtered messages.
func (result BatchResult) Checkpointed() uint32 {
	return result.checkpointed
}

// Checkpoint returns the last durable global position.
func (result BatchResult) Checkpoint() eventsourcing.GlobalPosition {
	return result.checkpoint
}

// RunBatch resumes after durable progress and handles at most the configured
// batch size.
func (runner *Runner) RunBatch(
	ctx context.Context,
) (result BatchResult, err error) {
	if ctx == nil || runner == nil {
		return BatchResult{}, eventsourcing.ErrInvalidArgument
	}
	status, err := runner.checkpoints.Status(ctx, runner.name)
	if err != nil {
		return BatchResult{}, err
	}
	if !status.Valid() {
		return BatchResult{}, ErrCheckpointCorrupt
	}
	checkpoint, hasCheckpoint := status.Checkpoint()
	result.checkpoint = checkpoint
	if status.State() == StatePaused {
		return result, ErrProjectionPaused
	}
	if !hasCheckpoint {
		checkpoint = 0
	}
	if checkpoint == eventsourcing.GlobalPosition(^uint64(0)) {
		return result, nil
	}
	options, err := eventsourcing.NewReadGlobalOptions(
		eventsourcing.ReadGlobalOptionsInput{
			FromPosition: checkpoint + 1,
			Limit:        runner.batchSize,
		},
	)
	if err != nil {
		return result, err
	}
	iterator, err := runner.reader.ReadGlobal(ctx, options)
	if err != nil {
		return result, err
	}
	if iterator == nil {
		return result, fmt.Errorf(
			"%w: global reader returned a nil iterator",
			eventsourcing.ErrInvalidArgument,
		)
	}
	defer func() {
		err = errors.Join(err, iterator.Err(), iterator.Close())
	}()

	for iterator.Next(ctx) {
		if result.scanned >= runner.batchSize {
			return result, eventsourcing.ErrCorruptHistory
		}
		message := iterator.Message()
		position, ok := message.GlobalPosition()
		if !ok || position != result.checkpoint+1 {
			return result, eventsourcing.ErrCorruptHistory
		}
		result.scanned++
		skipped := false
		var poisonFailure error
		if runner.hasFilter && !runner.filter.Match(message) {
			result.filtered++
		} else {
			// A Message with a global position is a validated persisted message,
			// and DeliveryReplay is a valid mode.
			delivery, _ := eventsourcing.NewDelivery(
				message,
				eventsourcing.DeliveryReplay,
			)
			if handlerErr := callHandler(
				ctx,
				runner.handler,
				delivery,
			); handlerErr != nil {
				handlerFailure := &HandlerError{Cause: handlerErr}
				if runner.poisonPolicy == nil {
					return result, handlerFailure
				}
				if cancellation := ctx.Err(); cancellation != nil {
					return result, errors.Join(
						handlerFailure,
						cancellation,
					)
				}
				decision, policyErr := callPoisonPolicy(
					ctx,
					runner.poisonPolicy,
					newPoisonedDelivery(delivery, handlerErr),
				)
				if policyErr != nil {
					return result, &PoisonPolicyError{
						Handler: handlerFailure,
						Policy:  policyErr,
					}
				}
				switch decision {
				case StopOnPoison:
					return result, handlerFailure
				case SkipPoison:
					skipped = true
					poisonFailure = handlerFailure
				default:
					return result, &PoisonPolicyError{
						Handler: handlerFailure,
						Policy:  ErrPoisonDecision,
					}
				}
			} else {
				result.handled++
			}
		}
		if saveErr := runner.checkpoints.Save(
			ctx,
			runner.name,
			result.checkpoint,
			position,
		); saveErr != nil {
			if skipped {
				return result, &PoisonSkipCheckpointError{
					Handler:    poisonFailure,
					Checkpoint: saveErr,
				}
			}

			return result, saveErr
		}
		result.checkpoint = position
		result.checkpointed++
		if skipped {
			result.skipped++
		}
	}

	return result, nil
}

// HandlerError redacts application projection diagnostics while preserving
// their cause.
type HandlerError struct {
	Cause error
}

// Error implements error without exposing projection state.
func (*HandlerError) Error() string {
	return "projection handler failed"
}

// Unwrap preserves the handler cause for errors.Is and errors.As.
func (err *HandlerError) Unwrap() error {
	return err.Cause
}

func callHandler(
	ctx context.Context,
	handler Handler,
	delivery eventsourcing.Delivery,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrHandlerPanic
		}
	}()

	return handler(ctx, delivery)
}

var _ error = (*HandlerError)(nil)
