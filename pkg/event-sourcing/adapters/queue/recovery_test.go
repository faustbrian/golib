package eventqueue

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	queuepkg "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func TestDispatcherPreservesAmbiguityForAcceptedFailuresAndDuplicateRetry(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	for name, failure := range map[string]error{
		"timeout":       context.DeadlineExceeded,
		"cancellation":  context.Canceled,
		"disconnection": errors.New("connection reset by peer"),
		"shutdown":      queuepkg.ErrQueueShutdown,
	} {
		failure := failure
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			queue := &retainingFailureQueue{err: failure}
			dispatcher, constructErr := NewDispatcher(DispatcherConfig{
				Queue: queue,
				Codec: codec,
			})
			if constructErr != nil {
				t.Fatalf("NewDispatcher() error = %v", constructErr)
			}
			for attempt := 0; attempt < 2; attempt++ {
				dispatchErr := dispatcher.Dispatch(
					context.Background(),
					[]eventsourcing.Delivery{minimalQueueDelivery(t)},
				)
				var outcome *DispatchError
				if !errors.Is(dispatchErr, failure) ||
					!errors.As(dispatchErr, &outcome) ||
					outcome.Acceptance() != AcceptanceUnknown ||
					outcome.Enqueued() != 0 ||
					outcome.Attempted() != 1 {
					t.Fatalf("Dispatch(attempt %d) error = %#v", attempt, dispatchErr)
				}
			}
			accepted := queue.acceptedPayloads()
			if len(accepted) != 2 || !bytes.Equal(accepted[0], accepted[1]) {
				t.Fatalf("accepted retry payloads = %d", len(accepted))
			}
		})
	}
}

func TestCancellationCannotInterruptAnAdmittedQueueCall(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	queue := &retainingFailureQueue{
		err:     context.DeadlineExceeded,
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	dispatcher, err := NewDispatcher(DispatcherConfig{Queue: queue, Codec: codec})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- dispatcher.Dispatch(
			ctx,
			[]eventsourcing.Delivery{minimalQueueDelivery(t)},
		)
	}()
	<-queue.entered
	cancel()
	select {
	case dispatchErr := <-result:
		t.Fatalf("Dispatch() returned before Queue: %v", dispatchErr)
	case <-time.After(20 * time.Millisecond):
	}
	close(queue.release)
	dispatchErr := <-result
	var outcome *DispatchError
	if !errors.Is(dispatchErr, context.DeadlineExceeded) ||
		!errors.As(dispatchErr, &outcome) ||
		outcome.Acceptance() != AcceptanceUnknown {
		t.Fatalf("Dispatch() error = %#v", dispatchErr)
	}
}

func TestDuplicateRedeliveryRequiresMessageIDIdempotency(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	encoded, err := codec.Encode(minimalQueueDelivery(t))
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	var lock sync.Mutex
	seen := map[string]struct{}{}
	effects := 0
	ctx, cancel := context.WithCancel(context.Background())
	handler, err := NewTaskHandler(codec, func(
		_ context.Context,
		delivery eventsourcing.Delivery,
	) error {
		lock.Lock()
		defer lock.Unlock()
		messageID := delivery.Message().ID().String()
		if _, exists := seen[messageID]; exists {
			return nil
		}
		seen[messageID] = struct{}{}
		effects++
		cancel()
		return nil
	})
	if err != nil {
		t.Fatalf("NewTaskHandler() error = %v", err)
	}
	if handleErr := handler.Handle(
		ctx,
		&settlementTask{body: encoded},
	); !errors.Is(handleErr, context.Canceled) {
		t.Fatalf("Handle(effect before settlement loss) error = %v", handleErr)
	}
	if handleErr := handler.Handle(
		context.Background(),
		&settlementTask{body: encoded},
	); handleErr != nil {
		t.Fatalf("Handle(redelivery) error = %v", handleErr)
	}
	if effects != 1 {
		t.Fatalf("durable effects = %d, want 1", effects)
	}
}

