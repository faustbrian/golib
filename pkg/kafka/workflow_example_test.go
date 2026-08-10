package kafka_test

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
)

func ExampleProducer_PublishRecord() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{"kafka.internal:9093"},
		ClientID:      "orders-api",
		AllowedTopics: []string{"orders.created.v1"},
	})
	if err != nil {
		log.Fatal(err)
	}

	result := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic: "orders.created.v1",
		Key:   []byte("order-123"),
		Value: []byte(`{"order_id":"order-123"}`),
	})
	if err := errors.Join(result.Err, producer.Close()); err != nil {
		// A timeout can be ambiguous. Reconcile it before deciding to retry.
		log.Fatal(err)
	}
	log.Printf("delivered to %s[%d] at offset %d",
		result.Topic,
		result.Partition,
		result.Offset,
	)
}

func ExampleProducer_PublishBatch() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{"kafka.internal:9093"},
		ClientID:      "orders-importer",
		AllowedTopics: []string{"orders.created.v1"},
	})
	if err != nil {
		log.Fatal(err)
	}

	results, publishErr := producer.PublishBatch(ctx, []kafka.ProducerRecord{
		{
			Topic: "orders.created.v1",
			Key:   []byte("order-123"),
			Value: []byte(`{"order_id":"order-123"}`),
		},
		{
			Topic: "orders.created.v1",
			Key:   []byte("order-124"),
			Value: []byte(`{"order_id":"order-124"}`),
		},
	})
	if publishErr != nil {
		// Results remain input-ordered and identify definite successes beside
		// failures. Reconcile them instead of retrying the complete batch.
		for index, result := range results {
			log.Printf("record %d: topic=%s partition=%d offset=%d error=%v",
				index,
				result.Topic,
				result.Partition,
				result.Offset,
				result.Err,
			)
		}
	}

	if err := errors.Join(publishErr, producer.Close()); err != nil {
		log.Fatal(err)
	}
}

func ExampleProducer_PublishAsync() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{"kafka.internal:9093"},
		ClientID:      "orders-api",
		AllowedTopics: []string{"orders.created.v1"},
	})
	if err != nil {
		log.Fatal(err)
	}

	delivery, err := producer.PublishAsync(ctx, kafka.ProducerRecord{
		Topic: "orders.created.v1",
		Key:   []byte("order-123"),
		Value: []byte(`{"order_id":"order-123"}`),
	})
	if err != nil {
		log.Fatal(errors.Join(err, producer.Close()))
	}

	var result kafka.DeliveryResult
	select {
	case result = <-delivery:
	case <-ctx.Done():
		// PublishAsync continues resolving an admitted record. Shutdown below
		// drains it rather than silently dropping it. A successful close means
		// the buffered result is now available for reconciliation.
		log.Print(ctx.Err())
		if err := producer.Close(); err != nil {
			log.Fatal(err)
		}
		result = <-delivery
		if result.Err != nil {
			log.Fatal(result.Err)
		}

		return
	}

	if err := errors.Join(result.Err, producer.Close()); err != nil {
		log.Fatal(err)
	}
}

func ExampleConsumer_Run() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:       []string{"kafka.internal:9093"},
		ClientID:      "billing-projection",
		GroupID:       "billing-projection-v1",
		Topics:        []string{"orders.created.v1"},
		ResetOffset:   kafka.OffsetEarliest,
		BalancePolicy: kafka.BalanceCooperativeSticky,
	})
	if err != nil {
		log.Fatal(err)
	}

	runErr := consumer.Run(ctx, kafka.HandlerFunc(func(
		ctx context.Context,
		record kafka.ConsumedRecord,
	) error {
		return persistProjection(ctx, record)
	}))
	if err := errors.Join(runErr, consumer.Close()); err != nil {
		log.Fatal(err)
	}
}

func persistProjection(context.Context, kafka.ConsumedRecord) error {
	// Application code must atomically persist an idempotent side effect and
	// its source topic, partition, and offset before returning nil.
	return nil
}

func ExampleProducer_RunTransaction() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:         []string{"kafka.internal:9093"},
		ClientID:        "billing-writer",
		AllowedTopics:   []string{"billing.balance.v1"},
		TransactionalID: "billing-writer-instance-7",
	})
	if err != nil {
		log.Fatal(err)
	}

	transactionErr := producer.RunTransaction(ctx, func(
		transaction kafka.Transaction,
	) error {
		return transaction.Publish(ctx, kafka.ProducerRecord{
			Topic: "billing.balance.v1",
			Key:   []byte("account-123"),
			Value: []byte(`{"balance":4200}`),
		})
	})
	if err := errors.Join(transactionErr, producer.Close()); err != nil {
		// An unknown commit outcome must be reconciled, not blindly retried.
		log.Fatal(err)
	}
}

func ExampleNewTransactionProcessor() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	processor, err := kafka.NewTransactionProcessor(
		kafka.TransactionProcessorConfig{
			Connection: kafka.TransactionConnectionConfig{
				Brokers:  []string{"kafka.internal:9093"},
				ClientID: "billing-projection",
			},
			Group: kafka.TransactionGroupConfig{
				GroupID:     "billing-projection-v1",
				Topics:      []string{"orders.created.v1"},
				ResetOffset: kafka.OffsetEarliest,
			},
			Output: kafka.TransactionOutputConfig{
				AllowedTopics:   []string{"billing.balance.v1"},
				TransactionalID: "billing-projection-instance-7",
			},
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	runErr := processor.Run(ctx, kafka.TransactionHandlerFunc(func(
		ctx context.Context,
		source kafka.ConsumedRecord,
		transaction kafka.Transaction,
	) error {
		return transaction.Publish(ctx, kafka.ProducerRecord{
			Topic: "billing.balance.v1",
			Key:   source.Key,
			Value: source.Value,
		})
	}))
	if err := errors.Join(runErr, processor.Close()); err != nil {
		log.Fatal(err)
	}
}

func ExampleReplayReader_Replay() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reader, err := kafka.NewReplayReader(kafka.ReplayConfig{
		Brokers:  []string{"kafka.internal:9093"},
		ClientID: "billing-replay-2026-08-10",
		Ranges: []kafka.ReplayRange{{
			Topic:       "orders.created.v1",
			Partition:   0,
			StartOffset: 100,
			EndOffset:   200,
		}},
		SideEffects: kafka.ReplaySideEffectsAllowed,
	})
	if err != nil {
		log.Fatal(err)
	}

	plan, err := reader.PlanAgainstBroker(ctx)
	if err != nil {
		log.Fatal(errors.Join(err, reader.Close()))
	}
	log.Printf("reviewed %d retained records", plan.TotalRemaining)

	result, replayErr := reader.Replay(ctx, kafka.ReplayHandlerFunc(func(
		ctx context.Context,
		record kafka.ReplayRecord,
	) error {
		// The application must make replay side effects idempotent.
		return persistProjection(ctx, record.ConsumedRecord)
	}))
	if err := errors.Join(replayErr, reader.Close()); err != nil {
		// Persist the checkpoint externally before constructing a new reader.
		checkpoint := result.Checkpoint()
		log.Printf("replay incomplete at %+v", checkpoint.Positions)
		log.Fatal(err)
	}
}
