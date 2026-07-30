//go:build integration

package clients_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/IBM/sarama"
	policy "github.com/faustbrian/golib/pkg/kafka"
	segmentkafka "github.com/segmentio/kafka-go"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const benchmarkReplayOperationTimeout = 30 * time.Second

type benchmarkReplayCandidate struct {
	name string
	run  func(
		context.Context,
		[]string,
		string,
		int64,
		int64,
		func(benchmarkConsumedRecord) error,
	) error
}

var benchmarkReplayCandidates = []benchmarkReplayCandidate{
	{name: "golib-policy", run: runPolicyBenchmarkReplay},
	{name: "raw-franz-go", run: runFranzBenchmarkReplay},
	{name: "kafka-go", run: runKafkaGoBenchmarkReplay},
	{name: "sarama", run: runSaramaBenchmarkReplay},
}

func BenchmarkEquivalentReplay(benchmark *testing.B) {
	brokers := benchmarkBrokers(benchmark)
	for _, recordCount := range []int{10, 100} {
		for _, payloadBytes := range []int{128, 1024} {
			for _, compression := range []benchmarkCompression{
				compressionNone,
				compressionSnappy,
			} {
				name := fmt.Sprintf(
					"%d-records/%dB/%s",
					recordCount,
					payloadBytes,
					compression,
				)
				benchmark.Run(name, func(benchmark *testing.B) {
					benchmarkEquivalentReplay(
						benchmark,
						brokers,
						recordCount,
						payloadBytes,
						compression,
					)
				})
			}
		}
	}
}

func benchmarkEquivalentReplay(
	benchmark *testing.B,
	brokers []string,
	recordCount int,
	payloadBytes int,
	compression benchmarkCompression,
) {
	benchmark.Helper()
	topic := ensureBenchmarkConsumerTopic(
		benchmark,
		brokers,
		recordCount,
		payloadBytes,
		compression,
	)
	for _, candidate := range benchmarkReplayCandidates {
		benchmark.Run(candidate.name, func(benchmark *testing.B) {
			var consumedBytes int64
			handler := func(record benchmarkConsumedRecord) error {
				consumedBytes += int64(len(record.key) + len(record.value))

				return nil
			}
			warmupCtx, warmupCancel := context.WithTimeout(
				context.Background(),
				benchmarkReplayOperationTimeout,
			)
			err := candidate.run(
				warmupCtx,
				brokers,
				topic,
				0,
				int64(recordCount),
				handler,
			)
			warmupCancel()
			if err != nil {
				benchmark.Fatalf("warm replay: %v", err)
			}
			consumedBytes = 0
			benchmark.ReportAllocs()
			benchmark.SetBytes(
				int64(benchmarkConsumerOperationBytes(recordCount, payloadBytes)),
			)
			benchmark.ResetTimer()
			for benchmark.Loop() {
				ctx, cancel := context.WithTimeout(
					context.Background(),
					benchmarkReplayOperationTimeout,
				)
				err := candidate.run(
					ctx,
					brokers,
					topic,
					0,
					int64(recordCount),
					handler,
				)
				cancel()
				if err != nil {
					benchmark.Fatalf("replay: %v", err)
				}
			}
			benchmark.StopTimer()
			benchmark.ReportMetric(
				float64(recordCount),
				"records/op",
			)
			if consumedBytes != int64(benchmark.N)*
				int64(benchmarkConsumerOperationBytes(recordCount, payloadBytes)) {
				benchmark.Fatalf(
					"consumed bytes = %d, want %d",
					consumedBytes,
					int64(benchmark.N)*
						int64(benchmarkConsumerOperationBytes(recordCount, payloadBytes)),
				)
			}
		})
	}
}

