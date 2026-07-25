package webhook

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestDeliveryRequestWireRoundTripIsDeterministic(t *testing.T) {
	t.Parallel()

	request := DeliveryRequest{
		Endpoint:       mustURL(t, "https://example.com/hooks?tenant=one"),
		Body:           []byte{0x00, 0xff, 'x'},
		EventID:        "event-1",
		IdempotencyKey: "idem-1",
		Headers: http.Header{
			"X-Z": {"last"},
			"X-A": {"one", "two"},
		},
		Metadata: map[string]string{"z": "last", "a": "first"},
	}
	first, err := MarshalDeliveryRequest(request, 4096)
	if err != nil {
		t.Fatalf("MarshalDeliveryRequest() error = %v", err)
	}
	second, err := MarshalDeliveryRequest(request, 4096)
	if err != nil {
		t.Fatalf("MarshalDeliveryRequest() second error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("wire bytes differ: %q != %q", first, second)
	}
	decoded, err := UnmarshalDeliveryRequest(first, 4096)
	if err != nil {
		t.Fatalf("UnmarshalDeliveryRequest() error = %v", err)
	}
	if decoded.Endpoint.String() != request.Endpoint.String() || !bytes.Equal(decoded.Body, request.Body) ||
		decoded.EventID != request.EventID || decoded.IdempotencyKey != request.IdempotencyKey ||
		decoded.Headers.Get("X-A") != "one" || decoded.Metadata["a"] != "first" {
		t.Fatalf("decoded request = %#v", decoded)
	}
}

func TestDeliveryRequestWireRejectsLimitsAndMalformedData(t *testing.T) {
	t.Parallel()

	valid := DeliveryRequest{Endpoint: mustURL(t, "https://example.com"), Body: []byte("body"), EventID: "event"}
	encoded, err := MarshalDeliveryRequest(valid, 1024)
	if err != nil {
		t.Fatalf("MarshalDeliveryRequest() error = %v", err)
	}
	if _, err := MarshalDeliveryRequest(valid, 1); !errors.Is(err, ErrDeliveryEncoding) {
		t.Fatalf("marshal limit error = %v", err)
	}
	if _, err := UnmarshalDeliveryRequest(encoded, 1); !errors.Is(err, ErrDeliveryEncoding) {
		t.Fatalf("unmarshal limit error = %v", err)
	}
	for name, value := range map[string][]byte{
		"unknown field":  []byte(`{"version":"v1","unknown":true}`),
		"trailing JSON":  append(append([]byte(nil), encoded...), []byte(` {}`)...),
		"missing fields": []byte(`{"version":"v1"}`),
		"invalid URL":    []byte(`{"version":"v1","endpoint":":","event_id":"event","body":""}`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := UnmarshalDeliveryRequest(value, 1024); !errors.Is(err, ErrDeliveryEncoding) {
				t.Fatalf("UnmarshalDeliveryRequest() error = %v, want ErrDeliveryEncoding", err)
			}
		})
	}
}

func TestDeliverOnceDisablesInternalRetries(t *testing.T) {
	t.Parallel()

	doer := &errorDoer{err: errors.New("network unavailable")}
	deliverer := deliveryFixture(t, doer, time.Unix(1_700_000_000, 0), func(_ context.Context, _ time.Duration) error { return nil })
	deliverer.retry.MaxAttempts = 5
	_, err := deliverer.DeliverOnce(context.Background(), DeliveryRequest{
		Endpoint: mustURL(t, "https://example.com"), Body: []byte("body"), EventID: "event", IdempotencyKey: "event",
	})
	if !errors.Is(err, ErrDeliveryFailed) || doer.calls != 1 {
		t.Fatalf("DeliverOnce() error = %v, calls = %d", err, doer.calls)
	}
}
