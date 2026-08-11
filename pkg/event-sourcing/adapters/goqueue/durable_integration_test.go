//go:build integration

package goqueue

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	_, endpoint := startValkeyIntegrationContainer(t)

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
	_, endpoint := startValkeyIntegrationContainer(t)
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

func TestValkeyStreamRecoversAcrossWorkerProcessDeathWindows(t *testing.T) {
	_, endpoint := startValkeyIntegrationContainer(t)
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	cases := []struct {
		name             string
		mode             string
		exitCode         int
		wantRedelivery   bool
		wantEffectBefore bool
	}{
		{
			name:           "before application effect and settlement",
			mode:           "before-effect",
			exitCode:       21,
			wantRedelivery: true,
		},
		{
			name:             "after application effect before settlement",
			mode:             "after-effect",
			exitCode:         22,
			wantRedelivery:   true,
			wantEffectBefore: true,
		},
		{
			name:             "after handler success before settlement",
			mode:             "after-handler",
			exitCode:         24,
			wantRedelivery:   true,
			wantEffectBefore: true,
		},
		{
			name:             "after application effect and settlement",
			mode:             "after-settlement",
			exitCode:         23,
			wantEffectBefore: true,
		},
	}
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			stream := fmt.Sprintf("process-death-%d", index)
			group := fmt.Sprintf("process-death-workers-%d", index)
			producer := newValkeyIntegrationPublisher(t, endpoint, stream)
			dispatcher, constructErr := NewDispatcher(DispatcherConfig{
				Queue: producer,
				Codec: codec,
			})
			if constructErr != nil {
				t.Fatalf("NewDispatcher() error = %v", constructErr)
			}
			if dispatchErr := dispatcher.Dispatch(
				t.Context(),
				[]eventsourcing.Delivery{minimalQueueDelivery(t)},
			); dispatchErr != nil {
				t.Fatalf("Dispatch() error = %v", dispatchErr)
			}
			if shutdownErr := producer.Shutdown(); shutdownErr != nil {
				t.Fatalf("producer Shutdown() error = %v", shutdownErr)
			}

			marker := t.TempDir() + "/effect"
			runCrashWorker(
				t,
				endpoint,
				stream,
				group,
				test.mode,
				marker,
				test.exitCode,
			)
			_, markerErr := os.ReadFile(marker)
			if test.wantEffectBefore && markerErr != nil {
				t.Fatalf("application effect before death: %v", markerErr)
			}
			if !test.wantEffectBefore && !errors.Is(markerErr, os.ErrNotExist) {
				t.Fatalf("unexpected application effect before death: %v", markerErr)
			}

			time.Sleep(30 * time.Millisecond)
			recovered := make(chan eventsourcing.Delivery, 1)
			recoveryHandler, handlerErr := NewTaskHandler(
				codec,
				func(_ context.Context, delivery eventsourcing.Delivery) error {
					messageID := delivery.Message().ID().String()
					existing, readErr := os.ReadFile(marker)
					if readErr == nil {
						if string(existing) != messageID {
							return errors.New("idempotency record does not match delivery")
						}
					} else if errors.Is(readErr, os.ErrNotExist) {
						if writeErr := os.WriteFile(
							marker,
							[]byte(messageID),
							0o600,
						); writeErr != nil {
							return writeErr
						}
					} else {
						return readErr
					}
					select {
					case recovered <- delivery:
					default:
					}
					return nil
				},
			)
			if handlerErr != nil {
				t.Fatalf("NewTaskHandler() error = %v", handlerErr)
			}
			recoveryWorker := newValkeyIntegrationWorkerWithReclaim(
				t,
				endpoint,
				stream,
				group,
				"recovery",
				recoveryHandler.Handle,
				time.Millisecond,
				time.Millisecond,
			)
			recoveryQueue, queueErr := queuepkg.NewQueue(
				queuepkg.WithWorker(recoveryWorker),
				queuepkg.WithWorkerCount(1),
				queuepkg.WithLogger(queuepkg.NewEmptyLogger()),
			)
			if queueErr != nil {
				t.Fatalf("NewQueue(recovery) error = %v", queueErr)
			}
			recoveryQueue.Start()
			t.Cleanup(recoveryQueue.Release)

			if test.wantRedelivery {
				select {
				case delivery := <-recovered:
					if delivery.Message().ID().String() != "message-1" {
						t.Fatalf("redelivery message ID = %s", delivery.Message().ID())
					}
				case <-time.After(5 * time.Second):
					t.Fatal("process-death delivery was not recovered")
				}
				waitForValkeySettlement(t, recoveryWorker, 1)
			} else {
				select {
				case delivery := <-recovered:
					t.Fatalf("settled delivery was redelivered: %#v", delivery)
				case <-time.After(200 * time.Millisecond):
				}
				waitForValkeySettlement(t, recoveryWorker, 0)
			}
		})
	}
}

