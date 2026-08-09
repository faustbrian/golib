//go:build integration

package goqueue_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/goqueue"
	"github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/faustbrian/golib/pkg/outbox/relay"
	firstpartyqueue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	redisstream "github.com/faustbrian/golib/pkg/queue/redisstream"
	"github.com/faustbrian/golib/pkg/queue/valkeystream"
)

const durableStreamCapacity int64 = 16

type durableProducer struct {
	queue goqueue.Queue
	close func()
}

type durableBackend struct {
	producer func(*testing.T) durableProducer
	consumer func(*testing.T) core.Worker
}

func TestPublisherPreservesDuplicateAndOrderingIdentityThroughDurableBackends(t *testing.T) {
	stream := func(backend string) string {
		return fmt.Sprintf("outbox-goqueue-%s-%d", backend, time.Now().UnixNano())
	}

	t.Run("Redis Streams", func(t *testing.T) {
		address := requiredEnvironment(t, "REDIS_ADDR")
		name := stream("redis")
		backend := durableBackend{
			producer: func(t *testing.T) durableProducer {
				t.Helper()
				worker := newRedisWorker(t, address, name)
				queue, err := firstpartyqueue.NewQueue(firstpartyqueue.WithWorker(worker))
				if err != nil {
					t.Fatalf("create Redis queue: %v", err)
				}

				return durableProducer{queue: queue, close: queue.Shutdown}
			},
			consumer: func(t *testing.T) core.Worker {
				t.Helper()
				return newRedisWorker(t, address, name)
			},
		}
		assertDurableRestartAndOrdering(t, backend)
		assertRelayProcessWindows(t, backend)
		assertRelayShutdownRace(t, backend)
	})

	t.Run("Valkey Streams", func(t *testing.T) {
		address := requiredEnvironment(t, "VALKEY_ADDR")
		name := stream("valkey")
		backend := durableBackend{
			producer: func(t *testing.T) durableProducer {
				t.Helper()
				producer, err := valkeystream.NewPublisherE(
					valkeystream.WithAddress(address), valkeystream.WithStreamName(name),
					valkeystream.WithMaxLength(durableStreamCapacity),
				)
				if err != nil {
					t.Fatalf("create Valkey publisher: %v", err)
				}

				return durableProducer{
					queue: producer,
					close: func() { _ = producer.Shutdown() },
				}
			},
			consumer: func(t *testing.T) core.Worker {
				t.Helper()
				worker, err := valkeystream.NewWorkerE(
					valkeystream.WithAddress(address), valkeystream.WithStreamName(name),
					valkeystream.WithGroup("outbox-goqueue"),
					valkeystream.WithMaxLength(durableStreamCapacity),
					valkeystream.WithRequestTimeout(5*time.Second),
				)
				if err != nil {
					t.Fatalf("create Valkey worker: %v", err)
				}

				return worker
			},
		}
		assertDurableRestartAndOrdering(t, backend)
		assertRelayProcessWindows(t, backend)
		assertRelayShutdownRace(t, backend)
	})
}

func newRedisWorker(t *testing.T, address, stream string) *redisstream.Worker {
	t.Helper()
	worker, err := redisstream.NewWorkerE(
		redisstream.WithAddr(address), redisstream.WithStreamName(stream),
		redisstream.WithGroup("outbox-goqueue"),
		redisstream.WithMaxLength(durableStreamCapacity),
		redisstream.WithRequestTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("create Redis worker: %v", err)
	}

	return worker
}

