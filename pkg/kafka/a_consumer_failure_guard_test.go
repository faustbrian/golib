package kafka

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConsumerFailureCriticalGuardsTerminateDeterministically(t *testing.T) {
	t.Run("delegate mode rejects target and publisher independently", func(t *testing.T) {
		base := FailureHandlerConfig{
			Handler: HandlerFunc(func(context.Context, ConsumedMessage) error {
				return nil
			}),
			Mode: FailureModeDelegate,
			Delegate: FailureDelegateFunc(func(
				context.Context,
				HandlerFailure,
			) error {
				return nil
			}),
		}
		for name, change := range map[string]func(*FailureHandlerConfig){
			"target": func(config *FailureHandlerConfig) {
				config.Target = FailureTarget{Topic: "events.retry.v1", Version: 1}
			},
			"publisher": func(config *FailureHandlerConfig) {
				config.Publisher = failurePublisherFunc(func(
					context.Context,
					ProducerRecord,
				) DeliveryResult {
					return DeliveryResult{}
				})
			},
		} {
			config := base
			change(&config)
			if err := config.Validate(); !errors.Is(err, ErrInvalidFailurePolicy) {
				t.Fatalf("%s delegate mode error = %v", name, err)
			}
		}
	})

	t.Run("single attempt rejects max backoff and categories independently", func(t *testing.T) {
		for name, policy := range map[string]FailureRetryPolicy{
			"max backoff": {
				MaxAttempts: 1,
				MaxBackoff:  time.Millisecond,
			},
			"categories": {
				MaxAttempts: 1,
				Categories:  []ErrorCategory{ErrorRetryable},
			},
		} {
			if _, err := normalizeFailureRetryPolicy(policy); !errors.Is(
				err,
				ErrInvalidFailurePolicy,
			) {
				t.Fatalf("%s retry policy error = %v", name, err)
			}
		}
	})
}
