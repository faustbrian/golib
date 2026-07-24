package manual

import (
	"container/heap"
	"time"

	clockpkg "github.com/faustbrian/golib/pkg/clock"
)

type eventState struct {
	active bool
	event  *scheduledEvent
}

type scheduledEvent struct {
	deadline time.Duration
	sequence uint64
	state    *eventState
	owner    any
	index    int
}

type eventHeap []*scheduledEvent

func (events eventHeap) Len() int { return len(events) }
func (events eventHeap) Less(left, right int) bool {
	if events[left].deadline == events[right].deadline {
		return events[left].sequence < events[right].sequence
	}
	return events[left].deadline < events[right].deadline
}
func (events eventHeap) Swap(left, right int) {
	events[left], events[right] = events[right], events[left]
	events[left].index = left
	events[right].index = right
}
func (events *eventHeap) Push(value any) {
	event := value.(*scheduledEvent) //nolint:forcetypeassert // The owned heap stores only scheduled events.
	event.index = len(*events)
	*events = append(*events, event)
}
func (events *eventHeap) Pop() any {
	old := *events
	last := old[len(old)-1]
	old[len(old)-1] = nil
	*events = old[:len(old)-1]
	last.index = -1
	return last
}

func (clock *Clock) activateLocked(state *eventState, duration time.Duration, owner any) error {
	if clock.closed {
		return ErrClosed
	}
	if clock.active >= clock.limits.MaxActive {
		return ErrActiveLimit
	}
	deadline := clock.elapsed
	if duration > 0 {
		var ok bool
		deadline, ok = addDuration(clock.elapsed, duration)
		if !ok {
			return clockpkg.ErrOverflow
		}
	}
	if clock.sequence == ^uint64(0) {
		return clockpkg.ErrOverflow
	}
	state.active = true
	clock.active++
	clock.replaceScheduledLocked(state, deadline, owner)
	clock.signalLocked()
	return nil
}

func (clock *Clock) rescheduleLocked(state *eventState, duration time.Duration, owner any) error {
	deadline := clock.elapsed
	if duration > 0 {
		var ok bool
		deadline, ok = addDuration(clock.elapsed, duration)
		if !ok {
			return clockpkg.ErrOverflow
		}
	}
	if clock.sequence == ^uint64(0) {
		return clockpkg.ErrOverflow
	}
	clock.replaceScheduledLocked(state, deadline, owner)
	clock.signalLocked()
	return nil
}

func (clock *Clock) replaceScheduledLocked(state *eventState, deadline time.Duration, owner any) {
	clock.removeScheduledLocked(state)
	clock.sequence++
	event := &scheduledEvent{
		deadline: deadline, sequence: clock.sequence, state: state, owner: owner,
	}
	state.event = event
	heap.Push(&clock.events, event)
}

func (clock *Clock) nextValidLocked() *scheduledEvent {
	if clock.events.Len() == 0 {
		return nil
	}
	return clock.events[0]
}

func (clock *Clock) fireLocked(event *scheduledEvent) func() {
	event.state.event = nil
	firedAt := clock.wallAt(event.deadline)
	switch owner := event.owner.(type) {
	case *Timer:
		owner.state.active = false
		clock.active--
		owner.channel <- firedAt
	case *Ticker:
		select {
		case owner.channel <- firedAt:
		default:
		}
		if err := clock.rescheduleLocked(&owner.state, owner.interval, owner); err != nil {
			owner.state.active = false
			clock.active--
		}
	case *Callback:
		owner.state.active = false
		clock.active--
		return owner.function
	case *sleepWaiter:
		owner.state.active = false
		clock.active--
		owner.done <- nil
	}
	return nil
}

func (clock *Clock) removeScheduledLocked(state *eventState) {
	if state.event == nil {
		return
	}
	heap.Remove(&clock.events, state.event.index)
	state.event = nil
}

type sleepWaiter struct {
	state eventState
	done  chan error
}
