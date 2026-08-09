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

func TestNewFailureHandlerValidatesPolicyBeforeUse(t *testing.T) {

	handler := HandlerFunc(func(context.Context, ConsumedMessage) error { return nil })
	publisher := &recordingFailurePublisher{}
	delegate := FailureDelegateFunc(func(context.Context, HandlerFailure) error { return nil })
	tests := []struct {
		name   string
		change func(*FailureHandlerConfig)
		want   error
	}{
		{
			name:   "handler required",
			change: func(config *FailureHandlerConfig) { config.Handler = nil },
			want:   ErrHandlerRequired,
		},
		{
			name: "invalid mode",
			change: func(config *FailureHandlerConfig) {
				config.Mode = FailureMode(255)
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "too many attempts",
			change: func(config *FailureHandlerConfig) {
				config.Retry.MaxAttempts = 33
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "retry requires backoff",
			change: func(config *FailureHandlerConfig) {
				config.Retry = FailureRetryPolicy{MaxAttempts: 2}
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "backoff requires retry",
			change: func(config *FailureHandlerConfig) {
				config.Retry = FailureRetryPolicy{
					MaxAttempts:    1,
					InitialBackoff: time.Millisecond,
					MaxBackoff:     time.Millisecond,
				}
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "backoff order",
			change: func(config *FailureHandlerConfig) {
				config.Retry = FailureRetryPolicy{
					MaxAttempts:    2,
					InitialBackoff: 2 * time.Millisecond,
					MaxBackoff:     time.Millisecond,
				}
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "duplicate retry category",
			change: func(config *FailureHandlerConfig) {
				config.Retry.Categories = []ErrorCategory{
					ErrorRetryable,
					ErrorRetryable,
				}
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "invalid retry category",
			change: func(config *FailureHandlerConfig) {
				config.Retry.Categories = []ErrorCategory{ErrorCategory(255)}
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "retry topic requires publisher",
			change: func(config *FailureHandlerConfig) {
				config.Mode = FailureModeRetryTopic
				config.Target = FailureTarget{Topic: "events.retry.v2", Version: 2}
				config.Publisher = nil
			},
			want: ErrFailurePublisherRequired,
		},
		{
			name: "dead letter requires publisher",
			change: func(config *FailureHandlerConfig) {
				config.Mode = FailureModeDeadLetter
				config.Target = FailureTarget{Topic: "events.dead-letter.v2", Version: 2}
				config.Publisher = nil
			},
			want: ErrFailurePublisherRequired,
		},
		{
			name: "invalid target topic",
			change: func(config *FailureHandlerConfig) {
				config.Mode = FailureModeRetryTopic
				config.Target = FailureTarget{Topic: "not valid", Version: 2}
				config.Publisher = publisher
			},
			want: ErrInvalidFailureTarget,
		},
		{
			name: "target version required",
			change: func(config *FailureHandlerConfig) {
				config.Mode = FailureModeDeadLetter
				config.Target = FailureTarget{Topic: "events.dead-letter.v2"}
				config.Publisher = publisher
			},
			want: ErrInvalidFailureTarget,
		},
		{
			name: "delegate required",
			change: func(config *FailureHandlerConfig) {
				config.Mode = FailureModeDelegate
				config.Delegate = nil
			},
			want: ErrFailureDelegateRequired,
		},
		{
			name: "delegate forbidden for stop",
			change: func(config *FailureHandlerConfig) {
				config.Delegate = delegate
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "publisher forbidden for stop",
			change: func(config *FailureHandlerConfig) {
				config.Publisher = publisher
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "publish timeout too small",
			change: func(config *FailureHandlerConfig) {
				config.Mode = FailureModeRetryTopic
				config.Target = FailureTarget{Topic: "events.retry.v2", Version: 2}
				config.Publisher = publisher
				config.PublishTimeout = time.Millisecond
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "delegate forbidden for publish",
			change: func(config *FailureHandlerConfig) {
				config.Mode = FailureModeRetryTopic
				config.Target = FailureTarget{Topic: "events.retry.v2", Version: 2}
				config.Publisher = publisher
				config.Delegate = delegate
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "publish fields forbidden for delegate",
			change: func(config *FailureHandlerConfig) {
				config.Mode = FailureModeDelegate
				config.Delegate = delegate
				config.PublishTimeout = time.Second
			},
			want: ErrInvalidFailurePolicy,
		},
		{
			name: "invalid limits",
			change: func(config *FailureHandlerConfig) {
				config.Limits.MaxHeaders = -1
			},
			want: ErrInvalidMessageLimits,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {

			config := FailureHandlerConfig{
				Handler: handler,
				Retry: FailureRetryPolicy{
					MaxAttempts:    3,
					InitialBackoff: time.Millisecond,
					MaxBackoff:     4 * time.Millisecond,
					Categories:     []ErrorCategory{ErrorRetryable},
				},
			}
			test.change(&config)

			got, err := NewFailureHandler(config)

			if got != nil || !errors.Is(err, test.want) {
				t.Fatalf("NewFailureHandler() = %#v, %v, want nil, %v", got, err, test.want)
			}
		})
	}
}

func TestFailureHandlerPolicyAcceptsInclusiveBoundaries(t *testing.T) {

	handler := HandlerFunc(func(context.Context, ConsumedMessage) error {
		return errors.New("failed")
	})
	publisher := &recordingFailurePublisher{}
	for _, timeout := range []time.Duration{100 * time.Millisecond, 2 * time.Minute} {
		config := FailureHandlerConfig{
			Handler:        handler,
			Mode:           FailureModeRetryTopic,
			Target:         FailureTarget{Topic: "events.retry.v1", Version: 1},
			Publisher:      publisher,
			PublishTimeout: timeout,
		}
		if err := config.Validate(); err != nil {
			t.Fatalf("publish timeout %s Validate() error = %v", timeout, err)
		}
	}

	retry, err := normalizeFailureRetryPolicy(FailureRetryPolicy{
		MaxAttempts:    maximumFailureAttempts,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     maximumFailureBackoff,
		Categories:     []ErrorCategory{ErrorPermanent, ErrorFatal},
	})
	if err != nil {
		t.Fatalf("maximum retry policy error = %v", err)
	}
	if retry.MaxAttempts != maximumFailureAttempts ||
		retry.MaxBackoff != maximumFailureBackoff ||
		!reflect.DeepEqual(
			retry.Categories,
			[]ErrorCategory{ErrorPermanent, ErrorFatal},
		) {
		t.Fatalf("maximum retry policy = %#v", retry)
	}
}

func TestFailureHandlerConfigValidateAndDefaultRetryCategory(t *testing.T) {

	handlerErr := errors.New("retryable application failure")
	attempts := 0
	categories := []ErrorCategory(nil)
	config := FailureHandlerConfig{
		Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
			attempts++
			if attempts == 1 {
				return handlerErr
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
			Categories:     categories,
		},
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	handler, err := newFailureHandler(
		config,
		func(context.Context, time.Duration) error { return nil },
	)
	if err != nil {
		t.Fatalf("newFailureHandler() error = %v", err)
	}
	config.Retry.Categories = []ErrorCategory{ErrorPermanent}

	if err := handler.Handle(
		context.Background(),
		ConsumedMessage{Topic: "events"},
	); err != nil ||
		attempts != 2 {
		t.Fatalf("Handle() error/attempts = %v/%d", err, attempts)
	}
}

func TestFailureHandlerRetriesBoundedlyWithCappedBackoff(t *testing.T) {

	handlerErr := errors.New("sensitive handler detail")
	attempts := 0
	var waits []time.Duration
	handler, err := newFailureHandler(
		FailureHandlerConfig{
			Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
				attempts++
				if attempts < 4 {
					return handlerErr
				}

				return nil
			}),
			Classifier: FailureClassifierFunc(func(error) ErrorCategory {
				return ErrorRetryable
			}),
			Retry: FailureRetryPolicy{
				MaxAttempts:    4,
				InitialBackoff: time.Second,
				MaxBackoff:     2 * time.Second,
				Categories:     []ErrorCategory{ErrorRetryable},
			},
		},
		func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)

			return nil
		},
	)
	if err != nil {
		t.Fatalf("newFailureHandler() error = %v", err)
	}

	err = handler.Handle(context.Background(), ConsumedMessage{
		Topic: "events", Partition: 1, Offset: 42,
	})

	if err != nil || attempts != 4 ||
		!reflect.DeepEqual(waits, []time.Duration{
			time.Second,
			2 * time.Second,
			2 * time.Second,
		}) {
		t.Fatalf("Handle() error/attempts/waits = %v/%d/%v", err, attempts, waits)
	}
}

func TestFailureHandlerStopsWithoutRetryingUnselectedCategory(t *testing.T) {

	handlerErr := errors.New("payload=do-not-render")
	handler, err := NewFailureHandler(FailureHandlerConfig{
		Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
			return handlerErr
		}),
		Retry: FailureRetryPolicy{
			MaxAttempts:    3,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
			Categories:     []ErrorCategory{ErrorRetryable},
		},
	})
	if err != nil {
		t.Fatalf("NewFailureHandler() error = %v", err)
	}

	got := handler.Handle(context.Background(), ConsumedMessage{
		Topic: "events", Partition: 2, Offset: 7,
	})

	var failureErr *FailureHandlingError
	if !errors.Is(got, ErrConsumerFailureStopped) ||
		!errors.Is(got, handlerErr) ||
		!errors.As(got, &failureErr) ||
		failureErr.Attempt() != 1 ||
		failureErr.Category() != ErrorPermanent ||
		failureErr.Stage() != FailureStageStop ||
		strings.Contains(got.Error(), "do-not-render") {
		t.Fatalf("Handle() error = %#v (%v)", got, got)
	}
}

func TestFailureHandlerPublishesOwnedDeadLetterMetadata(t *testing.T) {

	handlerErr := errors.New("database unavailable")
	publisher := &recordingFailurePublisher{}
	handler, err := NewFailureHandler(FailureHandlerConfig{
		Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
			return handlerErr
		}),
		Mode:      FailureModeDeadLetter,
		Target:    FailureTarget{Topic: "events.dead-letter.v3", Version: 3},
		Publisher: publisher,
		Classifier: FailureClassifierFunc(func(error) ErrorCategory {
			return ErrorRetryable
		}),
	})
	if err != nil {
		t.Fatalf("NewFailureHandler() error = %v", err)
	}
	message := ConsumedMessage{
		Topic:         "events.v1",
		Partition:     2,
		Offset:        81,
		Key:           []byte("key"),
		Value:         []byte("value"),
		Headers:       []Header{{Key: "correlation-id", Value: []byte("abc")}},
		Timestamp:     time.Date(2026, 7, 26, 10, 11, 12, 13, time.UTC),
		TimestampType: TimestampLogAppendTime,
		LeaderEpoch:   9,
	}

	if err := handler.Handle(context.Background(), message); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	message.Key[0] = 'X'
	message.Value[0] = 'X'
	message.Headers[0].Value[0] = 'X'

	if publisher.calls != 1 || publisher.timeout <= 0 ||
		publisher.record.Topic != "events.dead-letter.v3" ||
		string(publisher.record.Key) != "key" ||
		string(publisher.record.Value) != "value" ||
		!publisher.record.Timestamp.Equal(message.Timestamp) ||
		len(publisher.record.Headers) != 12 ||
		publisher.record.Headers[0].Key != "correlation-id" ||
		string(publisher.record.Headers[0].Value) != "abc" {
		t.Fatalf("publisher state = %#v", publisher)
	}
	wantMetadata := []Header{
		{Key: "golib.kafka.failure.schema-version", Value: []byte("1")},
		{Key: "golib.kafka.failure.kind", Value: []byte("dead-letter")},
		{Key: "golib.kafka.failure.target-version", Value: []byte("3")},
		{Key: "golib.kafka.failure.source-topic", Value: []byte("events.v1")},
		{Key: "golib.kafka.failure.source-partition", Value: []byte("2")},
		{Key: "golib.kafka.failure.source-offset", Value: []byte("81")},
		{Key: "golib.kafka.failure.source-timestamp", Value: []byte("2026-07-26T10:11:12.000000013Z")},
		{Key: "golib.kafka.failure.source-timestamp-type", Value: []byte("log-append-time")},
		{Key: "golib.kafka.failure.source-leader-epoch", Value: []byte("9")},
		{Key: "golib.kafka.failure.attempt", Value: []byte("1")},
		{Key: "golib.kafka.failure.error-category", Value: []byte("retryable")},
	}
	if !reflect.DeepEqual(publisher.record.Headers[1:], wantMetadata) {
		t.Fatalf("metadata headers = %#v, want %#v", publisher.record.Headers[1:], wantMetadata)
	}
}

func TestFailureHandlerPreservesFetchedBytesAcrossAttempts(t *testing.T) {

	handlerErr := errors.New("retryable")
	attempts := 0
	publisher := &recordingFailurePublisher{}
	handler, err := newFailureHandler(
		FailureHandlerConfig{
			Handler: HandlerFunc(func(
				_ context.Context,
				message ConsumedMessage,
			) error {
				attempts++
				if string(message.Key) != "key" ||
					string(message.Value) != "value" ||
					string(message.Headers[0].Value) != "header" {
					t.Errorf("attempt %d received mutated message %#v", attempts, message)
				}
				message.Key[0] = 'X'
				message.Value[0] = 'X'
				message.Headers[0].Value[0] = 'X'

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
			Mode: FailureModeDeadLetter,
			Target: FailureTarget{
				Topic: "events.dead-letter.v1", Version: 1,
			},
			Publisher: publisher,
		},
		func(context.Context, time.Duration) error { return nil },
	)
	if err != nil {
		t.Fatalf("newFailureHandler() error = %v", err)
	}

	err = handler.Handle(context.Background(), ConsumedMessage{
		Topic: "events",
		Key:   []byte("key"),
		Value: []byte("value"),
		Headers: []Header{{
			Key: "correlation", Value: []byte("header"),
		}},
	})

	if err != nil ||
		attempts != 2 ||
		string(publisher.record.Key) != "key" ||
		string(publisher.record.Value) != "value" ||
		string(publisher.record.Headers[0].Value) != "header" {
		t.Fatalf(
			"Handle() error/attempts/published = %v/%d/%#v",
			err,
			attempts,
			publisher.record,
		)
	}
}

func TestFailureHandlerRejectsUnsafeSourceBeforeHandlerAdmission(t *testing.T) {
	tests := []struct {
		name   string
		record ConsumedMessage
		limits MessageLimits
		want   error
	}{
		{
			name: "value limit",
			record: ConsumedMessage{
				Topic:     "events",
				Partition: 0,
				Offset:    1,
				Value:     []byte("too large"),
			},
			limits: func() MessageLimits {
				limits := DefaultMessageLimits()
				limits.MaxValueBytes = 1

				return limits
			}(),
			want: ErrValueTooLarge,
		},
		{
			name:   "invalid partition",
			record: ConsumedMessage{Topic: "events", Partition: -1, Offset: 1},
			limits: DefaultMessageLimits(),
		},
		{
			name:   "invalid offset",
			record: ConsumedMessage{Topic: "events", Partition: 0, Offset: -1},
			limits: DefaultMessageLimits(),
		},
		{
			name: "invalid timestamp type",
			record: ConsumedMessage{
				Topic:         "events",
				Partition:     0,
				Offset:        1,
				TimestampType: TimestampType(2),
			},
			limits: DefaultMessageLimits(),
		},
		{
			name: "invalid leader epoch",
			record: ConsumedMessage{
				Topic:       "events",
				Partition:   0,
				Offset:      1,
				LeaderEpoch: -2,
			},
			limits: DefaultMessageLimits(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handlerCalls := 0
			handler, err := NewFailureHandler(FailureHandlerConfig{
				Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
					handlerCalls++

					return nil
				}),
				Limits: test.limits,
			})
			if err != nil {
				t.Fatalf("NewFailureHandler() error = %v", err)
			}

			err = handler.Handle(context.Background(), test.record)

			if !errors.Is(err, ErrFailureRecordInvalid) ||
				(test.want != nil && !errors.Is(err, test.want)) ||
				handlerCalls != 0 {
				t.Fatalf("Handle() error/handler calls = %#v/%d", err, handlerCalls)
			}
		})
	}
}

func TestFailureHandlerAcceptsUnknownTimestampAndLeaderEpoch(t *testing.T) {
	called := false
	handler, err := NewFailureHandler(FailureHandlerConfig{
		Handler: HandlerFunc(func(_ context.Context, record ConsumedMessage) error {
			called = record.TimestampType == TimestampUnknown && record.LeaderEpoch == -1

			return nil
		}),
	})
	if err != nil {
		t.Fatalf("NewFailureHandler() error = %v", err)
	}

	err = handler.Handle(context.Background(), ConsumedMessage{
		Topic:         "events",
		Partition:     0,
		Offset:        0,
		TimestampType: TimestampUnknown,
		LeaderEpoch:   -1,
	})

	if err != nil || !called {
		t.Fatalf("Handle() error/called = %v/%t", err, called)
	}
}

func TestConsumerSettlesOnlyDefiniteFailurePublication(t *testing.T) {

	source := &kgo.Record{
		Topic: "events", Partition: 1, Offset: 8, Value: []byte("payload"),
	}
	tests := []struct {
		name          string
		publishErr    error
		wantProcessed int
		wantCommitted int
		wantErr       error
	}{
		{
			name:          "acknowledged dead letter",
			wantProcessed: 1,
			wantCommitted: 1,
		},
		{
			name:       "failed dead letter",
			publishErr: errors.New("delivery failed"),
			wantErr:    ErrFailurePublish,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {

			backend := &recordingConsumerBackend{
				fetches: recordFetches(source),
			}
			consumer := consumerWithBackend(
				backend,
				1,
				time.Second,
				time.Second,
			)
			publisher := &recordingFailurePublisher{err: test.publishErr}
			handler, err := NewFailureHandler(FailureHandlerConfig{
				Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
					return errors.New("projection failed")
				}),
				Mode:      FailureModeDeadLetter,
				Target:    FailureTarget{Topic: "events.dead-letter.v1", Version: 1},
				Publisher: publisher,
			})
			if err != nil {
				t.Fatalf("NewFailureHandler() error = %v", err)
			}

			result, runErr := consumer.RunOnce(context.Background(), handler)

			if !errors.Is(runErr, test.wantErr) ||
				result != (PollResult{
					Polled:    1,
					Processed: test.wantProcessed,
					Committed: test.wantCommitted,
				}) ||
				len(backend.committed) != test.wantCommitted ||
				backend.allowed != 1 ||
				publisher.calls != 1 {
				t.Fatalf(
					"RunOnce() result/error/backend/publisher = %#v/%v/%#v/%#v",
					result,
					runErr,
					backend,
					publisher,
				)
			}
		})
	}
}

func TestFailureHandlerPublishFailureDoesNotResolveSource(t *testing.T) {

	handlerErr := errors.New("handler failed")
	publishErr := errors.New("broker rejected")
	publisher := &recordingFailurePublisher{err: publishErr}
	handler, err := NewFailureHandler(FailureHandlerConfig{
		Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
			return handlerErr
		}),
		Mode:      FailureModeRetryTopic,
		Target:    FailureTarget{Topic: "events.retry.v1", Version: 1},
		Publisher: publisher,
	})
	if err != nil {
		t.Fatalf("NewFailureHandler() error = %v", err)
	}

	got := handler.Handle(context.Background(), ConsumedMessage{
		Topic: "events", Partition: 0, Offset: 3,
	})

	var failureErr *FailureHandlingError
	if !errors.Is(got, ErrFailurePublish) ||
		!errors.Is(got, handlerErr) ||
		!errors.Is(got, publishErr) ||
		!errors.As(got, &failureErr) ||
		failureErr.Stage() != FailureStagePublish ||
		strings.Contains(got.Error(), "handler failed") ||
		strings.Contains(got.Error(), "broker rejected") ||
		publisher.calls != 1 {
		t.Fatalf("Handle() error/publisher = %#v/%#v", got, publisher)
	}
}

