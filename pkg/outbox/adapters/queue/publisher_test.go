package outboxqueue_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/queue"
	"github.com/faustbrian/golib/pkg/outbox/relay"
	firstpartyqueue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
)

func TestPublisherQueuesCanonicalEnvelope(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	envelope := outbox.Envelope{
		ID: "evt-1", Topic: "orders.created", Payload: []byte(`{"id":1}`),
		PayloadVersion: 1, Metadata: map[string]string{"b": "2", "a": "1"},
		AvailableAt: time.Unix(1, 0).UTC(), CreatedAt: time.Unix(1, 0).UTC(),
	}

	if err := publisher.Publish(context.Background(), envelope); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if queue.calls != 1 || queue.message == nil {
		t.Fatalf("queue calls/message = %d/%#v", queue.calls, queue.message)
	}
	want := `{"task_id":"evt-1","idempotency_key":"evt-1","content":"eyJpZCI6MX0=",` +
		`"content_type":"application/json","event_name":"orders.created",` +
		`"schema_version":1,"metadata":{"a":"1","b":"2"}}`
	if string(queue.message.Bytes()) != want {
		t.Fatalf("queued bytes = %s", queue.message.Bytes())
	}
	mutated := queue.message.Bytes()
	mutated[0] = '!'
	if string(queue.message.Bytes()) != want {
		t.Fatalf("caller mutation changed retained task bytes: %s", queue.message.Bytes())
	}
}

func TestPublisherFreezesCanonicalTaskWithEveryField(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	envelope := outbox.Envelope{
		ID: "evt-1", Topic: "fallback-event", Payload: []byte{0x00, 0xff},
		PayloadVersion: 7, OrderingKey: "aggregate-9", IdempotencyKey: "command-4",
		Metadata: map[string]string{
			"z": "last", "a": "first", "es.event_name": "orders.shipped",
			"es.content_type": "application/octet-stream",
		},
	}

	if err := publisher.Publish(context.Background(), envelope); err != nil {
		t.Fatalf("publish: %v", err)
	}
	want := `{"task_id":"evt-1","idempotency_key":"command-4",` +
		`"ordering_key":"aggregate-9","content":"AP8=",` +
		`"content_type":"application/octet-stream","event_name":"orders.shipped",` +
		`"schema_version":7,"metadata":{"a":"first",` +
		`"es.content_type":"application/octet-stream",` +
		`"es.event_name":"orders.shipped","z":"last"}}`
	if got := string(queue.message.Bytes()); got != want {
		t.Fatalf("canonical task = %s, want %s", got, want)
	}
}

func TestPublisherMapsEnvelopeToStableOwnedTask(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	envelope := outbox.Envelope{
		ID:             "evt-1",
		Topic:          "orders.created",
		Payload:        []byte(`{"id":1}`),
		PayloadVersion: 3,
		Metadata: map[string]string{
			"es.content_type": "application/json",
			"es.event_name":   "order.created",
			"tenant":          "tenant-7",
		},
		OrderingKey:    "customer-7",
		IdempotencyKey: "create-order-42",
		Attempts:       4,
		AvailableAt:    time.Unix(20, 0).UTC(),
		CreatedAt:      time.Unix(10, 0).UTC(),
	}

	if err := publisher.Publish(context.Background(), envelope); err != nil {
		t.Fatalf("publish: %v", err)
	}

	var task outboxqueue.Task
	if err := json.Unmarshal(queue.message.Bytes(), &task); err != nil {
		t.Fatalf("decode queued task: %v", err)
	}
	if task.TaskID != envelope.ID || task.IdempotencyKey != envelope.IdempotencyKey ||
		task.OrderingKey != envelope.OrderingKey || task.EventName != "order.created" ||
		task.ContentType != "application/json" || task.SchemaVersion != envelope.PayloadVersion ||
		!bytes.Equal(task.Content, envelope.Payload) || task.Metadata["tenant"] != "tenant-7" {
		t.Fatalf("queued task = %#v", task)
	}
	if len(queue.options) != 1 || queue.options[0].Metadata == nil ||
		queue.options[0].Metadata.OriginalID != envelope.ID ||
		queue.options[0].Metadata.JobType != "order.created" ||
		queue.options[0].Metadata.PayloadSchemaVersion != "3" ||
		queue.options[0].RetryCount != nil {
		t.Fatalf("queue options = %#v", queue.options)
	}
}

