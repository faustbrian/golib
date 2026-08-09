package kafka

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

func BenchmarkBoundedFetchDecompression(b *testing.B) {
	source := bytes.Repeat([]byte("record-batch"), 8<<10)
	compressor, err := kgo.DefaultCompressor(kgo.ZstdCompression())
	if err != nil {
		b.Fatal(err)
	}
	var destination bytes.Buffer
	compressed, codec := compressor.Compress(&destination, source)
	decompressor, budget := newFetchDecompressionPolicy(1<<20, 8<<20)

	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	for b.Loop() {
		decoded, decodeErr := decompressor.Decompress(compressed, codec)
		if decodeErr != nil || len(decoded) != len(source) {
			b.Fatalf("Decompress() = (%d bytes, %v)", len(decoded), decodeErr)
		}
		budget.PutDecompressBytes(decoded)
	}
}

func BenchmarkMessageValidation(b *testing.B) {
	message := Message{
		Topic: "track.tracking-event.v1",
		Key:   []byte("tracked-item-1"),
		Value: []byte(`{"event_id":"event-1","schema_version":1}`),
		Headers: []Header{
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "schema-version", Value: []byte("1")},
		},
	}
	limits := DefaultMessageLimits()

	b.ReportAllocs()
	for b.Loop() {
		if err := message.validate(limits); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProducerPolicyOverhead removes broker, network, serialization, and
// retry time from the measurement. Every case uses the same synchronous
// no-I/O transport seam so the delta isolates root policy work: validation,
// allowlist enforcement, byte ownership, franz-go record mapping, lifecycle
// fencing, delivery normalization, and optional observation.
func BenchmarkProducerPolicyOverhead(b *testing.B) {
	ctx := context.Background()
	for _, payloadBytes := range []int{128, 1 << 10, 64 << 10} {
		record := benchmarkProducerRecord(payloadBytes)
		b.Run(fmt.Sprintf("single/%dB", payloadBytes), func(b *testing.B) {
			benchmarkProducerSinglePolicy(b, ctx, record)
		})
	}

	for _, batchRecords := range []int{10, 100} {
		for _, payloadBytes := range []int{128, 1 << 10} {
			records := make([]ProducerRecord, batchRecords)
			for index := range records {
				records[index] = benchmarkProducerRecord(payloadBytes)
			}
			b.Run(fmt.Sprintf(
				"batch/%d-records/%dB",
				batchRecords,
				payloadBytes,
			), func(b *testing.B) {
				benchmarkProducerBatchPolicy(b, ctx, records)
			})
		}
	}

	for _, windowRecords := range []int{10, 100} {
		for _, payloadBytes := range []int{128, 1 << 10} {
			record := benchmarkProducerRecord(payloadBytes)
			b.Run(fmt.Sprintf(
				"async/%d-records/%dB",
				windowRecords,
				payloadBytes,
			), func(b *testing.B) {
				benchmarkProducerAsyncPolicy(
					b,
					ctx,
					record,
					windowRecords,
				)
			})
		}
	}

	for _, transactionRecords := range []int{1, 10} {
		records := make([]ProducerRecord, transactionRecords)
		for index := range records {
			records[index] = benchmarkProducerRecord(1 << 10)
		}
		b.Run(fmt.Sprintf(
			"transaction/%d-records/1024B",
			transactionRecords,
		), func(b *testing.B) {
			benchmarkProducerTransactionPolicy(b, ctx, records)
		})
	}
}

func benchmarkProducerSinglePolicy(
	b *testing.B,
	ctx context.Context,
	record ProducerRecord,
) {
	b.Helper()
	backend := &benchmarkProducerBackend{}
	rawRecord := franzRecord(record.owned())
	b.Run("transport-floor", func(b *testing.B) {
		b.SetBytes(recordSize(record))
		b.ReportAllocs()
		for b.Loop() {
			results := backend.ProduceSync(ctx, rawRecord)
			if len(results) != 1 || results[0].Record != rawRecord ||
				results[0].Err != nil {
				b.Fatal("no-I/O transport returned an invalid delivery")
			}
		}
	})
	for _, observed := range []bool{false, true} {
		name := "policy"
		if observed {
			name = "policy-observed"
		}
		b.Run(name, func(b *testing.B) {
			producer := benchmarkPolicyProducer(backend, observed)
			result := producer.PublishRecord(ctx, record)
			if result.Err != nil || result.Topic != record.Topic ||
				result.Partition != 0 || result.Offset != 1 {
				b.Fatalf("PublishRecord() warm-up result = %#v", result)
			}
			b.SetBytes(recordSize(record))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result = producer.PublishRecord(ctx, record)
				if result.Err != nil || result.Partition != 0 ||
					result.Offset != 1 {
					b.Fatalf("PublishRecord() result = %#v", result)
				}
			}
		})
	}
}

