package eventoutbox

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/outbox"
)

func TestEnvelopeCodecCanonicalRoundTripOwnsEveryField(t *testing.T) {
	t.Parallel()

	codec := mustCodec(t, FixedTopic("account-events"), DefaultLimits())
	message := hardeningMessage(t, map[string]string{
		"zeta":  "last",
		"alpha": "first",
	})
	envelope, err := codec.Encode(message)
	if err != nil {
		t.Fatal(err)
	}

	wantMetadata := map[string]string{
		MetadataMessageID:          "message-hardening-1",
		MetadataAggregateType:      "account",
		MetadataAggregateID:        "account-hardening-1",
		MetadataStreamVersion:      "18446744073709551615",
		MetadataEventName:          "account.owner-changed",
		MetadataEventSchemaVersion: "4294967295",
		MetadataContentType:        "application/json",
		MetadataRecordedAt:         "2026-08-09T09:34:56.987654Z",
		MetadataCorrelationID:      "correlation-hardening-1",
		MetadataCausationID:        "causation-hardening-1",
		MetadataTenant:             "tenant-hardening-1",
		MetadataPartition:          "partition-hardening-1",
		MetadataGlobalPosition:     "18446744073709551615",
		MetadataApplication:        `{"alpha":"first","zeta":"last"}`,
		MetadataDeliveryMode:       "live",
	}
	if envelope.ID != "message-hardening-1" ||
		envelope.Topic != "account-events" ||
		!bytes.Equal(envelope.Payload, []byte(`{"owner":"Ada","active":true}`)) ||
		envelope.PayloadVersion != EnvelopePayloadVersion ||
		!reflect.DeepEqual(envelope.Metadata, wantMetadata) ||
		envelope.OrderingKey != "account-hardening-1" ||
		envelope.IdempotencyKey != "message-hardening-1" ||
		envelope.Attempts != 0 ||
		!envelope.AvailableAt.Equal(message.RecordedAt()) ||
		!envelope.CreatedAt.Equal(message.RecordedAt()) {
		t.Fatal("encoded envelope lost a message field")
	}

	canonical := envelope.CanonicalJSON()
	reordered := hardeningMessage(t, map[string]string{
		"alpha": "first",
		"zeta":  "last",
	})
	reorderedEnvelope, err := codec.Encode(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, reorderedEnvelope.CanonicalJSON()) {
		t.Fatal("map insertion order changed canonical envelope")
	}

	decoded, err := codec.Decode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(message) {
		t.Fatal("decoded message differs from the encoded message")
	}
	reencoded, err := codec.Encode(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded.CanonicalJSON(), canonical) {
		t.Fatal("decode/encode changed canonical form")
	}

	envelope.Payload[0] ^= 0xff
	envelope.Metadata[MetadataApplication] = `{"alpha":"changed"}`
	if !decoded.Equal(message) {
		t.Fatal("mutating the source envelope changed the decoded message")
	}
	decoded.Event().Payload()[0] ^= 0xff
	decoded.Metadata()["alpha"] = "changed"
	if !decoded.Equal(message) {
		t.Fatal("mutating returned payload or metadata changed the decoded message")
	}
}

func TestEnvelopeCodecOptionalMetadataPresenceIsCanonical(t *testing.T) {
	t.Parallel()

	codec := mustCodec(t, FixedTopic("events"), DefaultLimits())
	without, err := codec.Encode(internalMessage(t, false))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		MetadataCorrelationID,
		MetadataCausationID,
		MetadataTenant,
		MetadataPartition,
		MetadataGlobalPosition,
	} {
		if _, exists := without.Metadata[key]; exists {
			t.Fatalf("absent optional field %q was encoded", key)
		}
	}

	with, err := codec.Encode(internalMessage(t, true))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		MetadataCorrelationID:  "correlation-1",
		MetadataCausationID:    "causation-1",
		MetadataTenant:         "tenant-1",
		MetadataPartition:      "partition-1",
		MetadataGlobalPosition: "19",
	}
	for key, value := range want {
		if with.Metadata[key] != value {
			t.Fatalf("optional metadata %q = %q, want %q", key, with.Metadata[key], value)
		}
	}
}

