//go:build integration

package gokafka_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	gokafka "github.com/faustbrian/golib/pkg/event-sourcing/adapters/gokafka"
	"github.com/faustbrian/golib/pkg/kafka"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	crashWorkerEnvironment = "GOKAFKA_CRASH_WORKER"
	crashAfterHandlingExit = 91
	crashAfterDeadLetter   = 92
)

func TestKafkaAmbiguityAndProcessDeathPreserveAtLeastOnceDelivery(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	container, err := tckafka.Run(ctx, integrationKafkaImage)
	if container != nil {
		cleanupKafkaContainer(t, container)
	}
	if err != nil {
		t.Fatalf("start Kafka: %v", err)
	}
	brokers, err := container.Brokers(ctx)
	if err != nil {
		t.Fatalf("resolve Kafka brokers: %v", err)
	}
	topicPrefix := fmt.Sprintf("event-sourcing-hardening-%d", time.Now().UnixNano())

	t.Run("lost acknowledgement permits duplicates without duplicate effects", func(t *testing.T) {
		topic := topicPrefix + ".ambiguous"
		createIntegrationTopic(t, ctx, brokers, topic)
		codec := integrationCodec(t, topic)
		producer := integrationProducer(t, brokers, topic)
		publisher := &lostAcknowledgementPublisher{
			publisher: producer,
			failure:   errors.New("injected acknowledgement loss"),
		}
		dispatcher, err := gokafka.NewDispatcher(publisher, codec)
		if err != nil {
			t.Fatalf("construct ambiguous dispatcher: %v", err)
		}
		deliveries := integrationDeliveries(t)[:2]
		if err := dispatcher.Dispatch(ctx, deliveries[:1]); !errors.Is(
			err,
			publisher.failure,
		) {
			t.Fatalf("lost acknowledgement error = %v", err)
		}
		if err := dispatcher.Dispatch(ctx, deliveries[:1]); err != nil {
			t.Fatalf("retry ambiguous delivery: %v", err)
		}
		if err := dispatcher.Dispatch(ctx, deliveries[1:]); err != nil {
			t.Fatalf("publish following delivery: %v", err)
		}

		seen := make([]string, 0, 3)
		effects := make(map[string]struct{})
		handler, err := gokafka.NewRecordHandler(
			codec,
			gokafka.DeliveryConsumerFunc(func(
				_ context.Context,
				delivery eventsourcing.Delivery,
			) error {
				identity := delivery.Message().ID().String()
				seen = append(seen, identity)
				effects[identity] = struct{}{}

				return nil
			}),
		)
		if err != nil {
			t.Fatalf("construct idempotent handler: %v", err)
		}
		groupID := topic + ".group"
		runIntegrationConsumer(t, ctx, brokers, topic, groupID, handler, 3)
		if want := []string{"message-1", "message-1", "message-2"}; !slices.Equal(seen, want) {
			t.Fatalf("delivery order = %q, want %q", seen, want)
		}
		if len(effects) != 2 {
			t.Fatalf("idempotent effects = %d, want 2", len(effects))
		}
		assertGroupCommitted(t, ctx, brokers, topic, groupID, 3)
	})

	t.Run("cooperative rebalance preserves partition order", func(t *testing.T) {
		topic := topicPrefix + ".rebalance"
		createIntegrationTopicWithPartitions(t, ctx, brokers, topic, 2)
		codec := integrationCodec(t, topic)
		producer := integrationProducer(t, brokers, topic)
		groupID := topic + ".group"
		consumerOne := newHardeningConsumer(
			t,
			brokers,
			topic,
			groupID,
			groupID+".one",
		)
		defer consumerOne.Close()
		var mutex sync.Mutex
		positions := map[int32][]int64{0: {}, 1: {}}
		effects := make(map[string]struct{}, 4)
		deliveryHandler, err := gokafka.NewRecordHandler(
			codec,
			gokafka.DeliveryConsumerFunc(func(
				_ context.Context,
				delivery eventsourcing.Delivery,
			) error {
				mutex.Lock()
				defer mutex.Unlock()
				effects[delivery.Message().ID().String()] = struct{}{}

				return nil
			}),
		)
		if err != nil {
			t.Fatalf("construct rebalance handler: %v", err)
		}
		handlingStarted := make(chan struct{})
		releaseHandling := make(chan struct{})
		var blockOnce sync.Once
		handler := kafka.HandlerFunc(func(
			handlerCtx context.Context,
			record kafka.ConsumedMessage,
		) error {
			if err := deliveryHandler.Handle(handlerCtx, record); err != nil {
				return err
			}
			blockOnce.Do(func() {
				close(handlingStarted)
				<-releaseHandling
			})
			mutex.Lock()
			positions[record.Partition] = append(
				positions[record.Partition],
				record.Offset,
			)
			mutex.Unlock()

			return nil
		})

		runCtx, runCancel := context.WithCancel(ctx)
		defer runCancel()
		var processed atomic.Int64
		runErrors := make(chan error, 2)
		var wait sync.WaitGroup
		runConsumer := func(consumer *kafka.Consumer) {
			wait.Add(1)
			go func() {
				defer wait.Done()
				for runCtx.Err() == nil {
					result, err := consumer.RunOnce(runCtx, handler)
					if err != nil {
						if runCtx.Err() == nil {
							runErrors <- err
						}

						return
					}
					if processed.Add(int64(result.Processed)) >= 4 {
						runCancel()

						return
					}
				}
			}()
		}
		runConsumer(consumerOne)
		waitForStableGroupMembers(t, ctx, brokers, groupID, 1)

		for index, delivery := range integrationDeliveries(t) {
			record, err := codec.Encode(delivery)
			if err != nil {
				t.Fatalf("encode rebalance delivery %d: %v", index, err)
			}
			record.Partition = kafka.ExplicitPartition(int32(index % 2))
			if err := producer.Publish(ctx, record); err != nil {
				t.Fatalf("publish rebalance delivery %d: %v", index, err)
			}
		}
		select {
		case <-handlingStarted:
		case <-ctx.Done():
			t.Fatalf("wait for blocked rebalance handler: %v", ctx.Err())
		}
		consumerTwo := newHardeningConsumer(
			t,
			brokers,
			topic,
			groupID,
			groupID+".two",
		)
		defer consumerTwo.Close()
		runConsumer(consumerTwo)
		waitForGroupMembershipChange(t, ctx, brokers, groupID, 1)
		close(releaseHandling)
		waitForStableGroupMembers(t, ctx, brokers, groupID, 2)
		wait.Wait()
		close(runErrors)
		for err := range runErrors {
			t.Errorf("rebalance consumer: %v", err)
		}

		mutex.Lock()
		defer mutex.Unlock()
		for partition := range int32(2) {
			if want := []int64{0, 1}; !slices.Equal(positions[partition], want) {
				t.Fatalf(
					"partition %d offsets = %v, want %v",
					partition,
					positions[partition],
					want,
				)
			}
		}
		if len(effects) != 4 {
			t.Fatalf("rebalance effects = %d, want 4", len(effects))
		}
	})

	t.Run("handler death before commit redelivers without duplicate effect", func(t *testing.T) {
		topic := topicPrefix + ".handler-death"
		createIntegrationTopic(t, ctx, brokers, topic)
		codec := integrationCodec(t, topic)
		producer := integrationProducer(t, brokers, topic)
		dispatcher, err := gokafka.NewDispatcher(producer, codec)
		if err != nil {
			t.Fatalf("construct dispatcher: %v", err)
		}
		if err := dispatcher.Dispatch(ctx, integrationDeliveries(t)[:1]); err != nil {
			t.Fatalf("publish crash delivery: %v", err)
		}

		groupID := topic + ".group"
		effectPath := t.TempDir() + "/effects"
		runCrashWorker(t, brokers, topic, "", groupID, effectPath, "handler", crashAfterHandlingExit)
		if got := readCrashEffects(t, effectPath); !slices.Equal(got, []string{"message-1"}) {
			t.Fatalf("effects after crash = %q", got)
		}

		redeliveries := 0
		handler, err := gokafka.NewRecordHandler(
			codec,
			gokafka.DeliveryConsumerFunc(func(
				_ context.Context,
				delivery eventsourcing.Delivery,
			) error {
				redeliveries++
				if delivery.Message().ID().String() != "message-1" {
					return errors.New("redelivered identity changed")
				}
				if err := recordCrashEffectOnce(effectPath, "message-1"); err != nil {
					return fmt.Errorf("record idempotent effect: %w", err)
				}

				return nil
			}),
		)
		if err != nil {
			t.Fatalf("construct replacement handler: %v", err)
		}
		runIntegrationConsumer(t, ctx, brokers, topic, groupID, handler, 1)
		if redeliveries != 1 ||
			!slices.Equal(readCrashEffects(t, effectPath), []string{"message-1"}) {
			t.Fatalf("redeliveries/effects = %d/%q", redeliveries, readCrashEffects(t, effectPath))
		}
		assertGroupCommitted(t, ctx, brokers, topic, groupID, 1)
	})

	t.Run("death after dead letter acknowledgement repeats quarantine", func(t *testing.T) {
		sourceTopic := topicPrefix + ".poison"
		deadLetterTopic := sourceTopic + ".dead-letter"
		createIntegrationTopic(t, ctx, brokers, sourceTopic)
		createIntegrationTopic(t, ctx, brokers, deadLetterTopic)
		producer := integrationProducer(t, brokers, sourceTopic, deadLetterTopic)
		malformed := kafka.Message{
			Topic:     sourceTopic,
			Key:       []byte("malformed-key"),
			Value:     []byte("credential=must-not-leak"),
			Timestamp: time.Now().UTC(),
			Headers: []kafka.Header{
				{Key: "application", Value: []byte("panic=must-not-leak")},
			},
		}
		if err := producer.Publish(ctx, malformed); err != nil {
			t.Fatalf("publish poison record: %v", err)
		}

		groupID := sourceTopic + ".group"
		ackPath := t.TempDir() + "/dead-letter-acks"
		runCrashWorker(
			t,
			brokers,
			sourceTopic,
			deadLetterTopic,
			groupID,
			ackPath,
			"dead-letter",
			crashAfterDeadLetter,
		)
		if got := readCrashEffects(t, ackPath); !slices.Equal(got, []string{"acknowledged"}) {
			t.Fatalf("dead-letter acknowledgements before crash = %q", got)
		}

		codec := integrationCodec(t, sourceTopic, deadLetterTopic)
		policy, err := gokafka.NewDeadLetterPolicy(
			producer,
			gokafka.DeadLetterPolicyConfig{
				Topic:  deadLetterTopic,
				Limits: gokafka.DefaultRecordLimits(),
			},
		)
		if err != nil {
			t.Fatalf("construct replacement dead-letter policy: %v", err)
		}
		handler, err := gokafka.NewRecordHandler(
			codec,
			gokafka.DeliveryConsumerFunc(func(
				context.Context,
				eventsourcing.Delivery,
			) error {
				return errors.New("replacement-failure-do-not-copy")
			}),
			gokafka.WithFailurePolicy(policy),
		)
		if err != nil {
			t.Fatalf("construct replacement poison handler: %v", err)
		}
		runIntegrationConsumer(t, ctx, brokers, sourceTopic, groupID, handler, 1)
		assertGroupCommitted(t, ctx, brokers, sourceTopic, groupID, 1)

		identities := make([]string, 0, 2)
		idempotentPath := t.TempDir() + "/dead-letter-effects"
		runIntegrationConsumer(
			t,
			ctx,
			brokers,
			deadLetterTopic,
			deadLetterTopic+".group",
			kafka.HandlerFunc(func(
				_ context.Context,
				record kafka.ConsumedMessage,
			) error {
				identity := deadLetterIdentity(record.Headers)
				if identity == "" {
					return errors.New("dead-letter provenance is incomplete")
				}
				if err := recordCrashEffectOnce(idempotentPath, identity); err != nil {
					return fmt.Errorf("record idempotent dead-letter effect: %w", err)
				}
				for _, value := range append(
					[][]byte{record.Key, record.Value},
					headerValues(record.Headers)...,
				) {
					if strings.Contains(string(value), "worker-failure-do-not-copy") ||
						strings.Contains(string(value), "replacement-failure-do-not-copy") {
						return errors.New("failure diagnostic leaked to dead letter")
					}
				}
				identities = append(identities, identity)

				return nil
			}),
			2,
		)
		if len(identities) != 2 || identities[0] != identities[1] {
			t.Fatalf("dead-letter identities = %q", identities)
		}
		unique := make(map[string]struct{}, len(identities))
		for _, identity := range identities {
			unique[identity] = struct{}{}
		}
		if len(unique) != 1 {
			t.Fatalf("idempotent dead-letter identities = %d", len(unique))
		}
		if effects := readCrashEffects(t, idempotentPath); len(effects) != 1 ||
			effects[0] != identities[0] {
			t.Fatalf("idempotent dead-letter effects = %q", effects)
		}
	})

}

