package cloudevents

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestEncodeJSONIsDeterministicAndNormative(t *testing.T) {
	t.Parallel()

	attempt, err := NewIntegerAttribute(2)
	if err != nil {
		t.Fatalf("create integer attribute: %v", err)
	}
	data, err := NewJSONData([]byte(" { \"order\" : \"A-123\" } \n"))
	if err != nil {
		t.Fatalf("create JSON data: %v", err)
	}
	occurredAt := time.Date(2026, time.August, 9, 1, 5, 6, 120000000, time.UTC)
	event, err := NewEvent(Attributes{
		ID:              "evt-123",
		Source:          "/orders",
		Type:            "com.example.order.created.v1",
		DataContentType: "application/json",
		Time:            &occurredAt,
		Extensions: map[string]Attribute{
			"zeta":    NewBooleanAttribute(true),
			"attempt": attempt,
			"blob":    NewBinaryAttribute([]byte{0x00, 0xff}),
		},
	}, data)
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	want := []byte(`{"attempt":2,"blob":"AP8=","data":{"order":"A-123"},"datacontenttype":"application/json","id":"evt-123","source":"/orders","specversion":"1.0","time":"2026-08-09T01:05:06.12Z","type":"com.example.order.created.v1","zeta":true}`)
	for range 10 {
		got, err := EncodeJSON(event)
		if err != nil {
			t.Fatalf("EncodeJSON() error = %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("EncodeJSON() = %s, want %s", got, want)
		}
	}
}

func TestDecodeJSONRejectsAmbiguousAndOverLimitInput(t *testing.T) {
	t.Parallel()

	valid := `{"specversion":"1.0","id":"1","source":"/source","type":"example","data":{"nested":true}}`
	tests := []struct {
		name   string
		input  string
		limits func() Limits
		want   error
	}{
		{
			name:  "duplicate top-level member",
			input: `{"specversion":"1.0","id":"1","id":"2","source":"/source","type":"example"}`,
			want:  ErrInvalidEvent,
		},
		{
			name:  "conflicting data members",
			input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","data":null,"data_base64":""}`,
			want:  ErrInvalidEvent,
		},
		{
			name:  "nested extension value",
			input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","extension":{"nested":true}}`,
			want:  ErrInvalidEvent,
		},
		{
			name:  "noncanonical base64",
			input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","data_base64":"Zh=="}`,
			want:  ErrInvalidEvent,
		},
		{
			name:  "encoded data exceeds decoded limit",
			input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","data_base64":"QUJDRA=="}`,
			limits: func() Limits {
				limits := DefaultLimits()
				limits.MaxDataBytes = 3
				return limits
			},
			want: ErrLimitExceeded,
		},
		{
			name:  "oversize malformed encoded data is rejected before decode",
			input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","data_base64":"!!!!"}`,
			limits: func() Limits {
				limits := DefaultLimits()
				limits.MaxDataBytes = 1
				return limits
			},
			want: ErrLimitExceeded,
		},
		{
			name:  "event bytes",
			input: valid,
			limits: func() Limits {
				limits := DefaultLimits()
				limits.MaxEventBytes = int64(len(valid) - 1)
				return limits
			},
			want: ErrLimitExceeded,
		},
		{
			name:  "JSON depth",
			input: `{"specversion":"1.0","id":"1","source":"/source","type":"example","data":[[[null]]]}`,
			limits: func() Limits {
				limits := DefaultLimits()
				limits.MaxDepth = 3
				return limits
			},
			want: ErrLimitExceeded,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := DefaultLimits()
			if test.limits != nil {
				limits = test.limits()
			}
			_, err := DecodeJSON([]byte(test.input), limits)
			if !errors.Is(err, test.want) {
				t.Fatalf("DecodeJSON() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDecodeJSONPreservesUnknownExtensionsAndDataPresence(t *testing.T) {
	t.Parallel()

	input := []byte(`{
        "specversion":"1.0",
        "id":"evt-456",
        "source":"/orders",
        "type":"com.example.order.archived.v1",
        "subject":null,
        "tenantid":"tenant-a",
        "attempt":2,
        "sampled":true,
        "data_base64":"AAE="
    }`)
	event, err := DecodeJSON(input, DefaultLimits())
	if err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}
	for index := range input {
		input[index] = 'X'
	}

	if _, ok := event.Subject(); ok {
		t.Fatal("null subject was not treated as absent")
	}
	for name, want := range map[string]struct {
		kind AttributeKind
		text string
	}{
		"tenantid": {kind: AttributeString, text: "tenant-a"},
		"attempt":  {kind: AttributeInteger, text: "2"},
		"sampled":  {kind: AttributeBoolean, text: "true"},
	} {
		attribute, ok := event.Extension(name)
		if !ok || attribute.Kind() != want.kind || attribute.String() != want.text {
			t.Fatalf("extension %s = kind %v, text %q, present %v", name, attribute.Kind(), attribute.String(), ok)
		}
	}
	data := event.Data()
	if !data.Present() || data.Kind() != DataBinary || !bytes.Equal(data.Bytes(), []byte{0x00, 0x01}) {
		t.Fatalf("data = present %v, kind %v, bytes %v", data.Present(), data.Kind(), data.Bytes())
	}

	encoded, err := EncodeJSON(event)
	if err != nil {
		t.Fatalf("EncodeJSON() error = %v", err)
	}
	want := []byte(`{"attempt":2,"data_base64":"AAE=","id":"evt-456","sampled":true,"source":"/orders","specversion":"1.0","tenantid":"tenant-a","type":"com.example.order.archived.v1"}`)
	if !bytes.Equal(encoded, want) {
		t.Fatalf("round-trip JSON = %s, want %s", encoded, want)
	}
}

func TestJSONBatchRoundTripAndLimits(t *testing.T) {
	t.Parallel()

	if got, err := EncodeJSONBatch(nil); err != nil || string(got) != "[]" {
		t.Fatalf("empty EncodeJSONBatch() = %s, %v", got, err)
	}

	firstData, err := NewTextData("first")
	if err != nil {
		t.Fatalf("create first data: %v", err)
	}
	first, err := NewEvent(Attributes{ID: "1", Source: "/source", Type: "one"}, firstData)
	if err != nil {
		t.Fatalf("create first event: %v", err)
	}
	secondData, err := NewJSONData([]byte("null"))
	if err != nil {
		t.Fatalf("create second data: %v", err)
	}
	second, err := NewEvent(Attributes{ID: "2", Source: "/source", Type: "two"}, secondData)
	if err != nil {
		t.Fatalf("create second event: %v", err)
	}

	encoded, err := EncodeJSONBatch([]Event{first, second})
	if err != nil {
		t.Fatalf("EncodeJSONBatch() error = %v", err)
	}
	want := `[{"data":"first","id":"1","source":"/source","specversion":"1.0","type":"one"},{"data":null,"id":"2","source":"/source","specversion":"1.0","type":"two"}]`
	if string(encoded) != want {
		t.Fatalf("EncodeJSONBatch() = %s, want %s", encoded, want)
	}

	decoded, err := DecodeJSONBatch(encoded, DefaultLimits())
	if err != nil {
		t.Fatalf("DecodeJSONBatch() error = %v", err)
	}
	if len(decoded) != 2 || decoded[0].ID() != "1" || decoded[1].ID() != "2" || decoded[1].Data().Kind() != DataJSON {
		t.Fatalf("decoded batch = %#v", decoded)
	}

	limits := DefaultLimits()
	limits.MaxBatchEvents = 1
	if _, err := DecodeJSONBatch(encoded, limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("batch limit error = %v, want ErrLimitExceeded", err)
	}
}