func TestEnvelopeCodecRejectsEveryHostileEnvelopeFieldWithoutDisclosure(t *testing.T) {
	t.Parallel()

	codec := mustCodec(t, FixedTopic("events"), DefaultLimits())
	valid, err := codec.Encode(internalMessage(t, true))
	if err != nil {
		t.Fatal(err)
	}
	secret := "hostile-secret-diagnostic"
	tests := map[string]func(*outbox.Envelope){
		"id": func(value *outbox.Envelope) {
			value.ID = secret
		},
		"topic": func(value *outbox.Envelope) {
			value.Topic = ""
		},
		"payload": func(value *outbox.Envelope) {
			value.Payload = nil
		},
		"payload version": func(value *outbox.Envelope) {
			value.PayloadVersion++
		},
		"metadata": func(value *outbox.Envelope) {
			value.Metadata["es."+secret] = secret
		},
		"metadata message id": func(value *outbox.Envelope) {
			value.Metadata[MetadataMessageID] = secret
		},
		"metadata aggregate id": func(value *outbox.Envelope) {
			value.Metadata[MetadataAggregateID] = secret
		},
		"metadata content type": func(value *outbox.Envelope) {
			value.Metadata[MetadataContentType] = secret
		},
		"ordering key": func(value *outbox.Envelope) {
			value.OrderingKey = secret
		},
		"idempotency key": func(value *outbox.Envelope) {
			value.IdempotencyKey = secret
		},
		"attempts": func(value *outbox.Envelope) {
			value.Attempts = 1
		},
		"available at": func(value *outbox.Envelope) {
			value.AvailableAt = time.Time{}
		},
		"created at": func(value *outbox.Envelope) {
			value.CreatedAt = time.Time{}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			candidate := cloneEnvelope(valid)
			mutate(&candidate)
			_, err := codec.Decode(candidate)
			if !errors.Is(err, ErrEnvelopeCorrupt) {
				t.Fatalf("Decode error = %v, want ErrEnvelopeCorrupt", err)
			}
			if strings.Contains(err.Error(), secret) ||
				strings.Contains(err.Error(), string(valid.Payload)) {
				t.Fatalf("Decode disclosed hostile envelope data: %q", err)
			}
		})
	}
}

func TestEnvelopeCodecTreatsTopicAndSchedulingTimesAsTransportFields(t *testing.T) {
	t.Parallel()

	codec := mustCodec(t, FixedTopic("configured-events"), DefaultLimits())
	message := internalMessage(t, true)
	envelope, err := codec.Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	envelope.Topic = "alternate-events"
	envelope.AvailableAt = envelope.AvailableAt.Add(2 * time.Hour)
	envelope.CreatedAt = envelope.CreatedAt.Add(-2 * time.Hour)

	decoded, err := codec.Decode(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(message) {
		t.Fatal("transport-only envelope fields changed the decoded event")
	}
	canonical, err := codec.Encode(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Topic != "configured-events" ||
		!canonical.AvailableAt.Equal(message.RecordedAt()) ||
		!canonical.CreatedAt.Equal(message.RecordedAt()) {
		t.Fatal("re-encoding did not restore configured transport fields")
	}
}

func TestEnvelopeCodecHonorsEveryExactSizeBoundary(t *testing.T) {
	t.Parallel()

	message := internalMessageWithAggregateAndMetadata(
		t,
		"account-1",
		map[string]string{"source": "boundary"},
	)
	probe := mustCodec(t, FixedTopic("events"), DefaultLimits())
	envelope, err := probe.Encode(message)
	if err != nil {
		t.Fatal(err)
	}
	metadataBytes := 0
	for key, value := range envelope.Metadata {
		metadataBytes += len(key) + len(value)
	}
	exact := outbox.Limits{
		MaxIDBytes:             len(envelope.ID),
		MaxTopicBytes:          len(envelope.Topic),
		MaxPayloadBytes:        len(envelope.Payload),
		MaxMetadataEntries:     len(envelope.Metadata),
		MaxMetadataBytes:       metadataBytes,
		MaxOrderingKeyBytes:    len(envelope.OrderingKey),
		MaxIdempotencyKeyBytes: len(envelope.IdempotencyKey),
	}
	if _, err := mustCodec(t, FixedTopic("events"), exact).Encode(message); err != nil {
		t.Fatalf("exact envelope size limits rejected: %v", err)
	}

	tests := map[string]func(*outbox.Limits){
		"id":               func(value *outbox.Limits) { value.MaxIDBytes-- },
		"topic":            func(value *outbox.Limits) { value.MaxTopicBytes-- },
		"payload":          func(value *outbox.Limits) { value.MaxPayloadBytes-- },
		"metadata entries": func(value *outbox.Limits) { value.MaxMetadataEntries-- },
		"metadata bytes":   func(value *outbox.Limits) { value.MaxMetadataBytes-- },
		"ordering key":     func(value *outbox.Limits) { value.MaxOrderingKeyBytes-- },
		"idempotency key": func(value *outbox.Limits) {
			value.MaxIdempotencyKeyBytes--
		},
	}
	for name, narrow := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			limits := exact
			narrow(&limits)
			codec := mustCodec(t, FixedTopic("events"), limits)
			if _, err := codec.Encode(message); !errors.Is(err, ErrEnvelopeInvalid) {
				t.Fatalf("Encode error = %v, want ErrEnvelopeInvalid", err)
			}
		})
	}
}