func TestValkeyStreamDeadLetterFailureLeavesDeliveryRecoverable(t *testing.T) {
	container, endpoint := startValkeyIntegrationContainer(t)
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	producer := newValkeyIntegrationPublisher(t, endpoint, "dead-letter-failure")
	dispatcher, err := NewDispatcher(DispatcherConfig{Queue: producer, Codec: codec})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	if err := dispatcher.Dispatch(
		t.Context(),
		[]eventsourcing.Delivery{minimalQueueDelivery(t)},
	); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if err := producer.Shutdown(); err != nil {
		t.Fatalf("producer Shutdown() error = %v", err)
	}

	failure := management.NewFailure(
		management.ClassificationRetryable,
		"retry_requested",
		errors.New("retry requested"),
	)
	first := runOneFailedValkeyAttempt(
		t, endpoint, "dead-letter-failure", "workers", "first", failure, false,
	)
	if first.Pending != 1 || first.DeadLettered != 0 {
		t.Fatalf("first failure stats = %#v", first)
	}
	execValkey(t, container, "SET", "dead-letter-failure-dead", "wrong-type")
	time.Sleep(30 * time.Millisecond)
	second := runOneFailedValkeyAttempt(
		t, endpoint, "dead-letter-failure", "workers", "second", failure, true,
	)
	if second.Pending != 1 ||
		second.DeadLettered != 0 ||
		second.SettlementFailures == 0 {
		t.Fatalf("dead-letter failure stats = %#v", second)
	}
	execValkey(t, container, "DEL", "dead-letter-failure-dead")
	time.Sleep(30 * time.Millisecond)
	third := runOneFailedValkeyAttempt(
		t, endpoint, "dead-letter-failure", "workers", "third", failure, true,
	)
	if third.Pending != 0 || third.DeadLettered != 1 {
		t.Fatalf("recovered dead-letter stats = %#v", third)
	}
}

func TestValkeyPublisherDisconnectionAndShutdownRemainAmbiguous(t *testing.T) {
	container, endpoint := startValkeyIntegrationContainer(t)
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	publisher, err := valkeystream.NewPublisherE(
		valkeystream.WithAddress(endpoint),
		valkeystream.WithStreamName("publisher-failure"),
		valkeystream.WithDialTimeout(100*time.Millisecond),
		valkeystream.WithCommandTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewPublisherE() error = %v", err)
	}
	dispatcher, err := NewDispatcher(DispatcherConfig{Queue: publisher, Codec: codec})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	stopTimeout := time.Second
	if err := container.Stop(t.Context(), &stopTimeout); err != nil {
		t.Fatalf("stop Valkey: %v", err)
	}
	dispatchErr := dispatcher.Dispatch(
		t.Context(),
		[]eventsourcing.Delivery{minimalQueueDelivery(t)},
	)
	assertUnknownDispatch(t, dispatchErr)
	if err := container.Start(t.Context()); err != nil {
		t.Fatalf("restart Valkey: %v", err)
	}
	if err := publisher.Shutdown(); err != nil {
		t.Fatalf("Publisher.Shutdown() error = %v", err)
	}
	dispatchErr = dispatcher.Dispatch(
		t.Context(),
		[]eventsourcing.Delivery{minimalQueueDelivery(t)},
	)
	assertUnknownDispatch(t, dispatchErr)
}

