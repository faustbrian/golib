package hedge_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

func TestOriginalSuccessBeforeDelayNeedsNoHedge(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Delay = time.Hour
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	var factories atomic.Uint32
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		factories.Add(1)
		if info.Ordinal != 0 || info.Hedge {
			t.Fatalf("original info = %+v", info)
		}
		return func(context.Context) (string, error) { return "original", nil }, "pod-a", nil
	})

	value, report, err := hedge.Do(context.Background(), policy, factory)
	if err != nil || value != "original" {
		t.Fatalf("Do() = (%q, %v), want original success", value, err)
	}
	if factories.Load() != 1 || report.Reason != hedge.ReasonNoHedgeNeeded || report.AttemptsStarted != 1 || report.HedgesStarted != 0 || report.WinnerOrdinal != 0 {
		t.Fatalf("report = %+v, factories = %d", report, factories.Load())
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

func TestDelayedHedgeWinsCancelsAndDisposesOriginal(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Delay = 5 * time.Millisecond
	disposed := make(chan string, 1)
	config.Disposer = hedge.DisposeFunc[string](func(_ context.Context, value string) error {
		disposed <- value
		return nil
	})
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	originalCanceled := make(chan struct{})
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		if info.Ordinal == 0 {
			return func(ctx context.Context) (string, error) {
				<-ctx.Done()
				close(originalCanceled)
				return "original-resource", ctx.Err()
			}, "pod-a", nil
		}
		return func(context.Context) (string, error) { return "hedge", nil }, "pod-b", nil
	})

	value, report, err := hedge.Do(context.Background(), policy, factory)
	if err != nil || value != "hedge" || report.Reason != hedge.ReasonWinnerSelected || report.WinnerOrdinal != 1 || report.HedgesStarted != 1 {
		t.Fatalf("Do() = (%q, %+v, %v)", value, report, err)
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	select {
	case <-originalCanceled:
	default:
		t.Fatal("original attempt was not canceled")
	}
	select {
	case value := <-disposed:
		if value != "original-resource" {
			t.Fatalf("disposed %q", value)
		}
	default:
		t.Fatal("original result was not disposed")
	}
}

func TestAllFailuresSelectOriginalCauseDeterministically(t *testing.T) {
	t.Parallel()

	originalErr := errors.New("original secret-bearing detail")
	hedgeErr := errors.New("hedge secret-bearing detail")
	var disposedMu sync.Mutex
	var disposed []string
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		if info.Ordinal == 0 {
			return func(context.Context) (string, error) {
				time.Sleep(5 * time.Millisecond)
				return "original-partial", originalErr
			}, "pod-a", nil
		}
		return func(context.Context) (string, error) { return "hedge-partial", hedgeErr }, "pod-b", nil
	})
	// The policy owns this disposer; replace it by rebuilding the immutable policy.
	config := validConfig()
	config.Delay = 20 * time.Millisecond
	config.TotalTimeout = 5 * time.Second
	config.Disposer = hedge.DisposeFunc[string](func(_ context.Context, value string) error {
		disposedMu.Lock()
		disposed = append(disposed, value)
		disposedMu.Unlock()
		return nil
	})
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}

	value, report, gotErr := hedge.Do(context.Background(), policy, factory)
	if !errors.Is(gotErr, originalErr) || errors.Is(gotErr, hedgeErr) {
		t.Fatalf("Do() error = %v, want original cause only", gotErr)
	}
	if value != "original-partial" {
		t.Fatalf("Do() value = %q, want selected original partial result", value)
	}
	if gotErr.Error() != "hedge: all attempts failed" {
		t.Fatalf("error leaked raw detail: %q", gotErr)
	}
	if report.Reason != hedge.ReasonAllAttemptsFailed || len(report.Failures) != 2 || report.Failures[0].Ordinal != 0 || report.Failures[1].Ordinal != 1 {
		t.Fatalf("report = %+v", report)
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	disposedMu.Lock()
	defer disposedMu.Unlock()
	if len(disposed) != 1 || disposed[0] != "hedge-partial" {
		t.Fatalf("disposed = %v", disposed)
	}
}

func TestCompletedFailureIsDisposedWhenLaterHedgeWins(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Delay = 20 * time.Millisecond
	config.TotalTimeout = 5 * time.Second
	disposed := make(chan string, 1)
	config.Disposer = hedge.DisposeFunc[string](func(_ context.Context, value string) error {
		disposed <- value
		return nil
	})
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		if info.Ordinal == 0 {
			return func(context.Context) (string, error) { return "failed-resource", errors.New("failed") }, "pod-a", nil
		}
		return func(context.Context) (string, error) { return "winner", nil }, "pod-b", nil
	})
	value, report, gotErr := hedge.Do(context.Background(), policy, factory)
	if gotErr != nil || value != "winner" || len(report.Failures) != 1 {
		t.Fatalf("Do() = (%q, %+v, %v)", value, report, gotErr)
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := <-disposed; got != "failed-resource" {
		t.Fatalf("disposed = %q", got)
	}
}