func FuzzEnvelopeCodecCanonicalRoundTrip(f *testing.F) {
	f.Add(
		[]byte("identity-seed"),
		[]byte(`{"owner":"Ada"}`),
		[]byte("metadata-seed"),
		uint32(3),
		uint64(7),
		uint64(19),
		true,
	)
	f.Add([]byte{}, []byte{}, []byte{}, uint32(0), uint64(0), uint64(0), false)

	f.Fuzz(func(
		t *testing.T,
		identitySeed []byte,
		payload []byte,
		metadataSeed []byte,
		schemaVersion uint32,
		streamVersion uint64,
		globalPosition uint64,
		withOptional bool,
	) {
		identityDigest := sha256.Sum256(identitySeed)
		metadataDigest := sha256.Sum256(metadataSeed)
		identity := hex.EncodeToString(identityDigest[:12])
		metadataValue := hex.EncodeToString(metadataDigest[:])
		if len(payload) == 0 {
			payload = []byte{0}
		}
		if len(payload) > 4096 {
			payload = payload[:4096]
		}
		if schemaVersion == 0 {
			schemaVersion = 1
		}
		if streamVersion == 0 {
			streamVersion = 1
		}

		stream, err := eventsourcing.NewStreamID(
			"aggregate.a"+identity,
			"aggregate-"+identity,
		)
		if err != nil {
			t.Fatal(err)
		}
		event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
			Name:        "event.e" + identity,
			Version:     eventsourcing.SchemaVersion(schemaVersion),
			ContentType: "application/octet-stream",
			Payload:     payload,
		})
		if err != nil {
			t.Fatal(err)
		}
		recordedAt := time.Date(
			2000+int(identityDigest[0])%50,
			time.Month(1+identityDigest[1]%12),
			1+int(identityDigest[2]%28),
			int(identityDigest[3]%24),
			int(identityDigest[4]%60),
			int(identityDigest[5]%60),
			int(identityDigest[6])*1000,
			time.FixedZone("fuzz-offset", int(int8(identityDigest[7]))*60),
		)
		input := eventsourcing.PendingMessageInput{
			ID:         "message-" + identity,
			Stream:     stream,
			Event:      event,
			Metadata:   map[string]string{"zeta": metadataValue, "alpha": identity},
			RecordedAt: recordedAt,
		}
		if withOptional {
			input.CorrelationID = "correlation-" + identity
			input.CausationID = "causation-" + identity
			input.Tenant = "tenant-" + identity
			input.Partition = "partition-" + identity
		}
		pending, err := eventsourcing.NewPendingMessage(input)
		if err != nil {
			t.Fatal(err)
		}
		message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
			Pending:        pending,
			StreamVersion:  streamVersion,
			GlobalPosition: eventsourcing.GlobalPosition(globalPosition),
		})
		if err != nil {
			t.Fatal(err)
		}
		topic := "topic-" + identity
		codec := mustCodec(t, FixedTopic(topic), DefaultLimits())
		envelope, err := codec.Encode(message)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := codec.Decode(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if !decoded.Equal(message) {
			t.Fatal("decoded message differs from the encoded message")
		}
		reencoded, err := codec.Encode(decoded)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(envelope.CanonicalJSON(), reencoded.CanonicalJSON()) {
			t.Fatal("canonical round trip diverged")
		}
		if envelope.ID != message.ID().String() ||
			envelope.Topic != topic ||
			envelope.IdempotencyKey != message.ID().String() ||
			envelope.Attempts != 0 ||
			!envelope.AvailableAt.Equal(message.RecordedAt()) ||
			!envelope.CreatedAt.Equal(message.RecordedAt()) {
			t.Fatal("envelope identity or timestamps diverged")
		}
	})
}

