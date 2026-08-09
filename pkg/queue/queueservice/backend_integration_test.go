//go:build integration

package queueservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	redisstream "github.com/faustbrian/golib/pkg/queue/redisstream"
	"github.com/faustbrian/golib/pkg/queue/valkeystream"
	"github.com/faustbrian/golib/pkg/service"
)

var integrationIdentitySequence atomic.Uint64

func TestDurableBackendsDrainThroughQueueService(t *testing.T) {
	tests := []struct {
		name   string
		worker func(*testing.T, func(context.Context, core.TaskMessage) error) core.Worker
	}{
		{
			name: "redis-streams",
			worker: func(t *testing.T, handler func(context.Context, core.TaskMessage) error) core.Worker {
				t.Helper()
				identity := integrationIdentity(t)
				worker, err := redisstream.NewWorkerE(
					redisstream.WithAddr(requiredIntegrationAddress(t, "TEST_REDIS_ADDRESS")),
					redisstream.WithStreamName(identity),
					redisstream.WithGroup(identity+"-workers"),
					redisstream.WithConsumer(identity+"-consumer"),
					redisstream.WithBlockTime(10*time.Millisecond),
					redisstream.WithRequestTimeout(500*time.Millisecond),
					redisstream.WithRunFunc(handler),
				)
				if err != nil {
					t.Fatalf("NewWorkerE() error = %v", err)
				}

				return worker
			},
		},
		{
			name: "valkey-streams",
			worker: func(t *testing.T, handler func(context.Context, core.TaskMessage) error) core.Worker {
				t.Helper()
				identity := integrationIdentity(t)
				worker, err := valkeystream.NewWorkerE(
					valkeystream.WithAddress(requiredIntegrationAddress(t, "TEST_VALKEY_ADDRESS")),
					valkeystream.WithStreamName(identity),
					valkeystream.WithGroup(identity+"-workers"),
					valkeystream.WithConsumer(identity+"-consumer"),
					valkeystream.WithBlockTime(10*time.Millisecond),
					valkeystream.WithRequestTimeout(500*time.Millisecond),
					valkeystream.WithRunFunc(handler),
				)
				if err != nil {
					t.Fatalf("NewWorkerE() error = %v", err)
				}

				return worker
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handled := make(chan string, 1)
			backend := test.worker(t, func(_ context.Context, task core.TaskMessage) error {
				handled <- string(task.Payload())

				return nil
			})
			coordinator, err := queue.NewQueue(
				queue.WithWorker(backend),
				queue.WithWorkerCount(1),
			)
			if err != nil {
				t.Fatalf("queue.NewQueue() error = %v", err)
			}
			adapter, err := NewWorker(WorkerOptions{Name: test.name, Queue: coordinator})
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
			if err = coordinator.Queue(queuedPayload(test.name)); err != nil {
				t.Fatalf("Queue() error = %v", err)
			}
			select {
			case payload := <-handled:
				if payload != test.name {
					t.Fatalf("handled payload = %q, want %q", payload, test.name)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("durable backend did not deliver through the adapter")
			}
			if err = runtime.Drain(); err != nil {
				t.Fatalf("Drain() error = %v", err)
			}
			shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelShutdown()
			if err = runtime.Shutdown(shutdownContext); err != nil {
				t.Fatalf("Shutdown() error = %v", err)
			}
			if err = coordinator.Queue(queuedPayload("after-shutdown")); !errors.Is(err, queue.ErrQueueShutdown) {
				t.Fatalf("Queue() after shutdown error = %v, want ErrQueueShutdown", err)
			}
		})
	}
}

func TestDurableBackendAdaptersDisconnectAndReconnect(t *testing.T) {
	tests := []struct {
		name    string
		address string
		worker  func(*testing.T, string, func(context.Context, core.TaskMessage) error) (core.Worker, func(context.Context) error)
	}{
		{
			name:    "redis-streams",
			address: requiredIntegrationAddress(t, "TEST_REDIS_ADDRESS"),
			worker: func(t *testing.T, address string, handler func(context.Context, core.TaskMessage) error) (core.Worker, func(context.Context) error) {
				t.Helper()
				identity := integrationIdentity(t)
				worker, err := redisstream.NewWorkerE(
					redisstream.WithAddr(address),
					redisstream.WithStreamName(identity),
					redisstream.WithGroup(identity+"-workers"),
					redisstream.WithConsumer(identity+"-consumer"),
					redisstream.WithBlockTime(10*time.Millisecond),
					redisstream.WithRequestTimeout(100*time.Millisecond),
					redisstream.WithCommandTimeout(500*time.Millisecond),
					redisstream.WithRunFunc(handler),
				)
				if err != nil {
					t.Fatalf("NewWorkerE() error = %v", err)
				}

				return worker, func(ctx context.Context) error {
					_, statsErr := worker.Stats(ctx)

					return statsErr
				}
			},
		},
		{
			name:    "valkey-streams",
			address: requiredIntegrationAddress(t, "TEST_VALKEY_ADDRESS"),
			worker: func(t *testing.T, address string, handler func(context.Context, core.TaskMessage) error) (core.Worker, func(context.Context) error) {
				t.Helper()
				identity := integrationIdentity(t)
				worker, err := valkeystream.NewWorkerE(
					valkeystream.WithAddress(address),
					valkeystream.WithStreamName(identity),
					valkeystream.WithGroup(identity+"-workers"),
					valkeystream.WithConsumer(identity+"-consumer"),
					valkeystream.WithBlockTime(10*time.Millisecond),
					valkeystream.WithRequestTimeout(100*time.Millisecond),
					valkeystream.WithCommandTimeout(500*time.Millisecond),
					valkeystream.WithDialTimeout(500*time.Millisecond),
					valkeystream.WithReclaim(30*time.Second, 20*time.Millisecond, 16),
					valkeystream.WithRunFunc(handler),
				)
				if err != nil {
					t.Fatalf("NewWorkerE() error = %v", err)
				}

				return worker, func(ctx context.Context) error {
					_, statsErr := worker.Stats(ctx)

					return statsErr
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proxy := newBackendFaultProxy(t, test.address)
			handled := make(chan string, 8)
			backend, check := test.worker(t, proxy.Address(), func(_ context.Context, task core.TaskMessage) error {
				handled <- string(task.Payload())

				return nil
			})
			coordinator, err := queue.NewQueue(
				queue.WithWorker(backend),
				queue.WithWorkerCount(1),
			)
			if err != nil {
				t.Fatalf("queue.NewQueue() error = %v", err)
			}
			adapter, err := NewWorker(WorkerOptions{Name: test.name, Queue: coordinator})
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
			if err = coordinator.Queue(queuedPayload("before-outage")); err != nil {
				t.Fatalf("Queue() before outage error = %v", err)
			}
			awaitIntegrationPayload(t, handled, "before-outage")
			awaitBackendCheck(t, check)

			proxy.Pause()
			if err = coordinator.Queue(queuedPayload("during-outage")); err == nil {
				t.Fatal("Queue() during disconnected backend unexpectedly succeeded")
			}
			proxy.Resume()
			awaitBackendCheck(t, check)
			if err = coordinator.Queue(queuedPayload("after-reconnect")); err != nil {
				t.Fatalf("Queue() after reconnect error = %v", err)
			}
			awaitIntegrationPayload(t, handled, "after-reconnect")

			shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelShutdown()
			if err = runtime.Shutdown(shutdownContext); err != nil {
				t.Fatalf("Shutdown() error = %v", err)
			}
		})
	}
}

func awaitBackendCheck(t *testing.T, check func(context.Context) error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		err := check(ctx)
		cancel()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("backend did not recover: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func awaitIntegrationPayload(t *testing.T, handled <-chan string, wanted string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case payload := <-handled:
			if payload == wanted {
				return
			}
		case <-deadline:
			t.Fatalf("durable backend did not deliver %q", wanted)
		}
	}
}

type backendFaultProxy struct {
	listener net.Listener
	target   string

	mu     sync.Mutex
	paused bool
	closed bool
	active map[net.Conn]struct{}
	wait   sync.WaitGroup
}

func newBackendFaultProxy(t *testing.T, target string) *backendFaultProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for backend fault proxy: %v", err)
	}
	proxy := &backendFaultProxy{
		listener: listener,
		target:   target,
		active:   make(map[net.Conn]struct{}),
	}
	proxy.wait.Add(1)
	go proxy.accept()
	t.Cleanup(proxy.Close)

	return proxy
}