func TestPublisherRejectsUnsupportedAndOversizedEnvelopeValuesBeforeQueue(t *testing.T) {
	t.Parallel()

	limits := outboxqueue.Limits{
		MaxTaskBytes: 256, MaxIdentityBytes: 16, MaxContentBytes: 8,
		MaxMetadataEntries: 2, MaxMetadataBytes: 16,
	}
	tests := map[string]outbox.Envelope{
		"missing task identity": {Topic: "event", PayloadVersion: 1},
		"missing event":         {ID: "task", PayloadVersion: 1},
		"missing schema":        {ID: "task", Topic: "event"},
		"oversized content": {
			ID: "task", Topic: "event", PayloadVersion: 1, Payload: make([]byte, 9),
		},
		"oversized ordering identity": {
			ID: "task", Topic: "event", PayloadVersion: 1, OrderingKey: strings.Repeat("o", 17),
		},
		"oversized metadata": {
			ID: "task", Topic: "event", PayloadVersion: 1,
			Metadata: map[string]string{"12345678": "123456789"},
		},
		"unsupported text": {
			ID: "task", Topic: string([]byte{0xff}), PayloadVersion: 1,
		},
		"blank content type": {
			ID: "task", Topic: "event", PayloadVersion: 1,
			Metadata: map[string]string{"es.content_type": " "},
		},
	}

	for name, envelope := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			queue := &recordingQueue{}
			publisher, err := outboxqueue.New(queue, outboxqueue.WithLimits(limits))
			if err != nil {
				t.Fatalf("create publisher: %v", err)
			}
			if err := publisher.Publish(context.Background(), envelope); !errors.Is(err, outboxqueue.ErrInvalidEnvelope) || queue.calls != 0 {
				t.Fatalf("publish error/calls = %v/%d", err, queue.calls)
			}
		})
	}
}

func TestPublisherPreservesAcceptanceAndRetryDisposition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		queueError error
		cancel     bool
		envelope   outbox.Envelope
		want       outboxqueue.PublishOutcome
	}{
		{
			name: "accepted", envelope: validEnvelope(),
			want: outboxqueue.PublishOutcome{Acceptance: outboxqueue.AcceptanceAccepted},
		},
		{
			name: "invalid envelope", envelope: outbox.Envelope{},
			want: outboxqueue.PublishOutcome{
				Acceptance:  outboxqueue.AcceptanceRejected,
				Disposition: outboxqueue.DispositionPermanent,
			},
		},
		{
			name: "retryable backend failure", envelope: validEnvelope(),
			queueError: management.NewFailure(
				management.ClassificationRetryable, "capacity", errors.New("full"),
			),
			want: outboxqueue.PublishOutcome{
				Acceptance:  outboxqueue.AcceptanceUnknown,
				Disposition: outboxqueue.DispositionRetryable,
			},
		},
		{
			name: "permanent backend failure", envelope: validEnvelope(),
			queueError: management.NewFailure(
				management.ClassificationPermanent, "unsupported", errors.New("bad"),
			),
			want: outboxqueue.PublishOutcome{
				Acceptance:  outboxqueue.AcceptanceUnknown,
				Disposition: outboxqueue.DispositionPermanent,
			},
		},
		{
			name: "canceled before enqueue", cancel: true, envelope: validEnvelope(),
			want: outboxqueue.PublishOutcome{
				Acceptance:  outboxqueue.AcceptanceRejected,
				Disposition: outboxqueue.DispositionCanceled,
			},
		},
		{
			name: "backend cancellation is ambiguous", envelope: validEnvelope(),
			queueError: management.NewFailure(
				management.ClassificationCanceled, "deadline", context.DeadlineExceeded,
			),
			want: outboxqueue.PublishOutcome{
				Acceptance:  outboxqueue.AcceptanceUnknown,
				Disposition: outboxqueue.DispositionCanceled,
			},
		},
		{
			name: "unclassified backend cancellation is ambiguous", envelope: validEnvelope(),
			queueError: context.Canceled,
			want: outboxqueue.PublishOutcome{
				Acceptance:  outboxqueue.AcceptanceUnknown,
				Disposition: outboxqueue.DispositionCanceled,
			},
		},
		{
			name: "unclassified backend failure is ambiguous", envelope: validEnvelope(),
			queueError: errors.New("connection lost"),
			want: outboxqueue.PublishOutcome{
				Acceptance:  outboxqueue.AcceptanceUnknown,
				Disposition: outboxqueue.DispositionRetryable,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			queue := &recordingQueue{err: test.queueError}
			publisher, err := outboxqueue.New(queue)
			if err != nil {
				t.Fatalf("create publisher: %v", err)
			}
			ctx := context.Background()
			if test.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			publishErr := publisher.Publish(ctx, test.envelope)
			if got := outboxqueue.OutcomeOf(publishErr); got != test.want {
				t.Fatalf("outcome = %#v, want %#v (error %v)", got, test.want, publishErr)
			}
		})
	}
}

