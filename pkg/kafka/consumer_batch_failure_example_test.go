package kafka_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
)

func ExampleNewBatchFailureHandler() {
	attempts := 0
	handler, err := kafka.NewBatchFailureHandler(kafka.BatchFailureHandlerConfig{
		Handler: kafka.BatchHandlerFunc(func(
			context.Context,
			kafka.ConsumedBatch,
		) error {
			attempts++
			if attempts == 1 {
				return errors.New("dependency unavailable")
			}

			return nil
		}),
		Classifier: kafka.FailureClassifierFunc(func(error) kafka.ErrorCategory {
			return kafka.ErrorRetryable
		}),
		Retry: kafka.FailureRetryPolicy{
			MaxAttempts:    2,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		},
	})
	if err != nil {
		panic(err)
	}

	err = handler.HandleBatch(context.Background(), kafka.ConsumedBatch{
		Topic: "events", Partition: 0,
		Records: []kafka.ConsumedRecord{
			{Topic: "events", Partition: 0, Offset: 7},
			{Topic: "events", Partition: 0, Offset: 8},
		},
	})
	fmt.Println(err == nil, attempts)

	// Output: true 2
}
