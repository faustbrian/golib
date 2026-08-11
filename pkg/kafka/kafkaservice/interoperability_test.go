//go:build interoperability

package kafkaservice_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/kafkaservice"
)

const serviceKafkaImage = "apache/kafka:4.3.1@" +
	"sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837"

func TestLifecycleAdapterDrainsRootKafkaResources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	broker, container := startServiceKafka(t, ctx)
	topic := fmt.Sprintf("golib-kafkaservice-%d", time.Now().UnixNano())
	createServiceTopic(t, ctx, container, topic)

	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		t.Fatalf("construct correlation factory: %v", err)
	}
	producerResource, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{broker},
		ClientID:      "golib-kafkaservice-producer",
		AllowedTopics: []string{topic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct producer: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := producerResource.Close(); closeErr != nil {
			t.Errorf("close producer: %v", closeErr)
		}
	})
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*kafka.Producer]{
			Name:        "golib-kafkaservice-producer",
			Resource:    producerResource,
			Correlation: factory,
			Startup: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Health(ctx)
			},
			Readiness: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Health(ctx)
			},
			Publish: func(
				ctx context.Context,
				resource *kafka.Producer,
				record kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				result := resource.PublishRecord(ctx, record)

				return result, result.Err
			},
			Shutdown: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Shutdown(ctx)
			},
		},
	)
	if err != nil {
		t.Fatalf("construct producer adapter: %v", err)
	}
	producerComponent := producer.Component()
	if err = producerComponent.Start(ctx); err != nil {
		t.Fatalf("start producer adapter: %v", err)
	}
	readiness, ok := producer.Readiness()
	if !ok {
		t.Fatal("producer readiness is absent")
	}
	if err = readiness.Run(ctx); err != nil {
		t.Fatalf("check producer readiness: %v", err)
	}

	group := "golib-kafkaservice-consumer-v1"
	consumerResource, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:        []string{broker},
		ClientID:       "golib-kafkaservice-consumer",
		GroupID:        group,
		Topics:         []string{topic},
		ResetOffset:    kafka.OffsetEarliest,
		MaxPollRecords: 1,
		Security:       kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct consumer: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := consumerResource.Close(); closeErr != nil {
			t.Errorf("close consumer: %v", closeErr)
		}
	})
	firstHandled := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	var firstOnce sync.Once
	var secondOnce sync.Once
	var secondFinished atomic.Bool
	var attempts atomic.Int32
	consumer, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[*kafka.Consumer]{
			Name:        "golib-kafkaservice-consumer",
			Resource:    consumerResource,
			Correlation: factory,
			Handler: kafka.HandlerFunc(func(
				_ context.Context,
				record kafka.ConsumedRecord,
			) error {
				attempt := attempts.Add(1)
				if record.Topic != topic || int64(attempt-1) != record.Offset {
					return errors.New("unexpected consumed record metadata")
				}
				if record.Offset == 0 {
					firstOnce.Do(func() { close(firstHandled) })

					return nil
				}
				secondOnce.Do(func() { close(secondStarted) })
				select {
				case <-releaseSecond:
					secondFinished.Store(true)

					return nil
				case <-ctx.Done():
					return context.Cause(ctx)
				}
			}),
			Run: func(
				ctx context.Context,
				resource *kafka.Consumer,
				handler kafka.Handler,
			) error {
				return resource.Run(ctx, handler)
			},
			Shutdown: func(ctx context.Context, resource *kafka.Consumer) error {
				if !secondFinished.Load() {
					return errors.New("consumer shutdown preceded admitted handler")
				}

				return resource.Shutdown(ctx)
			},
		},
	)
	if err != nil {
		t.Fatalf("construct consumer adapter: %v", err)
	}
	plan := consumer.Plan()
	if err = plan.Components[0].Start(ctx); err != nil {
		t.Fatalf("start consumer adapter: %v", err)
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	runResult := make(chan error, 1)
	go func() {
		runResult <- plan.Tasks[0].Run(runCtx)
	}()

	parent, err := factory.Start()
	if err != nil {
		t.Fatalf("create parent correlation: %v", err)
	}
	_, firstDelivery, err := producer.Publish(
		correlation.WithValues(ctx, parent),
		kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte("lifecycle"),
			Value: []byte("drain"),
		},
	)
	if err != nil {
		t.Fatalf("publish through producer adapter: %v", err)
	}
	if firstDelivery.Topic != topic || firstDelivery.Partition != 0 ||
		firstDelivery.Offset != 0 {
		t.Fatalf("first delivery metadata = %#v", firstDelivery)
	}

	select {
	case <-firstHandled:
	case runErr := <-runResult:
		t.Fatalf("consumer task exited before first handling: %v", runErr)
	case <-ctx.Done():
		t.Fatalf("wait for first handler: %v", context.Cause(ctx))
	}
	waitForServiceCommit(t, ctx, broker, group, topic, 1, 0)
	_, secondDelivery, err := producer.Publish(
		correlation.WithValues(ctx, parent),
		kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte("lifecycle"),
			Value: []byte("redeliver"),
		},
	)
	if err != nil {
		t.Fatalf("publish in-flight cancellation record: %v", err)
	}
	if secondDelivery.Topic != topic || secondDelivery.Partition != 0 ||
		secondDelivery.Offset != 1 {
		t.Fatalf("second delivery metadata = %#v", secondDelivery)
	}
	select {
	case <-secondStarted:
	case runErr := <-runResult:
		t.Fatalf("consumer task exited before second handling: %v", runErr)
	case <-ctx.Done():
		t.Fatalf("wait for second handler: %v", context.Cause(ctx))
	}
	cancelRun()
	stopInvoked := make(chan struct{})
	stopResult := make(chan error, 1)
	go func() {
		close(stopInvoked)
		stopResult <- plan.Components[0].Stop(ctx)
	}()
	<-stopInvoked
	close(releaseSecond)
	if err = <-stopResult; err != nil {
		t.Fatalf("stop consumer adapter: %v", err)
	}
	if err = <-runResult; err != nil {
		t.Fatalf("run consumer adapter during cancellation: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("handler attempts = %d, want 2", attempts.Load())
	}
	waitForServiceCommit(t, ctx, broker, group, topic, 1, 1)

	replacement, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:        []string{broker},
		ClientID:       "golib-kafkaservice-replacement",
		GroupID:        group,
		Topics:         []string{topic},
		ResetOffset:    kafka.OffsetEarliest,
		MaxPollRecords: 1,
		Security:       kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct replacement consumer: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := replacement.Close(); closeErr != nil {
			t.Errorf("close replacement consumer: %v", closeErr)
		}
	})
	replacementResult, err := replacement.RunOnce(
		ctx,
		kafka.HandlerFunc(func(_ context.Context, record kafka.ConsumedRecord) error {
			if record.Topic != topic || record.Partition != 0 || record.Offset != 1 {
				return errors.New("replacement did not receive unsettled record")
			}

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("consume unsettled record with replacement: %v", err)
	}
	if replacementResult.Processed != 1 || replacementResult.Committed != 1 {
		t.Fatalf("replacement result = %#v", replacementResult)
	}
	if err = replacement.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown replacement consumer: %v", err)
	}
	waitForServiceCommit(t, ctx, broker, group, topic, 2, 0)

	if err = producerComponent.Stop(ctx); err != nil {
		t.Fatalf("stop producer adapter: %v", err)
	}
	if _, _, publishErr := producer.Publish(
		correlation.WithValues(ctx, parent),
		kafka.ProducerRecord{Topic: topic, Key: []byte("closed")},
	); !errors.Is(publishErr, kafkaservice.ErrUnavailable) {
		t.Fatalf("publish after stop error = %v", publishErr)
	}
}

