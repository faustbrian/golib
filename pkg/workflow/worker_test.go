package workflow_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestWorkDecisionMakesTerminalHandlingExplicit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	completed, err := workflow.NewWorkDecision(workflow.WorkDecisionSpec{Kind: workflow.WorkComplete})
	if err != nil || completed.Kind() != workflow.WorkComplete || !completed.Valid() {
		t.Fatalf("complete decision = %#v, %v", completed, err)
	}
	retry, err := workflow.NewWorkDecision(workflow.WorkDecisionSpec{
		Kind: workflow.WorkRetryDecision, Code: "temporary", RetryAt: now.Add(time.Minute),
	})
	if err != nil || retry.Code() != "temporary" || retry.RetryAt() != now.Add(time.Minute) || !retry.Valid() {
		t.Fatalf("retry decision = %#v, %v", retry, err)
	}
	dead, err := workflow.NewWorkDecision(workflow.WorkDecisionSpec{
		Kind: workflow.WorkDeadLetterDecision, Code: "poison",
	})
	if err != nil || dead.Code() != "poison" || !dead.RetryAt().IsZero() || !dead.Valid() {
		t.Fatalf("dead-letter decision = %#v, %v", dead, err)
	}
	for _, spec := range []workflow.WorkDecisionSpec{
		{},
		{Kind: workflow.WorkComplete, Code: "unexpected"},
		{Kind: workflow.WorkRetryDecision, Code: "temporary"},
		{Kind: workflow.WorkDeadLetterDecision},
	} {
		if _, err := workflow.NewWorkDecision(spec); !errors.Is(err, workflow.ErrInvalidWorker) {
			t.Fatalf("invalid decision error = %v for %#v", err, spec)
		}
	}
}

func TestWorkerRunsWithBoundedConcurrencyAndCompletesPersistedDecisions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	leases := []workflow.WorkLease{
		mustWorkerLease(t, now, "work-1", "tenant-1"),
		mustWorkerLease(t, now, "work-2", "tenant-2"),
		mustWorkerLease(t, now, "work-3", "tenant-1"),
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	store := &workerStore{claims: [][]workflow.WorkLease{leases}}
	processor := &countingProcessor{cancel: cancel, target: len(leases)}
	worker, err := workflow.NewWorker(workflow.WorkerConfig{
		Store: store, Processor: processor, Clock: workflow.SystemClock{}, Owner: "worker-1",
		MaxConcurrent: 2, ClaimLimit: 3, LeaseDuration: time.Minute,
		RenewEvery: 20 * time.Second, PollInterval: time.Millisecond, FinalizeTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("construct worker: %v", err)
	}
	if err := runWorkerWithin(t, worker, ctx); err != nil {
		t.Fatalf("run worker: %v", err)
	}
	if processor.maximum > 2 || processor.completed != 3 {
		t.Fatalf("processor concurrency = max %d completed %d", processor.maximum, processor.completed)
	}
	if len(store.completions) != 3 || store.claimLimits[0] != 2 {
		t.Fatalf("store calls = completions %d claim limits %#v", len(store.completions), store.claimLimits)
	}
	for _, limit := range store.claimLimits {
		if limit == 0 || limit > 2 {
			t.Fatalf("claim limit = %d, all limits %#v", limit, store.claimLimits)
		}
	}
}

func TestWorkerImmediatelyRefillsAvailableCapacity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	store := &workerStore{claims: [][]workflow.WorkLease{
		{mustWorkerLease(t, now, "work-1", "tenant-1")},
		{mustWorkerLease(t, now, "work-2", "tenant-2")},
	}}
	started := make(chan struct{}, 2)
	processor := processorFunc(func(ctx context.Context, _ workflow.WorkLease) (workflow.WorkDecision, error) {
		started <- struct{}{}
		<-ctx.Done()
		return workflow.WorkDecision{}, ctx.Err()
	})
	clock := newManualClock(now)
	worker := mustWorker(t, store, processor, clock)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	receiveWithin(t, started)
	receiveWithin(t, started)
	cancel()
	if err := receiveWithin(t, done); err != nil {
		t.Fatalf("stop capacity worker: %v", err)
	}
}

func TestWorkerPollsAfterAnEmptyClaim(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	clock := newManualClock(now)
	worker := mustWorker(t, &workerStore{}, &countingProcessor{}, clock)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	receiveWithin(t, clock.ready)
	cancel()
	if err := receiveWithin(t, done); err != nil {
		t.Fatalf("stop empty worker: %v", err)
	}
}