func TestClassifyErrorKeepsPermanentRejectionsOutOfRelayRetries(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{err: management.NewFailure(
		management.ClassificationPermanent, "unsupported", errors.New("bad payload"),
	)}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatal(err)
	}
	publishErr := publisher.Publish(context.Background(), validEnvelope())
	if got := outboxqueue.ClassifyError(publishErr); got != relay.ErrorPermanent {
		t.Fatalf("classification = %v, want permanent", got)
	}
	if got := outboxqueue.ClassifyError(errors.New("temporary")); got != relay.ErrorTransient {
		t.Fatalf("classification = %v, want transient", got)
	}
}

func TestPublisherConvertsQueuePanicToUnknownAcceptanceWithoutLeakingIt(t *testing.T) {
	t.Parallel()

	publisher, err := outboxqueue.New(panicQueue{})
	if err != nil {
		t.Fatal(err)
	}
	publishErr := publisher.Publish(context.Background(), validEnvelope())
	want := outboxqueue.PublishOutcome{
		Acceptance:  outboxqueue.AcceptanceUnknown,
		Disposition: outboxqueue.DispositionRetryable,
	}
	if !errors.Is(publishErr, outboxqueue.ErrQueuePanic) || outboxqueue.OutcomeOf(publishErr) != want {
		t.Fatalf("panic error/outcome = %v/%#v", publishErr, outboxqueue.OutcomeOf(publishErr))
	}
	if bytes.Contains([]byte(publishErr.Error()), []byte("sensitive-detail")) {
		t.Fatalf("panic value leaked in error: %v", publishErr)
	}
}

func TestPublisherConfigurationRejectsEveryUnsafeBoundAndOption(t *testing.T) {
	t.Parallel()

	valid := outboxqueue.Limits{
		MaxTaskBytes: 1, MaxIdentityBytes: 1, MaxContentBytes: 1,
		MaxMetadataEntries: 1, MaxMetadataBytes: 1,
	}
	invalid := []outboxqueue.Limits{
		{},
		{MaxIdentityBytes: 1, MaxContentBytes: 1, MaxMetadataEntries: 1, MaxMetadataBytes: 1},
		{MaxTaskBytes: 1, MaxContentBytes: 1, MaxMetadataEntries: 1, MaxMetadataBytes: 1},
		{MaxTaskBytes: 1, MaxIdentityBytes: 1, MaxMetadataEntries: 1, MaxMetadataBytes: 1},
		{MaxTaskBytes: 1, MaxIdentityBytes: 1, MaxContentBytes: 1, MaxMetadataBytes: 1},
		{MaxTaskBytes: 1, MaxIdentityBytes: 1, MaxContentBytes: 1, MaxMetadataEntries: 1},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid limits: %v", err)
	}
	for index, limits := range invalid {
		if err := limits.Validate(); !errors.Is(err, outboxqueue.ErrInvalidConfig) {
			t.Fatalf("invalid limits %d error = %v", index, err)
		}
		if _, err := outboxqueue.New(&recordingQueue{}, outboxqueue.WithLimits(limits)); !errors.Is(err, outboxqueue.ErrInvalidConfig) {
			t.Fatalf("invalid option %d error = %v", index, err)
		}
	}
	if _, err := outboxqueue.New(&recordingQueue{}, nil); !errors.Is(err, outboxqueue.ErrInvalidConfig) {
		t.Fatalf("nil option error = %v", err)
	}
	optionErr := errors.New("option failed")
	if _, err := outboxqueue.New(&recordingQueue{}, func(*outboxqueue.Publisher) error {
		return optionErr
	}); !errors.Is(err, optionErr) {
		t.Fatalf("custom option error = %v", err)
	}
}

