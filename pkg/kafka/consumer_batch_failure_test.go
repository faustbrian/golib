package kafka

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestNewBatchFailureHandlerValidatesBoundedPolicy(t *testing.T) {

	base := BatchFailureHandlerConfig{
		Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			return errors.New("failed")
		}),
	}
	tests := []struct {
		name   string
		change func(*BatchFailureHandlerConfig)
		want   error
	}{
		{
			name: "handler required",
			change: func(config *BatchFailureHandlerConfig) {
				config.Handler = nil
			},
			want: ErrBatchHandlerRequired,
		},
		{
			name: "mode bounded",
			change: func(config *BatchFailureHandlerConfig) {
				config.Mode = FailureMode(255)
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "limits valid",
			change: func(config *BatchFailureHandlerConfig) {
				config.Limits = MessageLimits{MaxTopicBytes: -1}
			},
			want: ErrInvalidMessageLimits,
		},
		{
			name: "retry policy valid",
			change: func(config *BatchFailureHandlerConfig) {
				config.Retry.MaxAttempts = -1
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "record count bounded",
			change: func(config *BatchFailureHandlerConfig) {
				config.MaxBatchRecords = 1_001
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "bytes bounded",
			change: func(config *BatchFailureHandlerConfig) {
				config.MaxBatchBytes = 101 << 20
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "stop fields incompatible",
			change: func(config *BatchFailureHandlerConfig) {
				config.PublishTimeout = time.Second
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "retry topic publisher required",
			change: func(config *BatchFailureHandlerConfig) {
				config.Mode = FailureModeRetryTopic
				config.Target = FailureTarget{Topic: "events.retry.v1", Version: 1}
			},
			want: ErrFailurePublisherRequired,
		},
		{
			name: "publish delegate incompatible",
			change: func(config *BatchFailureHandlerConfig) {
				config.Mode = FailureModeRetryTopic
				config.Target = FailureTarget{Topic: "events.retry.v1", Version: 1}
				config.Publisher = successfulBatchFailurePublisher()
				config.Delegate = BatchFailureDelegateFunc(func(
					context.Context,
					BatchFailure,
				) error {
					return nil
				})
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "publish target valid",
			change: func(config *BatchFailureHandlerConfig) {
				config.Mode = FailureModeRetryTopic
				config.Publisher = successfulBatchFailurePublisher()
			},
			want: ErrInvalidFailureTarget,
		},
		{
			name: "publish timeout bounded",
			change: func(config *BatchFailureHandlerConfig) {
				config.Mode = FailureModeRetryTopic
				config.Target = FailureTarget{Topic: "events.retry.v1", Version: 1}
				config.Publisher = successfulBatchFailurePublisher()
				config.PublishTimeout = time.Millisecond
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "delegate required",
			change: func(config *BatchFailureHandlerConfig) {
				config.Mode = FailureModeDelegate
			},
			want: ErrFailureDelegateRequired,
		},
		{
			name: "delegate fields incompatible",
			change: func(config *BatchFailureHandlerConfig) {
				config.Mode = FailureModeDelegate
				config.Delegate = BatchFailureDelegateFunc(func(
					context.Context,
					BatchFailure,
				) error {
					return nil
				})
				config.Target = FailureTarget{Topic: "events.retry.v1", Version: 1}
			},
			want: ErrInvalidFailurePolicy,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {

			config := base
			test.change(&config)
			if err := config.Validate(); !errors.Is(err, test.want) {
				t.Fatalf("Validate() error = %v, want %v", err, test.want)
			}
			if handler, err := NewBatchFailureHandler(config); handler != nil ||
				!errors.Is(err, test.want) {
				t.Fatalf("NewBatchFailureHandler() = %#v, %v", handler, err)
			}
		})
	}
}

func TestBatchFailureHandlerConfigAcceptsInclusivePolicyBoundaries(t *testing.T) {

	config := BatchFailureHandlerConfig{
		Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			return errors.New("failed")
		}),
		Mode:            FailureModeRetryTopic,
		Target:          FailureTarget{Topic: "events.retry.v1", Version: 1},
		Publisher:       successfulBatchFailurePublisher(),
		MaxBatchRecords: maximumFailureBatchRecords,
		MaxBatchBytes:   maximumFailureBatchBytes,
		PublishTimeout:  2 * time.Minute,
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("inclusive BatchFailureHandlerConfig.Validate() error = %v", err)
	}
	handler, err := NewBatchFailureHandler(config)
	if err != nil || handler == nil {
		t.Fatalf("inclusive NewBatchFailureHandler() = %#v/%v", handler, err)
	}

	config.PublishTimeout = 100 * time.Millisecond
	if err = config.Validate(); err != nil {
		t.Fatalf("minimum publish timeout Validate() error = %v", err)
	}
}

func TestBatchFailureHandlerRejectsInvalidBatchesBeforeRetention(t *testing.T) {

	valid := testFailureBatch()
	tests := []struct {
		name       string
		batch      ConsumedBatch
		maxRecords int
		maxBytes   int64
		want       error
	}{
		{name: "empty", batch: ConsumedBatch{Topic: "events"}, want: ErrRecordsRequired},
		{name: "record count", batch: valid, maxRecords: 1, want: ErrTooManyBatchRecords},
		{
			name: "partition",
			batch: func() ConsumedBatch {
				batch := testFailureBatch()
				batch.Partition = -1

				return batch
			}(),
			want: ErrInvalidFailureBatch,
		},
		{
			name: "record coordinates",
			batch: func() ConsumedBatch {
				batch := testFailureBatch()
				batch.Records[1].Partition = 4

				return batch
			}(),
			want: ErrInvalidFailureBatch,
		},
		{
			name: "offset order",
			batch: func() ConsumedBatch {
				batch := testFailureBatch()
				batch.Records[1].Offset = batch.Records[0].Offset

				return batch
			}(),
			want: ErrInvalidFailureBatch,
		},
		{
			name: "record limits",
			batch: func() ConsumedBatch {
				batch := testFailureBatch()
				batch.Records[0].Headers[0].Key = ""

				return batch
			}(),
			want: ErrHeaderKeyRequired,
		},
		{
			name: "record timestamp type",
			batch: func() ConsumedBatch {
				batch := testFailureBatch()
				batch.Records[0].TimestampType = TimestampType(2)

				return batch
			}(),
			want: ErrFailureRecordInvalid,
		},
		{
			name: "record leader epoch",
			batch: func() ConsumedBatch {
				batch := testFailureBatch()
				batch.Records[0].LeaderEpoch = -2

				return batch
			}(),
			want: ErrFailureRecordInvalid,
		},
		{name: "aggregate bytes", batch: valid, maxBytes: 1, want: ErrBatchTooLarge},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {

			calls := 0
			config := BatchFailureHandlerConfig{
				Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
					calls++

					return nil
				}),
			}
			if test.maxRecords != 0 {
				config.MaxBatchRecords = test.maxRecords
			}
			if test.maxBytes != 0 {
				config.MaxBatchBytes = test.maxBytes
			}
			handler, err := NewBatchFailureHandler(config)
			if err != nil {
				t.Fatalf("NewBatchFailureHandler() error = %v", err)
			}

			got := handler.HandleBatch(context.Background(), test.batch)

			if !errors.Is(got, ErrInvalidFailureBatch) ||
				!errors.Is(got, test.want) || calls != 0 {
				t.Fatalf("HandleBatch() error/calls = %v/%d", got, calls)
			}
		})
	}

	handler, err := NewBatchFailureHandler(BatchFailureHandlerConfig{
		Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			return nil
		}),
	})
	if err != nil {
		t.Fatalf("NewBatchFailureHandler() error = %v", err)
	}
	var nilContext context.Context
	if got := handler.HandleBatch(nilContext, valid); !errors.Is(got, ErrContextRequired) {
		t.Fatalf("HandleBatch(nil) error = %v", got)
	}
}

