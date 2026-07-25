package eventsourcing

import "fmt"

const (
	// MaxUpcastSegments is the maximum number of logical events one stored
	// event may expand into.
	MaxUpcastSegments = 1024
)

// DecodedEventInput supplies an explicitly identified application event.
type DecodedEventInput struct {
	Name    string
	Version SchemaVersion
	Value   any
}

// DecodedEvent is an immutable-by-contract application event with a stable
// persisted identity.
//
// The application owns immutability of Value. The lifecycle never modifies it.
type DecodedEvent struct {
	name    EventName
	version SchemaVersion
	value   any
}

// NewDecodedEvent validates an explicitly identified application event.
func NewDecodedEvent(input DecodedEventInput) (DecodedEvent, error) {
	if !validName(input.Name, MaxEventNameBytes) {
		return DecodedEvent{}, invalid("event_name", "must be a non-empty canonical name")
	}
	if input.Version == 0 {
		return DecodedEvent{}, invalid("event_schema_version", "must be greater than zero")
	}
	if input.Value == nil {
		return DecodedEvent{}, invalid("event_value", "must be assigned")
	}

	return DecodedEvent{
		name:    EventName{value: input.Name},
		version: input.Version,
		value:   input.Value,
	}, nil
}

// Name returns the stable persisted event identity.
func (event DecodedEvent) Name() EventName {
	return event.name
}

// Version returns the event schema version.
func (event DecodedEvent) Version() SchemaVersion {
	return event.version
}

// Value returns the application-owned immutable event value.
func (event DecodedEvent) Value() any {
	return event.value
}

// IsZero reports whether the event has not been assigned.
func (event DecodedEvent) IsZero() bool {
	return event.name.value == "" && event.version == 0 && event.value == nil
}

// HistoricalEventInput identifies one logical event produced from one stored
// stream version. Segment coordinates make split upcasts explicit.
type HistoricalEventInput struct {
	SourceVersion uint64
	SegmentIndex  uint32
	SegmentCount  uint32
	Event         DecodedEvent
}

// HistoricalEvent is one decoded logical event in an ordered stored history.
type HistoricalEvent struct {
	sourceVersion uint64
	segmentIndex  uint32
	segmentCount  uint32
	event         DecodedEvent
}

// NewHistoricalEvent validates a logical event's stored source coordinates.
func NewHistoricalEvent(input HistoricalEventInput) (HistoricalEvent, error) {
	if input.SourceVersion == 0 {
		return HistoricalEvent{}, invalid("source_version", "must be greater than zero")
	}
	if input.SegmentCount == 0 || input.SegmentCount > MaxUpcastSegments {
		return HistoricalEvent{}, invalid("segment_count", "must be within the expansion limit")
	}
	if input.SegmentIndex >= input.SegmentCount {
		return HistoricalEvent{}, invalid("segment_index", "must be less than segment count")
	}
	if input.Event.IsZero() {
		return HistoricalEvent{}, invalid("event", "must be assigned")
	}

	return HistoricalEvent{
		sourceVersion: input.SourceVersion,
		segmentIndex:  input.SegmentIndex,
		segmentCount:  input.SegmentCount,
		event:         input.Event,
	}, nil
}

// SourceVersion returns the stored stream version that produced this event.
func (event HistoricalEvent) SourceVersion() uint64 {
	return event.sourceVersion
}

// SegmentIndex returns the zero-based position within a split upcast.
func (event HistoricalEvent) SegmentIndex() uint32 {
	return event.segmentIndex
}

// SegmentCount returns the number of logical events from the stored event.
func (event HistoricalEvent) SegmentCount() uint32 {
	return event.segmentCount
}

// Event returns the decoded application event.
func (event HistoricalEvent) Event() DecodedEvent {
	return event.event
}

// ChangeSet is an immutable snapshot of one aggregate's pending events.
//
// A change set is valid only for the lifecycle instance and generation that
// created it.
type ChangeSet struct {
	owner      *Lifecycle
	generation uint64
	base       uint64
	events     []DecodedEvent
}

// BaseVersion returns the committed version the changes build on.
func (changes ChangeSet) BaseVersion() uint64 {
	return changes.base
}

// Len returns the number of pending events.
func (changes ChangeSet) Len() int {
	return len(changes.events)
}

// Empty reports whether there are no pending events.
func (changes ChangeSet) Empty() bool {
	return len(changes.events) == 0
}

// Events returns a defensive copy of pending event values.
func (changes ChangeSet) Events() []DecodedEvent {
	return cloneDecodedEvents(changes.events)
}

// Lifecycle tracks committed and pending aggregate versions.
//
// The zero value is ready for a new aggregate. A Lifecycle must not be copied
// after first use and is not safe for concurrent mutation.
type Lifecycle struct {
	committed  uint64
	pending    []DecodedEvent
	generation uint64
	poisoned   bool
	transition bool
}

// Record applies an event immediately and records it when application
// succeeds.
func (lifecycle *Lifecycle) Record(
	event DecodedEvent,
	apply func(DecodedEvent) error,
) error {
	if lifecycle.poisoned {
		return ErrLifecyclePoisoned
	}
	if lifecycle.transition {
		return ErrInvalidLifecycleState
	}
	if event.IsZero() {
		return invalid("event", "must be assigned")
	}
	if apply == nil {
		return invalid("apply", "must be assigned")
	}
	if lifecycle.committed == ^uint64(0) ||
		uint64(len(lifecycle.pending)) >= ^uint64(0)-lifecycle.committed {
		return ErrVersionOverflow
	}
	if err := lifecycle.apply(event, apply); err != nil {
		lifecycle.poisoned = true

		return fmt.Errorf("%w: apply recorded event: %w", ErrLifecyclePoisoned, err)
	}

	lifecycle.pending = append(lifecycle.pending, event)
	lifecycle.generation++

	return nil
}