func TestPublisherRejectsEncodedTaskAndMetadataBoundaries(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		limits   outboxqueue.Limits
		envelope outbox.Envelope
		want     error
	}{
		"encoded task": {
			limits: outboxqueue.Limits{
				MaxTaskBytes: 1, MaxIdentityBytes: 16, MaxContentBytes: 8,
				MaxMetadataEntries: 2, MaxMetadataBytes: 32,
			},
			envelope: validEnvelope(), want: outboxqueue.ErrTaskTooLarge,
		},
		"metadata entries": {
			limits: outboxqueue.Limits{
				MaxTaskBytes: 1024, MaxIdentityBytes: 16, MaxContentBytes: 8,
				MaxMetadataEntries: 1, MaxMetadataBytes: 32,
			},
			envelope: outbox.Envelope{
				ID: "task", Topic: "event", PayloadVersion: 1,
				Metadata: map[string]string{"a": "1", "b": "2"},
			},
			want: outboxqueue.ErrInvalidEnvelope,
		},
		"blank metadata key": {
			limits: outboxqueue.Limits{
				MaxTaskBytes: 1024, MaxIdentityBytes: 16, MaxContentBytes: 8,
				MaxMetadataEntries: 2, MaxMetadataBytes: 32,
			},
			envelope: outbox.Envelope{
				ID: "task", Topic: "event", PayloadVersion: 1,
				Metadata: map[string]string{" ": "value"},
			},
			want: outboxqueue.ErrInvalidEnvelope,
		},
		"invalid metadata value": {
			limits: outboxqueue.Limits{
				MaxTaskBytes: 1024, MaxIdentityBytes: 16, MaxContentBytes: 8,
				MaxMetadataEntries: 2, MaxMetadataBytes: 32,
			},
			envelope: outbox.Envelope{
				ID: "task", Topic: "event", PayloadVersion: 1,
				Metadata: map[string]string{"key": string([]byte{0xff})},
			},
			want: outboxqueue.ErrInvalidEnvelope,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			queue := &recordingQueue{}
			publisher, err := outboxqueue.New(queue, outboxqueue.WithLimits(test.limits))
			if err != nil {
				t.Fatal(err)
			}
			publishErr := publisher.Publish(context.Background(), test.envelope)
			if !errors.Is(publishErr, test.want) || queue.calls != 0 {
				t.Fatalf("publish error/calls = %v/%d", publishErr, queue.calls)
			}
		})
	}
}

