//go:build integration

package outboxqueue_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/queue"
	"github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/faustbrian/golib/pkg/outbox/relay"
	firstpartyqueue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	redisstream "github.com/faustbrian/golib/pkg/queue/redisstream"
	"github.com/faustbrian/golib/pkg/queue/valkeystream"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const durableStreamCapacity int64 = 16

type durableProducer struct {
	queue outboxqueue.Queue
	close func()
}

type durableBackend struct {
	kind     string
	address  string
	stream   string
	producer func(*testing.T) durableProducer
	consumer func(*testing.T, string, time.Duration) core.Worker
}

func TestPublisherPreservesDuplicateAndOrderingIdentityThroughDurableBackends(t *testing.T) {
	stream := func(backend string) string {
		return fmt.Sprintf("outbox-outboxqueue-%s-%d", backend, time.Now().UnixNano())
	}

	t.Run("Redis Streams", func(t *testing.T) {
		address := requiredEnvironment(t, "REDIS_ADDR")
		name := stream("redis")
		backend := durableBackend{
			kind: "redis", address: address, stream: name,
			producer: func(t *testing.T) durableProducer {
				t.Helper()
				worker := newRedisWorker(t, address, name)
				queue, err := firstpartyqueue.NewQueue(firstpartyqueue.WithWorker(worker))
				if err != nil {
					t.Fatalf("create Redis queue: %v", err)
				}

				return durableProducer{queue: queue, close: queue.Shutdown}
			},
			consumer: func(t *testing.T, consumer string, reclaim time.Duration) core.Worker {
				t.Helper()
				return newRedisWorker(t, address, name,
					redisstream.WithConsumer(consumer),
					redisstream.WithReclaim(reclaim, reclaim, durableStreamCapacity),
				)
			},
		}
		assertDurableRestartAndOrdering(t, backend)
		assertUnackedRedelivery(t, backend)
		assertRelayProcessWindows(t, backend)
		assertConcurrentRelays(t, backend)
		assertRelayShutdownRace(t, backend)
	})

	t.Run("Valkey Streams", func(t *testing.T) {
		address := requiredEnvironment(t, "VALKEY_ADDR")
		name := stream("valkey")
		backend := durableBackend{
			kind: "valkey", address: address, stream: name,
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
			consumer: func(t *testing.T, consumer string, reclaim time.Duration) core.Worker {
				t.Helper()
				worker, err := valkeystream.NewWorkerE(
					valkeystream.WithAddress(address), valkeystream.WithStreamName(name),
					valkeystream.WithGroup("outbox-outboxqueue"),
					valkeystream.WithConsumer(consumer),
					valkeystream.WithMaxLength(durableStreamCapacity),
					valkeystream.WithRequestTimeout(5*time.Second),
					valkeystream.WithReclaim(reclaim, reclaim, int(durableStreamCapacity)),
				)
				if err != nil {
					t.Fatalf("create Valkey worker: %v", err)
				}

				return worker
			},
		}
		assertDurableRestartAndOrdering(t, backend)
		assertUnackedRedelivery(t, backend)
		assertRelayProcessWindows(t, backend)
		assertConcurrentRelays(t, backend)
		assertRelayShutdownRace(t, backend)
	})
}

