package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestConsumedRecordRetainOwnsAllRecordBytes(t *testing.T) {
	t.Parallel()

	borrowed := ConsumedRecord{
		Topic:         "events",
		Partition:     3,
		Offset:        17,
		LeaderEpoch:   9,
		Timestamp:     time.Unix(1_700_000_000, 0),
		TimestampType: TimestampCreateTime,
		Key:           []byte("key"),
		Value:         []byte("value"),
		Headers: []Header{
			{Key: "first", Value: []byte("one")},
			{Key: "second", Value: nil},
		},
	}

	retained := borrowed.Retain()
	borrowed.Key[0] = 'K'
	borrowed.Value[0] = 'V'
	borrowed.Headers[0].Key = "changed"
	borrowed.Headers[0].Value[0] = 'O'
	borrowed.Headers = append(borrowed.Headers, Header{Key: "third"})

	if string(retained.Key) != "key" || string(retained.Value) != "value" {
		t.Fatalf("Retain() aliased key or value: %#v", retained)
	}
	if len(retained.Headers) != 2 ||
		retained.Headers[0].Key != "first" ||
		string(retained.Headers[0].Value) != "one" ||
		retained.Headers[1].Key != "second" ||
		retained.Headers[1].Value != nil {
		t.Fatalf("Retain() aliased headers: %#v", retained.Headers)
	}
	if retained.Topic != "events" ||
		retained.Partition != 3 ||
		retained.Offset != 17 ||
		retained.LeaderEpoch != 9 ||
		retained.TimestampType != TimestampCreateTime {
		t.Fatalf("Retain() changed metadata: %#v", retained)
	}
}

func TestProducerPublishRecordReturnsDeliveryMetadataAndOwnsInput(t *testing.T) {
	t.Parallel()

	deliveredAt := time.Unix(1_700_000_123, 456)
	backend := &recordingProducerBackend{
		prepareDelivery: func(record *kgo.Record) {
			record.Partition = 7
			record.Offset = 42
			record.Timestamp = deliveredAt
		},
	}
	producer := &Producer{client: backend, limits: DefaultMessageLimits()}
	record := ProducerRecord{
		Topic: "events",
		Key:   []byte("key"),
		Value: []byte("value"),
		Headers: []Header{{
			Key: "content-type", Value: []byte("application/octet-stream"),
		}},
	}

	result := producer.PublishRecord(context.Background(), record)
	record.Key[0] = 'K'
	record.Value[0] = 'V'
	record.Headers[0].Key = "changed"
	record.Headers[0].Value[0] = 'A'

	if result.Err != nil {
		t.Fatalf("PublishRecord() error = %v", result.Err)
	}
	if result.Topic != "events" ||
		result.Partition != 7 ||
		result.Offset != 42 ||
		!result.Timestamp.Equal(deliveredAt) {
		t.Fatalf("PublishRecord() result = %#v", result)
	}
	if len(backend.records) != 1 ||
		string(backend.records[0].Key) != "key" ||
		string(backend.records[0].Value) != "value" ||
		backend.records[0].Headers[0].Key != "content-type" ||
		string(backend.records[0].Headers[0].Value) != "application/octet-stream" {
		t.Fatalf("PublishRecord() retained caller bytes: %#v", backend.records)
	}
}

func TestProducerPublishRecordReturnsPerRecordFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("delivery failed")
	backend := &recordingProducerBackend{deliveryErr: want}
	producer := &Producer{client: backend, limits: DefaultMessageLimits()}

	result := producer.PublishRecord(context.Background(), ProducerRecord{
		Topic: "events",
		Key:   []byte("key"),
	})

	if !errors.Is(result.Err, want) {
		t.Fatalf("PublishRecord() error = %v, want %v", result.Err, want)
	}
}

