//go:build integration

package goqueue

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	queuepkg "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
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
		t, endpoint, "event-sourcing", "producer-observer", "producer", nil,
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
	consumerEntered := make(chan struct{}, 1)
	releaseConsumer := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-releaseConsumer:
		default:
			close(releaseConsumer)
		}
	})
	handler, err := NewTaskHandler(
		codec,
		func(_ context.Context, delivery eventsourcing.Delivery) error {
			received <- delivery
			consumerEntered <- struct{}{}
			<-releaseConsumer
			return nil
		},
	)
	if err != nil {
		t.Fatalf("NewTaskHandler() error = %v", err)
	}
	consumerWorker := newValkeyIntegrationWorker(
		t, endpoint, "event-sourcing", "event-sourcing-workers", "consumer",
		handler.Handle,
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
	case <-consumerEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("event consumer did not start")
	}
	stats, err := consumerWorker.Stats(t.Context())
	if err != nil {
		t.Fatalf("Stats(active consumer) error = %v", err)
	}
	if stats.Pending != 1 || stats.Acknowledged != 0 {
		t.Fatalf("Valkey Streams acknowledged before completion: %#v", stats)
	}
	close(releaseConsumer)
	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("Valkey Streams did not settle the retained event")
	}
	stats, err = consumerWorker.Stats(t.Context())
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats.Pending != 0 || stats.Acknowledged != 1 {
		t.Fatalf("Valkey Streams settlement stats = %#v", stats)
	}
}

func TestValkeyStreamFailuresRemainEligibleForRedelivery(t *testing.T) {
	endpoint := startValkeyIntegrationContainer(t)
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	want := queueDelivery(t, eventsourcing.DeliveryLive)
	producerWorker := newValkeyIntegrationWorker(
		t, endpoint, "event-redelivery", "producer-observer", "producer", nil,
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
	dispatcher, err := NewDispatcher(DispatcherConfig{
		Queue: producer,
		Codec: codec,
		Job: job.AllowOption{Metadata: &job.Metadata{
			RetryPolicy: "projection-v1",
			HandlerType: "account-projector",
		}},
	})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	if err := dispatcher.Dispatch(t.Context(), []eventsourcing.Delivery{want}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	producer.Release()

	cases := []struct {
		name     string
		consumer eventsourcing.ConsumerFunc
		terminal bool
	}{
		{name: "error", consumer: func(context.Context, eventsourcing.Delivery) error {
			return errors.New("consumer failed")
		}},
		{name: "cancellation", consumer: func(context.Context, eventsourcing.Delivery) error {
			return context.Canceled
		}},
		{name: "panic", consumer: func(context.Context, eventsourcing.Delivery) error {
			panic("consumer panic")
		}},
		{name: "retry request", terminal: true, consumer: func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return management.NewFailure(
				management.ClassificationRetryable,
				"retry_requested",
				errors.New("retry requested"),
			)
		}},
	}
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			group := fmt.Sprintf("redelivery-%d", index)
			handler, handlerErr := NewTaskHandler(codec, test.consumer)
			if handlerErr != nil {
				t.Fatalf("NewTaskHandler() error = %v", handlerErr)
			}
			failedWorker := newValkeyIntegrationWorker(
				t, endpoint, "event-redelivery", group, "failing", handler.Handle,
			)
			failedDone := make(chan struct{}, 1)
			failedQueue, queueErr := queuepkg.NewQueue(
				queuepkg.WithWorker(failedWorker),
				queuepkg.WithWorkerCount(1),
				queuepkg.WithLogger(queuepkg.NewEmptyLogger()),
				queuepkg.WithAfterFn(func() { failedDone <- struct{}{} }),
			)
			if queueErr != nil {
				t.Fatalf("NewQueue(failing) error = %v", queueErr)
			}
			failedQueue.Start()
			select {
			case <-failedDone:
			case <-time.After(5 * time.Second):
				t.Fatal("Valkey Streams did not return the failed attempt")
			}
			stats, statsErr := failedWorker.Stats(t.Context())
			if statsErr != nil {
				t.Fatalf("Stats(failed) error = %v", statsErr)
			}
			if stats.Pending != 1 || stats.Acknowledged != 0 {
				t.Fatalf("failed delivery settlement = %#v", stats)
			}
			assertFailureMetadata(t, failedWorker)
			failedQueue.Release()

			recovered := make(chan eventsourcing.Delivery, 1)
			recoveryConsumer := eventsourcing.ConsumerFunc(func(
				_ context.Context,
				delivery eventsourcing.Delivery,
			) error {
				select {
				case recovered <- delivery:
				default:
				}
				if test.terminal {
					return test.consumer(context.Background(), delivery)
				}
				return nil
			})
			recoveryHandler, handlerErr := NewTaskHandler(codec, recoveryConsumer)
			if handlerErr != nil {
				t.Fatalf("NewTaskHandler(recovery) error = %v", handlerErr)
			}
			recoveryWorker := newValkeyIntegrationWorkerWithReclaim(
				t, endpoint, "event-redelivery", group, "recovery",
				recoveryHandler.Handle, time.Millisecond, time.Millisecond,
			)
			recoveryDone := make(chan struct{}, 1)
			recoveryQueue, queueErr := queuepkg.NewQueue(
				queuepkg.WithWorker(recoveryWorker),
				queuepkg.WithWorkerCount(1),
				queuepkg.WithLogger(queuepkg.NewEmptyLogger()),
				queuepkg.WithAfterFn(func() { recoveryDone <- struct{}{} }),
			)
			if queueErr != nil {
				t.Fatalf("NewQueue(recovery) error = %v", queueErr)
			}
			recoveryQueue.Start()
			select {
			case got := <-recovered:
				if !got.Message().Equal(want.Message()) || got.Mode() != want.Mode() {
					t.Fatalf("recovered delivery = %#v", got)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Valkey Streams did not redeliver the failed event")
			}
			select {
			case <-recoveryDone:
			case <-time.After(5 * time.Second):
				t.Fatal("Valkey Streams did not settle the redelivery")
			}
			stats, statsErr = recoveryWorker.Stats(t.Context())
			if statsErr != nil {
				t.Fatalf("Stats(recovery) error = %v", statsErr)
			}
			if stats.Reclaimed < 1 || stats.Retries < 1 {
				t.Fatalf("Valkey Streams redelivery stats = %#v", stats)
			}
			if test.terminal {
				if stats.Pending != 0 || stats.DeadLettered != 1 {
					t.Fatalf("Valkey Streams dead letter stats = %#v", stats)
				}
				assertDeadLetterMetadata(t, recoveryWorker)
			} else if stats.Pending != 0 || stats.Acknowledged != 1 {
				t.Fatalf("Valkey Streams recovery settlement = %#v", stats)
			}
			recoveryQueue.Release()
		})
	}
}

