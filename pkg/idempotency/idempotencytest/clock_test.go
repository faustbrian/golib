package idempotencytest_test

import (
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
)

func TestClockAdvancesDeterministically(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	clock := idempotencytest.NewClock(start)
	if got := clock.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}

	clock.Advance(90 * time.Second)
	if got := clock.Now(); !got.Equal(start.Add(90 * time.Second)) {
		t.Fatalf("Now() after Advance() = %v", got)
	}

	replacement := start.Add(24 * time.Hour)
	clock.Set(replacement)
	if got := clock.Now(); !got.Equal(replacement) {
		t.Fatalf("Now() after Set() = %v", got)
	}
}

func TestClockIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	clock := idempotencytest.NewClock(time.Time{})
	const workers = 32
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			clock.Advance(time.Second)
			_ = clock.Now()
		}()
	}
	wait.Wait()

	if got := clock.Now(); !got.Equal(time.Time{}.Add(workers * time.Second)) {
		t.Fatalf("Now() = %v", got)
	}
}

func TestTokenSourceReturnsUniqueDeterministicTokens(t *testing.T) {
	t.Parallel()

	source := idempotencytest.NewTokenSource("test-owner")
	first, err := source.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	second, err := source.Next()
	if err != nil {
		t.Fatalf("Next() second error = %v", err)
	}
	if first != "test-owner-1" || second != "test-owner-2" {
		t.Fatalf("Next() tokens = %q, %q", first, second)
	}
}
