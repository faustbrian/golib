package password

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAdmissionAcquireAndShutdownBoundariesFailFast(t *testing.T) {
	immediate := newAdmission(1, 0)
	immediateContext, cancelImmediate := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelImmediate()
	release, err := immediate.Acquire(immediateContext)
	if err != nil || immediate.Active() != 1 {
		t.Fatalf("immediate acquire = active %d, error %v", immediate.Active(), err)
	}
	release()
	if immediate.Active() != 0 {
		t.Fatalf("released active count = %d", immediate.Active())
	}

	full := newAdmission(1, 0)
	full.active = 1
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := full.Acquire(ctx); !errors.Is(err, ErrAdmission) {
		t.Fatalf("zero-queue acquire error = %v", err)
	}

	queued := newAdmission(1, 1)
	queued.active = 1
	queueContext, cancelQueue := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelQueue()
	if _, err := queued.Acquire(queueContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued acquire error = %v", err)
	}
	if queued.Queued() != 0 {
		t.Fatalf("queued count after timeout = %d", queued.Queued())
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelShutdown()
	if err := immediate.Shutdown(shutdownContext); err != nil {
		t.Fatalf("drained shutdown error = %v", err)
	}
}

func TestNotifiedWaiterStateTransitions(t *testing.T) {
	closed := newAdmission(1, 1)
	closed.queued = 1
	closed.closing = true
	if _, decided, err := closed.admitNotifiedWaiter(); !decided || !errors.Is(err, ErrClosed) || closed.queued != 0 {
		t.Fatalf("closed = decided %v error %v queued %d", decided, err, closed.queued)
	}
	full := newAdmission(1, 1)
	full.active = 1
	full.queued = 1
	if _, decided, err := full.admitNotifiedWaiter(); decided || err != nil || full.queued != 1 {
		t.Fatalf("full = decided %v error %v queued %d", decided, err, full.queued)
	}
	ready := newAdmission(2, 2)
	ready.queued = 2
	release, decided, err := ready.admitNotifiedWaiter()
	if !decided || err != nil || ready.active != 1 || ready.queued != 1 {
		t.Fatalf("ready = decided %v error %v active %d queued %d", decided, err, ready.active, ready.queued)
	}
	release()
}

func TestSignalWaiterExactStates(t *testing.T) {
	for _, admission := range []*Admission{
		{capacity: 1, queued: 0, notify: make(chan struct{}, 1)},
		{capacity: 1, active: 1, queued: 1, notify: make(chan struct{}, 1)},
	} {
		admission.signalWaiterLocked()
		if len(admission.notify) != 0 {
			t.Fatal("signaled a waiter without available queued capacity")
		}
	}
	ready := &Admission{capacity: 1, queued: 1, notify: make(chan struct{}, 1)}
	ready.signalWaiterLocked()
	if len(ready.notify) != 1 {
		t.Fatal("did not signal a queued waiter with available capacity")
	}
}