func TestPublisherAcceptsExactConfiguredBoundaries(t *testing.T) {
	t.Parallel()

	envelope := outbox.Envelope{
		ID: "id", Topic: "event", Payload: []byte("data"), PayloadVersion: 1,
		Metadata: map[string]string{strings.Repeat("k", 16): "v"},
	}
	baselineQueue := &recordingQueue{}
	baseline, err := outboxqueue.New(baselineQueue)
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Publish(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	exactTaskBytes := len(baselineQueue.message.Bytes())

	queue := &recordingQueue{}
	publisher, err := outboxqueue.New(queue, outboxqueue.WithLimits(outboxqueue.Limits{
		MaxTaskBytes: exactTaskBytes, MaxIdentityBytes: 16, MaxContentBytes: 4,
		MaxMetadataEntries: 1, MaxMetadataBytes: 17,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), envelope); err != nil || queue.calls != 1 {
		t.Fatalf("exact-boundary publish error/calls = %v/%d", err, queue.calls)
	}
}

func TestPublisherFreezesEveryIdentityAndContentBoundary(t *testing.T) {
	t.Parallel()

	limits := outboxqueue.Limits{
		MaxTaskBytes: 4096, MaxIdentityBytes: 16, MaxContentBytes: 4,
		MaxMetadataEntries: 4, MaxMetadataBytes: 128,
	}
	tests := []struct {
		name     string
		envelope outbox.Envelope
		accepted bool
	}{
		{"task ID exact", boundaryEnvelope(), true},
		{"task ID above", mutateBoundary(func(envelope *outbox.Envelope) { envelope.ID = strings.Repeat("i", 17) }), false},
		{"idempotency exact", mutateBoundary(func(envelope *outbox.Envelope) { envelope.IdempotencyKey = strings.Repeat("i", 16) }), true},
		{"idempotency above", mutateBoundary(func(envelope *outbox.Envelope) { envelope.IdempotencyKey = strings.Repeat("i", 17) }), false},
		{"ordering exact", mutateBoundary(func(envelope *outbox.Envelope) { envelope.OrderingKey = strings.Repeat("o", 16) }), true},
		{"ordering above", mutateBoundary(func(envelope *outbox.Envelope) { envelope.OrderingKey = strings.Repeat("o", 17) }), false},
		{"content type exact", mutateBoundary(func(envelope *outbox.Envelope) {
			envelope.Metadata = map[string]string{"es.content_type": strings.Repeat("c", 16)}
		}), true},
		{"content type above", mutateBoundary(func(envelope *outbox.Envelope) {
			envelope.Metadata = map[string]string{"es.content_type": strings.Repeat("c", 17)}
		}), false},
		{"event name exact", mutateBoundary(func(envelope *outbox.Envelope) { envelope.Topic = strings.Repeat("e", 16) }), true},
		{"event name above", mutateBoundary(func(envelope *outbox.Envelope) { envelope.Topic = strings.Repeat("e", 17) }), false},
		{"metadata key exact", mutateBoundary(func(envelope *outbox.Envelope) { envelope.Metadata = map[string]string{strings.Repeat("k", 16): "v"} }), true},
		{"metadata key above", mutateBoundary(func(envelope *outbox.Envelope) { envelope.Metadata = map[string]string{strings.Repeat("k", 17): "v"} }), false},
		{"content exact", mutateBoundary(func(envelope *outbox.Envelope) { envelope.Payload = []byte("1234") }), true},
		{"content above", mutateBoundary(func(envelope *outbox.Envelope) { envelope.Payload = []byte("12345") }), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			queue := &recordingQueue{}
			publisher, err := outboxqueue.New(queue, outboxqueue.WithLimits(limits))
			if err != nil {
				t.Fatal(err)
			}
			publishErr := publisher.Publish(context.Background(), test.envelope)
			if test.accepted && (publishErr != nil || queue.calls != 1) {
				t.Fatalf("exact boundary error/calls = %v/%d", publishErr, queue.calls)
			}
			if !test.accepted && (!errors.Is(publishErr, outboxqueue.ErrInvalidEnvelope) || queue.calls != 0) {
				t.Fatalf("above boundary error/calls = %v/%d", publishErr, queue.calls)
			}
		})
	}
}

func TestPublisherRejectsMetadataTotalAboveLimitIndependently(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	publisher, err := outboxqueue.New(queue, outboxqueue.WithLimits(outboxqueue.Limits{
		MaxTaskBytes: 4096, MaxIdentityBytes: 32, MaxContentBytes: 8,
		MaxMetadataEntries: 2, MaxMetadataBytes: 8,
	}))
	if err != nil {
		t.Fatal(err)
	}
	envelope := validEnvelope()
	envelope.Metadata = map[string]string{"a": "1234", "b": "5678"}
	publishErr := publisher.Publish(context.Background(), envelope)
	if !errors.Is(publishErr, outboxqueue.ErrInvalidEnvelope) || queue.calls != 0 {
		t.Fatalf("publish error/calls = %v/%d", publishErr, queue.calls)
	}
}

func TestPublisherRejectsTaskThatExceedsFirstPartyQueueEnvelope(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	publisher, err := outboxqueue.New(queue, outboxqueue.WithLimits(outboxqueue.Limits{
		MaxTaskBytes: 900 << 10, MaxIdentityBytes: 255, MaxContentBytes: 700 << 10,
		MaxMetadataEntries: 64, MaxMetadataBytes: 16 << 10,
	}))
	if err != nil {
		t.Fatal(err)
	}
	envelope := validEnvelope()
	envelope.Payload = make([]byte, 600<<10)
	publishErr := publisher.Publish(context.Background(), envelope)
	if !errors.Is(publishErr, outboxqueue.ErrTaskTooLarge) || queue.calls != 0 {
		t.Fatalf("publish error/calls = %v/%d", publishErr, queue.calls)
	}
}

func TestPublisherAcceptsLargestTaskRepresentableBelowFirstPartyQueueEnvelopeLimit(t *testing.T) {
	t.Parallel()

	payloadBytes := largestPayloadWithinQueueEnvelope(job.DefaultMaxMessageBytes)
	if queueEnvelopeSize(payloadBytes) > job.DefaultMaxMessageBytes ||
		queueEnvelopeSize(payloadBytes+1) <= job.DefaultMaxMessageBytes {
		t.Fatalf("payload boundary sizes = %d/%d", queueEnvelopeSize(payloadBytes), queueEnvelopeSize(payloadBytes+1))
	}
	queue := &recordingQueue{}
	publisher, err := outboxqueue.New(queue, outboxqueue.WithLimits(outboxqueue.Limits{
		MaxTaskBytes: job.DefaultMaxMessageBytes, MaxIdentityBytes: 255,
		MaxContentBytes: payloadBytes, MaxMetadataEntries: 64, MaxMetadataBytes: 16 << 10,
	}))
	if err != nil {
		t.Fatal(err)
	}
	envelope := validEnvelope()
	envelope.Payload = make([]byte, payloadBytes)
	if err := publisher.Publish(context.Background(), envelope); err != nil || queue.calls != 1 {
		t.Fatalf("exact queue-envelope publish error/calls = %v/%d", err, queue.calls)
	}
}

func TestPublisherRejectsTaskUnsupportedByQueueOperationalMetadata(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	publisher, err := outboxqueue.New(queue, outboxqueue.WithLimits(outboxqueue.Limits{
		MaxTaskBytes: 4096, MaxIdentityBytes: 512, MaxContentBytes: 8,
		MaxMetadataEntries: 2, MaxMetadataBytes: 1024,
	}))
	if err != nil {
		t.Fatal(err)
	}
	envelope := validEnvelope()
	envelope.Metadata = map[string]string{
		"es.content_type": strings.Repeat("a", job.MaxMetadataValueBytes+1),
	}
	publishErr := publisher.Publish(context.Background(), envelope)
	if !errors.Is(publishErr, outboxqueue.ErrInvalidEnvelope) ||
		!errors.Is(publishErr, job.ErrInvalidMessage) || queue.calls != 0 {
		t.Fatalf("publish error/calls = %v/%d", publishErr, queue.calls)
	}
}

func TestPublisherClassifiesFirstPartyQueueRejectionsAndInfrastructure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want outboxqueue.PublishOutcome
	}{
		{"capacity", firstpartyqueue.ErrMaxCapacity, outboxqueue.PublishOutcome{
			Acceptance: outboxqueue.AcceptanceRejected, Disposition: outboxqueue.DispositionRetryable,
		}},
		{"shutdown", firstpartyqueue.ErrQueueShutdown, outboxqueue.PublishOutcome{
			Acceptance: outboxqueue.AcceptanceRejected, Disposition: outboxqueue.DispositionRetryable,
		}},
		{"closed", firstpartyqueue.ErrQueueHasBeenClosed, outboxqueue.PublishOutcome{
			Acceptance: outboxqueue.AcceptanceRejected, Disposition: outboxqueue.DispositionRetryable,
		}},
		{"malformed", management.NewFailure(
			management.ClassificationMalformed, "bad_task", errors.New("bad"),
		), outboxqueue.PublishOutcome{
			Acceptance: outboxqueue.AcceptanceUnknown, Disposition: outboxqueue.DispositionPermanent,
		}},
		{"infrastructure", management.NewFailure(
			management.ClassificationInfrastructure, "disconnect", errors.New("lost"),
		), outboxqueue.PublishOutcome{
			Acceptance: outboxqueue.AcceptanceUnknown, Disposition: outboxqueue.DispositionRetryable,
		}},
		{"invalid classified failure", management.NewFailure(
			management.ClassificationPermanent, "", errors.New("invalid"),
		), outboxqueue.PublishOutcome{
			Acceptance: outboxqueue.AcceptanceUnknown, Disposition: outboxqueue.DispositionRetryable,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			publisher, err := outboxqueue.New(&recordingQueue{err: test.err})
			if err != nil {
				t.Fatal(err)
			}
			if got := outboxqueue.OutcomeOf(publisher.Publish(context.Background(), validEnvelope())); got != test.want {
				t.Fatalf("outcome = %#v, want %#v", got, test.want)
			}
		})
	}
}

