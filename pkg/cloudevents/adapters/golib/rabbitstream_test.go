package golib_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/cloudevents"
	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	"github.com/faustbrian/golib/pkg/rabbitstream"
)

func TestRabbitStreamStructuredAdapterPreservesEventAndTransportState(t *testing.T) {
	t.Parallel()

	partitionKey, err := cloudevents.NewPartitionKeyAttribute("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	data, err := cloudevents.NewJSONData([]byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	occurredAt := time.Date(2026, 8, 22, 5, 0, 0, 123_000_000, time.UTC)
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "event-1", Source: "/source", Type: "example.created",
		DataContentType: "application/json", Time: &occurredAt,
		Extensions: map[string]cloudevents.Attribute{"partitionkey": partitionKey},
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	message, err := golib.EncodeRabbitStream(event, golib.RabbitStreamTransport{
		SuperStream: "events", CorrelationID: "correlation-1",
		Properties: []rabbitstream.MetadataEntry{{Key: "transport-attempt", Value: []byte("1")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.SuperStream != "events" || message.RoutingKey != "tenant-a" ||
		message.MessageID != "event-1" || message.CorrelationID != "correlation-1" ||
		message.ContentType != cloudevents.JSONMediaType || !message.Timestamp.Equal(occurredAt) {
		t.Fatalf("encoded RabbitStream message = %#v", message)
	}
	message.Partition = "events-0"
	message.Stream = "events-0"
	message.Offset = 42
	message.HasOffset = true

	decoded, state, err := golib.DecodeRabbitStream(message, cloudevents.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Event.ID() != event.ID() || decoded.Event.Type() != event.Type() ||
		state.SuperStream != "events" || state.Partition != "events-0" ||
		!state.HasOffset || state.Offset != 42 || len(decoded.TransportProperties) != 1 {
		t.Fatalf("decoded RabbitStream mapping = %#v, %#v", decoded, state)
	}
	message.Payload[0] = 'X'
	message.Properties[0].Value[0] = 'X'
	if bytes.Equal(decoded.Event.Data().Bytes(), message.Payload) ||
		string(decoded.TransportProperties[0].Value) != "1" {
		t.Fatal("decoded CloudEvent aliases the transport message")
	}
}

func TestRabbitStreamStructuredAdapterRejectsAmbiguousMappings(t *testing.T) {
	t.Parallel()

	partitionKey, err := cloudevents.NewPartitionKeyAttribute("event-key")
	if err != nil {
		t.Fatal(err)
	}
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "event-1", Source: "/source", Type: "example.created",
		Extensions: map[string]cloudevents.Attribute{"partitionkey": partitionKey},
	}, cloudevents.NewBinaryData([]byte("body")))
	if err != nil {
		t.Fatal(err)
	}
	for name, transport := range map[string]golib.RabbitStreamTransport{
		"missing target":    {},
		"multiple targets":  {Stream: "events", SuperStream: "events-super"},
		"routing collision": {SuperStream: "events", RoutingKey: "other-key"},
	} {
		transport := transport
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := golib.EncodeRabbitStream(event, transport); !errors.Is(err, golib.ErrMetadataCollision) {
				t.Fatalf("EncodeRabbitStream() error = %v", err)
			}
		})
	}
	if _, err := golib.EncodeRabbitStream(cloudevents.Event{}, golib.RabbitStreamTransport{Stream: "events"}); !errors.Is(err, cloudevents.ErrInvalidEvent) {
		t.Fatalf("invalid event error = %v", err)
	}

	valid, err := golib.EncodeRabbitStream(event, golib.RabbitStreamTransport{SuperStream: "events"})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]rabbitstream.Message{
		"content type": {
			Stream: "events", ContentType: "application/octet-stream", Payload: valid.Payload,
		},
		"invalid content type": {
			Stream: "events", ContentType: "not a media type", Payload: valid.Payload,
		},
		"payload": {
			Stream: "events", ContentType: cloudevents.JSONMediaType, Payload: []byte(`{}`),
		},
		"message ID": {
			Stream: "events", ContentType: cloudevents.JSONMediaType,
			Payload: valid.Payload, MessageID: "other",
		},
		"routing key": {
			Stream: "events", ContentType: cloudevents.JSONMediaType,
			Payload: valid.Payload, RoutingKey: "other",
		},
		"partition without offset": {
			Stream: "events", Partition: "events", ContentType: cloudevents.JSONMediaType,
			Payload: valid.Payload,
		},
		"offset flag without partition": {
			Stream: "events", HasOffset: true, ContentType: cloudevents.JSONMediaType,
			Payload: valid.Payload,
		},
		"offset without marker": {
			Stream: "events", Offset: 1, ContentType: cloudevents.JSONMediaType,
			Payload: valid.Payload,
		},
	}
	for name, message := range tests {
		message := message
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := golib.DecodeRabbitStream(message, cloudevents.DefaultLimits()); err == nil {
				t.Fatal("DecodeRabbitStream() accepted an ambiguous mapping")
			}
		})
	}
}

