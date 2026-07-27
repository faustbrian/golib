package processmanager_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/processmanager"
)

type managerCommand struct {
	AccountID string
	Action    string
}

func TestNewManagerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	valid := processmanager.Config[managerCommand]{
		Name:   "welcome-flow",
		Replay: processmanager.RejectReplay,
		EventNames: []eventsourcing.EventName{
			managerEventName(t, "account.opened"),
		},
		MaxCommands: 1,
		Planner: func(
			context.Context,
			eventsourcing.Delivery,
		) ([]managerCommand, error) {
			return nil, nil
		},
	}
	tests := map[string]func(*processmanager.Config[managerCommand]){
		"name": func(config *processmanager.Config[managerCommand]) {
			config.Name = ""
		},
		"replay": func(config *processmanager.Config[managerCommand]) {
			config.Replay = 99
		},
		"zero event name": func(config *processmanager.Config[managerCommand]) {
			config.EventNames = []eventsourcing.EventName{{}}
		},
		"duplicate event name": func(config *processmanager.Config[managerCommand]) {
			config.EventNames = append(config.EventNames, config.EventNames[0])
		},
		"too many event names": func(config *processmanager.Config[managerCommand]) {
			config.EventNames = make(
				[]eventsourcing.EventName,
				processmanager.MaxAcceptedEventNames+1,
			)
		},
		"zero limit": func(config *processmanager.Config[managerCommand]) {
			config.MaxCommands = 0
		},
		"large limit": func(config *processmanager.Config[managerCommand]) {
			config.MaxCommands = processmanager.MaxPlannedCommands + 1
		},
		"planner": func(config *processmanager.Config[managerCommand]) {
			config.Planner = nil
		},
	}
	for name, mutate := range tests {
		mutate := mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := valid
			mutate(&config)
			manager, err := processmanager.New(config)
			if manager != nil ||
				!errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("New() = %#v, %v", manager, err)
			}
		})
	}
}

func TestNewManagerRequiresExplicitAcceptedEvents(t *testing.T) {
	t.Parallel()

	manager, err := processmanager.New(processmanager.Config[managerCommand]{
		Name:        "welcome-flow",
		Replay:      processmanager.RejectReplay,
		MaxCommands: 1,
		Planner: func(
			context.Context,
			eventsourcing.Delivery,
		) ([]managerCommand, error) {
			return nil, nil
		},
	})
	if manager != nil ||
		!errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("New() = %#v, %v", manager, err)
	}
}

