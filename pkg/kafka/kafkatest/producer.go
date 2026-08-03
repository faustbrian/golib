package kafkatest

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
)

const operationTimeout = 30 * time.Second

// RunProducerConformance proves synchronous, batch, asynchronous, ownership,
// delivery-metadata, explicit-partition, and post-close producer contracts
// against an isolated Kafka broker fixture.
func RunProducerConformance(t *testing.T, harness BrokerHarness) {
	t.Helper()
	if err := harness.Validate(); err != nil {
		t.Fatal(err)
	}

	t.Run("synchronous binary-safe owned delivery", func(t *testing.T) {
		topic := harness.NewTopic(t, 1)
		producer := newConformanceProducer(t, harness, topic, "sync")
		key := []byte{0, 1, 2, 255}
		value := []byte{255, 2, 1, 0}
		headerValue := []byte{3, 0, 4, 255}
		timestamp := time.Date(2023, time.January, 2, 3, 4, 5, 0, time.UTC)
		result := producer.PublishRecord(t.Context(), kafka.ProducerRecord{
			Topic: topic, Partition: kafka.ExplicitPartition(0),
			Key: key, Value: value,
			Headers:   []kafka.Header{{Key: "binary", Value: headerValue}},
			Timestamp: timestamp,
		})
		if result.Err != nil || result.Topic != topic || result.Partition != 0 ||
			result.Offset < 0 || result.Timestamp.IsZero() {
			t.Fatalf("PublishRecord() = %#v, error = %v", result, result.Err)
		}
		key[0], value[0], headerValue[0] = 9, 8, 7
		records := readConformanceRecords(t, harness, ReadRequest{
			Topic: topic, Partition: 0, StartOffset: result.Offset,
			MaxRecords: 1, Isolation: ReadCommitted,
		})
		if len(records) != 1 || !bytes.Equal(records[0].Key, []byte{0, 1, 2, 255}) ||
			!bytes.Equal(records[0].Value, []byte{255, 2, 1, 0}) ||
			len(records[0].Headers) != 1 || records[0].Headers[0].Key != "binary" ||
			!bytes.Equal(records[0].Headers[0].Value, []byte{3, 0, 4, 255}) ||
			records[0].Offset != result.Offset || records[0].Partition != 0 ||
			!records[0].Timestamp.Equal(timestamp) {
			t.Fatalf("broker record count = %d or metadata/owned bytes differ", len(records))
		}
	})

	t.Run("batch results preserve input order across partitions", func(t *testing.T) {
		topic := harness.NewTopic(t, 2)
		producer := newConformanceProducer(t, harness, topic, "batch")
		records := []kafka.ProducerRecord{
			{Topic: topic, Partition: kafka.ExplicitPartition(1), Key: []byte("p1"), Value: []byte("first")},
			{Topic: topic, Partition: kafka.ExplicitPartition(0), Key: []byte("p0"), Value: []byte("second")},
			{Topic: topic, Partition: kafka.ExplicitPartition(1), Key: []byte("p1"), Value: []byte("third")},
		}
		results, err := producer.PublishBatch(t.Context(), records)
		if err != nil || len(results) != len(records) {
			t.Fatalf("PublishBatch() = %#v, %v", results, err)
		}
		for index, result := range results {
			if result.Err != nil || result.Topic != topic ||
				result.Partition != records[index].Partition.Partition || result.Offset < 0 {
				t.Fatalf("result[%d] = %#v", index, result)
			}
		}
		if results[0].Offset >= results[2].Offset {
			t.Fatalf("partition-one offsets = %d, %d", results[0].Offset, results[2].Offset)
		}
	})

	t.Run("asynchronous admission owns input and resolves once", func(t *testing.T) {
		topic := harness.NewTopic(t, 1)
		producer := newConformanceProducer(t, harness, topic, "async")
		key, value := []byte("async-key"), []byte("async-value")
		results, err := producer.PublishAsync(t.Context(), kafka.ProducerRecord{
			Topic: topic, Partition: kafka.ExplicitPartition(0), Key: key, Value: value,
		})
		if err != nil {
			t.Fatalf("PublishAsync() error = %v", err)
		}
		key[0], value[0] = 'X', 'X'
		timer := time.NewTimer(operationTimeout)
		defer timer.Stop()
		select {
		case result, open := <-results:
			if !open || result.Err != nil || result.Topic != topic || result.Partition != 0 || result.Offset < 0 {
				t.Fatalf("asynchronous result = %#v, open=%t", result, open)
			}
			if _, open := <-results; open {
				t.Fatal("asynchronous result channel remained open")
			}
		case <-timer.C:
			t.Fatal("asynchronous result did not resolve within the conformance bound")
		}
		records := readConformanceRecords(t, harness, ReadRequest{
			Topic: topic, Partition: 0, StartOffset: 0, MaxRecords: 1,
			Isolation: ReadCommitted,
		})
		if len(records) != 1 || string(records[0].Key) != "async-key" ||
			string(records[0].Value) != "async-value" {
			t.Fatalf("broker record count = %d", len(records))
		}
	})

	t.Run("close fences later delivery", func(t *testing.T) {
		topic := harness.NewTopic(t, 1)
		producer := newConformanceProducer(t, harness, topic, "close")
		if err := producer.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		result := producer.PublishRecord(t.Context(), kafka.ProducerRecord{
			Topic: topic, Key: []byte("closed"), Value: []byte("closed"),
		})
		if !errors.Is(result.Err, kafka.ErrProducerClosed) {
			t.Fatalf("post-close PublishRecord() error = %v", result.Err)
		}
	})
}

func newConformanceProducer(
	t *testing.T,
	harness BrokerHarness,
	topic string,
	suffix string,
) *kafka.Producer {
	t.Helper()
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       append([]string(nil), harness.Brokers...),
		ClientID:      "kafkatest-producer-" + suffix,
		AllowedTopics: []string{topic},
		Security:      harness.Security,
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	t.Cleanup(func() {
		if err := producer.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	return producer
}

func readConformanceRecords(
	t *testing.T,
	harness BrokerHarness,
	request ReadRequest,
) []kafka.ConsumedRecord {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), operationTimeout)
	defer cancel()
	records, err := harness.ReadRecords(ctx, request)
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}

	return records
}