func benchmarkProducerBatchPolicy(
	b *testing.B,
	ctx context.Context,
	records []ProducerRecord,
) {
	b.Helper()
	backend := &benchmarkProducerBackend{}
	rawRecords := make([]*kgo.Record, len(records))
	var bytes int64
	for index, record := range records {
		rawRecords[index] = franzRecord(record.owned())
		bytes += recordSize(record)
	}
	b.Run("transport-floor", func(b *testing.B) {
		b.SetBytes(bytes)
		b.ReportAllocs()
		for b.Loop() {
			results := backend.ProduceSync(ctx, rawRecords...)
			if len(results) != len(rawRecords) || results[0].Err != nil {
				b.Fatal("no-I/O transport returned an invalid batch")
			}
		}
	})
	for _, observed := range []bool{false, true} {
		name := "policy"
		if observed {
			name = "policy-observed"
		}
		b.Run(name, func(b *testing.B) {
			producer := benchmarkPolicyProducer(backend, observed)
			results, err := producer.PublishBatch(ctx, records)
			if err != nil || len(results) != len(records) {
				b.Fatalf("PublishBatch() warm-up = %d results, %v", len(results), err)
			}
			b.SetBytes(bytes)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				results, err = producer.PublishBatch(ctx, records)
				if err != nil || len(results) != len(records) ||
					results[0].Partition != 0 || results[0].Offset != 1 {
					b.Fatalf(
						"PublishBatch() = %d results, %v",
						len(results),
						err,
					)
				}
			}
		})
	}
}

func benchmarkProducerAsyncPolicy(
	b *testing.B,
	ctx context.Context,
	record ProducerRecord,
	windowRecords int,
) {
	b.Helper()
	backend := &benchmarkProducerBackend{}
	rawRecord := franzRecord(record.owned())
	var rawDelivery *kgo.Record
	rawPromise := func(delivered *kgo.Record, err error) {
		if err == nil {
			rawDelivery = delivered
		}
	}
	b.Run("transport-floor", func(b *testing.B) {
		b.SetBytes(recordSize(record) * int64(windowRecords))
		b.ReportAllocs()
		for b.Loop() {
			for range windowRecords {
				backend.Produce(ctx, rawRecord, rawPromise)
			}
			if rawDelivery != rawRecord {
				b.Fatal("no-I/O transport omitted an asynchronous delivery")
			}
		}
	})
	for _, observed := range []bool{false, true} {
		name := "policy"
		if observed {
			name = "policy-observed"
		}
		b.Run(name, func(b *testing.B) {
			producer := benchmarkPolicyProducer(backend, observed)
			deliveries := make([]<-chan DeliveryResult, windowRecords)
			b.SetBytes(recordSize(record) * int64(windowRecords))
			b.ReportAllocs()
			for b.Loop() {
				for index := range deliveries {
					var err error
					deliveries[index], err = producer.PublishAsync(ctx, record)
					if err != nil {
						b.Fatalf("PublishAsync() error = %v", err)
					}
				}
				for _, delivery := range deliveries {
					result := <-delivery
					if result.Err != nil || result.Partition != 0 ||
						result.Offset != 1 {
						b.Fatalf("PublishAsync() result = %#v", result)
					}
				}
			}
		})
	}
}