func TestKafkaCrashWorker(t *testing.T) {
	if os.Getenv(crashWorkerEnvironment) != "1" {
		t.Skip("helper process")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	brokers := strings.Split(os.Getenv("GOKAFKA_CRASH_BROKERS"), ",")
	topic := os.Getenv("GOKAFKA_CRASH_TOPIC")
	deadLetterTopic := os.Getenv("GOKAFKA_CRASH_DEAD_LETTER_TOPIC")
	groupID := os.Getenv("GOKAFKA_CRASH_GROUP")
	effectPath := os.Getenv("GOKAFKA_CRASH_EFFECT_PATH")
	mode := os.Getenv("GOKAFKA_CRASH_MODE")
	codec := integrationCodec(t, topic, deadLetterTopic)

	switch mode {
	case "handler":
		handler, err := gokafka.NewRecordHandler(
			codec,
			gokafka.DeliveryConsumerFunc(func(
				_ context.Context,
				delivery eventsourcing.Delivery,
			) error {
				if appendCrashEffect(
					effectPath,
					delivery.Message().ID().String(),
				) != nil {
					os.Exit(93)
				}
				os.Exit(crashAfterHandlingExit)
				return nil
			}),
		)
		if err != nil {
			t.Fatalf("construct crash handler: %v", err)
		}
		consumer := newIntegrationConsumer(t, brokers, topic, groupID)
		defer consumer.Close()
		_, _ = consumer.RunOnce(ctx, handler)
	case "dead-letter":
		producer := integrationProducer(t, brokers, topic, deadLetterTopic)
		policy, err := gokafka.NewDeadLetterPolicy(
			crashAfterAcknowledgementPublisher{
				publisher:  producer,
				effectPath: effectPath,
			},
			gokafka.DeadLetterPolicyConfig{
				Topic:  deadLetterTopic,
				Limits: gokafka.DefaultRecordLimits(),
			},
		)
		if err != nil {
			t.Fatalf("construct crash dead-letter policy: %v", err)
		}
		handler, err := gokafka.NewRecordHandler(
			codec,
			gokafka.DeliveryConsumerFunc(func(
				context.Context,
				eventsourcing.Delivery,
			) error {
				return errors.New("worker-failure-do-not-copy")
			}),
			gokafka.WithFailurePolicy(policy),
		)
		if err != nil {
			t.Fatalf("construct crash poison handler: %v", err)
		}
		consumer := newIntegrationConsumer(t, brokers, topic, groupID)
		defer consumer.Close()
		_, _ = consumer.RunOnce(ctx, handler)
	default:
		t.Fatalf("unknown crash mode %q", mode)
	}
	t.Fatal("crash worker returned without terminating")
}

type lostAcknowledgementPublisher struct {
	publisher gokafka.Publisher
	failure   error
	failed    bool
}

func (publisher *lostAcknowledgementPublisher) Publish(
	ctx context.Context,
	message kafka.Message,
) error {
	if err := publisher.publisher.Publish(ctx, message); err != nil {
		return err
	}
	if !publisher.failed {
		publisher.failed = true

		return publisher.failure
	}

	return nil
}

type crashAfterAcknowledgementPublisher struct {
	publisher  gokafka.Publisher
	effectPath string
}

func (publisher crashAfterAcknowledgementPublisher) Publish(
	ctx context.Context,
	message kafka.Message,
) error {
	if err := publisher.publisher.Publish(ctx, message); err != nil {
		return err
	}
	if appendCrashEffect(publisher.effectPath, "acknowledged") != nil {
		os.Exit(93)
	}
	os.Exit(crashAfterDeadLetter)

	return nil
}

func integrationCodec(t testing.TB, topics ...string) *gokafka.RecordCodec {
	t.Helper()

	allowedTopics := make([]string, 0, len(topics))
	for _, topic := range topics {
		if topic != "" {
			allowedTopics = append(allowedTopics, topic)
		}
	}
	codec, err := gokafka.NewRecordCodec(gokafka.RecordCodecConfig{
		Resolver:      gokafka.FixedTopic(allowedTopics[0]),
		AllowedTopics: allowedTopics,
	})
	if err != nil {
		t.Fatalf("construct integration codec: %v", err)
	}

	return codec
}

func integrationProducer(
	t testing.TB,
	brokers []string,
	topics ...string,
) *kafka.Producer {
	t.Helper()

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:                brokers,
		ClientID:               "event-sourcing-hardening-producer",
		AllowedTopics:          topics,
		Limits:                 gokafka.DefaultRecordLimits(),
		CompressionPreferences: []kafka.CompressionCodec{kafka.CompressionZstd},
		Security:               kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct integration producer: %v", err)
	}
	t.Cleanup(func() {
		if err := producer.Close(); err != nil {
			t.Errorf("close integration producer: %v", err)
		}
	})

	return producer
}

