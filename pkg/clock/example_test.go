package clock_test

import (
	"context"
	"fmt"
	"time"

	clock "github.com/faustbrian/golib/pkg/clock"
	"github.com/faustbrian/golib/pkg/clock/manual"
)

func ExampleSystem() {
	var timestamps clock.Clock = clock.System{}
	_ = timestamps.Now()
	fmt.Println("system clock ready")
	// Output: system clock ready
}

func ExampleFixed() {
	fixed := manual.NewFixed(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	fmt.Println(fixed.Now().Format(time.RFC3339))
	// Output: 2026-01-02T03:04:05Z
}

func ExampleClock() {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	manualClock, err := manual.New(start)
	if err != nil {
		panic(err)
	}
	timer, err := manualClock.NewTimer(time.Minute)
	if err != nil {
		panic(err)
	}
	waiter, err := manualClock.Advance(time.Minute)
	if err != nil {
		panic(err)
	}
	if _, err := waiter.Wait(context.Background()); err != nil {
		panic(err)
	}
	fmt.Println((<-timer.C()).Format(time.RFC3339))
	// Output: 2026-01-02T03:05:05Z
}

func ExampleObserve() {
	observed, err := clock.Observe(clock.System{}, clock.ObserverFunc(func(observation clock.Observation) {
		fmt.Println(observation.Kind, observation.Outcome)
	}))
	if err != nil {
		panic(err)
	}
	timer, err := observed.NewTimer(time.Hour)
	if err != nil {
		panic(err)
	}
	timer.Stop()
	// Output:
	// timer created
	// timer stopped
}
