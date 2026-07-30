package kafka

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestProducerCriticalGuardsTerminateDeterministically(t *testing.T) {
	t.Run("compression preferences require at least one codec", func(t *testing.T) {
		if err := validateCompressionPreferences(nil); !errors.Is(
			err,
			ErrInvalidCompressionPreference,
		) {
			t.Fatalf("validateCompressionPreferences() error = %v", err)
		}
	})

	t.Run("retry seed retains nonce and complete client identity", func(t *testing.T) {
		if producerRetrySeed(0, "a") == producerRetrySeed(1, "a") {
			t.Fatal("producerRetrySeed() discarded nonce entropy")
		}
		if producerRetrySeed(0, "a") == producerRetrySeed(0, "b") {
			t.Fatal("producerRetrySeed() discarded client identity")
		}
	})

	t.Run("cancellation invokes the configured interrupt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		interrupted := make(chan struct{})
		var interruptOnce sync.Once
		client := synchronousRecordProducerFunc(func(
			context.Context,
			...*kgo.Record,
		) kgo.ProduceResults {
			select {
			case <-interrupted:
			case <-time.After(100 * time.Millisecond):
			}

			return nil
		})

		_, err := transactionalProduceSync(
			ctx,
			time.Second,
			func() { interruptOnce.Do(func() { close(interrupted) }) },
			client,
			&kgo.Record{Topic: "events"},
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("transactionalProduceSync() error = %v", err)
		}
		select {
		case <-interrupted:
		default:
			t.Fatal("transactionalProduceSync() did not invoke interrupt")
		}
	})

	t.Run("batch byte limit includes every record", func(t *testing.T) {
		record := ProducerRecord{
			Topic: "events", Key: []byte("key"), Value: []byte("value"),
		}
		size := recordSize(record)
		backend := &recordingProducerBackend{}
		producer := producerForCriticalGuard(
			backend,
			3,
			3*size-1,
		)

		results, err := producer.PublishBatch(
			context.Background(),
			[]ProducerRecord{record, record, record},
		)
		if !errors.Is(err, ErrBatchTooLarge) || results != nil {
			t.Fatalf("PublishBatch() results/error = %#v/%v", results, err)
		}
		if len(backend.records) != 0 {
			t.Fatalf("backend received %d records", len(backend.records))
		}
	})

	t.Run("invalid deliveries do not hide later valid results", func(t *testing.T) {
		backend := &recordingProducerBackend{
			produceSync: func(
				_ context.Context,
				records ...*kgo.Record,
			) kgo.ProduceResults {
				return kgo.ProduceResults{
					{Record: nil},
					{Record: records[0]},
					{Record: &kgo.Record{Topic: "unexpected"}},
					{Record: records[1]},
				}
			},
		}
		producer := producerForCriticalGuard(backend, 2, 1<<20)

		results, err := producer.PublishBatch(
			context.Background(),
			[]ProducerRecord{
				{Topic: "events", Key: []byte("first")},
				{Topic: "events", Key: []byte("second")},
			},
		)
		if !errors.Is(err, ErrBatchDeliveryFailed) ||
			!errors.Is(err, ErrDeliveryResultInvalid) {
			t.Fatalf("PublishBatch() error = %v", err)
		}
		if len(results) != 2 || results[0].Err != nil || results[1].Err != nil {
			t.Fatalf("PublishBatch() results = %#v", results)
		}
	})

	t.Run("closed producer rejects health before backend access", func(t *testing.T) {
		backendErr := errors.New("backend must not be called")
		producer := producerForCriticalGuard(
			&recordingProducerBackend{healthErr: backendErr},
			1,
			1<<20,
		)
		producer.closed = true

		err := producer.Health(context.Background())
		if !errors.Is(err, ErrProducerClosed) || errors.Is(err, backendErr) {
			t.Fatalf("Health() error = %v", err)
		}
	})

	t.Run("nil admission channel is already drained", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := waitAdmissions(ctx, nil); err != nil {
			t.Fatalf("waitAdmissions() error = %v", err)
		}
	})

	t.Run("shutdown rejects active maintenance", func(t *testing.T) {
		producer := producerForCriticalGuard(
			&recordingProducerBackend{},
			1,
			1<<20,
		)
		producer.maintenanceActive = true
		if err := producer.Shutdown(context.Background()); !errors.Is(
			err,
			ErrProducerBusy,
		) {
			t.Fatalf("Shutdown() error = %v", err)
		}
	})

	t.Run("operation admission counters return to zero", func(t *testing.T) {
		producer := &Producer{}
		if err := producer.startOperation(); err != nil {
			t.Fatalf("startOperation() error = %v", err)
		}
		if producer.admitting != 1 || producer.inflight != 1 ||
			producer.admissionsDone == nil {
			t.Fatalf(
				"started counts/channel = %d/%d/%v",
				producer.admitting,
				producer.inflight,
				producer.admissionsDone,
			)
		}
		admissions := producer.admissionsDone
		producer.finishAdmission()
		producer.finishOperation()
		if producer.admitting != 0 || producer.inflight != 0 ||
			producer.admissionsDone != nil {
			t.Fatalf(
				"finished counts/channel = %d/%d/%v",
				producer.admitting,
				producer.inflight,
				producer.admissionsDone,
			)
		}
		select {
		case <-admissions:
		default:
			t.Fatal("admission completion channel is open")
		}
	})
}

type synchronousRecordProducerFunc func(
	context.Context,
	...*kgo.Record,
) kgo.ProduceResults

func (produce synchronousRecordProducerFunc) ProduceSync(
	ctx context.Context,
	records ...*kgo.Record,
) kgo.ProduceResults {
	return produce(ctx, records...)
}

func producerForCriticalGuard(
	backend producerBackend,
	maxBatchRecords int,
	maxBatchBytes int64,
) *Producer {
	return &Producer{
		client:          backend,
		limits:          DefaultMessageLimits(),
		maxBatchRecords: maxBatchRecords,
		maxBatchBytes:   maxBatchBytes,
		allowedTopics:   map[string]struct{}{"events": {}},
	}
}