func TestWorkerRenewsLeaseAndCancelsAStaleProcessor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	lease := mustWorkerLease(t, now, "work-1", "")
	clock := newManualClock(now)
	store := &workerStore{renewErr: workflow.ErrStaleWorkLease}
	processor := &cancelAwareProcessor{started: make(chan struct{}), canceled: make(chan struct{})}
	worker, err := workflow.NewWorker(workflow.WorkerConfig{
		Store: store, Processor: processor, Clock: clock, Owner: "worker-1",
		MaxConcurrent: 1, ClaimLimit: 1, LeaseDuration: time.Minute,
		RenewEvery: 20 * time.Second, PollInterval: time.Second, FinalizeTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("construct worker: %v", err)
	}
	done := make(chan struct{})
	go func() {
		worker.Handle(context.Background(), lease)
		close(done)
	}()
	receiveWithin(t, processor.started)
	receiveWithin(t, clock.ready)
	clock.FireNext(now.Add(20 * time.Second))
	select {
	case <-processor.canceled:
	case <-time.After(time.Second):
		t.Fatal("stale renewal did not cancel the processor")
	}
	receiveWithin(t, done)
	if len(store.completions) != 0 || store.renewals != 1 {
		t.Fatalf("stale handling calls = completions %d renewals %d", len(store.completions), store.renewals)
	}
}

func TestNewWorkerRejectsUnboundedOrMissingConfiguration(t *testing.T) {
	t.Parallel()

	valid := workflow.WorkerConfig{
		Store: &workerStore{}, Processor: &countingProcessor{}, Clock: workflow.SystemClock{},
		Owner: "worker-1", MaxConcurrent: 1, ClaimLimit: 1,
		LeaseDuration: time.Minute, RenewEvery: 20 * time.Second, PollInterval: time.Second,
		FinalizeTimeout: time.Second,
	}
	invalid := []workflow.WorkerConfig{
		{},
		func() workflow.WorkerConfig { config := valid; config.Store = nil; return config }(),
		func() workflow.WorkerConfig { config := valid; config.Processor = nil; return config }(),
		func() workflow.WorkerConfig { config := valid; config.Clock = nil; return config }(),
		func() workflow.WorkerConfig { config := valid; config.MaxConcurrent = 0; return config }(),
		func() workflow.WorkerConfig {
			config := valid
			config.MaxConcurrent = workflow.MaxWorkerConcurrency + 1
			return config
		}(),
		func() workflow.WorkerConfig { config := valid; config.ClaimLimit = 0; return config }(),
		func() workflow.WorkerConfig {
			config := valid
			config.ClaimLimit = workflow.MaxWorkClaimItems + 1
			return config
		}(),
		func() workflow.WorkerConfig { config := valid; config.LeaseDuration = 0; return config }(),
		func() workflow.WorkerConfig {
			config := valid
			config.LeaseDuration = workflow.MaxWorkLeaseDuration + time.Nanosecond
			return config
		}(),
		func() workflow.WorkerConfig { config := valid; config.RenewEvery = 0; return config }(),
		func() workflow.WorkerConfig { config := valid; config.RenewEvery = config.LeaseDuration; return config }(),
		func() workflow.WorkerConfig { config := valid; config.PollInterval = 0; return config }(),
		func() workflow.WorkerConfig {
			config := valid
			config.PollInterval = workflow.MaxWorkerPollInterval + time.Nanosecond
			return config
		}(),
		func() workflow.WorkerConfig { config := valid; config.FinalizeTimeout = 0; return config }(),
		func() workflow.WorkerConfig {
			config := valid
			config.FinalizeTimeout = workflow.MaxWorkLeaseDuration + time.Nanosecond
			return config
		}(),
	}
	for _, config := range invalid {
		if _, err := workflow.NewWorker(config); !errors.Is(err, workflow.ErrInvalidWorker) {
			t.Fatalf("invalid worker error = %v for %#v", err, config)
		}
	}
	maximum := valid
	maximum.MaxConcurrent = workflow.MaxWorkerConcurrency
	maximum.ClaimLimit = workflow.MaxWorkClaimItems
	maximum.LeaseDuration = workflow.MaxWorkLeaseDuration
	maximum.RenewEvery = workflow.MaxWorkLeaseDuration - time.Nanosecond
	maximum.PollInterval = workflow.MaxWorkerPollInterval
	maximum.FinalizeTimeout = workflow.MaxWorkLeaseDuration
	if _, err := workflow.NewWorker(maximum); err != nil {
		t.Fatalf("maximum bounded worker: %v", err)
	}
}