func TestProducerRequiresKeysUnlessUnkeyedProductionIsExplicit(t *testing.T) {
	t.Parallel()

	defaultConfig, err := normalizeProducerConfig(ProducerConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "producer",
	})
	if err != nil {
		t.Fatalf("normalize default producer: %v", err)
	}
	if defaultConfig.KeyPolicy != KeyRequired {
		t.Fatalf("default key policy = %v, want %v", defaultConfig.KeyPolicy, KeyRequired)
	}
	if _, err := normalizeProducerConfig(ProducerConfig{
		Brokers:   []string{"broker.internal:9092"},
		ClientID:  "producer",
		KeyPolicy: KeyPolicy(255),
	}); !errors.Is(err, ErrInvalidProducerConfig) {
		t.Fatalf("invalid key policy error = %v", err)
	}

	backend := &recordingProducerBackend{}
	producer := &Producer{
		client:      backend,
		limits:      DefaultMessageLimits(),
		keyRequired: true,
	}
	result := producer.PublishRecord(context.Background(), ProducerRecord{Topic: "events"})
	if !errors.Is(result.Err, ErrKeyRequired) {
		t.Fatalf("key-required PublishRecord() error = %v", result.Err)
	}
	if len(backend.records) != 0 {
		t.Fatalf("key-required PublishRecord() produced %d records", len(backend.records))
	}

	producer.keyRequired = false
	result = producer.PublishRecord(context.Background(), ProducerRecord{Topic: "events"})
	if result.Err != nil {
		t.Fatalf("explicit unkeyed PublishRecord() error = %v", result.Err)
	}
}

func TestProducerConfigurationBoundsBufferedBytesIndependentlyFromBatchBytes(t *testing.T) {
	t.Parallel()

	config, err := normalizeProducerConfig(ProducerConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "producer",
	})
	if err != nil {
		t.Fatalf("normalize default producer: %v", err)
	}
	if config.MaxBufferedBytes != 64<<20 {
		t.Fatalf("default MaxBufferedBytes = %d", config.MaxBufferedBytes)
	}
	if _, err := normalizeProducerConfig(ProducerConfig{
		Brokers:          []string{"broker.internal:9092"},
		ClientID:         "producer",
		MaxBatchBytes:    2 << 20,
		MaxBufferedBytes: 1 << 20,
	}); !errors.Is(err, ErrInvalidProducerConfig) {
		t.Fatalf("buffer smaller than batch error = %v", err)
	}
}

func TestProducerPublishBatchReturnsOrderedPartialDeliveryResults(t *testing.T) {
	t.Parallel()

	wantFailure := errors.New("second delivery failed")
	backend := &recordingProducerBackend{
		deliveryErrors: []error{nil, wantFailure, nil},
		prepareDelivery: func(record *kgo.Record) {
			record.Partition = int32(record.Key[0] - 'a')
			record.Offset = int64(record.Partition + 10)
		},
	}
	producer := &Producer{
		client:          backend,
		limits:          DefaultMessageLimits(),
		keyRequired:     true,
		maxBatchRecords: 3,
		maxBatchBytes:   1 << 20,
	}
	records := []ProducerRecord{
		{Topic: "events", Key: []byte("a"), Value: []byte("one")},
		{Topic: "events", Key: []byte("b"), Value: []byte("two")},
		{Topic: "events", Key: []byte("c"), Value: []byte("three")},
	}

	results, err := producer.PublishBatch(context.Background(), records)
	records[0].Key[0] = 'z'
	records[0].Value[0] = 'X'

	if !errors.Is(err, ErrBatchDeliveryFailed) {
		t.Fatalf("PublishBatch() error = %v, want %v", err, ErrBatchDeliveryFailed)
	}
	if len(results) != 3 {
		t.Fatalf("PublishBatch() results = %d, want 3", len(results))
	}
	for index, result := range results {
		if result.Topic != "events" ||
			result.Partition != int32(index) ||
			result.Offset != int64(index+10) {
			t.Fatalf("PublishBatch() result %d = %#v", index, result)
		}
	}
	if results[0].Err != nil ||
		!errors.Is(results[1].Err, wantFailure) ||
		results[2].Err != nil {
		t.Fatalf("PublishBatch() errors = %#v", results)
	}
	if string(backend.records[0].Key) != "a" ||
		string(backend.records[0].Value) != "one" {
		t.Fatalf("PublishBatch() aliased input: %#v", backend.records[0])
	}
}

