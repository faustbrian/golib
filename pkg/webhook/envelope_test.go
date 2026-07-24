package webhook

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestEnvelopeMarshalIsDeterministicAndPreservesData(t *testing.T) {
	t.Parallel()

	envelope := Envelope{
		ID:      "evt_123",
		Type:    "order.created",
		Source:  "urn:shop:1",
		Subject: "orders/42",
		Time:    time.Date(2026, 7, 15, 10, 11, 12, 345_000_000, time.FixedZone("EEST", 3*60*60)),
		Data:    json.RawMessage(`{"amount":10}`),
		Metadata: map[string]string{
			"z": "last",
			"a": "first",
		},
	}

	first, err := envelope.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	second, err := envelope.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() second error = %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("MarshalJSON() is nondeterministic: %q != %q", first, second)
	}
	want := `{"specversion":"1.0","id":"evt_123","type":"order.created","source":"urn:shop:1","subject":"orders/42","time":"2026-07-15T07:11:12.345Z","datacontenttype":"application/json","data":{"amount":10},"metadata":{"a":"first","z":"last"}}`
	if string(first) != want {
		t.Fatalf("MarshalJSON() = %s, want %s", first, want)
	}
}

func TestEnvelopeRejectsInvalidRequiredFieldsAndData(t *testing.T) {
	t.Parallel()

	valid := Envelope{ID: "id", Type: "type", Source: "source", Time: time.Now(), Data: json.RawMessage(`null`)}
	tests := map[string]Envelope{
		"missing ID":     func() Envelope { value := valid; value.ID = ""; return value }(),
		"missing type":   func() Envelope { value := valid; value.Type = ""; return value }(),
		"missing source": func() Envelope { value := valid; value.Source = ""; return value }(),
		"missing time":   func() Envelope { value := valid; value.Time = time.Time{}; return value }(),
		"invalid data":   func() Envelope { value := valid; value.Data = json.RawMessage(`{`); return value }(),
	}
	for name, envelope := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := envelope.MarshalJSON(); !errors.Is(err, ErrInvalidEnvelope) {
				t.Fatalf("MarshalJSON() error = %v, want ErrInvalidEnvelope", err)
			}
		})
	}
}
