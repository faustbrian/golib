// Package history provides bounded in-memory scheduler event history.
package history

import (
	"errors"
	"sync"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
)

// MaxCapacity is the largest supported in-memory event history.
const MaxCapacity = 1_000_000

// ErrInvalidCapacity reports a history capacity outside the supported range.
var ErrInvalidCapacity = errors.New("scheduler history: capacity must be between 1 and 1000000")

// Buffer is a concurrency-safe, bounded, oldest-first event history.
type Buffer struct {
	mu       sync.Mutex
	entries  []scheduler.Event
	capacity int
}

// NewBuffer constructs an event history with a fixed capacity.
func NewBuffer(capacity int) (*Buffer, error) {
	if capacity < 1 || capacity > MaxCapacity {
		return nil, ErrInvalidCapacity
	}
	return &Buffer{entries: make([]scheduler.Event, 0, capacity), capacity: capacity}, nil
}

// Observe appends an event and evicts the oldest event when full.
func (buffer *Buffer) Observe(event scheduler.Event) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if len(buffer.entries) == buffer.capacity {
		copy(buffer.entries, buffer.entries[1:])
		buffer.entries[len(buffer.entries)-1] = event
		return
	}
	buffer.entries = append(buffer.entries, event)
}

// Entries returns a snapshot ordered from oldest to newest.
func (buffer *Buffer) Entries() []scheduler.Event {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return append([]scheduler.Event(nil), buffer.entries...)
}
