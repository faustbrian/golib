package semaphore_test

import (
	"context"
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/semaphore"
)

func Example() {
	sem, err := semaphore.New(semaphore.Config{Capacity: 4, MaxWaiters: 32})
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	value, err := semaphore.Execute(ctx, sem, 2, func(context.Context) (string, error) {
		return "finished", nil
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(value, sem.Snapshot().Available)
	// Output: finished 4
}

func ExampleSemaphore_Acquire() {
	sem, err := semaphore.New(semaphore.Config{Capacity: 3, MaxWaiters: 8})
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	permit, err := sem.Acquire(ctx, 2)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := permit.Release(); err != nil {
			panic(err)
		}
	}()

	fmt.Println(permit.Weight(), sem.Snapshot().Available)
	// Output: 2 1
}
