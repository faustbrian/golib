package cloudevents

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestEncodeKafkaCoversModesOptionalHeadersAndPartitionMapping(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)
	extension, err := NewStringAttribute("value")
	if err != nil {
		t.Fatal(err)
	}
	data, err := NewJSONData([]byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewEvent(Attributes{
		ID: "1", Source: "/source", Type: "example",
		DataSchema: "https://schemas.example/event.json", Subject: "subject", Time: &occurredAt,
		Extensions: map[string]Attribute{"custom": extension},
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	record, err := EncodeKafka(event, BinaryMode, []byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(kafkaHeaderValue(record.Headers, "content-type")) != "application/json" ||
		kafkaHeaderValue(record.Headers, "ce_dataschema") == nil ||
		kafkaHeaderValue(record.Headers, "ce_subject") == nil ||
		kafkaHeaderValue(record.Headers, "ce_time") == nil ||
		string(kafkaHeaderValue(record.Headers, "ce_custom")) != "value" {
		t.Fatalf("binary headers = %#v", record.Headers)
	}
	eventWithContentType, err := NewEvent(Attributes{
		ID: "content-type", Source: "/source", Type: "example", DataContentType: "application/problem+json",
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	record, err = EncodeKafka(eventWithContentType, BinaryMode, nil)
	if err != nil || string(kafkaHeaderValue(record.Headers, "content-type")) != "application/problem+json" {
		t.Fatalf("explicit content type record = %#v, %v", record, err)
	}
	if _, err := EncodeKafka(event, StructuredMode, nil); err != nil {
		t.Fatalf("structured EncodeKafka() error = %v", err)
	}
	if _, err := EncodeKafka(Event{}, StructuredMode, nil); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("invalid structured EncodeKafka() error = %v", err)
	}
	if _, err := EncodeKafka(event, BatchMode, nil); !errors.Is(err, ErrUnsupportedMode) {
		t.Fatalf("batch EncodeKafka() error = %v", err)
	}
	if key, present := KafkaPartitionKey(event); present || key != nil {
		t.Fatalf("absent partition key = %q, %v", key, present)
	}
	corrupted := event
	corrupted.extensions = map[string]Attribute{"partitionkey": NewBooleanAttribute(true)}
	if key, present := KafkaPartitionKey(corrupted); present || key != nil {
		t.Fatalf("non-string partition key = %q, %v", key, present)
	}
}

func TestDecodeKafkaRejectsRecordAndContentTypeBoundaries(t *testing.T) {
	t.Parallel()

	value := []byte(`{"specversion":"1.0","id":"1","source":"/source","type":"example"}`)
	tests := []struct {
		name   string
		record KafkaRecord
		limits func() Limits
		want   error
	}{
		{name: "zero event limit", limits: func() Limits { l := DefaultLimits(); l.MaxEventBytes = 0; return l }, want: ErrLimitExceeded},
		{name: "zero data limit", limits: func() Limits { l := DefaultLimits(); l.MaxDataBytes = 0; return l }, want: ErrLimitExceeded},
		{name: "invalid header name", record: KafkaRecord{Headers: []KafkaHeader{{Key: string([]byte{0xff})}}}, want: ErrInvalidEvent},
		{name: "invalid content type Unicode", record: KafkaRecord{Headers: []KafkaHeader{{Key: "content-type", Value: []byte{0xff}}}, Value: value}, want: ErrInvalidEvent},
		{name: "malformed content type", record: KafkaRecord{Headers: []KafkaHeader{{Key: "content-type", Value: []byte(";")}}, Value: value}, want: ErrInvalidEvent},
		{name: "unsupported structured format", record: KafkaRecord{Headers: []KafkaHeader{{Key: "content-type", Value: []byte("application/cloudevents+avro")}}, Value: value}, want: ErrUnsupportedMode},
		{name: "structured over event limit", record: KafkaRecord{Headers: []KafkaHeader{{Key: "content-type", Value: []byte(JSONMediaType)}}, Value: value}, limits: func() Limits { l := DefaultLimits(); l.MaxEventBytes = 1; return l }, want: ErrLimitExceeded},
		{name: "invalid structured body", record: KafkaRecord{Headers: []KafkaHeader{{Key: "content-type", Value: []byte(JSONMediaType)}}, Value: []byte("{")}, want: ErrInvalidEvent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			if test.limits != nil {
				limits = test.limits()
			}
			if _, err := DecodeKafka(test.record, limits); !errors.Is(err, test.want) {
				t.Fatalf("DecodeKafka() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestStructuredKafkaMetadataCoversOptionalAndMalformedHeaders(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 8, 9, 1, 2, 3, 0, time.UTC)
	extension, err := NewStringAttribute("value")
	if err != nil {
		t.Fatal(err)
	}
	data, err := NewJSONData([]byte("null"))
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewEvent(Attributes{
		ID: "1", Source: "/source", Type: "example", DataContentType: "application/json",
		DataSchema: "https://schemas.example/event.json", Subject: "subject", Time: &occurredAt,
		Extensions: map[string]Attribute{"custom": extension},
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeJSON(event)
	if err != nil {
		t.Fatal(err)
	}
	headers := []KafkaHeader{{Key: "content-type", Value: []byte(JSONMediaType)}}
	for name, value := range map[string]string{
		"datacontenttype": "application/json", "dataschema": "https://schemas.example/event.json",
		"subject": "subject", "time": occurredAt.Format(time.RFC3339Nano), "custom": "value",
	} {
		headers = append(headers, KafkaHeader{Key: "ce_" + name, Value: []byte(value)})
	}
	if _, err := DecodeKafka(KafkaRecord{Value: encoded, Headers: headers}, DefaultLimits()); err != nil {
		t.Fatalf("matching optional metadata error = %v", err)
	}

	base := map[string][][]byte{"id": {[]byte("1")}}
	tests := []struct {
		name    string
		headers map[string][][]byte
		limits  func() Limits
	}{
		{name: "too many", headers: base, limits: func() Limits { l := DefaultLimits(); l.MaxAttributes = 0; return l }},
		{name: "duplicate", headers: map[string][][]byte{"id": {[]byte("1"), []byte("1")}}},
		{name: "name limit", headers: base, limits: func() Limits { l := DefaultLimits(); l.MaxAttributeNameBytes = 0; return l }},
		{name: "value limit", headers: base, limits: func() Limits { l := DefaultLimits(); l.MaxAttributeValueBytes = 0; return l }},
		{name: "invalid Unicode", headers: map[string][][]byte{"id": {[]byte{0xff}}}},
		{name: "unknown", headers: map[string][][]byte{"unknown": {[]byte("value")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			if test.limits != nil {
				limits = test.limits()
			}
			if err := validateStructuredKafkaMetadata(test.headers, event, limits); err == nil {
				t.Fatal("validateStructuredKafkaMetadata() error = nil")
			}
		})
	}
}

func TestDecodeKafkaBinaryRejectsEveryMalformedHeaderAndDataBoundary(t *testing.T) {
	t.Parallel()

	base := map[string][][]byte{
		"specversion": {[]byte("1.0")}, "id": {[]byte("1")},
		"source": {[]byte("/source")}, "type": {[]byte("example")},
	}
	clone := func() map[string][][]byte {
		result := make(map[string][][]byte, len(base))
		for key, values := range base {
			result[key] = values
		}
		return result
	}
	tests := []struct {
		name        string
		value       []byte
		contentType string
		headers     map[string][][]byte
		limits      func() Limits
	}{
		{name: "reserved content type", headers: map[string][][]byte{"datacontenttype": {[]byte("text/plain")}}},
		{name: "too many", headers: base, limits: func() Limits { l := DefaultLimits(); l.MaxAttributes = 1; return l }},
		{name: "name limit", headers: base, limits: func() Limits { l := DefaultLimits(); l.MaxAttributeNameBytes = 1; return l }},
		{name: "duplicate", headers: func() map[string][][]byte { h := clone(); h["id"] = [][]byte{[]byte("1"), []byte("1")}; return h }()},
		{name: "value limit", headers: base, limits: func() Limits { l := DefaultLimits(); l.MaxAttributeValueBytes = 0; return l }},
		{name: "invalid Unicode", headers: func() map[string][][]byte { h := clone(); h["id"] = [][]byte{{0xff}}; return h }()},
		{name: "wrong spec", headers: map[string][][]byte{"specversion": {[]byte("0.3")}}},
		{name: "invalid time", headers: func() map[string][][]byte { h := clone(); h["time"] = [][]byte{[]byte("today")}; return h }()},
		{name: "invalid extension", headers: func() map[string][][]byte { h := clone(); h["custom"] = [][]byte{[]byte("bad\nvalue")}; return h }()},
		{name: "data limit", value: []byte("xx"), headers: base, limits: func() Limits { l := DefaultLimits(); l.MaxDataBytes = 1; return l }},
		{name: "invalid JSON", value: []byte("{"), contentType: "application/json", headers: base},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			if test.limits != nil {
				limits = test.limits()
			}
			if _, err := decodeKafkaBinary(test.value, test.contentType, test.headers, limits); err == nil {
				t.Fatal("decodeKafkaBinary() error = nil")
			}
		})
	}

	event, err := decodeKafkaBinary([]byte{0xff}, "application/octet-stream", base, DefaultLimits())
	if err != nil || event.Data().Kind() != DataBinary || !bytes.Equal(event.Data().Bytes(), []byte{0xff}) {
		t.Fatalf("binary data event = %#v, %v", event, err)
	}
	withExtension := clone()
	withExtension["custom"] = [][]byte{[]byte("value")}
	event, err = decodeKafkaBinary(nil, "", withExtension, DefaultLimits())
	if attribute, present := event.Extension("custom"); err != nil || !present || attribute.String() != "value" {
		t.Fatalf("extension event = %#v, %v", event, err)
	}
}

func TestKafkaAcceptsExactRecordHeaderAndDataLimits(t *testing.T) {
	t.Parallel()

	structuredValue := []byte(`{"specversion":"1.0","id":"1","source":"/","type":"x"}`)
	structuredLimits := DefaultLimits()
	structuredLimits.MaxEventBytes = int64(len(structuredValue))
	message, err := DecodeKafka(KafkaRecord{
		Value: structuredValue,
		Headers: []KafkaHeader{
			{Key: "content-type", Value: []byte(JSONMediaType)},
			{Key: "transport", Value: []byte("owned")},
		},
	}, structuredLimits)
	if err != nil || len(message.TransportHeaders) != 1 || message.TransportHeaders[0].Key != "transport" {
		t.Fatalf("exact structured record = %#v, %v", message, err)
	}

	event, err := NewEvent(Attributes{ID: "1", Source: "/", Type: "x"}, Data{})
	if err != nil {
		t.Fatal(err)
	}
	metadataLimits := DefaultLimits()
	metadataLimits.MaxAttributes = 1
	metadataLimits.MaxAttributeValueBytes = 1
	if err := validateStructuredKafkaMetadata(map[string][][]byte{"id": {[]byte("1")}}, event, metadataLimits); err != nil {
		t.Fatalf("exact structured metadata limits error = %v", err)
	}
	metadataLimits.MaxAttributeValueBytes = 0
	if err := validateStructuredKafkaMetadata(map[string][][]byte{"id": {nil}}, event, metadataLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero structured metadata value limit error = %v", err)
	}
	metadataLimits = DefaultLimits()
	metadataLimits.MaxAttributeNameBytes = 0
	if err := validateStructuredKafkaMetadata(map[string][][]byte{"": {nil}}, event, metadataLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero-only structured metadata name limit error = %v", err)
	}
	metadataLimits.MaxAttributeNameBytes = 1
	if err := validateStructuredKafkaMetadata(map[string][][]byte{"id": {[]byte("1")}}, event, metadataLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("oversized-only structured metadata name limit error = %v", err)
	}
	metadataLimits.MaxAttributeNameBytes = len("id")
	if err := validateStructuredKafkaMetadata(map[string][][]byte{"id": {[]byte("1")}}, event, metadataLimits); err != nil {
		t.Fatalf("exact structured metadata name limit error = %v", err)
	}

	binaryHeaders := map[string][][]byte{
		"specversion": {[]byte("1.0")}, "id": {[]byte("1")}, "source": {[]byte("/")}, "type": {[]byte("x")},
	}
	binaryLimits := DefaultLimits()
	binaryLimits.MaxAttributes = 4
	binaryLimits.MaxAttributeNameBytes = len("specversion")
	binaryLimits.MaxAttributeValueBytes = len("1.0")
	binaryLimits.MaxDataBytes = 1
	if _, err := decodeKafkaBinary([]byte("x"), "application/octet-stream", binaryHeaders, binaryLimits); err != nil {
		t.Fatalf("exact binary limits error = %v", err)
	}
	emptyLimits := DefaultLimits()
	emptyLimits.MaxAttributeNameBytes = 0
	if _, err := decodeKafkaBinary(nil, "", map[string][][]byte{"": {nil}}, emptyLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero binary name limit error = %v", err)
	}
	emptyLimits = DefaultLimits()
	emptyLimits.MaxAttributeValueBytes = 0
	if _, err := decodeKafkaBinary(nil, "", map[string][][]byte{"": {nil}}, emptyLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero binary value limit error = %v", err)
	}
}

func TestDecodeKafkaBoundsAllCopiedRecordMetadata(t *testing.T) {
	t.Parallel()

	base := KafkaRecord{
		Value: []byte(`{"specversion":"1.0","id":"1","source":"/","type":"x","extensionname":"value"}`),
		Headers: []KafkaHeader{
			{Key: "content-type", Value: []byte(JSONMediaType)},
			{Key: "ce_extensionname", Value: []byte("value")},
		},
	}

	nameLimits := DefaultLimits()
	nameLimits.MaxAttributeNameBytes = len("specversion")
	if _, err := DecodeKafka(base, nameLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("structured oversized attribute name error = %v", err)
	}
	zeroNameLimits := DefaultLimits()
	zeroNameLimits.MaxAttributeNameBytes = 0
	if _, err := DecodeKafka(base, zeroNameLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("structured zero attribute name limit error = %v", err)
	}

	keyLimits := DefaultLimits()
	keyLimits.MaxKafkaKeyBytes = 1
	keyRecord := base
	keyRecord.Key = []byte("ab")
	keyRecord.Headers = keyRecord.Headers[:1]
	if _, err := DecodeKafka(keyRecord, keyLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("oversized key error = %v", err)
	}

	headerCountLimits := DefaultLimits()
	headerCountLimits.MaxKafkaHeaders = 1
	if _, err := DecodeKafka(base, headerCountLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("oversized header count error = %v", err)
	}

	headerNameLimits := DefaultLimits()
	headerNameLimits.MaxKafkaHeaderNameBytes = len("content-type")
	nameRecord := base
	nameRecord.Headers = append(nameRecord.Headers[:1:1], KafkaHeader{Key: "transport-header", Value: nil})
	if _, err := DecodeKafka(nameRecord, headerNameLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("oversized transport header name error = %v", err)
	}

	headerValueLimits := DefaultLimits()
	headerValueLimits.MaxKafkaHeaderValueBytes = len(JSONMediaType)
	valueRecord := base
	valueRecord.Headers = append(valueRecord.Headers[:1:1], KafkaHeader{Key: "transport", Value: make([]byte, len(JSONMediaType)+1)})
	if _, err := DecodeKafka(valueRecord, headerValueLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("oversized transport header value error = %v", err)
	}

	exact := base
	exact.Key = []byte("ab")
	exactLimits := DefaultLimits()
	exactLimits.MaxKafkaKeyBytes = len(exact.Key)
	exactLimits.MaxKafkaHeaders = len(exact.Headers)
	exactLimits.MaxKafkaHeaderNameBytes = len("ce_extensionname")
	exactLimits.MaxKafkaHeaderValueBytes = len(JSONMediaType)
	if _, err := DecodeKafka(exact, exactLimits); err != nil {
		t.Fatalf("exact Kafka metadata limits error = %v", err)
	}

	for _, mutate := range []func(*Limits){
		func(limits *Limits) { limits.MaxKafkaKeyBytes = 0 },
		func(limits *Limits) { limits.MaxKafkaHeaders = 0 },
		func(limits *Limits) { limits.MaxKafkaHeaderNameBytes = 0 },
		func(limits *Limits) { limits.MaxKafkaHeaderValueBytes = 0 },
	} {
		zeroLimits := DefaultLimits()
		mutate(&zeroLimits)
		if _, err := DecodeKafka(KafkaRecord{}, zeroLimits); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("zero Kafka metadata limit error = %v", err)
		}
	}
}
