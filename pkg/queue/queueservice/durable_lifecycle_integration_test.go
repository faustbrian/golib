//go:build integration

package queueservice

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/faustbrian/golib/pkg/service"
	"github.com/redis/go-redis/v9"
)

func TestDurableBackendHandlerTimeoutRedeliversAfterLeaseExpiry(t *testing.T) {
	for _, backend := range durableBackendIntegrationCases(t) {
		t.Run(backend.name, func(t *testing.T) {
			identity := integrationIdentity(t)
			stream := identity + "-jobs"
			group := identity + "-workers"
			client := redis.NewClient(&redis.Options{Addr: backend.address})
			t.Cleanup(func() { _ = client.Close() })
			var attempts atomic.Int64

			firstSettled := make(chan struct{})
			first := newDurableProcessBackendWorker(
				t,
				backend.name,
				backend.address,
				stream,
				"timeout-owner",
				func(ctx context.Context, _ core.TaskMessage) error {
					attempts.Add(1)
					<-ctx.Done()

					return ctx.Err()
				},
				false,
				3,
			)
			firstRuntime, firstQueue := startDurableAdapterRuntime(
				t, identity+"-first", first, firstSettled,
			)
			if err := firstQueue.Queue(
				queuedPayload("timeout-redelivery"),
				job.AllowOption{Timeout: job.Time(50 * time.Millisecond)},
			); err != nil {
				t.Fatalf("Queue() timed work error = %v", err)
			}
			awaitDurableSettlement(t, firstSettled)
			shutdownDurableAdapterRuntime(t, firstRuntime)
			awaitDurablePending(t, client, stream, group, 1)
			if attempts.Load() != 1 {
				t.Fatalf("timed handler attempts = %d, want 1", attempts.Load())
			}
			awaitDurableLeaseExpiry(t, client, stream, group, durableProcessLease)

			recoveredSettled := make(chan struct{})
			recovered := newDurableProcessBackendWorker(
				t,
				backend.name,
				backend.address,
				stream,
				"timeout-recovery",
				func(context.Context, core.TaskMessage) error {
					attempts.Add(1)

					return nil
				},
				true,
				3,
			)
			recoveredRuntime, _ := startDurableAdapterRuntime(
				t, identity+"-recovered", recovered, recoveredSettled,
			)
			awaitDurableSettlement(t, recoveredSettled)
			shutdownDurableAdapterRuntime(t, recoveredRuntime)
			awaitDurablePending(t, client, stream, group, 0)
			if attempts.Load() != 2 {
				t.Fatalf("handler attempts after redelivery = %d, want 2", attempts.Load())
			}
		})
	}
}

