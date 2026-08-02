package hedge_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

type signalingDeniedBudget struct{ denied chan struct{} }

func (signalingDeniedBudget) Capacity() uint { return 1 }

func (budget signalingDeniedBudget) TryAcquire(string) (hedge.Permit, bool) {
	select {
	case budget.denied <- struct{}{}:
	default:
	}
	return nil, false
}

func TestBudgetDenialDoesNotBecomeDownstreamFailure(t *testing.T) {
	t.Parallel()

	clock := newManualClock()
	config := validConfig()
	config.Clock = clock
	config.Delay = 10 * time.Millisecond
	denied := make(chan struct{}, 1)
	config.Budget = signalingDeniedBudget{denied: denied}
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(context.Context) (string, error) {
			<-release
			return "original", nil
		}, "pod-a", nil
	})
	type outcome struct {
		value  string
		report hedge.Report
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		value, report, doErr := hedge.Do(context.Background(), policy, factory)
		done <- outcome{value: value, report: report, err: doErr}
	}()
	clock.WaitTimers(2)
	clock.Advance(10 * time.Millisecond)
	<-denied
	close(release)
	result := <-done
	if result.err != nil || result.value != "original" || result.report.BudgetDenied != 1 || result.report.HedgesStarted != 0 || result.report.Reason != hedge.ReasonNoHedgeNeeded {
		t.Fatalf("Do() = (%q, %+v, %v)", result.value, result.report, result.err)
	}
}

func TestTotalDeadlineAndCallerCancellationAreDistinct(t *testing.T) {
	t.Parallel()

	for name, callerCancel := range map[string]bool{"deadline": false, "caller": true} {
		callerCancel := callerCancel
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			clock := newManualClock()
			config := validConfig()
			config.Clock = clock
			config.Delay = time.Hour
			config.TotalTimeout = time.Minute
			policy, err := hedge.NewPolicy(config)
			if err != nil {
				t.Fatal(err)
			}
			factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
				return func(ctx context.Context) (string, error) {
					<-ctx.Done()
					return "canceled-resource", ctx.Err()
				}, "pod-a", nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct {
				report hedge.Report
				err    error
			}, 1)
			go func() {
				_, report, doErr := hedge.Do(ctx, policy, factory)
				done <- struct {
					report hedge.Report
					err    error
				}{report: report, err: doErr}
			}()
			clock.WaitTimers(2)
			if callerCancel {
				cancel()
			} else {
				clock.Advance(time.Minute)
			}
			result := <-done
			if callerCancel {
				if !errors.Is(result.err, context.Canceled) || result.report.Reason != hedge.ReasonCallerCanceled {
					t.Fatalf("caller result = (%+v, %v)", result.report, result.err)
				}
			} else if !errors.Is(result.err, context.DeadlineExceeded) || result.report.Reason != hedge.ReasonTotalDeadline {
				t.Fatalf("deadline result = (%+v, %v)", result.report, result.err)
			}
			if err := result.report.Wait(context.Background()); err != nil {
				t.Fatalf("Wait() error = %v", err)
			}
		})
	}
}

func TestCleanupFailureAndUncooperativeAttemptRemainObservable(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Delay = time.Millisecond
	config.Disposer = hedge.DisposeFunc[string](func(context.Context, string) error {
		return errors.New("private cleanup detail")
	})
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		if info.Ordinal == 0 {
			return func(context.Context) (string, error) {
				<-release
				return "late-resource", errors.New("late")
			}, "pod-a", nil
		}
		return func(context.Context) (string, error) { return "winner", nil }, "pod-b", nil
	})
	_, report, doErr := hedge.Do(context.Background(), policy, factory)
	if doErr != nil {
		t.Fatalf("Do() error = %v", doErr)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if waitErr := report.Wait(waitCtx); !errors.Is(waitErr, context.DeadlineExceeded) {
		t.Fatalf("Wait() before release = %v", waitErr)
	}
	close(release)
	waitErr := report.Wait(context.Background())
	var cleanupErr *hedge.CleanupError
	if waitErr == nil {
		t.Fatal("Wait() after release unexpectedly succeeded")
	}
	if !errors.As(waitErr, &cleanupErr) || cleanupErr.Failures != 1 || waitErr.Error() != "hedge: 1 cleanup operation(s) failed" {
		t.Fatalf("Wait() after release = %v", waitErr)
	}
}