func assertDurableRestartAndOrdering(t *testing.T, backend durableBackend) {
	t.Helper()

	first := validEnvelope()
	first.OrderingKey = "aggregate-7"
	first.IdempotencyKey = "command-42"
	firstProducer := backend.producer(t)
	publishDurably(t, firstProducer.queue, first)
	firstProducer.close()

	// This restart models process death after enqueue and before the outbox row
	// is marked delivered: the next claim carries a larger Attempts value but
	// must enqueue identical task bytes and identity.
	secondProducer := backend.producer(t)
	first.Attempts++
	publishDurably(t, secondProducer.queue, first)
	second := first
	second.ID = "event-2"
	second.IdempotencyKey = "command-43"
	publishDurably(t, secondProducer.queue, second)
	secondProducer.close()

	publisher, err := goqueue.New(secondProducer.queue)
	if err != nil {
		t.Fatal(err)
	}
	closedErr := publisher.Publish(t.Context(), second)
	closedOutcome := goqueue.OutcomeOf(closedErr)
	if !errors.Is(closedErr, firstpartyqueue.ErrQueueShutdown) ||
		closedOutcome.Acceptance != goqueue.AcceptanceRejected ||
		closedOutcome.Disposition != goqueue.DispositionRetryable {
		t.Fatalf("closed producer error/outcome = %v/%#v", closedErr, closedOutcome)
	}

	consumer := backend.consumer(t)
	defer shutdownConsumer(t, consumer)
	tasks := make([]goqueue.Task, 0, 3)
	payloads := make([][]byte, 0, 3)
	for range 3 {
		delivery, requestErr := consumer.Request()
		if requestErr != nil {
			t.Fatalf("request durable task: %v", requestErr)
		}
		payload := append([]byte(nil), delivery.Payload()...)
		var task goqueue.Task
		if decodeErr := json.Unmarshal(payload, &task); decodeErr != nil {
			t.Fatalf("decode durable task: %v", decodeErr)
		}
		tasks = append(tasks, task)
		payloads = append(payloads, payload)
		acknowledger, ok := delivery.(core.Acknowledger)
		if !ok || !acknowledger.AcknowledgementRequired() {
			t.Fatal("durable delivery does not require acknowledgement")
		}
		if ackErr := acknowledger.Ack(); ackErr != nil {
			t.Fatalf("acknowledge durable task: %v", ackErr)
		}
	}
	if tasks[0].TaskID != first.ID || tasks[1].TaskID != first.ID ||
		tasks[2].TaskID != second.ID || tasks[0].OrderingKey != first.OrderingKey ||
		tasks[1].OrderingKey != first.OrderingKey || tasks[2].OrderingKey != first.OrderingKey {
		t.Fatalf("durable task order = %#v", tasks)
	}
	if !bytes.Equal(payloads[0], payloads[1]) {
		t.Fatalf("process-restart duplicate bytes changed: %s != %s", payloads[0], payloads[1])
	}
}

func assertRelayProcessWindows(t *testing.T, backend durableBackend) {
	t.Helper()

	t.Run("relay process windows", func(t *testing.T) {
		t.Run("before enqueue", func(t *testing.T) {
			store := newRelayWindowStore("before-enqueue")
			producer := backend.producer(t)
			t.Cleanup(producer.close)
			consumer := backend.consumer(t)
			publisher, err := goqueue.New(producer.queue)
			if err != nil {
				t.Fatal(err)
			}
			worker := newWindowRelay(t, store, publisher)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			result, runErr := worker.RunOnce(ctx)
			if runErr != nil || result.Published != 0 || result.Released != 1 {
				t.Fatalf("killed-before-enqueue result/error = %#v/%v", result, runErr)
			}
			result, runErr = newWindowRelay(t, store, publisher).RunOnce(context.Background())
			if runErr != nil || result.Delivered != 1 {
				t.Fatalf("restarted relay result/error = %#v/%v", result, runErr)
			}
			payloads := consumeDurablePayloads(t, consumer, 1)
			assertTaskIDs(t, payloads, "before-enqueue")
		})

		t.Run("after enqueue before mark", func(t *testing.T) {
			store := newRelayWindowStore("after-enqueue")
			store.markFailures = 1
			producer := backend.producer(t)
			t.Cleanup(producer.close)
			consumer := backend.consumer(t)
			publisher, err := goqueue.New(producer.queue)
			if err != nil {
				t.Fatal(err)
			}
			result, runErr := newWindowRelay(t, store, publisher).RunOnce(context.Background())
			if !errors.Is(runErr, errSimulatedProcessDeath) || result.Published != 1 || result.Delivered != 0 {
				t.Fatalf("killed-before-mark result/error = %#v/%v", result, runErr)
			}
			result, runErr = newWindowRelay(t, store, publisher).RunOnce(context.Background())
			if runErr != nil || result.Delivered != 1 {
				t.Fatalf("restarted relay result/error = %#v/%v", result, runErr)
			}
			payloads := consumeDurablePayloads(t, consumer, 2)
			assertTaskIDs(t, payloads, "after-enqueue", "after-enqueue")
			if !bytes.Equal(payloads[0], payloads[1]) {
				t.Fatal("after-enqueue/before-mark duplicate changed canonical task bytes")
			}
		})

		t.Run("after mark", func(t *testing.T) {
			store := newRelayWindowStore("after-mark")
			producer := backend.producer(t)
			t.Cleanup(producer.close)
			consumer := backend.consumer(t)
			publisher, err := goqueue.New(producer.queue)
			if err != nil {
				t.Fatal(err)
			}
			result, runErr := newWindowRelay(t, store, publisher).RunOnce(context.Background())
			if runErr != nil || result.Delivered != 1 {
				t.Fatalf("first relay result/error = %#v/%v", result, runErr)
			}
			result, runErr = newWindowRelay(t, store, publisher).RunOnce(context.Background())
			if runErr != nil || result.Claimed != 0 || result.Published != 0 {
				t.Fatalf("restarted-after-mark result/error = %#v/%v", result, runErr)
			}
			payloads := consumeDurablePayloads(t, consumer, 1)
			assertTaskIDs(t, payloads, "after-mark")
		})
	})
}

