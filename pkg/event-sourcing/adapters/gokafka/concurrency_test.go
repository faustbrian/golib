package gokafka

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

func TestSharedAdapterBoundariesRemainIndependentUnderConcurrency(t *testing.T) {
	t.Parallel()

	const workers = 64
	codec := testRecordCodec(t)
	live := encodedLiveRecord(t, codec, testMessage(t))
	publisher := &concurrentRecordingPublisher{}
	dispatcher, err := NewDispatcher(publisher, codec)
	if err != nil {
		t.Fatalf("construct shared dispatcher: %v", err)
	}
	consumer := &concurrentRecordingConsumer{}
	handler, err := NewRecordHandler(codec, consumer)
	if err != nil {
		t.Fatalf("construct shared handler: %v", err)
	}
	deadLetterPolicy, err := NewDeadLetterPolicy(
		publisher,
		DeadLetterPolicyConfig{Topic: "accounts.events.dead-letter"},
	)
	if err != nil {
		t.Fatalf("construct shared dead-letter policy: %v", err)
	}
	delivery, err := eventsourcing.NewDelivery(
		testMessage(t),
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatalf("construct shared delivery: %v", err)
	}

	start := make(chan struct{})
	errorsFound := make(chan error, workers*3)
	var wait sync.WaitGroup
	for worker := range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start

			if err := dispatcher.Dispatch(
				context.Background(),
				[]eventsourcing.Delivery{delivery},
			); err != nil {
				errorsFound <- fmt.Errorf("dispatch: %w", err)
			}
			record := consumedRecord(live)
			record.Offset = int64(worker)
			if err := handler.Handle(context.Background(), record); err != nil {
				errorsFound <- fmt.Errorf("handle: %w", err)
			}
			disposition, err := deadLetterPolicy.HandleFailure(
				context.Background(),
				record,
				errors.New("redacted failure"),
			)
			if err != nil || disposition != FailureHandled {
				errorsFound <- fmt.Errorf(
					"dead letter: disposition=%d error=%w",
					disposition,
					err,
				)
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}

	messages := publisher.snapshot()
	if len(messages) != workers*2 {
		t.Fatalf("published records = %d, want %d", len(messages), workers*2)
	}
	liveCount := 0
	deadLetterOffsets := make(map[string]struct{}, workers)
	for _, message := range messages {
		switch message.Topic {
		case "accounts.events.v1":
			liveCount++
		case "accounts.events.dead-letter":
			for _, header := range message.Headers {
				if header.Key == HeaderDeadLetterSourceOffset {
					deadLetterOffsets[string(header.Value)] = struct{}{}
				}
			}
		default:
			t.Fatalf("unexpected published topic %q", message.Topic)
		}
	}
	if liveCount != workers || len(deadLetterOffsets) != workers {
		t.Fatalf(
			"live/dead-letter identities = %d/%d, want %d/%d",
			liveCount,
			len(deadLetterOffsets),
			workers,
			workers,
		)
	}
	if consumed := consumer.count(); consumed != workers {
		t.Fatalf("consumed deliveries = %d, want %d", consumed, workers)
	}
}

type concurrentRecordingPublisher struct {
	mutex    sync.Mutex
	messages []kafka.Message
}

func (publisher *concurrentRecordingPublisher) Publish(
	_ context.Context,
	message kafka.Message,
) error {
	publisher.mutex.Lock()
	defer publisher.mutex.Unlock()
	publisher.messages = append(publisher.messages, message)

	return nil
}

func (publisher *concurrentRecordingPublisher) snapshot() []kafka.Message {
	publisher.mutex.Lock()
	defer publisher.mutex.Unlock()

	return append([]kafka.Message(nil), publisher.messages...)
}

type concurrentRecordingConsumer struct {
	mutex sync.Mutex
	calls int
}

func (consumer *concurrentRecordingConsumer) Consume(
	_ context.Context,
	delivery eventsourcing.Delivery,
) error {
	if delivery.Message().ID().String() != "msg-42" {
		return errors.New("delivery identity changed")
	}
	consumer.mutex.Lock()
	defer consumer.mutex.Unlock()
	consumer.calls++

	return nil
}

func (consumer *concurrentRecordingConsumer) count() int {
	consumer.mutex.Lock()
	defer consumer.mutex.Unlock()

	return consumer.calls
}
