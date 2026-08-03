package kafka

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kversion"
)

func TestTransactionProcessorConfigNormalizesAndOwnsPolicy(t *testing.T) {

	brokers := []string{"broker-a.internal:9092", "broker-b.internal:9092"}
	sourceTopics := []string{"source-events"}
	outputTopics := []string{"derived-events"}
	compression := []CompressionCodec{CompressionZstd, CompressionNone}
	config := validTransactionProcessorConfig()
	config.Connection.Brokers = brokers
	config.Group.Topics = sourceTopics
	config.Output.AllowedTopics = outputTopics
	config.Output.CompressionPreferences = compression

	normalized, err := normalizeTransactionProcessorConfig(config)
	if err != nil {
		t.Fatalf("normalizeTransactionProcessorConfig() error = %v", err)
	}
	brokers[0] = "mutated:9092"
	sourceTopics[0] = "mutated-source"
	outputTopics[0] = "mutated-output"
	compression[0] = CompressionNone

	if !reflect.DeepEqual(
		normalized.Connection.Brokers,
		[]string{"broker-a.internal:9092", "broker-b.internal:9092"},
	) ||
		!reflect.DeepEqual(normalized.Group.Topics, []string{"source-events"}) ||
		!reflect.DeepEqual(
			normalized.Output.AllowedTopics,
			[]string{"derived-events"},
		) ||
		!reflect.DeepEqual(
			normalized.Output.CompressionPreferences,
			[]CompressionCodec{CompressionZstd, CompressionNone},
		) {
		t.Fatalf("normalized config aliases caller data: %#v", normalized)
	}
	if normalized.Group.MaxPollRecords != 100 ||
		normalized.Group.FetchMinBytes != 1 ||
		normalized.Group.ProcessingTimeout != 30*time.Second ||
		normalized.Group.BrokerMaxReadBytes != 64<<20 ||
		normalized.Group.MaxDecompressedBatchBytes != 8<<20 ||
		normalized.Group.MaxBufferedDecompressedBytes != 64<<20 ||
		normalized.Connection.Protocol.MinimumVersion != "2.5" ||
		normalized.Output.MaxOutputRecords != 1_000 ||
		normalized.Output.MaxOutputBytes != 10<<20 ||
		normalized.Output.RetryBackoffMin != 250*time.Millisecond ||
		normalized.Output.RetryBackoffMax != time.Second ||
		normalized.Output.TransactionTimeout != 60*time.Second ||
		normalized.Output.TransactionEndTimeout != 10*time.Second ||
		normalized.ShutdownTimeout != 30*time.Second {
		t.Fatalf("normalized defaults = %#v", normalized)
	}
}

func TestTransactionProcessorConfigValidateAndConstruction(t *testing.T) {

	config := validTransactionProcessorConfig()
	config.Group.ResetOffset = OffsetLatest
	config.Group.InstanceID = "transaction-worker-instance"
	config.Group.Rack = "rack-a"
	config.Group.FetchMinBytes = 2 << 20
	config.Group.BrokerMaxReadBytes = 70 << 20
	config.Group.MaxDecompressedBatchBytes = 9 << 20
	config.Group.MaxBufferedDecompressedBytes = 10 << 20
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	processor, err := newTransactionProcessor(
		config,
		func(options ...kgo.Opt) (transactionProcessorBackend, error) {
			client, err := kgo.NewClient(options...)
			if err != nil {
				t.Fatalf("apply transaction processor options: %v", err)
			}
			defer client.Close()
			minimum, ok := client.OptValue(kgo.MinVersions).(*kversion.Versions)
			if client.OptValue(kgo.FetchIsolationLevel) != int8(1) ||
				client.OptValue(kgo.DisableAutoCommit) != true ||
				client.OptValue(kgo.TransactionalID) != "transaction-worker-0" ||
				client.OptValue(kgo.AlwaysRetryEOF) != true ||
				client.OptValue(kgo.StopProducerOnDataLossDetected) != true ||
				client.OptValue(kgo.AllowIdempotentProduceCancellation) != false ||
				client.OptValue(kgo.RecordDeliveryTimeout) != time.Duration(0) ||
				client.OptValue(kgo.FetchMinBytes) != int32(2<<20) ||
				client.OptValue(kgo.BrokerMaxReadBytes) != int32(70<<20) ||
				client.OptValue(kgo.MetadataMinAge) != 250*time.Millisecond ||
				!ok || !minimum.Equal(kversion.FromString("2.5")) {
				t.Fatalf("unsafe transaction processor options")
			}
			startOffset, ok := client.OptValue(kgo.ConsumeStartOffset).(kgo.Offset)
			if !ok || startOffset.EpochOffset().Offset != -1 {
				t.Fatalf("ConsumeStartOffset option = %#v, want end offset", startOffset)
			}
			if got := client.OptValue(kgo.Rack); got != "rack-a" {
				t.Fatalf("Rack option = %#v", got)
			}
			decompressor, ok := client.OptValue(kgo.WithDecompressor).(*boundedDecompressor)
			if !ok || decompressor.maximumBytes != 9<<20 {
				t.Fatalf("WithDecompressor option = %#v", decompressor)
			}
			if budget := fetchBudgetFromClient(t, client); budget.maximumBytes != 10<<20 {
				t.Fatalf("decompression budget = %#v", budget)
			}
			retryBackoff, ok := client.OptValue(kgo.RetryBackoffFn).(func(int) time.Duration)
			if !ok || retryBackoff(10) < 800*time.Millisecond ||
				retryBackoff(10) > time.Second {
				t.Fatalf("unsafe transaction processor retry backoff")
			}

			return &recordingTransactionProcessorBackend{}, nil
		},
	)
	if err != nil {
		t.Fatalf("newTransactionProcessor() error = %v", err)
	}
	if !processor.staticMembership {
		t.Fatal("transaction processor did not retain static membership")
	}
	if processor.deliveryWaitTimeout != 31*time.Second {
		t.Fatalf(
			"delivery wait timeout = %s, want %s",
			processor.deliveryWaitTimeout,
			31*time.Second,
		)
	}

	factoryErr := errors.New("client construction failed")
	processor, err = newTransactionProcessor(
		validTransactionProcessorConfig(),
		func(...kgo.Opt) (transactionProcessorBackend, error) {
			return nil, factoryErr
		},
	)
	if processor != nil || !errors.Is(err, factoryErr) {
		t.Fatalf("factory failure = (%#v, %v)", processor, err)
	}
}

