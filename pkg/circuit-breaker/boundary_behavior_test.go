package breaker_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/circuit-breaker/breakertest"
)

type panicRandom struct{}

func (panicRandom) Float64() float64 { panic("random source") }

func TestPermitTerminalStateErrors(t *testing.T) {
	t.Parallel()

	t.Run("completed cancel", func(t *testing.T) {
		b := mustBreaker(t, breaker.Config{Name: "inventory"})
		permit, _ := b.Acquire(context.Background())
		_ = permit.Complete(breaker.OutcomeSuccess, false)
		if err := permit.Cancel(); !errors.Is(err, breaker.ErrPermitCompleted) {
			t.Fatalf("Cancel() error = %v", err)
		}
	})
	t.Run("duplicate cancel", func(t *testing.T) {
		b := mustBreaker(t, breaker.Config{Name: "inventory"})
		permit, _ := b.Acquire(context.Background())
		_ = permit.Cancel()
		if err := permit.Cancel(); !errors.Is(err, breaker.ErrPermitCanceled) {
			t.Fatalf("Cancel() error = %v", err)
		}
	})
	t.Run("completion expires at deadline", func(t *testing.T) {
		clock := breakertest.NewClock(time.Unix(100, 0))
		b := mustBreaker(t, breaker.Config{Name: "inventory", Clock: clock, PermitTTL: time.Second})
		permit, _ := b.Acquire(context.Background())
		clock.Advance(time.Second)
		if err := permit.Complete(breaker.OutcomeSuccess, false); !errors.Is(err, breaker.ErrPermitExpired) {
			t.Fatalf("Complete() error = %v", err)
		}
		if err := permit.Cancel(); !errors.Is(err, breaker.ErrPermitExpired) {
			t.Fatalf("Cancel() error = %v", err)
		}
	})
	t.Run("cancel expires at deadline", func(t *testing.T) {
		clock := breakertest.NewClock(time.Unix(100, 0))
		b := mustBreaker(t, breaker.Config{Name: "inventory", Clock: clock, PermitTTL: time.Second})
		permit, _ := b.Acquire(context.Background())
		clock.Advance(time.Second)
		if err := permit.Cancel(); !errors.Is(err, breaker.ErrPermitExpired) {
			t.Fatalf("Cancel() error = %v", err)
		}
	})
}

func TestDefaultClassifierAndBackwardElapsedTime(t *testing.T) {
	t.Parallel()

	clock := breakertest.NewClock(time.Unix(100, 0))
	b := mustBreaker(t, breaker.Config{Name: "inventory", Clock: clock})
	result, err := breaker.Execute(context.Background(), b, func(context.Context) (string, error) {
		clock.Set(time.Unix(99, 0))
		return "ok", nil
	})
	if err != nil || result != "ok" {
		t.Fatalf("Execute() = %q, %v", result, err)
	}
	if got := b.Snapshot(); got.Successes != 1 || got.SlowSuccesses != 0 {
		t.Fatalf("Snapshot() = %+v", got)
	}
}

func TestRandomSourceFailureFallsBackToUnjitteredDuration(t *testing.T) {
	t.Parallel()

	for name, random := range map[string]breaker.Random{
		"panic": panicRandom{},
		"nan":   fixedRandom(math.NaN()),
		"low":   fixedRandom(-1),
		"high":  fixedRandom(1),
	} {
		random := random
		t.Run(name, func(t *testing.T) {
			b := mustBreaker(t, breaker.Config{
				Name:               "inventory",
				MinimumThroughput:  1,
				Opening:            &breaker.OpeningRules{FailureCount: 1},
				OpenDuration:       breaker.FixedOpenDuration(time.Second),
				OpenDurationJitter: 0.5,
				Random:             random,
			})
			completeOutcome(t, b, breaker.OutcomeFailure)
			if got := b.Snapshot().CurrentOpenDuration; got != time.Second {
				t.Fatalf("CurrentOpenDuration = %v, want unjittered", got)
			}
		})
	}
}