func validEnvelope() outbox.Envelope {
	return outbox.Envelope{ID: "event-1", Topic: "events", PayloadVersion: 1}
}

func boundaryEnvelope() outbox.Envelope {
	return outbox.Envelope{ID: strings.Repeat("i", 16), Topic: "evt", PayloadVersion: 1}
}

func mutateBoundary(mutate func(*outbox.Envelope)) outbox.Envelope {
	envelope := boundaryEnvelope()
	mutate(&envelope)

	return envelope
}

func largestPayloadWithinQueueEnvelope(target int) int {
	low, high := 0, target
	largest := 0
	for low <= high {
		candidate := low + (high-low)/2
		size := queueEnvelopeSize(candidate)
		if size <= target {
			largest = candidate
			low = candidate + 1
		} else {
			high = candidate - 1
		}
	}

	return largest
}

func queueEnvelopeSize(payloadBytes int) int {
	task := outboxqueue.Task{
		TaskID: "event-1", IdempotencyKey: "event-1", Content: make([]byte, payloadBytes),
		ContentType: "application/json", EventName: "events", SchemaVersion: 1,
		Metadata: map[string]string{},
	}
	encoded, err := json.Marshal(task)
	if err != nil {
		panic(err)
	}
	queued := job.NewMessage(testMessage(encoded), job.AllowOption{Metadata: &job.Metadata{
		OriginalID: "event-1", PayloadSchemaVersion: "1",
		ContentType: "application/json", JobType: "events",
	}})

	return len(queued.Bytes())
}