func TestTransactionProcessorPublicConstructor(t *testing.T) {

	config := validTransactionProcessorConfig()
	config.Group.InstanceID = "transaction-worker-instance"
	processor, err := NewTransactionProcessor(config)
	if err != nil {
		t.Fatalf("NewTransactionProcessor() error = %v", err)
	}
	if err := processor.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	config = validTransactionProcessorConfig()
	config.Output.RetryBackoffMin = time.Millisecond
	config.Output.RetryBackoffMax = time.Millisecond
	processor, err = NewTransactionProcessor(config)
	if err != nil {
		t.Fatalf("minimum retry backoff constructor error = %v", err)
	}
	if err := processor.Close(); err != nil {
		t.Fatalf("minimum retry backoff Close() error = %v", err)
	}

	if backend, err := newFranzTransactionProcessorBackend(
		kgo.SeedBrokers("127.0.0.1:1"),
		kgo.TransactionalID("transaction-worker-0"),
	); backend != nil || err == nil {
		t.Fatalf("invalid franz backend = (%#v, %v)", backend, err)
	}
}

func TestTransactionProcessorConfigRejectsUnsafePolicyBeforeConstruction(
	t *testing.T,
) {

	for name, mutate := range map[string]func(*TransactionProcessorConfig){
		"transactional ID required": func(config *TransactionProcessorConfig) {
			config.Output.TransactionalID = ""
		},
		"source and output overlap": func(config *TransactionProcessorConfig) {
			config.Output.AllowedTopics = []string{config.Group.Topics[0]}
		},
		"handler exceeds transaction": func(config *TransactionProcessorConfig) {
			config.Group.ProcessingTimeout = 51 * time.Second
			config.Output.TransactionTimeout = 60 * time.Second
		},
		"end exceeds rebalance": func(config *TransactionProcessorConfig) {
			config.Group.RebalanceTimeout = 30 * time.Second
			config.Output.TransactionEndTimeout = 30 * time.Second
		},
		"work and end exceed transaction": func(config *TransactionProcessorConfig) {
			config.Group.ProcessingTimeout = 31 * time.Second
			config.Output.TransactionTimeout = 41 * time.Second
		},
		"unbounded outputs": func(config *TransactionProcessorConfig) {
			config.Output.MaxOutputRecords = -1
		},
		"pre KIP-447 protocol": func(config *TransactionProcessorConfig) {
			config.Connection.Protocol.MinimumVersion = "2.4"
		},
		"broker read below fetch": func(config *TransactionProcessorConfig) {
			config.Group.FetchMaxBytes = 2 << 20
			config.Group.BrokerMaxReadBytes = 1 << 20
		},
		"decoded batch below record policy": func(config *TransactionProcessorConfig) {
			config.Group.MaxDecompressedBatchBytes = maximumRecordPolicyBytes(DefaultMessageLimits()) - 1
		},
		"decoded buffer below batch": func(config *TransactionProcessorConfig) {
			config.Group.MaxDecompressedBatchBytes = 9 << 20
			config.Group.MaxBufferedDecompressedBytes = 8 << 20
		},
	} {
		mutate := mutate
		t.Run(name, func(t *testing.T) {

			config := validTransactionProcessorConfig()
			mutate(&config)
			called := false
			processor, err := newTransactionProcessor(
				config,
				func(...kgo.Opt) (transactionProcessorBackend, error) {
					called = true

					return &recordingTransactionProcessorBackend{}, nil
				},
			)
			if !errors.Is(err, ErrInvalidTransactionProcessorConfig) ||
				processor != nil || called {
				t.Fatalf(
					"newTransactionProcessor() = (%#v, %v), factory called = %t",
					processor,
					err,
					called,
				)
			}
		})
	}
}

func TestTransactionProcessorOutputLimitsUseInclusiveBoundaries(t *testing.T) {

	minimumBytes := maximumRecordPolicyBytes(DefaultMessageLimits())
	validLimits := []struct {
		name    string
		records int
		bytes   int64
	}{
		{name: "minimum records", records: 1, bytes: minimumBytes},
		{name: "maximum records", records: 100_000, bytes: minimumBytes},
		{name: "maximum bytes", records: 1, bytes: 1 << 30},
	}
	for _, limit := range validLimits {
		limit := limit
		t.Run(limit.name, func(t *testing.T) {

			config := validTransactionProcessorConfig()
			config.Output.MaxOutputRecords = limit.records
			config.Output.MaxOutputBytes = limit.bytes
			normalized, err := normalizeTransactionProcessorConfig(config)
			if err != nil {
				t.Fatalf("normalizeTransactionProcessorConfig() error = %v", err)
			}
			if normalized.Output.MaxOutputRecords != limit.records ||
				normalized.Output.MaxOutputBytes != limit.bytes {
				t.Fatalf("normalized output limits = %#v", normalized.Output)
			}
		})
	}

	invalidLimits := []struct {
		name    string
		records int
		bytes   int64
	}{
		{name: "above maximum records", records: 100_001, bytes: minimumBytes},
		{name: "below minimum bytes", records: 1, bytes: minimumBytes - 1},
		{name: "above maximum bytes", records: 1, bytes: 1<<30 + 1},
	}
	for _, limit := range invalidLimits {
		limit := limit
		t.Run(limit.name, func(t *testing.T) {

			config := validTransactionProcessorConfig()
			config.Output.MaxOutputRecords = limit.records
			config.Output.MaxOutputBytes = limit.bytes
			if _, err := normalizeTransactionProcessorConfig(config); !errors.Is(
				err,
				ErrInvalidTransactionProcessorConfig,
			) {
				t.Fatalf("normalizeTransactionProcessorConfig() error = %v", err)
			}
		})
	}
}