func TestFailureHandlerDelegatesTerminalDecision(t *testing.T) {

	handlerErr := errors.New("handler failed")
	var failure HandlerFailure
	handler, err := NewFailureHandler(FailureHandlerConfig{
		Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
			return handlerErr
		}),
		Mode: FailureModeDelegate,
		Delegate: FailureDelegateFunc(func(
			_ context.Context,
			got HandlerFailure,
		) error {
			failure = got.Retain()

			return nil
		}),
	})
	if err != nil {
		t.Fatalf("NewFailureHandler() error = %v", err)
	}

	err = handler.Handle(context.Background(), ConsumedMessage{
		Topic: "events", Partition: 1, Offset: 12, Value: []byte("payload"),
	})

	if err != nil ||
		failure.Record.Topic != "events" ||
		failure.Record.Partition != 1 ||
		failure.Record.Offset != 12 ||
		string(failure.Record.Value) != "payload" ||
		failure.Attempt != 1 ||
		failure.Category != ErrorPermanent ||
		!errors.Is(failure.Cause(), handlerErr) {
		t.Fatalf("Handle() error/failure = %v/%#v", err, failure)
	}
}

func TestFailureHandlerBackoffHonorsCancellation(t *testing.T) {

	handlerErr := errors.New("retry me")
	handler, err := newFailureHandler(
		FailureHandlerConfig{
			Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
				return handlerErr
			}),
			Classifier: FailureClassifierFunc(func(error) ErrorCategory {
				return ErrorRetryable
			}),
			Retry: FailureRetryPolicy{
				MaxAttempts:    2,
				InitialBackoff: time.Second,
				MaxBackoff:     time.Second,
				Categories:     []ErrorCategory{ErrorRetryable},
			},
		},
		func(context.Context, time.Duration) error {
			return context.Canceled
		},
	)
	if err != nil {
		t.Fatalf("newFailureHandler() error = %v", err)
	}

	got := handler.Handle(context.Background(), ConsumedMessage{Topic: "events"})

	var failureErr *FailureHandlingError
	if !errors.Is(got, ErrFailureBackoff) ||
		!errors.Is(got, handlerErr) ||
		!errors.Is(got, context.Canceled) ||
		!errors.As(got, &failureErr) ||
		failureErr.Stage() != FailureStageBackoff {
		t.Fatalf("Handle() error = %#v", got)
	}
}

