package outbox_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
)

func FuzzEnvelopeBuilder(f *testing.F) {
	f.Add("fuzz-id", "orders.created", []byte(`{"id":1}`), "trace_id", "trace-1", "customer-1", "command-1", uint16(1))
	f.Add("", "", []byte{}, "", "", "", "", uint16(0))

	f.Fuzz(func(t *testing.T, id, topic string, payload []byte, metadataKey, metadataValue, orderingKey, idempotencyKey string, version uint16) {
		builder, err := outbox.NewEnvelopeBuilder(
			outbox.WithClock(func() time.Time { return time.Unix(1, 234_567_000) }),
			outbox.WithIDGenerator(func() (string, error) { return id, nil }),
			outbox.WithLimits(outbox.Limits{
				MaxIDBytes: 128, MaxTopicBytes: 128, MaxPayloadBytes: 4096,
				MaxMetadataEntries: 64, MaxMetadataBytes: 512,
				MaxOrderingKeyBytes: 128, MaxIdempotencyKeyBytes: 128,
			}),
		)
		if err != nil {
			t.Fatalf("create builder: %v", err)
		}
		envelope, err := builder.Build(outbox.NewEnvelopeParams{
			Topic: topic, Payload: payload, PayloadVersion: version,
			Metadata:    map[string]string{metadataKey: metadataValue},
			OrderingKey: orderingKey, IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			return
		}

		first := envelope.CanonicalJSON()
		second := envelope.CanonicalJSON()
		if !bytes.Equal(first, second) || !json.Valid(first) {
			t.Fatalf("non-canonical JSON: %q / %q", first, second)
		}
	})
}

func BenchmarkEnvelopeBuilderBuild(b *testing.B) {
	b.ReportAllocs()
	builder, err := outbox.NewEnvelopeBuilder(
		outbox.WithClock(func() time.Time { return time.Unix(1, 0) }),
		outbox.WithIDGenerator(func() (string, error) { return "benchmark-id", nil }),
	)
	if err != nil {
		b.Fatalf("create builder: %v", err)
	}
	params := outbox.NewEnvelopeParams{
		Topic: "orders.created", Payload: bytes.Repeat([]byte("x"), 1024),
		Metadata: map[string]string{"content_type": "application/json", "trace_id": "trace-1"},
	}

	for b.Loop() {
		if _, err := builder.Build(params); err != nil {
			b.Fatalf("build envelope: %v", err)
		}
	}
}

func BenchmarkEnvelopeCanonicalJSON(b *testing.B) {
	b.ReportAllocs()
	envelope := outbox.Envelope{
		ID: "benchmark-id", Topic: "orders.created", Payload: bytes.Repeat([]byte("x"), 1024),
		PayloadVersion: 1, Metadata: map[string]string{"trace_id": "trace-1"},
		AvailableAt: time.Unix(1, 0), CreatedAt: time.Unix(1, 0),
	}

	for b.Loop() {
		_ = envelope.CanonicalJSON()
	}
}