func TestTransactionProcessorConfigPreservesValidationCause(t *testing.T) {

	config := validTransactionProcessorConfig()
	config.Connection.Protocol.MinimumVersion = "unknown"
	err := config.Validate()
	if !errors.Is(err, ErrInvalidTransactionProcessorConfig) ||
		!errors.Is(err, ErrInvalidProtocolPolicy) {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestTransactionProcessorCommitsCompletePollAtomically(t *testing.T) {

	first := transactionSourceRecord(0, "first")
	second := transactionSourceRecord(1, "second")
	backend := &recordingTransactionProcessorBackend{
		fetches: []kgo.Fetches{transactionFetches(first, second)},
		endResults: []transactionEndResult{{
			committed: true,
		}},
	}
	processor := transactionProcessorForTest(t, backend)
	var retained Transaction
	result, err := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(
			ctx context.Context,
			record ConsumedRecord,
			transaction Transaction,
		) error {
			retained = transaction

			return transaction.Publish(ctx, ProducerRecord{
				Topic: "derived-events",
				Key:   record.Key,
				Value: append([]byte("derived-"), record.Value...),
			})
		}),
	)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result != (TransactionPollResult{
		Polled: 2, Processed: 2, Published: 2, Committed: true,
	}) {
		t.Fatalf("RunOnce() result = %#v", result)
	}
	if !reflect.DeepEqual(
		backend.endTries,
		[]kgo.TransactionEndTry{kgo.TryCommit},
	) || backend.beginCalls != 1 || len(backend.produced) != 2 {
		t.Fatalf("backend state = %#v", backend)
	}
	if err := retained.Publish(context.Background(), ProducerRecord{
		Topic: "derived-events", Key: []byte("late"), Value: []byte("late"),
	}); !errors.Is(err, ErrTransactionClosed) {
		t.Fatalf("retained transaction error = %v", err)
	}
}

func TestTransactionProcessorAbortsWholePollOnHandlerFailure(t *testing.T) {

	backend := &recordingTransactionProcessorBackend{
		fetches: []kgo.Fetches{transactionFetches(
			transactionSourceRecord(0, "first"),
			transactionSourceRecord(1, "second"),
		)},
		endResults: []transactionEndResult{{committed: false}},
	}
	processor := transactionProcessorForTest(t, backend)
	handlerErr := errors.New("transform failed")
	result, err := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(
			ctx context.Context,
			record ConsumedRecord,
			transaction Transaction,
		) error {
			if record.Offset == 1 {
				return handlerErr
			}

			return transaction.Publish(ctx, ProducerRecord{
				Topic: "derived-events", Key: record.Key, Value: record.Value,
			})
		}),
	)
	if !errors.Is(err, handlerErr) {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result != (TransactionPollResult{
		Polled: 2, Processed: 1, Published: 1,
	}) {
		t.Fatalf("RunOnce() result = %#v", result)
	}
	if !reflect.DeepEqual(
		backend.endTries,
		[]kgo.TransactionEndTry{kgo.TryAbort},
	) {
		t.Fatalf("end tries = %#v", backend.endTries)
	}
}

func TestTransactionProcessorSurfacesRebalanceAbort(t *testing.T) {

	backend := &recordingTransactionProcessorBackend{
		fetches: []kgo.Fetches{transactionFetches(
			transactionSourceRecord(0, "first"),
		)},
		endResults: []transactionEndResult{{committed: false}},
	}
	processor := transactionProcessorForTest(t, backend)
	result, err := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			return nil
		}),
	)
	var transactionErr *TransactionError
	if !errors.Is(err, ErrTransactionNotCommitted) ||
		!errors.As(err, &transactionErr) ||
		transactionErr.Category() != ErrorRetryable ||
		!transactionErr.Abortable() ||
		!transactionErr.OutcomeKnown() ||
		result != (TransactionPollResult{Polled: 1, Processed: 1}) {
		t.Fatalf("RunOnce() = (%#v, %v)", result, err)
	}
}

func TestTransactionProcessorAbortsPartialFetchError(t *testing.T) {

	fetchErr := errors.New("partial fetch failure")
	fetches := transactionFetches(transactionSourceRecord(0, "first"))
	fetches = append(fetches, kgo.NewErrFetch(fetchErr)...)
	backend := &recordingTransactionProcessorBackend{
		fetches:    []kgo.Fetches{fetches},
		endResults: []transactionEndResult{{committed: false}},
	}
	processor := transactionProcessorForTest(t, backend)
	result, err := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			t.Fatal("handler called for a partial failed fetch")

			return nil
		}),
	)
	if !errors.Is(err, fetchErr) ||
		result != (TransactionPollResult{Polled: 1}) ||
		!reflect.DeepEqual(
			backend.endTries,
			[]kgo.TransactionEndTry{kgo.TryAbort},
		) {
		t.Fatalf("RunOnce() = (%#v, %v), ends = %#v", result, err, backend.endTries)
	}
}

