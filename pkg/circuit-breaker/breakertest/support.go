package breakertest

import (
	"fmt"
	"sync"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

// Recorder is a bounded, concurrency-safe transition observer for tests.
type Recorder struct {
	mu       sync.Mutex
	events   []breaker.TransitionEvent
	capacity int
	dropped  uint64
}

// NewRecorder constructs a recorder that keeps the most recent capacity events.
func NewRecorder(capacity int) (*Recorder, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("breakertest: recorder capacity must be greater than zero")
	}
	return &Recorder{
		events:   make([]breaker.TransitionEvent, 0, capacity),
		capacity: capacity,
	}, nil
}

// Observe records an event and drops the oldest event when full.
func (r *Recorder) Observe(event breaker.TransitionEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == r.capacity {
		copy(r.events, r.events[1:])
		r.events[len(r.events)-1] = event
		r.dropped++
		return nil
	}
	r.events = append(r.events, event)
	return nil
}

// Events returns a copy of retained events in delivery order.
func (r *Recorder) Events() []breaker.TransitionEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]breaker.TransitionEvent(nil), r.events...)
}

// Dropped returns the number of events evicted since construction or Reset.
func (r *Recorder) Dropped() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

// Reset clears recorded events and the dropped count while retaining capacity.
func (r *Recorder) Reset() {
	r.mu.Lock()
	r.events = r.events[:0]
	r.dropped = 0
	r.mu.Unlock()
}

// ScriptedClassifier returns a fixed sequence and then a stable fallback.
// It records only call counts and never retains Completion values.
type ScriptedClassifier struct {
	mu       sync.Mutex
	fallback breaker.Outcome
	outcomes []breaker.Outcome
	next     int
}

// NewScriptedClassifier constructs a concurrency-safe scripted classifier.
func NewScriptedClassifier(
	fallback breaker.Outcome,
	outcomes ...breaker.Outcome,
) *ScriptedClassifier {
	return &ScriptedClassifier{
		fallback: fallback,
		outcomes: append([]breaker.Outcome(nil), outcomes...),
	}
}

// Classify returns the next scripted outcome or the fallback when exhausted.
func (c *ScriptedClassifier) Classify(_ breaker.Completion) breaker.Outcome {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.next >= len(c.outcomes) {
		c.next++
		return c.fallback
	}
	outcome := c.outcomes[c.next]
	c.next++
	return outcome
}

// Calls returns the number of completed Classify calls.
func (c *ScriptedClassifier) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.next
}

// Remaining returns the number of scripted outcomes not yet returned.
func (c *ScriptedClassifier) Remaining() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := len(c.outcomes) - c.next
	if remaining < 0 {
		return 0
	}
	return remaining
}
