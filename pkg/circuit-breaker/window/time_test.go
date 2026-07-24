package window_test

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/circuit-breaker/window"
)

func TestTimeMatchesReferenceAcrossLongRandomizedSequences(t *testing.T) {
	t.Parallel()

	for seed := uint64(1); seed <= 32; seed++ {
		random := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
		duration := time.Duration(random.Uint64()%uint64(time.Hour)) + 1
		bucketCount := int(random.Uint64()%32) + 1
		actual, err := window.NewTime(duration, bucketCount)
		if err != nil {
			t.Fatalf("seed %d: NewTime() error = %v", seed, err)
		}
		reference := newTimeReference(duration, bucketCount)
		for operation := range 512 {
			at := time.Unix(int64(random.Uint64()), int64(random.Uint64()%uint64(time.Second)))
			record := window.Record{
				Class: window.Class(random.Uint64() % 3),
				Slow:  random.Uint64()&1 != 0,
			}
			if random.Uint64()&3 != 0 {
				if err := actual.Add(at, record); err != nil {
					t.Fatalf("seed %d operation %d: Add() error = %v", seed, operation, err)
				}
				reference.add(at, record)
			}
			if got, want := actual.Snapshot(at), reference.snapshot(at); got != want {
				t.Fatalf(
					"seed %d operation %d at %v: Snapshot() = %+v, want %+v",
					seed,
					operation,
					at,
					got,
					want,
				)
			}
		}
	}
}

