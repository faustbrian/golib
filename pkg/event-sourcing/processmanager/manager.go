// Package processmanager plans explicit application commands from event
// deliveries without executing side effects.
package processmanager

import (
	"context"
	"errors"
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

const (
	// MaxAcceptedEventNames bounds one process manager's stable event-name
	// allowlist.
	MaxAcceptedEventNames = 1_000
	// MaxPlannedCommands bounds one process-manager reaction.
	MaxPlannedCommands = 1_000
)

var (
	// ErrReplayRejected reports replay delivered to a live-only manager.
	ErrReplayRejected = errors.New("process manager rejects replay")
	// ErrPlannerPanic reports a contained application planner panic.
	ErrPlannerPanic = errors.New("process manager planner panicked")
	// ErrCommandLimit reports a plan larger than its configured bound.
	ErrCommandLimit = errors.New("process manager command limit exceeded")
)

// ReplayPolicy controls whether historical delivery may invoke a planner.
type ReplayPolicy uint8

const (
	// RejectReplay is the safe zero-value policy.
	RejectReplay ReplayPolicy = iota
	// AllowReplay explicitly permits planning from historical delivery.
	AllowReplay
)

// Planner derives application-owned immutable commands without executing them.
type Planner[Command any] func(
	context.Context,
	eventsourcing.Delivery,
) ([]Command, error)

// Config supplies one process manager's explicit planning policy.
type Config[Command any] struct {
	Name        string
	Replay      ReplayPolicy
	EventNames  []eventsourcing.EventName
	MaxCommands uint32
	Planner     Planner[Command]
}

// Manager plans bounded commands and never executes them.
//
// Manager is immutable after construction. It starts no goroutines, stores no
// process state, and owns no retries or transactions.
type Manager[Command any] struct {
	planner     Planner[Command]
	replay      ReplayPolicy
	eventNames  map[eventsourcing.EventName]struct{}
	maxCommands uint32
}

// New validates one explicit process-manager composition.
func New[Command any](config Config[Command]) (*Manager[Command], error) {
	if _, err := eventsourcing.NewStreamID(
		"process-manager",
		config.Name,
	); err != nil {
		return nil, invalid("name must be canonical")
	}
	if config.Replay != RejectReplay && config.Replay != AllowReplay {
		return nil, invalid("replay policy is unknown")
	}
	if len(config.EventNames) == 0 ||
		len(config.EventNames) > MaxAcceptedEventNames {
		return nil, invalid("accepted event names must be bounded")
	}
	eventNames := make(
		map[eventsourcing.EventName]struct{},
		len(config.EventNames),
	)
	for _, eventName := range config.EventNames {
		if eventName.IsZero() {
			return nil, invalid("accepted event name must be assigned")
		}
		if _, duplicate := eventNames[eventName]; duplicate {
			return nil, invalid("accepted event names must be unique")
		}
		eventNames[eventName] = struct{}{}
	}
	if config.MaxCommands == 0 ||
		config.MaxCommands > MaxPlannedCommands {
		return nil, invalid("command limit must be bounded")
	}
	if config.Planner == nil {
		return nil, invalid("planner must be assigned")
	}

	return &Manager[Command]{
		planner:     config.Planner,
		replay:      config.Replay,
		eventNames:  eventNames,
		maxCommands: config.MaxCommands,
	}, nil
}

// PlanResult is one immutable-by-contract explicit reaction.
//
// Command values are application-owned and must themselves be immutable.
type PlanResult[Command any] struct {
	messageID eventsourcing.MessageID
	mode      eventsourcing.DeliveryMode
	accepted  bool
	commands  []Command
}

// MessageID returns the triggering event message identifier.
func (result PlanResult[Command]) MessageID() eventsourcing.MessageID {
	return result.messageID
}

// Mode returns whether the triggering delivery was live or replay.
func (result PlanResult[Command]) Mode() eventsourcing.DeliveryMode {
	return result.mode
}

// Accepted reports whether the triggering event name matched the manager's
// explicit allowlist and invoked its planner.
func (result PlanResult[Command]) Accepted() bool {
	return result.accepted
}

// CommandCount returns the number of planned commands without copying them.
func (result PlanResult[Command]) CommandCount() int {
	return len(result.commands)
}

// Commands returns a defensive copy of the ordered planned commands.
func (result PlanResult[Command]) Commands() []Command {
	return append([]Command(nil), result.commands...)
}

// Plan derives bounded commands for one delivery without executing them.
func (manager *Manager[Command]) Plan(
	ctx context.Context,
	delivery eventsourcing.Delivery,
) (PlanResult[Command], error) {
	if ctx == nil || manager == nil {
		return PlanResult[Command]{}, eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return PlanResult[Command]{}, err
	}
	message := delivery.Message()
	if message.ID().IsZero() ||
		(delivery.Mode() != eventsourcing.DeliveryLive &&
			delivery.Mode() != eventsourcing.DeliveryReplay) {
		return PlanResult[Command]{}, eventsourcing.ErrInvalidArgument
	}
	if delivery.Mode() == eventsourcing.DeliveryReplay &&
		manager.replay != AllowReplay {
		return PlanResult[Command]{}, ErrReplayRejected
	}
	if _, accepted := manager.eventNames[message.Event().Name()]; !accepted {
		return PlanResult[Command]{
			messageID: message.ID(),
			mode:      delivery.Mode(),
		}, nil
	}
	commands, err := callPlanner(ctx, manager.planner, delivery)
	if err != nil {
		return PlanResult[Command]{}, &PlannerError{Cause: err}
	}
	if len(commands) > int(manager.maxCommands) {
		return PlanResult[Command]{}, ErrCommandLimit
	}

	return PlanResult[Command]{
		messageID: message.ID(),
		mode:      delivery.Mode(),
		accepted:  true,
		commands:  commands,
	}, nil
}

// PlannerError redacts application planner diagnostics while preserving their
// cause for inspection.
type PlannerError struct {
	Cause error
}

// Error implements error without disclosing event or process state.
func (*PlannerError) Error() string {
	return "process manager planning failed"
}

// Unwrap preserves the planner cause for errors.Is and errors.As.
func (err *PlannerError) Unwrap() error {
	return err.Cause
}

func callPlanner[Command any](
	ctx context.Context,
	planner Planner[Command],
	delivery eventsourcing.Delivery,
) (commands []Command, err error) {
	defer func() {
		if recover() != nil {
			commands = nil
			err = ErrPlannerPanic
		}
	}()

	commands, err = planner(ctx, delivery)
	if err != nil {
		return nil, err
	}

	return append([]Command(nil), commands...), nil
}

func invalid(reason string) error {
	return fmt.Errorf("%w: %s", eventsourcing.ErrInvalidArgument, reason)
}

var _ error = (*PlannerError)(nil)
