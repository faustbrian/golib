package bulkhead

import (
	"context"
	"errors"
)

// Close idempotently stops new admission and releases queued callers with
// ErrClosed. Existing permits remain valid and retain capacity until released.
func (bulkhead *Bulkhead) Close() error {
	bulkhead.mu.Lock()
	if bulkhead.draining {
		bulkhead.mu.Unlock()
		return nil
	}
	bulkhead.draining = true
	for element := bulkhead.waiters.Front(); element != nil; element = element.Next() {
		waiter := element.Value.(*waiter)
		if waiter.done {
			continue
		}
		waiter.done = true
		waiter.terminal = ErrClosed
		close(waiter.ready)
	}
	event := bulkhead.eventLocked(EventDraining, "", 0, 0, 0)
	drained := bulkhead.maybeDrainedLocked()
	bulkhead.mu.Unlock()

	bulkhead.observe(event)
	bulkhead.observeDrained(drained)
	return nil
}

// Drain closes admission and waits for both admitted and queued callers to
// terminate within ctx. An incomplete result does not reclaim live capacity.
func (bulkhead *Bulkhead) Drain(ctx context.Context) error {
	if ctx == nil {
		return errors.Join(ErrDrainIncomplete, context.Canceled)
	}
	_ = bulkhead.Close()
	select {
	case <-bulkhead.drained:
		return nil
	case <-ctx.Done():
		return errors.Join(ErrDrainIncomplete, ctx.Err())
	}
}
