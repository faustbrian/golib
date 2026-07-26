package password

import (
	"errors"
	"testing"
)

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
