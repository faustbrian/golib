package cloudevents

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/IBM/sarama"
	sdkkafka "github.com/cloudevents/sdk-go/protocol/kafka_sarama/v2"
	sdkbinding "github.com/cloudevents/sdk-go/v2/binding"
	sdkevent "github.com/cloudevents/sdk-go/v2/event"
	sdkhttp "github.com/cloudevents/sdk-go/v2/protocol/http"
)

func TestOfficialGoSDKJSONInteroperabilityInBothDirections(t *testing.T) {
	t.Parallel()

	fromSDK := sdkevent.New()
	fromSDK.SetID("sdk-1")
	fromSDK.SetSource("/sdk")
	fromSDK.SetType("com.example.sdk")
	fromSDK.SetExtension("tenantid", "tenant-a")
	if err := fromSDK.SetData("application/json", map[string]any{"value": 42}); err != nil {
		t.Fatalf("SDK SetData() error = %v", err)
	}
	sdkJSON, err := json.Marshal(fromSDK)
	if err != nil {
		t.Fatalf("SDK MarshalJSON() error = %v", err)
	}
	ours, err := DecodeJSON(sdkJSON, DefaultLimits())
	if err != nil {
		t.Fatalf("DecodeJSON(SDK event) error = %v", err)
	}
	if ours.ID() != "sdk-1" || ours.Data().Kind() != DataJSON || string(ours.Data().Bytes()) != `{"value":42}` {
		t.Fatalf("decoded SDK event = id %q, kind %v, data %s", ours.ID(), ours.Data().Kind(), ours.Data().Bytes())
	}

	ourEvent, err := NewEvent(Attributes{
		ID:              "golib-1",
		Source:          "/golib",
		Type:            "com.example.golib",
		DataContentType: "application/octet-stream",
	}, NewBinaryData([]byte{0x00, 0xff, 0x01}))
	if err != nil {
		t.Fatalf("create Golib event: %v", err)
	}
	ourJSON, err := EncodeJSON(ourEvent)
	if err != nil {
		t.Fatalf("EncodeJSON(Golib event) error = %v", err)
	}
	var decodedBySDK sdkevent.Event
	if err := json.Unmarshal(ourJSON, &decodedBySDK); err != nil {
		t.Fatalf("SDK UnmarshalJSON() error = %v", err)
	}
	if err := decodedBySDK.Validate(); err != nil {
		t.Fatalf("SDK validation error = %v", err)
	}
	if decodedBySDK.ID() != "golib-1" || string(decodedBySDK.Data()) != string([]byte{0x00, 0xff, 0x01}) || !decodedBySDK.DataBase64 {
		t.Fatalf("SDK decoded Golib event = id %q, data %v, base64 %v", decodedBySDK.ID(), decodedBySDK.Data(), decodedBySDK.DataBase64)
	}
}

func TestOfficialGoSDKJSONBatchInteroperabilityInBothDirections(t *testing.T) {
	t.Parallel()

	sdkEvents := []sdkevent.Event{
		newOfficialSDKEvent(t, "sdk-batch-1", "com.example.sdk.batch.first", "sdk-batch-first"),
		newOfficialSDKEvent(t, "sdk-batch-2", "com.example.sdk.batch.second", "sdk-batch-second"),
	}
	sdkBatch, err := json.Marshal(sdkEvents)
	if err != nil {
		t.Fatalf("SDK batch MarshalJSON() error = %v", err)
	}
	decodedByGolib, err := DecodeJSONBatch(sdkBatch, DefaultLimits())
	if err != nil {
		t.Fatalf("DecodeJSONBatch(SDK batch) error = %v", err)
	}
	if len(decodedByGolib) != len(sdkEvents) {
		t.Fatalf("DecodeJSONBatch(SDK batch) events = %d, want %d", len(decodedByGolib), len(sdkEvents))
	}
	assertGolibInteropEvent(t, decodedByGolib[0], StructuredMode, "sdk-batch-1", "com.example.sdk.batch.first", "sdk-batch-first")
	assertGolibInteropEvent(t, decodedByGolib[1], StructuredMode, "sdk-batch-2", "com.example.sdk.batch.second", "sdk-batch-second")

	golibEvents := []Event{
		newGolibSDKInteropEvent(t, "golib-batch-1", "com.example.golib.batch.first", "golib-batch-first"),
		newGolibSDKInteropEvent(t, "golib-batch-2", "com.example.golib.batch.second", "golib-batch-second"),
	}
	golibBatch, err := EncodeJSONBatch(golibEvents)
	if err != nil {
		t.Fatalf("EncodeJSONBatch(Golib batch) error = %v", err)
	}
	var decodedBySDK []sdkevent.Event
	if err := json.Unmarshal(golibBatch, &decodedBySDK); err != nil {
		t.Fatalf("SDK batch UnmarshalJSON() error = %v", err)
	}
	if len(decodedBySDK) != len(golibEvents) {
		t.Fatalf("SDK decoded Golib batch events = %d, want %d", len(decodedBySDK), len(golibEvents))
	}
	for i := range decodedBySDK {
		if err := decodedBySDK[i].Validate(); err != nil {
			t.Fatalf("SDK decoded Golib batch event %d validation error = %v", i, err)
		}
	}
	assertOfficialSDKInteropEvent(t, decodedBySDK[0], "golib-batch-1", "com.example.golib.batch.first", "golib-batch-first")
	assertOfficialSDKInteropEvent(t, decodedBySDK[1], "golib-batch-2", "com.example.golib.batch.second", "golib-batch-second")
}

