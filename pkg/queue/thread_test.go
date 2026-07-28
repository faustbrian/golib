package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestRoutineGroupRun(t *testing.T) {
	t.Run("execute single function", func(t *testing.T) {
		g := newRoutineGroup()
		var counter int32

		g.Run(func() {
			atomic.AddInt32(&counter, 1)
		})

		g.Wait()

		if atomic.LoadInt32(&counter) != 1 {
			t.Errorf("expected counter to be 1, got %d", counter)
		}
	})

	t.Run("execute multiple functions", func(t *testing.T) {
		g := newRoutineGroup()
		var counter int32
		numRoutines := 10

		for i := 0; i < numRoutines; i++ {
			g.Run(func() {
				atomic.AddInt32(&counter, 1)
			})
		}

		g.Wait()

		if atomic.LoadInt32(&counter) != int32(numRoutines) {
			t.Errorf("expected counter to be %d, got %d", numRoutines, counter)
		}
	})
}

func TestRoutineGroupWaitContextCanBeCanceledWithoutLeakingAWaiter(t *testing.T) {
	group := newRoutineGroup()
	started := make(chan struct{})
	release := make(chan struct{})
	group.Run(func() {
		close(started)
		<-release
	})
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := group.WaitContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitContext() error = %v, want context cancellation", err)
	}
	close(release)
	group.Wait()
}
