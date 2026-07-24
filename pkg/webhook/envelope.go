package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidEnvelope = errors.New("invalid webhook envelope")

// Envelope is a small protocol-independent event envelope. Its v1 JSON field
// order and UTC timestamp representation are compatibility-sensitive.
type Envelope struct {
	ID       string
	Type     string
	Source   string
	Subject  string
	Time     time.Time
	Data     json.RawMessage
	Metadata map[string]string
}

// MarshalJSON validates required fields and emits deterministic JSON.
func (e Envelope) MarshalJSON() ([]byte, error) {
	if e.ID == "" || e.Type == "" || e.Source == "" || e.Time.IsZero() || !json.Valid(e.Data) {
		return nil, fmt.Errorf("%w: id, type, source, time, and valid JSON data are required", ErrInvalidEnvelope)
	}
	wire := struct {
		SpecVersion     string            `json:"specversion"`
		ID              string            `json:"id"`
		Type            string            `json:"type"`
		Source          string            `json:"source"`
		Subject         string            `json:"subject,omitempty"`
		Time            string            `json:"time"`
		DataContentType string            `json:"datacontenttype"`
		Data            json.RawMessage   `json:"data"`
		Metadata        map[string]string `json:"metadata,omitempty"`
	}{
		SpecVersion:     "1.0",
		ID:              e.ID,
		Type:            e.Type,
		Source:          e.Source,
		Subject:         e.Subject,
		Time:            e.Time.UTC().Format(time.RFC3339Nano),
		DataContentType: "application/json",
		Data:            e.Data,
		Metadata:        e.Metadata,
	}

	return json.Marshal(wire)
}
