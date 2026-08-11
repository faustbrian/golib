package cloudevents

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestStructuredFormatsPreserveDeclaredJSONPayloadBytes(t *testing.T) {
	t.Parallel()

	raw := []byte("{ \"value\" : 42 }")
	data, err := NewJSONData(raw)
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewEvent(Attributes{
		ID: "payload", Source: "/source", Type: "payload.test", DataContentType: "application/json",
	}, data)
	if err != nil {
		t.Fatal(err)
	}

	jsonValue, err := EncodeJSON(event)
	if err != nil {
		t.Fatal(err)
	}
	decodedJSON, err := DecodeJSON(jsonValue, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got := decodedJSON.Data().Bytes(); !bytes.Equal(got, raw) {
		t.Fatalf("JSON payload = %q, want exact bytes %q", got, raw)
	}

	httpHeaders, httpBody, err := EncodeHTTP([]Event{event}, StructuredMode)
	if err != nil {
		t.Fatal(err)
	}
	httpMessage, err := DecodeHTTP(context.Background(), httpHeaders, bytes.NewReader(httpBody), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got := httpMessage.Events[0].Data().Bytes(); !bytes.Equal(got, raw) {
		t.Fatalf("HTTP payload = %q, want exact bytes %q", got, raw)
	}

	kafkaRecord, err := EncodeKafka(event, StructuredMode, nil)
	if err != nil {
		t.Fatal(err)
	}
	kafkaMessage, err := DecodeKafka(kafkaRecord, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got := kafkaMessage.Event.Data().Bytes(); !bytes.Equal(got, raw) {
		t.Fatalf("Kafka payload = %q, want exact bytes %q", got, raw)
	}
}

func TestDecodeJSONBoundsUnknownExtensionValues(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxAttributeValueBytes = 3
	_, err := DecodeJSON([]byte(`{"specversion":"1.0","id":"i","source":"/s","type":"t","extra":"four"}`), limits)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("DecodeJSON() error = %v, want ErrLimitExceeded", err)
	}
}

func TestStrictEncodersRejectEveryImplicitConversionLoss(t *testing.T) {
	t.Parallel()

	uri, err := NewURIAttribute("https://example.test/extension")
	if err != nil {
		t.Fatal(err)
	}
	data, err := NewJSONData([]byte("{\"value\":42}\n"))
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewEvent(Attributes{
		ID: "loss", Source: "/source", Type: "loss.test",
		Extensions: map[string]Attribute{"custom": uri},
	}, data)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := EncodeJSON(event); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("EncodeJSON() error = %v, want ErrConversionLoss", err)
	}
	encoded, report, err := EncodeJSONWithReport(event)
	if err != nil || len(report.Losses) != 2 {
		t.Fatalf("EncodeJSONWithReport() = %s, %#v, %v", encoded, report, err)
	}
	if _, err := EncodeJSONBatch([]Event{event}); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("EncodeJSONBatch() error = %v, want ErrConversionLoss", err)
	}
	if _, batchReport, err := EncodeJSONBatchWithReport([]Event{event}); err != nil ||
		len(batchReport.Losses) != 2 || batchReport.Losses[0].Field != "events[0].data" {
		t.Fatalf("EncodeJSONBatchWithReport() = %#v, %v", batchReport, err)
	}

	if _, _, err := EncodeHTTP([]Event{event}, BinaryMode); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("EncodeHTTP() error = %v, want ErrConversionLoss", err)
	}
	header, body, report, err := EncodeHTTPWithReport([]Event{event}, BinaryMode)
	if err != nil || len(report.Losses) != 2 || header.Get("Content-Type") != "application/json" ||
		!bytes.Equal(body, []byte("{\"value\":42}\n")) {
		t.Fatalf("EncodeHTTPWithReport() = %#v, %q, %#v, %v", header, body, report, err)
	}
	if _, _, err := EncodeHTTP([]Event{event}, BatchMode); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("batch EncodeHTTP() error = %v, want ErrConversionLoss", err)
	}
	if _, _, batchReport, err := EncodeHTTPWithReport([]Event{event}, BatchMode); err != nil || len(batchReport.Losses) != 2 {
		t.Fatalf("batch EncodeHTTPWithReport() = %#v, %v", batchReport, err)
	}

	if _, err := EncodeKafka(event, BinaryMode, nil); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("EncodeKafka() error = %v, want ErrConversionLoss", err)
	}
	record, report, err := EncodeKafkaWithReport(event, BinaryMode, nil)
	if err != nil || len(report.Losses) != 2 || string(kafkaHeaderValue(record.Headers, "content-type")) != "application/json" {
		t.Fatalf("EncodeKafkaWithReport() = %#v, %#v, %v", record, report, err)
	}

	empty := newEventWithData(t, NewBinaryData([]byte{}))
	if _, _, err := EncodeHTTP([]Event{empty}, BinaryMode); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("empty EncodeHTTP() error = %v, want ErrConversionLoss", err)
	}
	header, body, report, err = EncodeHTTPWithReport([]Event{empty}, BinaryMode)
	if err != nil || len(report.Losses) != 1 || header.Get("Content-Type") != "application/octet-stream" || body == nil {
		t.Fatalf("empty EncodeHTTPWithReport() = %#v, %#v, %#v, %v", header, body, report, err)
	}
	decoded, err := DecodeHTTP(context.Background(), header, bytes.NewReader(body), DefaultLimits())
	if err != nil || !decoded.Events[0].Data().Present() || len(decoded.Events[0].Data().Bytes()) != 0 {
		t.Fatalf("empty DecodeHTTP() = %#v, %v", decoded, err)
	}
}

