package goqueue

import (
	"context"
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func TestDispatcherEnqueuesLiveDeliveriesInOrder(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	queue := &queueStub{}
	timeout := time.Second
	dispatcher, err := NewDispatcher(DispatcherConfig{
		Queue: queue,
		Codec: codec,
		Job:   job.AllowOption{Timeout: &timeout},
	})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	timeout = -1

	first := minimalQueueDelivery(t)
	second := queueDelivery(t, eventsourcing.DeliveryLive)
	if err := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{first, second},
	); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if len(queue.deliveries) != 2 {
		t.Fatalf("queued deliveries = %d", len(queue.deliveries))
	}
	for index, want := range []eventsourcing.Delivery{first, second} {
		got, decodeErr := codec.Decode(queue.deliveries[index])
		if decodeErr != nil || !got.Message().Equal(want.Message()) {
			t.Fatalf("delivery %d = %#v, %v", index, got, decodeErr)
		}
	}
	if queue.timeouts[0] != time.Second || queue.timeouts[1] != time.Second {
		t.Fatalf("queue timeouts = %v", queue.timeouts)
	}
}

func TestDispatcherRejectsReplayUnlessExplicit(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	queue := &queueStub{}
	dispatcher, err := NewDispatcher(DispatcherConfig{Queue: queue, Codec: codec})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	replay := queueDelivery(t, eventsourcing.DeliveryReplay)
	err = dispatcher.Dispatch(context.Background(), []eventsourcing.Delivery{replay})
	assertQueueDispatchError(t, err, ErrReplayDenied, 0, 1, 1, 1)
	if len(queue.deliveries) != 0 {
		t.Fatal("replay was queued")
	}

	dispatcher, err = NewReplayDispatcher(
		DispatcherConfig{Queue: queue, Codec: codec},
	)
	if err != nil {
		t.Fatalf("NewReplayDispatcher() error = %v", err)
	}
	if err := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{replay},
	); err != nil {
		t.Fatalf("Dispatch(replay) error = %v", err)
	}
}

func TestDispatcherReportsPartialFailureAndContainsQueuePanic(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	first := minimalQueueDelivery(t)
	second := queueDelivery(t, eventsourcing.DeliveryLive)
	queueErr := errors.New("queue unavailable with secret")

	for name, queue := range map[string]*queueStub{
		"error": {failAt: 2, err: queueErr},
		"panic": {failAt: 2, panicValue: "secret"},
	} {
		t.Run(name, func(t *testing.T) {
			dispatcher, constructErr := NewDispatcher(
				DispatcherConfig{Queue: queue, Codec: codec},
			)
			if constructErr != nil {
				t.Fatalf("NewDispatcher() error = %v", constructErr)
			}
			dispatchErr := dispatcher.Dispatch(
				context.Background(),
				[]eventsourcing.Delivery{first, second},
			)
			want := queueErr
			if name == "panic" {
				want = ErrQueuePanic
			}
			assertQueueDispatchError(t, dispatchErr, want, 1, 1, 2, 2)
			if dispatchErr.Error() != ErrDispatchFailed.Error() {
				t.Fatalf("Dispatch() disclosed diagnostics: %v", dispatchErr)
			}
		})
	}
}

func TestDispatcherValidatesStateContextAndCancellation(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	valid := DispatcherConfig{Queue: &queueStub{}, Codec: codec}
	invalidJob := valid
	invalidJob.Job.Timeout = job.Time(-1)
	for name, config := range map[string]DispatcherConfig{
		"queue": {Codec: codec},
		"codec": {Queue: &queueStub{}},
		"job":   invalidJob,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewDispatcher(config); err == nil {
				t.Fatal("NewDispatcher() error = nil")
			}
		})
	}

	dispatcher, err := NewDispatcher(valid)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	var nilContext context.Context
	if err := dispatcher.Dispatch(nilContext, nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Dispatch(nil) error = %v", err)
	}
	var nilDispatcher *Dispatcher
	if err := nilDispatcher.Dispatch(context.Background(), nil); !errors.Is(err, ErrQueueRequired) {
		t.Fatalf("nil Dispatch() error = %v", err)
	}
	if err := dispatcher.Dispatch(context.Background(), nil); err != nil {
		t.Fatalf("Dispatch(empty) error = %v", err)
	}
	dispatcher.codec = nil
	if err := dispatcher.Dispatch(context.Background(), nil); !errors.Is(err, ErrCodecRequired) {
		t.Fatalf("Dispatch(nil codec) error = %v", err)
	}
	dispatcher.codec = codec
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := dispatcher.Dispatch(
		ctx,
		[]eventsourcing.Delivery{minimalQueueDelivery(t)},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Dispatch(cancelled) error = %v", err)
	}
	queue := &queueStub{afterQueue: cancel}
	dispatcher, err = NewDispatcher(DispatcherConfig{Queue: queue, Codec: codec})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	err = dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{{}},
	)
	assertQueueDispatchError(t, err, ErrEnvelopeInvalid, 0, 1, 1, 1)
	ctx, cancel = context.WithCancel(context.Background())
	queue.afterQueue = cancel
	err = dispatcher.Dispatch(
		ctx,
		[]eventsourcing.Delivery{
			minimalQueueDelivery(t),
			minimalQueueDelivery(t),
		},
	)
	assertQueueDispatchError(t, err, context.Canceled, 1, 0, 1, 2)
}