func TestTransactionProcessorFencesPartialFetchCleanupFailure(t *testing.T) {

	fetchErr := errors.New("partial fetch failure")
	for name, backend := range map[string]*recordingTransactionProcessorBackend{
		"begin": {
			beginErr: errors.New("begin failure"),
		},
		"abort": {
			endResults: []transactionEndResult{{
				err: errors.New("abort failure"),
			}},
		},
	} {
		backend := backend
		t.Run(name, func(t *testing.T) {

			fetches := transactionFetches(transactionSourceRecord(0, "first"))
			backend.fetches = []kgo.Fetches{
				append(fetches, kgo.NewErrFetch(fetchErr)...),
			}
			processor := transactionProcessorForTest(t, backend)
			_, err := processor.RunOnce(
				context.Background(),
				TransactionHandlerFunc(func(
					context.Context,
					ConsumedRecord,
					Transaction,
				) error {
					return nil
				}),
			)
			if !errors.Is(err, fetchErr) {
				t.Fatalf("RunOnce() error = %v", err)
			}
			_, err = processor.RunOnce(
				context.Background(),
				TransactionHandlerFunc(func(
					context.Context,
					ConsumedRecord,
					Transaction,
				) error {
					return nil
				}),
			)
			if !errors.Is(err, ErrTransactionProcessorFatal) {
				t.Fatalf("second RunOnce() error = %v", err)
			}
		})
	}
}

func TestTransactionProcessorFencesLifecycleFailure(t *testing.T) {

	for name, backend := range map[string]*recordingTransactionProcessorBackend{
		"begin": {
			fetches: []kgo.Fetches{transactionFetches(
				transactionSourceRecord(0, "first"),
			)},
			beginErr: errors.New("begin failed"),
		},
		"commit": {
			fetches: []kgo.Fetches{transactionFetches(
				transactionSourceRecord(0, "first"),
			)},
			endResults: []transactionEndResult{{
				err: errors.New("commit outcome unknown"),
			}},
		},
	} {
		backend := backend
		t.Run(name, func(t *testing.T) {

			processor := transactionProcessorForTest(t, backend)
			_, firstErr := processor.RunOnce(
				context.Background(),
				TransactionHandlerFunc(func(
					context.Context,
					ConsumedRecord,
					Transaction,
				) error {
					return nil
				}),
			)
			if firstErr == nil {
				t.Fatal("RunOnce() unexpectedly succeeded")
			}
			_, secondErr := processor.RunOnce(
				context.Background(),
				TransactionHandlerFunc(func(
					context.Context,
					ConsumedRecord,
					Transaction,
				) error {
					return nil
				}),
			)
			if !errors.Is(secondErr, ErrTransactionProcessorFatal) ||
				backend.beginCalls != 1 {
				t.Fatalf(
					"second RunOnce() error = %v, begin calls = %d",
					secondErr,
					backend.beginCalls,
				)
			}
		})
	}
}

func TestTransactionProcessorRejectsInvalidOutputAndIgnoredDeliveryFailure(
	t *testing.T,
) {

	for name, record := range map[string]ProducerRecord{
		"invalid": {
			Topic: "derived-events",
			Key:   []byte("key"),
			Value: make([]byte, DefaultMessageLimits().MaxValueBytes+1),
		},
		"denied topic": {
			Topic: "other-events",
			Key:   []byte("key"),
		},
		"key required": {
			Topic: "derived-events",
		},
	} {
		record := record
		t.Run(name, func(t *testing.T) {

			backend := &recordingTransactionProcessorBackend{
				fetches: []kgo.Fetches{transactionFetches(
					transactionSourceRecord(0, "first"),
				)},
				endResults: []transactionEndResult{{committed: false}},
			}
			processor := transactionProcessorForTest(t, backend)
			result, err := processor.RunOnce(
				context.Background(),
				TransactionHandlerFunc(func(
					ctx context.Context,
					_ ConsumedRecord,
					transaction Transaction,
				) error {
					_ = transaction.Publish(ctx, record)

					return nil
				}),
			)
			if err == nil || result.Committed ||
				!reflect.DeepEqual(
					backend.endTries,
					[]kgo.TransactionEndTry{kgo.TryAbort},
				) {
				t.Fatalf(
					"RunOnce() = (%#v, %v), ends = %#v",
					result,
					err,
					backend.endTries,
				)
			}
		})
	}

	deliveryErr := errors.New("delivery failed")
	for name, results := range map[string]kgo.ProduceResults{
		"missing":    nil,
		"nil record": {{Record: nil}},
		"delivery error": {{
			Record: transactionSourceRecord(0, "output"),
			Err:    deliveryErr,
		}},
	} {
		results := results
		t.Run(name, func(t *testing.T) {

			backend := &recordingTransactionProcessorBackend{
				fetches: []kgo.Fetches{transactionFetches(
					transactionSourceRecord(0, "first"),
				)},
				produceResults: []kgo.ProduceResults{results},
				endResults:     []transactionEndResult{{committed: false}},
			}
			processor := transactionProcessorForTest(t, backend)
			_, err := processor.RunOnce(
				context.Background(),
				TransactionHandlerFunc(func(
					ctx context.Context,
					record ConsumedRecord,
					transaction Transaction,
				) error {
					_ = transaction.Publish(ctx, ProducerRecord{
						Topic: "derived-events", Key: record.Key, Value: record.Value,
					})

					return nil
				}),
			)
			if err == nil {
				t.Fatal("RunOnce() unexpectedly succeeded")
			}
		})
	}
}

func TestTransactionProcessorAbortsBrokerRecordOutsidePolicy(t *testing.T) {

	record := transactionSourceRecord(0, "first")
	record.Value = make([]byte, DefaultMessageLimits().MaxValueBytes+1)
	backend := &recordingTransactionProcessorBackend{
		fetches:    []kgo.Fetches{transactionFetches(record)},
		endResults: []transactionEndResult{{committed: false}},
	}
	processor := transactionProcessorForTest(t, backend)
	result, err := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			t.Fatal("handler called with oversized source record")

			return nil
		}),
	)
	if !errors.Is(err, ErrValueTooLarge) ||
		result != (TransactionPollResult{Polled: 1}) ||
		!reflect.DeepEqual(
			backend.endTries,
			[]kgo.TransactionEndTry{kgo.TryAbort},
		) {
		t.Fatalf("RunOnce() = (%#v, %v), ends = %#v", result, err, backend.endTries)
	}
}