func benchmarkProducerTransactionPolicy(
	b *testing.B,
	ctx context.Context,
	records []ProducerRecord,
) {
	b.Helper()
	backend := &benchmarkProducerBackend{}
	rawRecords := make([]*kgo.Record, len(records))
	var bytes int64
	for index, record := range records {
		rawRecords[index] = franzRecord(record.owned())
		bytes += recordSize(record)
	}
	b.Run("transport-floor", func(b *testing.B) {
		b.SetBytes(bytes)
		b.ReportAllocs()
		for b.Loop() {
			if err := backend.BeginTransaction(); err != nil {
				b.Fatal(err)
			}
			for _, record := range rawRecords {
				results := backend.ProduceSync(ctx, record)
				if len(results) != 1 || results[0].Err != nil {
					b.Fatal("no-I/O transport returned an invalid transaction delivery")
				}
			}
			if err := backend.EndTransaction(ctx, kgo.TryCommit); err != nil {
				b.Fatal(err)
			}
		}
	})
	for _, observed := range []bool{false, true} {
		name := "policy"
		if observed {
			name = "policy-observed"
		}
		b.Run(name, func(b *testing.B) {
			producer := benchmarkPolicyProducer(backend, observed)
			producer.transactionsEnabled = true
			producer.transactionEndTimeout = time.Minute
			callback := func(transaction Transaction) error {
				for _, record := range records {
					if err := transaction.Publish(ctx, record); err != nil {
						return err
					}
				}

				return nil
			}
			if err := producer.RunTransaction(ctx, callback); err != nil {
				b.Fatalf("RunTransaction() warm-up error = %v", err)
			}
			b.SetBytes(bytes)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := producer.RunTransaction(ctx, callback); err != nil {
					b.Fatalf("RunTransaction() error = %v", err)
				}
			}
		})
	}
}

func benchmarkProducerRecord(payloadBytes int) ProducerRecord {
	return ProducerRecord{
		Topic: "benchmark.events.v1",
		Key:   []byte("aggregate-1"),
		Value: bytes.Repeat([]byte{'v'}, payloadBytes),
		Headers: []Header{
			{Key: "content-type", Value: []byte("application/octet-stream")},
			{Key: "schema-version", Value: []byte("1")},
		},
		Timestamp: time.Unix(1_700_000_000, 0),
	}
}

func benchmarkPolicyProducer(
	backend producerBackend,
	observed bool,
) *Producer {
	producer := &Producer{
		client:              backend,
		clientID:            "benchmark-producer",
		limits:              DefaultMessageLimits(),
		keyRequired:         true,
		maxBatchRecords:     100,
		maxBatchBytes:       1 << 20,
		deliveryWaitTimeout: time.Minute,
		allowedTopics: map[string]struct{}{
			"benchmark.events.v1": {},
		},
	}
	if observed {
		producer.observers = newObserverDispatcher(ObserverPolicy{
			Observers: []ObserverFunc{
				func(context.Context, Observation) error { return nil },
			},
			FailureHandler: func(context.Context, ObservationFailure) {},
			Timeout:        time.Second,
		})
	}

	return producer
}

type benchmarkProducerBackend struct{}

func (*benchmarkProducerBackend) ProduceSync(
	_ context.Context,
	records ...*kgo.Record,
) kgo.ProduceResults {
	results := make(kgo.ProduceResults, len(records))
	for index, record := range records {
		record.Partition = 0
		record.Offset = 1
		results[index] = kgo.ProduceResult{Record: record}
	}

	return results
}