func TestPublisherPreservesQueueFailure(t *testing.T) {
	t.Parallel()

	queueErr := errors.New("backend failure includes sensitive-detail")
	publisher, err := outboxqueue.New(&recordingQueue{err: queueErr})
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	if err := publisher.Publish(context.Background(), validEnvelope()); !errors.Is(err, queueErr) {
		t.Fatalf("publish error = %v, want %v", err, queueErr)
	} else if bytes.Contains([]byte(err.Error()), []byte("sensitive-detail")) {
		t.Fatalf("publish error leaked backend diagnostics: %v", err)
	}
}

func TestPublisherErrorsDoNotLeakEnvelopeOrBackendSecrets(t *testing.T) {
	t.Parallel()

	const (
		payloadSecret  = "payload-secret"
		metadataSecret = "metadata-secret"
		backendSecret  = "backend-diagnostic"
		endpointSecret = "redis://private.internal:6379"
		credential     = "credential-secret"
	)
	queueErr := errors.New(backendSecret + " " + endpointSecret + " " + credential)
	queue := &recordingQueue{err: queueErr}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatal(err)
	}
	envelope := validEnvelope()
	envelope.Payload = []byte(payloadSecret)
	envelope.Metadata = map[string]string{"private": metadataSecret}
	publishErr := publisher.Publish(context.Background(), envelope)
	if !errors.Is(publishErr, queueErr) {
		t.Fatalf("publication error no longer preserves backend identity: %v", publishErr)
	}
	for _, secret := range []string{
		payloadSecret, metadataSecret, backendSecret, endpointSecret, credential,
	} {
		if strings.Contains(publishErr.Error(), secret) {
			t.Fatalf("publication error leaked %q", secret)
		}
	}
}

func TestPublisherOwnsEnvelopeBytesAndMetadataAfterReturn(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatal(err)
	}
	envelope := validEnvelope()
	envelope.Payload = []byte("owned-payload")
	envelope.Metadata = map[string]string{"tenant": "owned-metadata"}
	if err := publisher.Publish(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	want := append([]byte(nil), queue.message.Bytes()...)
	envelope.Payload[0] = '!'
	envelope.Metadata["tenant"] = "changed"

	if got := queue.message.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("caller mutation changed retained queue bytes: %q != %q", got, want)
	}
}

func TestPublisherKeepsDuplicateTaskIdentityStableAcrossRelayAttempts(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatal(err)
	}
	envelope := validEnvelope()
	envelope.Attempts = 1
	if err := publisher.Publish(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	first := append([]byte(nil), queue.message.Bytes()...)
	envelope.Attempts = 2
	if err := publisher.Publish(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, queue.message.Bytes()) {
		t.Fatalf("duplicate task bytes changed: %s != %s", first, queue.message.Bytes())
	}
	var task outboxqueue.Task
	if err := json.Unmarshal(first, &task); err != nil {
		t.Fatal(err)
	}
	if task.TaskID != envelope.ID || task.IdempotencyKey != envelope.ID {
		t.Fatalf("duplicate identity = %#v", task)
	}
}

func TestUnknownAcceptanceRetryMakesBackendDuplicateExplicit(t *testing.T) {
	t.Parallel()

	queue := &acceptThenFailQueue{err: errors.New("response lost after append")}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatal(err)
	}
	envelope := validEnvelope()
	for attempt := 1; attempt <= 2; attempt++ {
		envelope.Attempts = attempt
		publishErr := publisher.Publish(context.Background(), envelope)
		if outcome := outboxqueue.OutcomeOf(publishErr); outcome.Acceptance != outboxqueue.AcceptanceUnknown ||
			outcome.Disposition != outboxqueue.DispositionRetryable {
			t.Fatalf("attempt %d outcome = %#v", attempt, outcome)
		}
	}
	if len(queue.payloads) != 2 || !bytes.Equal(queue.payloads[0], queue.payloads[1]) {
		t.Fatalf("accepted duplicate payloads = %#v", queue.payloads)
	}
}