func TestManagerRejectsInvalidCalls(t *testing.T) {
	t.Parallel()

	manager := newProcessManager(
		t,
		processmanager.RejectReplay,
		1,
		func(
			context.Context,
			eventsourcing.Delivery,
		) ([]managerCommand, error) {
			return nil, nil
		},
	)
	var nilManager *processmanager.Manager[managerCommand]
	var nilContext context.Context
	if planned, err := nilManager.Plan(
		context.Background(),
		managerDelivery(t, eventsourcing.DeliveryLive),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
		len(planned.Commands()) != 0 {
		t.Fatalf("nil Plan() = %#v, %v", planned, err)
	}
	if planned, err := manager.Plan(
		nilContext,
		managerDelivery(t, eventsourcing.DeliveryLive),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
		len(planned.Commands()) != 0 {
		t.Fatalf("Plan(nil) = %#v, %v", planned, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if planned, err := manager.Plan(
		cancelled,
		managerDelivery(t, eventsourcing.DeliveryLive),
	); !errors.Is(err, context.Canceled) ||
		len(planned.Commands()) != 0 {
		t.Fatalf("Plan(cancelled) = %#v, %v", planned, err)
	}
	if planned, err := manager.Plan(
		context.Background(),
		eventsourcing.Delivery{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
		len(planned.Commands()) != 0 {
		t.Fatalf("Plan(zero delivery) = %#v, %v", planned, err)
	}
}

func TestManagerPlansExplicitCommandsForLiveDelivery(t *testing.T) {
	t.Parallel()

	delivery := managerDelivery(t, eventsourcing.DeliveryLive)
	manager := newProcessManager(t, processmanager.RejectReplay, 2, func(
		_ context.Context,
		received eventsourcing.Delivery,
	) ([]managerCommand, error) {
		if !received.Message().Equal(delivery.Message()) {
			t.Fatal("planner received a different message")
		}

		return []managerCommand{
			{AccountID: "account-1", Action: "notify"},
			{AccountID: "account-1", Action: "index"},
		}, nil
	})

	planned, err := manager.Plan(context.Background(), delivery)
	if err != nil {
		t.Fatal(err)
	}
	commands := planned.Commands()
	if planned.MessageID() != delivery.Message().ID() ||
		planned.Mode() != eventsourcing.DeliveryLive ||
		!planned.Accepted() ||
		planned.CommandCount() != 2 ||
		len(commands) != 2 ||
		commands[0].Action != "notify" ||
		commands[1].Action != "index" {
		t.Fatalf("Plan() = %#v", planned)
	}
	commands[0].Action = "mutated"
	if planned.Commands()[0].Action != "notify" {
		t.Fatal("Commands() exposed owned slice state")
	}
}

func TestManagerInvokesPlannerOnlyForAcceptedEventNames(t *testing.T) {
	t.Parallel()

	acceptedName := managerEventName(t, "account.opened")
	otherName := managerEventName(t, "account.closed")
	eventNames := []eventsourcing.EventName{acceptedName}
	calls := 0
	manager, err := processmanager.New(processmanager.Config[managerCommand]{
		Name:        "welcome-flow",
		Replay:      processmanager.RejectReplay,
		EventNames:  eventNames,
		MaxCommands: 1,
		Planner: func(
			context.Context,
			eventsourcing.Delivery,
		) ([]managerCommand, error) {
			calls++

			return []managerCommand{{Action: "notify"}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	eventNames[0] = otherName

	ignoredDelivery := managerDeliveryForEvent(
		t,
		eventsourcing.DeliveryLive,
		"account.closed",
	)
	ignored, err := manager.Plan(context.Background(), ignoredDelivery)
	if err != nil ||
		ignored.MessageID() != ignoredDelivery.Message().ID() ||
		ignored.Mode() != eventsourcing.DeliveryLive ||
		ignored.Accepted() ||
		ignored.CommandCount() != 0 ||
		calls != 0 {
		t.Fatalf("Plan(ignored) = %#v, %v calls=%d", ignored, err, calls)
	}

	accepted, err := manager.Plan(
		context.Background(),
		managerDeliveryForEvent(
			t,
			eventsourcing.DeliveryLive,
			"account.opened",
		),
	)
	if err != nil ||
		!accepted.Accepted() ||
		accepted.CommandCount() != 1 ||
		calls != 1 {
		t.Fatalf("Plan(accepted) = %#v, %v calls=%d", accepted, err, calls)
	}
}

func TestManagerRejectsReplayByDefault(t *testing.T) {
	t.Parallel()

	manager := newProcessManager(t, processmanager.RejectReplay, 1, func(
		context.Context,
		eventsourcing.Delivery,
	) ([]managerCommand, error) {
		t.Fatal("planner ran during rejected replay")

		return nil, nil
	})

	planned, err := manager.Plan(
		context.Background(),
		managerDelivery(t, eventsourcing.DeliveryReplay),
	)
	if !errors.Is(err, processmanager.ErrReplayRejected) ||
		len(planned.Commands()) != 0 {
		t.Fatalf("Plan(replay) = %#v, %v", planned, err)
	}
}

func TestManagerAllowsReplayOnlyWhenExplicitlySelected(t *testing.T) {
	t.Parallel()

	manager := newProcessManager(t, processmanager.AllowReplay, 1, func(
		context.Context,
		eventsourcing.Delivery,
	) ([]managerCommand, error) {
		return []managerCommand{{AccountID: "account-1", Action: "rebuild"}}, nil
	})

	planned, err := manager.Plan(
		context.Background(),
		managerDelivery(t, eventsourcing.DeliveryReplay),
	)
	if err != nil ||
		planned.Mode() != eventsourcing.DeliveryReplay ||
		!planned.Accepted() ||
		len(planned.Commands()) != 1 {
		t.Fatalf("Plan(replay) = %#v, %v", planned, err)
	}
}

func TestManagerSupportsApplicationOwnedDuplicateSuppression(t *testing.T) {
	t.Parallel()

	delivery := managerDelivery(t, eventsourcing.DeliveryLive)
	manager := newProcessManager(t, processmanager.RejectReplay, 1, func(
		context.Context,
		eventsourcing.Delivery,
	) ([]managerCommand, error) {
		return []managerCommand{{AccountID: "account-1", Action: "notify"}}, nil
	})

	processed := make(map[eventsourcing.MessageID]struct{})
	executions := 0
	for range 2 {
		planned, err := manager.Plan(context.Background(), delivery)
		if err != nil {
			t.Fatal(err)
		}
		if _, duplicate := processed[planned.MessageID()]; duplicate {
			continue
		}
		processed[planned.MessageID()] = struct{}{}
		executions += len(planned.Commands())
	}

	if executions != 1 || len(processed) != 1 {
		t.Fatalf(
			"duplicate execution = %d commands, %d message IDs",
			executions,
			len(processed),
		)
	}
}

func TestManagerReportsEmptyAndExcessivePlans(t *testing.T) {
	t.Parallel()

	empty := newProcessManager(t, processmanager.RejectReplay, 1, func(
		context.Context,
		eventsourcing.Delivery,
	) ([]managerCommand, error) {
		return nil, nil
	})
	planned, err := empty.Plan(
		context.Background(),
		managerDelivery(t, eventsourcing.DeliveryLive),
	)
	if err != nil || len(planned.Commands()) != 0 {
		t.Fatalf("empty Plan() = %#v, %v", planned, err)
	}
	if !planned.Accepted() {
		t.Fatal("empty accepted plan was reported as ignored")
	}

	excessive := newProcessManager(t, processmanager.RejectReplay, 1, func(
		context.Context,
		eventsourcing.Delivery,
	) ([]managerCommand, error) {
		return []managerCommand{{Action: "one"}, {Action: "two"}}, nil
	})
	planned, err = excessive.Plan(
		context.Background(),
		managerDelivery(t, eventsourcing.DeliveryLive),
	)
	if !errors.Is(err, processmanager.ErrCommandLimit) ||
		len(planned.Commands()) != 0 {
		t.Fatalf("excessive Plan() = %#v, %v", planned, err)
	}
}

func TestManagerRedactsPlannerFailureAndPanic(t *testing.T) {
	t.Parallel()

	secretFailure := errors.New("secret planner state")
	tests := map[string]struct {
		planner processmanager.Planner[managerCommand]
		want    error
	}{
		"error": {
			planner: func(
				context.Context,
				eventsourcing.Delivery,
			) ([]managerCommand, error) {
				return nil, secretFailure
			},
			want: secretFailure,
		},
		"panic": {
			planner: func(
				context.Context,
				eventsourcing.Delivery,
			) ([]managerCommand, error) {
				panic("secret panic state")
			},
			want: processmanager.ErrPlannerPanic,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			manager := newProcessManager(
				t,
				processmanager.RejectReplay,
				1,
				testCase.planner,
			)
			planned, err := manager.Plan(
				context.Background(),
				managerDelivery(t, eventsourcing.DeliveryLive),
			)
			var plannerError *processmanager.PlannerError
			if !errors.Is(err, testCase.want) ||
				!errors.As(err, &plannerError) ||
				strings.Contains(err.Error(), "secret") ||
				len(planned.Commands()) != 0 {
				t.Fatalf("Plan() = %#v, %v", planned, err)
			}
		})
	}
}

func newProcessManager(
	t *testing.T,
	replay processmanager.ReplayPolicy,
	maxCommands uint32,
	planner processmanager.Planner[managerCommand],
) *processmanager.Manager[managerCommand] {
	t.Helper()

	manager, err := processmanager.New(processmanager.Config[managerCommand]{
		Name:   "welcome-flow",
		Replay: replay,
		EventNames: []eventsourcing.EventName{
			managerEventName(t, "account.opened"),
		},
		MaxCommands: maxCommands,
		Planner:     planner,
	})
	if err != nil {
		t.Fatal(err)
	}

	return manager
}

func managerDelivery(
	t *testing.T,
	mode eventsourcing.DeliveryMode,
) eventsourcing.Delivery {
	t.Helper()

	return managerDeliveryForEvent(t, mode, "account.opened")
}

func managerDeliveryForEvent(
	t *testing.T,
	mode eventsourcing.DeliveryMode,
	eventName string,
) eventsourcing.Delivery {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-1")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        eventName,
		Version:     1,
		ContentType: "application/json",
		Payload:     []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "message-1",
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Date(2026, time.July, 25, 15, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  1,
		GlobalPosition: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := eventsourcing.NewDelivery(message, mode)
	if err != nil {
		t.Fatal(err)
	}

	return delivery
}

func managerEventName(t *testing.T, value string) eventsourcing.EventName {
	t.Helper()

	name, err := eventsourcing.NewEventName(value)
	if err != nil {
		t.Fatal(err)
	}

	return name
}