func (proxy *backendFaultProxy) Address() string { return proxy.listener.Addr().String() }

func (proxy *backendFaultProxy) Pause() {
	proxy.mu.Lock()
	proxy.paused = true
	connections := make([]net.Conn, 0, len(proxy.active))
	for connection := range proxy.active {
		connections = append(connections, connection)
	}
	proxy.mu.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
}

func (proxy *backendFaultProxy) Resume() {
	proxy.mu.Lock()
	proxy.paused = false
	proxy.mu.Unlock()
}

func (proxy *backendFaultProxy) Close() {
	proxy.mu.Lock()
	if proxy.closed {
		proxy.mu.Unlock()

		return
	}
	proxy.closed = true
	connections := make([]net.Conn, 0, len(proxy.active))
	for connection := range proxy.active {
		connections = append(connections, connection)
	}
	proxy.mu.Unlock()
	_ = proxy.listener.Close()
	for _, connection := range connections {
		_ = connection.Close()
	}
	proxy.wait.Wait()
}

func (proxy *backendFaultProxy) accept() {
	defer proxy.wait.Done()
	for {
		client, err := proxy.listener.Accept()
		if err != nil {
			return
		}
		proxy.mu.Lock()
		paused := proxy.paused
		proxy.mu.Unlock()
		if paused {
			_ = client.Close()

			continue
		}
		backend, err := net.DialTimeout("tcp", proxy.target, time.Second)
		if err != nil {
			_ = client.Close()

			continue
		}
		if !proxy.track(client, backend) {
			_ = client.Close()
			_ = backend.Close()

			continue
		}
		proxy.wait.Add(2)
		go proxy.copy(client, backend)
		go proxy.copy(backend, client)
	}
}

func (proxy *backendFaultProxy) track(connections ...net.Conn) bool {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	if proxy.paused || proxy.closed {
		return false
	}
	for _, connection := range connections {
		proxy.active[connection] = struct{}{}
	}

	return true
}

func (proxy *backendFaultProxy) copy(destination, source net.Conn) {
	defer proxy.wait.Done()
	_, _ = io.Copy(destination, source)
	_ = destination.Close()
	_ = source.Close()
	proxy.mu.Lock()
	delete(proxy.active, destination)
	delete(proxy.active, source)
	proxy.mu.Unlock()
}

func integrationIdentity(t *testing.T) string {
	t.Helper()

	return fmt.Sprintf(
		"queueservice-%d-%d-%s",
		os.Getpid(),
		integrationIdentitySequence.Add(1),
		strings.NewReplacer("/", "-", "_", "-").Replace(t.Name()),
	)
}

func requiredIntegrationAddress(t *testing.T, name string) string {
	t.Helper()
	address := os.Getenv(name)
	if address == "" {
		t.Fatalf("%s is required for durable backend integration", name)
	}

	return address
}
