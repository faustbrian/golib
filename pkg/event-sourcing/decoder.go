package eventsourcing

import (
	"context"
	"fmt"
)

// LogicalEvent is one decoded event produced from a persisted source message.
// Segment coordinates make split upcasts observable without inventing stored
// stream or global positions.
type LogicalEvent struct {
	source       Message
	event        DecodedEvent
	metadata     map[string]string
	segmentIndex uint32
	segmentCount uint32
}

// SourceMessage returns the immutable persisted message that produced this
// logical event. Its stored event and metadata remain unchanged.
func (event LogicalEvent) SourceMessage() Message {
	return event.source
}

// Event returns the decoded application event after upcasting.
func (event LogicalEvent) Event() DecodedEvent {
	return event.event
}

// Metadata returns transformed application metadata as a defensive copy.
func (event LogicalEvent) Metadata() map[string]string {
	return cloneMetadata(event.metadata)
}

// SegmentIndex returns the zero-based logical position within the source
// message's split upcast result.
func (event LogicalEvent) SegmentIndex() uint32 {
	return event.segmentIndex
}

// SegmentCount returns the total logical events produced by the source
// message.
func (event LogicalEvent) SegmentCount() uint32 {
	return event.segmentCount
}

// IsZero reports whether the logical event has not been assigned.
func (event LogicalEvent) IsZero() bool {
	return event.source.ID().IsZero() || event.event.IsZero() ||
		event.segmentCount == 0
}

// EventDecoder composes payload decoding with deterministic read-boundary
// upcasting. It is immutable after construction and starts no work.
type EventDecoder struct {
	codec     PayloadCodec
	upcasters Upcaster
}

// NewEventDecoder validates one reusable event read boundary.
func NewEventDecoder(
	codec PayloadCodec,
	upcasters Upcaster,
) (*EventDecoder, error) {
	if codec == nil || upcasters == nil {
		return nil, invalid("event_decoder", "dependencies must be assigned")
	}

	return &EventDecoder{codec: codec, upcasters: upcasters}, nil
}

// Decode returns zero, one, or many ordered logical events for one persisted
// message. Upcasting never modifies the stored message. A reviewed drop
// returns an empty slice.
func (decoder *EventDecoder) Decode(message Message) ([]LogicalEvent, error) {
	return decoder.DecodeContext(context.Background(), message)
}

// DecodeContext returns ordered logical events while propagating caller
// cancellation and context to optional codec and upcaster extensions.
func (decoder *EventDecoder) DecodeContext(
	ctx context.Context,
	message Message,
) ([]LogicalEvent, error) {
	if ctx == nil || decoder == nil || message.ID().IsZero() {
		return nil, ErrInvalidArgument
	}
	input := UpcastEvent{
		event:    cloneEvent(message.pending.event),
		metadata: cloneMetadata(message.pending.metadata),
	}
	upcasted, err := upcastWithContext(ctx, decoder.upcasters, input)
	if err != nil {
		return nil, err
	}
	logical := make([]LogicalEvent, len(upcasted))
	for index, encoded := range upcasted {
		decoded, err := decodePayload(ctx, decoder.codec, encoded.Event())
		if err != nil {
			return nil, err
		}
		if decoded.IsZero() {
			return nil, fmt.Errorf(
				"%w: decoded logical event is unassigned",
				ErrCorruptHistory,
			)
		}
		logical[index] = LogicalEvent{
			source:       message,
			event:        decoded,
			metadata:     encoded.Metadata(),
			segmentIndex: uint32(index),
			segmentCount: uint32(len(upcasted)),
		}
	}

	return logical, nil
}
