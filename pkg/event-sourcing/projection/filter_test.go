package projection

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestReplayFilterMatchesEveryConfiguredDimension(t *testing.T) {
	t.Parallel()

	accountOne := filterStream(t, "account", "account-1")
	filter := newReplayFilter(t, ReplayFilterInput{
		Streams:         []eventsourcing.StreamID{accountOne},
		AggregateTypes:  []string{"account"},
		EventNames:      []string{"account.changed"},
		FromPosition:    2,
		ThroughPosition: 3,
		RecordedFrom:    filterTime(1),
		RecordedThrough: filterTime(2),
	})
	if !filter.Match(filterMessage(
		t,
		accountOne,
		"account.changed",
		2,
		filterTime(1),
	)) {
		t.Fatal("matching message was rejected")
	}

	tests := map[string]eventsourcing.Message{
		"stream": filterMessage(
			t,
			filterStream(t, "account", "account-2"),
			"account.changed",
			2,
			filterTime(1),
		),
		"aggregate type": filterMessage(
			t,
			filterStream(t, "customer", "account-1"),
			"account.changed",
			2,
			filterTime(1),
		),
		"event": filterMessage(
			t,
			accountOne,
			"account.closed",
			2,
			filterTime(1),
		),
		"before position": filterMessage(
			t,
			accountOne,
			"account.changed",
			1,
			filterTime(1),
		),
		"after position": filterMessage(
			t,
			accountOne,
			"account.changed",
			4,
			filterTime(1),
		),
		"before time": filterMessage(
			t,
			accountOne,
			"account.changed",
			2,
			filterTime(0),
		),
		"after time": filterMessage(
			t,
			accountOne,
			"account.changed",
			2,
			filterTime(3),
		),
	}
	for name, message := range tests {
		message := message
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if filter.Match(message) {
				t.Fatal("non-matching message was accepted")
			}
		})
	}
}

func TestReplayFilterValidatesAndOwnsConfiguration(t *testing.T) {
	t.Parallel()

	streams := []eventsourcing.StreamID{
		filterStream(t, "account", "account-1"),
	}
	aggregateTypes := []string{"account"}
	eventNames := []string{"account.changed"}
	filter := newReplayFilter(t, ReplayFilterInput{
		Streams:        streams,
		AggregateTypes: aggregateTypes,
		EventNames:     eventNames,
	})
	streams[0] = filterStream(t, "account", "account-2")
	aggregateTypes[0] = "customer"
	eventNames[0] = "account.closed"
	if !filter.Valid() ||
		!filter.Match(filterMessage(
			t,
			filterStream(t, "account", "account-1"),
			"account.changed",
			1,
			filterTime(1),
		)) {
		t.Fatal("filter did not own its configuration")
	}

	empty := newReplayFilter(t, ReplayFilterInput{})
	if !empty.Match(filterMessage(
		t,
		filterStream(t, "account", "account-1"),
		"account.changed",
		1,
		filterTime(1),
	)) {
		t.Fatal("empty configured filter did not match")
	}
	if (ReplayFilter{}).Valid() ||
		(ReplayFilter{}).Match(eventsourcing.Message{}) ||
		empty.Match(eventsourcing.Message{}) {
		t.Fatal("zero filter or message was accepted")
	}
	aggregateOnly := newReplayFilter(t, ReplayFilterInput{
		AggregateTypes: []string{"account"},
	})
	if aggregateOnly.Match(filterMessage(
		t,
		filterStream(t, "customer", "customer-1"),
		"account.changed",
		1,
		filterTime(1),
	)) {
		t.Fatal("aggregate-type mismatch was accepted")
	}

	tooMany := make([]string, MaxReplayFilterValues+1)
	for index := range tooMany {
		tooMany[index] = "event." + strings.Repeat("a", index/26) +
			string(rune('a'+index%26))
	}
	tests := map[string]ReplayFilterInput{
		"zero stream": {
			Streams: []eventsourcing.StreamID{{}},
		},
		"duplicate stream": {
			Streams: []eventsourcing.StreamID{
				filterStream(t, "account", "account-1"),
				filterStream(t, "account", "account-1"),
			},
		},
		"invalid aggregate type": {
			AggregateTypes: []string{"Account"},
		},
		"duplicate aggregate type": {
			AggregateTypes: []string{"account", "account"},
		},
		"invalid event name": {
			EventNames: []string{"Account.Changed"},
		},
		"duplicate event name": {
			EventNames: []string{"account.changed", "account.changed"},
		},
		"too many values": {
			EventNames: tooMany,
		},
		"position range": {
			FromPosition:    3,
			ThroughPosition: 2,
		},
		"time range": {
			RecordedFrom:    filterTime(2),
			RecordedThrough: filterTime(1),
		},
	}
	for name, input := range tests {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			filter, err := NewReplayFilter(input)
			if filter.Valid() ||
				!errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("NewReplayFilter() = %#v, %v", filter, err)
			}
		})
	}
}