func TestEquivalentReplayOutcomes(t *testing.T) {
	brokers := benchmarkBrokers(t)
	const payloadBytes = 128
	topic := ensureBenchmarkConsumerTopic(
		t,
		brokers,
		3,
		payloadBytes,
		compressionNone,
	)
	want := benchmarkConsumerExpectedRecords(topic, 3, payloadBytes)[1:3]

	for _, candidate := range benchmarkReplayCandidates {
		t.Run(candidate.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(
				context.Background(),
				benchmarkReplayOperationTimeout,
			)
			defer cancel()
			var got []benchmarkConsumedRecord
			err := candidate.run(
				ctx,
				brokers,
				topic,
				1,
				3,
				func(record benchmarkConsumedRecord) error {
					got = append(got, retainBenchmarkConsumedRecord(record))

					return nil
				},
			)
			if err != nil {
				t.Fatalf("replay [1,3): %v", err)
			}
			if !slices.EqualFunc(got, want, equalBenchmarkConsumedRecord) {
				t.Fatalf("replay [1,3) = %#v, want %#v", got, want)
			}
		})
	}
}

func runPolicyBenchmarkReplay(
	ctx context.Context,
	brokers []string,
	topic string,
	start int64,
	end int64,
	handler func(benchmarkConsumedRecord) error,
) (resultErr error) {
	reader, err := policy.NewReplayReader(policy.ReplayConfig{
		Brokers:  brokers,
		ClientID: "golib-policy-replay-benchmark",
		Ranges: []policy.ReplayRange{{
			Topic:       topic,
			Partition:   0,
			StartOffset: start,
			EndOffset:   end,
		}},
		Security:               policy.DevelopmentPlaintextSecurity(),
		SideEffects:            policy.ReplaySideEffectsAllowed,
		MaxPollRecords:         int(end - start),
		MaxConcurrentFetches:   1,
		MaxConcurrentHandlers:  1,
		FetchMaxBytes:          benchmarkBatchBytes,
		FetchMaxPartitionBytes: benchmarkBatchBytes,
		FetchMaxWait:           500 * time.Millisecond,
		PlanningTimeout:        benchmarkRequestTimeout,
		ProgressTimeout:        benchmarkReplayOperationTimeout,
		HandlerTimeout:         benchmarkReplayOperationTimeout,
		ShutdownTimeout:        benchmarkReplayOperationTimeout,
		DialTimeout:            benchmarkRequestTimeout,
	})
	if err != nil {
		return fmt.Errorf("construct policy replay: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			benchmarkReplayOperationTimeout,
		)
		defer cancel()
		resultErr = errors.Join(resultErr, reader.Shutdown(shutdownCtx))
	}()
	result, err := reader.Replay(
		ctx,
		policy.ReplayHandlerFunc(func(
			_ context.Context,
			record policy.ReplayRecord,
		) error {
			return handler(benchmarkConsumedRecord{
				topic:     record.Topic,
				partition: record.Partition,
				offset:    record.Offset,
				key:       record.Key,
				value:     record.Value,
			})
		}),
	)
	if err != nil {
		return err
	}
	if result.Processed != end-start ||
		result.Failed != 0 ||
		result.Skipped != 0 ||
		result.IncompleteRanges != 0 ||
		result.CompletedRanges != 1 {
		return fmt.Errorf("unexpected policy replay result: %#v", result)
	}

	return nil
}

func runFranzBenchmarkReplay(
	ctx context.Context,
	brokers []string,
	topic string,
	start int64,
	end int64,
	handler func(benchmarkConsumedRecord) error,
) error {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("raw-franz-go-replay-benchmark"),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			topic: {0: kgo.NoResetOffset().At(start)},
		}),
		kgo.MaxConcurrentFetches(1),
		kgo.FetchMaxBytes(benchmarkBatchBytes),
		kgo.FetchMaxPartitionBytes(benchmarkBatchBytes),
		kgo.FetchMaxWait(500*time.Millisecond),
		kgo.DialTimeout(benchmarkRequestTimeout),
	)
	if err != nil {
		return fmt.Errorf("construct raw franz-go replay: %w", err)
	}
	defer client.Close()
	admin := kadm.NewClient(client)
	starts, err := admin.ListStartOffsets(ctx, topic)
	if err != nil {
		return fmt.Errorf("list raw franz-go replay starts: %w", err)
	}
	ends, err := admin.ListEndOffsets(ctx, topic)
	if err != nil {
		return fmt.Errorf("list raw franz-go replay ends: %w", err)
	}
	brokerStart, brokerEnd, err := franzReplayBounds(starts, ends, topic)
	if err != nil {
		return err
	}
	if err := validateBenchmarkReplayBounds(
		start,
		end,
		brokerStart,
		brokerEnd,
	); err != nil {
		return err
	}

	next := start
	for next < end {
		fetches := client.PollRecords(ctx, int(end-next))
		if err := fetches.Err(); err != nil {
			return fmt.Errorf("poll raw franz-go replay: %w", err)
		}
		records := fetches.Records()
		if len(records) == 0 {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}

			continue
		}
		for _, record := range records {
			if record.Offset >= end {
				break
			}
			if record.Topic != topic ||
				record.Partition != 0 ||
				record.Offset != next {
				return fmt.Errorf(
					"raw franz-go replay record = %s[%d]@%d, want %s[0]@%d",
					record.Topic,
					record.Partition,
					record.Offset,
					topic,
					next,
				)
			}
			if err := handler(benchmarkConsumedRecord{
				topic:     record.Topic,
				partition: record.Partition,
				offset:    record.Offset,
				key:       record.Key,
				value:     record.Value,
			}); err != nil {
				return err
			}
			next++
		}
	}

	return nil
}