func TestFailureBatchValidationAcceptsExactCountBytesAndOffsetZero(t *testing.T) {

	batch := testFailureBatch()
	batch.Records[0].Offset = 0
	batch.Records[1].Offset = 1
	exactBytes := failureBatchInputBytes(batch)
	if err := validateFailureBatch(
		batch,
		DefaultMessageLimits(),
		len(batch.Records),
		exactBytes,
	); err != nil {
		t.Fatalf("exact-limit validateFailureBatch() error = %v", err)
	}
	if err := validateFailureBatch(
		batch,
		DefaultMessageLimits(),
		len(batch.Records),
		exactBytes-1,
	); !errors.Is(err, ErrBatchTooLarge) {
		t.Fatalf("undersized validateFailureBatch() error = %v", err)
	}
}

func TestBatchFailureHandlerRetriesAnOwnedWholeBatch(t *testing.T) {

	handlerErr := errors.New("retry batch")
	input := testFailureBatch()
	var attempts int
	var waited time.Duration
	handler, err := newBatchFailureHandler(
		BatchFailureHandlerConfig{
			Handler: BatchHandlerFunc(func(_ context.Context, batch ConsumedBatch) error {
				attempts++
				if attempts == 1 {
					batch.Records[0].Value[0] = 'x'
					batch.Records[0].Headers[0].Value[0] = 'x'

					return handlerErr
				}
				if string(batch.Records[0].Value) != "first" ||
					string(batch.Records[0].Headers[0].Value) != "trace" {
					t.Fatalf("retry aliased first attempt: %#v", batch)
				}

				return nil
			}),
			Classifier: FailureClassifierFunc(func(error) ErrorCategory {
				return ErrorRetryable
			}),
			Retry: FailureRetryPolicy{
				MaxAttempts:    2,
				InitialBackoff: time.Millisecond,
				MaxBackoff:     time.Millisecond,
			},
		},
		func(_ context.Context, delay time.Duration) error {
			waited = delay

			return nil
		},
	)
	if err != nil {
		t.Fatalf("newBatchFailureHandler() error = %v", err)
	}

	if err := handler.HandleBatch(context.Background(), input); err != nil {
		t.Fatalf("HandleBatch() error = %v", err)
	}
	if attempts != 2 || waited != time.Millisecond ||
		string(input.Records[0].Value) != "first" ||
		string(input.Records[0].Headers[0].Value) != "trace" {
		t.Fatalf("attempts/wait/input = %d/%s/%#v", attempts, waited, input)
	}
}