func TestValkeyStreamPreservesInterleavedOrderingIdentifiers(t *testing.T) {
	_, endpoint := startValkeyIntegrationContainer(t)
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	deliveries := []eventsourcing.Delivery{
		orderingDelivery(t, "message-a-1", "aggregate-a", "partition-a", 1),
		orderingDelivery(t, "message-b-1", "aggregate-b", "partition-b", 1),
		orderingDelivery(t, "message-a-2", "aggregate-a", "partition-a", 2),
		orderingDelivery(t, "message-b-2", "aggregate-b", "partition-b", 2),
	}
	received := make(chan eventsourcing.Delivery, len(deliveries))
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
	worker := newValkeyIntegrationWorker(
		t,
		endpoint,
		"ordering-identifiers",
		"ordering-workers",
		"single-consumer",
		handler.Handle,
	)
	queue, err := queuepkg.NewQueue(
		queuepkg.WithWorker(worker),
		queuepkg.WithWorkerCount(1),
		queuepkg.WithLogger(queuepkg.NewEmptyLogger()),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	queue.Start()
	t.Cleanup(queue.Release)
	publisher := newValkeyIntegrationPublisher(t, endpoint, "ordering-identifiers")
	t.Cleanup(func() { _ = publisher.Shutdown() })
	dispatcher, err := NewDispatcher(DispatcherConfig{Queue: publisher, Codec: codec})
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	if err := dispatcher.Dispatch(t.Context(), deliveries); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	for index, want := range deliveries {
		select {
		case got := <-received:
			partition, _ := got.Message().Partition()
			wantPartition, _ := want.Message().Partition()
			if got.Message().ID() != want.Message().ID() ||
				got.Message().Stream() != want.Message().Stream() ||
				got.Message().StreamVersion() != want.Message().StreamVersion() ||
				partition != wantPartition {
				t.Fatalf("delivery %d = %#v, want %#v", index, got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Valkey Streams did not deliver item %d", index)
		}
	}
	waitForValkeySettlement(t, worker, uint64(len(deliveries)))
}

func TestValkeyCrashWorkerHelper(t *testing.T) {
	mode := os.Getenv("GOQUEUE_CRASH_MODE")
	if mode == "" {
		return
	}
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatal(err)
	}
	marker := os.Getenv("GOQUEUE_CRASH_MARKER")
	consumer := func(
		_ context.Context,
		delivery eventsourcing.Delivery,
	) error {
		switch mode {
		case "before-effect":
			os.Exit(21)
		case "after-effect", "after-handler", "after-settlement":
			if writeErr := os.WriteFile(
				marker,
				[]byte(delivery.Message().ID().String()),
				0o600,
			); writeErr != nil {
				t.Fatal(writeErr)
			}
			if mode == "after-effect" {
				os.Exit(22)
			}
		default:
			t.Fatalf("unknown crash mode %q", mode)
		}
		return nil
	}
	handler, err := NewTaskHandler(codec, consumer)
	if err != nil {
		t.Fatal(err)
	}
	run := handler.Handle
	if mode == "after-handler" {
		run = func(ctx context.Context, task core.TaskMessage) error {
			handleErr := handler.Handle(ctx, task)
			if handleErr == nil {
				os.Exit(24)
			}
			return handleErr
		}
	}
	worker := newValkeyIntegrationWorker(
		t,
		os.Getenv("GOQUEUE_CRASH_ENDPOINT"),
		os.Getenv("GOQUEUE_CRASH_STREAM"),
		os.Getenv("GOQUEUE_CRASH_GROUP"),
		"crashing-worker",
		run,
	)
	options := []queuepkg.Option{
		queuepkg.WithWorker(worker),
		queuepkg.WithWorkerCount(1),
		queuepkg.WithLogger(queuepkg.NewEmptyLogger()),
	}
	if mode == "after-settlement" {
		options = append(options, queuepkg.WithAfterFn(func() { os.Exit(23) }))
	}
	queue, err := queuepkg.NewQueue(options...)
	if err != nil {
		t.Fatal(err)
	}
	queue.Start()
	select {}
}

func BenchmarkValkeyDurablePublication(b *testing.B) {
	endpoint := startValkeyBenchmarkContainer(b)
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		b.Fatal(err)
	}
	delivery := minimalQueueDelivery(b)
	encoded, err := codec.Encode(delivery)
	if err != nil {
		b.Fatal(err)
	}
	option := deliveryJobOption(job.AllowOption{}, delivery)
	ctx := context.Background()
	benchmarks := []struct {
		name  string
		setup func(*valkeystream.Publisher) (func() error, error)
	}{
		{
			name: "raw durable queue",
			setup: func(publisher *valkeystream.Publisher) (func() error, error) {
				return func() error {
					return publisher.Queue(queueEnvelope(encoded), option)
				}, nil
			},
		},
		{
			name: "codec dispatcher durable queue",
			setup: func(publisher *valkeystream.Publisher) (func() error, error) {
				dispatcher, constructErr := NewDispatcher(DispatcherConfig{
					Queue: publisher,
					Codec: codec,
				})
				if constructErr != nil {
					return nil, constructErr
				}
				return func() error {
					return dispatcher.Dispatch(
						ctx,
						[]eventsourcing.Delivery{delivery},
					)
				}, nil
			},
		},
	}
	for index, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			publisher, publisherErr := valkeystream.NewPublisherE(
				valkeystream.WithAddress(endpoint),
				valkeystream.WithStreamName(fmt.Sprintf("benchmark-%d", index)),
				valkeystream.WithCommandTimeout(5*time.Second),
				valkeystream.WithDialTimeout(time.Second),
				valkeystream.WithLogger(queuepkg.NewEmptyLogger()),
			)
			if publisherErr != nil {
				b.Fatal(publisherErr)
			}
			b.Cleanup(func() { _ = publisher.Shutdown() })
			run, setupErr := benchmark.setup(publisher)
			if setupErr != nil {
				b.Fatal(setupErr)
			}
			b.ReportAllocs()
			for b.Loop() {
				if runErr := run(); runErr != nil {
					b.Fatal(runErr)
				}
			}
		})
	}
}

