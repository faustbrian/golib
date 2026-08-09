package cloudevents

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncodeKafkaBinaryPreservesKeyAndTombstoneSemantics(t *testing.T) {
	t.Parallel()

	partitionKey, err := NewStringAttribute("tenant-a")
	if err != nil {
		t.Fatalf("create partition key: %v", err)
	}
	event, err := NewEvent(Attributes{
		ID:         "evt-1",
		Source:     "/orders",
		Type:       "com.example.order.deleted",
		Extensions: map[string]Attribute{"partitionkey": partitionKey},
	}, Data{})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	key := []byte("caller-key")
	record, err := EncodeKafka(event, BinaryMode, key)
	if err != nil {
		t.Fatalf("EncodeKafka() error = %v", err)
	}
	key[0] = 'X'
	if !bytes.Equal(record.Key, []byte("caller-key")) {
		t.Fatalf("record key = %q", record.Key)
	}
	if record.Value != nil {
		t.Fatalf("absent-data record value = %v, want nil tombstone", record.Value)
	}
	if got := kafkaHeaderValue(record.Headers, "ce_partitionkey"); string(got) != "tenant-a" {
		t.Fatalf("ce_partitionkey = %q", got)
	}
	if got, ok := KafkaPartitionKey(event); !ok || string(got) != "tenant-a" {
		t.Fatalf("KafkaPartitionKey() = %q, %v", got, ok)
	}
	if _, ok := event.Extension("partitionkey"); !ok {
		t.Fatal("partition key mapper modified the event")
	}

	emptyEvent, err := NewEvent(Attributes{ID: "evt-2", Source: "/orders", Type: "empty"}, NewBinaryData(nil))
	if err != nil {
		t.Fatalf("create empty-data event: %v", err)
	}
	emptyRecord, err := EncodeKafka(emptyEvent, BinaryMode, nil)
	if err != nil {
		t.Fatalf("encode empty-data event: %v", err)
	}
	if emptyRecord.Value == nil || len(emptyRecord.Value) != 0 {
		t.Fatalf("present empty data value = %#v, want non-nil empty", emptyRecord.Value)
	}
}

func TestDecodeKafkaOfficialBinaryAndStructuredScenarios(t *testing.T) {
	t.Parallel()

	binaryRecord := KafkaRecord{
		Key:   []byte("key-1"),
		Value: []byte(`{"message":"Hello World!"}`),
		Headers: []KafkaHeader{
			{Key: "ce_specversion", Value: []byte("1.0")},
			{Key: "ce_id", Value: []byte("1234-1234-1234")},
			{Key: "ce_type", Value: []byte("com.example.someevent")},
			{Key: "ce_source", Value: []byte("/mycontext/subcontext")},
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "transport-attempt", Value: []byte("3")},
		},
	}
	binary, err := DecodeKafka(binaryRecord, DefaultLimits())
	if err != nil {
		t.Fatalf("DecodeKafka(binary) error = %v", err)
	}
	if binary.Mode != BinaryMode || binary.Event.ID() != "1234-1234-1234" || binary.Event.Data().Kind() != DataJSON {
		t.Fatalf("binary message = mode %v, id %q, data kind %v", binary.Mode, binary.Event.ID(), binary.Event.Data().Kind())
	}
	if !bytes.Equal(binary.Key, []byte("key-1")) || len(binary.TransportHeaders) != 1 || binary.TransportHeaders[0].Key != "transport-attempt" {
		t.Fatalf("binary transport metadata = key %q, headers %#v", binary.Key, binary.TransportHeaders)
	}

	structuredValue := []byte(`{"specversion":"1.0","id":"2","source":"/source","type":"example","data":null}`)
	structuredRecord := KafkaRecord{
		Value: structuredValue,
		Headers: []KafkaHeader{
			{Key: "content-type", Value: []byte("application/cloudevents+json; charset=utf-8")},
		},
	}
	structured, err := DecodeKafka(structuredRecord, DefaultLimits())
	if err != nil {
		t.Fatalf("DecodeKafka(structured) error = %v", err)
	}
	if structured.Mode != StructuredMode || structured.Event.ID() != "2" || structured.Event.Data().Kind() != DataJSON {
		t.Fatalf("structured message = mode %v, id %q, data kind %v", structured.Mode, structured.Event.ID(), structured.Event.Data().Kind())
	}

	duplicate := binaryRecord
	duplicate.Headers = append(append([]KafkaHeader(nil), binaryRecord.Headers...), KafkaHeader{Key: "ce_id", Value: []byte("other")})
	if _, err := DecodeKafka(duplicate, DefaultLimits()); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("duplicate header error = %v, want ErrInvalidEvent", err)
	}
}

func TestDecodeKafkaRejectsConflictingStructuredMetadataAndMalformedRecords(t *testing.T) {
	t.Parallel()

	value := []byte(`{"specversion":"1.0","id":"1","source":"/source","type":"example"}`)
	matching := KafkaRecord{
		Value: value,
		Headers: []KafkaHeader{
			{Key: "content-type", Value: []byte(JSONMediaType)},
			{Key: "ce_id", Value: []byte("1")},
		},
	}
	if _, err := DecodeKafka(matching, DefaultLimits()); err != nil {
		t.Fatalf("matching structured metadata error = %v", err)
	}

	conflicting := matching
	conflicting.Headers = []KafkaHeader{
		{Key: "content-type", Value: []byte(JSONMediaType)},
		{Key: "ce_id", Value: []byte("2")},
	}
	if _, err := DecodeKafka(conflicting, DefaultLimits()); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("conflicting structured metadata error = %v", err)
	}

	for _, record := range []KafkaRecord{
		{Value: value, Headers: []KafkaHeader{{Key: "content-type", Value: []byte(JSONMediaType)}, {Key: "content-type", Value: []byte(JSONMediaType)}}},
		{Value: value, Headers: []KafkaHeader{{Key: "content-type", Value: []byte(JSONBatchMediaType)}}},
		{Value: nil, Headers: []KafkaHeader{{Key: "content-type", Value: []byte(JSONMediaType)}}},
	} {
		if _, err := DecodeKafka(record, DefaultLimits()); err == nil {
			t.Fatalf("malformed record %#v error = nil", record)
		}
	}
}

func TestDecodeKafkaRejectsMissingStructuredValueAsMalformed(t *testing.T) {
	t.Parallel()

	_, err := DecodeKafka(KafkaRecord{
		Headers: []KafkaHeader{{Key: "content-type", Value: []byte(JSONMediaType)}},
	}, DefaultLimits())
	if !errors.Is(err, ErrInvalidEvent) || errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("missing structured value error = %v, want only ErrInvalidEvent", err)
	}
}

func kafkaHeaderValue(headers []KafkaHeader, name string) []byte {
	for _, header := range headers {
		if header.Key == name {
			return header.Value
		}
	}
	return nil
}