func TestTransactionProcessorBoundsTransactionOutputAggregate(t *testing.T) {

	backend := &recordingTransactionProcessorBackend{
		fetches: []kgo.Fetches{transactionFetches(
			transactionSourceRecord(0, "first"),
		)},
		endResults: []transactionEndResult{{committed: false}},
	}
	config := validTransactionProcessorConfig()
	config.Output.MaxOutputRecords = 1
	processor, err := newTransactionProcessor(
		config,
		func(...kgo.Opt) (transactionProcessorBackend, error) {
			return backend, nil
		},
	)
	if err != nil {
		t.Fatalf("newTransactionProcessor() error = %v", err)
	}
	result, err := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(
			ctx context.Context,
			record ConsumedRecord,
			transaction Transaction,
		) error {
			for range 2 {
				_ = transaction.Publish(ctx, ProducerRecord{
					Topic: "derived-events", Key: record.Key, Value: record.Value,
				})
			}

			return nil
		}),
	)
	if !errors.Is(err, ErrTooManyTransactionOutputRecords) ||
		result != (TransactionPollResult{Polled: 1, Published: 1}) ||
		!reflect.DeepEqual(
			backend.endTries,
			[]kgo.TransactionEndTry{kgo.TryAbort},
		) {
		t.Fatalf("RunOnce() = (%#v, %v), ends = %#v", result, err, backend.endTries)
	}

	bytesBackend := &recordingTransactionProcessorBackend{}
	publisher := &processorTransactionPublisher{
		client:         bytesBackend,
		limits:         DefaultMessageLimits(),
		allowedTopics:  map[string]struct{}{"derived-events": {}},
		maxOutputCount: 2,
		maxOutputBytes: recordSize(ProducerRecord{
			Topic: "derived-events", Key: []byte("key"), Value: []byte("value"),
		}),
	}
	record := ProducerRecord{
		Topic: "derived-events", Key: []byte("key"), Value: []byte("value"),
	}
	if err := publisher.publish(context.Background(), record); err != nil {
		t.Fatalf("first publish error = %v", err)
	}
	if err := publisher.publish(context.Background(), record); !errors.Is(
		err,
		ErrTransactionOutputTooLarge,
	) {
		t.Fatalf("second publish error = %v", err)
	}
}

func TestTransactionProcessorBoundsProcessingAndContainsPanic(t *testing.T) {

	for name, handler := range map[string]TransactionHandler{
		"panic": TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			panic("secret panic")
		}),
		"timeout": TransactionHandlerFunc(func(
			ctx context.Context,
			_ ConsumedRecord,
			_ Transaction,
		) error {
			<-ctx.Done()

			return nil
		}),
	} {
		handler := handler
		t.Run(name, func(t *testing.T) {

			backend := &recordingTransactionProcessorBackend{
				fetches: []kgo.Fetches{transactionFetches(
					transactionSourceRecord(0, "first"),
				)},
				endResults: []transactionEndResult{{committed: false}},
			}
			processor := transactionProcessorForTest(t, backend)
			if name == "timeout" {
				processor.processingTimeout = time.Millisecond
			}
			_, err := processor.RunOnce(context.Background(), handler)
			if err == nil ||
				!reflect.DeepEqual(
					backend.endTries,
					[]kgo.TransactionEndTry{kgo.TryAbort},
				) {
				t.Fatalf("RunOnce() error = %v, ends = %#v", err, backend.endTries)
			}
		})
	}
}

func TestTransactionProcessorClassifiesEndFailures(t *testing.T) {

	for name, test := range map[string]struct {
		try          kgo.TransactionEndTry
		committed    bool
		endErr       error
		wantCategory ErrorCategory
		wantKnown    bool
	}{
		"commit authorization": {
			try: kgo.TryCommit, endErr: kerr.TransactionalIDAuthorizationFailed,
			wantCategory: ErrorAuthorization, wantKnown: true,
		},
		"commit abortable": {
			try: kgo.TryCommit, endErr: kerr.TransactionAbortable,
			wantCategory: ErrorRetryable, wantKnown: true,
		},
		"commit ambiguous": {
			try: kgo.TryCommit, endErr: context.DeadlineExceeded,
			wantCategory: ErrorAmbiguous,
		},
		"abort ambiguous": {
			try: kgo.TryAbort, endErr: context.DeadlineExceeded,
			wantCategory: ErrorAmbiguous,
		},
		"abort impossible commit": {
			try: kgo.TryAbort, committed: true,
			wantCategory: ErrorAmbiguous,
		},
	} {
		test := test
		t.Run(name, func(t *testing.T) {

			backend := &recordingTransactionProcessorBackend{
				fetches: []kgo.Fetches{transactionFetches(
					transactionSourceRecord(0, "first"),
				)},
				endResults: []transactionEndResult{{
					committed: test.committed,
					err:       test.endErr,
				}},
			}
			processor := transactionProcessorForTest(t, backend)
			handler := TransactionHandlerFunc(func(
				context.Context,
				ConsumedRecord,
				Transaction,
			) error {
				if test.try == kgo.TryAbort {
					return errors.New("force abort")
				}

				return nil
			})
			_, err := processor.RunOnce(context.Background(), handler)
			var transactionErr *TransactionError
			if !errors.As(err, &transactionErr) ||
				transactionErr.Category() != test.wantCategory ||
				transactionErr.OutcomeKnown() != test.wantKnown {
				t.Fatalf("RunOnce() error = %v", err)
			}
		})
	}
}

