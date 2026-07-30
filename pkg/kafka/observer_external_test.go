package kafka_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/kafka"
)

func TestObservationFailureFormattingRedactsObserverCause(t *testing.T) {

	sensitive := errors.New("token=observer-secret")
	var got kafka.ObservationFailure
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{"localhost:9092"},
		ClientID:      "observer-redaction-test",
		AllowedTopics: []string{"events"},
		Security:      kafka.DevelopmentPlaintextSecurity(),
		Observers: kafka.ObserverPolicy{
			Observers: []kafka.ObserverFunc{
				func(context.Context, kafka.Observation) error {
					return sensitive
				},
			},
			FailureHandler: func(
				_ context.Context,
				failure kafka.ObservationFailure,
			) {
				got = failure
			},
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	t.Cleanup(func() {
		if err := producer.Close(); err != nil {
			t.Errorf("Producer.Close() error = %v", err)
		}
	})

	result := producer.PublishRecord(context.Background(), kafka.ProducerRecord{
		Topic: "events",
	})

	if !errors.Is(result.Err, kafka.ErrKeyRequired) {
		t.Fatalf("PublishRecord() error = %v", result.Err)
	}
	if !errors.Is(got.Cause(), sensitive) {
		t.Fatalf("observation failure cause = %v", got.Cause())
	}
	rendered := fmt.Sprintf("%v | %+v | %#v", got, got, got)
	if strings.Contains(rendered, "observer-secret") {
		t.Fatalf("observation failure disclosed observer cause: %s", rendered)
	}
}
