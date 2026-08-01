package kafka

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestConsumerDrainInterruptsIdlePollAndPermitsAnotherRunner(t *testing.T) {
	pollStarted := make(chan struct{})
	backend := &recordingConsumerBackend{}
	backend.poll = func(ctx context.Context, _ int) kgo.Fetches {
		close(pollStarted)
		<-ctx.Done()

		return kgo.NewErrFetch(ctx.Err())
	}
	consumer := consumerWithBackend(backend, 1, time.Second, time.Second)
	runCtx, cancelRun := context.WithCancel(context.Background())
	t.Cleanup(cancelRun)
	runDone := make(chan error, 1)
	go func() {
		runDone <- consumer.Run(runCtx, HandlerFunc(func(
			context.Context,
			ConsumedMessage,
		) error {
			t.Error("idle poll invoked handler")

			return nil
		}))
	}()
	<-pollStarted

	drainCtx, cancelDrain := context.WithTimeout(context.Background(), time.Second)
	defer cancelDrain()
	if err := consumer.Drain(drainCtx); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if backend.pollCalls != 1 || backend.allowed != 1 ||
		backend.leaveCalls != 0 || backend.closed != 0 {
		t.Fatalf("drained idle backend = %#v", backend)
	}

	backend.poll = nil
	backend.fetches = recordFetches(&kgo.Record{
		Topic: "events", Partition: 1, Offset: 0,
	})
	result, err := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error { return nil }),
	)
	if err != nil ||
		result != (PollResult{Polled: 1, Processed: 1, Committed: 1}) {
		t.Fatalf("RunOnce() after drain = (%#v, %v)", result, err)
	}
}

func TestConsumerDrainPreservesActiveHandlerAndIsRetriable(t *testing.T) {
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	backend := &recordingConsumerBackend{
		fetches: recordFetches(&kgo.Record{
			Topic: "events", Partition: 1, Offset: 0,
		}),
	}
	consumer := consumerWithBackend(backend, 1, time.Second, time.Second)
	runDone := make(chan error, 1)
	go func() {
		runDone <- consumer.Run(context.Background(), HandlerFunc(func(
			ctx context.Context,
			_ ConsumedMessage,
		) error {
			close(handlerStarted)
			select {
			case <-releaseHandler:
				return nil
			case <-ctx.Done():
				return context.Cause(ctx)
			}
		}))
	}()
	<-handlerStarted

	drainCtx, cancelDrain := context.WithCancel(context.Background())
	cancelDrain()
	err := consumer.Drain(drainCtx)
	if !errors.Is(err, ErrConsumerDrainIncomplete) ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("timed Drain() error = %v", err)
	}
	if _, runErr := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error { return nil }),
	); !errors.Is(runErr, ErrConsumerDraining) {
		t.Fatalf("RunOnce() during incomplete drain error = %v", runErr)
	}
	partition := TopicPartition{Topic: "events", Partition: 1}
	if pauseErr := consumer.PausePartitions(partition); !errors.Is(
		pauseErr,
		ErrConsumerDraining,
	) {
		t.Fatalf("PausePartitions() during drain error = %v", pauseErr)
	}
	if resumeErr := consumer.ResumePartitions(partition); !errors.Is(
		resumeErr,
		ErrConsumerDraining,
	) {
		t.Fatalf("ResumePartitions() during drain error = %v", resumeErr)
	}

	close(releaseHandler)
	if runErr := <-runDone; runErr != nil {
		t.Fatalf("Run() error = %v", runErr)
	}
	if len(backend.committed) != 1 || backend.committed[0].Offset != 0 {
		t.Fatalf("drained commit = %#v", backend.committed)
	}
	if err := consumer.Drain(context.Background()); err != nil {
		t.Fatalf("retry Drain() error = %v", err)
	}
}