func TestFailureHandlerExhaustionIsExplicit(t *testing.T) {

	handlerErr := errors.New("retryable")
	handler, err := newFailureHandler(
		FailureHandlerConfig{
			Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
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
		},
		func(context.Context, time.Duration) error { return nil },
	)
	if err != nil {
		t.Fatalf("newFailureHandler() error = %v", err)
	}

	got := handler.Handle(context.Background(), ConsumedMessage{Topic: "events"})

	var failureErr *FailureHandlingError
	if !errors.Is(got, ErrConsumerFailureStopped) ||
		!errors.Is(got, ErrFailureAttemptsExhausted) ||
		!errors.Is(got, handlerErr) ||
		!errors.As(got, &failureErr) ||
		failureErr.Attempt() != 2 {
		t.Fatalf("Handle() error = %#v", got)
	}
}

func TestFailureHandlerCancellationSkipsTerminalAction(t *testing.T) {

	handlerErr := errors.New("handler stopped")
	publisher := &recordingFailurePublisher{}
	handler, err := NewFailureHandler(FailureHandlerConfig{
		Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
			return handlerErr
		}),
		Mode:      FailureModeDeadLetter,
		Target:    FailureTarget{Topic: "events.dead-letter.v1", Version: 1},
		Publisher: publisher,
	})
	if err != nil {
		t.Fatalf("NewFailureHandler() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := handler.Handle(ctx, ConsumedMessage{Topic: "events"})

	if !errors.Is(got, ErrConsumerFailureStopped) ||
		!errors.Is(got, handlerErr) ||
		!errors.Is(got, context.Canceled) ||
		publisher.calls != 0 {
		t.Fatalf("Handle() error/publisher calls = %#v/%d", got, publisher.calls)
	}
}

func TestFailureHandlerContainsClassifierFailures(t *testing.T) {

	handlerErr := errors.New("handler failed")
	tests := []struct {
		name       string
		classifier FailureClassifier
		want       error
	}{
		{
			name: "invalid category",
			classifier: FailureClassifierFunc(func(error) ErrorCategory {
				return 0
			}),
			want: ErrInvalidFailureClassification,
		},
		{
			name: "panic",
			classifier: FailureClassifierFunc(func(error) ErrorCategory {
				panic("secret classifier detail")
			}),
			want: ErrFailureCallbackPanic,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {

			handler, err := NewFailureHandler(FailureHandlerConfig{
				Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
					return handlerErr
				}),
				Classifier: test.classifier,
			})
			if err != nil {
				t.Fatalf("NewFailureHandler() error = %v", err)
			}

			got := handler.Handle(
				context.Background(),
				ConsumedMessage{Topic: "events"},
			)

			var failureErr *FailureHandlingError
			if !errors.Is(got, test.want) ||
				!errors.Is(got, handlerErr) ||
				!errors.As(got, &failureErr) ||
				failureErr.Stage() != FailureStageClassify ||
				strings.Contains(got.Error(), "secret") {
				t.Fatalf("Handle() error = %#v", got)
			}
		})
	}
}

