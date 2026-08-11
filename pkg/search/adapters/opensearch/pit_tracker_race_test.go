package opensearch

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func TestPointInTimeTrackerCloseMakesLaterReleaseIdempotent(t *testing.T) {
	t.Parallel()

	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	tracker := newPointInTimeTracker(codec, 1)
	expiresAt, err := codec.Deadline(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := tracker.reserve(expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	tracker.close()
	tracker.release(lease)
	if snapshot := tracker.snapshot(); snapshot.Open != 0 {
		t.Fatalf("snapshot after close/release = %#v, want zero open PITs", snapshot)
	}
}

func TestPointInTimeTrackerRejectsDuplicateOwnershipAndConcurrentUse(t *testing.T) {
	t.Parallel()

	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	tracker := newPointInTimeTracker(codec, 2)
	expiresAt, err := codec.Deadline(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first, err := tracker.reserve(expiresAt)
	if err != nil || tracker.bind(first, "pit-a", expiresAt) != nil {
		t.Fatalf("first reservation/bind = %#v/%v", first, err)
	}
	tracker.yield(first)
	duplicate, err := tracker.reserve(expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if bindErr := tracker.bind(duplicate, "pit-a", expiresAt); !errors.Is(bindErr, errPointInTimeIDConflict) {
		t.Fatalf("duplicate bind = %v", bindErr)
	}
	tracker.release(duplicate)
	acquired, err := tracker.acquire("pit-a", expiresAt)
	if err != nil || acquired != first {
		t.Fatalf("first acquire = %#v/%v", acquired, err)
	}
	if _, err := tracker.acquire("pit-a", expiresAt); !errors.Is(err, ErrPointInTimeInUse) {
		t.Fatalf("concurrent acquire = %v, want ErrPointInTimeInUse", err)
	}
	tracker.yield(acquired)
	if snapshot := tracker.snapshot(); snapshot.Open != 1 {
		t.Fatalf("snapshot = %#v, want one original lease", snapshot)
	}
}

func TestPointInTimeTrackerNeverCallsCodecClockUnderItsLock(t *testing.T) {
	t.Parallel()

	now := time.Now()
	var tracker *pointInTimeTracker
	var checking atomic.Bool
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), func() time.Time {
		if tracker != nil && checking.CompareAndSwap(false, true) {
			_ = tracker.snapshot()
			checking.Store(false)
		}
		return now
	}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	tracker = newPointInTimeTracker(codec, 2)
	expiresAt := now.Add(time.Minute)
	lease, err := tracker.reserve(expiresAt)
	if err != nil || tracker.bind(lease, "pit-a", expiresAt) != nil {
		t.Fatalf("first reservation/bind = %#v/%v", lease, err)
	}
	tracker.yield(lease)
	completed := make(chan error, 1)
	go func() {
		_, reserveErr := tracker.reserve(expiresAt)
		completed <- reserveErr
	}()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("tracker called the codec clock while holding its lock")
	}
}
