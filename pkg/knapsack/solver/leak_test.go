package solver_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/constraint"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
)

const cancellationRounds = 32

type cancellationBarrier struct {
	entered chan struct{}
	once    sync.Once
}

func (barrier *cancellationBarrier) Check(ctx context.Context, _ constraint.PlacementView) constraint.Decision {
	barrier.once.Do(func() { close(barrier.entered) })
	<-ctx.Done()

	return constraint.Reject("cancelled", "test cancellation")
}

func TestSolversStopAfterRepeatedCancellation(t *testing.T) {
	request := exactRequest(t, 4, 1)
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	solvers := map[string]func(context.Context, solver.Options) error{
		"exact": func(ctx context.Context, options solver.Options) error {
			_, err := (solver.Exact{}).PackFixed(ctx, request, instances, options)

			return err
		},
		"heuristic": func(ctx context.Context, options solver.Options) error {
			_, err := (solver.Heuristic{}).PackFixed(ctx, request, instances, options)

			return err
		},
	}

	runtime.GC()
	baseline := runtime.NumGoroutine()
	for name, solve := range solvers {
		for round := range cancellationRounds {
			barrier := &cancellationBarrier{entered: make(chan struct{})}
			ctx, cancel := context.WithCancel(context.Background())
			returned := make(chan error, 1)
			go func() {
				returned <- solve(ctx, solver.Options{
					Constraints: []constraint.Placement{barrier},
				})
			}()

			select {
			case <-barrier.entered:
			case <-time.After(time.Second):
				cancel()
				t.Fatalf("%s round %d did not reach the callback", name, round)
			}
			cancel()
			select {
			case err := <-returned:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("%s round %d error = %v", name, round, err)
				}
			case <-time.After(time.Second):
				t.Fatalf("%s round %d continued after cancellation", name, round)
			}
		}
	}

	runtime.GC()
	if current := runtime.NumGoroutine(); current > baseline {
		t.Fatalf("goroutines after cancellation = %d; baseline = %d", current, baseline)
	}
}
