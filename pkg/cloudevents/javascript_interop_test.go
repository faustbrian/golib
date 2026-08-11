package cloudevents

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
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

	javascriptBatch := bytes.TrimSpace(readInteropFixture(t, "testdata/interoperability/javascript-batch.json"))
	decodedBatch, err = DecodeJSONBatch(javascriptBatch, DefaultLimits())
	if err != nil || len(decodedBatch) != 1 {
		t.Fatalf("DecodeJSONBatch(JavaScript SDK output) = %d events, %v", len(decodedBatch), err)
	}
	assertJavaScriptInteropContext(t, decodedBatch[0], "javascript-1", "/javascript", "com.example.javascript")
}

func TestJavaScriptSDKEdgeDataInteroperability(t *testing.T) {
	t.Parallel()

	edges, err := DecodeJSONBatch(
		bytes.TrimSpace(readInteropFixture(t, "testdata/interoperability/javascript-edge-batch.json")),
		DefaultLimits(),
	)
	if err != nil || len(edges) != 5 {
		t.Fatalf("DecodeJSONBatch(JavaScript SDK edge batch) = %d events, %v", len(edges), err)
	}
	if edges[0].Data().Present() {
		t.Fatalf("absent JavaScript data = %#v", edges[0].Data())
	}
	if edges[1].Data().Kind() != DataJSON || string(edges[1].Data().Bytes()) != "null" {
		t.Fatalf("null JavaScript data = %#v", edges[1].Data())
	}
	if edges[2].Data().Kind() != DataText || !edges[2].Data().Present() || len(edges[2].Data().Bytes()) != 0 {
		t.Fatalf("empty text JavaScript data = %#v", edges[2].Data())
	}
	if edges[3].Data().Kind() != DataBinary || !edges[3].Data().Present() || len(edges[3].Data().Bytes()) != 0 {
		t.Fatalf("empty binary JavaScript data = %#v", edges[3].Data())
	}
	contentType, present := edges[4].DataContentType()
	if !present || contentType != "application/json; charset=utf-8" ||
		edges[4].Data().Kind() != DataJSON || string(edges[4].Data().Bytes()) != `{"value":42}` {
		t.Fatalf("parameterized JavaScript data = %#v", edges[4])
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

func TestJavaScriptSDKProducedHTTPInteroperability(t *testing.T) {
	t.Parallel()

	for _, fixture := range []struct {
		name string
		mode ContentMode
	}{
		{name: "javascript-http-binary.json", mode: BinaryMode},
		{name: "javascript-http-structured.json", mode: StructuredMode},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			var message struct {
				Headers map[string]string `json:"headers"`
				Body    string            `json:"body"`
			}
			decodeInteropFixture(t, "testdata/interoperability/"+fixture.name, &message)
			header := make(http.Header, len(message.Headers))
			for name, value := range message.Headers {
				header.Set(name, value)
			}
			decoded, err := DecodeHTTP(t.Context(), header, bytes.NewReader([]byte(message.Body)), DefaultLimits())
			if err != nil {
				t.Fatalf("DecodeHTTP(JavaScript SDK output) error = %v", err)
			}
			if decoded.Mode != fixture.mode || len(decoded.Events) != 1 {
				t.Fatalf("DecodeHTTP(JavaScript SDK output) = mode %v, events %d", decoded.Mode, len(decoded.Events))
			}
			assertJavaScriptInteropContext(t, decoded.Events[0], "javascript-1", "/javascript", "com.example.javascript")
		})
	}
}

func TestJavaScriptSDKProducedKafkaInteroperability(t *testing.T) {
	t.Parallel()

	for _, fixture := range []struct {
		name              string
		mode              ContentMode
		wantBindingReject bool
	}{
		{name: "javascript-kafka-binary.json", mode: BinaryMode, wantBindingReject: true},
		{name: "javascript-kafka-structured.json", mode: StructuredMode},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			var message struct {
				Key     string            `json:"key"`
				Headers map[string]string `json:"headers"`
				Value   string            `json:"value"`
			}
			decodeInteropFixture(t, "testdata/interoperability/"+fixture.name, &message)
			record := KafkaRecord{Key: []byte(message.Key), Value: []byte(message.Value)}
			for name, value := range message.Headers {
				record.Headers = append(record.Headers, KafkaHeader{Key: name, Value: []byte(value)})
			}
			decoded, err := DecodeKafka(record, DefaultLimits())
			if fixture.wantBindingReject {
				if !errors.Is(err, ErrInvalidEvent) || !strings.Contains(err.Error(), "ce_datacontenttype") {
					t.Fatalf("DecodeKafka(JavaScript SDK non-standard binary output) error = %v, want ErrInvalidEvent", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeKafka(JavaScript SDK output) error = %v", err)
			}
			if decoded.Mode != fixture.mode || string(decoded.Key) != "partition-a" {
				t.Fatalf("DecodeKafka(JavaScript SDK output) = mode %v, key %q", decoded.Mode, decoded.Key)
			}
			assertJavaScriptInteropContext(t, decoded.Event, "javascript-1", "/javascript", "com.example.javascript")
		})
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
	opaque, err := NewStringAttribute("opaque-value")
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
			"opaqueext":    opaque,
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
	contentType, hasContentType := event.DataContentType()
	eventTime, hasTime := event.Time()
	wantTime := time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC)
	if !hasContentType || contentType != "application/json" || !hasTime || !eventTime.Equal(wantTime) ||
		event.Data().Kind() != DataJSON || string(event.Data().Bytes()) != `{"value":42}` {
		t.Fatalf("event content = content-type %q/%v, time %v/%v, kind %v, data %s",
			contentType, hasContentType, eventTime, hasTime, event.Data().Kind(), event.Data().Bytes())
	}
	for name, want := range map[string]string{
		"tenantid": "tenant-a", "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"partitionkey": "partition-a", "opaqueext": "opaque-value",
	} {
		attribute, present := event.Extension(name)
		if !present || attribute.String() != want {
			t.Fatalf("extension %q = %q, %v; want %q", name, attribute.String(), present, want)
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
