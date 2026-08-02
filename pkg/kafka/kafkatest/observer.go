package kafkatest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
)

// RunObserverConformance proves ordered synchronous delivery, copied
// registration, panic and error containment, cooperative timeout reporting,
// payload-free observations, and same-client reentrancy fencing.
func RunObserverConformance(t *testing.T) {
	t.Helper()

	t.Run("order containment ownership and reentrancy", func(t *testing.T) {
		observerErr := errors.New("kafkatest: observer failure")
		var (
			producer   *kafka.Producer
			order      []string
			failures   []kafka.ObservationFailure
			reentryErr error
			observed   kafka.Observation
		)
		observers := []kafka.ObserverFunc{
			func(ctx context.Context, observation kafka.Observation) error {
				order = append(order, "first")
				observed = observation
				if _, hasDeadline := ctx.Deadline(); !hasDeadline {
					return errors.New("observer context is unbounded")
				}
				return nil
			},
			func(context.Context, kafka.Observation) error {
				order = append(order, "panic")
				panic("kafkatest panic payload")
			},
			func(ctx context.Context, _ kafka.Observation) error {
				order = append(order, "error")
				reentryErr = producer.PublishRecord(ctx, kafka.ProducerRecord{
					Topic: "conformance", Key: []byte("nested"),
				}).Err
				return observerErr
			},
			func(context.Context, kafka.Observation) error {
				order = append(order, "last")
				return nil
			},
		}
		var err error
		producer, err = kafka.NewProducer(kafka.ProducerConfig{
			Brokers:       []string{"127.0.0.1:1"},
			ClientID:      "kafkatest-observer",
			AllowedTopics: []string{"conformance"},
			Security:      kafka.DevelopmentPlaintextSecurity(),
			Observers: kafka.ObserverPolicy{
				Observers: observers,
				FailureHandler: func(
					_ context.Context,
					failure kafka.ObservationFailure,
				) {
					failures = append(failures, failure)
				},
				Timeout: 100 * time.Millisecond,
			},
		})
		if err != nil {
			t.Fatalf("NewProducer() error = %v", err)
		}
		observers[0] = func(context.Context, kafka.Observation) error {
			order = append(order, "mutated")
			return nil
		}
		result := producer.PublishRecord(t.Context(), kafka.ProducerRecord{
			Topic: "conformance", Value: []byte("payload-never-observed"),
			Headers: []kafka.Header{{Key: "secret", Value: []byte("never-observed")}},
		})
		if !errors.Is(result.Err, kafka.ErrKeyRequired) {
			t.Fatalf("PublishRecord() error = %v", result.Err)
		}
		if fmt.Sprint(order) != "[first panic error last]" {
			t.Fatalf("observer order = %v", order)
		}
		if !errors.Is(reentryErr, kafka.ErrObserverReentry) {
			t.Fatalf("observer reentry error = %v", reentryErr)
		}
		if len(failures) != 2 || failures[0].ObserverIndex != 1 ||
			!failures[0].Panicked || failures[0].TimedOut ||
			!errors.Is(failures[0].Cause(), kafka.ErrObserverPanic) ||
			failures[1].ObserverIndex != 2 || failures[1].Panicked ||
			failures[1].TimedOut || !errors.Is(failures[1].Cause(), observerErr) {
			t.Fatalf("observer failure count = %d or classifications differ", len(failures))
		}
		if observed.Kind != kafka.ObservationProduceRecord ||
			observed.Topic != "conformance" || observed.Succeeded ||
			observed.Category != kafka.ErrorPermanent || observed.RecordCount != 1 ||
			observed.RecordBytes <= 0 {
			t.Fatalf("observation = %#v", observed)
		}
		if err := observed.Validate(); err != nil {
			t.Fatalf("Observation.Validate() error = %v", err)
		}
		if err := producer.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	t.Run("cooperative timeout is reported", func(t *testing.T) {
		var failure kafka.ObservationFailure
		producer, err := kafka.NewProducer(kafka.ProducerConfig{
			Brokers:       []string{"127.0.0.1:1"},
			ClientID:      "kafkatest-observer-timeout",
			AllowedTopics: []string{"conformance"},
			Security:      kafka.DevelopmentPlaintextSecurity(),
			Observers: kafka.ObserverPolicy{
				Observers: []kafka.ObserverFunc{
					func(ctx context.Context, _ kafka.Observation) error {
						<-ctx.Done()
						return nil
					},
				},
				FailureHandler: func(
					_ context.Context,
					got kafka.ObservationFailure,
				) {
					failure = got
				},
				Timeout: time.Millisecond,
			},
		})
		if err != nil {
			t.Fatalf("NewProducer() error = %v", err)
		}
		result := producer.PublishRecord(t.Context(), kafka.ProducerRecord{
			Topic: "conformance",
		})
		if !errors.Is(result.Err, kafka.ErrKeyRequired) {
			t.Fatalf("PublishRecord() error = %v", result.Err)
		}
		if !failure.TimedOut || failure.Panicked ||
			!errors.Is(failure.Cause(), context.DeadlineExceeded) {
			t.Fatalf(
				"timeout failure index=%d timed_out=%t panicked=%t",
				failure.ObserverIndex,
				failure.TimedOut,
				failure.Panicked,
			)
		}
		if err := producer.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
}
