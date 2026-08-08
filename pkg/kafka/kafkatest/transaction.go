package kafkatest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
)

// RunTransactionConformance proves Kafka transaction commit and abort
// isolation, callback-lifetime fencing, and atomic consume-transform-produce
// source-offset settlement against an isolated broker fixture.
func RunTransactionConformance(t *testing.T, harness BrokerHarness) {
	t.Helper()
	if err := harness.Validate(); err != nil {
		t.Fatal(err)
	}
	historicalTimestamp := time.Date(2023, time.January, 2, 3, 4, 5, 0, time.UTC)

	t.Run("producer commit abort and callback fencing", func(t *testing.T) {
		topic := harness.NewTopic(t, 1)
		producer := newConformanceTransactionalProducer(t, harness, topic, "producer")
		var escaped kafka.Transaction
		if err := producer.RunTransaction(t.Context(), func(transaction kafka.Transaction) error {
			escaped = transaction
			diagnostic := producer.Diagnostic()
			if diagnostic.Accepting || !diagnostic.TransactionsEnabled ||
				!diagnostic.TransactionActive || diagnostic.Fatal {
				t.Fatalf("transactional producer Diagnostic() = %#v", diagnostic)
			}
			return transaction.Publish(t.Context(), kafka.ProducerRecord{
				Topic: topic, Partition: kafka.ExplicitPartition(0),
				Key:       []byte("committed"),
				Value:     []byte("committed"),
				Timestamp: historicalTimestamp,
			})
		}); err != nil {
			t.Fatalf("committed RunTransaction() error = %v", err)
		}
		if err := escaped.Publish(t.Context(), kafka.ProducerRecord{
			Topic: topic, Key: []byte("escaped"), Value: []byte("escaped"),
		}); !errors.Is(err, kafka.ErrTransactionClosed) {
			t.Fatalf("escaped transaction Publish() error = %v", err)
		}

		abortCause := errors.New("kafkatest: abort transaction")
		err := producer.RunTransaction(t.Context(), func(transaction kafka.Transaction) error {
			if err := transaction.Publish(t.Context(), kafka.ProducerRecord{
				Topic: topic, Partition: kafka.ExplicitPartition(0),
				Key: []byte("aborted"), Value: []byte("aborted"),
			}); err != nil {
				return err
			}
			return abortCause
		})
		if !errors.Is(err, abortCause) {
			t.Fatalf("aborted RunTransaction() error = %v", err)
		}

		committed := readConformanceRecords(t, harness, ReadRequest{
			Topic: topic, Partition: 0, StartOffset: 0, MaxRecords: 1,
			Isolation: ReadCommitted,
		})
		if len(committed) != 1 || string(committed[0].Value) != "committed" ||
			!committed[0].Timestamp.Equal(historicalTimestamp) {
			t.Fatalf("read-committed record count = %d", len(committed))
		}
		uncommitted := readConformanceRecords(t, harness, ReadRequest{
			Topic: topic, Partition: 0, StartOffset: 0, MaxRecords: 2,
			Isolation: ReadUncommitted,
		})
		if len(uncommitted) != 2 || string(uncommitted[0].Value) != "committed" ||
			string(uncommitted[1].Value) != "aborted" {
			t.Fatalf("read-uncommitted record count = %d", len(uncommitted))
		}
	})

	t.Run("consume transform produce commits output with source offset", func(t *testing.T) {
		source := harness.NewTopic(t, 1)
		output := harness.NewTopic(t, 1)
		publishConformanceValues(t, harness, source, "source")
		group := nextConformanceGroup("transaction-processor")
		processor := newConformanceTransactionProcessor(
			t,
			harness,
			source,
			output,
			group,
			"commit",
		)
		ctx, cancel := context.WithTimeout(t.Context(), operationTimeout)
		defer cancel()
		var sourceRecord kafka.ConsumedRecord
		var result kafka.TransactionPollResult
		var err error
		for result.Polled == 0 && err == nil {
			result, err = processor.RunOnce(ctx, kafka.TransactionHandlerFunc(func(
				callbackCtx context.Context,
				record kafka.ConsumedRecord,
				transaction kafka.Transaction,
			) error {
				sourceRecord = record.Retain()
				diagnostic := processor.Diagnostic()
				if diagnostic.Accepting || !diagnostic.Running ||
					!diagnostic.TransactionActive || diagnostic.Fatal {
					t.Fatalf("transaction processor Diagnostic() = %#v", diagnostic)
				}
				return transaction.Publish(callbackCtx, kafka.ProducerRecord{
					Topic: output, Partition: kafka.ExplicitPartition(0),
					Key:       append([]byte(nil), record.Key...),
					Value:     []byte("transformed-" + string(record.Value)),
					Timestamp: historicalTimestamp,
				})
			}))
		}
		if err != nil || result.Polled != 1 || result.Processed != 1 ||
			result.Published != 1 || !result.Committed || sourceRecord.Topic != source ||
			sourceRecord.Offset != 0 {
			t.Fatalf("RunOnce() = %#v, %v, source=%s/%d@%d", result, err, sourceRecord.Topic, sourceRecord.Partition, sourceRecord.Offset)
		}
		diagnostic := processor.Diagnostic()
		if !diagnostic.Accepting || diagnostic.Running ||
			diagnostic.TransactionActive || diagnostic.Fatal {
			t.Fatalf("completed processor Diagnostic() = %#v", diagnostic)
		}
		outputs := readConformanceRecords(t, harness, ReadRequest{
			Topic: output, Partition: 0, StartOffset: 0, MaxRecords: 1,
			Isolation: ReadCommitted,
		})
		if len(outputs) != 1 || string(outputs[0].Value) != "transformed-source" ||
			!outputs[0].Timestamp.Equal(historicalTimestamp) {
			t.Fatalf("transactional output count = %d", len(outputs))
		}
		assertConformanceCommittedOffset(t, harness, group, source, 0, 1)
	})
}