func TestBatchFailureCausePreservesProgrammaticIdentity(t *testing.T) {

	cause := errors.New("handler failed")
	failure := BatchFailure{cause: cause}
	if !errors.Is(failure.Cause(), cause) {
		t.Fatalf("Cause() = %v", failure.Cause())
	}
}

func TestBatchFailureHandlerFailureStagesRemainBoundedAndRedacted(t *testing.T) {

	handlerErr := errors.New("sensitive batch handler detail")
	tests := []struct {
		name       string
		classifier FailureClassifier
		wait       failureWait
		cancel     bool
		want       error
		stage      FailureStage
	}{
		{
			name: "classifier panic",
			classifier: FailureClassifierFunc(func(error) ErrorCategory {
				panic("sensitive classifier detail")
			}),
			want:  ErrFailureCallbackPanic,
			stage: FailureStageClassify,
		},
		{
			name: "invalid classification",
			classifier: FailureClassifierFunc(func(error) ErrorCategory {
				return 0
			}),
			want:  ErrInvalidFailureClassification,
			stage: FailureStageClassify,
		},
		{
			name:   "cancellation",
			cancel: true,
			want:   context.Canceled,
			stage:  FailureStageStop,
		},
		{
			name: "backoff",
			classifier: FailureClassifierFunc(func(error) ErrorCategory {
				return ErrorRetryable
			}),
			wait:  func(context.Context, time.Duration) error { return context.DeadlineExceeded },
			want:  ErrFailureBackoff,
			stage: FailureStageBackoff,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {

			ctx, cancel := context.WithCancel(context.Background())
			if test.cancel {
				cancel()
			} else {
				defer cancel()
			}
			retry := FailureRetryPolicy{}
			if test.wait != nil {
				retry = FailureRetryPolicy{
					MaxAttempts: 2, InitialBackoff: time.Millisecond,
					MaxBackoff: time.Millisecond,
				}
			}
			wait := test.wait
			if wait == nil {
				wait = func(context.Context, time.Duration) error { return nil }
			}
			handler, err := newBatchFailureHandler(
				BatchFailureHandlerConfig{
					Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
						return handlerErr
					}),
					Classifier: test.classifier,
					Retry:      retry,
				},
				wait,
			)
			if err != nil {
				t.Fatalf("newBatchFailureHandler() error = %v", err)
			}

			got := handler.HandleBatch(ctx, testFailureBatch())

			var failureErr *FailureHandlingError
			if !errors.Is(got, test.want) || !errors.Is(got, handlerErr) ||
				!errors.As(got, &failureErr) || failureErr.Stage() != test.stage ||
				strings.Contains(got.Error(), "sensitive") {
				t.Fatalf("HandleBatch() error = %#v", got)
			}
		})
	}
}

