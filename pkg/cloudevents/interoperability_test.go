package cloudevents

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

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

func TestOfficialGoSDKKafkaInteroperabilityInBothDirections(t *testing.T) {
	t.Parallel()

	sdkEvent := sdkevent.New()
	sdkEvent.SetID("sdk-kafka")
	sdkEvent.SetSource("/sdk")
	sdkEvent.SetType("com.example.sdk.kafka")
	if err := sdkEvent.SetData("application/json", map[string]any{"source": "sdk"}); err != nil {
		t.Fatalf("SDK SetData() error = %v", err)
	}
	producerRecord := &sarama.ProducerMessage{}
	if err := sdkkafka.WriteProducerMessage(
		sdkbinding.WithForceBinary(context.Background()),
		sdkbinding.ToMessage(&sdkEvent),
		producerRecord,
	); err != nil {
		t.Fatalf("SDK WriteProducerMessage() error = %v", err)
	}
	value, err := producerRecord.Value.Encode()
	if err != nil {
		t.Fatalf("encode SDK Kafka value: %v", err)
	}
	record := KafkaRecord{Value: value, Headers: make([]KafkaHeader, 0, len(producerRecord.Headers))}
	for _, header := range producerRecord.Headers {
		record.Headers = append(record.Headers, KafkaHeader{Key: string(header.Key), Value: header.Value})
	}
	decoded, err := DecodeKafka(record, DefaultLimits())
	if err != nil {
		t.Fatalf("DecodeKafka(SDK record) error = %v", err)
	}
	if decoded.Mode != BinaryMode || decoded.Event.ID() != "sdk-kafka" || string(decoded.Event.Data().Bytes()) != `{"source":"sdk"}` {
		t.Fatalf("decoded SDK Kafka record = mode %v, id %q, data %s", decoded.Mode, decoded.Event.ID(), decoded.Event.Data().Bytes())
	}

	data, err := NewJSONData([]byte(`{"source":"golib"}`))
	if err != nil {
		t.Fatalf("create Golib data: %v", err)
	}
	golibEvent, err := NewEvent(Attributes{
		ID:              "golib-kafka",
		Source:          "/golib",
		Type:            "com.example.golib.kafka",
		DataContentType: "application/json",
	}, data)
	if err != nil {
		t.Fatalf("create Golib event: %v", err)
	}
	golibRecord, err := EncodeKafka(golibEvent, StructuredMode, nil)
	if err != nil {
		t.Fatalf("EncodeKafka() error = %v", err)
	}
	headers := make(map[string][]byte, len(golibRecord.Headers))
	contentType := ""
	for _, header := range golibRecord.Headers {
		if header.Key == "content-type" {
			contentType = string(header.Value)
		} else {
			headers[header.Key] = header.Value
		}
	}
	sdkMessage := sdkkafka.NewMessage(golibRecord.Value, contentType, headers)
	decodedBySDK, err := sdkbinding.ToEvent(context.Background(), sdkMessage)
	if err != nil {
		t.Fatalf("SDK ToEvent() error = %v", err)
	}
	if decodedBySDK.ID() != "golib-kafka" || string(decodedBySDK.Data()) != `{"source":"golib"}` {
		t.Fatalf("SDK decoded Golib Kafka event = id %q, data %s", decodedBySDK.ID(), decodedBySDK.Data())
	}
}

func TestOfficialGoSDKHTTPInteroperabilityInBothDirections(t *testing.T) {
	t.Parallel()

	sdkEvent := sdkevent.New()
	sdkEvent.SetID("sdk-http")
	sdkEvent.SetSource("/sdk")
	sdkEvent.SetType("com.example.sdk.http")
	if err := sdkEvent.SetData("application/json", map[string]any{"source": "sdk"}); err != nil {
		t.Fatalf("SDK SetData() error = %v", err)
	}
	request := httptest.NewRequest("POST", "https://example.test/events", nil)
	if err := sdkhttp.WriteRequest(
		sdkbinding.WithForceBinary(context.Background()),
		sdkbinding.ToMessage(&sdkEvent),
		request,
	); err != nil {
		t.Fatalf("SDK WriteRequest() error = %v", err)
	}
	decoded, err := DecodeHTTP(request.Context(), request.Header, request.Body, DefaultLimits())
	if err != nil {
		t.Fatalf("DecodeHTTP(SDK request) error = %v", err)
	}
	if decoded.Mode != BinaryMode || decoded.Events[0].ID() != "sdk-http" || string(decoded.Events[0].Data().Bytes()) != `{"source":"sdk"}` {
		t.Fatalf("decoded SDK request = mode %v, id %q, data %s", decoded.Mode, decoded.Events[0].ID(), decoded.Events[0].Data().Bytes())
	}

	data, err := NewJSONData([]byte(`{"source":"golib"}`))
	if err != nil {
		t.Fatalf("create Golib data: %v", err)
	}
	golibEvent, err := NewEvent(Attributes{
		ID:              "golib-http",
		Source:          "/golib",
		Type:            "com.example.golib.http",
		DataContentType: "application/json",
	}, data)
	if err != nil {
		t.Fatalf("create Golib event: %v", err)
	}
	header, body, err := EncodeHTTP([]Event{golibEvent}, StructuredMode)
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
	if decodedBySDK.ID() != "golib-http" || string(decodedBySDK.Data()) != `{"source":"golib"}` {
		t.Fatalf("SDK decoded Golib HTTP event = id %q, data %s", decodedBySDK.ID(), decodedBySDK.Data())
	}
}