func TestDurableBackendDeadLetterFailureLeavesWorkForRecovery(t *testing.T) {
	for _, backend := range durableBackendIntegrationCases(t) {
		t.Run(backend.name, func(t *testing.T) {
			identity := integrationIdentity(t)
			stream := identity + "-jobs"
			group := identity + "-workers"
			deadLetters := stream + "-dead"
			client := redis.NewClient(&redis.Options{Addr: backend.address})
			t.Cleanup(func() { _ = client.Close() })
			if err := client.Set(t.Context(), deadLetters, "wrong-type", 0).Err(); err != nil {
				t.Fatalf("create unavailable dead-letter destination: %v", err)
			}
			terminal := management.NewFailure(
				management.ClassificationPermanent,
				"integration_terminal",
				errors.New("terminal handler failure"),
			)
			var attempts atomic.Int64
			firstSettled := make(chan struct{})
			first := newDurableProcessBackendWorker(
				t,
				backend.name,
				backend.address,
				stream,
				"dead-letter-owner",
				func(context.Context, core.TaskMessage) error {
					attempts.Add(1)

					return terminal
				},
				false,
				2,
			)
			firstRuntime, firstQueue := startDurableAdapterRuntime(
				t, identity+"-first", first, firstSettled,
			)
			if err := firstQueue.Queue(queuedPayload("dead-letter-recovery")); err != nil {
				t.Fatalf("Queue() terminal work error = %v", err)
			}
			awaitDurableSettlement(t, firstSettled)
			shutdownDurableAdapterRuntime(t, firstRuntime)
			awaitDurablePending(t, client, stream, group, 1)
			if attempts.Load() != 1 {
				t.Fatalf("terminal handler attempts = %d, want 1", attempts.Load())
			}
			if err := client.Del(t.Context(), deadLetters).Err(); err != nil {
				t.Fatalf("restore dead-letter destination: %v", err)
			}
			awaitDurableLeaseExpiry(t, client, stream, group, durableProcessLease)

			recoveredSettled := make(chan struct{})
			recovered := newDurableProcessBackendWorker(
				t,
				backend.name,
				backend.address,
				stream,
				"dead-letter-recovery",
				func(context.Context, core.TaskMessage) error {
					attempts.Add(1)

					return terminal
				},
				true,
				2,
			)
			recoveredRuntime, _ := startDurableAdapterRuntime(
				t, identity+"-recovered", recovered, recoveredSettled,
			)
			awaitDurableSettlement(t, recoveredSettled)
			shutdownDurableAdapterRuntime(t, recoveredRuntime)
			awaitDurablePending(t, client, stream, group, 0)
			length, err := client.XLen(t.Context(), deadLetters).Result()
			if err != nil {
				t.Fatalf("read recovered dead-letter destination: %v", err)
			}
			if length != 1 || attempts.Load() != 2 {
				t.Fatalf(
					"dead letters/attempts = %d/%d, want 1/2",
					length,
					attempts.Load(),
				)
			}
		})
	}
}

func TestDurableBackendScaleAndRollingDeploymentRetainOneSettlementOwner(t *testing.T) {
	const (
		batchSize          = 32
		scaleRecoveryLease = 3 * time.Second
	)
	for _, backend := range durableBackendIntegrationCases(t) {
		t.Run(backend.name, func(t *testing.T) {
			identity := integrationIdentity(t)
			stream := identity + "-jobs"
			releaseBlocked := make(chan struct{})
			blockedEntered := make(chan struct{}, 2)
			var blocked atomic.Int64
			var handledMu sync.Mutex
			handled := make(map[string]int, batchSize*3)
			handledEvents := make(chan struct{}, batchSize*3)
			handler := func(_ context.Context, task core.TaskMessage) error {
				if blocked.Add(1) <= 2 {
					blockedEntered <- struct{}{}
					<-releaseBlocked
				}
				handledMu.Lock()
				handled[string(task.Payload())]++
				handledMu.Unlock()
				handledEvents <- struct{}{}

				return nil
			}
			type runningAdapter struct {
				runtime     *service.Service
				coordinator *queue.Queue
			}
			start := func(role string, index int) runningAdapter {
				worker := newDurableProcessBackendWorkerWithLease(
					t,
					backend.name,
					backend.address,
					stream,
					fmt.Sprintf("%s-%d", role, index),
					handler,
					true,
					scaleRecoveryLease,
					3,
				)
				runtime, coordinator := startDurableAdapterRuntime(
					t,
					fmt.Sprintf("%s-%s-%d", identity, role, index),
					worker,
					make(chan struct{}),
				)

				return runningAdapter{runtime: runtime, coordinator: coordinator}
			}

			old := []runningAdapter{start("old", 0), start("old", 1)}
			enqueueDurableScaleBatch(t, old[0].coordinator, "initial", batchSize)
			awaitDurableHandlerAdmissions(t, blockedEntered, 2)
			current := []runningAdapter{start("new", 0), start("new", 1)}

			oldStop := make(chan error, 1)
			go func() { oldStop <- shutdownDurableAdapterRuntimeResult(old[0].runtime) }()
			awaitDurableRuntimeState(t, old[0].runtime, service.StateStopping)
			select {
			case err := <-oldStop:
				t.Fatalf("scale-down returned before admitted handler completed: %v", err)
			default:
			}
			close(releaseBlocked)
			if err := awaitDurableShutdown(t, oldStop); err != nil {
				t.Fatalf("scale-down Shutdown() error = %v", err)
			}

			enqueueDurableScaleBatch(t, old[1].coordinator, "scaled", batchSize)
			awaitDurableHandledCount(t, handledEvents, batchSize*2)
			shutdownDurableAdapterRuntime(t, old[1].runtime)
			enqueueDurableScaleBatch(t, current[0].coordinator, "rolled", batchSize)
			awaitDurableHandledCount(t, handledEvents, batchSize)
			shutdownDurableAdapterRuntime(t, current[0].runtime)
			shutdownDurableAdapterRuntime(t, current[1].runtime)

			handledMu.Lock()
			defer handledMu.Unlock()
			if len(handled) != batchSize*3 {
				t.Fatalf("unique handled work = %d, want %d", len(handled), batchSize*3)
			}
			for payload, count := range handled {
				if count != 1 {
					t.Fatalf("handled %q %d times, want exactly once", payload, count)
				}
			}
		})
	}
}

