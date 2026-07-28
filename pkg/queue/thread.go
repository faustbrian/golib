package queue

import (
	"context"
	"sync"
)

// routineGroup owns a set of joined goroutines and exposes a context-aware
// idle signal.
//
// Design rationale:
//   - Run registers work before launching it.
//   - The idle channel is replaced atomically when a new generation starts.
//   - WaitContext never creates a waiter goroutine that could leak.
type routineGroup struct {
	mu     sync.Mutex
	active int
	idle   chan struct{}
}

// newRoutineGroup creates a new routineGroup for managing goroutines.
func newRoutineGroup() *routineGroup {
	idle := make(chan struct{})
	close(idle)

	return &routineGroup{idle: idle}
}

// Run launches a goroutine to execute the provided function.
// The function is registered before launch and tracked until it completes.
// This method is safe to call concurrently.
//
// Example:
//
//	rg := newRoutineGroup()
//	rg.Run(func() {
//	    // Do work in background
//	})
//	rg.Wait() // Wait for all goroutines to complete
func (g *routineGroup) Run(fn func()) {
	g.mu.Lock()
	if g.active == 0 {
		g.idle = make(chan struct{})
	}
	g.active++
	g.mu.Unlock()

	go func() {
		defer g.done()
		fn()
	}()
}

// Wait blocks until all goroutines launched via Run() have completed.
// This method is safe to call multiple times and from multiple goroutines.
func (g *routineGroup) Wait() {
	_ = g.WaitContext(context.Background())
}

// WaitContext waits for every currently owned routine while allowing a
// lifecycle owner to enforce its shutdown budget without creating a waiter
// goroutine that could outlive that budget.
func (g *routineGroup) WaitContext(ctx context.Context) error {
	if ctx == nil {
		return context.Canceled
	}
	g.mu.Lock()
	idle := g.idle
	g.mu.Unlock()

	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *routineGroup) done() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active--
	if g.active == 0 {
		close(g.idle)
	}
}
