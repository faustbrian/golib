package gokafka

import (
	"context"
	"errors"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

func TestRecordHandlerSettlesOnlyAfterFailurePolicyHandlesRecord(
	t *testing.T,
) {
	t.Parallel()

	codec := testRecordCodec(t)
	record := consumedRecord(encodedLiveRecord(t, codec, testMessage(t)))
	record.Partition = 2
	record.Offset = 29
	consumerFailure := errors.New("projection failed")
	var policyRecord kafka.ConsumedMessage
	var policyCause error
	handler, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return consumerFailure
		}),
		WithFailurePolicy(FailurePolicyFunc(func(
			_ context.Context,
			failed kafka.ConsumedMessage,
			cause error,
		) (FailureDisposition, error) {
			policyRecord = failed
			policyCause = cause

			return FailureHandled, nil
		})),
	)
	if err != nil {
		t.Fatalf("construct handler: %v", err)
	}

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle policy-settled record: %v", err)
	}
	if policyRecord.Topic != record.Topic ||
		policyRecord.Partition != 2 ||
		policyRecord.Offset != 29 ||
		!errors.Is(policyCause, consumerFailure) {
		t.Fatalf(
			"policy input = %s/%d/%d cause=%v",
			policyRecord.Topic,
			policyRecord.Partition,
			policyRecord.Offset,
			policyCause,
		)
	}
}

func TestRecordHandlerFailurePolicyControlsDecodeAndConsumerRetry(
	t *testing.T,
) {
	t.Parallel()

	codec := testRecordCodec(t)
	good := consumedRecord(encodedLiveRecord(t, codec, testMessage(t)))
	corrupt := good
	corrupt.Value = nil
	policyCalls := 0
	retrying, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return errors.New("consumer failure")
		}),
		WithFailurePolicy(FailurePolicyFunc(func(
			_ context.Context,
			_ kafka.ConsumedMessage,
			cause error,
		) (FailureDisposition, error) {
			policyCalls++
			if !errors.Is(cause, ErrRecordCorrupt) {
				t.Fatalf("decode cause = %v", cause)
			}

			return FailureRetry, nil
		})),
	)
	if err != nil {
		t.Fatalf("construct retrying handler: %v", err)
	}
	err = retrying.Handle(context.Background(), corrupt)
	assertHandlerError(
		t,
		err,
		ErrRecordCorrupt,
		corrupt.Topic,
		corrupt.Partition,
		corrupt.Offset,
	)
	if policyCalls != 1 {
		t.Fatalf("policy calls = %d", policyCalls)
	}

	handled, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			t.Fatal("consumer called for corrupt record")

			return nil
		}),
		WithFailurePolicy(FailurePolicyFunc(func(
			context.Context,
			kafka.ConsumedMessage,
			error,
		) (FailureDisposition, error) {
			return FailureHandled, nil
		})),
	)
	if err != nil {
		t.Fatalf("construct handled policy: %v", err)
	}
	if err := handled.Handle(context.Background(), corrupt); err != nil {
		t.Fatalf("handle quarantined corrupt record: %v", err)
	}
}

func TestRecordHandlerFailurePolicyFailsClosed(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	record := consumedRecord(encodedLiveRecord(t, codec, testMessage(t)))
	consumerFailure := errors.New("consumer credential=secret")
	policyFailure := errors.New("dead-letter credential=secret")

	tests := map[string]struct {
		policy FailurePolicy
		target error
	}{
		"policy error": {
			policy: FailurePolicyFunc(func(
				context.Context,
				kafka.ConsumedMessage,
				error,
			) (FailureDisposition, error) {
				return 0, policyFailure
			}),
			target: policyFailure,
		},
		"policy panic": {
			policy: FailurePolicyFunc(func(
				context.Context,
				kafka.ConsumedMessage,
				error,
			) (FailureDisposition, error) {
				panic("credential=secret")
			}),
			target: ErrFailurePolicyPanic,
		},
		"invalid disposition": {
			policy: FailurePolicyFunc(func(
				context.Context,
				kafka.ConsumedMessage,
				error,
			) (FailureDisposition, error) {
				return 99, nil
			}),
			target: ErrInvalidFailureDisposition,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			handler, err := NewRecordHandler(
				codec,
				DeliveryConsumerFunc(func(
					context.Context,
					eventsourcing.Delivery,
				) error {
					return consumerFailure
				}),
				WithFailurePolicy(test.policy),
			)
			if err != nil {
				t.Fatalf("construct handler: %v", err)
			}
			err = handler.Handle(context.Background(), record)
			assertHandlerError(
				t,
				err,
				test.target,
				record.Topic,
				record.Partition,
				record.Offset,
			)
			if !errors.Is(err, consumerFailure) {
				t.Fatalf("error = %v, want consumer failure", err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("failure diagnostic was disclosed: %v", err)
			}
		})
	}
}