func TestWorkerRunValidatesRuntimeBoundariesAndStopsIdlePolling(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	if err := runWorkerWithin(t, nil, context.Background()); !errors.Is(err, workflow.ErrInvalidWorker) {
		t.Fatalf("nil worker run = %v", err)
	}
	worker := mustWorker(t, &workerStore{}, &countingProcessor{}, newManualClock(now))
	if err := runWorkerWithin(t, worker, nil); !errors.Is(err, workflow.ErrInvalidWorker) {
		t.Fatalf("nil worker context = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runWorkerWithin(t, worker, canceled); err != nil {
		t.Fatalf("pre-canceled worker = %v", err)
	}

	zeroClock := newManualClock(time.Time{})
	zeroWorker := mustWorker(t, &workerStore{}, &countingProcessor{}, zeroClock)
	if err := runWorkerWithin(t, zeroWorker, context.Background()); !errors.Is(err, workflow.ErrInvalidWorker) {
		t.Fatalf("zero clock run = %v", err)
	}

	overLimitStore := &workerStore{
		claims: [][]workflow.WorkLease{{
			mustWorkerLease(t, now, "work-1", ""), mustWorkerLease(t, now, "work-2", ""),
			mustWorkerLease(t, now, "work-3", ""),
		}},
		ignoreClaimLimit: true,
	}
	overLimitWorker := mustWorker(t, overLimitStore, &countingProcessor{}, newManualClock(now))
	if err := runWorkerWithin(t, overLimitWorker, context.Background()); !errors.Is(err, workflow.ErrInvalidWorker) {
		t.Fatalf("over-limit adapter run = %v", err)
	}

	idleClock := newManualClock(now)
	idleWorker := mustWorker(t, &workerStore{claimErr: errors.New("temporary claim failure")}, &countingProcessor{}, idleClock)
	idleContext, stopIdle := context.WithCancel(context.Background())
	idleDone := make(chan error, 1)
	go func() { idleDone <- idleWorker.Run(idleContext) }()
	receiveWithin(t, idleClock.ready)
	stopIdle()
	if err := receiveWithin(t, idleDone); err != nil {
		t.Fatalf("stop idle worker: %v", err)
	}
}

func TestWorkerHandleAppliesRetryAndDeadLetterDecisions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		decision    workflow.WorkDecisionSpec
		disposition workflow.WorkDisposition
	}{
		{name: "retry", decision: workflow.WorkDecisionSpec{
			Kind: workflow.WorkRetryDecision, Code: "temporary", RetryAt: now.Add(time.Minute),
		}, disposition: workflow.WorkRetry},
		{name: "dead letter", decision: workflow.WorkDecisionSpec{
			Kind: workflow.WorkDeadLetterDecision, Code: "poison",
		}, disposition: workflow.WorkDeadLetter},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &workerStore{}
			decision, err := workflow.NewWorkDecision(test.decision)
			if err != nil {
				t.Fatalf("construct decision: %v", err)
			}
			worker := mustWorker(t, store, processorFunc(func(context.Context, workflow.WorkLease) (workflow.WorkDecision, error) {
				return decision, nil
			}), newManualClock(now))
			if err := worker.Handle(context.Background(), mustWorkerLease(t, now, "work-1", "")); err != nil {
				t.Fatalf("handle work: %v", err)
			}
			if len(store.failures) != 1 || store.failures[0].Disposition() != test.disposition ||
				store.failures[0].Code() != test.decision.Code {
				t.Fatalf("persisted failures = %#v", store.failures)
			}
		})
	}
}

func TestWorkerHandleRenewsAndFinalizesWithTheCurrentLease(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	clock := newManualClock(now)
	started := make(chan struct{})
	release := make(chan struct{})
	renewed := mustWorkerLeaseAt(t, now.Add(20*time.Second), "work-1", "", 1, now.Add(80*time.Second))
	store := &workerStore{renewLease: renewed}
	processor := processorFunc(func(context.Context, workflow.WorkLease) (workflow.WorkDecision, error) {
		close(started)
		<-release
		return workflow.NewWorkDecision(workflow.WorkDecisionSpec{Kind: workflow.WorkComplete})
	})
	worker := mustWorker(t, store, processor, clock)
	done := make(chan error, 1)
	go func() { done <- worker.Handle(context.Background(), mustWorkerLease(t, now, "work-1", "")) }()
	receiveWithin(t, started)
	receiveWithin(t, clock.ready)
	clock.FireNext(now.Add(20 * time.Second))
	waitUntil(t, func() bool {
		store.mu.Lock()
		renewals := store.renewals
		store.mu.Unlock()
		return renewals == 1
	})
	close(release)
	if err := receiveWithin(t, done); err != nil {
		t.Fatalf("handle renewed work: %v", err)
	}
	if len(store.completions) != 1 || store.completions[0].Token() != renewed.Token() {
		t.Fatalf("renewed completion = %#v", store.completions)
	}
}

