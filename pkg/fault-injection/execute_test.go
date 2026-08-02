package faultinject_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	faultinject "github.com/faustbrian/golib/pkg/fault-injection"
)

func TestRunAppliesPhasesInOrderWithInjectedSleeper(t *testing.T) {
	t.Parallel()

	sleeper := &recordingSleeper{}
	injector := mustInjector(t, faultinject.Config{
		Sleeper: sleeper,
		Rules: []faultinject.Rule{{
			ID:          "phases",
			Scope:       faultinject.BoundaryFunction,
			Activation:  faultinject.Active,
			Maximum:     1,
			Terminal:    faultinject.Continue,
			Observation: faultinject.Suppress,
			Schedule:    faultinject.Every(1),
			Faults: []faultinject.Fault{
				faultinject.LatencyFault(faultinject.PhaseBefore, time.Millisecond),
				faultinject.LatencyFault(faultinject.PhaseDuring, 2*time.Millisecond),
				faultinject.LatencyFault(faultinject.PhaseAfter, 3*time.Millisecond),
			},
		}},
	})

	value, err := faultinject.Run(context.Background(), injector, faultinject.Metadata{
		Boundary: faultinject.BoundaryFunction,
	}, func(context.Context) (string, error) { return "value", nil })
	if err != nil || value != "value" {
		t.Fatalf("Run() = %q, %v", value, err)
	}
	if !reflect.DeepEqual(sleeper.delays, []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond}) {
		t.Fatalf("sleep delays = %v", sleeper.delays)
	}
}

func TestRunHonorsErrorsCancellationDeadlinesAndPanic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fault     faultinject.Fault
		wantError error
		called    bool
	}{
		{name: "before error", fault: faultinject.ErrorFault(faultinject.PhaseBefore, errInjected), wantError: errInjected},
		{name: "before cancel", fault: faultinject.CancelFault(faultinject.PhaseBefore), wantError: context.Canceled},
		{name: "before deadline", fault: faultinject.DeadlineFault(faultinject.PhaseBefore), wantError: context.DeadlineExceeded},
		{name: "during cancel", fault: faultinject.CancelFault(faultinject.PhaseDuring), wantError: context.Canceled, called: true},
		{name: "during deadline", fault: faultinject.DeadlineFault(faultinject.PhaseDuring), wantError: context.DeadlineExceeded, called: true},
		{name: "after error", fault: faultinject.ErrorFault(faultinject.PhaseAfter, errInjected), wantError: errInjected, called: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			injector := injectorWithFault(t, test.fault)
			called := false
			value, err := faultinject.Run(context.Background(), injector, faultinject.Metadata{Boundary: faultinject.BoundaryFunction}, func(ctx context.Context) (int, error) {
				called = true
				if test.fault.Phase() == faultinject.PhaseDuring {
					return 99, ctx.Err()
				}
				return 99, nil
			})
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Run() error = %v, want %v", err, test.wantError)
			}
			if value != 0 {
				t.Fatalf("Run() value = %d, want zero", value)
			}
			if called != test.called {
				t.Fatalf("operation called = %t, want %t", called, test.called)
			}
		})
	}

	t.Run("panic", func(t *testing.T) {
		t.Parallel()
		injector := injectorWithFault(t, faultinject.PanicFault(faultinject.PhaseBefore, "safe_panic"))
		defer func() {
			if recovered := recover(); recovered != "safe_panic" {
				t.Fatalf("panic = %#v", recovered)
			}
			if injector.Snapshot().Injections != 1 {
				t.Fatal("panic corrupted accounting")
			}
		}()
		_, _ = faultinject.Run(context.Background(), injector, faultinject.Metadata{Boundary: faultinject.BoundaryFunction}, func(context.Context) (int, error) {
			return 1, nil
		})
	})
}

func TestRunPropagatesCallerCancellationDuringInjectedLatency(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	injector := injectorWithConfig(t, faultinject.Config{
		Sleeper: blockingSleeper{},
		Rules:   []faultinject.Rule{ruleWithFault("latency", faultinject.LatencyFault(faultinject.PhaseBefore, time.Second))},
	})
	called := false
	_, err := faultinject.Run(ctx, injector, faultinject.Metadata{Boundary: faultinject.BoundaryFunction}, func(context.Context) (int, error) {
		called = true
		return 0, nil
	})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("Run() error = %v, operation called = %t", err, called)
	}
}

func TestRunWithDisabledInjectorDelegatesDirectly(t *testing.T) {
	t.Parallel()

	value, err := faultinject.Run(context.Background(), nil, faultinject.Metadata{}, func(context.Context) (int, error) {
		return 42, errInjected
	})
	if value != 42 || !errors.Is(err, errInjected) {
		t.Fatalf("Run() = %d, %v", value, err)
	}
}

func injectorWithFault(t *testing.T, fault faultinject.Fault) *faultinject.Injector {
	t.Helper()
	return injectorWithConfig(t, faultinject.Config{Rules: []faultinject.Rule{ruleWithFault("fault", fault)}})
}

func injectorWithConfig(t *testing.T, configuration faultinject.Config) *faultinject.Injector {
	t.Helper()
	injector, err := faultinject.New(configuration)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return injector
}

func mustInjector(t *testing.T, configuration faultinject.Config) *faultinject.Injector {
	t.Helper()
	return injectorWithConfig(t, configuration)
}

func ruleWithFault(id string, fault faultinject.Fault) faultinject.Rule {
	return faultinject.Rule{
		ID: id, Scope: faultinject.BoundaryFunction, Activation: faultinject.Active,
		Maximum: 1, Terminal: faultinject.Continue, Observation: faultinject.Suppress,
		Schedule: faultinject.Every(1), Faults: []faultinject.Fault{fault},
	}
}

type recordingSleeper struct{ delays []time.Duration }

func (sleeper *recordingSleeper) Sleep(_ context.Context, delay time.Duration) error {
	sleeper.delays = append(sleeper.delays, delay)
	return nil
}

type blockingSleeper struct{}

func (blockingSleeper) Sleep(ctx context.Context, _ time.Duration) error {
	return ctx.Err()
}