func runCrashWorker(
	t *testing.T,
	endpoint string,
	stream string,
	group string,
	mode string,
	marker string,
	wantExitCode int,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		os.Args[0],
		"-test.run=^TestValkeyCrashWorkerHelper$",
	)
	command.Env = append(
		os.Environ(),
		"GOQUEUE_CRASH_MODE="+mode,
		"GOQUEUE_CRASH_ENDPOINT="+endpoint,
		"GOQUEUE_CRASH_STREAM="+stream,
		"GOQUEUE_CRASH_GROUP="+group,
		"GOQUEUE_CRASH_MARKER="+marker,
	)
	output, err := command.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != wantExitCode {
		t.Fatalf(
			"crash worker exit = %v, want %d; output: %s",
			err,
			wantExitCode,
			output,
		)
	}
}

func runOneFailedValkeyAttempt(
	t *testing.T,
	endpoint string,
	stream string,
	group string,
	consumer string,
	failure error,
	reclaim bool,
) valkeystream.Stats {
	t.Helper()
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	handler, err := NewTaskHandler(
		codec,
		func(context.Context, eventsourcing.Delivery) error {
			return failure
		},
	)
	if err != nil {
		t.Fatalf("NewTaskHandler() error = %v", err)
	}
	reclaimIdle := time.Hour
	reclaimInterval := time.Hour
	if reclaim {
		reclaimIdle = time.Millisecond
		reclaimInterval = 100 * time.Millisecond
	}
	worker := newValkeyIntegrationWorkerWithReclaim(
		t,
		endpoint,
		stream,
		group,
		consumer,
		handler.Handle,
		reclaimIdle,
		reclaimInterval,
	)
	done := make(chan struct{}, 1)
	queue, err := queuepkg.NewQueue(
		queuepkg.WithWorker(worker),
		queuepkg.WithWorkerCount(1),
		queuepkg.WithLogger(queuepkg.NewEmptyLogger()),
		queuepkg.WithAfterFn(func() {
			select {
			case done <- struct{}{}:
			default:
			}
		}),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	queue.Start()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		queue.Release()
		t.Fatal("Valkey Streams did not finish failed attempt")
	}
	stats, err := worker.Stats(t.Context())
	queue.Release()
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	return stats
}

func waitForValkeySettlement(
	t *testing.T,
	worker *valkeystream.Worker,
	wantAcknowledged uint64,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		stats, err := worker.Stats(t.Context())
		if err == nil &&
			stats.Pending == 0 &&
			stats.Acknowledged == wantAcknowledged {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"Valkey settlement pending/acknowledged = %d/%d, error = %v",
				stats.Pending,
				stats.Acknowledged,
				err,
			)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func execValkey(
	t *testing.T,
	container testcontainers.Container,
	arguments ...string,
) {
	t.Helper()
	command := append([]string{"valkey-cli"}, arguments...)
	exitCode, output, err := container.Exec(t.Context(), command)
	if err == nil && exitCode == 0 {
		return
	}
	diagnostic, _ := io.ReadAll(output)
	t.Fatalf(
		"valkey-cli %v exit = %d, error = %v, output = %s",
		arguments,
		exitCode,
		err,
		diagnostic,
	)
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

func startValkeyIntegrationContainer(
	t *testing.T,
) (testcontainers.Container, string) {
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
	return container, endpoint
}

func startValkeyBenchmarkContainer(b *testing.B) string {
	b.Helper()
	container, err := testcontainers.GenericContainer(
		b.Context(),
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
		b.Fatalf("start Valkey container: %v", err)
	}
	b.Cleanup(func() { testcontainers.CleanupContainer(b, container) })
	endpoint, err := container.PortEndpoint(b.Context(), "6379/tcp", "")
	if err != nil {
		b.Fatalf("resolve Valkey endpoint: %v", err)
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

func newValkeyIntegrationPublisher(
	t *testing.T,
	endpoint string,
	stream string,
) *valkeystream.Publisher {
	t.Helper()
	publisher, err := valkeystream.NewPublisherE(
		valkeystream.WithAddress(endpoint),
		valkeystream.WithStreamName(stream),
		valkeystream.WithCommandTimeout(time.Second),
		valkeystream.WithDialTimeout(time.Second),
		valkeystream.WithLogger(queuepkg.NewEmptyLogger()),
	)
	if err != nil {
		t.Fatalf("NewPublisherE() error = %v", err)
	}
	return publisher
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
