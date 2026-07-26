package eventsourcing_test

import (
	"bytes"
	"maps"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func FuzzUpcasterChain(fuzz *testing.F) {
	fuzz.Add("legacy.event", uint32(1), []byte(`{"value":1}`), "source", "legacy")
	fuzz.Add("current.event", uint32(2), []byte{}, "", "")

	rule, err := eventsourcing.NewUpcastRule(
		"legacy.event",
		1,
		func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			metadata := input.Metadata()
			metadata["migrated"] = "true"
			encoded, encodeErr := eventsourcing.NewEncodedEvent(
				eventsourcing.EncodedEventInput{
					Name:        "current.event",
					Version:     2,
					ContentType: eventsourcing.JSONContentType,
					Payload:     input.Event().Payload(),
				},
			)
			if encodeErr != nil {
				return nil, encodeErr
			}
			output, outputErr := eventsourcing.NewUpcastEvent(encoded, metadata)
			if outputErr != nil {
				return nil, outputErr
			}

			return []eventsourcing.UpcastEvent{output}, nil
		},
	)
	if err != nil {
		fuzz.Fatal(err)
	}
	chain, err := eventsourcing.NewUpcasterChain(rule)
	if err != nil {
		fuzz.Fatal(err)
	}

	fuzz.Fuzz(func(
		t *testing.T,
		name string,
		version uint32,
		payload []byte,
		metadataKey string,
		metadataValue string,
	) {
		encoded, err := eventsourcing.NewEncodedEvent(
			eventsourcing.EncodedEventInput{
				Name:        name,
				Version:     eventsourcing.SchemaVersion(version),
				ContentType: eventsourcing.JSONContentType,
				Payload:     payload,
			},
		)
		if err != nil {
			return
		}
		input, err := eventsourcing.NewUpcastEvent(
			encoded,
			map[string]string{metadataKey: metadataValue},
		)
		if err != nil {
			return
		}

		first, firstErr := chain.Upcast(input)
		second, secondErr := chain.Upcast(input)
		if firstErr != nil || secondErr != nil {
			t.Fatalf("Upcast() errors = %v, %v", firstErr, secondErr)
		}
		if !equalUpcastOutput(first, second) {
			t.Fatalf("Upcast() is non-deterministic")
		}
	})
}

func equalUpcastOutput(
	left []eventsourcing.UpcastEvent,
	right []eventsourcing.UpcastEvent,
) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		leftEvent := left[index].Event()
		rightEvent := right[index].Event()
		if leftEvent.Name() != rightEvent.Name() ||
			leftEvent.Version() != rightEvent.Version() ||
			leftEvent.ContentType() != rightEvent.ContentType() ||
			!bytes.Equal(leftEvent.Payload(), rightEvent.Payload()) ||
			!maps.Equal(left[index].Metadata(), right[index].Metadata()) {
			return false
		}
	}

	return true
}