func TestBatchFailureHandlerStopExhaustionAndInternalMode(t *testing.T) {

	handlerErr := errors.New("failed")
	handler, err := newBatchFailureHandler(
		BatchFailureHandlerConfig{
			Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
				return handlerErr
			}),
			Classifier: FailureClassifierFunc(func(error) ErrorCategory {
				return ErrorRetryable
			}),
			Retry: FailureRetryPolicy{
				MaxAttempts: 2, InitialBackoff: time.Millisecond,
				MaxBackoff: time.Millisecond,
			},
		},
		func(context.Context, time.Duration) error { return nil },
	)
	if err != nil {
		t.Fatalf("newBatchFailureHandler() error = %v", err)
	}

	got := handler.HandleBatch(context.Background(), testFailureBatch())
	if !errors.Is(got, ErrConsumerFailureStopped) ||
		!errors.Is(got, ErrFailureAttemptsExhausted) ||
		!errors.Is(got, handlerErr) {
		t.Fatalf("HandleBatch() exhaustion error = %#v", got)
	}

	handler.mode = FailureMode(255)
	got = handler.HandleBatch(context.Background(), testFailureBatch())
	if !errors.Is(got, ErrInvalidFailurePolicy) || !errors.Is(got, handlerErr) {
		t.Fatalf("HandleBatch() invalid mode error = %#v", got)
	}
}

func TestBatchFailureHandlerSingleStopAttemptIsNotExhaustion(t *testing.T) {

	handlerErr := errors.New("failed")
	handler, err := NewBatchFailureHandler(BatchFailureHandlerConfig{
		Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			return handlerErr
		}),
		Classifier: FailureClassifierFunc(func(error) ErrorCategory {
			return ErrorRetryable
		}),
		Retry: FailureRetryPolicy{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("NewBatchFailureHandler() error = %v", err)
	}
	got := handler.HandleBatch(context.Background(), testFailureBatch())
	if !errors.Is(got, ErrConsumerFailureStopped) ||
		!errors.Is(got, handlerErr) ||
		errors.Is(got, ErrFailureAttemptsExhausted) {
		t.Fatalf("single-attempt stop error = %#v", got)
	}
}