func (*benchmarkProducerBackend) Produce(
	_ context.Context,
	record *kgo.Record,
	promise func(*kgo.Record, error),
) {
	record.Partition = 0
	record.Offset = 1
	promise(record, nil)
}

func (*benchmarkProducerBackend) BufferedProduceRecords() int64 { return 0 }
func (*benchmarkProducerBackend) BufferedProduceBytes() int64   { return 0 }
func (*benchmarkProducerBackend) Flush(context.Context) error   { return nil }
func (*benchmarkProducerBackend) Ping(context.Context) error    { return nil }
func (*benchmarkProducerBackend) BeginTransaction() error       { return nil }
func (*benchmarkProducerBackend) AbortBufferedRecords(context.Context) error {
	return nil
}
func (*benchmarkProducerBackend) EndTransaction(
	context.Context,
	kgo.TransactionEndTry,
) error {
	return nil
}
func (*benchmarkProducerBackend) Close() {}

func BenchmarkFailureHandlerSuccess(b *testing.B) {
	ctx := context.Background()
	record := ConsumedMessage{
		Topic: "track.tracking-event.v1",
		Key:   []byte("tracked-item-1"),
		Value: []byte(`{"event_id":"event-1","schema_version":1}`),
		Headers: []Header{
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "schema-version", Value: []byte("1")},
		},
	}
	direct := HandlerFunc(func(context.Context, ConsumedMessage) error {
		return nil
	})
	decorated, err := NewFailureHandler(FailureHandlerConfig{Handler: direct})
	if err != nil {
		b.Fatal(err)
	}

	b.Run("direct-handler", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := direct.Handle(ctx, record); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("failure-policy", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := decorated.Handle(ctx, record); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkBatchFailureHandlerSuccess(b *testing.B) {
	ctx := context.Background()
	batch := ConsumedBatch{
		Topic: "track.tracking-event.v1", Partition: 0,
		Records: make([]ConsumedRecord, 10),
	}
	for index := range batch.Records {
		batch.Records[index] = ConsumedRecord{
			Topic: batch.Topic, Partition: batch.Partition, Offset: int64(index),
			Key:   []byte("tracked-item-1"),
			Value: []byte(`{"event_id":"event-1","schema_version":1}`),
			Headers: []Header{
				{Key: "content-type", Value: []byte("application/json")},
				{Key: "schema-version", Value: []byte("1")},
			},
		}
	}
	direct := BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
		return nil
	})
	decorated, err := NewBatchFailureHandler(BatchFailureHandlerConfig{
		Handler: direct,
	})
	if err != nil {
		b.Fatal(err)
	}

	b.Run("direct-handler", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := direct.HandleBatch(ctx, batch); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("failure-policy", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := decorated.HandleBatch(ctx, batch); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkConsumerPartitionWorkers(b *testing.B) {
	batches := make([]consumerPartitionBatch, 8)
	process := func(consumerPartitionBatch) consumerPartitionResult {
		return consumerPartitionResult{processed: 1, successful: 1}
	}

	b.Run("sequential", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if results := runConsumerPartitionWorkers(
				batches,
				1,
				process,
			); len(results) != len(batches) {
				b.Fatal("worker result count changed")
			}
		}
	})
	b.Run("four-workers", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if results := runConsumerPartitionWorkers(
				batches,
				4,
				process,
			); len(results) != len(batches) {
				b.Fatal("worker result count changed")
			}
		}
	})
}

