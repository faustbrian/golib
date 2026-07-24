package webhook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

var ErrDeliveryEncoding = errors.New("invalid webhook delivery encoding")

type deliveryWire struct {
	Version        string            `json:"version"`
	Endpoint       string            `json:"endpoint"`
	Body           []byte            `json:"body"`
	EventID        string            `json:"event_id"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Headers        http.Header       `json:"headers,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// MarshalDeliveryRequest emits deterministic v1 bytes for queue and outbox
// adapters and rejects output beyond maxBytes.
func MarshalDeliveryRequest(delivery DeliveryRequest, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 || delivery.Endpoint == nil || !delivery.Endpoint.IsAbs() ||
		delivery.Endpoint.User != nil || delivery.Endpoint.Fragment != "" || delivery.EventID == "" {
		return nil, fmt.Errorf("%w: unsafe or incomplete request", ErrDeliveryEncoding)
	}
	encoded, err := json.Marshal(deliveryWire{
		Version:        "v1",
		Endpoint:       delivery.Endpoint.String(),
		Body:           delivery.Body,
		EventID:        delivery.EventID,
		IdempotencyKey: delivery.IdempotencyKey,
		Headers:        delivery.Headers,
		Metadata:       delivery.Metadata,
	})
	if err != nil || len(encoded) > maxBytes {
		return nil, fmt.Errorf("%w: output exceeds limit or cannot be encoded", ErrDeliveryEncoding)
	}

	return encoded, nil
}

// UnmarshalDeliveryRequest bounds input before strict decoding and rejects
// unknown fields, trailing data, unsafe URLs, and incomplete identities.
func UnmarshalDeliveryRequest(encoded []byte, maxBytes int) (DeliveryRequest, error) {
	if maxBytes <= 0 || len(encoded) > maxBytes {
		return DeliveryRequest{}, fmt.Errorf("%w: input exceeds limit", ErrDeliveryEncoding)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var wire deliveryWire
	if err := decoder.Decode(&wire); err != nil {
		return DeliveryRequest{}, fmt.Errorf("%w: malformed JSON", ErrDeliveryEncoding)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return DeliveryRequest{}, err
	}
	endpoint, err := url.Parse(wire.Endpoint)
	if err != nil || wire.Version != "v1" || wire.EventID == "" || !endpoint.IsAbs() ||
		endpoint.User != nil || endpoint.Fragment != "" {
		return DeliveryRequest{}, fmt.Errorf("%w: invalid wire fields", ErrDeliveryEncoding)
	}

	return DeliveryRequest{
		Endpoint:       endpoint,
		Body:           append([]byte(nil), wire.Body...),
		EventID:        wire.EventID,
		IdempotencyKey: wire.IdempotencyKey,
		Headers:        wire.Headers.Clone(),
		Metadata:       cloneStrings(wire.Metadata),
	}, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing JSON", ErrDeliveryEncoding)
	}

	return nil
}

func cloneStrings(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}

	return clone
}
