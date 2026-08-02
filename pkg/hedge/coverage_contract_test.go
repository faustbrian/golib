package hedge_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

func TestDoRejectsMissingRuntimeDependencies(t *testing.T) {
	t.Parallel()

	policy, err := hedge.NewPolicy(validConfig())
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(context.Context) (string, error) { return "", nil }, "", nil
	})
	for name, run := range map[string]func() error{
		"nil context": func() error {
			//lint:ignore SA1012 This test verifies the documented rejection path.
			_, _, err := hedge.Do[string](nil, policy, factory) //nolint:staticcheck // verifies nil rejection
			return err
		},
		"nil policy": func() error {
			_, _, err := hedge.Do(context.Background(), (*hedge.Policy[string])(nil), factory)
			return err
		},
		"nil factory": func() error { _, _, err := hedge.Do[string](context.Background(), policy, nil); return err },
	} {
		if err := run(); !errors.Is(err, hedge.ErrInvalidPolicy) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	if err := (hedge.Report{}).Wait(context.Background()); err != nil {
		t.Fatalf("zero Report.Wait() = %v", err)
	}
}

func TestFactoryOriginalFailuresAreBounded(t *testing.T) {
	t.Parallel()

	policy, err := hedge.NewPolicy(validConfig())
	if err != nil {
		t.Fatal(err)
	}
	factoryErr := errors.New("private factory detail")
	tests := map[string]hedge.AttemptFactoryFunc[string]{
		"error":       func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) { return nil, "", factoryErr },
		"nil attempt": func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) { return nil, "pod", nil },
		"long endpoint": func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
			return func(context.Context) (string, error) { return "", nil }, strings.Repeat("x", hedge.MaxResourceLength+1), nil
		},
		"panic": func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) { panic("private") },
	}
	for name, factory := range tests {
		_, report, gotErr := hedge.Do(context.Background(), policy, factory)
		if gotErr == nil || report.Reason != hedge.ReasonFactoryFailure || report.AttemptsStarted != 0 || gotErr.Error() != "hedge: all attempts failed" {
			t.Fatalf("%s result = (%+v, %v)", name, report, gotErr)
		}
	}
}

func TestAttemptAndClassifierExceptionalResultsAreTerminal(t *testing.T) {
	t.Parallel()

	tests := map[string]hedge.Classifier[string]{
		"invalid": hedge.ClassifyFunc[string](func(context.Context, hedge.AttemptResult[string]) (hedge.Classification, error) {
			return hedge.Classification(255), nil
		}),
		"error": hedge.ClassifyFunc[string](func(context.Context, hedge.AttemptResult[string]) (hedge.Classification, error) {
			return hedge.ClassificationFailure, errors.New("classifier")
		}),
		"terminal": hedge.ClassifyFunc[string](func(context.Context, hedge.AttemptResult[string]) (hedge.Classification, error) {
			return hedge.ClassificationTerminal, nil
		}),
	}
	for name, classifier := range tests {
		config := validConfig()
		config.Classifier = classifier
		policy, err := hedge.NewPolicy(config)
		if err != nil {
			t.Fatal(err)
		}
		factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
			return func(context.Context) (string, error) { return "partial", errors.New("downstream") }, "pod", nil
		})
		value, report, gotErr := hedge.Do(context.Background(), policy, factory)
		if gotErr == nil || report.Reason != hedge.ReasonTerminalFailure || value != "partial" {
			t.Fatalf("%s result = (%q, %+v, %v)", name, value, report, gotErr)
		}
	}

	config := validConfig()
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	panicFactory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(context.Context) (string, error) { panic("private operation detail") }, "pod", nil
	})
	_, report, gotErr := hedge.Do(context.Background(), policy, panicFactory)
	if gotErr == nil || report.Reason != hedge.ReasonAllAttemptsFailed {
		t.Fatalf("panic result = (%+v, %v)", report, gotErr)
	}
}

type countingPermit struct{ releases atomic.Uint32 }

func (permit *countingPermit) Release() { permit.releases.Add(1) }

type onePermitBudget struct{ permit *countingPermit }

