package window_test

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/circuit-breaker/window"
)

func TestWindowConstructorsRejectOversizedAllocation(t *testing.T) {
	t.Parallel()

	if _, err := window.NewCount(window.MaxCountSize + 1); err == nil {
		t.Fatal("NewCount() oversized error = nil")
	}
	if _, err := window.NewTime(time.Second, window.MaxBucketCount+1); err == nil {
		t.Fatal("NewTime() oversized error = nil")
	}
}

func TestNewTimeRejectsOverflowingInterval(t *testing.T) {
	t.Parallel()

	if _, err := window.NewTime(time.Duration(1<<62), 3); err == nil {
		t.Fatal("NewTime() overflowing interval error = nil")
	}
}