func TestTransactionProcessorTerminatesAfterAmbiguousPublishDeadline(
	t *testing.T,
) {

	clientCtx, cancelClient := context.WithCancel(context.Background())
	release := make(chan struct{})
	releaseTimer := time.AfterFunc(250*time.Millisecond, func() { close(release) })
	defer releaseTimer.Stop()
	backend := &recordingTransactionProcessorBackend{
		fetches: []kgo.Fetches{transactionFetches(&kgo.Record{
			Topic: "source-events",
			Key:   []byte("source-key"),
			Value: []byte("source-value"),
		})},
		produceSync: func(
			_ context.Context,
			records ...*kgo.Record,
		) kgo.ProduceResults {
			select {
			case <-clientCtx.Done():
				return kgo.ProduceResults{{
					Record: records[0],
					Err:    kgo.ErrClientClosed,
				}}
			case <-release:
				return kgo.ProduceResults{{Record: records[0]}}
			}
		},
	}
	processor := transactionProcessorForTest(t, backend)
	processor.deliveryWaitTimeout = 20 * time.Millisecond
	processor.cancelClient = cancelClient

	result, err := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(
			_ context.Context,
			_ ConsumedRecord,
			transaction Transaction,
		) error {
			return transaction.Publish(context.Background(), ProducerRecord{
				Topic: "derived-events",
				Key:   []byte("derived-key"),
				Value: []byte("derived-value"),
			})
		}),
	)
	var deliveryErr *DeliveryError
	if result.Polled != 1 || result.Processed != 0 ||
		result.Published != 0 || result.Committed ||
		!errors.Is(err, ErrTransactionProcessorFatal) ||
		!errors.Is(err, context.DeadlineExceeded) ||
		!errors.As(err, &deliveryErr) ||
		deliveryErr.Category() != ErrorAmbiguous ||
		len(backend.endTries) != 0 || !backend.closed {
		t.Fatalf("RunOnce() result/error/backend = %#v/%v/%#v", result, err, backend)
	}
	if _, err := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(context.Context, ConsumedRecord, Transaction) error {
			return nil
		}),
	); !errors.Is(err, ErrTransactionProcessorFatal) {
		t.Fatalf("RunOnce() after fatal delivery = %v", err)
	}
	if err := processor.Close(); err != nil {
		t.Fatalf("Close() after fatal delivery = %v", err)
	}
}

func TestTransactionProcessorRetainsTerminalPublishAfterIgnoredFailure(
	t *testing.T,
) {

	clientCtx, cancelClient := context.WithCancel(context.Background())
	backend := &recordingTransactionProcessorBackend{
		fetches: []kgo.Fetches{transactionFetches(transactionSourceRecord(0, "source"))},
		produceSync: func(
			_ context.Context,
			records ...*kgo.Record,
		) kgo.ProduceResults {
			<-clientCtx.Done()

			return kgo.ProduceResults{{
				Record: records[0],
				Err:    kgo.ErrClientClosed,
			}}
		},
	}
	processor := transactionProcessorForTest(t, backend)
	processor.deliveryWaitTimeout = 20 * time.Millisecond
	processor.cancelClient = cancelClient

	result, err := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(
			_ context.Context,
			_ ConsumedRecord,
			transaction Transaction,
		) error {
			_ = transaction.Publish(context.Background(), ProducerRecord{
				Topic: "not-allowed",
				Key:   []byte("key"),
			})
			_ = transaction.Publish(context.Background(), ProducerRecord{
				Topic: "derived-events",
				Key:   []byte("key"),
			})

			return nil
		}),
	)
	var deliveryErr *DeliveryError
	if result != (TransactionPollResult{Polled: 1}) ||
		!errors.Is(err, ErrTopicNotAllowed) ||
		!errors.Is(err, ErrTransactionProcessorFatal) ||
		!errors.Is(err, context.DeadlineExceeded) ||
		!errors.As(err, &deliveryErr) ||
		deliveryErr.Category() != ErrorAmbiguous ||
		len(backend.endTries) != 0 || !backend.closed {
		t.Fatalf("RunOnce() result/error/backend = %#v/%v/%#v", result, err, backend)
	}
}

func TestTransactionProcessorPublisherBoundsRetainedTerminalFailures(t *testing.T) {

	ordinaryErr := errors.New("ordinary failure")
	firstTerminalErr := errors.New("first terminal failure")
	secondTerminalErr := errors.New("second terminal failure")
	publisher := &processorTransactionPublisher{}
	publisher.recordFailure(ordinaryErr)
	publisher.recordTerminalFailure(firstTerminalErr)
	publisher.recordTerminalFailure(secondTerminalErr)

	err := publisher.failure()
	if !errors.Is(err, ordinaryErr) || !errors.Is(err, firstTerminalErr) ||
		errors.Is(err, secondTerminalErr) || !publisher.terminalFailure() {
		t.Fatalf("bounded retained failure = %v", err)
	}
}

