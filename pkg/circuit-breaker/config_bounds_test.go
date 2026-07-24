package breaker_test

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/circuit-breaker/window"
)

func TestNewRejectsNonFinitePolicyValues(t *testing.T) {
	t.Parallel()

	tests := []breaker.Config{
		{Name: "inventory", Opening: &breaker.OpeningRules{FailureRatio: math.NaN()}},
		{Name: "inventory", Opening: &breaker.OpeningRules{SlowRatio: math.Inf(1)}},
		{
			Name: "inventory",
			OpenDuration: breaker.ExponentialOpenDuration{
				Initial:    time.Second,
				Multiplier: math.NaN(),
				Maximum:    time.Minute,
			},
		},
		{
			Name: "inventory",
			HalfOpen: &breaker.HalfOpenPolicy{
				MaxProbes:    2,
				SuccessRatio: math.Inf(1),
			},
		},
	}

	for _, config := range tests {
		if _, err := breaker.New(config); !errors.Is(err, breaker.ErrInvalidConfig) {
			t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
		}
	}
}

func TestNewRejectsAllocationSizedConfiguration(t *testing.T) {
	t.Parallel()

	tests := []breaker.Config{
		{Name: "inventory", Window: breaker.CountWindow{Size: window.MaxCountSize + 1}},
		{
			Name: "inventory",
			Window: breaker.TimeWindow{
				BucketDuration: time.Second,
				BucketCount:    window.MaxBucketCount + 1,
			},
		},
		{
			Name: "inventory",
			HalfOpen: &breaker.HalfOpenPolicy{
				MaxProbes:         breaker.MaxHalfOpenProbes + 1,
				RequiredSuccesses: 1,
			},
		},
		{
			Name:     "inventory",
			Observer: func(breaker.TransitionEvent) error { return nil },
			EventDelivery: breaker.AsynchronousEvents{
				Buffer: breaker.MaxEventBuffer + 1,
			},
		},
	}

	for _, config := range tests {
		if _, err := breaker.New(config); !errors.Is(err, breaker.ErrInvalidConfig) {
			t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
		}
	}
}

func TestNewRejectsUnboundedNameAndImpossibleCountThresholds(t *testing.T) {
	t.Parallel()

	tests := []breaker.Config{
		{Name: strings.Repeat("n", 257)},
		{
			Name:              "inventory",
			Window:            breaker.CountWindow{Size: 2},
			MinimumThroughput: 1,
			Opening:           &breaker.OpeningRules{FailureCount: 3},
		},
		{
			Name:              "inventory",
			Window:            breaker.CountWindow{Size: 2},
			MinimumThroughput: 1,
			Opening:           &breaker.OpeningRules{SlowCount: 3},
		},
	}
	for _, config := range tests {
		if _, err := breaker.New(config); !errors.Is(err, breaker.ErrInvalidConfig) {
			t.Fatalf("New(%+v) error = %v, want ErrInvalidConfig", config, err)
		}
	}
}

func TestNewRejectsOverflowingTimeWindowInterval(t *testing.T) {
	t.Parallel()

	_, err := breaker.New(breaker.Config{
		Name: "inventory",
		Window: breaker.TimeWindow{
			BucketDuration: time.Duration(math.MaxInt64),
			BucketCount:    2,
		},
	})
	if !errors.Is(err, breaker.ErrInvalidConfig) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
	}
}

func TestNewRejectsTypedNilClock(t *testing.T) {
	t.Parallel()

	var clock *fakeClock
	_, err := breaker.New(breaker.Config{Name: "inventory", Clock: clock})
	if !errors.Is(err, breaker.ErrInvalidConfig) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
	}
}

func TestNewRejectsTypedNilRandom(t *testing.T) {
	t.Parallel()

	var random *panicRandom
	_, err := breaker.New(breaker.Config{Name: "inventory", Random: random})
	if !errors.Is(err, breaker.ErrInvalidConfig) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
	}
}

func TestNewRejectsUnsupportedPointerPolicies(t *testing.T) {
	t.Parallel()

	duration := breaker.FixedOpenDuration(time.Second)
	tests := []breaker.Config{
		{Name: "inventory", Window: &breaker.CountWindow{Size: 10}},
		{Name: "inventory", OpenDuration: &duration},
		{Name: "inventory", HalfOpenAdmission: &breaker.RejectExcessProbes{}},
		{
			Name:          "inventory",
			Observer:      func(breaker.TransitionEvent) error { return nil },
			EventDelivery: &breaker.SynchronousEvents{},
		},
		{Name: "inventory", EventDelivery: breaker.SynchronousEvents{}},
	}
	for _, config := range tests {
		if _, err := breaker.New(config); !errors.Is(err, breaker.ErrInvalidConfig) {
			t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
		}
	}
}