func TestRecordHandlerFailurePolicyCannotSettleCancellation(
	t *testing.T,
) {
	t.Parallel()

	codec := testRecordCodec(t)
	record := consumedRecord(encodedLiveRecord(t, codec, testMessage(t)))
	ctx, cancel := context.WithCancel(context.Background())
	policyCalls := 0
	handler, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			cancel()

			return errors.New("consumer failed")
		}),
		WithFailurePolicy(FailurePolicyFunc(func(
			context.Context,
			kafka.ConsumedMessage,
			error,
		) (FailureDisposition, error) {
			policyCalls++

			return FailureHandled, nil
		})),
	)
	if err != nil {
		t.Fatalf("construct handler: %v", err)
	}
	err = handler.Handle(ctx, record)
	assertHandlerError(
		t,
		err,
		context.Canceled,
		record.Topic,
		record.Partition,
		record.Offset,
	)
	if policyCalls != 0 {
		t.Fatalf("policy calls after cancellation = %d", policyCalls)
	}

	ctx, cancel = context.WithCancel(context.Background())
	handler, err = NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return errors.New("consumer failed")
		}),
		WithFailurePolicy(FailurePolicyFunc(func(
			context.Context,
			kafka.ConsumedMessage,
			error,
		) (FailureDisposition, error) {
			policyCalls++
			cancel()

			return FailureHandled, nil
		})),
	)
	if err != nil {
		t.Fatalf("construct policy-cancel handler: %v", err)
	}
	err = handler.Handle(ctx, record)
	assertHandlerError(
		t,
		err,
		context.Canceled,
		record.Topic,
		record.Partition,
		record.Offset,
	)
	if policyCalls != 1 {
		t.Fatalf("policy calls before cancellation = %d", policyCalls)
	}
}

func TestNewRecordHandlerValidatesDependenciesAndOptions(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	consumer := DeliveryConsumerFunc(func(
		context.Context,
		eventsourcing.Delivery,
	) error {
		return nil
	})
	tests := map[string]struct {
		codec    *RecordCodec
		consumer DeliveryConsumer
		options  []RecordHandlerOption
		target   error
	}{
		"codec": {
			consumer: consumer,
			target:   ErrCodecRequired,
		},
		"consumer": {
			codec:  codec,
			target: ErrConsumerRequired,
		},
		"nil option": {
			codec:    codec,
			consumer: consumer,
			options:  []RecordHandlerOption{nil},
			target:   ErrInvalidHandlerOption,
		},
		"duplicate replay": {
			codec:    codec,
			consumer: consumer,
			options: []RecordHandlerOption{
				AllowReplayHandling(),
				AllowReplayHandling(),
			},
			target: ErrInvalidHandlerOption,
		},
		"nil failure policy": {
			codec:    codec,
			consumer: consumer,
			options: []RecordHandlerOption{
				WithFailurePolicy(nil),
			},
			target: ErrInvalidHandlerOption,
		},
		"duplicate failure policy": {
			codec:    codec,
			consumer: consumer,
			options: []RecordHandlerOption{
				WithFailurePolicy(retryFailurePolicy()),
				WithFailurePolicy(retryFailurePolicy()),
			},
			target: ErrInvalidHandlerOption,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewRecordHandler(
				test.codec,
				test.consumer,
				test.options...,
			)
			if !errors.Is(err, test.target) {
				t.Fatalf("error = %v, want %v", err, test.target)
			}
		})
	}

	var nilConsumer DeliveryConsumerFunc
	if err := nilConsumer.Consume(
		context.Background(),
		eventsourcing.Delivery{},
	); !errors.Is(err, ErrConsumerRequired) {
		t.Fatalf("nil function error = %v", err)
	}
	var nilPolicy FailurePolicyFunc
	if _, err := nilPolicy.HandleFailure(
		context.Background(),
		kafka.ConsumedMessage{},
		errors.New("failure"),
	); !errors.Is(err, ErrFailurePolicyRequired) {
		t.Fatalf("nil policy error = %v", err)
	}
}