func TestNewRunnerRejectsInvalidReplayFilter(t *testing.T) {
	t.Parallel()

	config := internalRunnerConfig()
	config.Filter = &ReplayFilter{}
	runner, err := NewRunner(config)
	if runner != nil ||
		!errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewRunner() = %#v, %v", runner, err)
	}
}

func TestRunnerFiltersReplayAndCheckpointsScannedMessages(t *testing.T) {
	t.Parallel()

	ignored := filterMessage(
		t,
		filterStream(t, "account", "account-1"),
		"account.ignored",
		1,
		filterTime(1),
	)
	selected := filterMessage(
		t,
		filterStream(t, "account", "account-1"),
		"account.changed",
		2,
		filterTime(2),
	)
	filter := newReplayFilter(t, ReplayFilterInput{
		EventNames: []string{"account.changed"},
	})
	var handled []string
	var saves [][2]eventsourcing.GlobalPosition
	config := internalRunnerConfig()
	config.BatchSize = 2
	config.Filter = &filter
	config.Reader = readerWithIterator(&internalIterator{
		messages: []eventsourcing.Message{ignored, selected},
	})
	config.Handler = func(
		_ context.Context,
		delivery eventsourcing.Delivery,
	) error {
		handled = append(handled, delivery.Message().ID().String())

		return nil
	}
	config.Checkpoints = internalCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			return 0, ErrCheckpointNotFound
		},
		save: func(
			_ context.Context,
			_ string,
			expected eventsourcing.GlobalPosition,
			next eventsourcing.GlobalPosition,
		) error {
			saves = append(
				saves,
				[2]eventsourcing.GlobalPosition{expected, next},
			)

			return nil
		},
	}
	runner := internalRunner(t, config)

	result, err := runner.RunBatch(context.Background())
	if err != nil ||
		result.Scanned() != 2 ||
		result.Filtered() != 1 ||
		result.Handled() != 1 ||
		result.Checkpointed() != 2 ||
		result.Checkpoint() != 2 ||
		len(handled) != 1 ||
		handled[0] != "message-2" ||
		len(saves) != 2 ||
		saves[0] != [2]eventsourcing.GlobalPosition{0, 1} ||
		saves[1] != [2]eventsourcing.GlobalPosition{1, 2} {
		t.Fatalf(
			"RunBatch() = %#v, %v handled=%v saves=%v",
			result,
			err,
			handled,
			saves,
		)
	}
}

func TestRunnerReportsFilteredCheckpointFailure(t *testing.T) {
	t.Parallel()

	checkpointFailure := errors.New("checkpoint failed")
	filter := newReplayFilter(t, ReplayFilterInput{
		EventNames: []string{"account.selected"},
	})
	config := internalRunnerConfig()
	config.Filter = &filter
	config.Reader = readerWithIterator(&internalIterator{
		messages: []eventsourcing.Message{
			internalProjectionMessage(t, 1),
		},
	})
	config.Handler = func(context.Context, eventsourcing.Delivery) error {
		t.Fatal("handler ran for filtered message")

		return nil
	}
	config.Checkpoints = internalCheckpointStore{
		load: func(
			context.Context,
			string,
		) (eventsourcing.GlobalPosition, error) {
			return 0, ErrCheckpointNotFound
		},
		save: func(
			context.Context,
			string,
			eventsourcing.GlobalPosition,
			eventsourcing.GlobalPosition,
		) error {
			return checkpointFailure
		},
	}
	runner := internalRunner(t, config)

	result, err := runner.RunBatch(context.Background())
	if !errors.Is(err, checkpointFailure) ||
		result.Scanned() != 1 ||
		result.Filtered() != 1 ||
		result.Handled() != 0 ||
		result.Checkpointed() != 0 ||
		result.Checkpoint() != 0 {
		t.Fatalf("RunBatch() = %#v, %v", result, err)
	}
}

func newReplayFilter(t *testing.T, input ReplayFilterInput) ReplayFilter {
	t.Helper()

	filter, err := NewReplayFilter(input)
	if err != nil {
		t.Fatal(err)
	}

	return filter
}

func filterStream(
	t *testing.T,
	aggregateType string,
	aggregateID string,
) eventsourcing.StreamID {
	t.Helper()

	stream, err := eventsourcing.NewStreamID(aggregateType, aggregateID)
	if err != nil {
		t.Fatal(err)
	}

	return stream
}

func filterMessage(
	t *testing.T,
	stream eventsourcing.StreamID,
	eventName string,
	position eventsourcing.GlobalPosition,
	recordedAt time.Time,
) eventsourcing.Message {
	t.Helper()

	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        eventName,
			Version:     1,
			ContentType: "application/json",
			Payload:     []byte("{}"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "message-" + string(rune('0'+position)),
			Stream:     stream,
			Event:      event,
			RecordedAt: recordedAt,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  uint64(position),
		GlobalPosition: position,
	})
	if err != nil {
		t.Fatal(err)
	}

	return message
}

func filterTime(second int) time.Time {
	return time.Date(2026, time.July, 25, 15, 0, second, 0, time.UTC)
}