func TestWorkerHandleLeavesFailedOrInvalidProcessingForRecovery(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	processorFailure := errors.New("processor failure")
	tests := []struct {
		name      string
		processor workflow.WorkProcessor
		want      error
	}{
		{name: "processor error", processor: processorFunc(func(context.Context, workflow.WorkLease) (workflow.WorkDecision, error) {
			return workflow.WorkDecision{}, processorFailure
		}), want: processorFailure},
		{name: "invalid decision", processor: processorFunc(func(context.Context, workflow.WorkLease) (workflow.WorkDecision, error) {
			return workflow.WorkDecision{}, nil
		}), want: workflow.ErrInvalidWorker},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &workerStore{}
			worker := mustWorker(t, store, test.processor, newManualClock(now))
			if err := worker.Handle(context.Background(), mustWorkerLease(t, now, "work-1", "")); !errors.Is(err, test.want) {
				t.Fatalf("handle error = %v", err)
			}
			if len(store.completions) != 0 || len(store.failures) != 0 {
				t.Fatal("failed processing acknowledged durable work")
			}
		})
	}
	worker := mustWorker(t, &workerStore{}, &countingProcessor{}, newManualClock(now))
	if err := (*workflow.Worker)(nil).Handle(context.Background(), mustWorkerLease(t, now, "work-1", "")); !errors.Is(err, workflow.ErrInvalidWorker) {
		t.Fatalf("nil worker handle = %v", err)
	}
	if err := worker.Handle(nil, mustWorkerLease(t, now, "work-1", "")); !errors.Is(err, workflow.ErrInvalidWorker) {
		t.Fatalf("nil context handle = %v", err)
	}
	if err := worker.Handle(context.Background(), workflow.WorkLease{}); !errors.Is(err, workflow.ErrInvalidWorker) {
		t.Fatalf("invalid lease handle = %v", err)
	}
}

func TestWorkerHandleCancellationPreservesOnlyKnownDecisions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		result     workflow.WorkDecision
		resultErr  error
		want       error
		completion bool
	}{
		{name: "processor canceled", resultErr: context.Canceled, want: context.Canceled},
		{name: "invalid known result", want: workflow.ErrInvalidWorker},
		{name: "known completion", result: mustWorkDecision(t, workflow.WorkDecisionSpec{Kind: workflow.WorkComplete}), completion: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			processor := processorFunc(func(ctx context.Context, _ workflow.WorkLease) (workflow.WorkDecision, error) {
				close(started)
				<-ctx.Done()
				return test.result, test.resultErr
			})
			store := &workerStore{}
			worker := mustWorker(t, store, processor, newManualClock(now))
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- worker.Handle(ctx, mustWorkerLease(t, now, "work-1", "")) }()
			receiveWithin(t, started)
			cancel()
			err := receiveWithin(t, done)
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("canceled handle error = %v", err)
			}
			if test.want == nil && err != nil {
				t.Fatalf("known completion error = %v", err)
			}
			if (len(store.completions) == 1) != test.completion {
				t.Fatalf("completion count = %d", len(store.completions))
			}
		})
	}
}

func TestWorkerHandleRejectsInvalidRenewalAndFinalizationTimes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	clock := newManualClock(now)
	processor := &cancelAwareProcessor{started: make(chan struct{}), canceled: make(chan struct{})}
	worker := mustWorker(t, &workerStore{}, processor, clock)
	done := make(chan error, 1)
	go func() { done <- worker.Handle(context.Background(), mustWorkerLease(t, now, "work-1", "")) }()
	receiveWithin(t, processor.started)
	receiveWithin(t, clock.ready)
	clock.FireNext(time.Time{})
	if err := receiveWithin(t, done); !errors.Is(err, workflow.ErrInvalidWorkLease) {
		t.Fatalf("invalid renewal time = %v", err)
	}

	tests := []struct {
		name     string
		clock    workflow.Clock
		decision workflow.WorkDecision
	}{
		{name: "complete zero time", clock: newManualClock(time.Time{}), decision: mustWorkDecision(t, workflow.WorkDecisionSpec{Kind: workflow.WorkComplete})},
		{name: "retry elapsed", clock: newManualClock(now), decision: mustWorkDecision(t, workflow.WorkDecisionSpec{
			Kind: workflow.WorkRetryDecision, Code: "temporary", RetryAt: now,
		})},
		{name: "dead letter zero time", clock: newManualClock(time.Time{}), decision: mustWorkDecision(t, workflow.WorkDecisionSpec{
			Kind: workflow.WorkDeadLetterDecision, Code: "poison",
		})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			worker := mustWorker(t, &workerStore{}, processorFunc(func(context.Context, workflow.WorkLease) (workflow.WorkDecision, error) {
				return test.decision, nil
			}), test.clock)
			if err := worker.Handle(context.Background(), mustWorkerLease(t, now, "work-1", "")); !errors.Is(err, workflow.ErrInvalidWorkLease) {
				t.Fatalf("invalid finalization time = %v", err)
			}
		})
	}
}

