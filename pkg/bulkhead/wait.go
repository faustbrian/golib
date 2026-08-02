package bulkhead

import (
	"container/list"
	"context"
	"errors"
	"time"
)

type waiter struct {
	weight   int64
	deadline time.Time
	ready    chan struct{}
	element  *list.Element
	done     bool
	granted  bool
	terminal error
}

func (bulkhead *Bulkhead) wait(ctx context.Context, waiter *waiter, start time.Time) (*Permit, error) {
	remaining := waiter.deadline.Sub(bulkhead.now())
	if remaining <= 0 {
		return bulkhead.finishWait(waiter, start, RejectionTimeout, ErrWaitTimeout)
	}
	timer := bulkhead.config.clock.NewTimer(remaining)
	defer timer.Stop()

	select {
	case <-waiter.ready:
		return bulkhead.finishWait(waiter, start, "", nil)
	case <-ctx.Done():
		return bulkhead.finishWait(
			waiter,
			start,
			RejectionCaller,
			errors.Join(ErrCallerCanceled, ctx.Err()),
		)
	case <-timer.C():
		return bulkhead.finishWait(waiter, start, RejectionTimeout, ErrWaitTimeout)
	}
}

func (bulkhead *Bulkhead) finishWait(
	waiter *waiter,
	start time.Time,
	reason RejectionReason,
	terminal error,
) (*Permit, error) {
	now := bulkhead.now()
	waited := nonNegativeDuration(now.Sub(start))

	bulkhead.mu.Lock()
	if waiter.granted {
		bulkhead.totalWait += waited
		event := bulkhead.eventLocked(EventAdmitted, "", waiter.weight, waited, 0)
		bulkhead.mu.Unlock()
		bulkhead.observe(event)
		return &Permit{bulkhead: bulkhead, weight: waiter.weight, waitDuration: waited}, nil
	}
	if waiter.terminal != nil {
		terminal = waiter.terminal
		reason = rejectionReason(terminal)
	}
	if !waiter.done {
		waiter.done = true
		waiter.terminal = terminal
	}
	bulkhead.removeWaiterLocked(waiter)
	bulkhead.grantWaitersLocked(now)
	event, drained, terminal := bulkhead.failedWaitLocked(waiter.weight, reason, terminal, waited)
	bulkhead.mu.Unlock()
	bulkhead.observe(event)
	bulkhead.observeDrained(drained)
	return nil, terminal
}

func (bulkhead *Bulkhead) grantWaitersLocked(now time.Time) {
	if bulkhead.draining {
		return
	}
	for element := bulkhead.waiters.Front(); element != nil; {
		next := element.Next()
		waiter := element.Value.(*waiter)
		if waiter.done {
			element = next
			continue
		}
		if !now.Before(waiter.deadline) {
			waiter.done = true
			waiter.terminal = ErrWaitTimeout
			close(waiter.ready)
			element = next
			continue
		}
		if !bulkhead.semaphore.TryAcquire(waiter.weight) {
			return
		}
		bulkhead.removeWaiterLocked(waiter)
		waiter.done = true
		waiter.granted = true
		bulkhead.active += waiter.weight
		bulkhead.admissions++
		close(waiter.ready)
		element = next
	}
}

func (bulkhead *Bulkhead) removeWaiterLocked(waiter *waiter) {
	if waiter.element == nil {
		return
	}
	bulkhead.waiters.Remove(waiter.element)
	waiter.element = nil
	bulkhead.queued--
}

func rejectionReason(err error) RejectionReason {
	switch {
	case errors.Is(err, ErrClosed):
		return RejectionShutdown
	case errors.Is(err, ErrWaitTimeout):
		return RejectionTimeout
	default:
		return RejectionCaller
	}
}

func nonNegativeDuration(duration time.Duration) time.Duration {
	return max(duration, 0)
}