func TestProducerPublishBatchRejectsInvalidBatchBeforeProduction(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		records []ProducerRecord
		want    error
	}{
		"empty": {want: ErrRecordsRequired},
		"record count": {
			records: []ProducerRecord{
				{Topic: "events", Key: []byte("a")},
				{Topic: "events", Key: []byte("b")},
			},
			want: ErrTooManyBatchRecords,
		},
		"aggregate bytes": {
			records: []ProducerRecord{{
				Topic: "events", Key: []byte("a"), Value: []byte("too large"),
			}},
			want: ErrBatchTooLarge,
		},
		"record": {
			records: []ProducerRecord{{Topic: "", Key: []byte("a")}},
			want:    ErrTopicRequired,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			backend := &recordingProducerBackend{}
			producer := &Producer{
				client:          backend,
				limits:          DefaultMessageLimits(),
				keyRequired:     true,
				maxBatchRecords: 1,
				maxBatchBytes:   16,
			}

			results, err := producer.PublishBatch(context.Background(), test.records)

			if results != nil {
				t.Fatalf("PublishBatch() results = %#v, want nil", results)
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("PublishBatch() error = %v, want %v", err, test.want)
			}
			if len(backend.records) != 0 {
				t.Fatalf("PublishBatch() produced %d records", len(backend.records))
			}
		})
	}
}

func TestProducerPublishAsyncOwnsInputAndReportsLaterDelivery(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := &Producer{
		client:      backend,
		limits:      DefaultMessageLimits(),
		keyRequired: true,
	}
	record := ProducerRecord{
		Topic: "events",
		Key:   []byte("key"),
		Value: []byte("value"),
		Headers: []Header{{
			Key: "kind", Value: []byte("created"),
		}},
	}

	delivery, err := producer.PublishAsync(context.Background(), record)
	if err != nil {
		t.Fatalf("PublishAsync() error = %v", err)
	}
	if delivery == nil {
		t.Fatal("PublishAsync() returned a nil delivery channel")
	}
	record.Key[0] = 'K'
	record.Value[0] = 'V'
	record.Headers[0].Key = "changed"
	record.Headers[0].Value[0] = 'C'
	if len(backend.records) != 1 ||
		string(backend.records[0].Key) != "key" ||
		string(backend.records[0].Value) != "value" ||
		backend.records[0].Headers[0].Key != "kind" ||
		string(backend.records[0].Headers[0].Value) != "created" {
		t.Fatalf("PublishAsync() aliased input: %#v", backend.records)
	}
	select {
	case result := <-delivery:
		t.Fatalf("PublishAsync() completed before backend delivery: %#v", result)
	default:
	}

	backend.completeAsync(0, 4, 23, nil)
	result, open := <-delivery
	if !open {
		t.Fatal("PublishAsync() closed without a result")
	}
	if result.Err != nil ||
		result.Topic != "events" ||
		result.Partition != 4 ||
		result.Offset != 23 {
		t.Fatalf("PublishAsync() result = %#v", result)
	}
	if _, open := <-delivery; open {
		t.Fatal("PublishAsync() delivery channel remained open")
	}
}

func TestProducerCloseIsIdempotentAndFencesNewOperations(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := &Producer{client: backend, limits: DefaultMessageLimits()}

	producer.Close()
	producer.Close()
	publishResult := producer.PublishRecord(context.Background(), ProducerRecord{
		Topic: "events",
		Key:   []byte("key"),
	})
	batchResults, batchErr := producer.PublishBatch(context.Background(), []ProducerRecord{{
		Topic: "events",
		Key:   []byte("key"),
	}})
	asyncResult, asyncErr := producer.PublishAsync(context.Background(), ProducerRecord{
		Topic: "events",
		Key:   []byte("key"),
	})
	healthErr := producer.Health(context.Background())

	if backend.closes != 1 {
		t.Fatalf("Close() calls = %d, want 1", backend.closes)
	}
	if !errors.Is(publishResult.Err, ErrProducerClosed) ||
		batchResults != nil || !errors.Is(batchErr, ErrProducerClosed) ||
		asyncResult != nil || !errors.Is(asyncErr, ErrProducerClosed) ||
		!errors.Is(healthErr, ErrProducerClosed) {
		t.Fatalf(
			"post-close results = %#v/%#v/%v/%v/%v/%v",
			publishResult,
			batchResults,
			batchErr,
			asyncResult,
			asyncErr,
			healthErr,
		)
	}
	if len(backend.records) != 0 {
		t.Fatalf("post-close produced %d records", len(backend.records))
	}
}

func TestProducerLifecycleSupportsBoundedDrainAbortAndShutdown(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{flushErr: context.Canceled}
	producer := &Producer{client: backend, limits: DefaultMessageLimits()}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	if err := producer.Drain(canceled); !errors.Is(err, ErrDrainIncomplete) ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("Drain() error = %v", err)
	}
	backend.flushErr = nil
	if err := producer.Drain(context.Background()); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if err := producer.Abort(context.Background()); err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
	if err := producer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := producer.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
	if backend.flushes != 3 || backend.aborts != 1 || backend.closes != 1 {
		t.Fatalf("lifecycle calls = flush:%d abort:%d close:%d", backend.flushes, backend.aborts, backend.closes)
	}
}

