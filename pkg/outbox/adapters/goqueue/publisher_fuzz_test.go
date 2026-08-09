package goqueue_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/goqueue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func FuzzPublisherEnvelope(f *testing.F) {
	f.Add(
		"message-id", "topic", []byte(`{"value":1}`), uint16(1),
		"ordering", "idempotency", "key", "value", "other", "metadata",
		1, int64(1), int64(2), uint8(0),
	)
	f.Add(
		"", "", []byte{}, uint16(0), "", "", "", "", "", "",
		0, int64(0), int64(0), uint8(2),
	)

	f.Fuzz(func(
		t *testing.T,
		id, topic string,
		payload []byte,
		schema uint16,
		orderingKey, idempotencyKey string,
		metadataKey, metadataValue, otherKey, otherValue string,
		attempts int,
		availableUnix, createdUnix int64,
		queueMode uint8,
	) {
		queue := &fuzzQueue{mode: queueMode % 3}
		publisher, err := goqueue.New(queue)
		if err != nil {
			t.Fatalf("create publisher: %v", err)
		}
		envelope := outbox.Envelope{
			ID: id, Topic: topic, Payload: payload, PayloadVersion: schema,
			OrderingKey: orderingKey, IdempotencyKey: idempotencyKey,
			Metadata: map[string]string{
				metadataKey: metadataValue,
				otherKey:    otherValue,
			},
			Attempts: attempts, AvailableAt: time.Unix(availableUnix, 0),
			CreatedAt: time.Unix(createdUnix, 0),
		}
		publishErr := publisher.Publish(context.Background(), envelope)
		if queue.calls == 0 {
			outcome := goqueue.OutcomeOf(publishErr)
			if outcome.Acceptance != goqueue.AcceptanceRejected ||
				outcome.Disposition != goqueue.DispositionPermanent ||
				(!errors.Is(publishErr, goqueue.ErrInvalidEnvelope) &&
					!errors.Is(publishErr, goqueue.ErrTaskTooLarge)) {
				t.Fatalf("pre-queue error/outcome = %v/%#v", publishErr, outcome)
			}
			return
		}
		if queue.mode == 0 {
			if publishErr != nil || !json.Valid(queue.payloads[0]) {
				t.Fatalf("accepted error/JSON = %v/%q", publishErr, queue.payloads[0])
			}
			envelope.Attempts++
			if duplicateErr := publisher.Publish(context.Background(), envelope); duplicateErr != nil {
				t.Fatalf("publish duplicate: %v", duplicateErr)
			}
			if !bytes.Equal(queue.payloads[0], queue.payloads[1]) {
				t.Fatalf("attempt state changed task: %q != %q", queue.payloads[0], queue.payloads[1])
			}
			return
		}
		outcome := goqueue.OutcomeOf(publishErr)
		if outcome.Acceptance != goqueue.AcceptanceUnknown ||
			outcome.Disposition != goqueue.DispositionRetryable {
			t.Fatalf("hostile queue error/outcome = %v/%#v", publishErr, outcome)
		}
	})
}

type fuzzQueue struct {
	mode     uint8
	calls    int
	payloads [][]byte
}

func (queue *fuzzQueue) Queue(message core.QueuedMessage, _ ...job.AllowOption) error {
	queue.calls++
	queue.payloads = append(queue.payloads, message.Bytes())
	switch queue.mode {
	case 1:
		return errors.New("backend disconnected")
	case 2:
		panic("hostile queue panic")
	default:
		return nil
	}
}

func BenchmarkPublisherMapping(b *testing.B) {
	b.ReportAllocs()
	publisher, err := goqueue.New(&recordingQueue{})
	if err != nil {
		b.Fatalf("create publisher: %v", err)
	}
	envelope := outbox.Envelope{
		ID: "benchmark", Topic: "topic", Payload: []byte("payload"), PayloadVersion: 1,
	}

	for b.Loop() {
		if err := publisher.Publish(context.Background(), envelope); err != nil {
			b.Fatalf("publish: %v", err)
		}
	}
}