func TestLossAwareEncodersRemainLosslessWhenNoReportIsNeeded(t *testing.T) {
	t.Parallel()

	data, err := NewJSONData([]byte(`{"value":42}`))
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewEvent(Attributes{
		ID: "lossless", Source: "/source", Type: "lossless.test", DataContentType: "application/json",
	}, data)
	if err != nil {
		t.Fatal(err)
	}

	if _, report, err := EncodeJSONWithReport(event); err != nil || len(report.Losses) != 0 {
		t.Fatalf("EncodeJSONWithReport() report = %#v, %v", report, err)
	}
	if header, _, report, err := EncodeHTTPWithReport([]Event{event}, StructuredMode); err != nil ||
		len(report.Losses) != 0 || header.Get("Content-Type") != JSONMediaType {
		t.Fatalf("EncodeHTTPWithReport() = %#v, %#v, %v", header, report, err)
	}
	if record, report, err := EncodeKafkaWithReport(event, StructuredMode, nil); err != nil ||
		len(report.Losses) != 0 || string(kafkaHeaderValue(record.Headers, "content-type")) != JSONMediaType {
		t.Fatalf("EncodeKafkaWithReport() = %#v, %#v, %v", record, report, err)
	}

	if _, _, report, err := EncodeHTTPWithReport(nil, ContentMode(255)); !errors.Is(err, ErrUnsupportedMode) || len(report.Losses) != 0 {
		t.Fatalf("unsupported HTTP report = %#v, %v", report, err)
	}
	if _, report, err := EncodeKafkaWithReport(event, BatchMode, nil); !errors.Is(err, ErrUnsupportedMode) || len(report.Losses) != 0 {
		t.Fatalf("unsupported Kafka report = %#v, %v", report, err)
	}
	if record, report, err := EncodeKafkaWithReport(newEventWithData(t, NewBinaryData([]byte{})), BinaryMode, nil); err != nil ||
		len(report.Losses) != 1 || string(kafkaHeaderValue(record.Headers, "content-type")) != "application/octet-stream" {
		t.Fatalf("empty Kafka report = %#v, %v", report, err)
	}

	report := canonicalConversionReport(ConversionReport{Losses: []ConversionLoss{
		{Field: "same", Reason: "z"}, {Field: "same", Reason: "a"},
	}})
	if report.Losses[0].Reason != "a" {
		t.Fatalf("canonical report = %#v", report)
	}
}

