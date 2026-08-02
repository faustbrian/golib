package resilience_test

import (
	"context"
	"errors"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
	"github.com/faustbrian/golib/pkg/retry"
)

var errCampaign = errors.New("injected campaign failure")

func TestInjectorDrivesRetryRecoveryWithoutProductionDependency(t *testing.T) {
	t.Parallel()

	injector := firstCallInjector(t)
	policy, err := retry.NewPolicy(retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 2,
		Clock: retry.SystemClock{}, Sleeper: retry.SystemSleeper{},
		Classifier: retry.RetryableClassifier(),
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	value, result, err := retry.Do(context.Background(), policy, func(ctx context.Context) (string, error) {
		calls++
		value, runErr := faultinject.Run(ctx, injector,
			faultinject.Metadata{Boundary: faultinject.BoundaryFunction},
			func(context.Context) (string, error) { return "recovered", nil },
		)
		if runErr != nil {
			return "", retry.Retryable(runErr)
		}
		return value, nil
	})
	if err != nil || value != "recovered" || calls != 2 || result.Attempts != 2 || result.Reason != retry.ReasonSucceeded {
		t.Fatalf("retry campaign = %q, %+v, %v, calls=%d", value, result, err, calls)
	}
}

func TestInjectorDrivesCircuitBreakerOpeningWithoutProductionDependency(t *testing.T) {
	t.Parallel()

	injector := firstCallInjector(t)
	circuit, err := breaker.New(breaker.Config{
		Name: "fault-campaign", Window: breaker.CountWindow{Size: 1},
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureRatio: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Minute),
		HalfOpen:          &breaker.HalfOpenPolicy{MaxProbes: 1, RequiredSuccesses: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, firstErr := breaker.Execute(context.Background(), circuit, func(ctx context.Context) (struct{}, error) {
		return faultinject.Run(ctx, injector,
			faultinject.Metadata{Boundary: faultinject.BoundaryFunction},
			func(context.Context) (struct{}, error) { return struct{}{}, nil },
		)
	})
	if !errors.Is(firstErr, errCampaign) {
		t.Fatalf("first breaker campaign error = %v", firstErr)
	}
	if _, secondErr := breaker.Execute(context.Background(), circuit, func(context.Context) (struct{}, error) {
		t.Fatal("open breaker called the protected operation")
		return struct{}{}, nil
	}); !errors.Is(secondErr, breaker.ErrOpen) {
		t.Fatalf("second breaker campaign error = %v", secondErr)
	}
	if snapshot := injector.Snapshot(); snapshot.Injections != 1 {
		t.Fatalf("injector campaign snapshot = %+v", snapshot)
	}
}

func firstCallInjector(t testing.TB) *faultinject.Injector {
	t.Helper()
	injector, err := faultinject.New(faultinject.Config{Rules: []faultinject.Rule{{
		ID: "first-call", Scope: faultinject.BoundaryFunction,
		Activation: faultinject.Active, Maximum: 1,
		Terminal: faultinject.Continue, Observation: faultinject.Suppress,
		Schedule: faultinject.Nth(1),
		Faults: []faultinject.Fault{
			faultinject.ErrorFault(faultinject.PhaseBefore, errCampaign),
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return injector
}