func TestWorkerHooksObserveClaimProcessingAndLeaseLoss(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	t.Run("successful processing", func(t *testing.T) {
		hooks := newRecordingHooks()
		store := &workerStore{}
		processor := processorFunc(func(context.Context, workflow.WorkLease) (workflow.WorkDecision, error) {
			return mustWorkDecision(t, workflow.WorkDecisionSpec{Kind: workflow.WorkComplete}), nil
		})
		worker := mustWorkerWithHooks(t, store, processor, newManualClock(now), hooks)
		if err := worker.Handle(context.Background(), mustWorkerLease(t, now, "work-1", "tenant-1")); err != nil {
			t.Fatalf("handle work: %v", err)
		}
		started := receiveWithin(t, hooks.channel)
		completed := receiveWithin(t, hooks.channel)
		if started.Kind() != workflow.WorkerProcessingStarted ||
			completed.Kind() != workflow.WorkerCompleted ||
			started.WorkKind() != workflow.WorkActivity || started.Attempt() != 1 ||
			started.WorkID() != "work-1" || completed.WorkID() != "work-1" {
			t.Fatalf("successful lifecycle hooks = %#v %#v", started, completed)
		}
	})

	t.Run("lease heartbeat", func(t *testing.T) {
		clock := newManualClock(now)
		hooks := newRecordingHooks()
		release := make(chan struct{})
		processor := processorFunc(func(context.Context, workflow.WorkLease) (workflow.WorkDecision, error) {
			<-release
			return mustWorkDecision(t, workflow.WorkDecisionSpec{Kind: workflow.WorkComplete}), nil
		})
		store := &workerStore{renewLease: mustWorkerLeaseAttemptAt(
			t, now.Add(20*time.Second), "work-1", "", 1, 1, now.Add(80*time.Second),
		)}
		worker := mustWorkerWithHooks(t, store, processor, clock, hooks)
		done := make(chan error, 1)
		go func() { done <- worker.Handle(context.Background(), mustWorkerLease(t, now, "work-1", "")) }()
		started := receiveWithin(t, hooks.channel)
		receiveWithin(t, clock.ready)
		clock.FireDuration(20*time.Second, now.Add(20*time.Second))
		renewed := receiveWithin(t, hooks.channel)
		close(release)
		completed := receiveWithin(t, hooks.channel)
		if err := receiveWithin(t, done); err != nil {
			t.Fatalf("handle renewed work: %v", err)
		}
		if started.Kind() != workflow.WorkerProcessingStarted ||
			renewed.Kind() != workflow.WorkerLeaseRenewed ||
			completed.Kind() != workflow.WorkerCompleted {
			t.Fatalf("heartbeat hooks = %#v %#v %#v", started, renewed, completed)
		}
	})

	t.Run("retry and dead letter", func(t *testing.T) {
		tests := []struct {
			name     string
			decision workflow.WorkDecisionSpec
			want     workflow.WorkerEventKind
		}{
			{name: "retry", decision: workflow.WorkDecisionSpec{
				Kind: workflow.WorkRetryDecision, Code: "temporary", RetryAt: now.Add(time.Minute),
			}, want: workflow.WorkerRetryScheduled},
			{name: "dead letter", decision: workflow.WorkDecisionSpec{
				Kind: workflow.WorkDeadLetterDecision, Code: "poison",
			}, want: workflow.WorkerDeadLettered},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				hooks := newRecordingHooks()
				processor := processorFunc(func(context.Context, workflow.WorkLease) (workflow.WorkDecision, error) {
					return mustWorkDecision(t, test.decision), nil
				})
				worker := mustWorkerWithHooks(t, &workerStore{}, processor, newManualClock(now), hooks)
				if err := worker.Handle(context.Background(), mustWorkerLease(t, now, "work-1", "")); err != nil {
					t.Fatalf("handle work: %v", err)
				}
				_ = receiveWithin(t, hooks.channel)
				finalized := receiveWithin(t, hooks.channel)
				if finalized.Kind() != test.want || finalized.Cause() != nil {
					t.Fatalf("finalization hook = %#v", finalized)
				}
			})
		}
	})

	t.Run("failed finalization emits no success", func(t *testing.T) {
		failure := errors.New("store failure")
		tests := []struct {
			name        string
			decision    workflow.WorkDecisionSpec
			completeErr error
			failErr     error
		}{
			{name: "completion", decision: workflow.WorkDecisionSpec{Kind: workflow.WorkComplete}, completeErr: failure},
			{name: "retry", decision: workflow.WorkDecisionSpec{
				Kind: workflow.WorkRetryDecision, Code: "temporary", RetryAt: now.Add(time.Minute),
			}, failErr: failure},
			{name: "dead letter", decision: workflow.WorkDecisionSpec{
				Kind: workflow.WorkDeadLetterDecision, Code: "poison",
			}, failErr: failure},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				hooks := newRecordingHooks()
				store := &workerStore{completeErr: test.completeErr, failErr: test.failErr}
				processor := processorFunc(func(context.Context, workflow.WorkLease) (workflow.WorkDecision, error) {
					return mustWorkDecision(t, test.decision), nil
				})
				worker := mustWorkerWithHooks(t, store, processor, newManualClock(now), hooks)
				err := worker.Handle(context.Background(), mustWorkerLease(t, now, "work-1", ""))
				if !errors.Is(err, failure) {
					t.Fatalf("finalization error = %v", err)
				}
				if started := receiveWithin(t, hooks.channel); started.Kind() != workflow.WorkerProcessingStarted {
					t.Fatalf("processing start hook = %#v", started)
				}
				select {
				case event := <-hooks.channel:
					t.Fatalf("failed finalization emitted success hook %#v", event)
				default:
				}
			})
		}
	})

	t.Run("readmission", func(t *testing.T) {
		hooks := newRecordingHooks()
		lease := mustWorkerLeaseAttemptAt(t, now, "work-1", "", 2, 2, now.Add(time.Minute))
		store := &workerStore{claims: [][]workflow.WorkLease{{lease}}}
		processor := processorFunc(func(context.Context, workflow.WorkLease) (workflow.WorkDecision, error) {
			return mustWorkDecision(t, workflow.WorkDecisionSpec{Kind: workflow.WorkComplete}), nil
		})
		worker := mustWorkerWithHooks(t, store, processor, newManualClock(now), hooks)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- worker.Run(ctx) }()
		readmitted := receiveWithin(t, hooks.channel)
		started := receiveWithin(t, hooks.channel)
		completed := receiveWithin(t, hooks.channel)
		cancel()
		if err := receiveWithin(t, done); err != nil {
			t.Fatalf("stop readmitted worker: %v", err)
		}
		if readmitted.Kind() != workflow.WorkerWorkReadmitted || readmitted.Attempt() != 2 ||
			started.Kind() != workflow.WorkerProcessingStarted ||
			completed.Kind() != workflow.WorkerCompleted {
			t.Fatalf("readmission hooks = %#v %#v %#v", readmitted, started, completed)
		}
	})

	t.Run("claim", func(t *testing.T) {
		clock := newManualClock(now)
		hooks := newRecordingHooks()
		worker := mustWorkerWithHooks(t, &workerStore{claimErr: errors.New("claim failed")}, &countingProcessor{}, clock, hooks)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- worker.Run(ctx) }()
		event := receiveWithin(t, hooks.channel)
		cancel()
		if err := receiveWithin(t, done); err != nil {
			t.Fatalf("stop claim worker: %v", err)
		}
		if event.Kind() != workflow.WorkerClaimFailed || event.WorkID() != "" ||
			event.At() != now || event.Cause() == nil {
			t.Fatalf("claim hook = %#v", event)
		}
	})

	t.Run("processing", func(t *testing.T) {
		hooks := newRecordingHooks()
		ctx, cancel := context.WithCancel(context.Background())
		processorFailure := errors.New("processor failed")
		processor := processorFunc(func(context.Context, workflow.WorkLease) (workflow.WorkDecision, error) {
			return workflow.WorkDecision{}, processorFailure
		})
		store := &workerStore{claims: [][]workflow.WorkLease{{mustWorkerLease(t, now, "work-1", "")}}}
		worker := mustWorkerWithHooks(t, store, processor, newManualClock(now), hooks)
		done := make(chan error, 1)
		go func() { done <- worker.Run(ctx) }()
		claimed := receiveWithin(t, hooks.channel)
		started := receiveWithin(t, hooks.channel)
		event := receiveWithin(t, hooks.channel)
		cancel()
		if err := receiveWithin(t, done); err != nil {
			t.Fatalf("run processing worker: %v", err)
		}
		if claimed.Kind() != workflow.WorkerWorkClaimed ||
			started.Kind() != workflow.WorkerProcessingStarted ||
			event.Kind() != workflow.WorkerProcessingFailed || event.WorkID() != "work-1" ||
			event.WorkKind() != workflow.WorkActivity || event.Attempt() != 1 ||
			!errors.Is(event.Cause(), processorFailure) {
			t.Fatalf("processing hooks = %#v %#v %#v", claimed, started, event)
		}
	})

	t.Run("lease lost", func(t *testing.T) {
		clock := newManualClock(now)
		hooks := newRecordingHooks()
		processor := &cancelAwareProcessor{started: make(chan struct{}), canceled: make(chan struct{})}
		store := &workerStore{
			claims:   [][]workflow.WorkLease{{mustWorkerLease(t, now, "work-1", "")}},
			renewErr: workflow.ErrStaleWorkLease,
		}
		worker := mustWorkerWithHooks(t, store, processor, clock, hooks)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- worker.Run(ctx) }()
		receiveWithin(t, processor.started)
		receiveWithin(t, clock.ready)
		clock.FireDuration(20*time.Second, now.Add(20*time.Second))
		claimed := receiveWithin(t, hooks.channel)
		started := receiveWithin(t, hooks.channel)
		event := receiveWithin(t, hooks.channel)
		cancel()
		if err := receiveWithin(t, done); err != nil {
			t.Fatalf("stop stale worker: %v", err)
		}
		if claimed.Kind() != workflow.WorkerWorkClaimed ||
			started.Kind() != workflow.WorkerProcessingStarted ||
			event.Kind() != workflow.WorkerLeaseLost || event.WorkID() != "work-1" ||
			!errors.Is(event.Cause(), workflow.ErrStaleWorkLease) {
			t.Fatalf("lease-lost hooks = %#v %#v %#v", claimed, started, event)
		}
	})
}