func TestConsumerDrainRejectsLifecycleConflicts(t *testing.T) {
	var nilContext context.Context
	consumer := consumerWithBackend(
		&recordingConsumerBackend{},
		1,
		time.Second,
		time.Second,
	)
	if err := consumer.Drain(nilContext); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Drain(nil) error = %v", err)
	}
	observerCtx := context.WithValue(
		context.Background(),
		observerContextKey{},
		true,
	)
	if err := consumer.Drain(observerCtx); !errors.Is(err, ErrObserverReentry) {
		t.Fatalf("Drain(observer context) error = %v", err)
	}

	tests := []struct {
		name  string
		state func(*Consumer)
		want  error
	}{
		{
			name: "observer callback",
			state: func(consumer *Consumer) {
				consumer.observerCallbacks = 1
			},
			want: ErrObserverReentry,
		},
		{
			name:  "closed",
			state: func(consumer *Consumer) { consumer.closed = true },
			want:  ErrConsumerClosed,
		},
		{
			name:  "closing",
			state: func(consumer *Consumer) { consumer.closing = true },
			want:  ErrConsumerClosing,
		},
		{
			name: "fatal",
			state: func(consumer *Consumer) {
				consumer.fatalErr = ErrConsumerInstanceFenced
			},
			want: ErrConsumerFatal,
		},
		{
			name:  "active drain",
			state: func(consumer *Consumer) { consumer.drainActive = true },
			want:  ErrConsumerDrainActive,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			consumer := consumerWithBackend(
				&recordingConsumerBackend{},
				1,
				time.Second,
				time.Second,
			)
			test.state(consumer)
			if err := consumer.Drain(context.Background()); !errors.Is(
				err,
				test.want,
			) {
				t.Fatalf("Drain() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestConsumerShutdownRejectsActiveDrain(t *testing.T) {
	backend := &recordingConsumerBackend{}
	consumer := consumerWithBackend(backend, 1, time.Second, time.Second)
	consumer.drainActive = true

	if err := consumer.Shutdown(context.Background()); !errors.Is(
		err,
		ErrConsumerDrainActive,
	) {
		t.Fatalf("Shutdown() during drain error = %v", err)
	}
	if consumer.closing || consumer.shutdownActive ||
		backend.leaveCalls != 0 || backend.closed != 0 {
		t.Fatalf("Shutdown() changed state during drain: %#v", consumer)
	}
}

func TestConsumerDrainReturnsFatalStateReachedWhileWaiting(t *testing.T) {
	consumer := consumerWithBackend(
		&recordingConsumerBackend{},
		1,
		time.Second,
		time.Second,
	)
	done := make(chan struct{})
	consumer.runDone = done
	drainDone := make(chan error, 1)
	go func() {
		drainDone <- consumer.Drain(context.Background())
	}()
	waitForConsumerDrainActive(t, consumer)
	consumer.lifecycleMu.Lock()
	consumer.fatalErr = ErrConsumerInstanceFenced
	consumer.lifecycleMu.Unlock()
	close(done)

	err := <-drainDone
	if !errors.Is(err, ErrConsumerFatal) ||
		!errors.Is(err, ErrConsumerInstanceFenced) {
		t.Fatalf("Drain() fatal transition error = %v", err)
	}
	if consumer.drainRequested || consumer.drainActive {
		t.Fatalf("fatal drain retained state: %#v", consumer)
	}
}

func waitForConsumerDrainActive(t *testing.T, consumer *Consumer) {
	t.Helper()

	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		consumer.lifecycleMu.Lock()
		active := consumer.drainActive
		consumer.lifecycleMu.Unlock()
		if active {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("consumer drain did not become active")
		default:
			runtime.Gosched()
		}
	}
}

func TestConsumerDrainInterruptsIdleBatchPoll(t *testing.T) {
	pollStarted := make(chan struct{})
	backend := &recordingConsumerBackend{}
	backend.poll = func(ctx context.Context, _ int) kgo.Fetches {
		close(pollStarted)
		<-ctx.Done()

		return kgo.NewErrFetch(ctx.Err())
	}
	consumer := consumerWithBackend(backend, 1, time.Second, time.Second)
	runDone := make(chan error, 1)
	go func() {
		_, err := consumer.RunBatchOnce(
			context.Background(),
			BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
				t.Error("idle batch poll invoked handler")

				return nil
			}),
		)
		runDone <- err
	}()
	<-pollStarted

	if err := consumer.Drain(context.Background()); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("RunBatchOnce() error = %v", err)
	}
	if backend.pollCalls != 1 || backend.allowed != 1 {
		t.Fatalf("drained idle batch backend = %#v", backend)
	}
}