func TestRecordHandlerRejectsInvalidReceiverAndContext(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	record := consumedRecord(encodedLiveRecord(t, codec, testMessage(t)))
	handler, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("construct handler: %v", err)
	}
	var nilContext context.Context
	if err := handler.Handle(nilContext, record); !errors.Is(
		err,
		ErrContextRequired,
	) {
		t.Fatalf("nil context error = %v", err)
	}
	if err := (*RecordHandler)(nil).Handle(
		context.Background(),
		record,
	); !errors.Is(err, ErrConsumerRequired) {
		t.Fatalf("nil handler error = %v", err)
	}
}

func TestRecordHandlerRejectsInvalidKafkaPosition(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	handler, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			t.Fatal("consumer called for invalid Kafka position")

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("construct handler: %v", err)
	}
	for name, mutate := range map[string]func(*kafka.ConsumedMessage){
		"partition": func(record *kafka.ConsumedMessage) {
			record.Partition = -1
		},
		"offset": func(record *kafka.ConsumedMessage) {
			record.Offset = -1
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			record := consumedRecord(
				encodedLiveRecord(t, codec, testMessage(t)),
			)
			mutate(&record)
			err := handler.Handle(context.Background(), record)
			assertHandlerError(
				t,
				err,
				ErrInvalidKafkaPosition,
				record.Topic,
				record.Partition,
				record.Offset,
			)
		})
	}
}

func TestRecordHandlerReturnsRedactedPositionedFailures(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	record := consumedRecord(encodedLiveRecord(t, codec, testMessage(t)))
	record.Partition = 3
	record.Offset = 17

	corrupt := record
	corrupt.Value = nil
	handler, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			t.Fatal("consumer called for corrupt record")

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("construct corrupt handler: %v", err)
	}
	err = handler.Handle(context.Background(), corrupt)
	assertHandlerError(t, err, ErrRecordCorrupt, record.Topic, 3, 17)

	sensitive := errors.New("credential=secret")
	failing, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return sensitive
		}),
	)
	if err != nil {
		t.Fatalf("construct failing handler: %v", err)
	}
	err = failing.Handle(context.Background(), record)
	assertHandlerError(t, err, sensitive, record.Topic, 3, 17)
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("consumer diagnostic was disclosed: %v", err)
	}

	panicking, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			panic("credential=secret")
		}),
	)
	if err != nil {
		t.Fatalf("construct panicking handler: %v", err)
	}
	err = panicking.Handle(context.Background(), record)
	assertHandlerError(t, err, ErrConsumerPanic, record.Topic, 3, 17)
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("panic value was disclosed: %v", err)
	}
}

func TestRecordHandlerFailsClosedOnCancellation(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	record := consumedRecord(encodedLiveRecord(t, codec, testMessage(t)))
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	handler, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			calls++

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("construct handler: %v", err)
	}
	if err := handler.Handle(ctx, record); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("pre-cancelled error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("consumer calls = %d", calls)
	}

	ctx, cancel = context.WithCancel(context.Background())
	late, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			cancel()

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("construct late-cancel handler: %v", err)
	}
	err = late.Handle(ctx, record)
	assertHandlerError(
		t,
		err,
		context.Canceled,
		record.Topic,
		record.Partition,
		record.Offset,
	)
}

