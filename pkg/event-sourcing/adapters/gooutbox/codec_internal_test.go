package gooutbox

import (
	"errors"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/outbox"
)

func TestEnvelopeCodecRejectsInvalidConstructionAndEncoding(t *testing.T) {
	t.Parallel()

	if _, err := NewEnvelopeCodec(nil, outbox.DefaultLimits()); !errors.Is(
		err,
		ErrResolverRequired,
	) {
		t.Fatalf("nil resolver error = %v", err)
	}
	if _, err := NewEnvelopeCodec(
		FixedTopic("events"),
		outbox.Limits{},
	); !errors.Is(err, ErrEnvelopeInvalid) {
		t.Fatalf("invalid limits error = %v", err)
	}
	if _, err := (TopicResolverFunc(nil)).ResolveTopic(
		eventsourcing.Message{},
	); !errors.Is(err, ErrResolverRequired) {
		t.Fatalf("nil function error = %v", err)
	}

	message := internalMessage(t, true)
	sentinel := errors.New("resolver failed")
	tests := map[string]*EnvelopeCodec{
		"nil codec": nil,
		"nil resolver": {
			limits: outbox.DefaultLimits(),
		},
		"resolver error": {
			resolver: TopicResolverFunc(
				func(eventsourcing.Message) (string, error) {
					return "", sentinel
				},
			),
			limits: outbox.DefaultLimits(),
		},
		"control topic": mustCodec(
			t,
			FixedTopic("events\ninvalid"),
			outbox.DefaultLimits(),
		),
		"oversized envelope": mustCodec(
			t,
			FixedTopic("events"),
			outbox.Limits{
				MaxIDBytes:             1,
				MaxTopicBytes:          255,
				MaxPayloadBytes:        eventsourcing.MaxPayloadBytes,
				MaxMetadataEntries:     32,
				MaxMetadataBytes:       eventsourcing.MaxMetadataBytes + 4096,
				MaxOrderingKeyBytes:    255,
				MaxIdempotencyKeyBytes: 255,
			},
		),
	}
	for name, codec := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := codec.Encode(message); !errors.Is(
				err,
				ErrEnvelopeInvalid,
			) {
				t.Fatalf("Encode error = %v", err)
			}
		})
	}
}

func TestEnvelopeCodecContainsTopicResolverPanic(t *testing.T) {
	t.Parallel()

	codec := mustCodec(
		t,
		TopicResolverFunc(func(eventsourcing.Message) (string, error) {
			panic("sensitive panic value")
		}),
		outbox.DefaultLimits(),
	)
	if _, err := codec.Encode(internalMessage(t, false)); !errors.Is(
		err,
		ErrResolverPanic,
	) {
		t.Fatalf("Encode error = %v, want ErrResolverPanic", err)
	}
}

func TestEnvelopeCodecRedactsTopicResolverError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sensitive resolver diagnostic")
	codec := mustCodec(
		t,
		TopicResolverFunc(func(eventsourcing.Message) (string, error) {
			return "", sentinel
		}),
		outbox.DefaultLimits(),
	)
	_, err := codec.Encode(internalMessage(t, false))
	if !errors.Is(err, ErrEnvelopeInvalid) ||
		!errors.Is(err, sentinel) ||
		strings.Contains(err.Error(), sentinel.Error()) {
		t.Fatalf("Encode error = %v", err)
	}
}

func TestEnvelopeCodecDecodesAbsentOptionalFields(t *testing.T) {
	t.Parallel()

	codec := mustCodec(
		t,
		FixedTopic("events"),
		outbox.DefaultLimits(),
	)
	message := internalMessage(t, false)
	envelope, err := codec.Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(message) {
		t.Fatalf("decoded = %s, want %s", decoded, message)
	}
}

