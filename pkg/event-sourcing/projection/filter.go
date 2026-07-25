package projection

import (
	"fmt"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

const (
	// MaxReplayFilterValues bounds the combined exact-match allowlists.
	MaxReplayFilterValues = 1_000
)

// ReplayFilterInput supplies inclusive deterministic replay criteria.
//
// Empty criteria match every assigned persisted message.
type ReplayFilterInput struct {
	Streams         []eventsourcing.StreamID
	AggregateTypes  []string
	EventNames      []string
	FromPosition    eventsourcing.GlobalPosition
	ThroughPosition eventsourcing.GlobalPosition
	RecordedFrom    time.Time
	RecordedThrough time.Time
}

// ReplayFilter is an immutable deterministic conjunction of replay criteria.
//
// Each non-empty allowlist must match. Position and time bounds are inclusive.
// The zero value is invalid and matches nothing.
type ReplayFilter struct {
	streams         map[eventsourcing.StreamID]struct{}
	aggregateTypes  map[string]struct{}
	eventNames      map[string]struct{}
	fromPosition    eventsourcing.GlobalPosition
	throughPosition eventsourcing.GlobalPosition
	recordedFrom    time.Time
	recordedThrough time.Time
	valid           bool
}

// NewReplayFilter validates and owns replay criteria.
func NewReplayFilter(input ReplayFilterInput) (ReplayFilter, error) {
	valueCount := len(input.Streams) +
		len(input.AggregateTypes) +
		len(input.EventNames)
	if valueCount > MaxReplayFilterValues {
		return ReplayFilter{}, invalidFilter(
			"exact-match allowlists exceed the combined limit",
		)
	}
	if input.FromPosition != 0 &&
		input.ThroughPosition != 0 &&
		input.ThroughPosition < input.FromPosition {
		return ReplayFilter{}, invalidFilter(
			"position range must be ordered",
		)
	}

	recordedFrom := normalizeReplayTime(input.RecordedFrom)
	recordedThrough := normalizeReplayTime(input.RecordedThrough)
	if !recordedFrom.IsZero() &&
		!recordedThrough.IsZero() &&
		recordedThrough.Before(recordedFrom) {
		return ReplayFilter{}, invalidFilter("time range must be ordered")
	}

	streams := make(
		map[eventsourcing.StreamID]struct{},
		len(input.Streams),
	)
	for _, stream := range input.Streams {
		if stream.IsZero() {
			return ReplayFilter{}, invalidFilter(
				"stream allowlist contains an invalid stream",
			)
		}
		if _, exists := streams[stream]; exists {
			return ReplayFilter{}, invalidFilter(
				"stream allowlist contains a duplicate",
			)
		}
		streams[stream] = struct{}{}
	}

	aggregateTypes := make(map[string]struct{}, len(input.AggregateTypes))
	for _, aggregateType := range input.AggregateTypes {
		if _, err := eventsourcing.NewStreamID(
			aggregateType,
			"replay-filter",
		); err != nil {
			return ReplayFilter{}, invalidFilter(
				"aggregate-type allowlist contains an invalid value",
			)
		}
		if _, exists := aggregateTypes[aggregateType]; exists {
			return ReplayFilter{}, invalidFilter(
				"aggregate-type allowlist contains a duplicate",
			)
		}
		aggregateTypes[aggregateType] = struct{}{}
	}

	eventNames := make(map[string]struct{}, len(input.EventNames))
	for _, eventName := range input.EventNames {
		if _, err := eventsourcing.NewEventName(eventName); err != nil {
			return ReplayFilter{}, invalidFilter(
				"event-name allowlist contains an invalid value",
			)
		}
		if _, exists := eventNames[eventName]; exists {
			return ReplayFilter{}, invalidFilter(
				"event-name allowlist contains a duplicate",
			)
		}
		eventNames[eventName] = struct{}{}
	}

	return ReplayFilter{
		streams:         streams,
		aggregateTypes:  aggregateTypes,
		eventNames:      eventNames,
		fromPosition:    input.FromPosition,
		throughPosition: input.ThroughPosition,
		recordedFrom:    recordedFrom,
		recordedThrough: recordedThrough,
		valid:           true,
	}, nil
}

// Valid reports whether the filter was constructed successfully.
func (filter ReplayFilter) Valid() bool {
	return filter.valid
}

// Match reports whether an assigned persisted message satisfies every
// configured criterion.
func (filter ReplayFilter) Match(message eventsourcing.Message) bool {
	if !filter.valid ||
		message.ID().IsZero() ||
		message.Stream().IsZero() ||
		message.EventName().IsZero() ||
		message.RecordedAt().IsZero() {
		return false
	}

	stream := message.Stream()
	if len(filter.streams) != 0 {
		if _, matches := filter.streams[stream]; !matches {
			return false
		}
	}
	if len(filter.aggregateTypes) != 0 {
		if _, matches := filter.aggregateTypes[stream.AggregateType()]; !matches {
			return false
		}
	}
	if len(filter.eventNames) != 0 {
		eventName := message.EventName().String()
		if _, matches := filter.eventNames[eventName]; !matches {
			return false
		}
	}

	position, hasPosition := message.GlobalPosition()
	if filter.fromPosition != 0 &&
		(!hasPosition || position < filter.fromPosition) {
		return false
	}
	if filter.throughPosition != 0 &&
		(!hasPosition || position > filter.throughPosition) {
		return false
	}
	recordedAt := message.RecordedAt()
	if !filter.recordedFrom.IsZero() &&
		recordedAt.Before(filter.recordedFrom) {
		return false
	}
	if !filter.recordedThrough.IsZero() &&
		recordedAt.After(filter.recordedThrough) {
		return false
	}

	return true
}

func normalizeReplayTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}

	return value.UTC().Truncate(time.Microsecond)
}

func invalidFilter(reason string) error {
	return fmt.Errorf(
		"%w: replay filter %s",
		eventsourcing.ErrInvalidArgument,
		reason,
	)
}
