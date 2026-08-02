package hedge_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

func TestOutcomeLabelsAreBounded(t *testing.T) {
	t.Parallel()

	want := map[hedge.Outcome]string{
		hedge.OutcomeNoHedgeNeeded:     "no_hedge_needed",
		hedge.OutcomeHedgeStarted:      "hedge_started",
		hedge.OutcomeBudgetDenied:      "budget_denied",
		hedge.OutcomeWinnerSelected:    "winner_selected",
		hedge.OutcomeAllAttemptsFailed: "all_attempts_failed",
		hedge.OutcomeCallerCanceled:    "caller_canceled",
		hedge.OutcomeTotalDeadline:     "total_deadline",
		hedge.OutcomeAttemptCompleted:  "attempt_completed",
		hedge.OutcomeCleanupFailed:     "cleanup_failed",
		hedge.Outcome(255):             "unknown",
	}
	for outcome, label := range want {
		if got := outcome.String(); got != label {
			t.Fatalf("Outcome(%d).String() = %q, want %q", outcome, got, label)
		}
	}
}

func TestObserverReceivesBoundedLifecycleWithoutAffectingWinner(t *testing.T) {
	t.Parallel()

	collector := &observationCollector{}
	config := validConfig()
	config.Delay = time.Millisecond
	config.Observer = collector
	config.Disposer = hedge.DisposeFunc[string](func(context.Context, string) error { return errors.New("cleanup") })
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
	value, report, gotErr := hedge.Do(context.Background(), policy, factory)
	if gotErr != nil || value != "winner" {
		t.Fatalf("Do() = (%q, %v)", value, gotErr)
	}
	_ = report.Wait(context.Background())
	collector.mu.Lock()
	defer collector.mu.Unlock()
	seen := map[hedge.Outcome]bool{}
	for _, event := range collector.events {
		seen[event.Outcome] = true
		if event.Resource != "inventory-read" || len(event.Endpoint) > hedge.MaxResourceLength {
			t.Fatalf("unbounded event = %+v", event)
		}
	}
	for _, outcome := range []hedge.Outcome{hedge.OutcomeHedgeStarted, hedge.OutcomeWinnerSelected, hedge.OutcomeCleanupFailed} {
		if !seen[outcome] {
			t.Fatalf("missing outcome %s in %+v", outcome.String(), collector.events)
		}
	}
}

type panicObserver struct{}

func (panicObserver) TryObserve(hedge.Observation) bool { panic("observer") }

func TestObserverPanicDoesNotChangeExecution(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Observer = panicObserver{}
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(context.Context) (string, error) { return "winner", nil }, "pod", nil
	})
	if value, _, err := hedge.Do(context.Background(), policy, factory); err != nil || value != "winner" {
		t.Fatalf("Do() = (%q, %v)", value, err)
	}
}