func TestStrictEncodersReportAmbiguousAndConflictingDataKinds(t *testing.T) {
	t.Parallel()

	binary := newEventWithData(t, NewBinaryData([]byte(" payload ")))
	if report := jsonConversionReport(binary); len(report.Losses) != 0 {
		t.Fatalf("binary JSON report = %#v", report)
	}
	if _, _, err := EncodeHTTP([]Event{binary}, BinaryMode); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("binary HTTP error = %v, want ErrConversionLoss", err)
	}
	if _, err := EncodeKafka(binary, BinaryMode, nil); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("binary Kafka error = %v, want ErrConversionLoss", err)
	}
	for _, contentType := range []string{"application/json", "text/plain"} {
		conflictingBinary, err := NewEvent(Attributes{
			ID: "binary-conflict", Source: "/source", Type: "binary.test", DataContentType: contentType,
		}, NewBinaryData([]byte("payload")))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := EncodeHTTP([]Event{conflictingBinary}, BinaryMode); !errors.Is(err, ErrConversionLoss) {
			t.Fatalf("binary HTTP with %q error = %v, want ErrConversionLoss", contentType, err)
		}
		if _, err := EncodeKafka(conflictingBinary, BinaryMode, nil); !errors.Is(err, ErrConversionLoss) {
			t.Fatalf("binary Kafka with %q error = %v, want ErrConversionLoss", contentType, err)
		}
	}

	text, err := NewTextData("")
	if err != nil {
		t.Fatal(err)
	}
	emptyText := newEventWithData(t, text)
	if _, err := EncodeJSON(emptyText); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("empty text JSON error = %v, want ErrConversionLoss", err)
	}
	header, body, report, err := EncodeHTTPWithReport([]Event{emptyText}, BinaryMode)
	if err != nil || len(report.Losses) != 1 || header.Get("Content-Type") != "text/plain" || body == nil || len(body) != 0 {
		t.Fatalf("empty text EncodeHTTPWithReport() = %#v, %#v, %#v, %v", header, body, report, err)
	}
	httpMessage, err := DecodeHTTP(context.Background(), header, bytes.NewReader(body), DefaultLimits())
	if err != nil || httpMessage.Events[0].Data().Kind() != DataText || !httpMessage.Events[0].Data().Present() {
		t.Fatalf("empty text HTTP round trip = %#v, %v", httpMessage, err)
	}
	if _, err := EncodeKafka(emptyText, BinaryMode, nil); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("empty text Kafka error = %v, want ErrConversionLoss", err)
	}
	declaredText, err := NewEvent(Attributes{
		ID: "text", Source: "/source", Type: "text.test", DataContentType: "text/plain; charset=utf-8",
	}, text)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := EncodeHTTP([]Event{declaredText}, BinaryMode); err != nil {
		t.Fatalf("declared text HTTP error = %v", err)
	}
	if _, err := EncodeKafka(declaredText, BinaryMode, nil); err != nil {
		t.Fatalf("declared text Kafka error = %v", err)
	}
	conflictingText, err := NewEvent(Attributes{
		ID: "text-json", Source: "/source", Type: "text.test", DataContentType: "application/json",
	}, text)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeJSON(conflictingText); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("JSON-typed text error = %v, want ErrConversionLoss", err)
	}
	kafkaRecord, kafkaReport, err := EncodeKafkaWithReport(emptyText, BinaryMode, nil)
	if err != nil || len(kafkaReport.Losses) != 1 {
		t.Fatalf("empty text Kafka report = %#v, %v", kafkaReport, err)
	}
	kafkaMessage, err := DecodeKafka(kafkaRecord, DefaultLimits())
	if err != nil || kafkaMessage.Event.Data().Kind() != DataText || !kafkaMessage.Event.Data().Present() {
		t.Fatalf("empty text Kafka round trip = %#v, %v", kafkaMessage, err)
	}

	jsonData, err := NewJSONData([]byte(`{"value":42}`))
	if err != nil {
		t.Fatal(err)
	}
	conflicting, err := NewEvent(Attributes{
		ID: "conflict", Source: "/source", Type: "conflict.test", DataContentType: "text/plain",
	}, jsonData)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeJSON(conflicting); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("conflicting JSON error = %v, want ErrConversionLoss", err)
	}
	if _, _, err := EncodeHTTP([]Event{conflicting}, BinaryMode); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("conflicting HTTP error = %v, want ErrConversionLoss", err)
	}
	if _, err := EncodeKafka(conflicting, BinaryMode, nil); !errors.Is(err, ErrConversionLoss) {
		t.Fatalf("conflicting Kafka error = %v, want ErrConversionLoss", err)
	}
}

func newEventWithData(t *testing.T, data Data) Event {
	t.Helper()
	event, err := NewEvent(Attributes{ID: "data", Source: "/source", Type: "data.test"}, data)
	if err != nil {
		t.Fatal(err)
	}
	return event
}