func TestRabbitStreamStructuredAdapterSupportsUnkeyedTimelessEvents(t *testing.T) {
	t.Parallel()
	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "event-1", Source: "/source", Type: "example.created",
	}, cloudevents.NewBinaryData(nil))
	if err != nil {
		t.Fatal(err)
	}
	message, err := golib.EncodeRabbitStream(event, golib.RabbitStreamTransport{Stream: "events"})
	if err != nil {
		t.Fatal(err)
	}
	if message.RoutingKey != "" || !message.Timestamp.IsZero() || message.Properties != nil {
		t.Fatalf("unkeyed message = %#v", message)
	}
	decoded, _, err := golib.DecodeRabbitStream(message, cloudevents.DefaultLimits())
	if err != nil || decoded.TransportProperties != nil {
		t.Fatalf("DecodeRabbitStream() = %#v, %v", decoded, err)
	}
	message.RoutingKey = "broker-selected-key"
	decoded, state, err := golib.DecodeRabbitStream(message, cloudevents.DefaultLimits())
	if err != nil || decoded.Event.ID() != event.ID() || state.RoutingKey != "broker-selected-key" {
		t.Fatalf("DecodeRabbitStream() with transport-only routing key = %#v, %#v, %v", decoded, state, err)
	}
}

func TestRabbitStreamStructuredAdapterBoundsTransportPropertiesBeforeCopying(t *testing.T) {
	t.Parallel()

	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "event-1", Source: "/source", Type: "example.created",
	}, cloudevents.NewBinaryData([]byte("body")))
	if err != nil {
		t.Fatal(err)
	}
	message, err := golib.EncodeRabbitStream(event, golib.RabbitStreamTransport{Stream: "events"})
	if err != nil {
		t.Fatal(err)
	}
	message.Properties = make(
		[]rabbitstream.MetadataEntry,
		rabbitstream.DefaultLimits().MaxMetadataEntries+1,
	)
	for index := range message.Properties {
		message.Properties[index] = rabbitstream.MetadataEntry{Key: "property", Value: []byte("value")}
	}

	if _, _, err := golib.DecodeRabbitStream(message, cloudevents.DefaultLimits()); !errors.Is(err, rabbitstream.ErrValidation) {
		t.Fatalf("DecodeRabbitStream() error = %v, want bounded validation", err)
	}
}

func TestRabbitStreamStructuredAdapterBoundsTransportPropertiesBeforeEncoding(t *testing.T) {
	t.Parallel()

	event, err := cloudevents.NewEvent(cloudevents.Attributes{
		ID: "event-1", Source: "/source", Type: "example.created",
	}, cloudevents.NewBinaryData([]byte("body")))
	if err != nil {
		t.Fatal(err)
	}
	properties := make(
		[]rabbitstream.MetadataEntry,
		rabbitstream.DefaultLimits().MaxMetadataEntries+1,
	)
	for index := range properties {
		properties[index] = rabbitstream.MetadataEntry{Key: "property", Value: []byte("value")}
	}

	if _, err := golib.EncodeRabbitStream(event, golib.RabbitStreamTransport{
		Stream: "events", Properties: properties,
	}); !errors.Is(err, rabbitstream.ErrValidation) {
		t.Fatalf("EncodeRabbitStream() error = %v, want bounded validation", err)
	}
}