func TestTransactionProcessorRejectsCanceledOutputBeforeAdmission(t *testing.T) {

	backend := &recordingTransactionProcessorBackend{}
	publisher := &processorTransactionPublisher{
		client:         backend,
		limits:         DefaultMessageLimits(),
		allowedTopics:  map[string]struct{}{"derived-events": {}},
		maxOutputCount: 1,
		maxOutputBytes: 1 << 20,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := publisher.publish(ctx, ProducerRecord{
		Topic: "derived-events",
		Key:   []byte("key"),
		Value: []byte("value"),
	})
	var deliveryErr *DeliveryError
	if !errors.Is(err, context.Canceled) ||
		!errors.As(err, &deliveryErr) ||
		deliveryErr.Category() != ErrorAmbiguous ||
		publisher.terminalFailure() || len(backend.produced) != 0 {
		t.Fatalf("canceled processor output = %v/%#v", err, backend)
	}
}

func TestTransactionProcessorRunLifecycle(t *testing.T) {

	processor := transactionProcessorForTest(
		t,
		&recordingTransactionProcessorBackend{},
	)
	var nilContext context.Context
	if _, err := processor.RunOnce(nilContext, TransactionHandlerFunc(func(
		context.Context,
		ConsumedRecord,
		Transaction,
	) error {
		return nil
	})); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("RunOnce(nil) error = %v", err)
	}
	if _, err := processor.RunOnce(context.Background(), nil); !errors.Is(
		err,
		ErrTransactionHandlerRequired,
	) {
		t.Fatalf("RunOnce(nil handler) error = %v", err)
	}
	if err := processor.Run(nilContext, TransactionHandlerFunc(func(
		context.Context,
		ConsumedRecord,
		Transaction,
	) error {
		return nil
	})); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Run(nil) error = %v", err)
	}
	if err := processor.Run(context.Background(), nil); !errors.Is(
		err,
		ErrTransactionHandlerRequired,
	) {
		t.Fatalf("Run(nil handler) error = %v", err)
	}
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := processor.Run(
		canceledCtx,
		TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			return nil
		}),
	); err != nil {
		t.Fatalf("Run(canceled) error = %v", err)
	}

	fetchErr := errors.New("fetch failed")
	failed := transactionProcessorForTest(t, &recordingTransactionProcessorBackend{
		fetches: []kgo.Fetches{kgo.NewErrFetch(fetchErr)},
	})
	if err := failed.Run(
		context.Background(),
		TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			return nil
		}),
	); !errors.Is(err, fetchErr) {
		t.Fatalf("Run(fetch failure) error = %v", err)
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	success := transactionProcessorForTest(t, &recordingTransactionProcessorBackend{
		fetches: []kgo.Fetches{transactionFetches(
			transactionSourceRecord(0, "first"),
		)},
		endResults: []transactionEndResult{{committed: false}},
	})
	if err := success.Run(
		runCtx,
		TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			cancelRun()

			return nil
		}),
	); err != nil {
		t.Fatalf("Run(success) error = %v", err)
	}

	fatalCtx, cancelFatal := context.WithCancel(context.Background())
	fatal := transactionProcessorForTest(t, &recordingTransactionProcessorBackend{
		fetches: []kgo.Fetches{transactionFetches(
			transactionSourceRecord(0, "first"),
		)},
		endResults: []transactionEndResult{{
			err: errors.New("abort outcome unknown"),
		}},
	})
	err := fatal.Run(
		fatalCtx,
		TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			cancelFatal()

			return nil
		}),
	)
	if err == nil || !errors.Is(err, ErrTransactionOutcomeUnknown) {
		t.Fatalf("Run(canceled with abort failure) error = %v", err)
	}
}

func TestTransactionProcessorSerializesRunAndBoundsShutdown(t *testing.T) {

	pollStarted := make(chan struct{})
	releasePoll := make(chan struct{})
	backend := &recordingTransactionProcessorBackend{
		pollStarted: pollStarted,
		releasePoll: releasePoll,
	}
	processor := transactionProcessorForTest(t, backend)
	runDone := make(chan error, 1)
	go func() {
		_, err := processor.RunOnce(
			context.Background(),
			TransactionHandlerFunc(func(
				context.Context,
				ConsumedRecord,
				Transaction,
			) error {
				return nil
			}),
		)
		runDone <- err
	}()
	<-pollStarted

	if _, err := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			return nil
		}),
	); !errors.Is(err, ErrTransactionProcessorBusy) {
		t.Fatalf("concurrent RunOnce() error = %v", err)
	}
	shutdownCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := processor.Shutdown(shutdownCtx); !errors.Is(
		err,
		ErrTransactionProcessorShutdownIncomplete,
	) {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if _, err := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			return nil
		}),
	); !errors.Is(err, ErrTransactionProcessorClosing) {
		t.Fatalf("RunOnce() while closing error = %v", err)
	}
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- processor.Shutdown(context.Background())
	}()
	waitTransactionProcessorShutdown(t, processor)
	if err := processor.Shutdown(context.Background()); !errors.Is(
		err,
		ErrTransactionProcessorShutdownActive,
	) {
		t.Fatalf("concurrent Shutdown() error = %v", err)
	}
	close(releasePoll)
	if err := <-runDone; err != nil {
		t.Fatalf("active RunOnce() error = %v", err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatalf("retry Shutdown() error = %v", err)
	}
	if err := processor.Shutdown(context.Background()); err != nil {
		t.Fatalf("idempotent Shutdown() error = %v", err)
	}
	if _, err := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			return nil
		}),
	); !errors.Is(err, ErrTransactionProcessorClosed) {
		t.Fatalf("RunOnce() after shutdown error = %v", err)
	}
	if !backend.closed || backend.leaveCalls != 1 {
		t.Fatalf("backend close state = %#v", backend)
	}
	if err := processor.Run(
		context.Background(),
		TransactionHandlerFunc(func(
			context.Context,
			ConsumedRecord,
			Transaction,
		) error {
			return nil
		}),
	); !errors.Is(err, ErrTransactionProcessorClosed) {
		t.Fatalf("Run() after shutdown error = %v", err)
	}
}

func waitTransactionProcessorShutdown(
	t *testing.T,
	processor *TransactionProcessor,
) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for {
		processor.lifecycleMu.Lock()
		active := processor.shutdownActive
		processor.lifecycleMu.Unlock()
		if active {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("transaction processor shutdown did not start")
		}
		runtime.Gosched()
	}
}

func TestTransactionProcessorShutdownFailuresAndStaticMembership(t *testing.T) {

	processor := transactionProcessorForTest(
		t,
		&recordingTransactionProcessorBackend{},
	)
	var nilContext context.Context
	if err := processor.Shutdown(nilContext); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Shutdown(nil) error = %v", err)
	}

	leaveErr := errors.New("leave failed")
	leaveBackend := &recordingTransactionProcessorBackend{leaveErr: leaveErr}
	leaveProcessor := transactionProcessorForTest(t, leaveBackend)
	if err := leaveProcessor.Shutdown(context.Background()); !errors.Is(
		err,
		ErrTransactionProcessorShutdownIncomplete,
	) || !errors.Is(err, leaveErr) {
		t.Fatalf("Shutdown() leave error = %v", err)
	}

	staticBackend := &recordingTransactionProcessorBackend{}
	staticConfig := validTransactionProcessorConfig()
	staticConfig.Group.InstanceID = "transaction-worker-instance"
	staticProcessor, err := newTransactionProcessor(
		staticConfig,
		func(...kgo.Opt) (transactionProcessorBackend, error) {
			return staticBackend, nil
		},
	)
	if err != nil {
		t.Fatalf("construct static processor: %v", err)
	}
	cancelCalled := false
	staticProcessor.cancelClient = func() {
		cancelCalled = true
	}
	if err := staticProcessor.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if staticBackend.leaveCalls != 0 || !staticBackend.closed || !cancelCalled {
		t.Fatalf("static close state = %#v", staticBackend)
	}
}