func assertFailureMetadata(t *testing.T, worker *valkeystream.Worker) {
	t.Helper()
	records, err := worker.ListFailures(t.Context(), management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt,
		Direction: management.SortAscending,
	})
	if err != nil || len(records.Items) != 1 {
		t.Fatalf("ListFailures() = %#v, %v", records, err)
	}
	assertOperationalMetadata(t, records.Items[0])
}

func assertDeadLetterMetadata(t *testing.T, worker *valkeystream.Worker) {
	t.Helper()
	records, err := worker.ListDeadLetters(t.Context(), management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt,
		Direction: management.SortAscending,
	})
	if err != nil || len(records.Items) != 1 {
		t.Fatalf("ListDeadLetters() = %#v, %v", records, err)
	}
	assertOperationalMetadata(t, records.Items[0])
}

func assertOperationalMetadata(t *testing.T, record management.JobRecord) {
	t.Helper()
	if record.OriginalID != "message-1" ||
		record.PayloadSchemaVersion != "2" ||
		record.JobType != "account.opened" ||
		record.RetryPolicy != "projection-v1" ||
		record.HandlerType != "account-projector" ||
		record.TenantID != "tenant-1" {
		t.Fatalf("operational metadata = %#v", record)
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
	stream string,
	group string,
	consumer string,
	run func(context.Context, core.TaskMessage) error,
) *valkeystream.Worker {
	t.Helper()
	return newValkeyIntegrationWorkerWithReclaim(
		t, endpoint, stream, group, consumer, run, time.Hour, time.Hour,
	)
}

func newValkeyIntegrationWorkerWithReclaim(
	t *testing.T,
	endpoint string,
	stream string,
	group string,
	consumer string,
	run func(context.Context, core.TaskMessage) error,
	reclaimIdle time.Duration,
	reclaimInterval time.Duration,
) *valkeystream.Worker {
	t.Helper()
	options := []valkeystream.Option{
		valkeystream.WithAddress(endpoint),
		valkeystream.WithStreamName(stream),
		valkeystream.WithGroup(group),
		valkeystream.WithConsumer(consumer),
		valkeystream.WithBlockTime(20 * time.Millisecond),
		valkeystream.WithRequestTimeout(5 * time.Second),
		valkeystream.WithCommandTimeout(time.Second),
		valkeystream.WithDialTimeout(time.Second),
		valkeystream.WithShutdownTimeout(time.Second),
		valkeystream.WithReclaim(reclaimIdle, reclaimInterval, 8),
		valkeystream.WithFailureStream(stream + "-failures"),
		valkeystream.WithDeadLetter(stream+"-dead", 2),
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
