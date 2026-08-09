package cloudevents

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestJavaScriptSDKJSONAndBatchInteroperability(t *testing.T) {
	t.Parallel()
	javascriptFixture := readInteropFixture(t, "testdata/interoperability/javascript-event.json")
	fromJavaScript, err := DecodeJSON(bytes.TrimSpace(javascriptFixture), DefaultLimits())
	if err != nil {
		t.Fatalf("DecodeJSON(JavaScript SDK event) error = %v", err)
	}
	assertJavaScriptInteropContext(t, fromJavaScript, "javascript-1", "/javascript", "com.example.javascript")

	golibEvent := newJavaScriptInteropEvent(t)
	encoded, err := EncodeJSON(golibEvent)
	if err != nil {
		t.Fatalf("EncodeJSON() error = %v", err)
	}
	wantEvent := bytes.TrimSpace(readInteropFixture(t, "testdata/interoperability/golib-event.json"))
	if !bytes.Equal(encoded, wantEvent) {
		t.Fatalf("EncodeJSON() = %s, want JavaScript fixture %s", encoded, wantEvent)
	}

	batch, err := EncodeJSONBatch([]Event{golibEvent})
	if err != nil {
		t.Fatalf("EncodeJSONBatch() error = %v", err)
	}
	wantBatch := bytes.TrimSpace(readInteropFixture(t, "testdata/interoperability/golib-batch.json"))
	if !bytes.Equal(batch, wantBatch) {
		t.Fatalf("EncodeJSONBatch() = %s, want JavaScript fixture %s", batch, wantBatch)
	}
	decodedBatch, err := DecodeJSONBatch(wantBatch, DefaultLimits())
	if err != nil || len(decodedBatch) != 1 {
		t.Fatalf("DecodeJSONBatch(JavaScript SDK batch) = %d events, %v", len(decodedBatch), err)
	}
}

func TestJavaScriptSDKBinaryTransportInteroperability(t *testing.T) {
	t.Parallel()
	event := newJavaScriptInteropEvent(t)

	var wantHTTP struct {
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	decodeInteropFixture(t, "testdata/interoperability/golib-http-binary.json", &wantHTTP)
	header, body, err := EncodeHTTP([]Event{event}, BinaryMode)
	if err != nil {
		t.Fatalf("EncodeHTTP() error = %v", err)
	}
	if string(body) != wantHTTP.Body || !equalHTTPFixtureHeaders(header, wantHTTP.Headers) {
		t.Fatalf("EncodeHTTP() = headers %v, body %q; want %v, %q", header, body, wantHTTP.Headers, wantHTTP.Body)
	}

	var wantKafka struct {
		Key     string            `json:"key"`
		Headers map[string]string `json:"headers"`
		Value   string            `json:"value"`
	}
	decodeInteropFixture(t, "testdata/interoperability/golib-kafka-binary.json", &wantKafka)
	record, err := EncodeKafka(event, BinaryMode, []byte(wantKafka.Key))
	if err != nil {
		t.Fatalf("EncodeKafka() error = %v", err)
	}
	gotHeaders := make(map[string]string, len(record.Headers))
	for _, item := range record.Headers {
		gotHeaders[item.Key] = string(item.Value)
	}
	if string(record.Key) != wantKafka.Key || string(record.Value) != wantKafka.Value ||
		!equalStringMaps(gotHeaders, wantKafka.Headers) {
		t.Fatalf("EncodeKafka() = key %q, headers %v, value %q; want %q, %v, %q", record.Key, gotHeaders, record.Value, wantKafka.Key, wantKafka.Headers, wantKafka.Value)
	}
}

func newJavaScriptInteropEvent(t *testing.T) Event {
	t.Helper()
	data, err := NewJSONData([]byte(`{"value":42}`))
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
	eventTime := time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)
	event, err := NewEvent(Attributes{
		ID:              "golib-js",
		Source:          "/golib",
		Type:            "com.example.golib.javascript",
		DataContentType: "application/json",
		Time:            &eventTime,
		Extensions: map[string]Attribute{
			"tenantid":     tenant,
			"traceparent":  traceParent,
			"partitionkey": partitionKey,
		},
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func assertJavaScriptInteropContext(t *testing.T, event Event, id, source, eventType string) {
	t.Helper()
	if event.ID() != id || event.Source() != source || event.Type() != eventType {
		t.Fatalf("context = id %q, source %q, type %q", event.ID(), event.Source(), event.Type())
	}
	for _, name := range []string{"tenantid", "traceparent", "partitionkey"} {
		if _, present := event.Extension(name); !present {
			t.Fatalf("extension %q is absent", name)
		}
	}
}

func readInteropFixture(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return value
}

func decodeInteropFixture(t *testing.T, path string, target any) {
	t.Helper()
	if err := json.Unmarshal(readInteropFixture(t, path), target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func equalHTTPFixtureHeaders(header http.Header, want map[string]string) bool {
	if len(header) != len(want) {
		return false
	}
	for name, value := range want {
		if header.Get(name) != value {
			return false
		}
	}
	return true
}

func equalStringMaps(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range right {
		if left[key] != value {
			return false
		}
	}
	return true
}
