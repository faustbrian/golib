package eventsourcing_test

import (
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/eventtest"
)

func Example_scenarioTesting() {
	scenario, err := eventtest.NewScenario(
		eventtest.AggregateConfig[*quickstartAccount]{
			New: func() (*quickstartAccount, error) {
				return &quickstartAccount{id: "account-42"}, nil
			},
			Lifecycle: func(account *quickstartAccount) *eventsourcing.Lifecycle {
				return &account.lifecycle
			},
			Apply: func(
				account *quickstartAccount,
				event eventsourcing.DecodedEvent,
			) error {
				return account.apply(event)
			},
		},
	)
	if err != nil {
		panic(err)
	}
	result := scenario.GivenNone().When(
		func(account *quickstartAccount) error {
			return account.open("Ada")
		},
	)
	if err := result.Error(); err != nil {
		panic(err)
	}

	fmt.Printf(
		"events=%d committed=%d current=%d\n",
		len(result.Events()),
		result.CommittedVersion(),
		result.Version(),
	)
	// Output: events=1 committed=0 current=1
}
