package kafka

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBatchFailureCriticalGuardsTerminateDeterministically(t *testing.T) {
	t.Run("stop mode rejects every incompatible callback and target", func(t *testing.T) {
		handler := BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			return nil
		})
		for name, change := range map[string]func(*BatchFailureHandlerConfig){
			"target": func(config *BatchFailureHandlerConfig) {
				config.Target = FailureTarget{Topic: "events.retry.v1", Version: 1}
			},
			"publisher": func(config *BatchFailureHandlerConfig) {
				config.Publisher = BatchFailurePublisherFunc(func(
					context.Context,
					[]ProducerRecord,
				) ([]DeliveryResult, error) {
					return nil, nil
				})
			},
			"delegate": func(config *BatchFailureHandlerConfig) {
				config.Delegate = BatchFailureDelegateFunc(func(
					context.Context,
					BatchFailure,
				) error {
					return nil
				})
			},
		} {
			config := BatchFailureHandlerConfig{Handler: handler}
			change(&config)
			if err := config.Validate(); !errors.Is(err, ErrInvalidFailurePolicy) {
				t.Fatalf("%s stop mode error = %v", name, err)
			}
		}
	})

	t.Run("reroute mode rejects an invalid target topic", func(t *testing.T) {
		config := BatchFailureHandlerConfig{
			Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
				return nil
			}),
			Mode:   FailureModeRetryTopic,
			Target: FailureTarget{Topic: "invalid/topic", Version: 1},
			Publisher: BatchFailurePublisherFunc(func(
				context.Context,
				[]ProducerRecord,
			) ([]DeliveryResult, error) {
				return nil, nil
			}),
		}
		if err := config.Validate(); !errors.Is(err, ErrInvalidFailureTarget) {
			t.Fatalf("invalid target error = %v", err)
		}
	})

	t.Run("failure batch rejects an invalid topic", func(t *testing.T) {
		batch := testFailureBatch()
		batch.Topic = "invalid/topic"
		for index := range batch.Records {
			batch.Records[index].Topic = batch.Topic
		}
		if err := validateFailureBatch(
			batch,
			DefaultMessageLimits(),
			len(batch.Records),
			1<<20,
		); !errors.Is(err, ErrInvalidFailureBatch) {
			t.Fatalf("invalid failure batch error = %v", err)
		}
	})

	t.Run("retry exhaustion advances attempts once", func(t *testing.T) {
		handlerErr := errors.New("handler failed")
		unexpectedWait := errors.New("unexpected second wait")
		waitCalls := 0
		handler, err := newBatchFailureHandler(BatchFailureHandlerConfig{
			Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
				return handlerErr
			}),
			Classifier: FailureClassifierFunc(func(error) ErrorCategory {
				return ErrorRetryable
			}),
			Retry: FailureRetryPolicy{
				MaxAttempts:    2,
				InitialBackoff: time.Millisecond,
				MaxBackoff:     time.Millisecond,
			},
		}, func(context.Context, time.Duration) error {
			waitCalls++
			if waitCalls > 1 {
				return unexpectedWait
			}

			return nil
		})
		if err != nil {
			t.Fatalf("newBatchFailureHandler() error = %v", err)
		}

		err = handler.HandleBatch(context.Background(), testFailureBatch())

		if !errors.Is(err, ErrFailureAttemptsExhausted) ||
			errors.Is(err, unexpectedWait) || waitCalls != 1 {
			t.Fatalf("HandleBatch() error/waits = %v/%d", err, waitCalls)
		}
	})

	t.Run("permanent failure does not enter retry backoff", func(t *testing.T) {
		unexpectedWait := errors.New("unexpected wait")
		waitCalls := 0
		handler, err := newBatchFailureHandler(BatchFailureHandlerConfig{
			Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
				return errors.New("permanent failure")
			}),
			Classifier: FailureClassifierFunc(func(error) ErrorCategory {
				return ErrorPermanent
			}),
			Retry: FailureRetryPolicy{
				MaxAttempts:    2,
				InitialBackoff: time.Millisecond,
				MaxBackoff:     time.Millisecond,
				Categories:     []ErrorCategory{ErrorRetryable},
			},
		}, func(context.Context, time.Duration) error {
			waitCalls++

			return unexpectedWait
		})
		if err != nil {
			t.Fatalf("newBatchFailureHandler() error = %v", err)
		}

		err = handler.HandleBatch(context.Background(), testFailureBatch())

		if !errors.Is(err, ErrConsumerFailureStopped) ||
			errors.Is(err, unexpectedWait) || waitCalls != 0 {
			t.Fatalf("HandleBatch() error/waits = %v/%d", err, waitCalls)
		}
	})

	t.Run("source aggregate bytes include every record", func(t *testing.T) {
		batch := threeRecordFailureBatch()
		maximum := failureBatchInputBytes(batch) - 1

		err := validateFailureBatch(
			batch,
			DefaultMessageLimits(),
			len(batch.Records),
			maximum,
		)

		if !errors.Is(err, ErrBatchTooLarge) {
			t.Fatalf("validateFailureBatch() error = %v, want %v", err, ErrBatchTooLarge)
		}
	})

	t.Run("target aggregate bytes include every record", func(t *testing.T) {
		batch := threeRecordFailureBatch()
		var targetBytes int64
		config := BatchFailureHandlerConfig{
			Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
				return errors.New("failed")
			}),
			Mode:   FailureModeRetryTopic,
			Target: FailureTarget{Topic: "events.retry.v1", Version: 1},
			Publisher: BatchFailurePublisherFunc(func(
				_ context.Context,
				records []ProducerRecord,
			) ([]DeliveryResult, error) {
				results := make([]DeliveryResult, len(records))
				for index, record := range records {
					targetBytes += recordSize(record)
					results[index].Topic = "events.retry.v1"
				}

				return results, nil
			}),
		}
		handler, err := NewBatchFailureHandler(config)
		if err != nil {
			t.Fatalf("NewBatchFailureHandler(capture) error = %v", err)
		}
		if err = handler.HandleBatch(context.Background(), batch); err != nil {
			t.Fatalf("capture HandleBatch() error = %v", err)
		}

		publishCalls := 0
		config.MaxBatchBytes = targetBytes - 1
		config.Publisher = BatchFailurePublisherFunc(func(
			context.Context,
			[]ProducerRecord,
		) ([]DeliveryResult, error) {
			publishCalls++

			return nil, nil
		})
		handler, err = NewBatchFailureHandler(config)
		if err != nil {
			t.Fatalf("NewBatchFailureHandler(undersized) error = %v", err)
		}

		err = handler.HandleBatch(context.Background(), batch)

		if !errors.Is(err, ErrFailurePublish) ||
			!errors.Is(err, ErrBatchTooLarge) || publishCalls != 0 {
			t.Fatalf("HandleBatch() error/publish calls = %v/%d", err, publishCalls)
		}
	})
}

func threeRecordFailureBatch() ConsumedBatch {
	batch := testFailureBatch()
	batch.Records = append(batch.Records, ConsumedRecord{
		Topic: "events", Partition: 3, Offset: 9, Value: []byte("third"),
	})

	return batch
}
