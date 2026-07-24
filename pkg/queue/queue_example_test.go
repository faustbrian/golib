package queue_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
)

func ExampleWithWorkerLifecycle() {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	lifecycle, err := management.NewWorkerLifecycle(
		management.WorkerLifecycleConfig{
			Metadata: management.StatusMetadata{
				ID: "worker-1", Version: "v1.0.0", Concurrency: 1,
				Protocol: management.ProtocolVersion{Major: 1},
			},
			WorkerGroup: "payments", Queue: "critical",
			MaxCommandResults: 100, Now: func() time.Time { return now },
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	q, err := queue.NewQueue(
		queue.WithWorker(exampleManagedWorker{now: now}),
		queue.WithWorkerLifecycle(lifecycle),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer q.Release()

	status, err := q.ObserveWorker(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(status.State, status.CurrentJobs)
	// Output: running 0
}

func ExampleNewPool_queueTask() {
	taskN := 7
	rets := make(chan int, taskN)
	// allocate a pool with 5 goroutines to deal with those tasks
	p := queue.NewPool(5)
	// don't forget to release the pool in the end
	defer p.Release()

	// assign tasks to asynchronous goroutine pool
	for i := 0; i < taskN; i++ {
		idx := i
		if err := p.QueueTask(func(context.Context) error {
			rets <- idx
			return nil
		}); err != nil {
			log.Println(err)
		}
	}

	// wait until all tasks done
	for i := 0; i < taskN; i++ {
		fmt.Println("index:", <-rets)
	}

	// Unordered output:
	// index: 3
	// index: 0
	// index: 2
	// index: 4
	// index: 5
	// index: 6
	// index: 1
}

func ExampleNewPool_queueTaskTimeout() {
	taskN := 7
	rets := make(chan int, taskN)
	resps := make(chan error, 1)
	completed := make(chan struct{}, taskN)
	// allocate a pool with 5 goroutines to deal with those tasks
	q := queue.NewPool(5, queue.WithAfterFn(func() {
		completed <- struct{}{}
	}))
	// don't forget to release the pool in the end
	defer q.Release()

	// assign tasks to asynchronous goroutine pool
	for i := 0; i < taskN; i++ {
		idx := i
		if err := q.QueueTask(func(ctx context.Context) error {
			// panic job
			if idx == 5 {
				panic("system error")
			}
			// timeout job
			if idx == 6 {
				<-ctx.Done()
			}
			select {
			case <-ctx.Done():
				resps <- ctx.Err()
			default:
			}

			rets <- idx
			return nil
		}, job.AllowOption{
			Timeout: job.Time(100 * time.Millisecond),
		}); err != nil {
			log.Println(err)
		}
	}

	// wait until all tasks done
	for i := 0; i < taskN-1; i++ {
		fmt.Println("index:", <-rets)
	}
	for i := 0; i < taskN; i++ {
		<-completed
	}
	close(resps)
	for e := range resps {
		fmt.Println(e.Error())
	}

	fmt.Println("success task count:", q.SuccessTasks())
	fmt.Println("failure task count:", q.FailureTasks())
	fmt.Println("submitted task count:", q.SubmittedTasks())

	// Unordered output:
	// index: 3
	// index: 0
	// index: 2
	// index: 4
	// index: 6
	// index: 1
	// context deadline exceeded
	// success task count: 5
	// failure task count: 2
	// submitted task count: 7
}

type exampleManagedWorker struct {
	now time.Time
}

func (exampleManagedWorker) Run(context.Context, core.TaskMessage) error { return nil }
func (exampleManagedWorker) Shutdown() error                             { return nil }
func (exampleManagedWorker) Queue(core.TaskMessage) error                { return nil }
func (exampleManagedWorker) Request() (core.TaskMessage, error) {
	return nil, queue.ErrNoTaskInQueue
}
func (w exampleManagedWorker) ObserveWorker(context.Context) (management.WorkerStatus, error) {
	return management.WorkerStatus{
		ID: "worker-1", Version: "v1.0.0", StartedAt: w.now,
		HeartbeatAt: w.now, Queues: []string{"critical"}, Concurrency: 1,
		State: management.WorkerRunning, DrainStatus: management.DrainNotRequested,
		Backend: "example", Protocol: management.ProtocolVersion{Major: 1},
	}, nil
}
func (w exampleManagedWorker) ObserveQueue(context.Context) (management.QueueStatus, error) {
	return management.QueueStatus{
		Backend: "example", Queue: "critical", ObservedAt: w.now,
	}, nil
}