func TestFailureHandlerContainsHandlerAndTerminalCallbackPanics(t *testing.T) {

	handler := HandlerFunc(func(context.Context, ConsumedMessage) error {
		panic("sensitive handler panic")
	})
	delegateHandler, err := NewFailureHandler(FailureHandlerConfig{
		Handler: handler,
		Mode:    FailureModeDelegate,
		Delegate: FailureDelegateFunc(func(context.Context, HandlerFailure) error {
			panic("sensitive delegate panic")
		}),
	})
	if err != nil {
		t.Fatalf("NewFailureHandler(delegate) error = %v", err)
	}
	publisherHandler, err := NewFailureHandler(FailureHandlerConfig{
		Handler: handler,
		Mode:    FailureModeRetryTopic,
		Target:  FailureTarget{Topic: "events.retry.v1", Version: 1},
		Publisher: failurePublisherFunc(func(
			context.Context,
			ProducerRecord,
		) DeliveryResult {
			panic("sensitive publisher panic")
		}),
	})
	if err != nil {
		t.Fatalf("NewFailureHandler(publisher) error = %v", err)
	}

	delegateErr := delegateHandler.Handle(
		context.Background(),
		ConsumedMessage{Topic: "events"},
	)
	publisherErr := publisherHandler.Handle(
		context.Background(),
		ConsumedMessage{Topic: "events"},
	)

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

func TestFailureHandlerRejectsUnsafePublishedRecord(t *testing.T) {

	handlerErr := errors.New("handler failed")
	tests := []struct {
		name    string
		message ConsumedMessage
		limits  MessageLimits
		want    error
	}{
		{
			name:    "source equals target",
			message: ConsumedMessage{Topic: "events.retry.v1"},
			limits:  DefaultMessageLimits(),
			want:    ErrInvalidFailureTarget,
		},
		{
			name:    "metadata exceeds header count",
			message: ConsumedMessage{Topic: "events"},
			limits: func() MessageLimits {
				limits := DefaultMessageLimits()
				limits.MaxHeaders = 10

				return limits
			}(),
			want: ErrFailureRecordInvalid,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {

			publisher := &recordingFailurePublisher{}
			handler, err := NewFailureHandler(FailureHandlerConfig{
				Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
					return handlerErr
				}),
				Mode:      FailureModeRetryTopic,
				Target:    FailureTarget{Topic: "events.retry.v1", Version: 1},
				Publisher: publisher,
				Limits:    test.limits,
			})
			if err != nil {
				t.Fatalf("NewFailureHandler() error = %v", err)
			}

			got := handler.Handle(context.Background(), test.message)

			if !errors.Is(got, ErrFailurePublish) ||
				!errors.Is(got, test.want) ||
				!errors.Is(got, handlerErr) ||
				publisher.calls != 0 {
				t.Fatalf("Handle() error/publisher calls = %#v/%d", got, publisher.calls)
			}
		})
	}
}