func TestConsumerDrainDoesNotSuppressUnrelatedPollErrors(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Consumer, context.Context) error
	}{
		{
			name: "record",
			run: func(consumer *Consumer, ctx context.Context) error {
				_, err := consumer.RunOnce(
					ctx,
					HandlerFunc(func(context.Context, ConsumedMessage) error {
						t.Fatal("failed poll invoked record handler")

						return nil
					}),
				)

				return err
			},
		},
		{
			name: "batch",
			run: func(consumer *Consumer, ctx context.Context) error {
				_, err := consumer.RunBatchOnce(
					ctx,
					BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
						t.Fatal("failed poll invoked batch handler")

						return nil
					}),
				)

				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pollStarted := make(chan struct{})
			fetchErr := errors.New("fetch failed during drain")
			backend := &recordingConsumerBackend{}
			backend.poll = func(ctx context.Context, _ int) kgo.Fetches {
				close(pollStarted)
				<-ctx.Done()

				return kgo.NewErrFetch(fetchErr)
			}
			consumer := consumerWithBackend(
				backend,
				1,
				time.Second,
				time.Second,
			)
			runDone := make(chan error, 1)
			go func() {
				runDone <- test.run(consumer, context.Background())
			}()
			<-pollStarted

			if err := consumer.Drain(context.Background()); err != nil {
				t.Fatalf("Drain() error = %v", err)
			}
			if err := <-runDone; !errors.Is(err, fetchErr) {
				t.Fatalf("runner error = %v, want %v", err, fetchErr)
			}
			if backend.allowed != 1 {
				t.Fatalf("AllowRebalance() calls = %d, want 1", backend.allowed)
			}
		})
	}
}

func TestConsumerPollCancellationRequiresDrainCauseToBeSuppressed(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Consumer) error
	}{
		{
			name: "record",
			run: func(consumer *Consumer) error {
				_, err := consumer.RunOnce(
					context.Background(),
					HandlerFunc(func(context.Context, ConsumedMessage) error {
						t.Fatal("canceled poll invoked record handler")

						return nil
					}),
				)

				return err
			},
		},
		{
			name: "batch",
			run: func(consumer *Consumer) error {
				_, err := consumer.RunBatchOnce(
					context.Background(),
					BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
						t.Fatal("canceled poll invoked batch handler")

						return nil
					}),
				)

				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &recordingConsumerBackend{
				fetches: kgo.NewErrFetch(context.Canceled),
			}
			consumer := consumerWithBackend(
				backend,
				1,
				time.Second,
				time.Second,
			)

			if err := test.run(consumer); !errors.Is(err, context.Canceled) {
				t.Fatalf("runner error = %v, want context.Canceled", err)
			}
			if backend.allowed != 1 {
				t.Fatalf("AllowRebalance() calls = %d, want 1", backend.allowed)
			}
		})
	}
}

func TestConsumerDrainRequestPreventsPollAdmission(t *testing.T) {
	backend := &recordingConsumerBackend{
		fetches: recordFetches(&kgo.Record{
			Topic: "events", Partition: 1, Offset: 0,
		}),
	}
	consumer := consumerWithBackend(backend, 1, time.Second, time.Second)
	consumer.drainRequested = true
	recordResult, recordErr := consumer.runOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			t.Error("draining consumer invoked record handler")

			return nil
		}),
	)
	batchResult, batchErr := consumer.runBatchOnce(
		context.Background(),
		BatchHandlerFunc(func(context.Context, ConsumedBatch) error {
			t.Error("draining consumer invoked batch handler")

			return nil
		}),
	)
	if recordErr != nil || batchErr != nil ||
		recordResult != (PollResult{}) || batchResult != (PollResult{}) ||
		backend.pollCalls != 0 || backend.allowed != 0 {
		t.Fatalf(
			"draining results/errors/backend = %#v/%v/%#v/%v/%#v",
			recordResult,
			recordErr,
			batchResult,
			batchErr,
			backend,
		)
	}
}

func TestConsumerShutdownInterruptsIdlePoll(t *testing.T) {
	pollStarted := make(chan struct{})
	backend := &recordingConsumerBackend{}
	backend.poll = func(ctx context.Context, _ int) kgo.Fetches {
		close(pollStarted)
		<-ctx.Done()

		return kgo.NewErrFetch(ctx.Err())
	}
	consumer := consumerWithBackend(backend, 1, time.Second, time.Second)
	runDone := make(chan error, 1)
	go func() {
		runDone <- consumer.Run(context.Background(), HandlerFunc(func(
			context.Context,
			ConsumedMessage,
		) error {
			t.Error("idle poll invoked handler")

			return nil
		}))
	}()
	<-pollStarted

	if err := consumer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if backend.pollCalls != 1 || backend.allowed != 1 ||
		backend.leaveCalls != 1 || backend.closed != 1 {
		t.Fatalf("shutdown idle backend = %#v", backend)
	}
}