func mustWorkDecision(t *testing.T, spec workflow.WorkDecisionSpec) workflow.WorkDecision {
	t.Helper()
	decision, err := workflow.NewWorkDecision(spec)
	if err != nil {
		t.Fatalf("construct work decision: %v", err)
	}
	return decision
}

func receiveWithin[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for asynchronous result")
		var zero T
		return zero
	}
}

func runWorkerWithin(t *testing.T, worker *workflow.Worker, ctx context.Context) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	return receiveWithin(t, done)
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for asynchronous condition")
		case <-ticker.C:
		}
	}
}

func mustWorker(t *testing.T, store workflow.WorkStore, processor workflow.WorkProcessor, clock workflow.Clock) *workflow.Worker {
	return mustWorkerWithHooks(t, store, processor, clock, nil)
}

func mustWorkerWithHooks(
	t *testing.T,
	store workflow.WorkStore,
	processor workflow.WorkProcessor,
	clock workflow.Clock,
	hooks workflow.WorkerHooks,
) *workflow.Worker {
	t.Helper()
	worker, err := workflow.NewWorker(workflow.WorkerConfig{
		Store: store, Processor: processor, Clock: clock, Owner: "worker-1",
		MaxConcurrent: 2, ClaimLimit: 2, LeaseDuration: time.Minute,
		RenewEvery: 20 * time.Second, PollInterval: time.Second, FinalizeTimeout: time.Second,
		Hooks: hooks,
	})
	if err != nil {
		t.Fatalf("construct worker: %v", err)
	}
	return worker
}