func FuzzEnvelopeCodecEnvelopeFieldInvariants(f *testing.F) {
	codec := mustCodec(f, FixedTopic("events"), DefaultLimits())
	fixture, err := codec.Encode(internalMessage(f, true))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(
		fixture.ID,
		fixture.Topic,
		fixture.Payload,
		fixture.PayloadVersion,
		fixture.OrderingKey,
		fixture.IdempotencyKey,
		fixture.Attempts,
		fixture.AvailableAt.Unix(),
		fixture.CreatedAt.Unix(),
		uint8(0),
		fixture.Metadata[MetadataApplication],
	)
	for field := uint8(0); field < 25; field++ {
		f.Add(
			"",
			"",
			[]byte{},
			uint16(0),
			"",
			"",
			1,
			int64(^uint64(0)>>1),
			int64(^uint64(0)>>1),
			field,
			"hostile",
		)
	}

	f.Fuzz(func(
		t *testing.T,
		id string,
		topic string,
		payload []byte,
		payloadVersion uint16,
		orderingKey string,
		idempotencyKey string,
		attempts int,
		availableSeconds int64,
		createdSeconds int64,
		metadataField uint8,
		metadataValue string,
	) {
		candidate := cloneEnvelope(fixture)
		switch metadataField % 25 {
		case 0:
			candidate.ID = id
		case 1:
			candidate.Topic = topic
		case 2:
			candidate.Payload = append([]byte(nil), payload...)
		case 3:
			candidate.PayloadVersion = payloadVersion
		case 4:
			candidate.OrderingKey = orderingKey
		case 5:
			candidate.IdempotencyKey = idempotencyKey
		case 6:
			candidate.Attempts = attempts
		case 7:
			candidate.AvailableAt = time.Unix(availableSeconds, 0).UTC()
		case 8:
			candidate.CreatedAt = time.Unix(createdSeconds, 0).UTC()
		case 9:
			candidate.Metadata[MetadataApplication] = metadataValue
		case 10:
			candidate.Metadata[MetadataMessageID] = metadataValue
		case 11:
			candidate.Metadata[MetadataAggregateType] = metadataValue
		case 12:
			candidate.Metadata[MetadataAggregateID] = metadataValue
		case 13:
			candidate.Metadata[MetadataStreamVersion] = metadataValue
		case 14:
			candidate.Metadata[MetadataEventName] = metadataValue
		case 15:
			candidate.Metadata[MetadataEventSchemaVersion] = metadataValue
		case 16:
			candidate.Metadata[MetadataContentType] = metadataValue
		case 17:
			candidate.Metadata[MetadataRecordedAt] = metadataValue
		case 18:
			candidate.Metadata[MetadataCorrelationID] = metadataValue
		case 19:
			candidate.Metadata[MetadataCausationID] = metadataValue
		case 20:
			candidate.Metadata[MetadataTenant] = metadataValue
		case 21:
			candidate.Metadata[MetadataPartition] = metadataValue
		case 22:
			candidate.Metadata[MetadataGlobalPosition] = metadataValue
		case 23:
			candidate.Metadata[MetadataDeliveryMode] = metadataValue
		case 24:
			candidate.Metadata["es.hostile"] = metadataValue
		}

		decoded, err := codec.Decode(candidate)
		if err != nil {
			if !errors.Is(err, ErrEnvelopeCorrupt) ||
				err.Error() != ErrEnvelopeCorrupt.Error() {
				t.Fatalf("Decode returned a non-redacted error category: %T", err)
			}
			return
		}
		wantPayload := append([]byte(nil), decoded.Event().Payload()...)
		wantMetadata := decoded.Metadata()
		wantID := candidate.ID
		if len(candidate.Payload) > 0 {
			candidate.Payload[0] ^= 0xff
		}
		candidate.Metadata[MetadataApplication] = `{"changed":"value"}`
		if !bytes.Equal(decoded.Event().Payload(), wantPayload) ||
			!reflect.DeepEqual(decoded.Metadata(), wantMetadata) ||
			decoded.ID().String() != wantID {
			t.Fatal("successful decode did not preserve and own envelope identity or payload")
		}
		if validTopic(candidate.Topic, DefaultLimits().MaxTopicBytes) {
			roundTripCodec := mustCodec(t, FixedTopic(candidate.Topic), DefaultLimits())
			reencoded, encodeErr := roundTripCodec.Encode(decoded)
			if encodeErr != nil {
				t.Fatal("successful decode could not be re-encoded")
			}
			second, decodeErr := roundTripCodec.Decode(reencoded)
			if decodeErr != nil || !second.Equal(decoded) {
				t.Fatal("successful decode was not logically stable after re-encoding")
			}
		}
	})
}

func hardeningMessage(
	t testing.TB,
	metadata map[string]string,
) eventsourcing.Message {
	t.Helper()

	stream, err := eventsourcing.NewStreamID(
		"account",
		"account-hardening-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "account.owner-changed",
		Version:     eventsourcing.SchemaVersion(^uint32(0)),
		ContentType: "application/json",
		Payload:     []byte(`{"owner":"Ada","active":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:            "message-hardening-1",
			Stream:        stream,
			Event:         event,
			Metadata:      metadata,
			RecordedAt:    time.Date(2026, 8, 9, 12, 34, 56, 987654321, time.FixedZone("source", 3*60*60)),
			CorrelationID: "correlation-hardening-1",
			CausationID:   "causation-hardening-1",
			Tenant:        "tenant-hardening-1",
			Partition:     "partition-hardening-1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  ^uint64(0),
		GlobalPosition: eventsourcing.GlobalPosition(^uint64(0)),
	})
	if err != nil {
		t.Fatal(err)
	}

	return message
}