func TestPublisherIsSafeForConcurrentSynchronousPublication(t *testing.T) {
	t.Parallel()

	queue := &concurrentQueue{}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatal(err)
	}
	const publications = 64
	var wait sync.WaitGroup
	wait.Add(publications)
	for range publications {
		go func() {
			defer wait.Done()
			if publishErr := publisher.Publish(context.Background(), validEnvelope()); publishErr != nil {
				t.Errorf("publish: %v", publishErr)
			}
		}()
	}
	wait.Wait()
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.payloads) != publications {
		t.Fatalf("payload count = %d", len(queue.payloads))
	}
	for _, payload := range queue.payloads[1:] {
		if !bytes.Equal(payload, queue.payloads[0]) {
			t.Fatal("concurrent publications changed stable task bytes")
		}
	}
}

func TestPublisherRejectsCancellationBeforeQueueing(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publisher.Publish(ctx, outbox.Envelope{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("publish error = %v", err)
	}
	if queue.calls != 0 {
		t.Fatalf("queue calls = %d, want 0", queue.calls)
	}
}

func TestPublisherRejectsNilContextBeforeQueueing(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatal(err)
	}
	var nilContext context.Context
	publishErr := publisher.Publish(nilContext, validEnvelope())
	want := outboxqueue.PublishOutcome{
		Acceptance:  outboxqueue.AcceptanceRejected,
		Disposition: outboxqueue.DispositionPermanent,
	}
	if !errors.Is(publishErr, outboxqueue.ErrContextRequired) ||
		outboxqueue.OutcomeOf(publishErr) != want || queue.calls != 0 {
		t.Fatalf("publish error/outcome/calls = %v/%#v/%d", publishErr, outboxqueue.OutcomeOf(publishErr), queue.calls)
	}
}

func TestPublisherDoesNotMisreportCancellationAfterQueueAcceptance(t *testing.T) {
	t.Parallel()

	queue := &blockingQueue{started: make(chan struct{}), release: make(chan struct{})}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- publisher.Publish(ctx, validEnvelope())
	}()
	select {
	case <-queue.started:
	case err := <-done:
		t.Fatalf("publish returned before entering the synchronous queue call: %v", err)
	case <-time.After(time.Second):
		t.Fatal("publish did not enter the synchronous queue call")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("publish returned before synchronous queue call completed: %v", err)
	default:
	}
	close(queue.release)
	if err := <-done; err != nil {
		t.Fatalf("accepted queue result changed after cancellation: %v", err)
	}
}

func TestNewRequiresQueue(t *testing.T) {
	t.Parallel()

	if _, err := outboxqueue.New(nil); !errors.Is(err, outboxqueue.ErrQueueRequired) {
		t.Fatalf("error = %v, want %v", err, outboxqueue.ErrQueueRequired)
	}
	var typedNil *recordingQueue
	if _, err := outboxqueue.New(typedNil); !errors.Is(err, outboxqueue.ErrQueueRequired) {
		t.Fatalf("typed nil error = %v, want %v", err, outboxqueue.ErrQueueRequired)
	}
}

type recordingQueue struct {
	message core.QueuedMessage
	options []job.AllowOption
	err     error
	calls   int
}

type blockingQueue struct {
	started chan struct{}
	release chan struct{}
}

type panicQueue struct{}

type testMessage []byte

type concurrentQueue struct {
	mu       sync.Mutex
	payloads [][]byte
}

type acceptThenFailQueue struct {
	payloads [][]byte
	err      error
}

func (message testMessage) Bytes() []byte { return message }

func (queue *acceptThenFailQueue) Queue(message core.QueuedMessage, _ ...job.AllowOption) error {
	queue.payloads = append(queue.payloads, message.Bytes())

	return queue.err
}

func (queue *concurrentQueue) Queue(message core.QueuedMessage, _ ...job.AllowOption) error {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.payloads = append(queue.payloads, message.Bytes())

	return nil
}

func (panicQueue) Queue(core.QueuedMessage, ...job.AllowOption) error {
	panic("sensitive-detail")
}

func (queue *blockingQueue) Queue(core.QueuedMessage, ...job.AllowOption) error {
	close(queue.started)
	<-queue.release

	return nil
}

func (queue *recordingQueue) Queue(message core.QueuedMessage, options ...job.AllowOption) error {
	queue.calls++
	queue.message = message
	queue.options = options

	return queue.err
}
