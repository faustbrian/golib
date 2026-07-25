package breaker_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/circuit-breaker/breakertest"
)

func ExampleExecute() {
	circuit, _ := breaker.New(breaker.Config{Name: "catalog"})
	result, err := breaker.Execute(context.Background(), circuit,
		func(context.Context) (string, error) { return "available", nil })
	fmt.Println(result, err)
	// Output: available <nil>
}

func ExampleBreaker_Acquire() {
	circuit, _ := breaker.New(breaker.Config{Name: "stream"})
	permit, err := circuit.Acquire(context.Background())
	if err != nil {
		return
	}
	defer func() { _ = permit.Cancel() }()

	_ = permit.Complete(breaker.OutcomeSuccess, false)
	fmt.Println(circuit.Snapshot().Successes)
	// Output: 1
}

func ExampleConfig_failureRate() {
	circuit, _ := breaker.New(breaker.Config{
		Name:              "database",
		Window:            breaker.CountWindow{Size: 20},
		MinimumThroughput: 2,
		Opening:           &breaker.OpeningRules{FailureRatio: 0.5},
		OpenDuration:      breaker.FixedOpenDuration(time.Minute),
	})
	for range 2 {
		_, _ = breaker.Execute(context.Background(), circuit,
			func(context.Context) (struct{}, error) {
				return struct{}{}, errors.New("unavailable")
			})
	}
	fmt.Println(circuit.Snapshot().State)
	// Output: open
}

func ExampleConfig_timeWindowAndSlowCalls() {
	circuit, _ := breaker.New(breaker.Config{
		Name:              "search",
		Window:            breaker.TimeWindow{BucketDuration: time.Second, BucketCount: 30},
		MinimumThroughput: 10,
		Opening:           &breaker.OpeningRules{SlowRatio: 0.8},
		SlowCallDuration:  500 * time.Millisecond,
	})
	snapshot := circuit.Snapshot()
	fmt.Println(snapshot.WindowCapacity, snapshot.MinimumThroughput)
	// Output: 30 10
}

func ExampleConfig_observer() {
	circuit, _ := breaker.New(breaker.Config{
		Name:              "payments",
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		Observer: func(event breaker.TransitionEvent) error {
			fmt.Println(event.Before.State, event.After.State, event.Reason)
			return nil
		},
		EventDelivery: breaker.SynchronousEvents{},
	})
	_, _ = breaker.Execute(context.Background(), circuit,
		func(context.Context) (struct{}, error) {
			return struct{}{}, errors.New("declined upstream")
		})
	// Output: closed open policy-opened
}

func ExampleBreaker_ForceOpen() {
	circuit, _ := breaker.New(breaker.Config{Name: "maintenance"})
	_ = circuit.ForceOpen()
	_, err := circuit.Acquire(context.Background())
	fmt.Println(errors.Is(err, breaker.ErrForceOpen))
	_ = circuit.Release()
	// Output: true
}

func ExampleBreaker_SetMode() {
	circuit, _ := breaker.New(breaker.Config{Name: "maintenance"})
	_ = circuit.SetMode(breaker.ModeDisabled)
	permit, _ := circuit.Acquire(context.Background())
	_ = permit.Complete(breaker.OutcomeFailure, false)
	fmt.Println(circuit.Snapshot().Mode, circuit.Snapshot().Failures)

	_ = circuit.SetMode(breaker.ModeIsolated)
	_, err := circuit.Acquire(context.Background())
	fmt.Println(errors.Is(err, breaker.ErrIsolated))

	_ = circuit.Reset()
	fmt.Println(circuit.Snapshot().State, circuit.Snapshot().Mode)
	// Output:
	// disabled 0
	// true
	// closed normal
}

func ExampleBreaker_Shutdown() {
	var observed atomic.Uint64
	circuit, _ := breaker.New(breaker.Config{
		Name: "events",
		Observer: func(breaker.TransitionEvent) error {
			observed.Add(1)
			return nil
		},
	})
	_ = circuit.ForceOpen()
	_ = circuit.Shutdown(context.Background())
	fmt.Println(observed.Load())
	// Output: 1
}

func ExampleConfig_halfOpenRecovery() {
	clock := breakertest.NewClock(time.Unix(100, 0))
	circuit, _ := breaker.New(breaker.Config{
		Name:              "catalog",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Second),
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         1,
			RequiredSuccesses: 1,
		},
	})
	_, _ = breaker.Execute(context.Background(), circuit,
		func(context.Context) (struct{}, error) {
			return struct{}{}, errors.New("unavailable")
		})
	clock.Advance(time.Second)
	_, _ = breaker.Execute(context.Background(), circuit,
		func(context.Context) (struct{}, error) { return struct{}{}, nil })
	fmt.Println(circuit.Snapshot().State)
	// Output: closed
}

func ExampleConfig_exponentialOpenDuration() {
	clock := breakertest.NewClock(time.Unix(100, 0))
	circuit, _ := breaker.New(breaker.Config{
		Name:              "catalog",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration: breaker.ExponentialOpenDuration{
			Initial:    time.Second,
			Multiplier: 2,
			Maximum:    time.Minute,
		},
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         1,
			RequiredSuccesses: 1,
		},
	})
	completeFailure := func() {
		permit, _ := circuit.Acquire(context.Background())
		_ = permit.Complete(breaker.OutcomeFailure, false)
	}
	completeFailure()
	fmt.Println(circuit.Snapshot().CurrentOpenDuration)
	clock.Advance(time.Second)
	completeFailure()
	fmt.Println(circuit.Snapshot().CurrentOpenDuration)
	// Output:
	// 1s
	// 2s
}