func TestTimeExpiresBucketsAtExactBoundary(t *testing.T) {
	t.Parallel()

	w, err := window.NewTime(time.Second, 3)
	if err != nil {
		t.Fatalf("NewTime() error = %v", err)
	}
	start := time.Unix(100, 0)

	if err := w.Add(start, window.Record{Class: window.Failure}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := w.Add(start.Add(2*time.Second), window.Record{Class: window.Success}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	before := w.Snapshot(start.Add(2999 * time.Millisecond))
	if before.Classified != 2 || before.Failures != 1 {
		t.Fatalf("Snapshot(before expiry) = %+v", before)
	}

	atBoundary := w.Snapshot(start.Add(3 * time.Second))
	if atBoundary.Classified != 1 || atBoundary.Successes != 1 {
		t.Fatalf("Snapshot(at expiry) = %+v", atBoundary)
	}
}

func TestTimeClearsAllBucketsAfterIdleGap(t *testing.T) {
	t.Parallel()

	w, err := window.NewTime(time.Second, 2)
	if err != nil {
		t.Fatalf("NewTime() error = %v", err)
	}
	start := time.Unix(100, 0)

	if err := w.Add(start, window.Record{Class: window.Failure, Slow: true}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got := w.Snapshot(start.Add(time.Hour)); got != (window.Snapshot{}) {
		t.Fatalf("Snapshot() after idle gap = %+v, want empty", got)
	}
}

func TestTimeDoesNotResurrectExpiredDataAfterClockMovesBackward(t *testing.T) {
	t.Parallel()

	w, err := window.NewTime(time.Second, 2)
	if err != nil {
		t.Fatalf("NewTime() error = %v", err)
	}
	start := time.Unix(100, 0)

	if err := w.Add(start, window.Record{Class: window.Failure}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got := w.Snapshot(start.Add(3 * time.Second)); got != (window.Snapshot{}) {
		t.Fatalf("Snapshot() after expiry = %+v, want empty", got)
	}
	if got := w.Snapshot(start); got != (window.Snapshot{}) {
		t.Fatalf("Snapshot() after backward jump = %+v, want empty", got)
	}
}

func TestTimeClampsBackwardCompletionToLatestObservedBucket(t *testing.T) {
	t.Parallel()

	w, err := window.NewTime(time.Second, 2)
	if err != nil {
		t.Fatalf("NewTime() error = %v", err)
	}
	start := time.Unix(100, 0)

	_ = w.Snapshot(start.Add(3 * time.Second))
	if err := w.Add(start, window.Record{Class: window.Success}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	got := w.Snapshot(start.Add(3 * time.Second))
	if got.Classified != 1 || got.Successes != 1 {
		t.Fatalf("Snapshot() = %+v, want clamped success", got)
	}
}

func TestNewTimeValidatesBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		duration time.Duration
		buckets  int
	}{
		{duration: 0, buckets: 2},
		{duration: -time.Second, buckets: 2},
		{duration: time.Second, buckets: 0},
		{duration: time.Second, buckets: -1},
	} {
		if _, err := window.NewTime(test.duration, test.buckets); err == nil {
			t.Fatalf("NewTime(%v, %d) error = nil", test.duration, test.buckets)
		}
	}
}

func TestTimeRejectsUnknownClassWithoutMutation(t *testing.T) {
	t.Parallel()

	w, err := window.NewTime(time.Second, 2)
	if err != nil {
		t.Fatalf("NewTime() error = %v", err)
	}
	if err := w.Add(time.Unix(100, 0), window.Record{Class: window.Class(99)}); err == nil {
		t.Fatal("Add() error = nil")
	}
	if got := w.Snapshot(time.Unix(100, 0)); got != (window.Snapshot{}) {
		t.Fatalf("Snapshot() = %+v", got)
	}
}

func TestTimeSupportsPreEpochBuckets(t *testing.T) {
	t.Parallel()

	w, err := window.NewTime(time.Second, 2)
	if err != nil {
		t.Fatalf("NewTime() error = %v", err)
	}
	at := time.Unix(-1, 0)
	if err := w.Add(at, window.Record{Class: window.Success}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got := w.Snapshot(at); got.Successes != 1 {
		t.Fatalf("Snapshot() = %+v", got)
	}
}

func TestTimeUsesFloorDivisionAcrossEpoch(t *testing.T) {
	t.Parallel()

	w, err := window.NewTime(time.Second, 1)
	if err != nil {
		t.Fatalf("NewTime() error = %v", err)
	}
	if err := w.Add(time.Unix(-1, 500*time.Millisecond.Nanoseconds()), window.Record{Class: window.Failure}); err != nil {
		t.Fatalf("Add(pre-epoch) error = %v", err)
	}
	if err := w.Add(time.Unix(0, 500*time.Millisecond.Nanoseconds()), window.Record{Class: window.Success}); err != nil {
		t.Fatalf("Add(post-epoch) error = %v", err)
	}
	if got := w.Snapshot(time.Unix(0, 500*time.Millisecond.Nanoseconds())); got.Classified != 1 || got.Successes != 1 {
		t.Fatalf("Snapshot() = %+v, want only post-epoch success", got)
	}
}

func TestTimeRetainsBucketAtMinimumRepresentableNanosecond(t *testing.T) {
	t.Parallel()

	w, err := window.NewTime(time.Nanosecond, 2)
	if err != nil {
		t.Fatalf("NewTime() error = %v", err)
	}
	at := time.Unix(0, math.MinInt64)
	if err := w.Add(at, window.Record{Class: window.Success}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got := w.Snapshot(at); got.Classified != 1 || got.Successes != 1 {
		t.Fatalf("Snapshot() = %+v, want retained minimum bucket", got)
	}
}

func TestTimeExpiresDataAfterTimestampBeyondUnixNanoRange(t *testing.T) {
	t.Parallel()

	w, err := window.NewTime(time.Second, 2)
	if err != nil {
		t.Fatalf("NewTime() error = %v", err)
	}
	if err := w.Add(time.Unix(100, 0), window.Record{Class: window.Failure}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	farFuture := time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)
	if got := w.Snapshot(farFuture); got != (window.Snapshot{}) {
		t.Fatalf("Snapshot(far future) = %+v, want empty", got)
	}
}

func TestTimeDoesNotAliasDistinctBucketsBeyondUnixNanoRange(t *testing.T) {
	t.Parallel()

	w, err := window.NewTime(time.Nanosecond, 2)
	if err != nil {
		t.Fatalf("NewTime() error = %v", err)
	}
	old := time.Date(9998, time.January, 1, 0, 0, 0, 0, time.UTC)
	current := time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC)
	if err := w.Add(old, window.Record{Class: window.Failure}); err != nil {
		t.Fatalf("Add(old) error = %v", err)
	}
	if got := w.Snapshot(current); got != (window.Snapshot{}) {
		t.Fatalf("Snapshot(current) = %+v, want expired old bucket", got)
	}
}