func newRedisWorker(t *testing.T, address, stream string, extra ...redisstream.Option) *redisstream.Worker {
	t.Helper()
	options := []redisstream.Option{
		redisstream.WithAddr(address), redisstream.WithStreamName(stream),
		redisstream.WithGroup("outbox-outboxqueue"),
		redisstream.WithMaxLength(durableStreamCapacity),
		redisstream.WithRequestTimeout(5 * time.Second),
	}
	options = append(options, extra...)
	worker, err := redisstream.NewWorkerE(options...)
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

	publisher, err := outboxqueue.New(secondProducer.queue)
	if err != nil {
		t.Fatal(err)
	}
	closedErr := publisher.Publish(t.Context(), second)
	closedOutcome := outboxqueue.OutcomeOf(closedErr)
	if !errors.Is(closedErr, firstpartyqueue.ErrQueueShutdown) ||
		closedOutcome.Acceptance != outboxqueue.AcceptanceRejected ||
		closedOutcome.Disposition != outboxqueue.DispositionRetryable {
		t.Fatalf("closed producer error/outcome = %v/%#v", closedErr, closedOutcome)
	}

	consumer := backend.consumer(t, "restart-order", 30*time.Second)
	defer shutdownConsumer(t, consumer)
	tasks := make([]outboxqueue.Task, 0, 3)
	payloads := make([][]byte, 0, 3)
	for range 3 {
		delivery, requestErr := consumer.Request()
		if requestErr != nil {
			t.Fatalf("request durable task: %v", requestErr)
		}
		payload := append([]byte(nil), delivery.Payload()...)
		var task outboxqueue.Task
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

func assertUnackedRedelivery(t *testing.T, backend durableBackend) {
	t.Helper()
	t.Run("unacked delivery is reclaimed", func(t *testing.T) {
		producer := backend.producer(t)
		publishDurably(t, producer.queue, outbox.Envelope{
			ID: "unacked-redelivery", Topic: "events", PayloadVersion: 1,
		})
		producer.close()

		const reclaim = 50 * time.Millisecond
		first := backend.consumer(t, "abandoned-consumer", reclaim)
		delivery, err := first.Request()
		if err != nil {
			t.Fatalf("request first delivery: %v", err)
		}
		firstPayload := append([]byte(nil), delivery.Payload()...)
		if err := first.Shutdown(); err != nil {
			t.Fatalf("abandon first consumer: %v", err)
		}

		reclaimer := backend.consumer(t, "reclaiming-consumer", reclaim)
		defer shutdownConsumer(t, reclaimer)
		redelivery, err := reclaimer.Request()
		if err != nil {
			t.Fatalf("request reclaimed delivery: %v", err)
		}
		if !bytes.Equal(firstPayload, redelivery.Payload()) {
			t.Fatalf("reclaimed payload changed: %s != %s", firstPayload, redelivery.Payload())
		}
		acknowledger, ok := redelivery.(core.Acknowledger)
		if !ok || !acknowledger.AcknowledgementRequired() {
			t.Fatal("reclaimed delivery does not require acknowledgement")
		}
		if err := acknowledger.Ack(); err != nil {
			t.Fatalf("acknowledge reclaimed delivery: %v", err)
		}
	})
}

func assertRelayProcessWindows(t *testing.T, backend durableBackend) {
	t.Helper()
	t.Run("relay process windows", func(t *testing.T) {
		connectionString, pool := startProcessWindowPostgres(t)
		for _, test := range []struct {
			name         string
			stage        string
			wantMessages int
		}{
			{"before enqueue", "before-enqueue", 1},
			{"after enqueue before mark", "after-enqueue-before-mark", 2},
			{"after mark", "after-mark", 1},
		} {
			t.Run(test.name, func(t *testing.T) {
				id := backend.kind + "-" + test.stage
				insertOutboxEnvelope(t, pool, id)
				runRelayDeathProcess(t, connectionString, backend, test.stage)

				var state string
				if err := pool.QueryRow(t.Context(), "SELECT state FROM outbox_messages WHERE id = $1", id).Scan(&state); err != nil {
					t.Fatalf("read crashed relay state: %v", err)
				}
				if test.stage == "after-mark" {
					if state != "delivered" {
						t.Fatalf("state after delivered process death = %q", state)
					}
				} else {
					if state != "leased" {
						t.Fatalf("state after pre-delivery process death = %q", state)
					}
					if _, err := pool.Exec(t.Context(), "UPDATE outbox_messages SET leased_until = clock_timestamp() - interval '1 second' WHERE id = $1", id); err != nil {
						t.Fatalf("expire crashed relay lease: %v", err)
					}
				}

				producer := backend.producer(t)
				publisher, err := outboxqueue.New(producer.queue)
				if err != nil {
					t.Fatal(err)
				}
				store, err := postgres.NewStore(pool, postgres.StoreConfig{})
				if err != nil {
					t.Fatal(err)
				}
				result, runErr := newWindowRelay(t, store, publisher).RunOnce(t.Context())
				producer.close()
				if runErr != nil {
					t.Fatalf("recover crashed relay: %v", runErr)
				}
				wantRecovered := 1
				if test.stage == "after-mark" {
					wantRecovered = 0
				}
				if result.Delivered != wantRecovered {
					t.Fatalf("recovery result = %#v, want delivered %d", result, wantRecovered)
				}

				consumer := backend.consumer(t, "process-window-"+test.stage, 30*time.Second)
				payloads := consumeDurablePayloads(t, consumer, test.wantMessages)
				ids := make([]string, test.wantMessages)
				for index := range ids {
					ids[index] = id
				}
				assertTaskIDs(t, payloads, ids...)
				if len(payloads) == 2 && !bytes.Equal(payloads[0], payloads[1]) {
					t.Fatal("process-death duplicate changed canonical task bytes")
				}
			})
		}
	})
}

const relayDeathExitCode = 42

func TestGoqueueRelayDeathHelper(t *testing.T) {
	if os.Getenv("GOQUEUE_RELAY_DEATH_HELPER") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("GOQUEUE_POSTGRES_URL"))
	if err != nil {
		t.Fatalf("connect process helper store: %v", err)
	}
	defer pool.Close()
	store, err := postgres.NewStore(pool, postgres.StoreConfig{})
	if err != nil {
		t.Fatalf("create process helper store: %v", err)
	}
	queue, err := processHelperQueue(
		os.Getenv("GOQUEUE_BACKEND"), os.Getenv("GOQUEUE_BACKEND_ADDRESS"),
		os.Getenv("GOQUEUE_STREAM"),
	)
	if err != nil {
		t.Fatalf("create process helper queue: %v", err)
	}
	publisher, err := outboxqueue.New(queue)
	if err != nil {
		t.Fatalf("create process helper publisher: %v", err)
	}
	stage := os.Getenv("GOQUEUE_RELAY_DEATH_STAGE")
	workerStore := relay.Store(store)
	if stage == "after-mark" {
		workerStore = &processDeathStore{Store: store}
	}
	workerPublisher := relay.Publisher(publisher)
	if stage == "before-enqueue" || stage == "after-enqueue-before-mark" {
		workerPublisher = &processDeathPublisher{Publisher: publisher, stage: stage}
	}
	worker, err := relay.New(workerStore, workerPublisher, relay.Config{
		Owner: "doomed-relay", BatchSize: 1, Workers: 1,
		LeaseDuration: time.Second, LeaseRenewalInterval: 250 * time.Millisecond,
		ClassifyError: outboxqueue.ClassifyError,
	})
	if err != nil {
		t.Fatalf("create process helper relay: %v", err)
	}
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("run process helper relay: %v", err)
	}
	t.Fatalf("relay process did not exit at stage %q", stage)
}

