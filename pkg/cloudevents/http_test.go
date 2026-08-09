package cloudevents

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestEncodeHTTPBinaryMapsAttributesAndExactData(t *testing.T) {
	t.Parallel()

	note, err := NewStringAttribute("Euro € 😀")
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	event, err := NewEvent(Attributes{
		ID:              "evt-1",
		Source:          "/sensors/temperature",
		Type:            "com.example.temperature.measured",
		DataContentType: "application/octet-stream",
		Subject:         "room A\"%",
		Extensions:      map[string]Attribute{"note": note},
	}, NewBinaryData([]byte{0x00, 0xff, 0x01}))
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	header, body, err := EncodeHTTP([]Event{event}, BinaryMode)
	if err != nil {
		t.Fatalf("EncodeHTTP() error = %v", err)
	}
	wantHeaders := http.Header{
		"Ce-Id":          {"evt-1"},
		"Ce-Note":        {"Euro%20%E2%82%AC%20%F0%9F%98%80"},
		"Ce-Source":      {"/sensors/temperature"},
		"Ce-Specversion": {"1.0"},
		"Ce-Subject":     {"room%20A%22%25"},
		"Ce-Type":        {"com.example.temperature.measured"},
		"Content-Type":   {"application/octet-stream"},
	}
	if !headersEqual(header, wantHeaders) {
		t.Fatalf("headers = %#v, want %#v", header, wantHeaders)
	}
	if !bytes.Equal(body, []byte{0x00, 0xff, 0x01}) {
		t.Fatalf("body = %v", body)
	}
	body[0] = 0xff
	if bytes.Equal(event.Data().Bytes(), body) {
		t.Fatal("HTTP body aliases event data")
	}
}

func TestDecodeHTTPBinaryDecodesPercentEncodingAndJSONData(t *testing.T) {
	t.Parallel()

	header := http.Header{
		"content-type":   {"application/json; charset=utf-8"},
		"ce-specversion": {"1.0"},
		"ce-id":          {"evt-2"},
		"ce-source":      {"/sensors/temperature"},
		"ce-type":        {"com.example.temperature.measured"},
		"ce-subject":     {`"room%20A%22%25"`},
		"ce-note":        {"Euro%20%E2%82%AC%20%F0%9F%98%80"},
	}
	body := []byte(`{"temperature":21.5}`)
	message, err := DecodeHTTP(context.Background(), header, bytes.NewReader(body), DefaultLimits())
	if err != nil {
		t.Fatalf("DecodeHTTP() error = %v", err)
	}
	if message.Mode != BinaryMode || len(message.Events) != 1 {
		t.Fatalf("message = mode %v, events %d", message.Mode, len(message.Events))
	}
	event := message.Events[0]
	if got, ok := event.Subject(); !ok || got != `room A"%` {
		t.Fatalf("subject = %q, %v", got, ok)
	}
	if note, ok := event.Extension("note"); !ok || note.String() != "Euro € 😀" {
		t.Fatalf("note = %q, %v", note.String(), ok)
	}
	if got, ok := event.DataContentType(); !ok || got != "application/json; charset=utf-8" {
		t.Fatalf("datacontenttype = %q, %v", got, ok)
	}
	if data := event.Data(); data.Kind() != DataJSON || !bytes.Equal(data.Bytes(), body) {
		t.Fatalf("data = kind %v, bytes %q", data.Kind(), data.Bytes())
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DecodeHTTP(cancelled, header, bytes.NewReader(body), DefaultLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled DecodeHTTP() error = %v", err)
	}
}

func TestHTTPStructuredAndBatchModesRoundTrip(t *testing.T) {
	t.Parallel()

	data, err := NewJSONData([]byte(`{"message":"Hello World!"}`))
	if err != nil {
		t.Fatalf("create data: %v", err)
	}
	event, err := NewEvent(Attributes{
		ID:              "1234-1234-1234",
		Source:          "/mycontext/subcontext",
		Type:            "com.example.someevent",
		DataContentType: "application/json",
	}, data)
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	for _, test := range []struct {
		name      string
		mode      ContentMode
		events    []Event
		mediaType string
	}{
		{name: "structured", mode: StructuredMode, events: []Event{event}, mediaType: JSONMediaType},
		{name: "batch", mode: BatchMode, events: []Event{event, event}, mediaType: JSONBatchMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			header, body, err := EncodeHTTP(test.events, test.mode)
			if err != nil {
				t.Fatalf("EncodeHTTP() error = %v", err)
			}
			if got := header.Get("Content-Type"); got != test.mediaType {
				t.Fatalf("content type = %q, want %q", got, test.mediaType)
			}
			message, err := DecodeHTTP(context.Background(), header, bytes.NewReader(body), DefaultLimits())
			if err != nil {
				t.Fatalf("DecodeHTTP() error = %v", err)
			}
			if message.Mode != test.mode || len(message.Events) != len(test.events) {
				t.Fatalf("message = mode %v, events %d", message.Mode, len(message.Events))
			}
			for _, decoded := range message.Events {
				if decoded.ID() != event.ID() || !bytes.Equal(decoded.Data().Bytes(), event.Data().Bytes()) {
					t.Fatalf("decoded event = id %q, data %q", decoded.ID(), decoded.Data().Bytes())
				}
			}
		})
	}
}