func TestOfficialGoSDKEdgeDataInteroperability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		data        any
		setData     bool
		wantKind    DataKind
		wantData    []byte
		wantPresent bool
	}{
		{name: "absent"},
		{name: "nil normalized to absent", contentType: "application/json", data: nil, setData: true},
		{name: "explicit null", contentType: "application/json", data: json.RawMessage("null"), setData: true, wantKind: DataJSON, wantData: []byte("null"), wantPresent: true},
		{name: "empty text", contentType: "text/plain; charset=utf-8", data: "", setData: true, wantKind: DataText, wantData: []byte{}, wantPresent: true},
		{name: "empty binary", contentType: "application/octet-stream", data: []byte{}, setData: true, wantKind: DataBinary, wantData: []byte{}, wantPresent: true},
		{name: "JSON parameter", contentType: "application/json; charset=utf-8", data: map[string]any{"value": 42}, setData: true, wantKind: DataJSON, wantData: []byte(`{"value":42}`), wantPresent: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			event := sdkevent.New()
			event.SetID("sdk-edge")
			event.SetSource("/sdk-edge")
			event.SetType("com.example.sdk.edge")
			if test.setData {
				if err := event.SetData(test.contentType, test.data); err != nil {
					t.Fatalf("SDK SetData() error = %v", err)
				}
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatalf("SDK MarshalJSON() error = %v", err)
			}
			decoded, err := DecodeJSON(encoded, DefaultLimits())
			if err != nil {
				t.Fatalf("DecodeJSON(SDK edge event) error = %v", err)
			}
			if decoded.Data().Present() != test.wantPresent || decoded.Data().Kind() != test.wantKind ||
				!bytes.Equal(decoded.Data().Bytes(), test.wantData) {
				t.Fatalf("decoded SDK edge data = %#v", decoded.Data())
			}
		})
	}
}