func (onePermitBudget) Capacity() uint                                { return 1 }
func (budget onePermitBudget) TryAcquire(string) (hedge.Permit, bool) { return budget.permit, true }

func TestInvalidHedgeFactoryMetadataReleasesPermit(t *testing.T) {
	t.Parallel()

	permit := &countingPermit{}
	config := validConfig()
	config.Delay = time.Millisecond
	config.Budget = onePermitBudget{permit: permit}
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		if info.Hedge {
			return nil, "pod", nil
		}
		return func(ctx context.Context) (string, error) { <-ctx.Done(); return "original", ctx.Err() }, "pod", nil
	})
	_, report, gotErr := hedge.Do(context.Background(), policy, factory)
	if gotErr == nil || report.Reason != hedge.ReasonFactoryFailure || permit.releases.Load() != 1 {
		t.Fatalf("Do() = (%+v, %v), releases=%d", report, gotErr, permit.releases.Load())
	}
}

func TestDynamicDelayCanFailAfterStartingAHedge(t *testing.T) {
	t.Parallel()

	clock := newManualClock()
	config := validConfig()
	config.Clock = clock
	config.MaxHedges = 2
	config.Delay = 0
	config.DynamicDelay = func(input hedge.DelayInput) (time.Duration, error) {
		if input.Hedge == 1 {
			return time.Millisecond, nil
		}
		return 0, errors.New("second delay")
	}
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 2)
	factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		started <- struct{}{}
		return func(ctx context.Context) (string, error) { <-ctx.Done(); return "resource", ctx.Err() }, "pod", nil
	})
	done := make(chan hedge.Report, 1)
	go func() { _, report, _ := hedge.Do(context.Background(), policy, factory); done <- report }()
	<-started
	clock.WaitTimers(2)
	clock.Advance(time.Millisecond)
	<-started
	report := <-done
	if report.Reason != hedge.ReasonDelayFailure || report.HedgesStarted != 1 {
		t.Fatalf("report = %+v", report)
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestErrorMessagesAndNilBudgetMethods(t *testing.T) {
	t.Parallel()

	var budget *hedge.OutstandingBudget
	if permit, ok := budget.TryAcquire("resource"); ok || permit != nil || budget.Outstanding() != 0 {
		t.Fatal("nil budget admitted work")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	policy, _ := hedge.NewPolicy(validConfig())
	factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) { return nil, "", nil })
	_, _, canceled := hedge.Do(ctx, policy, factory)
	if canceled.Error() != "hedge: caller canceled" {
		t.Fatalf("canceled error = %q", canceled)
	}
	deadline := &hedge.DeadlineError{}
	if deadline.Error() != "hedge: total deadline exceeded" || !errors.Is(deadline, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", deadline)
	}
}

func TestCancellationObservedThroughDerivedTotalContext(t *testing.T) {
	t.Parallel()

	parent := &delayedCanceledContext{done: make(chan struct{})}
	clock := &cancelingClock{parent: parent}
	config := validConfig()
	config.Clock = clock
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(ctx context.Context) (string, error) { <-ctx.Done(); return "resource", ctx.Err() }, "pod", nil
	})
	_, report, gotErr := hedge.Do(parent, policy, factory)
	if !errors.Is(gotErr, context.Canceled) || report.Reason != hedge.ReasonCallerCanceled {
		t.Fatalf("Do() = (%+v, %v)", report, gotErr)
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type delayedCanceledContext struct {
	done     chan struct{}
	canceled atomic.Bool
}

func (*delayedCanceledContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx *delayedCanceledContext) Done() <-chan struct{}   { return ctx.done }
func (ctx *delayedCanceledContext) Err() error {
	if ctx.canceled.Load() {
		return context.Canceled
	}
	return nil
}
func (*delayedCanceledContext) Value(any) any { return nil }

type cancelingClock struct {
	hedge.RealClock
	parent *delayedCanceledContext
	used   atomic.Bool
}

func (clock *cancelingClock) WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if clock.used.CompareAndSwap(false, true) {
		clock.parent.canceled.Store(true)
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx, func() {}
	}
	return clock.RealClock.WithTimeout(parent, timeout)
}