// Reconstitute applies a structurally valid ordered history without recording
// pending events. The base version supports state restored from a snapshot.
func (lifecycle *Lifecycle) Reconstitute(
	baseVersion uint64,
	history []HistoricalEvent,
	apply func(DecodedEvent) error,
) error {
	if lifecycle.poisoned {
		return ErrLifecyclePoisoned
	}
	if lifecycle.transition ||
		lifecycle.committed != 0 ||
		len(lifecycle.pending) != 0 ||
		lifecycle.generation != 0 {
		return ErrInvalidLifecycleState
	}
	if apply == nil {
		return invalid("apply", "must be assigned")
	}
	if err := validateHistory(baseVersion, history); err != nil {
		lifecycle.poisoned = true

		return err
	}

	for _, historical := range history {
		if err := lifecycle.apply(historical.event, apply); err != nil {
			lifecycle.poisoned = true

			return fmt.Errorf("%w: apply historical event: %w", ErrLifecyclePoisoned, err)
		}
	}

	lifecycle.committed = baseVersion
	if len(history) != 0 {
		lifecycle.committed = history[len(history)-1].sourceVersion
	}
	lifecycle.generation++

	return nil
}

// Poisoned reports whether failed event application made this lifecycle unsafe
// to save or reuse.
func (lifecycle *Lifecycle) Poisoned() bool {
	return lifecycle.poisoned
}

// CommittedVersion returns the last durably acknowledged stream version.
func (lifecycle *Lifecycle) CommittedVersion() uint64 {
	return lifecycle.committed
}

// Version returns the aggregate version including pending events.
func (lifecycle *Lifecycle) Version() uint64 {
	return lifecycle.committed + uint64(len(lifecycle.pending))
}

// Changes returns an immutable snapshot without releasing pending events.
func (lifecycle *Lifecycle) Changes() (ChangeSet, error) {
	if lifecycle.poisoned {
		return ChangeSet{}, ErrLifecyclePoisoned
	}
	if lifecycle.transition {
		return ChangeSet{}, ErrInvalidLifecycleState
	}

	return ChangeSet{
		owner:      lifecycle,
		generation: lifecycle.generation,
		base:       lifecycle.committed,
		events:     cloneDecodedEvents(lifecycle.pending),
	}, nil
}

func (lifecycle *Lifecycle) apply(
	event DecodedEvent,
	apply func(DecodedEvent) error,
) (err error) {
	lifecycle.transition = true

	defer func() {
		lifecycle.transition = false
		if recover() != nil {
			err = ErrApplyPanic
		}
	}()

	return apply(event)
}

func validateHistory(baseVersion uint64, history []HistoricalEvent) error {
	expectedVersion := baseVersion
	for index := 0; index < len(history); {
		if expectedVersion == ^uint64(0) {
			return fmt.Errorf("%w: source version overflow", ErrCorruptHistory)
		}
		expectedVersion++

		first := history[index]
		if first.event.IsZero() ||
			first.sourceVersion != expectedVersion ||
			first.segmentIndex != 0 ||
			first.segmentCount == 0 ||
			first.segmentCount > MaxUpcastSegments {
			return fmt.Errorf("%w: invalid source sequence", ErrCorruptHistory)
		}
		if uint64(first.segmentCount) > uint64(len(history)-index) {
			return fmt.Errorf("%w: incomplete split event", ErrCorruptHistory)
		}

		for segment := uint32(0); segment < first.segmentCount; segment++ {
			current := history[index+int(segment)]
			if current.event.IsZero() ||
				current.sourceVersion != expectedVersion ||
				current.segmentIndex != segment ||
				current.segmentCount != first.segmentCount {
				return fmt.Errorf("%w: invalid split sequence", ErrCorruptHistory)
			}
		}

		index += int(first.segmentCount)
	}

	return nil
}

// Acknowledge releases exactly the change set represented by messages.
func (lifecycle *Lifecycle) Acknowledge(
	changes ChangeSet,
	prepared []PendingMessage,
	messages []Message,
) error {
	if lifecycle.poisoned {
		return ErrLifecyclePoisoned
	}
	if lifecycle.transition {
		return ErrInvalidLifecycleState
	}
	if changes.owner != lifecycle ||
		changes.generation != lifecycle.generation ||
		changes.base != lifecycle.committed ||
		len(changes.events) == 0 ||
		len(changes.events) != len(lifecycle.pending) {
		return ErrInvalidChangeSet
	}
	if len(prepared) != len(changes.events) ||
		len(messages) != len(changes.events) {
		return lifecycle.persistenceMismatch()
	}

	for index, event := range changes.events {
		pending := prepared[index]
		message := messages[index]
		if pending.event.name != event.name ||
			pending.event.version != event.version ||
			!pendingMessagesEqual(pending, message.pending) ||
			message.StreamVersion() != changes.base+uint64(index)+1 ||
			message.pending.event.name != event.name ||
			message.pending.event.version != event.version {
			return lifecycle.persistenceMismatch()
		}
	}

	lifecycle.committed += uint64(len(changes.events))
	lifecycle.pending = nil
	lifecycle.generation++

	return nil
}

func (lifecycle *Lifecycle) persistenceMismatch() error {
	lifecycle.poisoned = true

	return fmt.Errorf("%w: %w", ErrLifecyclePoisoned, ErrPersistenceMismatch)
}

func cloneDecodedEvents(input []DecodedEvent) []DecodedEvent {
	if input == nil {
		return nil
	}

	output := make([]DecodedEvent, len(input))
	copy(output, input)

	return output
}