func TestClassifierPanicBecomesBoundedTerminalFailure(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Classifier = hedge.ClassifyFunc[string](func(context.Context, hedge.AttemptResult[string]) (hedge.Classification, error) {
		panic("private classifier detail")
	})
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(context.Context) (string, error) { return "partial", errors.New("downstream") }, "pod-a", nil
	})
	value, report, gotErr := hedge.Do(context.Background(), policy, factory)
	if gotErr == nil || gotErr.Error() != "hedge: all attempts failed" || report.Reason != hedge.ReasonTerminalFailure || value != "partial" {
		t.Fatalf("Do() = (%q, %+v, %v)", value, report, gotErr)
	}
}

func TestAlreadyCanceledCallerStartsNoAttempt(t *testing.T) {
	t.Parallel()

	policy, err := hedge.NewPolicy(validConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		called = true
		return func(context.Context) (string, error) { return "unsafe", nil }, "pod", nil
	})
	_, report, gotErr := hedge.Do(ctx, policy, factory)
	if !errors.Is(gotErr, context.Canceled) || called || report.Reason != hedge.ReasonCallerCanceled || report.AttemptsStarted != 0 {
		t.Fatalf("Do() = (%+v, %v), called = %v", report, gotErr, called)
	}
}

func TestAttemptDeadlineIsClassifiedWithoutBecomingTotalDeadline(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Delay = 10 * time.Millisecond
	config.AttemptTimeout = 100 * time.Millisecond
	config.TotalTimeout = 5 * time.Second
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(ctx context.Context) (string, error) {
			<-ctx.Done()
			return "timed-out", ctx.Err()
		}, "pod", nil
	})
	value, report, gotErr := hedge.Do(context.Background(), policy, factory)
	if !errors.Is(gotErr, context.DeadlineExceeded) || report.Reason != hedge.ReasonAllAttemptsFailed || value != "timed-out" || report.AttemptsStarted != 2 {
		t.Fatalf("Do() = (%q, %+v, %v)", value, report, gotErr)
	}
}

func TestDisposerPanicIsReportedAsCleanupFailure(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Delay = time.Millisecond
	config.Disposer = hedge.DisposeFunc[string](func(context.Context, string) error { panic("private") })
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		if info.Hedge {
			return func(context.Context) (string, error) { return "winner", nil }, "pod-b", nil
		}
		return func(ctx context.Context) (string, error) { <-ctx.Done(); return "loser", ctx.Err() }, "pod-a", nil
	})
	_, report, gotErr := hedge.Do(context.Background(), policy, factory)
	if gotErr != nil {
		t.Fatal(gotErr)
	}
	var cleanupErr *hedge.CleanupError
	if err := report.Wait(context.Background()); !errors.As(err, &cleanupErr) || cleanupErr.Failures != 1 {
		t.Fatalf("Wait() error = %v", err)
	}
}

func TestBlockedDisposerDoesNotDelayWinnerPublication(t *testing.T) {
	t.Parallel()

	releaseCleanup := make(chan struct{})
	cleanupStarted := make(chan struct{}, 1)
	config := validConfig()
	config.Delay = 20 * time.Millisecond
	config.TotalTimeout = 5 * time.Second
	config.Disposer = hedge.DisposeFunc[string](func(context.Context, string) error {
		cleanupStarted <- struct{}{}
		<-releaseCleanup
		return nil
	})
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		if info.Ordinal == 0 {
			return func(context.Context) (string, error) { return "failed", errors.New("failed") }, "pod-a", nil
		}
		return func(context.Context) (string, error) { return "winner", nil }, "pod-b", nil
	})
	type result struct {
		value  string
		report hedge.Report
		err    error
	}
	done := make(chan result, 1)
	go func() {
		value, report, doErr := hedge.Do(context.Background(), policy, factory)
		done <- result{value: value, report: report, err: doErr}
	}()
	select {
	case got := <-done:
		if got.err != nil || got.value != "winner" {
			t.Fatalf("Do() = (%q, %v)", got.value, got.err)
		}
		<-cleanupStarted
		waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		if waitErr := got.report.Wait(waitCtx); !errors.Is(waitErr, context.DeadlineExceeded) {
			t.Fatalf("Wait() = %v", waitErr)
		}
		cancel()
		close(releaseCleanup)
		if waitErr := got.report.Wait(context.Background()); waitErr != nil {
			t.Fatal(waitErr)
		}
	case <-time.After(time.Second):
		t.Fatal("winner was blocked by disposer")
	}
}