func TestLifecycleAdapterRecoversFromKafkaFailureBoundaries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	broker, container := startServiceKafka(t, ctx)
	topic := fmt.Sprintf("golib-kafkaservice-failure-%d", time.Now().UnixNano())
	createServiceTopic(t, ctx, container, topic)
	var paused atomic.Bool
	pause := func() {
		runServiceDocker(t, ctx, "pause", container)
		paused.Store(true)
	}
	unpause := func() {
		runServiceDocker(t, ctx, "unpause", container)
		paused.Store(false)
		waitForServiceKafka(t, ctx, container)
	}
	t.Cleanup(func() {
		if paused.Load() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			if output, err := exec.CommandContext(
				cleanupCtx,
				"docker",
				"unpause",
				container,
			).CombinedOutput(); err != nil {
				t.Errorf("unpause Apache Kafka container: %v: %s", err, boundedServiceOutput(output))
			}
		}
	})

	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		t.Fatalf("construct correlation factory: %v", err)
	}
	pause()
	partialResource, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{broker},
		ClientID:      "golib-kafkaservice-partial-startup",
		AllowedTopics: []string{topic},
		DialTimeout:   200 * time.Millisecond,
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct partial-startup producer: %v", err)
	}
	t.Cleanup(func() { _ = partialResource.Close() })
	partial, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*kafka.Producer]{
			Name:        "golib-kafkaservice-partial-startup",
			Resource:    partialResource,
			Correlation: factory,
			Startup: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Health(ctx)
			},
			Publish: func(
				ctx context.Context,
				resource *kafka.Producer,
				record kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				result := resource.PublishRecord(ctx, record)

				return result, result.Err
			},
			Shutdown: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Shutdown(ctx)
			},
		},
	)
	if err != nil {
		t.Fatalf("construct partial-startup adapter: %v", err)
	}
	startupCtx, cancelStartup := context.WithTimeout(ctx, 300*time.Millisecond)
	startupErr := partial.Component().Start(startupCtx)
	cancelStartup()
	var classifiedStartup *kafkaservice.StartupError
	if !errors.As(startupErr, &classifiedStartup) ||
		!errors.Is(startupErr, context.DeadlineExceeded) {
		t.Fatalf("partial Start() error = %v, want bounded StartupError", startupErr)
	}
	var startupCallback *kafkaservice.CallbackError
	if !errors.As(classifiedStartup.Validation, &startupCallback) ||
		startupCallback.Operation != kafkaservice.CallbackStartup {
		t.Fatalf("partial startup callback error = %#v", classifiedStartup.Validation)
	}
	parent, parentErr := factory.Start()
	if parentErr != nil {
		t.Fatalf("construct partial-startup parent correlation: %v", parentErr)
	}
	if _, _, publishErr := partial.Publish(
		correlation.WithValues(ctx, parent),
		kafka.ProducerRecord{Topic: topic},
	); !errors.Is(publishErr, kafkaservice.ErrUnavailable) {
		t.Fatalf("partial adapter Publish() error = %v", publishErr)
	}
	unpause()

	producerResource, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:         []string{broker},
		ClientID:        "golib-kafkaservice-recovered-producer",
		AllowedTopics:   []string{topic},
		DeliveryTimeout: 10 * time.Second,
		RequestTimeout:  time.Second,
		Security:        kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct recovered producer: %v", err)
	}
	t.Cleanup(func() { _ = producerResource.Close() })
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*kafka.Producer]{
			Name:        "golib-kafkaservice-recovered-producer",
			Resource:    producerResource,
			Correlation: factory,
			Startup: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Health(ctx)
			},
			Readiness: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Health(ctx)
			},
			Publish: func(
				ctx context.Context,
				resource *kafka.Producer,
				record kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				result := resource.PublishRecord(ctx, record)

				return result, result.Err
			},
			Shutdown: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Shutdown(ctx)
			},
		},
	)
	if err != nil {
		t.Fatalf("construct recovered producer adapter: %v", err)
	}
	producerComponent := producer.Component()
	if err = producerComponent.Start(ctx); err != nil {
		t.Fatalf("start recovered producer adapter: %v", err)
	}
	readiness, ok := producer.Readiness()
	if !ok {
		t.Fatal("recovered producer readiness is absent")
	}
	if err = readiness.Run(ctx); err != nil {
		t.Fatalf("check recovered producer readiness: %v", err)
	}

	pause()
	delivery, err := producerResource.PublishAsync(ctx, kafka.ProducerRecord{
		Topic: topic,
		Key:   []byte("broker-loss"),
		Value: []byte("ambiguous-until-recovery"),
	})
	if err != nil {
		t.Fatalf("admit asynchronous record before shutdown: %v", err)
	}
	stopCtx, cancelStop := context.WithTimeout(ctx, 300*time.Millisecond)
	err = producerComponent.Stop(stopCtx)
	cancelStop()
	if !errors.Is(err, kafka.ErrDrainIncomplete) ||
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("producer Stop() during broker loss error = %v", err)
	}
	var shutdownCallback *kafkaservice.CallbackError
	if !errors.As(err, &shutdownCallback) ||
		shutdownCallback.Operation != kafkaservice.CallbackShutdown {
		t.Fatalf("producer Stop() callback error = %#v", err)
	}
	unpause()
	select {
	case result := <-delivery:
		if result.Err != nil || result.Topic != topic {
			t.Fatalf("delivery after broker recovery = %#v", result)
		}
	case <-ctx.Done():
		t.Fatalf("wait for ambiguous delivery reconciliation: %v", context.Cause(ctx))
	}
	if err = producerComponent.Stop(ctx); err != nil {
		t.Fatalf("retry producer Stop() after recovery: %v", err)
	}

	group := "golib-kafkaservice-commit-timeout-v1"
	consumerResource, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:           []string{broker},
		ClientID:          "golib-kafkaservice-commit-timeout",
		GroupID:           group,
		InstanceID:        group + "-instance",
		Topics:            []string{topic},
		ResetOffset:       kafka.OffsetEarliest,
		MaxPollRecords:    1,
		SessionTimeout:    6 * time.Second,
		HeartbeatInterval: time.Second,
		HandlerTimeout:    time.Second,
		CommitTimeout:     300 * time.Millisecond,
		RebalanceTimeout:  10 * time.Second,
		Security:          kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct commit-timeout consumer: %v", err)
	}
	t.Cleanup(func() { _ = consumerResource.Close() })
	handlerReady := make(chan struct{})
	brokerPaused := make(chan struct{})
	consumer, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[*kafka.Consumer]{
			Name:        "golib-kafkaservice-commit-timeout",
			Resource:    consumerResource,
			Correlation: factory,
			Handler: kafka.HandlerFunc(func(
				context.Context,
				kafka.ConsumedRecord,
			) error {
				close(handlerReady)
				<-brokerPaused

				return nil
			}),
			Run: func(
				ctx context.Context,
				resource *kafka.Consumer,
				handler kafka.Handler,
			) error {
				return resource.Run(ctx, handler)
			},
			Shutdown: func(ctx context.Context, resource *kafka.Consumer) error {
				return resource.Shutdown(ctx)
			},
		},
	)
	if err != nil {
		t.Fatalf("construct commit-timeout adapter: %v", err)
	}
	plan := consumer.Plan()
	if err = plan.Components[0].Start(ctx); err != nil {
		t.Fatalf("start commit-timeout adapter: %v", err)
	}
	runCtx, cancelRun := context.WithTimeout(ctx, 20*time.Second)
	runResult := make(chan error, 1)
	go func() { runResult <- plan.Tasks[0].Run(runCtx) }()
	select {
	case <-handlerReady:
	case <-runCtx.Done():
		t.Fatalf("wait for commit-timeout handler: %v", context.Cause(runCtx))
	}
	pause()
	close(brokerPaused)
	err = <-runResult
	cancelRun()
	var runCallback *kafkaservice.CallbackError
	var consumerErr *kafka.ConsumerError
	if err == nil || !errors.As(err, &runCallback) ||
		runCallback.Operation != kafkaservice.CallbackRun ||
		!errors.As(err, &consumerErr) ||
		consumerErr.Operation() != kafka.ConsumerOperationCommit ||
		consumerErr.Category() != kafka.ErrorTimeout {
		t.Fatalf("consumer commit-timeout classification = %#v", err)
	}
	if err = plan.Components[0].Stop(ctx); err != nil {
		t.Fatalf("stop paused static commit-timeout consumer: %v", err)
	}
	unpause()

	replacement, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:           []string{broker},
		ClientID:          "golib-kafkaservice-commit-replacement",
		GroupID:           group,
		InstanceID:        group + "-instance",
		Topics:            []string{topic},
		ResetOffset:       kafka.OffsetEarliest,
		MaxPollRecords:    1,
		SessionTimeout:    6 * time.Second,
		HeartbeatInterval: time.Second,
		HandlerTimeout:    time.Second,
		CommitTimeout:     300 * time.Millisecond,
		RebalanceTimeout:  10 * time.Second,
		Security:          kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct commit replacement: %v", err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
	replacementCtx, cancelReplacement := context.WithTimeout(ctx, 20*time.Second)
	replacementResult, err := replacement.RunOnce(
		replacementCtx,
		kafka.HandlerFunc(func(_ context.Context, record kafka.ConsumedRecord) error {
			if record.Topic != topic || record.Offset != 0 {
				return errors.New("replacement did not receive commit-timeout record")
			}

			return nil
		}),
	)
	cancelReplacement()
	if err != nil {
		t.Fatalf("consume commit-timeout record with replacement: %v", err)
	}
	if replacementResult.Processed != 1 || replacementResult.Committed != 1 {
		t.Fatalf("commit replacement result = %#v", replacementResult)
	}
	waitForServiceCommit(t, ctx, broker, group, topic, 1, 0)
	if err = replacement.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown commit replacement: %v", err)
	}
}

func TestLifecycleAdapterRebalancesDuringShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	broker, container := startServiceKafka(t, ctx)
	topic := fmt.Sprintf("golib-kafkaservice-rebalance-%d", time.Now().UnixNano())
	createServiceTopic(t, ctx, container, topic)
	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		t.Fatalf("construct correlation factory: %v", err)
	}
	producerResource, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{broker},
		ClientID:      "golib-kafkaservice-rebalance-producer",
		AllowedTopics: []string{topic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct rebalance producer: %v", err)
	}
	t.Cleanup(func() { _ = producerResource.Close() })
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*kafka.Producer]{
			Name:        "golib-kafkaservice-rebalance-producer",
			Resource:    producerResource,
			Correlation: factory,
			Publish: func(
				ctx context.Context,
				resource *kafka.Producer,
				record kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				result := resource.PublishRecord(ctx, record)

				return result, result.Err
			},
			Shutdown: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Shutdown(ctx)
			},
		},
	)
	if err != nil {
		t.Fatalf("construct rebalance producer adapter: %v", err)
	}
	producerComponent := producer.Component()
	if err = producerComponent.Start(ctx); err != nil {
		t.Fatalf("start rebalance producer adapter: %v", err)
	}
	parent, err := factory.Start()
	if err != nil {
		t.Fatalf("construct rebalance parent correlation: %v", err)
	}
	_, delivery, err := producer.Publish(
		correlation.WithValues(ctx, parent),
		kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte("rebalance"),
			Value: []byte("redeliver-after-shutdown"),
		},
	)
	if err != nil || delivery.Offset != 0 {
		t.Fatalf("publish rebalance record = %#v, %v", delivery, err)
	}
	if err = producerComponent.Stop(ctx); err != nil {
		t.Fatalf("stop rebalance producer adapter: %v", err)
	}

	group := "golib-kafkaservice-rebalance-v1"
	blocked := make(chan kafka.Observation, 1)
	rebalanceWait := make(chan kafka.Observation, 1)
	observers := kafka.ObserverPolicy{
		Observers: []kafka.ObserverFunc{func(
			_ context.Context,
			observation kafka.Observation,
		) error {
			switch observation.Kind {
			case kafka.ObservationConsumeBlocked:
				select {
				case blocked <- observation:
				default:
				}
			case kafka.ObservationConsumeRebalanceWait:
				select {
				case rebalanceWait <- observation:
				default:
				}
			}

			return nil
		}},
		FailureHandler: func(context.Context, kafka.ObservationFailure) {},
		Timeout:        time.Second,
	}
	originalResource, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:          []string{broker},
		ClientID:         "golib-kafkaservice-rebalance-original",
		GroupID:          group,
		Topics:           []string{topic},
		ResetOffset:      kafka.OffsetEarliest,
		MaxPollRecords:   1,
		RebalanceHandler: kafka.RebalanceDrainHandler,
		SessionTimeout:   6 * time.Second,
		RebalanceTimeout: 15 * time.Second,
		HandlerTimeout:   5 * time.Second,
		CommitTimeout:    time.Second,
		Observers:        observers,
		Security:         kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct original rebalance consumer: %v", err)
	}
	t.Cleanup(func() { _ = originalResource.Close() })
	handlerStarted := make(chan struct{})
	handlerCanceled := make(chan struct{})
	shutdownStarted := make(chan struct{})
	original, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[*kafka.Consumer]{
			Name:        "golib-kafkaservice-rebalance-original",
			Resource:    originalResource,
			Correlation: factory,
			Handler: kafka.HandlerFunc(func(
				ctx context.Context,
				record kafka.ConsumedRecord,
			) error {
				if record.Topic != topic || record.Offset != 0 {
					return errors.New("original received unexpected rebalance record")
				}
				close(handlerStarted)
				<-ctx.Done()
				close(handlerCanceled)

				return context.Cause(ctx)
			}),
			Run: func(
				ctx context.Context,
				resource *kafka.Consumer,
				handler kafka.Handler,
			) error {
				return resource.Run(ctx, handler)
			},
			Shutdown: func(ctx context.Context, resource *kafka.Consumer) error {
				close(shutdownStarted)

				return resource.Shutdown(ctx)
			},
		},
	)
	if err != nil {
		t.Fatalf("construct original rebalance adapter: %v", err)
	}
	plan := original.Plan()
	if err = plan.Components[0].Start(ctx); err != nil {
		t.Fatalf("start original rebalance adapter: %v", err)
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	originalRun := make(chan error, 1)
	go func() { originalRun <- plan.Tasks[0].Run(runCtx) }()
	select {
	case <-handlerStarted:
	case runErr := <-originalRun:
		t.Fatalf("original rebalance run exited before handler admission: %v", runErr)
	case <-ctx.Done():
		t.Fatalf("wait for original rebalance handler: %v", context.Cause(ctx))
	}

	replacement, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:          []string{broker},
		ClientID:         "golib-kafkaservice-rebalance-replacement",
		GroupID:          group,
		Topics:           []string{topic},
		ResetOffset:      kafka.OffsetEarliest,
		MaxPollRecords:   1,
		RebalanceHandler: kafka.RebalanceCancelHandler,
		SessionTimeout:   6 * time.Second,
		RebalanceTimeout: 15 * time.Second,
		HandlerTimeout:   5 * time.Second,
		CommitTimeout:    time.Second,
		Security:         kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct rebalance replacement: %v", err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
	type replacementOutcome struct {
		result kafka.PollResult
		err    error
	}
	replacementRun := make(chan replacementOutcome, 1)
	go func() {
		result, runErr := replacement.RunOnce(
			ctx,
			kafka.HandlerFunc(func(
				_ context.Context,
				record kafka.ConsumedRecord,
			) error {
				if record.Topic != topic || record.Offset != 0 {
					return errors.New("replacement received unexpected rebalance record")
				}

				return nil
			}),
		)
		replacementRun <- replacementOutcome{result: result, err: runErr}
	}()
	select {
	case observation := <-blocked:
		if !observation.Succeeded || observation.GroupID != group {
			t.Fatalf("blocked rebalance observation = %#v", observation)
		}
	case <-ctx.Done():
		t.Fatalf("wait for blocked rebalance: %v", context.Cause(ctx))
	}
	if err = plan.Components[0].CloseAdmission(); err != nil {
		t.Fatalf("close rebalance admission: %v", err)
	}
	stopResult := make(chan error, 1)
	go func() { stopResult <- plan.Components[0].Stop(ctx) }()
	select {
	case <-shutdownStarted:
		t.Fatal("consumer shutdown started before the admitted run exited")
	default:
	}
	cancelRun()
	select {
	case <-handlerCanceled:
	case <-ctx.Done():
		t.Fatalf("wait for rebalance handler cancellation: %v", context.Cause(ctx))
	}
	if runErr := <-originalRun; runErr != nil {
		t.Fatalf("original rebalance run error = %v", runErr)
	}
	if err = <-stopResult; err != nil {
		t.Fatalf("stop original rebalance adapter: %v", err)
	}
	select {
	case observation := <-rebalanceWait:
		if !observation.Succeeded || observation.Category != kafka.ErrorUnknown {
			t.Fatalf("rebalance wait observation = %#v", observation)
		}
	case <-ctx.Done():
		t.Fatalf("wait for resolved rebalance observation: %v", context.Cause(ctx))
	}
	select {
	case outcome := <-replacementRun:
		if outcome.err != nil || outcome.result.Processed != 1 ||
			outcome.result.Committed != 1 {
			t.Fatalf("rebalance replacement outcome = %#v, %v", outcome.result, outcome.err)
		}
	case <-ctx.Done():
		t.Fatalf("wait for rebalance replacement: %v", context.Cause(ctx))
	}
	waitForServiceCommit(t, ctx, broker, group, topic, 1, 0)
	if err = replacement.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown rebalance replacement: %v", err)
	}
}

