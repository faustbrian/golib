package eventtest

import (
	"context"
	"errors"
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
)

// ProjectionRunner executes one bounded projection batch.
type ProjectionRunner interface {
	RunBatch(context.Context) (projection.BatchResult, error)
}

// ExpectedProjectionBatch describes the observable progress from one runner
// batch.
type ExpectedProjectionBatch struct {
	Scanned      uint32
	Handled      uint32
	Filtered     uint32
	Skipped      uint32
	Checkpointed uint32
	Checkpoint   eventsourcing.GlobalPosition
}

// ProjectionScenario describes one bounded runner call and optional
// application-owned read-model predicate. WantError uses errors.Is semantics.
type ProjectionScenario struct {
	Runner    ProjectionRunner
	Expected  ExpectedProjectionBatch
	WantError error
	State     func() bool
}

// CheckProjectionScenario executes one bounded projection batch and compares
// durable progress plus application state without formatting event or model
// data.
func CheckProjectionScenario(
	ctx context.Context,
	scenario ProjectionScenario,
) error {
	if ctx == nil || scenario.Runner == nil {
		return eventsourcing.ErrInvalidArgument
	}

	result, err := scenario.Runner.RunBatch(ctx)
	if scenario.WantError == nil {
		if err != nil {
			return err
		}
	} else if !errors.Is(err, scenario.WantError) {
		return fmt.Errorf(
			"%w: projection error category differs",
			ErrConformance,
		)
	}
	if result.Scanned() != scenario.Expected.Scanned {
		return projectionMismatch("scanned count")
	}
	if result.Handled() != scenario.Expected.Handled {
		return projectionMismatch("handled count")
	}
	if result.Filtered() != scenario.Expected.Filtered {
		return projectionMismatch("filtered count")
	}
	if result.Skipped() != scenario.Expected.Skipped {
		return projectionMismatch("skipped count")
	}
	if result.Checkpointed() != scenario.Expected.Checkpointed {
		return projectionMismatch("checkpointed count")
	}
	if result.Checkpoint() != scenario.Expected.Checkpoint {
		return projectionMismatch("checkpoint")
	}
	if scenario.State != nil && !scenario.State() {
		return projectionMismatch("application state")
	}

	return nil
}

func projectionMismatch(field string) error {
	return fmt.Errorf("%w: projection %s differs", ErrConformance, field)
}

var _ ProjectionRunner = (*projection.Runner)(nil)