func TestBatchFailureHandlerSurfacesPartialRetryTopicPublication(t *testing.T) {

	handlerErr := errors.New("batch failed")
	publishErr := errors.New("second delivery failed")
	var published []ProducerRecord
	publisher := BatchFailurePublisherFunc(func(
		_ context.Context,
		records []ProducerRecord,
	) ([]DeliveryResult, error) {
		published = append([]ProducerRecord(nil), records...)

		return []DeliveryResult{
			{Topic: "events.retry.v1", Partition: 1, Offset: 20},
			{Topic: "events.retry.v1", Err: publishErr},
		}, errors.Join(ErrBatchDeliveryFailed, publishErr)
	})
	handler, err := NewBatchFailureHandler(BatchFailureHandlerConfig{
		Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			return handlerErr
		}),
		Mode:      FailureModeRetryTopic,
		Target:    FailureTarget{Topic: "events.retry.v1", Version: 1},
		Publisher: publisher,
	})
	if err != nil {
		t.Fatalf("NewBatchFailureHandler() error = %v", err)
	}

	got := handler.HandleBatch(context.Background(), testFailureBatch())

	var failureErr *FailureHandlingError
	if !errors.Is(got, ErrFailurePublish) ||
		!errors.Is(got, ErrBatchDeliveryFailed) ||
		!errors.Is(got, handlerErr) ||
		!errors.Is(got, publishErr) ||
		!errors.As(got, &failureErr) ||
		failureErr.Stage() != FailureStagePublish ||
		failureErr.Attempt() != 1 {
		t.Fatalf("HandleBatch() error = %#v", got)
	}
	deliveries := failureErr.DeliveryResults()
	if len(deliveries) != 2 || deliveries[0].Offset != 20 ||
		!errors.Is(deliveries[1].Err, publishErr) {
		t.Fatalf("DeliveryResults() = %#v", deliveries)
	}
	deliveries[0].Offset = 99
	if failureErr.DeliveryResults()[0].Offset != 20 {
		t.Fatal("DeliveryResults() exposed mutable error state")
	}
	if len(published) != 2 ||
		published[0].Topic != "events.retry.v1" ||
		string(published[0].Key) != "key-1" ||
		string(published[0].Value) != "first" ||
		failureHeaderValue(published[0], "source-offset") != "7" ||
		failureHeaderValue(published[1], "source-offset") != "8" ||
		failureHeaderValue(published[0], "batch-index") != "0" ||
		failureHeaderValue(published[1], "batch-index") != "1" ||
		failureHeaderValue(published[0], "batch-count") != "2" {
		t.Fatalf("published records = %#v", published)
	}

	backend := &recordingConsumerBackend{fetches: recordFetches(
		&kgo.Record{Topic: "events", Partition: 3, Offset: 7},
		&kgo.Record{Topic: "events", Partition: 3, Offset: 8},
	)}
	consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
	result, runErr := consumer.RunBatchOnce(context.Background(), handler)
	if !errors.Is(runErr, ErrFailurePublish) ||
		result != (PollResult{Polled: 2}) || len(backend.committed) != 0 {
		t.Fatalf("partial settlement result/error/commits = %#v/%v/%#v", result, runErr, backend.committed)
	}
}

func TestBatchFailureHandlerPreservesPublisherErrorWithoutDeliveryFailures(t *testing.T) {

	handlerErr := errors.New("batch failed")
	publishErr := errors.New("publisher failed")
	handler, err := NewBatchFailureHandler(BatchFailureHandlerConfig{
		Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			return handlerErr
		}),
		Mode:   FailureModeRetryTopic,
		Target: FailureTarget{Topic: "events.retry.v1", Version: 1},
		Publisher: BatchFailurePublisherFunc(func(
			_ context.Context,
			records []ProducerRecord,
		) ([]DeliveryResult, error) {
			results := make([]DeliveryResult, len(records))
			for index := range results {
				results[index].Topic = "events.retry.v1"
			}

			return results, publishErr
		}),
	})
	if err != nil {
		t.Fatalf("NewBatchFailureHandler() error = %v", err)
	}
	got := handler.HandleBatch(context.Background(), testFailureBatch())
	if !errors.Is(got, ErrFailurePublish) ||
		!errors.Is(got, handlerErr) ||
		!errors.Is(got, publishErr) {
		t.Fatalf("publisher-only failure error = %#v", got)
	}
}