func TestRecordHandlerRejectsReplayUnlessExplicitlyEnabled(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	record := consumedRecord(testEncodedRecord(t, codec))
	calls := 0
	policyCalls := 0
	consumer := DeliveryConsumerFunc(func(
		context.Context,
		eventsourcing.Delivery,
	) error {
		calls++

		return nil
	})
	rejected, err := NewRecordHandler(
		codec,
		consumer,
		WithFailurePolicy(FailurePolicyFunc(func(
			context.Context,
			kafka.ConsumedMessage,
			error,
		) (FailureDisposition, error) {
			policyCalls++

			return FailureHandled, nil
		})),
	)
	if err != nil {
		t.Fatalf("construct rejecting handler: %v", err)
	}
	if err := rejected.Handle(
		context.Background(),
		record,
	); !errors.Is(err, ErrReplayHandlingDenied) {
		t.Fatalf("replay error = %v, want ErrReplayHandlingDenied", err)
	}
	if calls != 0 {
		t.Fatalf("consumer calls after rejection = %d", calls)
	}
	if policyCalls != 0 {
		t.Fatalf("policy calls after replay rejection = %d", policyCalls)
	}

	allowed, err := NewRecordHandler(
		codec,
		consumer,
		AllowReplayHandling(),
	)
	if err != nil {
		t.Fatalf("construct replay handler: %v", err)
	}
	if err := allowed.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle replay: %v", err)
	}
	if calls != 1 {
		t.Fatalf("consumer calls = %d", calls)
	}
}

func TestRecordHandlerDecodesAndHandlesLiveDelivery(t *testing.T) {
	t.Parallel()

	codec := testRecordCodec(t)
	message := testMessage(t)
	record := encodedLiveRecord(t, codec, message)
	var handled eventsourcing.Delivery
	handler, err := NewRecordHandler(
		codec,
		DeliveryConsumerFunc(func(
			_ context.Context,
			delivery eventsourcing.Delivery,
		) error {
			handled = delivery

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("construct record handler: %v", err)
	}

	if err := handler.Handle(
		context.Background(),
		consumedRecord(record),
	); err != nil {
		t.Fatalf("handle record: %v", err)
	}
	if handled.Mode() != eventsourcing.DeliveryLive ||
		!handled.Message().Equal(message) {
		t.Fatalf("handled delivery = %#v", handled)
	}
}

func encodedLiveRecord(
	t testing.TB,
	codec *RecordCodec,
	message eventsourcing.Message,
) kafka.Message {
	t.Helper()

	delivery, err := eventsourcing.NewDelivery(
		message,
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatalf("construct live delivery: %v", err)
	}
	record, err := codec.Encode(delivery)
	if err != nil {
		t.Fatalf("encode live delivery: %v", err)
	}

	return record
}

func assertHandlerError(
	t testing.TB,
	err error,
	cause error,
	topic string,
	partition int32,
	offset int64,
) {
	t.Helper()

	if !errors.Is(err, ErrRecordHandlingFailed) ||
		!errors.Is(err, cause) {
		t.Fatalf("error = %v, want handling failure and %v", err, cause)
	}
	var handlerError *HandlerError
	if !errors.As(err, &handlerError) {
		t.Fatalf("error type = %T, want *HandlerError", err)
	}
	if handlerError.Topic() != topic ||
		handlerError.Partition() != partition ||
		handlerError.Offset() != offset {
		t.Fatalf(
			"position = %s/%d/%d",
			handlerError.Topic(),
			handlerError.Partition(),
			handlerError.Offset(),
		)
	}
}

func retryFailurePolicy() FailurePolicy {
	return FailurePolicyFunc(func(
		context.Context,
		kafka.ConsumedMessage,
		error,
	) (FailureDisposition, error) {
		return FailureRetry, nil
	})
}