func waitForServiceCommit(
	t *testing.T,
	ctx context.Context,
	broker string,
	group string,
	topic string,
	offset int64,
	lag int64,
) {
	t.Helper()

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  []string{broker},
		ClientID: "golib-kafkaservice-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct commit inspector: %v", err)
	}
	defer inspector.Close()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var last []kafka.ConsumerGroupState
	var lastErr error
	for {
		last, lastErr = inspector.ConsumerGroupLag(ctx, group)
		if lastErr == nil && len(last) == 1 && len(last[0].Partitions) == 1 {
			partition := last[0].Partitions[0]
			if partition.Topic == topic && partition.Partition == 0 &&
				partition.CommittedOffset == offset && partition.Lag == lag {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf(
				"wait for committed source offset: %v; state = %#v; error = %v",
				context.Cause(ctx),
				last,
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func startServiceKafka(t *testing.T, ctx context.Context) (string, string) {
	t.Helper()

	port := reserveServicePort(t)
	container := fmt.Sprintf("golib-kafkaservice-%d-%d", os.Getpid(), time.Now().UnixNano())
	args := []string{
		"run", "--detach", "--rm", "--name", container,
		"--publish", "127.0.0.1:" + strconv.Itoa(port) + ":9092",
		"--env", "KAFKA_NODE_ID=1",
		"--env", "KAFKA_PROCESS_ROLES=broker,controller",
		"--env", "KAFKA_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT",
		"--env", "KAFKA_CONTROLLER_QUORUM_VOTERS=1@localhost:9093",
		"--env", "KAFKA_LISTENERS=INTERNAL://:19092,CONTROLLER://:9093,EXTERNAL://:9092",
		"--env", "KAFKA_ADVERTISED_LISTENERS=INTERNAL://localhost:19092,EXTERNAL://127.0.0.1:" + strconv.Itoa(port),
		"--env", "KAFKA_INTER_BROKER_LISTENER_NAME=INTERNAL",
		"--env", "KAFKA_CONTROLLER_LISTENER_NAMES=CONTROLLER",
		"--env", "CLUSTER_ID=4L6g3nShT-eMCtK--X86sw",
		"--env", "KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1",
		"--env", "KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR=1",
		"--env", "KAFKA_TRANSACTION_STATE_LOG_MIN_ISR=1",
		"--env", "KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS=0",
		"--env", "KAFKA_AUTO_CREATE_TOPICS_ENABLE=false",
		serviceKafkaImage,
	}
	if output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("start pinned Apache Kafka container: %v: %s", err, boundedServiceOutput(output))
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if output, err := exec.CommandContext(
			cleanupCtx,
			"docker",
			"rm",
			"--force",
			container,
		).CombinedOutput(); err != nil {
			t.Errorf("remove Apache Kafka container: %v: %s", err, boundedServiceOutput(output))
		}
	})

	waitForServiceKafka(t, ctx, container)
	version := runServiceDockerExec(
		t,
		ctx,
		container,
		"/opt/kafka/bin/kafka-broker-api-versions.sh",
		"--version",
	)
	fields := strings.Fields(version)
	if len(fields) == 0 || fields[0] != "4.3.1" {
		t.Fatalf("Apache Kafka runtime version = %q, want 4.3.1", version)
	}

	return "127.0.0.1:" + strconv.Itoa(port), container
}

func waitForServiceKafka(t *testing.T, ctx context.Context, container string) {
	t.Helper()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var lastOutput string
	for {
		output, err := exec.CommandContext(
			ctx,
			"docker",
			"exec",
			container,
			"/opt/kafka/bin/kafka-broker-api-versions.sh",
			"--bootstrap-server",
			"localhost:19092",
		).CombinedOutput()
		if err == nil {
			return
		}
		lastOutput = boundedServiceOutput(output)
		select {
		case <-ctx.Done():
			t.Fatalf("wait for Apache Kafka: %v: %s", context.Cause(ctx), lastOutput)
		case <-ticker.C:
		}
	}
}

func createServiceTopic(t *testing.T, ctx context.Context, container string, topic string) {
	t.Helper()

	runServiceDockerExec(
		t,
		ctx,
		container,
		"/opt/kafka/bin/kafka-topics.sh",
		"--bootstrap-server",
		"localhost:19092",
		"--create",
		"--topic",
		topic,
		"--partitions",
		"1",
		"--replication-factor",
		"1",
	)
}

func runServiceDockerExec(
	t *testing.T,
	ctx context.Context,
	container string,
	args ...string,
) string {
	t.Helper()

	commandArgs := append([]string{"exec", container}, args...)
	output, err := exec.CommandContext(ctx, "docker", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("execute Apache Kafka fixture command: %v: %s", err, boundedServiceOutput(output))
	}

	return strings.TrimSpace(string(output))
}

func runServiceDocker(
	t *testing.T,
	ctx context.Context,
	args ...string,
) string {
	t.Helper()

	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("execute Apache Kafka fixture operation: %v: %s", err, boundedServiceOutput(output))
	}

	return strings.TrimSpace(string(output))
}

func reserveServicePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve Apache Kafka port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release Apache Kafka port reservation: %v", err)
	}

	return port
}

func boundedServiceOutput(output []byte) string {
	const maximum = 4 << 10
	if len(output) > maximum {
		output = output[len(output)-maximum:]
	}

	return strings.TrimSpace(string(output))
}