func runCrashWorker(
	t *testing.T,
	brokers []string,
	topic string,
	deadLetterTopic string,
	groupID string,
	effectPath string,
	mode string,
	wantExit int,
) {
	t.Helper()

	command := exec.Command(os.Args[0], "-test.run=^TestKafkaCrashWorker$")
	command.Env = append(os.Environ(),
		crashWorkerEnvironment+"=1",
		"GOKAFKA_CRASH_BROKERS="+strings.Join(brokers, ","),
		"GOKAFKA_CRASH_TOPIC="+topic,
		"GOKAFKA_CRASH_DEAD_LETTER_TOPIC="+deadLetterTopic,
		"GOKAFKA_CRASH_GROUP="+groupID,
		"GOKAFKA_CRASH_EFFECT_PATH="+effectPath,
		"GOKAFKA_CRASH_MODE="+mode,
	)
	output, err := command.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != wantExit {
		t.Fatalf(
			"crash worker error = %v, want exit %d; output = %s",
			err,
			wantExit,
			output,
		)
	}
}

func appendCrashEffect(path, value string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(value + "\n"); err != nil {
		_ = file.Close()

		return err
	}

	return file.Close()
}

func recordCrashEffectOnce(path, value string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if slices.Contains(strings.Fields(string(data)), value) {
		return nil
	}

	return appendCrashEffect(path, value)
}