func newConformanceTransactionalProducer(
	t *testing.T,
	harness BrokerHarness,
	topic string,
	suffix string,
) *kafka.Producer {
	t.Helper()
	identity := fmt.Sprintf(
		"kafkatest-transaction-%s-%d-%d",
		suffix,
		time.Now().UnixNano(),
		groupSequence.Add(1),
	)
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:         append([]string(nil), harness.Brokers...),
		ClientID:        identity,
		AllowedTopics:   []string{topic},
		TransactionalID: identity,
		Security:        harness.Security,
	})
	if err != nil {
		t.Fatalf("NewProducer(transactional) error = %v", err)
	}
	t.Cleanup(func() {
		if err := producer.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	return producer
}

func newConformanceTransactionProcessor(
	t *testing.T,
	harness BrokerHarness,
	source string,
	output string,
	group string,
	suffix string,
) *kafka.TransactionProcessor {
	t.Helper()
	identity := fmt.Sprintf(
		"kafkatest-processor-%s-%d-%d",
		suffix,
		time.Now().UnixNano(),
		groupSequence.Add(1),
	)
	processor, err := kafka.NewTransactionProcessor(kafka.TransactionProcessorConfig{
		Connection: kafka.TransactionConnectionConfig{
			Brokers:  append([]string(nil), harness.Brokers...),
			ClientID: identity,
			Security: harness.Security,
		},
		Group: kafka.TransactionGroupConfig{
			GroupID:        group,
			Topics:         []string{source},
			ResetOffset:    kafka.OffsetEarliest,
			MaxPollRecords: 100,
		},
		Output: kafka.TransactionOutputConfig{
			AllowedTopics:   []string{output},
			TransactionalID: identity,
		},
	})
	if err != nil {
		t.Fatalf("NewTransactionProcessor() error = %v", err)
	}
	t.Cleanup(func() {
		if err := processor.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	return processor
}
