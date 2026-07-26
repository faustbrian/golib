package gooutbox

import (
	"testing"

	"github.com/faustbrian/golib/pkg/outbox"
)

func FuzzEnvelopeCodecDecode(f *testing.F) {
	codec := mustCodec(f, FixedTopic("events"), outbox.DefaultLimits())
	envelope, err := codec.Encode(internalMessage(f, true))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(
		"account",
		"account-1",
		"7",
		"account.opened",
		"1",
		"application/json",
		"2026-07-25T12:00:00Z",
		"19",
		`{"source":"test"}`,
		[]byte(`{"owner":"Ada"}`),
	)
	f.Add("", "", "0", "", "0", "", "", "0", "[]", []byte{})

	f.Fuzz(func(
		_ *testing.T,
		aggregateType string,
		aggregateID string,
		streamVersion string,
		eventName string,
		schemaVersion string,
		contentType string,
		recordedAt string,
		globalPosition string,
		applicationMetadata string,
		payload []byte,
	) {
		candidate := cloneEnvelope(envelope)
		candidate.OrderingKey = aggregateID
		candidate.Payload = append([]byte(nil), payload...)
		candidate.Metadata[MetadataAggregateType] = aggregateType
		candidate.Metadata[MetadataAggregateID] = aggregateID
		candidate.Metadata[MetadataStreamVersion] = streamVersion
		candidate.Metadata[MetadataEventName] = eventName
		candidate.Metadata[MetadataEventSchemaVersion] = schemaVersion
		candidate.Metadata[MetadataContentType] = contentType
		candidate.Metadata[MetadataRecordedAt] = recordedAt
		candidate.Metadata[MetadataGlobalPosition] = globalPosition
		candidate.Metadata[MetadataApplication] = applicationMetadata
		_, _ = codec.Decode(candidate)
	})
}

func FuzzEnvelopeCodecEncodeTopic(f *testing.F) {
	message := internalMessage(f, false)
	f.Add("events")
	f.Add("")
	f.Add(string([]byte{0xff}))

	f.Fuzz(func(t *testing.T, topic string) {
		codec := mustCodec(t, FixedTopic(topic), outbox.DefaultLimits())
		_, _ = codec.Encode(message)
	})
}
