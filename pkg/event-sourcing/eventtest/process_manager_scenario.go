package eventtest

import (
	"context"
	"errors"
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/processmanager"
)

// ProcessManager plans commands for one persisted event delivery.
type ProcessManager[Command any] interface {
	Plan(
		context.Context,
		eventsourcing.Delivery,
	) (processmanager.PlanResult[Command], error)
}

// ProcessManagerScenario describes one expected planning outcome. Commands are
// compared in order. WantError selects an errors.Is failure expectation.
type ProcessManagerScenario[Command any] struct {
	Manager   ProcessManager[Command]
	Delivery  eventsourcing.Delivery
	Commands  []Command
	Equal     func(Command, Command) bool
	Ignored   bool
	WantError error
}

// CheckProcessManagerScenario executes one process-manager plan and compares
// its public result without formatting commands, event data, or error values.
func CheckProcessManagerScenario[Command any](
	ctx context.Context,
	scenario ProcessManagerScenario[Command],
) error {
	if ctx == nil || scenario.Manager == nil || scenario.Delivery.IsZero() {
		return eventsourcing.ErrInvalidArgument
	}
	if scenario.WantError != nil {
		if len(scenario.Commands) != 0 ||
			scenario.Equal != nil ||
			scenario.Ignored {
			return eventsourcing.ErrInvalidArgument
		}
	} else if scenario.Ignored &&
		(len(scenario.Commands) != 0 || scenario.Equal != nil) {
		return eventsourcing.ErrInvalidArgument
	} else if len(scenario.Commands) != 0 && scenario.Equal == nil {
		return eventsourcing.ErrInvalidArgument
	}

	result, err := scenario.Manager.Plan(ctx, scenario.Delivery)
	if scenario.WantError != nil {
		return checkProcessManagerFailure(result, err, scenario.WantError)
	}
	if err != nil {
		return err
	}
	if result.MessageID() != scenario.Delivery.Message().ID() {
		return fmt.Errorf(
			"%w: process-manager message identity differs",
			ErrConformance,
		)
	}
	if result.Mode() != scenario.Delivery.Mode() {
		return fmt.Errorf(
			"%w: process-manager delivery mode differs",
			ErrConformance,
		)
	}
	if result.Accepted() == scenario.Ignored {
		return fmt.Errorf(
			"%w: process-manager event acceptance differs",
			ErrConformance,
		)
	}
	commands := result.Commands()
	if len(commands) != len(scenario.Commands) {
		return fmt.Errorf(
			"%w: process-manager command count differs",
			ErrConformance,
		)
	}
	for index := range commands {
		if !scenario.Equal(commands[index], scenario.Commands[index]) {
			return fmt.Errorf(
				"%w: process-manager command %d differs",
				ErrConformance,
				index,
			)
		}
	}

	return nil
}

func checkProcessManagerFailure[Command any](
	result processmanager.PlanResult[Command],
	err error,
	want error,
) error {
	if !errors.Is(err, want) {
		return fmt.Errorf(
			"%w: process-manager error category differs",
			ErrConformance,
		)
	}
	if !result.MessageID().IsZero() ||
		result.Mode() != 0 ||
		result.Accepted() ||
		len(result.Commands()) != 0 {
		return fmt.Errorf(
			"%w: failed process-manager plan returned partial output",
			ErrConformance,
		)
	}

	return nil
}

var _ ProcessManager[struct{}] = (*processmanager.Manager[struct{}])(nil)
