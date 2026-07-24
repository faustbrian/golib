package breaker_test

import (
	"errors"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

func TestNewAcceptsCompletePolicyConfiguration(t *testing.T) {
	t.Parallel()

	_, err := breaker.New(breaker.Config{
		Name: "inventory",
		Window: breaker.TimeWindow{
			BucketDuration: time.Second,
			BucketCount:    10,
		},
		MinimumThroughput: 5,
		Opening: &breaker.OpeningRules{
			ConsecutiveFailures: 3,
			FailureRatio:        0.5,
			SlowRatio:           0.8,
			Combination:         breaker.OpenWhenAny,
		},
		OpenDuration: breaker.ExponentialOpenDuration{
			Initial:    time.Second,
			Multiplier: 2,
			Maximum:    time.Minute,
		},
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         3,
			RequiredSuccesses: 2,
			FailureAction:     breaker.ReopenImmediately,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
}

func TestNewRejectsInvalidPolicyCombinations(t *testing.T) {
	t.Parallel()

	tests := map[string]breaker.Config{
		"time bucket duration": {
			Name:   "inventory",
			Window: breaker.TimeWindow{BucketCount: 10},
		},
		"time bucket count": {
			Name:   "inventory",
			Window: breaker.TimeWindow{BucketDuration: time.Second},
		},
		"negative minimum throughput": {
			Name:              "inventory",
			MinimumThroughput: -1,
		},
		"empty opening rules": {
			Name:    "inventory",
			Opening: &breaker.OpeningRules{},
		},
		"failure ratio over one": {
			Name:    "inventory",
			Opening: &breaker.OpeningRules{FailureRatio: 1.1},
		},
		"slow ratio below zero": {
			Name:    "inventory",
			Opening: &breaker.OpeningRules{SlowRatio: -0.1},
		},
		"unknown combination": {
			Name: "inventory",
			Opening: &breaker.OpeningRules{
				FailureCount: 1,
				Combination:  breaker.RuleCombination(99),
			},
		},
		"unknown ignored behavior": {
			Name: "inventory",
			Opening: &breaker.OpeningRules{
				FailureCount:    1,
				IgnoredBehavior: breaker.IgnoredConsecutiveBehavior(99),
			},
		},
		"non-positive fixed duration": {
			Name:         "inventory",
			OpenDuration: breaker.FixedOpenDuration(0),
		},
		"exponential maximum below initial": {
			Name: "inventory",
			OpenDuration: breaker.ExponentialOpenDuration{
				Initial:    time.Minute,
				Multiplier: 2,
				Maximum:    time.Second,
			},
		},
		"exponential initial is not positive": {
			Name: "inventory",
			OpenDuration: breaker.ExponentialOpenDuration{
				Initial:    0,
				Multiplier: 2,
				Maximum:    time.Minute,
			},
		},
		"exponential multiplier below one": {
			Name: "inventory",
			OpenDuration: breaker.ExponentialOpenDuration{
				Initial:    time.Second,
				Multiplier: 0.5,
				Maximum:    time.Minute,
			},
		},
		"half-open without probes": {
			Name:     "inventory",
			HalfOpen: &breaker.HalfOpenPolicy{},
		},
		"half-open without recovery threshold": {
			Name:     "inventory",
			HalfOpen: &breaker.HalfOpenPolicy{MaxProbes: 1},
		},
		"half-open successes exceed probes": {
			Name: "inventory",
			HalfOpen: &breaker.HalfOpenPolicy{
				MaxProbes:         2,
				RequiredSuccesses: 3,
			},
		},
		"half-open ambiguous recovery": {
			Name: "inventory",
			HalfOpen: &breaker.HalfOpenPolicy{
				MaxProbes:         2,
				RequiredSuccesses: 1,
				SuccessRatio:      0.5,
			},
		},
		"half-open unknown failure action": {
			Name: "inventory",
			HalfOpen: &breaker.HalfOpenPolicy{
				MaxProbes:         1,
				RequiredSuccesses: 1,
				FailureAction:     breaker.HalfOpenFailureAction(99),
			},
		},
	}

	for name, config := range tests {
		config := config
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := breaker.New(config)
			if !errors.Is(err, breaker.ErrInvalidConfig) {
				t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}