func mustWorkerLease(t *testing.T, now time.Time, id, tenant string) workflow.WorkLease {
	return mustWorkerLeaseAt(t, now, id, tenant, 1, now.Add(time.Minute))
}

func mustWorkerLeaseAt(t *testing.T, now time.Time, id, tenant string, token uint64, expiresAt time.Time) workflow.WorkLease {
	return mustWorkerLeaseAttemptAt(t, now, id, tenant, token, 1, expiresAt)
}

func mustWorkerLeaseAttemptAt(
	t *testing.T,
	now time.Time,
	id string,
	tenant string,
	token uint64,
	attempt uint32,
	expiresAt time.Time,
) workflow.WorkLease {
	t.Helper()
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: id, Kind: workflow.WorkActivity, InstanceID: "instance-" + id,
		Sequence: 1, AvailableAt: now, Deadline: now.Add(time.Hour), TenantID: tenant,
	})
	if err != nil {
		t.Fatalf("construct worker work: %v", err)
	}
	lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: token, Attempt: attempt,
		ClaimedAt: now, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("construct worker lease: %v", err)
	}
	return lease
}

type workerStore struct {
	mu               sync.Mutex
	claims           [][]workflow.WorkLease
	claimLimits      []uint32
	completions      []workflow.WorkCompletion
	renewals         int
	renewErr         error
	renewLease       workflow.WorkLease
	failures         []workflow.WorkFailure
	completeErr      error
	failErr          error
	claimErr         error
	ignoreClaimLimit bool
}