func readCrashEffects(t testing.TB, path string) []string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read crash effects: %v", err)
	}

	return strings.Fields(string(data))
}

func deadLetterIdentity(headers []kafka.Header) string {
	values := make(map[string]string, 3)
	for _, header := range headers {
		switch header.Key {
		case gokafka.HeaderDeadLetterSourceTopic,
			gokafka.HeaderDeadLetterSourcePartition,
			gokafka.HeaderDeadLetterSourceOffset:
			values[header.Key] = string(header.Value)
		}
	}
	if values[gokafka.HeaderDeadLetterSourceTopic] == "" ||
		values[gokafka.HeaderDeadLetterSourcePartition] == "" ||
		values[gokafka.HeaderDeadLetterSourceOffset] == "" {
		return ""
	}

	return values[gokafka.HeaderDeadLetterSourceTopic] + "/" +
		values[gokafka.HeaderDeadLetterSourcePartition] + "/" +
		values[gokafka.HeaderDeadLetterSourceOffset]
}

func headerValues(headers []kafka.Header) [][]byte {
	values := make([][]byte, 0, len(headers))
	for _, header := range headers {
		values = append(values, header.Value)
	}

	return values
}

func newHardeningConsumer(
	t testing.TB,
	brokers []string,
	topic string,
	groupID string,
	clientID string,
) *kafka.Consumer {
	t.Helper()

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:               brokers,
		ClientID:              clientID,
		GroupID:               groupID,
		Topics:                []string{topic},
		ResetOffset:           kafka.OffsetEarliest,
		BalancePolicy:         kafka.BalanceCooperativeSticky,
		RebalanceHandler:      kafka.RebalanceDrainHandler,
		MaxPollRecords:        1,
		MaxConcurrentHandlers: 1,
		MaxAssignedPartitions: 2,
		FetchMaxWait:          100 * time.Millisecond,
		SessionTimeout:        6 * time.Second,
		RebalanceTimeout:      20 * time.Second,
		HeartbeatInterval:     time.Second,
		HandlerTimeout:        10 * time.Second,
		CommitTimeout:         3 * time.Second,
		DialTimeout:           10 * time.Second,
		Security:              kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct hardening consumer %q: %v", clientID, err)
	}

	return consumer
}