func TestBatchFailureHandlerAcceptsExactTargetBatchBytes(t *testing.T) {

	handlerErr := errors.New("batch failed")
	var captured []ProducerRecord
	capturingPublisher := BatchFailurePublisherFunc(func(
		_ context.Context,
		records []ProducerRecord,
	) ([]DeliveryResult, error) {
		captured = make([]ProducerRecord, len(records))
		copy(captured, records)
		results := make([]DeliveryResult, len(records))
		for index := range results {
			results[index].Topic = "events.retry.v1"
		}

		return results, nil
	})
	config := BatchFailureHandlerConfig{
		Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			return handlerErr
		}),
		Mode:      FailureModeRetryTopic,
		Target:    FailureTarget{Topic: "events.retry.v1", Version: 1},
		Publisher: capturingPublisher,
	}
	handler, err := NewBatchFailureHandler(config)
	if err != nil {
		t.Fatalf("NewBatchFailureHandler(capture) error = %v", err)
	}
	if err = handler.HandleBatch(context.Background(), testFailureBatch()); err != nil {
		t.Fatalf("capture HandleBatch() error = %v", err)
	}
	var exactBytes int64
	for _, record := range captured {
		exactBytes += recordSize(record)
	}

	config.MaxBatchBytes = exactBytes
	handler, err = NewBatchFailureHandler(config)
	if err != nil {
		t.Fatalf("NewBatchFailureHandler(exact) error = %v", err)
	}
	if err = handler.HandleBatch(context.Background(), testFailureBatch()); err != nil {
		t.Fatalf("exact-byte HandleBatch() error = %v", err)
	}

	publishCalls := 0
	config.MaxBatchBytes = exactBytes - 1
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
	got := handler.HandleBatch(context.Background(), testFailureBatch())
	if !errors.Is(got, ErrBatchTooLarge) || publishCalls != 0 {
		t.Fatalf("undersized target HandleBatch() = %v/calls:%d", got, publishCalls)
	}
}

func TestBatchFailureHandlerRejectsUnsafeTargetBatchBeforePublishing(t *testing.T) {

	tests := []struct {
		name       string
		target     string
		limits     MessageLimits
		batchBytes int64
		want       error
	}{
		{name: "source target", target: "events", want: ErrInvalidFailureTarget},
		{
			name:   "metadata limits",
			target: "events.retry.v1",
			limits: func() MessageLimits {
				limits := DefaultMessageLimits()
				limits.MaxHeaders = 12

				return limits
			}(),
			want: ErrFailureRecordInvalid,
		},
		{
			name:       "aggregate target bytes",
			target:     "events.retry.v1",
			batchBytes: failureBatchInputBytes(testFailureBatch()),
			want:       ErrBatchTooLarge,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {

			calls := 0
			config := BatchFailureHandlerConfig{
				Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
					return errors.New("failed")
				}),
				Mode:   FailureModeRetryTopic,
				Target: FailureTarget{Topic: test.target, Version: 1},
				Publisher: BatchFailurePublisherFunc(func(
					context.Context,
					[]ProducerRecord,
				) ([]DeliveryResult, error) {
					calls++

					return nil, nil
				}),
				Limits:        test.limits,
				MaxBatchBytes: test.batchBytes,
			}
			handler, err := NewBatchFailureHandler(config)
			if err != nil {
				t.Fatalf("NewBatchFailureHandler() error = %v", err)
			}

			got := handler.HandleBatch(context.Background(), testFailureBatch())

			if !errors.Is(got, ErrFailurePublish) ||
				!errors.Is(got, test.want) || calls != 0 {
				t.Fatalf("HandleBatch() error/calls = %#v/%d", got, calls)
			}
		})
	}
}