func assertRelayShutdownRace(t *testing.T, backend durableBackend) {
	t.Helper()

	t.Run("relay and backend shutdown race", func(t *testing.T) {
		producer := backend.producer(t)
		gate := &shutdownRaceQueue{
			queue: producer.queue, firstAccepted: make(chan struct{}), release: make(chan struct{}),
		}
		publisher, err := goqueue.New(gate)
		if err != nil {
			t.Fatal(err)
		}
		store := newRelayBatchStore(32)
		worker, err := relay.New(store, publisher, relay.Config{
			Owner: "shutdown-race", BatchSize: 32, Workers: 8,
			ClassifyError: goqueue.ClassifyError,
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, runErr := worker.RunOnce(ctx)
			done <- runErr
		}()
		select {
		case <-gate.firstAccepted:
		case <-time.After(10 * time.Second):
			t.Fatal("relay did not accept the first durable task")
		}
		cancel()
		close(gate.release)
		producer.close()
		select {
		case runErr := <-done:
			if runErr != nil {
				t.Fatalf("relay shutdown race: %v", runErr)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("relay shutdown race did not finish")
		}
		payloads := gate.acceptedPayloads()
		if len(payloads) == 0 {
			t.Fatal("shutdown race accepted no durable task")
		}
		for _, payload := range payloads {
			var task goqueue.Task
			if err := json.Unmarshal(payload, &task); err != nil || task.TaskID == "" {
				t.Fatalf("shutdown race task = %#v, error = %v", task, err)
			}
		}
	})
}

func newWindowRelay(t *testing.T, store relay.Store, publisher relay.Publisher) *relay.Relay {
	t.Helper()
	worker, err := relay.New(store, publisher, relay.Config{
		Owner: "process-window", BatchSize: 1, Workers: 1,
		ClassifyError: goqueue.ClassifyError,
	})
	if err != nil {
		t.Fatal(err)
	}

	return worker
}

func consumeDurablePayloads(t *testing.T, consumer core.Worker, count int) [][]byte {
	t.Helper()
	defer shutdownConsumer(t, consumer)
	payloads := make([][]byte, 0, count)
	for range count {
		delivery, err := consumer.Request()
		if err != nil {
			t.Fatalf("request durable relay task: %v", err)
		}
		payloads = append(payloads, append([]byte(nil), delivery.Payload()...))
		acknowledger, ok := delivery.(core.Acknowledger)
		if !ok || !acknowledger.AcknowledgementRequired() {
			t.Fatal("durable relay delivery does not require acknowledgement")
		}
		if err := acknowledger.Ack(); err != nil {
			t.Fatalf("acknowledge durable relay task: %v", err)
		}
	}
	return payloads
}

func shutdownConsumer(t *testing.T, consumer core.Worker) {
	t.Helper()
	if err := consumer.Shutdown(); err != nil {
		t.Errorf("shutdown durable consumer: %v", err)
	}
}

func assertTaskIDs(t *testing.T, payloads [][]byte, ids ...string) {
	t.Helper()
	for index, payload := range payloads {
		var task goqueue.Task
		if err := json.Unmarshal(payload, &task); err != nil || task.TaskID != ids[index] {
			t.Fatalf("durable task %d = %#v, error = %v", index, task, err)
		}
	}
}

var errSimulatedProcessDeath = errors.New("simulated relay process death")

type relayWindowStore struct {
	mu           sync.Mutex
	envelope     outbox.Envelope
	delivered    bool
	markFailures int
	claims       int
}

func newRelayWindowStore(id string) *relayWindowStore {
	return &relayWindowStore{envelope: outbox.Envelope{ID: id, Topic: "events", PayloadVersion: 1}}
}

func (*relayWindowStore) Ping(context.Context) error { return nil }

func (store *relayWindowStore) Claim(context.Context, postgres.ClaimRequest) ([]postgres.Claim, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.delivered {
		return nil, nil
	}
	store.claims++
	envelope := store.envelope
	envelope.Attempts = store.claims

	return []postgres.Claim{{Envelope: envelope, Owner: "process-window", LeaseToken: "lease"}}, nil
}

func (*relayWindowStore) ExtendLease(context.Context, postgres.LeaseRef, time.Duration) (time.Time, error) {
	return time.Now().Add(time.Minute), nil
}

func (store *relayWindowStore) MarkDelivered(context.Context, postgres.LeaseRef) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.markFailures > 0 {
		store.markFailures--

		return errSimulatedProcessDeath
	}
	store.delivered = true

	return nil
}

func (*relayWindowStore) Retry(context.Context, postgres.LeaseRef, time.Duration, error) error {
	return nil
}

func (*relayWindowStore) DeadLetter(context.Context, postgres.LeaseRef, error) error { return nil }

func (*relayWindowStore) ReleaseLease(context.Context, postgres.LeaseRef) error { return nil }

type relayBatchStore struct {
	claims []postgres.Claim
}

func newRelayBatchStore(count int) *relayBatchStore {
	claims := make([]postgres.Claim, count)
	for index := range claims {
		id := fmt.Sprintf("shutdown-%d", index)
		claims[index] = postgres.Claim{
			Envelope: outbox.Envelope{ID: id, Topic: "events", PayloadVersion: 1},
			Owner:    "shutdown-race", LeaseToken: "lease-" + id,
		}
	}

	return &relayBatchStore{claims: claims}
}

func (*relayBatchStore) Ping(context.Context) error { return nil }

func (store *relayBatchStore) Claim(context.Context, postgres.ClaimRequest) ([]postgres.Claim, error) {
	return append([]postgres.Claim(nil), store.claims...), nil
}

func (*relayBatchStore) ExtendLease(context.Context, postgres.LeaseRef, time.Duration) (time.Time, error) {
	return time.Now().Add(time.Minute), nil
}

func (*relayBatchStore) MarkDelivered(context.Context, postgres.LeaseRef) error { return nil }

func (*relayBatchStore) Retry(context.Context, postgres.LeaseRef, time.Duration, error) error {
	return nil
}

func (*relayBatchStore) DeadLetter(context.Context, postgres.LeaseRef, error) error { return nil }

func (*relayBatchStore) ReleaseLease(context.Context, postgres.LeaseRef) error { return nil }

type shutdownRaceQueue struct {
	queue         goqueue.Queue
	firstAccepted chan struct{}
	release       chan struct{}
	calls         atomic.Int64
	once          sync.Once
	mu            sync.Mutex
	payloads      [][]byte
}

func (queue *shutdownRaceQueue) Queue(message core.QueuedMessage, options ...job.AllowOption) error {
	call := queue.calls.Add(1)
	if call > 1 {
		<-queue.release
	}
	err := queue.queue.Queue(message, options...)
	if err == nil {
		queue.mu.Lock()
		queue.payloads = append(queue.payloads, message.Bytes())
		queue.mu.Unlock()
		queue.once.Do(func() { close(queue.firstAccepted) })
	}

	return err
}

func (queue *shutdownRaceQueue) acceptedPayloads() [][]byte {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	payloads := make([][]byte, len(queue.payloads))
	for index := range queue.payloads {
		payloads[index] = append([]byte(nil), queue.payloads[index]...)
	}

	return payloads
}

func publishDurably(t *testing.T, queue goqueue.Queue, envelope outbox.Envelope) {
	t.Helper()
	publisher, err := goqueue.New(queue)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(t.Context(), envelope); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func requiredEnvironment(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required for durable integration", name)
	}

	return value
}