func TestPublishedResultAtDelayBoundaryPrecedesAdditionalWork(t *testing.T) {
	t.Parallel()

	for _, originalSucceeds := range []bool{true, false} {
		originalSucceeds := originalSucceeds
		t.Run(map[bool]string{true: "success", false: "failure"}[originalSucceeds], func(t *testing.T) {
			t.Parallel()
			clock := newBoundaryClock()
			published := make(publishedObserver, 1)
			config := validConfig()
			config.Clock = clock
			config.Delay = time.Second
			config.TotalTimeout = 5 * time.Second
			config.Observer = published
			policy, err := hedge.NewPolicy(config)
			if err != nil {
				t.Fatal(err)
			}
			releaseOriginal := make(chan struct{})
			factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
				if info.Ordinal == 0 {
					return func(context.Context) (string, error) {
						<-releaseOriginal
						if originalSucceeds {
							return "original", nil
						}
						return "failed", errors.New("failed")
					}, "pod-a", nil
				}
				return func(context.Context) (string, error) { return "hedge", nil }, "pod-b", nil
			})
			type result struct {
				value  string
				report hedge.Report
				err    error
			}
			done := make(chan result, 1)
			go func() {
				value, report, doErr := hedge.Do(context.Background(), policy, factory)
				done <- result{value: value, report: report, err: doErr}
			}()
			timer := <-clock.created
			timer.events <- time.Now()
			<-timer.stopCalled
			close(releaseOriginal)
			if ordinal := <-published; ordinal != 0 {
				t.Fatalf("published ordinal = %d", ordinal)
			}
			close(timer.releaseStop)
			got := <-done
			if originalSucceeds {
				if got.err != nil || got.value != "original" || got.report.HedgesStarted != 0 {
					t.Fatalf("success result = (%q, %+v, %v)", got.value, got.report, got.err)
				}
			} else if got.err != nil || got.value != "hedge" || got.report.HedgesStarted != 1 || len(got.report.Failures) != 1 {
				t.Fatalf("failure result = (%q, %+v, %v)", got.value, got.report, got.err)
			}
			if err := got.report.Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type publishedObserver chan uint

func (observer publishedObserver) TryObserve(observation hedge.Observation) bool {
	if observation.Outcome == hedge.OutcomeAttemptCompleted && observation.Ordinal == 0 {
		observer <- observation.Ordinal
	}
	return true
}

type boundaryClock struct{ created chan *blockingStopTimer }

func newBoundaryClock() *boundaryClock {
	return &boundaryClock{created: make(chan *blockingStopTimer, 1)}
}

func (*boundaryClock) Now() time.Time { return time.Now() }

func (clock *boundaryClock) NewTimer(time.Duration) hedge.Timer {
	timer := &blockingStopTimer{events: make(chan time.Time, 1), stopCalled: make(chan struct{}), releaseStop: make(chan struct{})}
	clock.created <- timer
	return timer
}

func (*boundaryClock) WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}

type blockingStopTimer struct {
	events      chan time.Time
	stopCalled  chan struct{}
	releaseStop chan struct{}
	once        sync.Once
}

func (timer *blockingStopTimer) C() <-chan time.Time { return timer.events }

func (timer *blockingStopTimer) Stop() bool {
	stopped := false
	timer.once.Do(func() {
		stopped = true
		close(timer.stopCalled)
		<-timer.releaseStop
	})
	return stopped
}