func TestEnvelopeCodecHashesOversizedAggregateOrderingKey(t *testing.T) {
	t.Parallel()

	codec := mustCodec(t, FixedTopic("events"), DefaultLimits())
	stream, err := eventsourcing.NewStreamID(
		"account",
		strings.Repeat("a", eventsourcing.MaxAggregateIDBytes),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, fixture := internalPending(t)
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         fixture.ID().String(),
			Stream:     stream,
			Event:      fixture.Event(),
			RecordedAt: fixture.RecordedAt(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	envelope, err := codec.Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.OrderingKey == stream.AggregateID() ||
		len(envelope.OrderingKey) > DefaultLimits().MaxOrderingKeyBytes {
		t.Fatalf("ordering key = %q", envelope.OrderingKey)
	}
	decoded, err := codec.Decode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(message) {
		t.Fatalf("decoded = %s, want %s", decoded, message)
	}
}

func TestEnvelopeCodecRejectsMalformedEnvelopeFields(t *testing.T) {
	t.Parallel()

	codec := mustCodec(
		t,
		FixedTopic("events"),
		outbox.DefaultLimits(),
	)
	valid, err := codec.Encode(internalMessage(t, true))
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*outbox.Envelope){
		"invalid insert envelope": func(value *outbox.Envelope) {
			value.ID = ""
		},
		"delivery mode": func(value *outbox.Envelope) {
			value.Metadata[MetadataDeliveryMode] = "replay"
		},
		"unknown reserved metadata": func(value *outbox.Envelope) {
			value.Metadata["es.unknown"] = "value"
		},
		"stream version": func(value *outbox.Envelope) {
			value.Metadata[MetadataStreamVersion] = "invalid"
		},
		"schema version": func(value *outbox.Envelope) {
			value.Metadata[MetadataEventSchemaVersion] = "4294967296"
		},
		"global position": func(value *outbox.Envelope) {
			value.Metadata[MetadataGlobalPosition] = "0"
		},
		"recorded time": func(value *outbox.Envelope) {
			value.Metadata[MetadataRecordedAt] = time.Time{}.Format(
				time.RFC3339Nano,
			)
		},
		"application metadata": func(value *outbox.Envelope) {
			value.Metadata[MetadataApplication] = "[]"
		},
		"stream identity": func(value *outbox.Envelope) {
			value.Metadata[MetadataAggregateType] = ""
		},
		"event identity": func(value *outbox.Envelope) {
			value.Metadata[MetadataEventName] = ""
		},
		"pending identity": func(value *outbox.Envelope) {
			value.Metadata[MetadataCorrelationID] = "invalid value"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			envelope := cloneEnvelope(valid)
			mutate(&envelope)
			if _, err := codec.Decode(envelope); !errors.Is(
				err,
				ErrEnvelopeCorrupt,
			) {
				t.Fatalf("Decode error = %v", err)
			}
		})
	}

	var nilCodec *EnvelopeCodec
	if _, err := nilCodec.Decode(valid); !errors.Is(
		err,
		ErrEnvelopeCorrupt,
	) {
		t.Fatalf("nil codec error = %v", err)
	}
}

func TestTopicValidationRejectsInvalidUTF8AndBounds(t *testing.T) {
	t.Parallel()

	if validTopic(string([]byte{0xff}), 255) ||
		validTopic(strings.Repeat("a", 2), 1) ||
		validTopic("", 255) {
		t.Fatal("invalid topic accepted")
	}
}

func mustCodec(
	t testing.TB,
	resolver TopicResolver,
	limits outbox.Limits,
) *EnvelopeCodec {
	t.Helper()

	codec, err := NewEnvelopeCodec(resolver, limits)
	if err != nil {
		t.Fatal(err)
	}

	return codec
}

func internalMessage(
	t testing.TB,
	withOptionalFields bool,
) eventsourcing.Message {
	t.Helper()

	stream, pending := internalPending(t)
	input := eventsourcing.PendingMessageInput{
		ID:         pending.ID().String(),
		Stream:     stream,
		Event:      pending.Event(),
		RecordedAt: pending.RecordedAt(),
	}
	position := eventsourcing.GlobalPosition(0)
	if withOptionalFields {
		input.Metadata = map[string]string{"source": "test"}
		input.CorrelationID = "correlation-1"
		input.CausationID = "causation-1"
		input.Tenant = "tenant-1"
		input.Partition = "partition-1"
		position = 19
	}
	pending, err := eventsourcing.NewPendingMessage(input)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  7,
		GlobalPosition: position,
	})
	if err != nil {
		t.Fatal(err)
	}

	return message
}

func cloneEnvelope(value outbox.Envelope) outbox.Envelope {
	value.Payload = append([]byte(nil), value.Payload...)
	metadata := value.Metadata
	value.Metadata = make(map[string]string, len(metadata))
	for key, metadataValue := range metadata {
		value.Metadata[key] = metadataValue
	}

	return value
}
