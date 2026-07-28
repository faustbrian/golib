package kafkaservice_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/kafkaservice"
)

func FuzzHandlerCorrelationBoundary(fuzz *testing.F) {
	fuzz.Add("workflow", "producer-request", true, false)
	fuzz.Add("contains spaces", "producer-request", true, true)
	fuzz.Add("", "", false, false)

	fuzz.Fuzz(func(
		t *testing.T,
		correlationValue string,
		requestValue string,
		trusted bool,
		rejectInvalid bool,
	) {
		factory, err := correlation.NewFactory(correlation.FactoryOptions{})
		if err != nil {
			t.Fatalf("NewFactory() error = %v", err)
		}
		calls := 0
		var values correlation.Values
		handler, err := kafkaservice.NewHandler(kafkaservice.HandlerOptions{
			Correlation: factory, TrustedMetadata: trusted,
			RejectInvalidMetadata: rejectInvalid,
			Handler: kafka.HandlerFunc(func(
				ctx context.Context,
				_ kafka.ConsumedMessage,
			) error {
				calls++
				values, _ = correlation.FromContext(ctx)

				return nil
			}),
		})
		if err != nil {
			t.Fatalf("NewHandler() error = %v", err)
		}
		err = handler.Handle(context.Background(), kafka.ConsumedMessage{
			Headers: []kafka.Header{
				{Key: "correlation_id", Value: []byte(correlationValue)},
				{Key: "request_id", Value: []byte(requestValue)},
			},
		})
		if err != nil {
			if !rejectInvalid || calls != 0 ||
				!errors.Is(err, correlation.ErrInvalidCarrier) {
				t.Fatalf("Handle() error/calls = %v/%d", err, calls)
			}

			return
		}
		if calls != 1 {
			t.Fatalf("handler calls = %d, want 1", calls)
		}
		if _, err = correlation.ParseCorrelationID(
			values.CorrelationID.String(),
			correlation.Policy{},
		); err != nil {
			t.Fatalf("CorrelationID = %q: %v", values.CorrelationID, err)
		}
		if _, err = correlation.ParseRequestID(
			values.RequestID.String(),
			correlation.Policy{},
		); err != nil {
			t.Fatalf("RequestID = %q: %v", values.RequestID, err)
		}
		if values.CorrelationID == "" || values.RequestID == "" {
			t.Fatalf("handler values = %#v", values)
		}
	})
}