func ExampleConfig_customClassifier() {
	errLocal := errors.New("local validation")
	circuit, _ := breaker.New(breaker.Config{
		Name: "catalog",
		Classifier: func(completion breaker.Completion) breaker.Outcome {
			if errors.Is(completion.Err, errLocal) {
				return breaker.OutcomeIgnored
			}
			return breaker.OutcomeFailure
		},
	})
	_, err := breaker.Execute(context.Background(), circuit,
		func(context.Context) (struct{}, error) { return struct{}{}, errLocal })
	fmt.Println(errors.Is(err, errLocal), circuit.Snapshot().Ignored)
	// Output: true 1
}

func ExampleConfig_waitForProbe() {
	config := breaker.Config{
		Name:              "catalog",
		HalfOpenAdmission: breaker.WaitForProbe{MaxWait: 250 * time.Millisecond},
	}
	circuit, _ := breaker.New(config)
	fmt.Printf("%T\n", config.HalfOpenAdmission)
	_ = circuit.Close()
	// Output: breaker.WaitForProbe
}

func ExampleConfig_combinedOpeningRules() {
	circuit, _ := breaker.New(breaker.Config{
		Name:              "search",
		MinimumThroughput: 2,
		Opening: &breaker.OpeningRules{
			FailureCount: 2,
			SlowCount:    2,
			Combination:  breaker.OpenWhenAll,
		},
	})
	for range 2 {
		permit, _ := circuit.Acquire(context.Background())
		_ = permit.Complete(breaker.OutcomeFailure, true)
	}
	fmt.Println(circuit.Snapshot().State)
	// Output: open
}

func ExampleConfig_ignoredOutcomeBehavior() {
	circuit, _ := breaker.New(breaker.Config{
		Name:              "catalog",
		MinimumThroughput: 2,
		Opening: &breaker.OpeningRules{
			ConsecutiveFailures: 2,
			IgnoredBehavior:     breaker.ResetConsecutiveFailures,
		},
	})
	for _, outcome := range []breaker.Outcome{
		breaker.OutcomeFailure,
		breaker.OutcomeIgnored,
		breaker.OutcomeFailure,
	} {
		permit, _ := circuit.Acquire(context.Background())
		_ = permit.Complete(outcome, false)
	}
	fmt.Println(circuit.Snapshot().State)
	// Output: closed
}

func ExampleConfig_successRatioRecovery() {
	clock := breakertest.NewClock(time.Unix(100, 0))
	circuit, _ := breaker.New(breaker.Config{
		Name:              "catalog",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Second),
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:     3,
			SuccessRatio:  2.0 / 3.0,
			FailureAction: breaker.ReopenAfterSample,
		},
	})
	permit, _ := circuit.Acquire(context.Background())
	_ = permit.Complete(breaker.OutcomeFailure, false)
	clock.Advance(time.Second)
	for _, outcome := range []breaker.Outcome{
		breaker.OutcomeSuccess,
		breaker.OutcomeFailure,
		breaker.OutcomeSuccess,
	} {
		permit, _ = circuit.Acquire(context.Background())
		_ = permit.Complete(outcome, false)
	}
	fmt.Println(circuit.Snapshot().State)
	// Output: closed
}

type exampleRandom float64

func (r exampleRandom) Float64() float64 { return float64(r) }

func ExampleConfig_openDurationJitter() {
	circuit, _ := breaker.New(breaker.Config{
		Name:               "catalog",
		MinimumThroughput:  1,
		Opening:            &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:       breaker.FixedOpenDuration(10 * time.Second),
		OpenDurationJitter: 0.5,
		Random:             exampleRandom(0.5),
	})
	permit, _ := circuit.Acquire(context.Background())
	_ = permit.Complete(breaker.OutcomeFailure, false)
	fmt.Println(circuit.Snapshot().CurrentOpenDuration)
	// Output: 7.5s
}

func ExampleRejectExcessProbes() {
	clock := breakertest.NewClock(time.Unix(100, 0))
	circuit, _ := breaker.New(breaker.Config{
		Name:              "catalog",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Second),
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         1,
			RequiredSuccesses: 1,
		},
		HalfOpenAdmission: breaker.RejectExcessProbes{},
	})
	permit, _ := circuit.Acquire(context.Background())
	_ = permit.Complete(breaker.OutcomeFailure, false)
	clock.Advance(time.Second)
	probe, _ := circuit.Acquire(context.Background())
	_, err := circuit.Acquire(context.Background())
	fmt.Println(errors.Is(err, breaker.ErrHalfOpenExhausted))
	_ = probe.Cancel()
	// Output: true
}

func ExamplePermit_Cancel() {
	circuit, _ := breaker.New(breaker.Config{Name: "stream"})
	permit, _ := circuit.Acquire(context.Background())
	fmt.Println(permit.Cancel())
	fmt.Println(errors.Is(permit.Cancel(), breaker.ErrPermitCanceled))
	// Output:
	// <nil>
	// true
}

func ExampleRejectionError() {
	circuit, _ := breaker.New(breaker.Config{Name: "catalog"})
	_ = circuit.ForceOpen()
	_, err := circuit.Acquire(context.Background())
	var rejection *breaker.RejectionError
	fmt.Println(errors.As(err, &rejection), rejection.Name, rejection.Mode)
	// Output: true catalog force-open
}

func ExampleConfig_permitTTL() {
	clock := breakertest.NewClock(time.Unix(100, 0))
	circuit, _ := breaker.New(breaker.Config{
		Name:      "stream",
		Clock:     clock,
		PermitTTL: time.Second,
	})
	permit, _ := circuit.Acquire(context.Background())
	clock.Advance(time.Second)
	fmt.Println(errors.Is(permit.Complete(breaker.OutcomeSuccess, false), breaker.ErrPermitExpired))
	// Output: true
}
