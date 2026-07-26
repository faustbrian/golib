package eventtest

import (
	"context"
	"errors"
	"fmt"
	"sync"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

var (
	// ErrConformance classifies an observed mismatch in a testing helper.
	ErrConformance = errors.New("eventtest conformance failure")
	// ErrSequenceExhausted reports that a deterministic ID sequence has no
	// unused values.
	ErrSequenceExhausted = errors.New("eventtest message ID sequence exhausted")
)

// ExpectedEvent identifies one decoded event and optionally validates its
// application value. Matcher error text is treated as sensitive and redacted.
type ExpectedEvent struct {
	Name    string
	Version eventsourcing.SchemaVersion
	Value   func(any) error
}

// MatchEvent compares stable identity and an optional application value
// predicate without formatting the event value or predicate error.
func MatchEvent(
	event eventsourcing.DecodedEvent,
	expected ExpectedEvent,
) error {
	if event.IsZero() || !validExpectedEvent(expected.Name, expected.Version) {
		return eventsourcing.ErrInvalidArgument
	}
	if event.Name().String() != expected.Name {
		return fmt.Errorf("%w: event name differs", ErrConformance)
	}
	if event.Version() != expected.Version {
		return fmt.Errorf("%w: event schema version differs", ErrConformance)
	}
	if expected.Value != nil && expected.Value(event.Value()) != nil {
		return fmt.Errorf("%w: event value predicate failed", ErrConformance)
	}

	return nil
}

// MatchMetadata compares complete metadata maps. Diagnostics identify only the
// mismatched key and never include metadata values.
func MatchMetadata(actual, expected map[string]string) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("%w: metadata key count differs", ErrConformance)
	}
	for key := range expected {
		actualValue, exists := actual[key]
		if !exists {
			return fmt.Errorf("%w: metadata key %q is missing", ErrConformance, key)
		}
		if actualValue != expected[key] {
			return fmt.Errorf("%w: metadata key %q differs", ErrConformance, key)
		}
	}
	return nil
}

// CheckPayloadRoundTrip verifies stable event identity and application-defined
// value equality through one payload codec.
func CheckPayloadRoundTrip(
	codec eventsourcing.PayloadCodec,
	event eventsourcing.DecodedEvent,
	equal func(any, any) bool,
) error {
	if codec == nil || event.IsZero() || equal == nil {
		return eventsourcing.ErrInvalidArgument
	}
	encoded, err := codec.Encode(event)
	if err != nil {
		return err
	}
	if encoded.Name() != event.Name() || encoded.Version() != event.Version() {
		return fmt.Errorf("%w: encoded identity differs", ErrConformance)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil {
		return err
	}
	if decoded.Name() != event.Name() || decoded.Version() != event.Version() {
		return fmt.Errorf("%w: decoded identity differs", ErrConformance)
	}
	if !equal(event.Value(), decoded.Value()) {
		return fmt.Errorf("%w: decoded value differs", ErrConformance)
	}

	return nil
}

// ExpectedUpcastEvent identifies one logical upcast output. Payload predicate
// errors are redacted.
type ExpectedUpcastEvent struct {
	Name     string
	Version  eventsourcing.SchemaVersion
	Metadata map[string]string
	Payload  func([]byte) error
}

// CheckUpcast verifies an ordered deterministic upcast result.
func CheckUpcast(
	chain *eventsourcing.UpcasterChain,
	input eventsourcing.UpcastEvent,
	expected []ExpectedUpcastEvent,
) error {
	if chain == nil || input.IsZero() ||
		len(expected) > eventsourcing.MaxUpcastSegments {
		return eventsourcing.ErrInvalidArgument
	}
	actual, err := chain.Upcast(input)
	if err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("%w: upcast output length differs", ErrConformance)
	}
	for index, expectedEvent := range expected {
		if !validExpectedEvent(expectedEvent.Name, expectedEvent.Version) {
			return eventsourcing.ErrInvalidArgument
		}
		event := actual[index].Event()
		if event.Name().String() != expectedEvent.Name {
			return fmt.Errorf("%w: upcast event %d name differs", ErrConformance, index)
		}
		if event.Version() != expectedEvent.Version {
			return fmt.Errorf(
				"%w: upcast event %d schema version differs",
				ErrConformance,
				index,
			)
		}
		if err := MatchMetadata(
			actual[index].Metadata(),
			expectedEvent.Metadata,
		); err != nil {
			return fmt.Errorf("%w: upcast event %d metadata differs", ErrConformance, index)
		}
		if expectedEvent.Payload != nil &&
			expectedEvent.Payload(event.Payload()) != nil {
			return fmt.Errorf(
				"%w: upcast event %d payload predicate failed",
				ErrConformance,
				index,
			)
		}
	}

	return nil
}

// MessageIDSequence is a concurrency-safe deterministic generator for tests.
type MessageIDSequence struct {
	mutex sync.Mutex
	ids   []eventsourcing.MessageID
	next  int
}

// NewMessageIDSequence validates and owns a non-empty bounded ID sequence.
func NewMessageIDSequence(values ...string) (*MessageIDSequence, error) {
	if len(values) == 0 || len(values) > eventsourcing.MaxAppendMessages {
		return nil, eventsourcing.ErrInvalidArgument
	}
	ids := make([]eventsourcing.MessageID, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		id, err := eventsourcing.NewMessageID(value)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, eventsourcing.ErrInvalidArgument
		}
		seen[value] = struct{}{}
		ids[index] = id
	}

	return &MessageIDSequence{ids: ids}, nil
}

// NewMessageID returns the next identifier exactly once.
func (sequence *MessageIDSequence) NewMessageID(
	ctx context.Context,
) (eventsourcing.MessageID, error) {
	if sequence == nil || ctx == nil {
		return eventsourcing.MessageID{}, eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return eventsourcing.MessageID{}, err
	}

	sequence.mutex.Lock()
	defer sequence.mutex.Unlock()
	if sequence.next >= len(sequence.ids) {
		return eventsourcing.MessageID{}, ErrSequenceExhausted
	}
	id := sequence.ids[sequence.next]
	sequence.next++

	return id, nil
}

func validExpectedEvent(
	name string,
	version eventsourcing.SchemaVersion,
) bool {
	_, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    name,
		Version: version,
		Value:   struct{}{},
	})

	return err == nil
}

var _ eventsourcing.MessageIDGenerator = (*MessageIDSequence)(nil)