func (store *workerStore) Claim(_ context.Context, request workflow.WorkClaimRequest) ([]workflow.WorkLease, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.claimLimits = append(store.claimLimits, request.Limit())
	if store.claimErr != nil {
		return nil, store.claimErr
	}
	if len(store.claims) == 0 {
		return nil, nil
	}
	leases := store.claims[0]
	if store.ignoreClaimLimit {
		store.claims = store.claims[1:]
		return leases, nil
	}
	if len(leases) <= int(request.Limit()) {
		store.claims = store.claims[1:]
		return leases, nil
	}
	claimed := append([]workflow.WorkLease(nil), leases[:request.Limit()]...)
	store.claims[0] = leases[request.Limit():]
	return claimed, nil
}

func (store *workerStore) Renew(context.Context, workflow.WorkLeaseRenewal) (workflow.WorkLease, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.renewals++
	return store.renewLease, store.renewErr
}

func (store *workerStore) Complete(_ context.Context, completion workflow.WorkCompletion) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.completions = append(store.completions, completion)
	return store.completeErr
}

func (store *workerStore) Fail(_ context.Context, failure workflow.WorkFailure) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.failures = append(store.failures, failure)
	return store.failErr
}

type processorFunc func(context.Context, workflow.WorkLease) (workflow.WorkDecision, error)

func (processor processorFunc) Process(ctx context.Context, lease workflow.WorkLease) (workflow.WorkDecision, error) {
	return processor(ctx, lease)
}

type countingProcessor struct {
	mu        sync.Mutex
	active    int
	maximum   int
	completed int
	target    int
	cancel    context.CancelFunc
}

func (processor *countingProcessor) Process(context.Context, workflow.WorkLease) (workflow.WorkDecision, error) {
	processor.mu.Lock()
	processor.active++
	if processor.active > processor.maximum {
		processor.maximum = processor.active
	}
	processor.mu.Unlock()
	time.Sleep(time.Millisecond)
	processor.mu.Lock()
	processor.active--
	processor.completed++
	completed := processor.completed
	processor.mu.Unlock()
	if completed == processor.target && processor.cancel != nil {
		processor.cancel()
	}
	return workflow.NewWorkDecision(workflow.WorkDecisionSpec{Kind: workflow.WorkComplete})
}

type cancelAwareProcessor struct {
	started  chan struct{}
	canceled chan struct{}
}

func (processor *cancelAwareProcessor) Process(ctx context.Context, _ workflow.WorkLease) (workflow.WorkDecision, error) {
	close(processor.started)
	<-ctx.Done()
	close(processor.canceled)
	return workflow.WorkDecision{}, ctx.Err()
}

type manualClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*manualTimer
	ready  chan struct{}
}

func newManualClock(now time.Time) *manualClock {
	return &manualClock{now: now, ready: make(chan struct{}, 10)}
}

func (clock *manualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *manualClock) NewTimer(duration time.Duration) workflow.ClockTimer {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	timer := &manualTimer{channel: make(chan time.Time, 1), duration: duration}
	clock.timers = append(clock.timers, timer)
	clock.ready <- struct{}{}
	return timer
}

func (clock *manualClock) FireNext(now time.Time) {
	clock.mu.Lock()
	clock.now = now
	timer := clock.timers[0]
	clock.timers = clock.timers[1:]
	clock.mu.Unlock()
	timer.channel <- now
}

func (clock *manualClock) FireDuration(duration time.Duration, now time.Time) {
	clock.mu.Lock()
	clock.now = now
	for index, timer := range clock.timers {
		if timer.duration == duration {
			clock.timers = append(clock.timers[:index], clock.timers[index+1:]...)
			clock.mu.Unlock()
			timer.channel <- now
			return
		}
	}
	clock.mu.Unlock()
	panic("manual timer duration not found")
}

type manualTimer struct {
	channel  chan time.Time
	duration time.Duration
}

func (timer *manualTimer) C() <-chan time.Time { return timer.channel }
func (*manualTimer) Stop() bool                { return true }

type recordingHooks struct {
	mu      sync.Mutex
	events  []workflow.WorkerEvent
	channel chan workflow.WorkerEvent
}

func newRecordingHooks() *recordingHooks {
	return &recordingHooks{channel: make(chan workflow.WorkerEvent, 10)}
}

func (hooks *recordingHooks) OnWorkerEvent(event workflow.WorkerEvent) {
	hooks.mu.Lock()
	hooks.events = append(hooks.events, event)
	hooks.mu.Unlock()
	hooks.channel <- event
}