func TestBatchFailureHandlerContainsTerminalCallbackPanics(t *testing.T) {

	handler := BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
		panic("sensitive handler panic")
	})
	delegateHandler, err := NewBatchFailureHandler(BatchFailureHandlerConfig{
		Handler: handler,
		Mode:    FailureModeDelegate,
		Delegate: BatchFailureDelegateFunc(func(context.Context, BatchFailure) error {
			panic("sensitive delegate panic")
		}),
	})
	if err != nil {
		t.Fatalf("NewBatchFailureHandler(delegate) error = %v", err)
	}
	publisherHandler, err := NewBatchFailureHandler(BatchFailureHandlerConfig{
		Handler: handler,
		Mode:    FailureModeRetryTopic,
		Target:  FailureTarget{Topic: "events.retry.v1", Version: 1},
		Publisher: BatchFailurePublisherFunc(func(
			context.Context,
			[]ProducerRecord,
		) ([]DeliveryResult, error) {
			panic("sensitive publisher panic")
		}),
	})
	if err != nil {
		t.Fatalf("NewBatchFailureHandler(publisher) error = %v", err)
	}

	delegateErr := delegateHandler.HandleBatch(context.Background(), testFailureBatch())
	publisherErr := publisherHandler.HandleBatch(context.Background(), testFailureBatch())

	if !errors.Is(delegateErr, ErrHandlerPanic) ||
		!errors.Is(delegateErr, ErrFailureDelegate) ||
		!errors.Is(delegateErr, ErrFailureCallbackPanic) ||
		strings.Contains(delegateErr.Error(), "sensitive") ||
		!errors.Is(publisherErr, ErrHandlerPanic) ||
		!errors.Is(publisherErr, ErrFailurePublish) ||
		!errors.Is(publisherErr, ErrFailureCallbackPanic) ||
		strings.Contains(publisherErr.Error(), "sensitive") {
		t.Fatalf("delegate/publisher errors = %#v/%#v", delegateErr, publisherErr)
	}
}

func TestBatchFailureHandlerRejectsInvalidDeliveryResultSets(t *testing.T) {

	tests := []struct {
		name    string
		results []DeliveryResult
		want    error
	}{
		{name: "missing", results: []DeliveryResult{{Topic: "events.retry.v1"}}, want: ErrDeliveryResultMissing},
		{name: "extra", results: []DeliveryResult{{}, {}, {}}, want: ErrDeliveryResultInvalid},
		{name: "wrong topic", results: []DeliveryResult{{Topic: "wrong"}, {Topic: "events.retry.v1"}}, want: ErrDeliveryResultInvalid},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {

			handler, err := NewBatchFailureHandler(BatchFailureHandlerConfig{
				Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
					return errors.New("failed")
				}),
				Mode:   FailureModeRetryTopic,
				Target: FailureTarget{Topic: "events.retry.v1", Version: 1},
				Publisher: BatchFailurePublisherFunc(func(
					context.Context,
					[]ProducerRecord,
				) ([]DeliveryResult, error) {
					return test.results, nil
				}),
			})
			if err != nil {
				t.Fatalf("NewBatchFailureHandler() error = %v", err)
			}

			got := handler.HandleBatch(context.Background(), testFailureBatch())

			if !errors.Is(got, ErrFailurePublish) ||
				!errors.Is(got, ErrBatchDeliveryFailed) ||
				!errors.Is(got, test.want) {
				t.Fatalf("HandleBatch() error = %#v", got)
			}
		})
	}
}

