package cloudevents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// These cases are transcribed from the pinned cloudevents/conformance v0.4.1
// HTTP and Kafka feature files. docs/provenance.md records their exact hashes.
func TestPinnedOfficialConformanceFixtures(t *testing.T) {
	t.Parallel()
	fixtures := map[string]string{
		"testdata/conformance/http-protocol-binding.feature":  "71654ab1de4b363e6cb433e2c4b77f950f39659fe0182de2e50f5ffd4195fd05",
		"testdata/conformance/kafka-protocol-binding.feature": "1c8286a82a1fc458bd51147f41d5a1cc58bc294b7360d744df35421593df5285",
	}
	for path, want := range fixtures {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read official fixture %s: %v", path, err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(content)); got != want {
			t.Fatalf("official fixture %s digest = %s, want %s", path, got, want)
		}
	}
}

func TestOfficialConformanceHTTPBindingScenarios(t *testing.T) {
	t.Parallel()

	binaryBody := []byte("{\n    \"message\": \"Hello World!\"\n}")
	for _, contentType := range []string{"application/json", "application/json; charset=utf-8"} {
		t.Run("binary "+contentType, func(t *testing.T) {
			t.Parallel()
			message, err := DecodeHTTP(context.Background(), http.Header{
				"Ce-Specversion": {"1.0"},
				"Ce-Type":        {"com.example.someevent"},
				"Ce-Time":        {"2018-04-05T03:56:24Z"},
				"Ce-Id":          {"1234-1234-1234"},
				"Ce-Source":      {"/mycontext/subcontext"},
				"Content-Type":   {contentType},
			}, bytes.NewReader(binaryBody), DefaultLimits())
			if err != nil {
				t.Fatalf("DecodeHTTP() error = %v", err)
			}
			assertOfficialConformanceEvent(t, message.Events[0], contentType)
		})
	}

	structuredBody := officialStructuredConformanceEvent()
	for _, contentType := range []string{JSONMediaType, JSONMediaType + "; charset=utf-8"} {
		t.Run("structured "+contentType, func(t *testing.T) {
			t.Parallel()
			message, err := DecodeHTTP(context.Background(), http.Header{
				"Content-Type": {contentType},
			}, bytes.NewReader(structuredBody), DefaultLimits())
			if err != nil {
				t.Fatalf("DecodeHTTP() error = %v", err)
			}
			assertOfficialConformanceEvent(t, message.Events[0], "application/json")
		})
	}
}

func TestOfficialConformanceKafkaBindingScenarios(t *testing.T) {
	t.Parallel()

	binary, err := DecodeKafka(KafkaRecord{
		Value: []byte(`{"message": "Hello World!"}`),
		Headers: []KafkaHeader{
			{Key: "ce_specversion", Value: []byte("1.0")},
			{Key: "ce_id", Value: []byte("1234-1234-1234")},
			{Key: "ce_type", Value: []byte("com.example.someevent")},
			{Key: "ce_source", Value: []byte("/mycontext/subcontext")},
			{Key: "ce_time", Value: []byte("2018-04-05T03:56:24Z")},
			{Key: "content-type", Value: []byte("application/json")},
		},
	}, DefaultLimits())
	if err != nil {
		t.Fatalf("binary DecodeKafka() error = %v", err)
	}
	assertOfficialConformanceEvent(t, binary.Event, "application/json")

	for _, contentType := range []string{JSONMediaType, JSONMediaType + "; charset=utf-8"} {
		t.Run("structured "+contentType, func(t *testing.T) {
			t.Parallel()
			message, err := DecodeKafka(KafkaRecord{
				Value:   officialStructuredConformanceEvent(),
				Headers: []KafkaHeader{{Key: "content-type", Value: []byte(contentType)}},
			}, DefaultLimits())
			if err != nil {
				t.Fatalf("DecodeKafka() error = %v", err)
			}
			assertOfficialConformanceEvent(t, message.Event, "application/json")
		})
	}
}

func officialStructuredConformanceEvent() []byte {
	return []byte(`{
        "specversion": "1.0",
        "type": "com.example.someevent",
        "time": "2018-04-05T03:56:24Z",
        "id": "1234-1234-1234",
        "source": "/mycontext/subcontext",
        "datacontenttype": "application/json",
        "data": {"message": "Hello World!"}
    }`)
}

func assertOfficialConformanceEvent(t *testing.T, event Event, contentType string) {
	t.Helper()
	if event.ID() != "1234-1234-1234" || event.Type() != "com.example.someevent" ||
		event.Source() != "/mycontext/subcontext" {
		t.Fatalf("context = id %q, type %q, source %q", event.ID(), event.Type(), event.Source())
	}
	gotContentType, present := event.DataContentType()
	if !present || gotContentType != contentType {
		t.Fatalf("datacontenttype = %q, %v; want %q", gotContentType, present, contentType)
	}
	gotTime, present := event.Time()
	wantTime := time.Date(2018, time.April, 5, 3, 56, 24, 0, time.UTC)
	if !present || !gotTime.Equal(wantTime) {
		t.Fatalf("time = %v, %v; want %v", gotTime, present, wantTime)
	}
	data := event.Data()
	var compact bytes.Buffer
	if data.Kind() != DataJSON || json.Compact(&compact, data.Bytes()) != nil ||
		!bytes.Equal(compact.Bytes(), []byte(`{"message":"Hello World!"}`)) {
		t.Fatalf("data = kind %v, bytes %s", data.Kind(), data.Bytes())
	}
}