func TestProducerFailedShutdownFencesProductionButAllowsRecovery(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{flushErr: context.Canceled}
	producer := &Producer{client: backend, limits: DefaultMessageLimits()}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	if err := producer.Shutdown(canceled); !errors.Is(err, ErrDrainIncomplete) {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if result := producer.PublishRecord(context.Background(), ProducerRecord{
		Topic: "events",
		Key:   []byte("key"),
	}); !errors.Is(result.Err, ErrProducerClosed) {
		t.Fatalf("post-shutdown PublishRecord() error = %v", result.Err)
	}
	if err := producer.Abort(context.Background()); err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
	backend.flushErr = nil
	if err := producer.Shutdown(context.Background()); err != nil {
		t.Fatalf("retry Shutdown() error = %v", err)
	}
	if backend.aborts != 1 || backend.closes != 1 {
		t.Fatalf("recovery calls = abort:%d close:%d", backend.aborts, backend.closes)
	}
}

func TestProducerRejectsNilOperationContexts(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := transactionalProducer(backend)
	record := ProducerRecord{Topic: "events", Key: []byte("key")}
	var nilContext context.Context

	if result := producer.PublishRecord(nilContext, record); !errors.Is(result.Err, ErrContextRequired) {
		t.Fatalf("PublishRecord(nil) error = %v", result.Err)
	}
	if results, err := producer.PublishBatch(nilContext, []ProducerRecord{record}); results != nil || !errors.Is(err, ErrContextRequired) {
		t.Fatalf("PublishBatch(nil) = %#v, %v", results, err)
	}
	if result, err := producer.PublishAsync(nilContext, record); result != nil || !errors.Is(err, ErrContextRequired) {
		t.Fatalf("PublishAsync(nil) = %v, %v", result, err)
	}
	if err := producer.Health(nilContext); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Health(nil) error = %v", err)
	}
	if err := producer.RunTransaction(nilContext, func(Transaction) error { return nil }); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("RunTransaction(nil) error = %v", err)
	}
	if err := (Transaction{session: &transactionSession{producer: producer}}).Publish(nilContext, record); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Transaction.Publish(nil) error = %v", err)
	}
	if err := producer.Drain(nilContext); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Drain(nil) error = %v", err)
	}
	if err := producer.Abort(nilContext); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Abort(nil) error = %v", err)
	}
	if err := producer.Shutdown(nilContext); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Shutdown(nil) error = %v", err)
	}
	if backend.begins != 0 || len(backend.records) != 0 {
		t.Fatalf("nil contexts reached backend: %#v", backend)
	}
}

func TestProducerReportsMissingSyncAndAsyncDeliveryResults(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{omitDeliveries: true}
	producer := &Producer{
		client: backend, limits: DefaultMessageLimits(),
		maxBatchRecords: 2, maxBatchBytes: 1 << 20,
	}
	records := []ProducerRecord{
		{Topic: "events", Key: []byte("a")},
		{Topic: "events", Key: []byte("b")},
	}

	if result := producer.PublishRecord(context.Background(), records[0]); !errors.Is(result.Err, ErrDeliveryResultMissing) {
		t.Fatalf("PublishRecord() error = %v", result.Err)
	}
	results, err := producer.PublishBatch(context.Background(), records)
	if !errors.Is(err, ErrBatchDeliveryFailed) ||
		len(results) != 2 ||
		!errors.Is(results[0].Err, ErrDeliveryResultMissing) ||
		!errors.Is(results[1].Err, ErrDeliveryResultMissing) {
		t.Fatalf("PublishBatch() = %#v, %v", results, err)
	}

	backend.omitDeliveries = false
	delivery, err := producer.PublishAsync(context.Background(), records[0])
	if err != nil {
		t.Fatalf("PublishAsync() error = %v", err)
	}
	backend.completeAsyncMissing(0, context.DeadlineExceeded)
	result := <-delivery
	if !errors.Is(result.Err, ErrDeliveryResultMissing) ||
		!errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("PublishAsync() missing delivery error = %v", result.Err)
	}
}