func TestOfficialGoSDKKafkaInteroperabilityInBothDirections(t *testing.T) {
	t.Parallel()

	for _, mode := range []ContentMode{BinaryMode, StructuredMode} {
		mode := mode
		t.Run(contentModeName(mode), func(t *testing.T) {
			t.Parallel()
			sdkEvent := newOfficialSDKEvent(t, "sdk-kafka", "com.example.sdk.kafka", "sdk")
			producerRecord := &sarama.ProducerMessage{}
			ctx := sdkbinding.WithForceBinary(context.Background())
			if mode == StructuredMode {
				ctx = sdkbinding.WithForceStructured(context.Background())
			}
			if err := sdkkafka.WriteProducerMessage(ctx, sdkbinding.ToMessage(&sdkEvent), producerRecord); err != nil {
				t.Fatalf("SDK WriteProducerMessage() error = %v", err)
			}
			value, err := producerRecord.Value.Encode()
			if err != nil {
				t.Fatalf("encode SDK Kafka value: %v", err)
			}
			key, err := producerRecord.Key.Encode()
			if err != nil || string(key) != "partition-a" {
				t.Fatalf("SDK Kafka key = %q, %v; want partition-a", key, err)
			}
			record := KafkaRecord{Key: key, Value: value, Headers: make([]KafkaHeader, 0, len(producerRecord.Headers))}
			for _, header := range producerRecord.Headers {
				record.Headers = append(record.Headers, KafkaHeader{Key: string(header.Key), Value: header.Value})
			}
			decoded, err := DecodeKafka(record, DefaultLimits())
			if err != nil {
				t.Fatalf("DecodeKafka(SDK record) error = %v", err)
			}
			if decoded.Mode != mode {
				t.Fatalf("DecodeKafka(SDK record) mode = %v, want %v", decoded.Mode, mode)
			}
			if string(decoded.Key) != "partition-a" {
				t.Fatalf("Golib decoded SDK Kafka key = %q, want partition-a", decoded.Key)
			}
			assertGolibInteropEvent(t, decoded.Event, mode, "sdk-kafka", "com.example.sdk.kafka", "sdk")

			golibRecord, err := EncodeKafka(newGolibSDKInteropEvent(t, "golib-kafka", "com.example.golib.kafka", "golib"), mode, []byte("partition-a"))
			if err != nil {
				t.Fatalf("EncodeKafka() error = %v", err)
			}
			if string(golibRecord.Key) != "partition-a" {
				t.Fatalf("Golib Kafka key = %q, want partition-a", golibRecord.Key)
			}
			headers := make(map[string][]byte, len(golibRecord.Headers))
			contentType := ""
			for _, header := range golibRecord.Headers {
				if header.Key == "content-type" {
					contentType = string(header.Value)
				}
				headers[header.Key] = header.Value
			}
			sdkMessage := sdkkafka.NewMessage(golibRecord.Value, contentType, headers)
			decodedBySDK, err := sdkbinding.ToEvent(context.Background(), sdkMessage)
			if err != nil {
				t.Fatalf("SDK ToEvent() error = %v", err)
			}
			// sdk-go's inbound Kafka Message models CloudEvents metadata and data,
			// not the transport key; partitionkey remains the portable event hint.
			assertOfficialSDKInteropEvent(t, *decodedBySDK, "golib-kafka", "com.example.golib.kafka", "golib")
		})
	}
}

func TestOfficialGoSDKHTTPInteroperabilityInBothDirections(t *testing.T) {
	t.Parallel()

	for _, mode := range []ContentMode{BinaryMode, StructuredMode} {
		mode := mode
		t.Run(contentModeName(mode), func(t *testing.T) {
			t.Parallel()
			sdkEvent := newOfficialSDKEvent(t, "sdk-http", "com.example.sdk.http", "sdk")
			request := httptest.NewRequest("POST", "https://example.test/events", nil)
			ctx := sdkbinding.WithForceBinary(context.Background())
			if mode == StructuredMode {
				ctx = sdkbinding.WithForceStructured(context.Background())
			}
			if err := sdkhttp.WriteRequest(ctx, sdkbinding.ToMessage(&sdkEvent), request); err != nil {
				t.Fatalf("SDK WriteRequest() error = %v", err)
			}
			decoded, err := DecodeHTTP(request.Context(), request.Header, request.Body, DefaultLimits())
			if err != nil {
				t.Fatalf("DecodeHTTP(SDK request) error = %v", err)
			}
			if decoded.Mode != mode || len(decoded.Events) != 1 {
				t.Fatalf("DecodeHTTP(SDK request) = mode %v, events %d; want mode %v", decoded.Mode, len(decoded.Events), mode)
			}
			assertGolibInteropEvent(t, decoded.Events[0], mode, "sdk-http", "com.example.sdk.http", "sdk")

			header, body, err := EncodeHTTP([]Event{newGolibSDKInteropEvent(t, "golib-http", "com.example.golib.http", "golib")}, mode)
			if err != nil {
				t.Fatalf("EncodeHTTP() error = %v", err)
			}
			sdkMessage := sdkhttp.NewMessage(header, io.NopCloser(bytes.NewReader(body)))
			decodedBySDK, err := sdkbinding.ToEvent(context.Background(), sdkMessage)
			if err != nil {
				t.Fatalf("SDK ToEvent() error = %v", err)
			}
			if err := sdkMessage.Finish(nil); err != nil {
				t.Fatalf("SDK message finish error = %v", err)
			}
			assertOfficialSDKInteropEvent(t, *decodedBySDK, "golib-http", "com.example.golib.http", "golib")
		})
	}
}