func franzReplayBounds(
	starts kadm.ListedOffsets,
	ends kadm.ListedOffsets,
	topic string,
) (int64, int64, error) {
	start, startExists := starts.Lookup(topic, 0)
	end, endExists := ends.Lookup(topic, 0)
	if !startExists || !endExists {
		return 0, 0, errors.New("raw franz-go replay bounds are missing")
	}
	if start.Err != nil || end.Err != nil {
		return 0, 0, errors.Join(start.Err, end.Err)
	}

	return start.Offset, end.Offset, nil
}

func runKafkaGoBenchmarkReplay(
	ctx context.Context,
	brokers []string,
	topic string,
	start int64,
	end int64,
	handler func(benchmarkConsumedRecord) error,
) (resultErr error) {
	transport := &segmentkafka.Transport{
		ClientID:    "kafka-go-replay-benchmark",
		MetadataTTL: benchmarkRetryMin,
	}
	defer transport.CloseIdleConnections()
	client := &segmentkafka.Client{
		Addr:      segmentkafka.TCP(brokers...),
		Transport: transport,
	}
	brokerStart, err := kafkaGoReplayOffset(
		ctx,
		client,
		topic,
		segmentkafka.FirstOffsetOf(0),
		true,
	)
	if err != nil {
		return err
	}
	brokerEnd, err := kafkaGoReplayOffset(
		ctx,
		client,
		topic,
		segmentkafka.LastOffsetOf(0),
		false,
	)
	if err != nil {
		return err
	}
	if err := validateBenchmarkReplayBounds(
		start,
		end,
		brokerStart,
		brokerEnd,
	); err != nil {
		return err
	}
	reader := segmentkafka.NewReader(segmentkafka.ReaderConfig{
		Brokers:   brokers,
		Topic:     topic,
		Partition: 0,
		Dialer: &segmentkafka.Dialer{
			ClientID: "kafka-go-replay-reader-benchmark",
			Timeout:  benchmarkRequestTimeout,
		},
		QueueCapacity:    max(1, int(end-start)),
		MaxBytes:         benchmarkBatchBytes,
		MaxWait:          500 * time.Millisecond,
		ReadBatchTimeout: benchmarkRequestTimeout,
		ReadLagInterval:  -1,
	})
	defer func() {
		resultErr = errors.Join(resultErr, reader.Close())
	}()
	if err := reader.SetOffset(start); err != nil {
		return fmt.Errorf("set kafka-go replay offset: %w", err)
	}
	for next := start; next < end; next++ {
		message, err := reader.FetchMessage(ctx)
		if err != nil {
			return fmt.Errorf("fetch kafka-go replay record: %w", err)
		}
		if message.Topic != topic ||
			message.Partition != 0 ||
			message.Offset != next {
			return fmt.Errorf(
				"kafka-go replay record = %s[%d]@%d, want %s[0]@%d",
				message.Topic,
				message.Partition,
				message.Offset,
				topic,
				next,
			)
		}
		if err := handler(benchmarkConsumedRecord{
			topic:     message.Topic,
			partition: int32(message.Partition),
			offset:    message.Offset,
			key:       message.Key,
			value:     message.Value,
		}); err != nil {
			return err
		}
	}

	return nil
}