func TestDispatcherOwnsCompleteJobOption(t *testing.T) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	retryCount := int64(3)
	retryDelay := time.Second
	retryFactor := 3.0
	retryMin := time.Second
	retryMax := 2 * time.Second
	jitter := true
	timeout := 3 * time.Second
	enqueuedAt := time.Date(2026, 7, 25, 1, 2, 3, 0, time.UTC)
	metadata := &job.Metadata{
		OriginalID:  "original",
		EnqueuedAt:  &enqueuedAt,
		Tags:        map[string]string{"scope": "events"},
		HandlerType: "projector",
	}
	option := job.AllowOption{
		RetryCount:  &retryCount,
		RetryDelay:  &retryDelay,
		RetryFactor: &retryFactor,
		RetryMin:    &retryMin,
		RetryMax:    &retryMax,
		Jitter:      &jitter,
		Timeout:     &timeout,
		Metadata:    metadata,
	}
	queue := &queueStub{}
	dispatcher, err := NewDispatcher(
		DispatcherConfig{Queue: queue, Codec: codec, Job: option},
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	retryCount = 0
	retryDelay = 0
	retryFactor = 1
	retryMin = 0
	retryMax = 0
	jitter = false
	timeout = time.Second
	enqueuedAt = time.Time{}
	metadata.OriginalID = "changed"
	metadata.Tags["scope"] = "changed"

	if err := dispatcher.Dispatch(
		context.Background(),
		[]eventsourcing.Delivery{minimalQueueDelivery(t)},
	); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	got := queue.options[0]
	if *got.RetryCount != 3 ||
		*got.RetryDelay != time.Second ||
		*got.RetryFactor != 3 ||
		*got.RetryMin != time.Second ||
		*got.RetryMax != 2*time.Second ||
		!*got.Jitter ||
		*got.Timeout != 3*time.Second ||
		got.Metadata.OriginalID != "original" ||
		got.Metadata.EnqueuedAt.IsZero() ||
		got.Metadata.Tags["scope"] != "events" {
		t.Fatalf("queued option = %#v", got)
	}
}

type queueStub struct {
	deliveries [][]byte
	timeouts   []time.Duration
	failAt     int
	err        error
	panicValue any
	afterQueue func()
	options    []job.AllowOption
}

func (queue *queueStub) Queue(
	message core.QueuedMessage,
	options ...job.AllowOption,
) error {
	if len(options) != 1 {
		panic("expected exactly one job option")
	}
	queue.deliveries = append(queue.deliveries, message.Bytes())
	queue.options = append(queue.options, options[0])
	timeout := time.Duration(0)
	if options[0].Timeout != nil {
		timeout = *options[0].Timeout
	}
	queue.timeouts = append(queue.timeouts, timeout)
	if queue.afterQueue != nil {
		queue.afterQueue()
	}
	if len(queue.deliveries) == queue.failAt {
		if queue.panicValue != nil {
			panic(queue.panicValue)
		}
		return queue.err
	}
	return nil
}

func assertQueueDispatchError(
	t *testing.T,
	err error,
	cause error,
	enqueued int,
	failed int,
	attempted int,
	total int,
) {
	t.Helper()
	var dispatchErr *DispatchError
	if !errors.Is(err, ErrDispatchFailed) ||
		!errors.Is(err, cause) ||
		!errors.As(err, &dispatchErr) ||
		dispatchErr.Enqueued() != enqueued ||
		dispatchErr.Failed() != failed ||
		dispatchErr.Attempted() != attempted ||
		dispatchErr.Total() != total {
		t.Fatalf("Dispatch() error = %#v", err)
	}
}
