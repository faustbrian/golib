package window_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/circuit-breaker/window"
)

func TestCountRetainsOnlyMostRecentClassifiedOutcomes(t *testing.T) {
	t.Parallel()

	w, err := window.NewCount(3)
	if err != nil {
		t.Fatalf("NewCount() error = %v", err)
	}

	_ = w.Add(window.Record{Class: window.Success})
	_ = w.Add(window.Record{Class: window.Failure, Slow: true})
	_ = w.Add(window.Record{Class: window.Ignored, Slow: true})
	_ = w.Add(window.Record{Class: window.Failure})

	got := w.Snapshot()
	want := window.Snapshot{
		Classified:  3,
		Successes:   1,
		Failures:    2,
		Ignored:     1,
		SlowFailure: 1,
	}
	if got != want {
		t.Fatalf("Snapshot() = %+v, want %+v", got, want)
	}
}

func TestCountIgnoredOutcomesDoNotEvictClassifiedOutcomes(t *testing.T) {
	t.Parallel()

	w, err := window.NewCount(2)
	if err != nil {
		t.Fatalf("NewCount() error = %v", err)
	}
	_ = w.Add(window.Record{Class: window.Failure})
	_ = w.Add(window.Record{Class: window.Success})
	for range 100 {
		_ = w.Add(window.Record{Class: window.Ignored})
	}

	got := w.Snapshot()
	if got.Classified != 2 || got.Failures != 1 || got.Successes != 1 {
		t.Fatalf("Snapshot() classified data = %+v", got)
	}
	if got.Ignored != 100 {
		t.Fatalf("Snapshot().Ignored = %d, want 100", got.Ignored)
	}
}

func TestCountTracksSlowSuccessAndFailureSeparately(t *testing.T) {
	t.Parallel()

	w, err := window.NewCount(4)
	if err != nil {
		t.Fatalf("NewCount() error = %v", err)
	}

	_ = w.Add(window.Record{Class: window.Success, Slow: true})
	_ = w.Add(window.Record{Class: window.Failure, Slow: true})
	_ = w.Add(window.Record{Class: window.Success})
	_ = w.Add(window.Record{Class: window.Ignored})

	got := w.Snapshot()
	if got.Successes != 2 || got.Failures != 1 || got.Ignored != 1 {
		t.Fatalf("Snapshot() counts = %+v", got)
	}
	if got.SlowSuccess != 1 || got.SlowFailure != 1 {
		t.Fatalf("Snapshot() slow counts = %+v", got)
	}
	if got.Classified != 3 {
		t.Fatalf("Snapshot().Classified = %d, want 3", got.Classified)
	}
}

func TestCountEvictionRemovesEveryClassifiedDimension(t *testing.T) {
	t.Parallel()

	w, err := window.NewCount(2)
	if err != nil {
		t.Fatalf("NewCount() error = %v", err)
	}
	_ = w.Add(window.Record{Class: window.Success, Slow: true})
	_ = w.Add(window.Record{Class: window.Failure, Slow: true})
	_ = w.Add(window.Record{Class: window.Success})
	_ = w.Add(window.Record{Class: window.Failure})

	want := window.Snapshot{Classified: 2, Successes: 1, Failures: 1}
	if got := w.Snapshot(); got != want {
		t.Fatalf("Snapshot() = %+v, want %+v", got, want)
	}
}

func TestNewCountRejectsNonPositiveSize(t *testing.T) {
	t.Parallel()

	for _, size := range []int{-1, 0} {
		if _, err := window.NewCount(size); err == nil {
			t.Fatalf("NewCount(%d) error = nil", size)
		}
	}
}

func TestCountRejectsUnknownClassWithoutMutation(t *testing.T) {
	t.Parallel()

	w, err := window.NewCount(2)
	if err != nil {
		t.Fatalf("NewCount() error = %v", err)
	}

	if err := w.Add(window.Record{Class: window.Class(99)}); err == nil {
		t.Fatal("Add() error = nil")
	}
	if got := w.Snapshot(); got != (window.Snapshot{}) {
		t.Fatalf("Snapshot() = %+v, want empty", got)
	}
}