func TestFailureHandlerDelegateErrorRemainsUnsettled(t *testing.T) {

	handlerErr := errors.New("handler failed")
	delegateErr := errors.New("delegate failed")
	handler, err := NewFailureHandler(FailureHandlerConfig{
		Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
			return handlerErr
		}),
		Mode: FailureModeDelegate,
		Delegate: FailureDelegateFunc(func(context.Context, HandlerFailure) error {
			return delegateErr
		}),
	})
	if err != nil {
		t.Fatalf("NewFailureHandler() error = %v", err)
	}

	got := handler.Handle(context.Background(), ConsumedMessage{Topic: "events"})

	var failureErr *FailureHandlingError
	if !errors.Is(got, ErrFailureDelegate) ||
		!errors.Is(got, handlerErr) ||
		!errors.Is(got, delegateErr) ||
		!errors.As(got, &failureErr) ||
		failureErr.Stage() != FailureStageDelegate {
		t.Fatalf("Handle() error = %#v", got)
	}
}

func TestFailureHandlerContextAndInternalFailClosedPaths(t *testing.T) {

	handler, err := NewFailureHandler(FailureHandlerConfig{
		Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
			return errors.New("failed")
		}),
	})
	if err != nil {
		t.Fatalf("NewFailureHandler() error = %v", err)
	}
	var nilContext context.Context
	if got := handler.Handle(nilContext, ConsumedMessage{}); !errors.Is(got, ErrContextRequired) {
		t.Fatalf("Handle(nil) error = %v", got)
	}
	observerCtx := context.WithValue(
		context.Background(),
		observerContextKey{},
		true,
	)
	if got := handler.Handle(
		observerCtx,
		ConsumedMessage{Topic: "events"},
	); !errors.Is(got, ErrObserverReentry) {
		t.Fatalf("Handle(observer context) error = %v", got)
	}

	internal := handler.(*failureHandler)
	internal.mode = FailureMode(255)
	got := internal.Handle(context.Background(), ConsumedMessage{Topic: "events"})
	if !errors.Is(got, ErrInvalidFailurePolicy) {
		t.Fatalf("Handle(invalid internal mode) error = %v", got)
	}
}

