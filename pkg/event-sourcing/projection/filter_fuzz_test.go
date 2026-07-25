package projection

import (
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func FuzzReplayFilter(fuzz *testing.F) {
	fuzz.Add(
		"account",
		"account.changed",
		uint64(1),
		uint64(2),
		int64(1),
		int64(2),
	)
	fuzz.Add("", "", uint64(0), uint64(0), int64(0), int64(0))

	fuzz.Fuzz(func(
		t *testing.T,
		aggregateType string,
		eventName string,
		fromPosition uint64,
		throughPosition uint64,
		recordedFrom int64,
		recordedThrough int64,
	) {
		input := ReplayFilterInput{
			AggregateTypes: []string{aggregateType},
			EventNames:     []string{eventName},
			FromPosition:   eventsourcing.GlobalPosition(fromPosition),
			ThroughPosition: eventsourcing.GlobalPosition(
				throughPosition,
			),
			RecordedFrom: time.Unix(
				recordedFrom%4_102_444_800,
				0,
			),
			RecordedThrough: time.Unix(
				recordedThrough%4_102_444_800,
				0,
			),
		}
		filter, err := NewReplayFilter(input)
		if err != nil {
			if !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
				filter.Valid() {
				t.Fatalf("NewReplayFilter() = %#v, %v", filter, err)
			}

			return
		}
		if !filter.Valid() {
			t.Fatal("successful filter is invalid")
		}
		message := internalProjectionMessage(t, 1)
		equivalent, equivalentErr := NewReplayFilter(input)
		if equivalentErr != nil ||
			filter.Match(message) != equivalent.Match(message) {
			t.Fatal("filter result is nondeterministic")
		}
	})
}