type durableBackendIntegrationCase struct {
	name    string
	address string
}

func durableBackendIntegrationCases(t *testing.T) []durableBackendIntegrationCase {
	t.Helper()

	return []durableBackendIntegrationCase{
		{name: "redis-streams", address: requiredIntegrationAddress(t, "TEST_REDIS_ADDRESS")},
		{name: "valkey-streams", address: requiredIntegrationAddress(t, "TEST_VALKEY_ADDRESS")},
	}
}

func startDurableAdapterRuntime(
	t *testing.T,
	name string,
	backend core.Worker,
	settled chan struct{},
) (*service.Service, *queue.Queue) {
	t.Helper()
	var settlementOnce sync.Once
	coordinator, err := queue.NewQueue(
		queue.WithWorker(backend),
		queue.WithWorkerCount(1),
		queue.WithLogger(queue.NewEmptyLogger()),
		queue.WithAfterFn(func() {
			settlementOnce.Do(func() { close(settled) })
		}),
	)
	if err != nil {
		t.Fatalf("queue.NewQueue() error = %v", err)
	}
	adapter, err := NewWorker(WorkerOptions{Name: name, Queue: coordinator})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	runtime, err := service.New(service.Config{
		Components: []service.Component{adapter.Component()},
	})
	if err != nil {
		t.Fatalf("service.New() error = %v", err)
	}
	if err = runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = runtime.Shutdown(ctx)
	})

	return runtime, coordinator
}

func shutdownDurableAdapterRuntime(t *testing.T, runtime *service.Service) {
	t.Helper()
	if err := shutdownDurableAdapterRuntimeResult(runtime); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func shutdownDurableAdapterRuntimeResult(runtime *service.Service) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return runtime.Shutdown(ctx)
}

func awaitDurableSettlement(t *testing.T, settled <-chan struct{}) {
	t.Helper()
	select {
	case <-settled:
	case <-time.After(5 * time.Second):
		t.Fatal("durable backend did not finish handler settlement")
	}
}

func enqueueDurableScaleBatch(
	t *testing.T,
	coordinator *queue.Queue,
	prefix string,
	count int,
) {
	t.Helper()
	for index := range count {
		if err := coordinator.Queue(queuedPayload(fmt.Sprintf("%s-%d", prefix, index))); err != nil {
			t.Fatalf("Queue(%s, %d) error = %v", prefix, index, err)
		}
	}
}

func awaitDurableHandlerAdmissions(t *testing.T, admitted <-chan struct{}, count int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for range count {
		select {
		case <-admitted:
		case <-deadline:
			t.Fatal("durable scale workers did not admit blocked handlers")
		}
	}
}

func awaitDurableHandledCount(t *testing.T, handled <-chan struct{}, count int) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for range count {
		select {
		case <-handled:
		case <-deadline:
			t.Fatalf("durable scale workers handled fewer than %d expected messages", count)
		}
	}
}

func awaitDurableShutdown(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("durable adapter shutdown did not finish")

		return nil
	}
}

func awaitDurableRuntimeState(
	t *testing.T,
	runtime *service.Service,
	wanted service.State,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.State() != wanted {
		if time.Now().After(deadline) {
			t.Fatalf("service state = %s, want %s", runtime.State(), wanted)
		}
		time.Sleep(time.Millisecond)
	}
}