func BenchmarkReplayProgress(b *testing.B) {
	ctx := context.Background()
	handler := ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
		return nil
	})
	records := make([]*kgo.Record, 4)
	ranges := make([]ReplayRange, 4)
	for partition := range 4 {
		records[partition] = &kgo.Record{
			Topic:     "track.tracking-event.v1",
			Partition: int32(partition),
			Offset:    100,
			Key:       []byte("tracked-item-1"),
			Value:     []byte(`{"event_id":"event-1","schema_version":1}`),
		}
		ranges[partition] = ReplayRange{
			Topic:       records[partition].Topic,
			Partition:   records[partition].Partition,
			StartOffset: records[partition].Offset,
			EndOffset:   records[partition].Offset + 1,
		}
	}

	for _, benchmark := range []struct {
		name     string
		handlers int
	}{
		{name: "serial", handlers: 1},
		{name: "four-partition-workers", handlers: 4},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				reader := replayReaderWithSafety(
					&recordingReplayBackend{fetches: []kgo.Fetches{
						recordFetches(records...),
					}},
					ranges,
					ReplayCheckpoint{},
				)
				reader.maxConcurrentHandlers = benchmark.handlers
				result, err := reader.Replay(ctx, handler)
				if err != nil ||
					result.Processed != int64(len(records)) ||
					result.IncompleteRanges != 0 {
					b.Fatalf(
						"Replay() result/error = %#v/%v",
						result,
						err,
					)
				}
			}
		})
	}
}

func BenchmarkInspectorTopicState(b *testing.B) {
	backend := &metadataInspectorBackend{
		metadata: kadm.Metadata{Topics: kadm.TopicDetails{
			"events": {
				Topic: "events",
				Partitions: kadm.PartitionDetails{0: {
					Topic: "events", Partition: 0,
					Leader: 1, LeaderEpoch: 4,
					Replicas: []int32{1, 2, 3},
					ISR:      []int32{1, 2, 3},
				}},
			},
		}},
		startOffsets: kadm.ListedOffsets{
			"events": {0: {Topic: "events", Partition: 0, Offset: 10}},
		},
		endOffsets: kadm.ListedOffsets{
			"events": {0: {Topic: "events", Partition: 0, Offset: 25}},
		},
		configs: kadm.ResourceConfigs{
			validTopicInspectionResource("events", "2"),
		},
	}
	inspector := inspectorWithMetadataBackend(backend)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		topics, err := inspector.Topics(ctx, "events")
		if err != nil ||
			len(topics) != 1 ||
			len(topics[0].Partitions) != 1 ||
			topics[0].Partitions[0].EndOffset != 25 {
			b.Fatalf("Topics() result/error = %#v/%v", topics, err)
		}
	}
}

func BenchmarkInspectorConsumerGroupState(b *testing.B) {
	backend := &metadataInspectorBackend{
		recordingInspectorBackend: recordingInspectorBackend{
			groupLags: inspectorGroupLags{
				"orders-v1": {
					group:         "orders-v1",
					coordinatorID: 1,
					state:         "Stable",
					protocolType:  "consumer",
					protocol:      "cooperative-sticky",
					members: []inspectorGroupMember{{
						memberID:          "member-1",
						clientID:          "orders-worker",
						clientHost:        "/host",
						assignmentDecoded: true,
						assignments: map[string][]int32{
							"orders": {0, 1, 2, 3},
						},
					}},
				},
			},
		},
	}
	inspector := inspectorWithMetadataBackend(backend)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		groups, err := inspector.ConsumerGroupLag(ctx, "orders-v1")
		if err != nil ||
			len(groups) != 1 ||
			len(groups[0].Members) != 1 ||
			len(groups[0].Members[0].Assignments) != 4 {
			b.Fatalf("ConsumerGroupLag() result/error = %#v/%v", groups, err)
		}
	}
}

func BenchmarkInspectorReadiness(b *testing.B) {
	backend := &metadataInspectorBackend{}
	inspector := inspectorWithMetadataBackend(backend)
	inspector.readinessPolicy = ReadinessPolicy{
		FailureThreshold:  3,
		RecoveryThreshold: 2,
	}
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		state, err := inspector.Readiness(ctx)
		if err != nil || !state.DependencyHealthy {
			b.Fatalf("Readiness() result/error = %#v/%v", state, err)
		}
	}
}