func TestDecodeHTTPRejectsDuplicateAndConflictingMetadata(t *testing.T) {
	t.Parallel()

	structured := []byte(`{"specversion":"1.0","id":"1","source":"/source","type":"example"}`)
	tests := []struct {
		name   string
		header http.Header
		body   []byte
		want   error
	}{
		{
			name: "conflicting structured attribute",
			header: http.Header{
				"Content-Type": {JSONMediaType},
				"Ce-Id":        {"2"},
			},
			body: structured,
			want: ErrInvalidEvent,
		},
		{
			name: "duplicate binary attribute",
			header: http.Header{
				"Ce-Specversion": {"1.0"},
				"Ce-Id":          {"1", "1"},
				"Ce-Source":      {"/source"},
				"Ce-Type":        {"example"},
			},
			want: ErrInvalidEvent,
		},
		{
			name: "binary ce-datacontenttype",
			header: http.Header{
				"Content-Type":       {"text/plain"},
				"Ce-Specversion":     {"1.0"},
				"Ce-Id":              {"1"},
				"Ce-Source":          {"/source"},
				"Ce-Type":            {"example"},
				"Ce-Datacontenttype": {"text/plain"},
			},
			want: ErrInvalidEvent,
		},
		{
			name: "unsupported structured format",
			header: http.Header{
				"Content-Type": {"application/cloudevents+avro"},
			},
			want: ErrUnsupportedMode,
		},
		{
			name: "invalid percent encoding",
			header: http.Header{
				"Ce-Specversion": {"1.0"},
				"Ce-Id":          {"%C0%A0"},
				"Ce-Source":      {"/source"},
				"Ce-Type":        {"example"},
			},
			want: ErrInvalidEvent,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeHTTP(context.Background(), test.header, bytes.NewReader(test.body), DefaultLimits())
			if !errors.Is(err, test.want) {
				t.Fatalf("DecodeHTTP() error = %v, want %v", err, test.want)
			}
		})
	}

	matching := http.Header{
		"Content-Type": {JSONMediaType},
		"Ce-Id":        {"1"},
		"Ce-Source":    {"/source"},
	}
	if _, err := DecodeHTTP(context.Background(), matching, bytes.NewReader(structured), DefaultLimits()); err != nil {
		t.Fatalf("matching redundant metadata error = %v", err)
	}
}

func headersEqual(left, right http.Header) bool {
	if len(left) != len(right) {
		return false
	}
	for name, values := range left {
		other := right.Values(name)
		if len(values) != len(other) {
			return false
		}
		for index := range values {
			if values[index] != other[index] {
				return false
			}
		}
	}
	return true
}
