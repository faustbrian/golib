package eventtest_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/eventtest"
	"github.com/faustbrian/golib/pkg/event-sourcing/processmanager"
)

type scenarioCommand struct {
	accountID string
	action    string
}

type processManagerFunc[Command any] func(
	context.Context,
	eventsourcing.Delivery,
) (processmanager.PlanResult[Command], error)

func (function processManagerFunc[Command]) Plan(
	ctx context.Context,
	delivery eventsourcing.Delivery,
) (processmanager.PlanResult[Command], error) {
	return function(ctx, delivery)
}

func TestCheckProcessManagerScenarioMatchesPlanAndExpectedFailure(
	t *testing.T,
) {
	t.Parallel()

	delivery := processManagerDelivery(t, eventsourcing.DeliveryLive)
	manager, err := processmanager.New(processmanager.Config[scenarioCommand]{
		Name:        "welcome-flow",
		Replay:      processmanager.RejectReplay,
		MaxCommands: 2,
		Planner: func(
			context.Context,
			eventsourcing.Delivery,
		) ([]scenarioCommand, error) {
			return []scenarioCommand{
				{accountID: "account-1", action: "notify"},
				{accountID: "account-1", action: "index"},
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = eventtest.CheckProcessManagerScenario(
		context.Background(),
		eventtest.ProcessManagerScenario[scenarioCommand]{
			Manager:  manager,
			Delivery: delivery,
			Commands: []scenarioCommand{
				{accountID: "account-1", action: "notify"},
				{accountID: "account-1", action: "index"},
			},
			Equal: func(left, right scenarioCommand) bool {
				return left == right
			},
		},
	)
	if err != nil {
		t.Fatalf("CheckProcessManagerScenario(success) error = %v", err)
	}

	replay := processManagerDelivery(t, eventsourcing.DeliveryReplay)
	err = eventtest.CheckProcessManagerScenario(
		context.Background(),
		eventtest.ProcessManagerScenario[scenarioCommand]{
			Manager:   manager,
			Delivery:  replay,
			WantError: processmanager.ErrReplayRejected,
		},
	)
	if err != nil {
		t.Fatalf("CheckProcessManagerScenario(error) error = %v", err)
	}
}

func TestCheckProcessManagerScenarioRejectsInvalidConfiguration(
	t *testing.T,
) {
	t.Parallel()

	delivery := processManagerDelivery(t, eventsourcing.DeliveryLive)
	manager, err := processmanager.New(processmanager.Config[scenarioCommand]{
		Name:        "validation-flow",
		MaxCommands: 1,
		Planner: func(
			context.Context,
			eventsourcing.Delivery,
		) ([]scenarioCommand, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	valid := eventtest.ProcessManagerScenario[scenarioCommand]{
		Manager:  manager,
		Delivery: delivery,
	}
	var nilContext context.Context
	tests := map[string]struct {
		ctx      context.Context
		scenario eventtest.ProcessManagerScenario[scenarioCommand]
	}{
		"nil context": {
			ctx:      nilContext,
			scenario: valid,
		},
		"nil manager": {
			ctx: context.Background(),
			scenario: eventtest.ProcessManagerScenario[scenarioCommand]{
				Delivery: delivery,
			},
		},
		"zero delivery": {
			ctx: context.Background(),
			scenario: eventtest.ProcessManagerScenario[scenarioCommand]{
				Manager: manager,
			},
		},
		"commands without equality": {
			ctx: context.Background(),
			scenario: eventtest.ProcessManagerScenario[scenarioCommand]{
				Manager:  manager,
				Delivery: delivery,
				Commands: []scenarioCommand{{action: "notify"}},
			},
		},
		"error with commands": {
			ctx: context.Background(),
			scenario: eventtest.ProcessManagerScenario[scenarioCommand]{
				Manager:   manager,
				Delivery:  delivery,
				Commands:  []scenarioCommand{{action: "notify"}},
				WantError: errors.New("expected"),
			},
		},
		"error with equality": {
			ctx: context.Background(),
			scenario: eventtest.ProcessManagerScenario[scenarioCommand]{
				Manager:   manager,
				Delivery:  delivery,
				Equal:     func(scenarioCommand, scenarioCommand) bool { return true },
				WantError: errors.New("expected"),
			},
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := eventtest.CheckProcessManagerScenario(
				testCase.ctx,
				testCase.scenario,
			)
			if !errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("CheckProcessManagerScenario() error = %v", err)
			}
		})
	}
}

func TestCheckProcessManagerScenarioReportsRedactedMismatches(
	t *testing.T,
) {
	t.Parallel()

	delivery := processManagerDelivery(t, eventsourcing.DeliveryLive)
	secretFailure := errors.New("secret planner failure")
	success, err := processmanager.New(processmanager.Config[scenarioCommand]{
		Name:        "mismatch-flow",
		MaxCommands: 1,
		Planner: func(
			context.Context,
			eventsourcing.Delivery,
		) ([]scenarioCommand, error) {
			return []scenarioCommand{{action: "secret-command"}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	failure := processManagerFunc[scenarioCommand](func(
		context.Context,
		eventsourcing.Delivery,
	) (processmanager.PlanResult[scenarioCommand], error) {
		return processmanager.PlanResult[scenarioCommand]{}, secretFailure
	})
	otherDelivery := processManagerDeliveryWithID(
		t,
		eventsourcing.DeliveryLive,
		"process-manager-message-2",
	)
	otherResult, err := success.Plan(context.Background(), otherDelivery)
	if err != nil {
		t.Fatal(err)
	}
	wrongIdentity := processManagerFunc[scenarioCommand](func(
		context.Context,
		eventsourcing.Delivery,
	) (processmanager.PlanResult[scenarioCommand], error) {
		return otherResult, nil
	})
	replayManager, err := processmanager.New(
		processmanager.Config[scenarioCommand]{
			Name:        "replay-mismatch-flow",
			Replay:      processmanager.AllowReplay,
			MaxCommands: 1,
			Planner: func(
				context.Context,
				eventsourcing.Delivery,
			) ([]scenarioCommand, error) {
				return []scenarioCommand{{action: "secret-command"}}, nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	replayResult, err := replayManager.Plan(
		context.Background(),
		processManagerDelivery(t, eventsourcing.DeliveryReplay),
	)
	if err != nil {
		t.Fatal(err)
	}
	wrongMode := processManagerFunc[scenarioCommand](func(
		context.Context,
		eventsourcing.Delivery,
	) (processmanager.PlanResult[scenarioCommand], error) {
		return replayResult, nil
	})
	partialFailure := processManagerFunc[scenarioCommand](func(
		context.Context,
		eventsourcing.Delivery,
	) (processmanager.PlanResult[scenarioCommand], error) {
		return otherResult, secretFailure
	})
	tests := map[string]eventtest.ProcessManagerScenario[scenarioCommand]{
		"length": {
			Manager:  success,
			Delivery: delivery,
		},
		"command": {
			Manager:  success,
			Delivery: delivery,
			Commands: []scenarioCommand{{action: "different-secret"}},
			Equal:    func(left, right scenarioCommand) bool { return left == right },
		},
		"message identity": {
			Manager:  wrongIdentity,
			Delivery: delivery,
			Commands: []scenarioCommand{{action: "secret-command"}},
			Equal:    func(left, right scenarioCommand) bool { return left == right },
		},
		"delivery mode": {
			Manager:  wrongMode,
			Delivery: delivery,
			Commands: []scenarioCommand{{action: "secret-command"}},
			Equal:    func(left, right scenarioCommand) bool { return left == right },
		},
		"unexpected failure": {
			Manager:  failure,
			Delivery: delivery,
		},
		"wrong failure": {
			Manager:   failure,
			Delivery:  delivery,
			WantError: context.Canceled,
		},
		"missing failure": {
			Manager:   success,
			Delivery:  delivery,
			WantError: secretFailure,
		},
		"partial failure": {
			Manager:   partialFailure,
			Delivery:  delivery,
			WantError: secretFailure,
		},
	}
	for name, scenario := range tests {
		scenario := scenario
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := eventtest.CheckProcessManagerScenario(
				context.Background(),
				scenario,
			)
			if name == "unexpected failure" {
				if !errors.Is(err, secretFailure) {
					t.Fatalf("CheckProcessManagerScenario() error = %v", err)
				}
				return
			}
			if !errors.Is(err, eventtest.ErrConformance) ||
				strings.Contains(err.Error(), "secret") {
				t.Fatalf("CheckProcessManagerScenario() error = %v", err)
			}
		})
	}
}

func processManagerDelivery(
	t testing.TB,
	mode eventsourcing.DeliveryMode,
) eventsourcing.Delivery {
	t.Helper()

	return processManagerDeliveryWithID(
		t,
		mode,
		"process-manager-message-1",
	)
}

func processManagerDeliveryWithID(
	t testing.TB,
	mode eventsourcing.DeliveryMode,
	messageID string,
) eventsourcing.Delivery {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-1")
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.opened",
		Version:     1,
		ContentType: "application/json",
		Payload:     []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         messageID,
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC),
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