func TestProducerBatchAndAsyncValidateEveryRecordPolicy(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := &Producer{
		client: backend, limits: DefaultMessageLimits(), keyRequired: true,
		maxBatchRecords: 2, maxBatchBytes: 1 << 20,
	}

	if results, err := producer.PublishBatch(context.Background(), []ProducerRecord{{
		Topic: "events",
	}}); results != nil || !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("key-required PublishBatch() = %#v, %v", results, err)
	}
	if delivery, err := producer.PublishAsync(context.Background(), ProducerRecord{}); delivery != nil || !errors.Is(err, ErrTopicRequired) {
		t.Fatalf("invalid PublishAsync() = %v, %v", delivery, err)
	}
	if delivery, err := producer.PublishAsync(context.Background(), ProducerRecord{Topic: "events"}); delivery != nil || !errors.Is(err, ErrKeyRequired) {
		t.Fatalf("key-required PublishAsync() = %v, %v", delivery, err)
	}

	producer.keyRequired = false
	results, err := producer.PublishBatch(context.Background(), []ProducerRecord{{
		Topic:   "events",
		Headers: []Header{{Key: "kind", Value: []byte("created")}},
	}})
	if err != nil || len(results) != 1 || results[0].Err != nil {
		t.Fatalf("successful PublishBatch() = %#v, %v", results, err)
	}
}

func TestProducerRejectsOperationsAcrossTransactionAndMaintenanceBoundaries(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := transactionalProducer(backend)
	err := producer.RunTransaction(context.Background(), func(Transaction) error {
		if healthErr := producer.Health(context.Background()); !errors.Is(healthErr, ErrTransactionInProgress) {
			t.Fatalf("Health() during transaction = %v", healthErr)
		}
		if nestedErr := producer.RunTransaction(context.Background(), func(Transaction) error { return nil }); !errors.Is(nestedErr, ErrTransactionInProgress) {
			t.Fatalf("nested RunTransaction() = %v", nestedErr)
		}
		if drainErr := producer.Drain(context.Background()); !errors.Is(drainErr, ErrProducerBusy) {
			t.Fatalf("Drain() during transaction = %v", drainErr)
		}
		if abortErr := producer.Abort(context.Background()); !errors.Is(abortErr, ErrProducerBusy) {
			t.Fatalf("Abort() during transaction = %v", abortErr)
		}
		if shutdownErr := producer.Shutdown(context.Background()); !errors.Is(shutdownErr, ErrProducerBusy) {
			t.Fatalf("Shutdown() during transaction = %v", shutdownErr)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("RunTransaction() error = %v", err)
	}

	backend.flushStarted = make(chan struct{})
	backend.flushRelease = make(chan struct{})
	drainDone := make(chan error, 1)
	go func() { drainDone <- producer.Drain(context.Background()) }()
	<-backend.flushStarted
	if result := producer.PublishRecord(context.Background(), ProducerRecord{Topic: "events"}); !errors.Is(result.Err, ErrProducerBusy) {
		t.Fatalf("PublishRecord() during drain = %v", result.Err)
	}
	if txErr := producer.RunTransaction(context.Background(), func(Transaction) error { return nil }); !errors.Is(txErr, ErrProducerBusy) {
		t.Fatalf("RunTransaction() during drain = %v", txErr)
	}
	close(backend.flushRelease)
	if err := <-drainDone; err != nil {
		t.Fatalf("Drain() error = %v", err)
	}

	delivery, err := producer.PublishAsync(context.Background(), ProducerRecord{Topic: "events"})
	if err != nil {
		t.Fatalf("PublishAsync() error = %v", err)
	}
	if txErr := producer.RunTransaction(context.Background(), func(Transaction) error { return nil }); !errors.Is(txErr, ErrProducerBusy) {
		t.Fatalf("RunTransaction() with async delivery = %v", txErr)
	}
	backend.completeAsync(0, 0, 1, nil)
	<-delivery

	producer.Close()
	if txErr := producer.RunTransaction(context.Background(), func(Transaction) error { return nil }); !errors.Is(txErr, ErrProducerClosed) {
		t.Fatalf("RunTransaction() after close = %v", txErr)
	}
	if err := producer.Drain(context.Background()); !errors.Is(err, ErrProducerClosed) {
		t.Fatalf("Drain() after close = %v", err)
	}
	if err := producer.Abort(context.Background()); !errors.Is(err, ErrProducerClosed) {
		t.Fatalf("Abort() after close = %v", err)
	}
}