func waitForStableGroupMembers(
	t testing.TB,
	ctx context.Context,
	brokers []string,
	groupID string,
	members int,
) {
	t.Helper()

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatalf("construct rebalance inspector: %v", err)
	}
	defer client.Close()
	admin := kadm.NewClient(client)
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastState string
	for {
		attemptCtx, attemptCancel := context.WithTimeout(waitCtx, 2*time.Second)
		groups, describeErr := admin.DescribeGroups(attemptCtx, groupID)
		attemptCancel()
		if describeErr == nil {
			if group, exists := groups[groupID]; exists && group.Err == nil {
				lastState = fmt.Sprintf("%s/%d", group.State, len(group.Members))
				if group.State == "Stable" && len(group.Members) == members {
					return
				}
			}
		}
		select {
		case <-waitCtx.Done():
			t.Fatalf("wait for %d stable group members: %s", members, lastState)
		case <-ticker.C:
		}
	}
}

func waitForGroupMembershipChange(
	t testing.TB,
	ctx context.Context,
	brokers []string,
	groupID string,
	initialMembers int,
) {
	t.Helper()

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatalf("construct rebalance transition inspector: %v", err)
	}
	defer client.Close()
	admin := kadm.NewClient(client)
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastState string
	for {
		attemptCtx, attemptCancel := context.WithTimeout(waitCtx, 2*time.Second)
		groups, describeErr := admin.DescribeGroups(attemptCtx, groupID)
		attemptCancel()
		if describeErr == nil {
			if group, exists := groups[groupID]; exists && group.Err == nil {
				lastState = fmt.Sprintf("%s/%d", group.State, len(group.Members))
				if group.State != "Stable" || len(group.Members) != initialMembers {
					return
				}
			}
		}
		select {
		case <-waitCtx.Done():
			t.Fatalf("wait for group membership change: %s", lastState)
		case <-ticker.C:
		}
	}
}