func TestFranzTransactionProcessorBackendLifecycle(t *testing.T) {

	backend, err := newFranzTransactionProcessorBackend(
		kgo.SeedBrokers("127.0.0.1:1"),
		kgo.ConsumerGroup("transaction-worker"),
		kgo.ConsumeTopics("source-events"),
		kgo.TransactionalID("transaction-worker-0"),
	)
	if err != nil {
		t.Fatalf("newFranzTransactionProcessorBackend() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if fetches := backend.PollRecords(ctx, 1); fetches.Err() == nil {
		t.Fatal("PollRecords() unexpectedly succeeded")
	}
	if err := backend.Begin(); err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	results := backend.ProduceSync(ctx, &kgo.Record{
		Topic: "derived-events", Key: []byte("key"), Value: []byte("value"),
	})
	if len(results) != 1 || results[0].Err == nil {
		t.Fatalf("ProduceSync() results = %#v", results)
	}
	if _, err := backend.End(ctx, kgo.TryAbort); err == nil {
		t.Fatal("End() unexpectedly succeeded")
	}
	if err := backend.LeaveGroupContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("LeaveGroupContext() error = %v", err)
	}
	backend.Close()
}

func validTransactionProcessorConfig() TransactionProcessorConfig {
	return TransactionProcessorConfig{
		Connection: TransactionConnectionConfig{
			Brokers:  []string{"broker.internal:9092"},
			ClientID: "transaction-worker",
			Security: DevelopmentPlaintextSecurity(),
		},
		Group: TransactionGroupConfig{
			GroupID:     "transaction-worker",
			Topics:      []string{"source-events"},
			ResetOffset: OffsetEarliest,
		},
		Output: TransactionOutputConfig{
			AllowedTopics:   []string{"derived-events"},
			TransactionalID: "transaction-worker-0",
		},
	}
}

func transactionProcessorForTest(
	t *testing.T,
	backend transactionProcessorBackend,
) *TransactionProcessor {
	t.Helper()

	processor, err := newTransactionProcessor(
		validTransactionProcessorConfig(),
		func(...kgo.Opt) (transactionProcessorBackend, error) {
			return backend, nil
		},
	)
	if err != nil {
		t.Fatalf("newTransactionProcessor() error = %v", err)
	}

	return processor
}

type transactionEndResult struct {
	committed bool
	err       error
}

type recordingTransactionProcessorBackend struct {
	mu             sync.Mutex
	fetches        []kgo.Fetches
	endResults     []transactionEndResult
	produceResults []kgo.ProduceResults
	produced       []*kgo.Record
	endTries       []kgo.TransactionEndTry
	pollStarted    chan struct{}
	releasePoll    chan struct{}
	beginErr       error
	leaveErr       error
	beginCalls     int
	leaveCalls     int
	closed         bool
	produceSync    func(context.Context, ...*kgo.Record) kgo.ProduceResults
}

func (backend *recordingTransactionProcessorBackend) PollRecords(
	context.Context,
	int,
) kgo.Fetches {
	if backend.pollStarted != nil {
		close(backend.pollStarted)
		backend.pollStarted = nil
	}
	if backend.releasePoll != nil {
		<-backend.releasePoll
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.fetches) == 0 {
		return nil
	}
	fetches := backend.fetches[0]
	backend.fetches = backend.fetches[1:]

	return fetches
}

func (backend *recordingTransactionProcessorBackend) Begin() error {
	backend.beginCalls++

	return backend.beginErr
}

func (backend *recordingTransactionProcessorBackend) ProduceSync(
	ctx context.Context,
	records ...*kgo.Record,
) kgo.ProduceResults {
	backend.produced = append(backend.produced, records...)
	if backend.produceSync != nil {
		return backend.produceSync(ctx, records...)
	}
	if len(backend.produceResults) != 0 {
		results := backend.produceResults[0]
		backend.produceResults = backend.produceResults[1:]

		return results
	}
	results := make(kgo.ProduceResults, len(records))
	for index, record := range records {
		results[index] = kgo.ProduceResult{Record: record}
	}

	return results
}

func (backend *recordingTransactionProcessorBackend) End(
	_ context.Context,
	try kgo.TransactionEndTry,
) (bool, error) {
	backend.endTries = append(backend.endTries, try)
	if len(backend.endResults) == 0 {
		return false, nil
	}
	result := backend.endResults[0]
	backend.endResults = backend.endResults[1:]

	return result.committed, result.err
}

func (backend *recordingTransactionProcessorBackend) LeaveGroupContext(
	context.Context,
) error {
	backend.leaveCalls++

	return backend.leaveErr
}

func (backend *recordingTransactionProcessorBackend) Close() {
	backend.closed = true
}

func transactionFetches(records ...*kgo.Record) kgo.Fetches {
	return kgo.Fetches{{
		Topics: []kgo.FetchTopic{{
			Topic: "source-events",
			Partitions: []kgo.FetchPartition{{
				Partition: 0,
				Records:   records,
			}},
		}},
	}}
}

func transactionSourceRecord(offset int64, value string) *kgo.Record {
	return &kgo.Record{
		Topic:     "source-events",
		Partition: 0,
		Offset:    offset,
		Key:       []byte("key"),
		Value:     []byte(value),
	}
}