func TestBatchFailureDelegateControlsWholeBatchSettlement(t *testing.T) {

	delegateErr := errors.New("delegate failed")
	for _, test := range []struct {
		name          string
		delegateError error
		wantCommitted int
		wantError     error
	}{
		{name: "resolved", wantCommitted: 2},
		{name: "unresolved", delegateError: delegateErr, wantError: delegateErr},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {

			last := &kgo.Record{Topic: "events", Partition: 0, Offset: 8}
			backend := &recordingConsumerBackend{fetches: recordFetches(
				&kgo.Record{Topic: "events", Partition: 0, Offset: 7},
				last,
			)}
			consumer := consumerWithBackend(backend, 10, time.Second, time.Second)
			var delegated BatchFailure
			handler, err := NewBatchFailureHandler(BatchFailureHandlerConfig{
				Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
					return errors.New("handler failed")
				}),
				Mode: FailureModeDelegate,
				Delegate: BatchFailureDelegateFunc(func(
					_ context.Context,
					failure BatchFailure,
				) error {
					delegated = failure.Retain()

					return test.delegateError
				}),
			})
			if err != nil {
				t.Fatalf("NewBatchFailureHandler() error = %v", err)
			}

			result, runErr := consumer.RunBatchOnce(context.Background(), handler)

			if !errors.Is(runErr, test.wantError) ||
				result.Committed != test.wantCommitted ||
				len(delegated.Batch.Records) != 2 ||
				delegated.Attempt != 1 ||
				delegated.Category != ErrorPermanent {
				t.Fatalf("result/error/failure = %#v/%v/%#v", result, runErr, delegated)
			}
			if test.wantCommitted == 2 &&
				(len(backend.committed) != 1 || backend.committed[0] != last) {
				t.Fatalf("committed = %#v", backend.committed)
			}
			if test.wantCommitted == 0 && len(backend.committed) != 0 {
				t.Fatalf("committed unresolved batch = %#v", backend.committed)
			}
		})
	}
}

func testFailureBatch() ConsumedBatch {
	return ConsumedBatch{
		Topic:     "events",
		Partition: 3,
		Records: []ConsumedRecord{
			{
				Topic: "events", Partition: 3, Offset: 7,
				Key: []byte("key-1"), Value: []byte("first"),
				Headers: []Header{{Key: "trace", Value: []byte("trace")}},
			},
			{
				Topic: "events", Partition: 3, Offset: 8,
				Key: []byte("key-2"), Value: []byte("second"),
			},
		},
	}
}

func failureBatchInputBytes(batch ConsumedBatch) int64 {
	var total int64
	for _, record := range batch.Records {
		total += recordSize(ProducerRecord{
			Topic: record.Topic, Key: record.Key, Value: record.Value,
			Headers: record.Headers,
		})
	}

	return total
}

func successfulBatchFailurePublisher() BatchFailurePublisher {
	return BatchFailurePublisherFunc(func(
		_ context.Context,
		records []ProducerRecord,
	) ([]DeliveryResult, error) {
		results := make([]DeliveryResult, len(records))
		for index, record := range records {
			results[index].Topic = record.Topic
		}

		return results, nil
	})
}

func failureHeaderValue(record ProducerRecord, name string) string {
	for _, header := range record.Headers {
		if header.Key == "golib.kafka.failure."+name {
			return string(header.Value)
		}
	}

	return ""
}

func TestBatchFailureHandlerPublishedRecordsRemainInputOrdered(t *testing.T) {

	var offsets []string
	handler, err := NewBatchFailureHandler(BatchFailureHandlerConfig{
		Handler: BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			return errors.New("failed")
		}),
		Mode:   FailureModeDeadLetter,
		Target: FailureTarget{Topic: "events.dead-letter.v1", Version: 1},
		Publisher: BatchFailurePublisherFunc(func(
			_ context.Context,
			records []ProducerRecord,
		) ([]DeliveryResult, error) {
			for _, record := range records {
				offsets = append(offsets, failureHeaderValue(record, "source-offset"))
			}
			for index := range records {
				records[index].Topic = "publisher-owned"
				records[index].Value = nil
			}

			return []DeliveryResult{
				{Topic: "events.dead-letter.v1"},
				{Topic: "events.dead-letter.v1"},
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewBatchFailureHandler() error = %v", err)
	}

	if err := handler.HandleBatch(context.Background(), testFailureBatch()); err != nil {
		t.Fatalf("HandleBatch() error = %v", err)
	}
	if !reflect.DeepEqual(offsets, []string{"7", "8"}) {
		t.Fatalf("published source offsets = %#v", offsets)
	}
}