func TestFailureHelpersCoverStableDiagnosticsAndTiming(t *testing.T) {

	if got := (FailureStage(0)).String(); got != "unknown" {
		t.Fatalf("FailureStage(0).String() = %q", got)
	}
	for stage, want := range map[FailureStage]string{
		FailureStageStop:     "stop",
		FailureStageClassify: "classify",
		FailureStageBackoff:  "backoff",
		FailureStagePublish:  "publish",
		FailureStageDelegate: "delegate",
	} {
		if got := stage.String(); got != want {
			t.Fatalf("%v.String() = %q, want %q", stage, got, want)
		}
	}
	var nilErr *FailureHandlingError
	if nilErr.Error() != "kafka: consumer failure handling failed" ||
		nilErr.Unwrap() != nil ||
		nilErr.Stage() != 0 ||
		nilErr.Category() != 0 ||
		nilErr.Attempt() != 0 ||
		nilErr.DeliveryResults() != nil ||
		(newFailureHandlingError(FailureStageStop, ErrorPermanent, 1)).DeliveryResults() != nil {
		t.Fatalf("nil FailureHandlingError methods are inconsistent")
	}

	cause := errors.New("cause")
	failureErr := newFailureHandlingError(
		FailureStageStop,
		ErrorPermanent,
		1,
		nil,
		cause,
	)
	unwrapped := failureErr.Unwrap()
	unwrapped[0] = nil
	if !errors.Is(failureErr, cause) {
		t.Fatal("Unwrap() exposed mutable internal causes")
	}

	if got := failureTimestampType(TimestampCreateTime); got != "create-time" {
		t.Fatalf("create-time string = %q", got)
	}
	if got := failureTimestampType(TimestampUnknown); got != "unknown" {
		t.Fatalf("unknown timestamp string = %q", got)
	}
	if got := failureBackoff(FailureRetryPolicy{
		InitialBackoff: 3,
		MaxBackoff:     5,
	}, 2); got != 5 {
		t.Fatalf("saturated backoff = %v, want 5", got)
	}
	for attempt, want := range map[int]time.Duration{
		1: 2,
		2: 4,
		3: 5,
		4: 5,
	} {
		if got := failureBackoff(FailureRetryPolicy{
			InitialBackoff: 2,
			MaxBackoff:     5,
		}, attempt); got != want {
			t.Fatalf("failureBackoff(attempt %d) = %v, want %v", attempt, got, want)
		}
	}

	if err := waitFailureBackoff(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("waitFailureBackoff(timer) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitFailureBackoff(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitFailureBackoff(canceled) error = %v", err)
	}
}

type recordingFailurePublisher struct {
	record  ProducerRecord
	err     error
	calls   int
	timeout time.Duration
}

type failurePublisherFunc func(context.Context, ProducerRecord) DeliveryResult

func (publisher failurePublisherFunc) PublishRecord(
	ctx context.Context,
	record ProducerRecord,
) DeliveryResult {
	return publisher(ctx, record)
}

func (publisher *recordingFailurePublisher) PublishRecord(
	ctx context.Context,
	record ProducerRecord,
) DeliveryResult {
	publisher.calls++
	publisher.record = record.owned()
	if deadline, ok := ctx.Deadline(); ok {
		publisher.timeout = time.Until(deadline)
	}

	return DeliveryResult{Topic: record.Topic, Err: publisher.err}
}
