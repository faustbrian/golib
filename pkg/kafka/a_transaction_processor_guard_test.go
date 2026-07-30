package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestTransactionProcessorCriticalGuardsTerminateDeterministically(t *testing.T) {
	t.Run("transactional ID is required after producer normalization", func(t *testing.T) {
		config := validTransactionProcessorConfig()
		config.Output.TransactionalID = ""
		if _, err := normalizeTransactionProcessorConfig(config); !errors.Is(
			err,
			ErrInvalidTransactionProcessorConfig,
		) {
			t.Fatalf("normalizeTransactionProcessorConfig() error = %v", err)
		}
	})

	t.Run("canceled run does not poll", func(t *testing.T) {
		pollStarted := make(chan struct{})
		backend := &recordingTransactionProcessorBackend{
			pollStarted: pollStarted,
			fetches: []kgo.Fetches{transactionFetches(
				transactionSourceRecord(0, "canceled"),
			)},
		}
		processor := transactionProcessorForTest(t, backend)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := processor.Run(
			ctx,
			TransactionHandlerFunc(func(
				context.Context,
				ConsumedRecord,
				Transaction,
			) error {
				t.Fatal("handler called for canceled run")

				return nil
			}),
		)
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		select {
		case <-pollStarted:
			t.Fatal("Run() polled after cancellation")
		default:
		}
	})

	t.Run("nil cancel function still closes client", func(t *testing.T) {
		backend := &recordingTransactionProcessorBackend{}
		processor := &TransactionProcessor{client: backend}

		processor.interruptClient()

		if !backend.closed {
			t.Fatal("interruptClient() did not close backend")
		}
	})

	t.Run("shutdown waits for an active run", func(t *testing.T) {
		backend := &recordingTransactionProcessorBackend{}
		processor := transactionProcessorForTest(t, backend)
		done := make(chan struct{})
		processor.lifecycleMu.Lock()
		processor.running = true
		processor.runDone = done
		processor.staticMembership = true
		processor.lifecycleMu.Unlock()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := processor.Shutdown(ctx)
		if !errors.Is(err, ErrTransactionProcessorShutdownIncomplete) ||
			!errors.Is(err, context.Canceled) {
			t.Fatalf("Shutdown() error = %v", err)
		}
		if backend.closed {
			t.Fatal("Shutdown() closed backend before active run completed")
		}

		close(done)
		if err := processor.Shutdown(context.Background()); err != nil {
			t.Fatalf("second Shutdown() error = %v", err)
		}
	})

	t.Run("first handler failure stops the poll", func(t *testing.T) {
		handlerErr := errors.New("first transform failed")
		unexpectedErr := errors.New("handler continued after failure")
		backend := &recordingTransactionProcessorBackend{
			fetches: []kgo.Fetches{transactionFetches(
				transactionSourceRecord(0, "first"),
				transactionSourceRecord(1, "second"),
				transactionSourceRecord(2, "third"),
			)},
			endResults: []transactionEndResult{{committed: false}},
		}
		processor := transactionProcessorForTest(t, backend)
		calls := 0

		result, err := processor.RunOnce(
			context.Background(),
			TransactionHandlerFunc(func(
				context.Context,
				ConsumedRecord,
				Transaction,
			) error {
				calls++
				if calls == 1 {
					return handlerErr
				}

				return unexpectedErr
			}),
		)

		if !errors.Is(err, handlerErr) || errors.Is(err, unexpectedErr) ||
			result != (TransactionPollResult{Polled: 3}) || calls != 1 {
			t.Fatalf("RunOnce() result/error/calls = %#v/%v/%d", result, err, calls)
		}
	})

	t.Run("output byte reservation includes every record", func(t *testing.T) {
		record := ProducerRecord{
			Topic: "derived-events", Key: []byte("key"), Value: []byte("value"),
		}
		size := recordSize(record)
		publisher := &processorTransactionPublisher{
			limits:         DefaultMessageLimits(),
			allowedTopics:  map[string]struct{}{"derived-events": {}},
			maxOutputCount: 3,
			maxOutputBytes: 3*size - 1,
			waitTimeout:    time.Second,
		}

		if err := publisher.reserve(record); err != nil {
			t.Fatalf("first reserve() error = %v", err)
		}
		if err := publisher.reserve(record); err != nil {
			t.Fatalf("second reserve() error = %v", err)
		}
		if err := publisher.reserve(record); !errors.Is(err, ErrTransactionOutputTooLarge) {
			t.Fatalf("third reserve() error = %v, want %v", err, ErrTransactionOutputTooLarge)
		}
		if publisher.reservedCount != 2 || publisher.reservedBytes != 2*size {
			t.Fatalf(
				"reserved count/bytes = %d/%d",
				publisher.reservedCount,
				publisher.reservedBytes,
			)
		}
	})
}
