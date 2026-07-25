package password

import (
	"context"
	"sync"
	"time"
)

// Admission bounds active and queued expensive operations without worker
// goroutines. It is safe for concurrent use.
type Admission struct {
	mu         sync.Mutex
	capacity   int
	active     int
	queueLimit int
	queued     int
	closed     chan struct{}
	notify     chan struct{}
	closing    bool
}

// NewAdmission constructs an open controller with hard active and queue limits.
func NewAdmission(concurrent, queue int) (*Admission, error) {
	if concurrent < 1 || queue < 0 {
		return nil, newError(ErrInvalidPolicy, "configure admission", nil)
	}
	return newAdmission(concurrent, queue), nil
}

func newAdmission(concurrent, queue int) *Admission {
	return &Admission{capacity: concurrent, queueLimit: queue, closed: make(chan struct{}), notify: make(chan struct{}, 1)}
}

// Acquire waits for capacity within ctx and returns an idempotent release
// function. Queue overflow returns ErrAdmission; shutdown returns ErrClosed.
func (a *Admission) Acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.closing {
		a.mu.Unlock()
		return nil, newError(ErrClosed, "acquire", nil)
	}
	if a.active < a.capacity {
		a.active++
		a.mu.Unlock()
		return a.release(), nil
	}
	if a.queued >= a.queueLimit {
		a.mu.Unlock()
		return nil, newError(ErrAdmission, "acquire", nil)
	}
	a.queued++
	a.mu.Unlock()
	for {
		select {
		case <-a.closed:
			a.removeWaiter()
			return nil, newError(ErrClosed, "acquire", nil)
		case <-ctx.Done():
			a.removeWaiter()
			return nil, ctx.Err()
		case <-a.notify:
			release, decided, err := a.admitNotifiedWaiter()
			if decided {
				return release, err
			}
		}
	}
}

func (a *Admission) admitNotifiedWaiter() (func(), bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closing {
		a.queued--
		return nil, true, newError(ErrClosed, "acquire", nil)
	}
	if a.active >= a.capacity {
		return nil, false, nil
	}
	a.queued--
	a.active++
	a.signalWaiterLocked()
	return a.release(), true, nil
}

func (a *Admission) removeWaiter() { a.mu.Lock(); a.queued--; a.mu.Unlock() }

func (a *Admission) signalWaiterLocked() {
	if a.queued == 0 || a.active >= a.capacity {
		return
	}
	select {
	case a.notify <- struct{}{}:
	default:
	}
}

func (a *Admission) release() func() {
	var once sync.Once
	return func() { once.Do(func() { a.mu.Lock(); a.active--; a.signalWaiterLocked(); a.mu.Unlock() }) }
}

// Queued returns a concurrency-safe instantaneous waiter count.
func (a *Admission) Queued() int { a.mu.Lock(); defer a.mu.Unlock(); return a.queued }

// Active returns a concurrency-safe instantaneous active-operation count.
func (a *Admission) Active() int { a.mu.Lock(); defer a.mu.Unlock(); return a.active }

// Closed reports whether Shutdown has begun.
func (a *Admission) Closed() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.closing }

// Shutdown rejects new work, wakes queued callers, and waits for admitted work
// to release capacity. It is repeatable and bounded by ctx.
func (a *Admission) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	if !a.closing {
		a.closing = true
		close(a.closed)
	}
	a.mu.Unlock()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for a.Active() != 0 || a.Queued() != 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}