type processDeathPublisher struct {
	relay.Publisher
	stage string
}

func (publisher *processDeathPublisher) Publish(ctx context.Context, envelope outbox.Envelope) error {
	if publisher.stage == "before-enqueue" {
		os.Exit(relayDeathExitCode)
	}
	if err := publisher.Publisher.Publish(ctx, envelope); err != nil {
		return err
	}
	os.Exit(relayDeathExitCode)
	return nil
}

type processDeathStore struct{ *postgres.Store }

func (store *processDeathStore) MarkDelivered(ctx context.Context, lease postgres.LeaseRef) error {
	if err := store.Store.MarkDelivered(ctx, lease); err != nil {
		return err
	}
	os.Exit(relayDeathExitCode)
	return nil
}

func processHelperQueue(kind, address, stream string) (outboxqueue.Queue, error) {
	switch kind {
	case "redis":
		worker, err := redisstream.NewWorkerE(
			redisstream.WithAddr(address), redisstream.WithStreamName(stream),
			redisstream.WithGroup("outbox-outboxqueue"),
			redisstream.WithMaxLength(durableStreamCapacity),
		)
		if err != nil {
			return nil, err
		}
		return firstpartyqueue.NewQueue(firstpartyqueue.WithWorker(worker))
	case "valkey":
		return valkeystream.NewPublisherE(
			valkeystream.WithAddress(address), valkeystream.WithStreamName(stream),
			valkeystream.WithMaxLength(durableStreamCapacity),
		)
	default:
		return nil, fmt.Errorf("unsupported process helper backend %q", kind)
	}
}

func startProcessWindowPostgres(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	image := os.Getenv("POSTGRES_IMAGE")
	if image == "" {
		image = "postgres:18.4-alpine@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15"
	}
	container, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase("outbox"), tcpostgres.WithUsername("outbox"),
		tcpostgres.WithPassword("outbox"), tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start process-window PostgreSQL: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelCleanup()
		if err := container.Terminate(cleanupContext); err != nil {
			t.Errorf("terminate process-window PostgreSQL: %v", err)
		}
	})
	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get process-window PostgreSQL connection: %v", err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatalf("connect process-window PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	contents, err := fs.ReadFile(postgres.Migrations(), "000001_create_outbox.sql")
	if err != nil {
		t.Fatalf("read outbox migration: %v", err)
	}
	const upMarker = "-- +migrations Up\n"
	const downMarker = "-- +migrations Down\n"
	down := strings.Index(string(contents), downMarker)
	if !strings.HasPrefix(string(contents), upMarker) || down < len(upMarker) {
		t.Fatal("canonical outbox migration has invalid directives")
	}
	if _, err := pool.Exec(ctx, string(contents[len(upMarker):down])); err != nil {
		t.Fatalf("apply outbox migration: %v", err)
	}
	return connectionString, pool
}

func insertOutboxEnvelope(t *testing.T, pool *pgxpool.Pool, id string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), `
INSERT INTO outbox_messages
    (id, topic, payload, payload_version, available_at, created_at)
VALUES ($1, 'events', '{}', 1, clock_timestamp(), clock_timestamp())`, id); err != nil {
		t.Fatalf("insert process-window envelope: %v", err)
	}
}