func TestAdministrativeNoOpAndInvalidMode(t *testing.T) {
	t.Parallel()

	b := mustBreaker(t, breaker.Config{Name: "inventory"})
	if err := b.SetMode(breaker.ModeNormal); err != nil {
		t.Fatalf("SetMode(normal) error = %v", err)
	}
	if got := b.Snapshot().Generation; got != 1 {
		t.Fatalf("generation after no-op mode = %d", got)
	}
	if err := b.SetMode(breaker.Mode(99)); err == nil {
		t.Fatal("SetMode(unknown) error = nil")
	}
}

func TestPublicStringAndErrorContracts(t *testing.T) {
	t.Parallel()

	for got, want := range map[string]string{
		breaker.State(99).String():                 "unknown",
		breaker.Mode(99).String():                  "unknown",
		breaker.Outcome(99).String():               "unknown",
		breaker.ReasonPolicyOpened.String():        "policy-opened",
		breaker.ReasonOpenIntervalElapsed.String(): "open-interval-elapsed",
		breaker.ReasonHalfOpenRecovered.String():   "half-open-recovered",
		breaker.ReasonHalfOpenFailed.String():      "half-open-failed",
		breaker.ReasonForceOpen.String():           "force-open",
		breaker.ReasonDisabled.String():            "disabled",
		breaker.ReasonIsolated.String():            "isolated",
		breaker.ReasonReleased.String():            "released",
		breaker.ReasonReset.String():               "reset",
		breaker.TransitionReason(99).String():      "unknown",
	} {
		if got != want {
			t.Fatalf("String() = %q, want %q", got, want)
		}
	}

	invalid := &breaker.InvalidConfigError{Field: "Name", Message: "required"}
	if !strings.Contains(invalid.Error(), "Name") || !errors.Is(invalid, breaker.ErrInvalidConfig) {
		t.Fatalf("InvalidConfigError = %q", invalid)
	}
	rejection := &breaker.RejectionError{Name: "inventory", State: breaker.StateOpen, Cause: breaker.ErrOpen}
	if !strings.Contains(rejection.Error(), "inventory") || !errors.Is(rejection, breaker.ErrOpen) {
		t.Fatalf("RejectionError = %q", rejection)
	}
}

func TestDefaultAsynchronousObserverAndDropOldest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	b := mustBreaker(t, breaker.Config{
		Name: "inventory",
		Observer: func(breaker.TransitionEvent) error {
			select {
			case <-started:
			default:
				close(started)
			}
			<-release
			return nil
		},
		EventDelivery: breaker.AsynchronousEvents{Buffer: 1, Overflow: breaker.DropOldestEvent},
	})
	_ = b.ForceOpen()
	<-started
	_ = b.Release()
	_ = b.ForceOpen()
	if got := b.Snapshot().DroppedEvents; got != 1 {
		t.Fatalf("DroppedEvents = %d, want oldest eviction", got)
	}
	close(release)
	_ = b.Shutdown(context.Background())
}

func TestDefaultAsynchronousDeliveryAndExplicitImmediateRejection(t *testing.T) {
	t.Parallel()

	observed := make(chan struct{}, 1)
	b := mustBreaker(t, breaker.Config{
		Name: "inventory",
		Observer: func(breaker.TransitionEvent) error {
			observed <- struct{}{}
			return nil
		},
		HalfOpenAdmission: breaker.RejectExcessProbes{},
	})
	if err := b.ForceOpen(); err != nil {
		t.Fatalf("ForceOpen() error = %v", err)
	}
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("default asynchronous observer did not receive event")
	}
	_ = b.Shutdown(context.Background())
}

func TestOneNanosecondJitterNeverBecomesNonPositive(t *testing.T) {
	t.Parallel()

	b := mustBreaker(t, breaker.Config{
		Name:               "inventory",
		MinimumThroughput:  1,
		Opening:            &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:       breaker.FixedOpenDuration(time.Nanosecond),
		OpenDurationJitter: math.Nextafter(1, 0),
		Random:             fixedRandom(math.Nextafter(1, 0)),
	})
	completeOutcome(t, b, breaker.OutcomeFailure)
	if got := b.Snapshot().CurrentOpenDuration; got != time.Nanosecond {
		t.Fatalf("CurrentOpenDuration = %v, want 1ns minimum", got)
	}
}
