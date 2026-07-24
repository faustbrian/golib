package prompts_test

import (
	"errors"
	"testing"
	"time"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

func TestVirtualClockAdvancesTimersAndCoalescesTickers(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	clock := prompts.NewVirtualClock(start)
	timer := clock.NewTimer(2 * time.Second)
	ticker := clock.NewTicker(time.Second)
	if got := clock.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v", got)
	}
	if err := clock.Advance(time.Second); err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	select {
	case tick := <-ticker.C():
		if !tick.Equal(start.Add(time.Second)) {
			t.Fatalf("ticker tick = %v", tick)
		}
	default:
		t.Fatal("ticker did not fire")
	}
	select {
	case <-timer.C():
		t.Fatal("timer fired early")
	default:
	}
	if err := clock.Advance(3 * time.Second); err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	select {
	case tick := <-timer.C():
		if !tick.Equal(start.Add(2 * time.Second)) {
			t.Fatalf("timer tick = %v", tick)
		}
	default:
		t.Fatal("timer did not fire")
	}
	select {
	case tick := <-ticker.C():
		if !tick.Equal(start.Add(2 * time.Second)) {
			t.Fatalf("coalesced ticker tick = %v", tick)
		}
	default:
		t.Fatal("ticker did not coalesce a tick")
	}
	if timer.Stop() {
		t.Fatal("expired timer reported active")
	}
	ticker.Stop()
	if err := clock.Advance(time.Second); err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	select {
	case <-ticker.C():
		t.Fatal("stopped ticker fired")
	default:
	}
}

func TestVirtualClockStopAndImmediateEvents(t *testing.T) {
	t.Parallel()

	clock := prompts.NewVirtualClock(time.Time{})
	timer := clock.NewTimer(time.Hour)
	if !timer.Stop() || timer.Stop() {
		t.Fatal("Timer.Stop() active state was not idempotent")
	}
	immediate := clock.NewTimer(0)
	select {
	case tick := <-immediate.C():
		if !tick.IsZero() {
			t.Fatalf("immediate tick = %v", tick)
		}
	default:
		t.Fatal("immediate timer did not fire")
	}
	disabled := clock.NewTicker(0)
	if err := clock.Advance(time.Second); err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	select {
	case <-disabled.C():
		t.Fatal("non-positive ticker fired")
	default:
	}
	disabled.Stop()
	if err := clock.Advance(-time.Second); !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("negative Advance() error = %v", err)
	}
}
