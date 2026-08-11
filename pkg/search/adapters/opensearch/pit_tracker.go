package opensearch

import (
	"errors"
	"sync"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

const (
	// DefaultMaximumOpenPointInTimes bounds abandoned cursor resources per
	// client unless the application selects a smaller explicit budget.
	DefaultMaximumOpenPointInTimes = 64
	// MaximumOpenPointInTimes is the largest accepted per-client PIT budget.
	MaximumOpenPointInTimes = 4_096
)

var (
	// ErrPointInTimeCapacity identifies rejection before PIT creation because
	// this client already owns its configured maximum number of live PITs.
	ErrPointInTimeCapacity = errors.New("search/opensearch: point in time capacity exhausted")
	// ErrPointInTimeInUse identifies concurrent consumption of one cursor by
	// the same client. Cursor continuations are single-consumer operations.
	ErrPointInTimeInUse      = errors.New("search/opensearch: point in time cursor is already in use")
	errPointInTimeIDConflict = errors.New("search/opensearch: point in time identifier is already owned")
)

// PointInTimeSnapshot reports the bounded process-local PIT ownership state.
// Open excludes cursors created by another client instance and cursors whose
// signed expiry has elapsed.
type PointInTimeSnapshot struct {
	Maximum int
	Open    int
}

type pointInTimeLease struct {
	id        string
	expiresAt time.Time
	active    bool
	inUse     bool
}

type pointInTimeTracker struct {
	codec   *search.CursorCodec
	maximum int

	mu struct {
		sync.Mutex
		closed bool
		open   int
		byID   map[string]*pointInTimeLease
		leases map[*pointInTimeLease]struct{}
	}
}

func newPointInTimeTracker(codec *search.CursorCodec, maximum int) *pointInTimeTracker {
	tracker := &pointInTimeTracker{codec: codec, maximum: maximum}
	tracker.mu.byID = make(map[string]*pointInTimeLease, maximum)
	tracker.mu.leases = make(map[*pointInTimeLease]struct{}, maximum)
	return tracker
}

func (tracker *pointInTimeTracker) reserve(expiresAt time.Time) (*pointInTimeLease, error) {
	tracker.reapExpired()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.mu.closed {
		return nil, ErrClosed
	}
	if tracker.mu.open >= tracker.maximum {
		return nil, errors.Join(ErrBackpressure, ErrPointInTimeCapacity)
	}
	lease := &pointInTimeLease{expiresAt: expiresAt, active: true, inUse: true}
	tracker.mu.leases[lease] = struct{}{}
	tracker.mu.open++
	return lease, nil
}

func (tracker *pointInTimeTracker) bind(lease *pointInTimeLease, id string, expiresAt time.Time) error {
	if lease == nil {
		return nil
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.mu.closed || !lease.active {
		return ErrClosed
	}
	if existing := tracker.mu.byID[id]; existing != nil && existing != lease {
		return errPointInTimeIDConflict
	}
	if lease.id != "" {
		delete(tracker.mu.byID, lease.id)
	}
	lease.id = id
	lease.expiresAt = expiresAt
	tracker.mu.byID[id] = lease
	return nil
}

func (tracker *pointInTimeTracker) acquire(id string, expiresAt time.Time) (*pointInTimeLease, error) {
	tracker.reapExpired()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.mu.closed {
		return nil, ErrClosed
	}
	if lease := tracker.mu.byID[id]; lease != nil {
		if !lease.active {
			return nil, ErrClosed
		}
		if lease.inUse {
			return nil, ErrPointInTimeInUse
		}
		lease.inUse = true
		return lease, nil
	}
	if tracker.mu.open >= tracker.maximum {
		return nil, errors.Join(ErrBackpressure, ErrPointInTimeCapacity)
	}
	lease := &pointInTimeLease{id: id, expiresAt: expiresAt, active: true, inUse: true}
	tracker.mu.byID[id] = lease
	tracker.mu.leases[lease] = struct{}{}
	tracker.mu.open++
	return lease, nil
}

func (tracker *pointInTimeTracker) rotate(lease *pointInTimeLease, oldID, newID string) bool {
	if lease == nil || oldID == newID {
		return true
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if !lease.active || !lease.inUse || lease.id != oldID || tracker.mu.byID[oldID] != lease {
		return false
	}
	if existing := tracker.mu.byID[newID]; existing != nil && existing != lease {
		return false
	}
	delete(tracker.mu.byID, oldID)
	lease.id = newID
	tracker.mu.byID[newID] = lease
	return true
}

func (tracker *pointInTimeTracker) yield(lease *pointInTimeLease) {
	if lease == nil {
		return
	}
	tracker.mu.Lock()
	if lease.active {
		lease.inUse = false
	}
	tracker.mu.Unlock()
}

func (tracker *pointInTimeTracker) release(lease *pointInTimeLease) {
	if lease == nil {
		return
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.releaseLocked(lease)
}

func (tracker *pointInTimeTracker) snapshot() PointInTimeSnapshot {
	if tracker == nil {
		return PointInTimeSnapshot{}
	}
	tracker.reapExpired()
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return PointInTimeSnapshot{Maximum: tracker.maximum, Open: tracker.mu.open}
}

func (tracker *pointInTimeTracker) close() {
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	tracker.mu.closed = true
	for lease := range tracker.mu.leases {
		lease.active = false
		lease.inUse = false
	}
	tracker.mu.open = 0
	clear(tracker.mu.byID)
	clear(tracker.mu.leases)
	tracker.mu.Unlock()
}

func (tracker *pointInTimeTracker) reapExpired() {
	tracker.mu.Lock()
	leases := make([]*pointInTimeLease, 0, len(tracker.mu.leases))
	for lease := range tracker.mu.leases {
		if lease.active {
			leases = append(leases, lease)
		}
	}
	tracker.mu.Unlock()
	for _, lease := range leases {
		if _, err := tracker.codec.Remaining(lease.expiresAt); err != nil {
			tracker.mu.Lock()
			if lease.active {
				tracker.releaseLocked(lease)
			}
			tracker.mu.Unlock()
		}
	}
}

func (tracker *pointInTimeTracker) releaseLocked(lease *pointInTimeLease) {
	if !lease.active {
		return
	}
	if lease.id != "" && tracker.mu.byID[lease.id] == lease {
		delete(tracker.mu.byID, lease.id)
	}
	lease.active = false
	lease.inUse = false
	delete(tracker.mu.leases, lease)
	if tracker.mu.open > 0 {
		tracker.mu.open--
	}
}