func kafkaGoReplayOffset(
	ctx context.Context,
	client *segmentkafka.Client,
	topic string,
	request segmentkafka.OffsetRequest,
	first bool,
) (int64, error) {
	response, err := client.ListOffsets(ctx, &segmentkafka.ListOffsetsRequest{
		Topics: map[string][]segmentkafka.OffsetRequest{
			topic: {request},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("list kafka-go replay offset: %w", err)
	}
	offsets := response.Topics[topic]
	if len(offsets) != 1 || offsets[0].Partition != 0 {
		return 0, errors.New("kafka-go replay offset response is incomplete")
	}
	if offsets[0].Error != nil {
		return 0, offsets[0].Error
	}
	if first {
		return offsets[0].FirstOffset, nil
	}

	return offsets[0].LastOffset, nil
}

func runSaramaBenchmarkReplay(
	ctx context.Context,
	brokers []string,
	topic string,
	start int64,
	end int64,
	handler func(benchmarkConsumedRecord) error,
) (resultErr error) {
	config := sarama.NewConfig()
	config.ClientID = "sarama-replay-benchmark"
	config.Version = sarama.V3_5_0_0
	config.Net.DialTimeout = benchmarkRequestTimeout
	config.Net.ReadTimeout = benchmarkRequestTimeout
	config.Net.WriteTimeout = benchmarkRequestTimeout
	config.Metadata.AllowAutoTopicCreation = false
	config.Consumer.Fetch.Max = benchmarkBatchBytes
	config.Consumer.MaxWaitTime = 500 * time.Millisecond
	config.Consumer.Return.Errors = true
	client, err := sarama.NewClient(brokers, config)
	if err != nil {
		return fmt.Errorf("construct Sarama replay client: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, client.Close())
	}()
	brokerStart, err := client.GetOffset(topic, 0, sarama.OffsetOldest)
	if err != nil {
		return fmt.Errorf("list Sarama replay start: %w", err)
	}
	brokerEnd, err := client.GetOffset(topic, 0, sarama.OffsetNewest)
	if err != nil {
		return fmt.Errorf("list Sarama replay end: %w", err)
	}
	if err := validateBenchmarkReplayBounds(
		start,
		end,
		brokerStart,
		brokerEnd,
	); err != nil {
		return err
	}
	consumer, err := sarama.NewConsumerFromClient(client)
	if err != nil {
		return fmt.Errorf("construct Sarama replay consumer: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, consumer.Close())
	}()
	partition, err := consumer.ConsumePartition(topic, 0, start)
	if err != nil {
		return fmt.Errorf("construct Sarama partition replay: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, partition.Close())
	}()
	for next := start; next < end; {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case consumerErr, open := <-partition.Errors():
			if !open {
				return errors.New("Sarama replay error channel closed early")
			}
			if consumerErr != nil {
				return consumerErr
			}
		case record, open := <-partition.Messages():
			if !open {
				return errors.New("Sarama replay record channel closed early")
			}
			if record.Topic != topic ||
				record.Partition != 0 ||
				record.Offset != next {
				return fmt.Errorf(
					"Sarama replay record = %s[%d]@%d, want %s[0]@%d",
					record.Topic,
					record.Partition,
					record.Offset,
					topic,
					next,
				)
			}
			if err := handler(benchmarkConsumedRecord{
				topic:     record.Topic,
				partition: record.Partition,
				offset:    record.Offset,
				key:       record.Key,
				value:     record.Value,
			}); err != nil {
				return err
			}
			next++
		}
	}

	return nil
}

func validateBenchmarkReplayBounds(
	start int64,
	end int64,
	brokerStart int64,
	brokerEnd int64,
) error {
	if start < brokerStart || end > brokerEnd || end <= start {
		return fmt.Errorf(
			"replay range [%d,%d) is outside broker bounds [%d,%d)",
			start,
			end,
			brokerStart,
			brokerEnd,
		)
	}

	return nil
}
