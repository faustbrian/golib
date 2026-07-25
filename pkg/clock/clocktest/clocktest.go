// Package clocktest contains deterministic testing helpers for clock.
package clocktest

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	clock "github.com/faustbrian/golib/pkg/clock"
	"github.com/faustbrian/golib/pkg/clock/manual"
)

// SystemBubble runs test in a testing/synctest bubble with a System clock.
// Prefer this helper when the complete unit under test can live inside one
// bubble. Use dependency-injected manual clocks for explicit business
// timestamps, wall jumps, and cross-package clock contracts.
func SystemBubble(t *testing.T, test func(*testing.T, clock.System)) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		t.Helper()
		test(t, clock.System{})
	})
}

// Wait checks ctx and then waits for every other goroutine in the current
// synctest bubble to become durably blocked. It must be called from a bubble.
func Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	synctest.Wait()
	return ctx.Err()
}

// Advance advances a manual clock, waits for all synchronously triggered work,
// and fails the current test if either phase reports an error.
func Advance(t testing.TB, clock *manual.Clock, duration time.Duration) manual.Result {
	t.Helper()
	waiter, err := clock.Advance(duration)
	if err != nil {
		t.Fatalf("clocktest.Advance(%v): %v", duration, err)
	}
	result, err := waiter.Wait(t.Context())
	if err != nil {
		t.Fatalf("clocktest.Advance(%v) wait: %v", duration, err)
	}
	return result
}
