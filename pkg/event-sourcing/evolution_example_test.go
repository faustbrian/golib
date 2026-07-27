package eventsourcing_test

import (
	"encoding/json"
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

type ownerRenamed struct {
	Name string `json:"name"`
}

func Example_eventSchemaEvolution() {
	stored, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.renamed",
			Version:     1,
			ContentType: eventsourcing.JSONContentType,
			Payload:     []byte(`{"name":"Ada"}`),
		},
	)
	if err != nil {
		panic(err)
	}
	rule, err := eventsourcing.NewUpcastRule(
		"account.renamed",
		1,
		func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			var legacy ownerRenamed
			if err := json.Unmarshal(input.Event().Payload(), &legacy); err != nil {
				return nil, err
			}
			payload, err := json.Marshal(ownerRenamed{Name: legacy.Name})
			if err != nil {
				return nil, err
			}
			current, err := eventsourcing.NewEncodedEvent(
				eventsourcing.EncodedEventInput{
					Name:        "account.owner-renamed",
					Version:     2,
					ContentType: eventsourcing.JSONContentType,
					Payload:     payload,
				},
			)
			if err != nil {
				return nil, err
			}
			upcast, err := eventsourcing.NewUpcastEvent(
				current,
				input.Metadata(),
			)
			if err != nil {
				return nil, err
			}

			return []eventsourcing.UpcastEvent{upcast}, nil
		},
	)
	if err != nil {
		panic(err)
	}
	chain, err := eventsourcing.NewUpcasterChain(rule)
	if err != nil {
		panic(err)
	}
	input, err := eventsourcing.NewUpcastEvent(
		stored,
		map[string]string{"source": "legacy"},
	)
	if err != nil {
		panic(err)
	}
	upcast, err := chain.Upcast(input)
	if err != nil {
		panic(err)
	}
	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[ownerRenamed](
			"account.owner-renamed",
			2,
		),
	)
	if err != nil {
		panic(err)
	}
	decoded, err := codec.Decode(upcast[0].Event())
	if err != nil {
		panic(err)
	}
	event := decoded.Value().(ownerRenamed)

	fmt.Printf(
		"%s@%d name=%s source=%s\n",
		decoded.Name().String(),
		decoded.Version(),
		event.Name,
		upcast[0].Metadata()["source"],
	)
	// Output: account.owner-renamed@2 name=Ada source=legacy
}
