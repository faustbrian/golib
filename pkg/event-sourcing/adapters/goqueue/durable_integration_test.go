//go:build integration

package goqueue

import (
	"context"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	queuepkg "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/valkeystream"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const valkeyIntegrationImage = "valkey/valkey:9.1.0@sha256:8e8d64b405ce18f41b8e5ee20aa4687a8ed0022d1298f2ce31cdcf3a76e09411"

func TestValkeyStreamRetainsAndSettlesCompleteDelivery(t *testing.T) {
	endpoint := startValkeyIntegrationContainer(t)

	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	want := queueDelivery(t, eventsourcing.DeliveryLive)

	producerWorker := newValkeyIntegrationWorker(
		t, endpoint, "producer-observer", "producer", nil,
	)
	producer, err := queuepkg.NewQueue(
		queuepkg.WithWorker(producerWorker),
		queuepkg.WithWorkerCount(0),
		queuepkg.WithLogger(queuepkg.NewEmptyLogger()),
	)
	if err != nil {
		t.Fatalf("NewQueue(producer) error = %v", err)
	}
	t.Cleanup(producer.Release)
	dispatcher, err := NewDispatcher(DispatcherConfig{Queue: producer, Codec: codec})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	if err := dispatcher.Dispatch(t.Context(), []eventsourcing.Delivery{want}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	producer.Release()

	received := make(chan eventsourcing.Delivery, 1)
	handler, err := NewTaskHandler(
		codec,
		func(_ context.Context, delivery eventsourcing.Delivery) error {
			received <- delivery
			return nil
		},
	)
	if err != nil {
		t.Fatalf("NewTaskHandler() error = %v", err)
	}
	consumerWorker := newValkeyIntegrationWorker(
		t, endpoint, "event-sourcing-workers", "consumer", handler.Handle,
	)
	completed := make(chan struct{}, 1)
	consumer, err := queuepkg.NewQueue(
		queuepkg.WithWorker(consumerWorker),
		queuepkg.WithWorkerCount(1),
		queuepkg.WithLogger(queuepkg.NewEmptyLogger()),
		queuepkg.WithAfterFn(func() { completed <- struct{}{} }),
	)
	if err != nil {
		t.Fatalf("NewQueue(consumer) error = %v", err)
	}
	consumer.Start()
	t.Cleanup(consumer.Release)

	select {
	case got := <-received:
		if !got.Message().Equal(want.Message()) || got.Mode() != want.Mode() {
			t.Fatalf("received delivery = %#v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Valkey Streams did not deliver the retained event")
	}
	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("Valkey Streams did not settle the retained event")
	}
	stats, err := consumerWorker.Stats(t.Context())
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.Pending != 0 || stats.Acknowledged != 1 {
		t.Fatalf("Valkey Streams settlement stats = %#v", stats)
	}
}

func startValkeyIntegrationContainer(t *testing.T) string {
	t.Helper()
	container, err := testcontainers.GenericContainer(
		t.Context(),
		testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        valkeyIntegrationImage,
				ExposedPorts: []string{"6379/tcp"},
				Cmd:          []string{"valkey-server", "--appendonly", "yes"},
				WaitingFor: wait.ForLog("Ready to accept connections").
					WithStartupTimeout(2 * time.Minute),
			},
			Started: true,
		},
	)
	if err != nil {
		t.Fatalf("start Valkey container: %v", err)
	}
	t.Cleanup(func() { testcontainers.CleanupContainer(t, container) })
	endpoint, err := container.PortEndpoint(t.Context(), "6379/tcp", "")
	if err != nil {
		t.Fatalf("resolve Valkey endpoint: %v", err)
	}
	return endpoint
}

func newValkeyIntegrationWorker(
	t *testing.T,
	endpoint string,
	group string,
	consumer string,
	run func(context.Context, core.TaskMessage) error,
) *valkeystream.Worker {
	t.Helper()
	options := []valkeystream.Option{
		valkeystream.WithAddress(endpoint),
		valkeystream.WithStreamName("event-sourcing"),
		valkeystream.WithGroup(group),
		valkeystream.WithConsumer(consumer),
		valkeystream.WithBlockTime(20 * time.Millisecond),
		valkeystream.WithRequestTimeout(5 * time.Second),
		valkeystream.WithCommandTimeout(time.Second),
		valkeystream.WithDialTimeout(time.Second),
		valkeystream.WithShutdownTimeout(time.Second),
		valkeystream.WithReclaim(time.Hour, time.Hour, 8),
		valkeystream.WithFailureStream("event-sourcing-failures"),
		valkeystream.WithDeadLetter("event-sourcing-dead", 2),
		valkeystream.WithLogger(queuepkg.NewEmptyLogger()),
	}
	if run != nil {
		options = append(options, valkeystream.WithRunFunc(run))
	}
	worker, err := valkeystream.NewWorkerE(options...)
	if err != nil {
		t.Fatalf("NewWorkerE() error = %v", err)
	}
	return worker
}