func newOfficialSDKEvent(t *testing.T, id, eventType, source string) sdkevent.Event {
	t.Helper()
	event := sdkevent.New()
	event.SetID(id)
	event.SetSource("/" + source)
	event.SetType(eventType)
	event.SetTime(time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC))
	event.SetExtension("tenantid", "tenant-a")
	event.SetExtension("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	event.SetExtension("partitionkey", "partition-a")
	event.SetExtension("opaqueext", "opaque-value")
	if err := event.SetData("application/json", map[string]any{"source": source}); err != nil {
		t.Fatalf("SDK SetData() error = %v", err)
	}
	return event
}

func newGolibSDKInteropEvent(t *testing.T, id, eventType, source string) Event {
	t.Helper()
	data, err := NewJSONData([]byte(`{"source":"` + source + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := NewStringAttribute("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	traceParent, err := NewTraceParentAttribute("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if err != nil {
		t.Fatal(err)
	}
	partitionKey, err := NewPartitionKeyAttribute("partition-a")
	if err != nil {
		t.Fatal(err)
	}
	opaque, err := NewStringAttribute("opaque-value")
	if err != nil {
		t.Fatal(err)
	}
	eventTime := time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)
	event, err := NewEvent(Attributes{
		ID: id, Source: "/" + source, Type: eventType, DataContentType: "application/json",
		Time: &eventTime,
		Extensions: map[string]Attribute{
			"tenantid": tenant, "traceparent": traceParent, "partitionkey": partitionKey,
			"opaqueext": opaque,
		},
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func assertGolibInteropEvent(t *testing.T, event Event, mode ContentMode, id, eventType, source string) {
	t.Helper()
	contentType, hasContentType := event.DataContentType()
	eventTime, hasTime := event.Time()
	wantTime := time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)
	if event.ID() != id || event.Source() != "/"+source || event.Type() != eventType ||
		!hasContentType || contentType != "application/json" || !hasTime || !eventTime.Equal(wantTime) ||
		event.Data().Kind() != DataJSON || string(event.Data().Bytes()) != `{"source":"`+source+`"}` {
		t.Fatalf("decoded event = id %q, source %q, type %q, content-type %q/%v, time %v/%v, kind %v, data %s",
			event.ID(), event.Source(), event.Type(), contentType, hasContentType, eventTime, hasTime, event.Data().Kind(), event.Data().Bytes())
	}
	for name, want := range map[string]string{"tenantid": "tenant-a", "partitionkey": "partition-a", "opaqueext": "opaque-value", "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"} {
		attribute, present := event.Extension(name)
		if !present || attribute.String() != want {
			t.Fatalf("mode %v extension %s = %q, %v", mode, name, attribute.String(), present)
		}
	}
}

func assertOfficialSDKInteropEvent(t *testing.T, event sdkevent.Event, id, eventType, source string) {
	t.Helper()
	wantTime := time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)
	if event.ID() != id || event.Source() != "/"+source || event.Type() != eventType ||
		event.DataContentType() != "application/json" || !event.Time().Equal(wantTime) ||
		string(event.Data()) != `{"source":"`+source+`"}` {
		t.Fatalf("SDK decoded event = id %q, source %q, type %q, content-type %q, time %v, data %s",
			event.ID(), event.Source(), event.Type(), event.DataContentType(), event.Time(), event.Data())
	}
	for name, want := range map[string]string{"tenantid": "tenant-a", "partitionkey": "partition-a", "opaqueext": "opaque-value", "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"} {
		if got := event.Extensions()[name]; got != want {
			t.Fatalf("SDK extension %s = %#v, want %q", name, got, want)
		}
	}
}

func contentModeName(mode ContentMode) string {
	if mode == BinaryMode {
		return "binary"
	}
	return "structured"
}
