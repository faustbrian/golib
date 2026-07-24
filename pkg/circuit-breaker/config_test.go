package breaker_test

import (
	"errors"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

func TestNewAppliesFiniteDefaults(t *testing.T) {
	t.Parallel()

	b, err := breaker.New(breaker.Config{Name: "inventory"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	snapshot := b.Snapshot()
	if snapshot.Name != "inventory" {
		t.Fatalf("Snapshot().Name = %q, want inventory", snapshot.Name)
	}
	if snapshot.State != breaker.StateClosed {
		t.Fatalf("Snapshot().State = %v, want closed", snapshot.State)
	}
	if snapshot.Mode != breaker.ModeNormal {
		t.Fatalf("Snapshot().Mode = %v, want normal", snapshot.Mode)
	}
	if snapshot.Generation != 1 {
		t.Fatalf("Snapshot().Generation = %d, want 1", snapshot.Generation)
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := map[string]breaker.Config{
		"missing name":       {},
		"negative threshold": {Name: "inventory", SlowCallDuration: -time.Second},
		"empty count window": {
			Name: "inventory",
			Window: breaker.CountWindow{
				Size: 0,
			},
		},
		"impossible minimum throughput": {
			Name:              "inventory",
			MinimumThroughput: 11,
			Window: breaker.CountWindow{
				Size: 10,
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

			var invalid *breaker.InvalidConfigError
			if !errors.As(err, &invalid) {
				t.Fatalf("New() error type = %T, want *InvalidConfigError", err)
			}
			if invalid.Field == "" {
				t.Fatal("InvalidConfigError.Field is empty")
			}
		})
	}
}

func TestPublicEnumsHaveStableStrings(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		breaker.StateClosed.String():    "closed",
		breaker.StateOpen.String():      "open",
		breaker.StateHalfOpen.String():  "half-open",
		breaker.ModeNormal.String():     "normal",
		breaker.ModeForceOpen.String():  "force-open",
		breaker.ModeDisabled.String():   "disabled",
		breaker.ModeIsolated.String():   "isolated",
		breaker.OutcomeSuccess.String(): "success",
		breaker.OutcomeFailure.String(): "failure",
		breaker.OutcomeIgnored.String(): "ignored",
	}

	for got, want := range tests {
		if got != want {
			t.Fatalf("String() = %q, want %q", got, want)
		}
	}
}