func runRelayDeathProcess(t *testing.T, connectionString string, backend durableBackend, stage string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGoqueueRelayDeathHelper$", "-test.v")
	command.Env = append(os.Environ(),
		"GOQUEUE_RELAY_DEATH_HELPER=1",
		"GOQUEUE_POSTGRES_URL="+connectionString,
		"GOQUEUE_BACKEND="+backend.kind,
		"GOQUEUE_BACKEND_ADDRESS="+backend.address,
		"GOQUEUE_STREAM="+backend.stream,
		"GOQUEUE_RELAY_DEATH_STAGE="+stage,
	)
	output, err := command.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != relayDeathExitCode {
		t.Fatalf("relay death stage %q error = %v, output:\n%s", stage, err, output)
	}
}

func assertConcurrentRelays(t *testing.T, backend durableBackend) {
	t.Helper()
	t.Run("concurrent relay instances claim disjoint durable work", func(t *testing.T) {
		_, pool := startProcessWindowPostgres(t)
		const messages = 16
		for index := range messages {
			insertOutboxEnvelope(t, pool, fmt.Sprintf("%s-concurrent-%02d", backend.kind, index))
		}
		store, err := postgres.NewStore(pool, postgres.StoreConfig{})
		if err != nil {
			t.Fatal(err)
		}
		producers := []durableProducer{backend.producer(t), backend.producer(t)}
		defer producers[0].close()
		defer producers[1].close()
		var wait sync.WaitGroup
		results := make(chan relay.Result, 2)
		errorsFound := make(chan error, 2)
		start := make(chan struct{})
		runContext := t.Context()
		for index := range 2 {
			publisher, publisherErr := outboxqueue.New(producers[index].queue)
			if publisherErr != nil {
				t.Fatal(publisherErr)
			}
			worker, relayErr := relay.New(store, publisher, relay.Config{
				Owner: fmt.Sprintf("concurrent-relay-%d", index), BatchSize: messages / 2, Workers: 8,
				ClassifyError: outboxqueue.ClassifyError,
			})
			if relayErr != nil {
				t.Fatal(relayErr)
			}
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				result, runErr := worker.RunOnce(runContext)
				results <- result
				errorsFound <- runErr
			}()
		}
		close(start)
		wait.Wait()
		close(results)
		close(errorsFound)
		for runErr := range errorsFound {
			if runErr != nil {
				t.Fatalf("concurrent relay: %v", runErr)
			}
		}
		delivered := 0
		for result := range results {
			if result.Claimed == 0 {
				t.Fatal("a concurrent relay claimed no work")
			}
			delivered += result.Delivered
		}
		if delivered != messages {
			t.Fatalf("concurrent relays delivered %d, want %d", delivered, messages)
		}
		var persisted int
		if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM outbox_messages WHERE state = 'delivered'").Scan(&persisted); err != nil {
			t.Fatalf("count concurrent deliveries: %v", err)
		}
		if persisted != messages {
			t.Fatalf("persisted concurrent deliveries = %d, want %d", persisted, messages)
		}
		consumer := backend.consumer(t, "concurrent-relays", 30*time.Second)
		payloads := consumeDurablePayloads(t, consumer, messages)
		seen := make(map[string]struct{}, messages)
		for _, payload := range payloads {
			var task outboxqueue.Task
			if err := json.Unmarshal(payload, &task); err != nil {
				t.Fatalf("decode concurrent task: %v", err)
			}
			if _, duplicate := seen[task.TaskID]; duplicate {
				t.Fatalf("concurrent relays duplicated task %q", task.TaskID)
			}
			seen[task.TaskID] = struct{}{}
		}
	})
}

func assertRelayShutdownRace(t *testing.T, backend durableBackend) {
	t.Helper()

	t.Run("relay and backend shutdown race", func(t *testing.T) {
		producer := backend.producer(t)
		gate := &shutdownRaceQueue{
			queue: producer.queue, firstAccepted: make(chan struct{}), release: make(chan struct{}),
		}
		publisher, err := outboxqueue.New(gate)
		if err != nil {
			t.Fatal(err)
		}
		store := newRelayBatchStore(32)
		worker, err := relay.New(store, publisher, relay.Config{
			Owner: "shutdown-race", BatchSize: 32, Workers: 8,
			ClassifyError: outboxqueue.ClassifyError,
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
			var task outboxqueue.Task
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
		ClassifyError: outboxqueue.ClassifyError,
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
		var task outboxqueue.Task
		if err := json.Unmarshal(payload, &task); err != nil || task.TaskID != ids[index] {
			t.Fatalf("durable task %d = %#v, error = %v", index, task, err)
		}
	}
}

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
	queue         outboxqueue.Queue
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

func publishDurably(t *testing.T, queue outboxqueue.Queue, envelope outbox.Envelope) {
	t.Helper()
	publisher, err := outboxqueue.New(queue)
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
