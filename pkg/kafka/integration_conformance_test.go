//go:build interoperability

package kafka_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/kafkatest"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestPublicConformance(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	container, err := tckafka.Run(ctx, integrationKafkaImage)
	if err != nil {
		t.Fatalf("start Kafka: %v", err)
	}
	cleanupKafkaContainer(t, container)
	assertIntegrationKafkaVersion(t, ctx, container)
	brokers, err := container.Brokers(ctx)
	if err != nil {
		t.Fatalf("resolve Kafka brokers: %v", err)
	}

	var topicSequence atomic.Uint64
	harness := kafkatest.BrokerHarness{
		Brokers:  brokers,
		Security: kafka.DevelopmentPlaintextSecurity(),
		NewTopic: func(tb *testing.T, partitions int) string {
			tb.Helper()
			topic := fmt.Sprintf(
				"golib-conformance-%d-%d",
				time.Now().UnixNano(),
				topicSequence.Add(1),
			)
			topicCtx, topicCancel := context.WithTimeout(tb.Context(), 30*time.Second)
			defer topicCancel()
			createIntegrationTopic(tb, topicCtx, brokers, topic, int32(partitions))

			return topic
		},
		ReadRecords: func(
			readCtx context.Context,
			request kafkatest.ReadRequest,
		) ([]kafka.ConsumedRecord, error) {
			return readConformanceRecords(readCtx, brokers, request)
		},
		CommittedOffset: func(
			offsetCtx context.Context,
			group string,
			topic string,
			partition int32,
		) (int64, error) {
			return conformanceCommittedOffset(offsetCtx, brokers, group, topic, partition)
		},
	}

	kafkatest.RunProducerConformance(t, harness)
	kafkatest.RunConsumerConformance(t, harness)
	kafkatest.RunTransactionConformance(t, harness)
	kafkatest.RunReplayConformance(t, harness)
	kafkatest.RunInspectorConformance(t, harness)
}

func readConformanceRecords(
	ctx context.Context,
	brokers []string,
	request kafkatest.ReadRequest,
) ([]kafka.ConsumedRecord, error) {
	isolation := kgo.ReadUncommitted()
	if request.Isolation == kafkatest.ReadCommitted {
		isolation = kgo.ReadCommitted()
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-public-conformance-reader"),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			request.Topic: {request.Partition: kgo.NewOffset().At(request.StartOffset)},
		}),
		kgo.FetchIsolationLevel(isolation),
		kgo.DialTimeout(10*time.Second),
	)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	result := make([]kafka.ConsumedRecord, 0, request.MaxRecords)
	for len(result) < request.MaxRecords {
		fetches := client.PollRecords(ctx, request.MaxRecords-len(result))
		if err := fetches.Err(); err != nil {
			return nil, err
		}
		for _, record := range fetches.Records() {
			if record.Topic != request.Topic || record.Partition != request.Partition ||
				record.Offset < request.StartOffset {
				continue
			}
			headers := make([]kafka.Header, len(record.Headers))
			for index := range record.Headers {
				headers[index] = kafka.Header{
					Key:   record.Headers[index].Key,
					Value: append([]byte(nil), record.Headers[index].Value...),
				}
			}
			result = append(result, kafka.ConsumedRecord{
				Topic: record.Topic, Partition: record.Partition, Offset: record.Offset,
				Key:     append([]byte(nil), record.Key...),
				Value:   append([]byte(nil), record.Value...),
				Headers: headers, Timestamp: record.Timestamp,
				TimestampType: kafka.TimestampType(record.Attrs.TimestampType()),
				LeaderEpoch:   record.LeaderEpoch,
			})
		}
	}

	return result, nil
}

func conformanceCommittedOffset(
	ctx context.Context,
	brokers []string,
	group string,
	topic string,
	partition int32,
) (int64, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-public-conformance-offset-reader"),
		kgo.DialTimeout(10*time.Second),
	)
	if err != nil {
		return -1, err
	}
	defer client.Close()
	offsets, err := kadm.NewClient(client).FetchOffsets(kadm.RequireStable(ctx), group)
	if err != nil {
		return -1, err
	}
	offset, exists := offsets.Lookup(topic, partition)
	if !exists {
		return -1, nil
	}
	if offset.Err != nil {
		return -1, offset.Err
	}

	return offset.At, nil
}