func TestSharedCodecDispatcherAndHandlerOwnConcurrentData(t *testing.T) {
	t.Parallel()

	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	queue := &retainingFailureQueue{}
	dispatcher, err := NewDispatcher(DispatcherConfig{
		Queue: queue,
		Codec: codec,
		Job: job.AllowOption{Metadata: &job.Metadata{
			Tags:         map[string]string{"scope": "events"},
			Correlation:  map[string]string{"request": "request-1"},
			TraceContext: map[string]string{"traceparent": "trace-1"},
		}},
	})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	const workers = 64
	retainedDeliveries := make(chan eventsourcing.Delivery, workers)
	var consumed atomic.Int64
	handler, err := NewTaskHandler(codec, func(
		_ context.Context,
		delivery eventsourcing.Delivery,
	) error {
		consumed.Add(1)
		retainedDeliveries <- delivery
		return nil
	})
	if err != nil {
		t.Fatalf("NewTaskHandler() error = %v", err)
	}
	delivery := minimalQueueDelivery(t)
	encoded, err := codec.Encode(delivery)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer group.Done()
			if dispatchErr := dispatcher.Dispatch(
				context.Background(),
				[]eventsourcing.Delivery{delivery},
			); dispatchErr != nil {
				t.Errorf("Dispatch() error = %v", dispatchErr)
				return
			}
			if handleErr := handler.Handle(
				context.Background(),
				&settlementTask{body: encoded},
			); handleErr != nil {
				t.Errorf("Handle() error = %v", handleErr)
			}
		}()
	}
	group.Wait()
	if consumed.Load() != workers {
		t.Fatalf("consumer calls = %d", consumed.Load())
	}

	messages, options := queue.retained()
	if len(messages) != workers || len(options) != workers {
		t.Fatalf("retained messages/options = %d/%d", len(messages), len(options))
	}
	first := messages[0].Bytes()
	first[0] ^= 0xff
	if bytes.Equal(first, messages[0].Bytes()) {
		t.Fatal("queued message retained returned byte storage")
	}
	options[0].Metadata.Tags["scope"] = "mutated"
	options[0].Metadata.Correlation["request"] = "mutated"
	options[0].Metadata.TraceContext["traceparent"] = "mutated"
	for index := 1; index < len(options); index++ {
		if options[index].Metadata.Tags["scope"] != "events" ||
			options[index].Metadata.Correlation["request"] != "request-1" ||
			options[index].Metadata.TraceContext["traceparent"] != "trace-1" {
			t.Fatalf("option %d shared mutable metadata: %#v", index, options[index])
		}
	}
	encoded[0] ^= 0xff
	retainedDelivery := <-retainedDeliveries
	if !retainedDelivery.Message().Equal(delivery.Message()) {
		t.Fatalf("consumer-retained delivery = %#v", retainedDelivery)
	}
	retained, err := codec.Decode(messages[1].Bytes())
	if err != nil || !retained.Message().Equal(delivery.Message()) {
		t.Fatalf("retained delivery = %#v, %v", retained, err)
	}
}

type retainingFailureQueue struct {
	mu       sync.Mutex
	messages []core.QueuedMessage
	options  []job.AllowOption
	err      error
	entered  chan struct{}
	release  chan struct{}
}

func (queue *retainingFailureQueue) Queue(
	message core.QueuedMessage,
	options ...job.AllowOption,
) error {
	if len(options) != 1 {
		return errors.New("expected exactly one job option")
	}
	queue.mu.Lock()
	queue.messages = append(queue.messages, message)
	queue.options = append(queue.options, options[0])
	queue.mu.Unlock()
	if queue.entered != nil {
		queue.entered <- struct{}{}
	}
	if queue.release != nil {
		<-queue.release
	}
	return queue.err
}

func (queue *retainingFailureQueue) acceptedPayloads() [][]byte {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	payloads := make([][]byte, len(queue.messages))
	for index, message := range queue.messages {
		payloads[index] = message.Bytes()
	}
	return payloads
}

func (queue *retainingFailureQueue) retained() (
	[]core.QueuedMessage,
	[]job.AllowOption,
) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return append([]core.QueuedMessage(nil), queue.messages...),
		append([]job.AllowOption(nil), queue.options...)
}
