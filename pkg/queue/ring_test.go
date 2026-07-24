package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func waitForRingSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func TestMaxCapacity(t *testing.T) {
	w := NewRing(WithQueueSize(2))

	assert.NoError(t, w.Queue(&mockMessage{}))
	assert.NoError(t, w.Queue(&mockMessage{}))
	assert.Error(t, w.Queue(&mockMessage{}))

	err := w.Queue(&mockMessage{})
	assert.Equal(t, ErrMaxCapacity, err)
}

func TestRingCapacityIsAtomicUnderConcurrentEnqueue(t *testing.T) {
	const producers = 100
	worker := NewRing(WithQueueSize(1))
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	var accepted atomic.Int64
	ready.Add(producers)
	done.Add(producers)

	for range producers {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			if worker.Queue(&mockMessage{}) == nil {
				accepted.Add(1)
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()

	assert.Equal(t, int64(1), accepted.Load())
	assert.Equal(t, 1, worker.count)
}

func TestCustomFuncAndWait(t *testing.T) {
	m := mockMessage{
		message: "foo",
	}
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	w := NewRing(
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			started <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}),
	)
	q, err := NewQueue(
		WithWorker(w),
		WithWorkerCount(2),
		WithLogger(NewLogger()),
	)
	assert.NoError(t, err)
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	q.Start()
	waitForRingSignal(t, started)
	waitForRingSignal(t, started)
	assert.Equal(t, 2, int(q.metric.BusyWorkers()))
	close(release)
	require.Eventually(t, func() bool { return q.CompletedTasks() == 4 }, time.Second, time.Millisecond)
	q.Release()
}

func TestEnqueueJobAfterShutdown(t *testing.T) {
	m := mockMessage{
		message: "foo",
	}
	w := NewRing()
	q, err := NewQueue(
		WithWorker(w),
		WithWorkerCount(2),
	)
	assert.NoError(t, err)
	q.Start()
	q.Shutdown()
	// can't queue task after shutdown
	err = q.Queue(m)
	assert.Error(t, err)
	assert.Equal(t, ErrQueueShutdown, err)
	q.Wait()
}

func TestJobReachTimeout(t *testing.T) {
	m := mockMessage{
		message: "foo",
	}
	started := make(chan struct{})
	deadline := make(chan error, 1)
	w := NewRing(
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			close(started)
			<-ctx.Done()
			deadline <- ctx.Err()
			return ctx.Err()
		}),
	)
	q, err := NewQueue(
		WithWorker(w),
		WithWorkerCount(2),
	)
	assert.NoError(t, err)
	assert.NoError(t, q.Queue(m, job.AllowOption{Timeout: job.Time(30 * time.Millisecond)}))
	q.Start()
	waitForRingSignal(t, started)
	assert.ErrorIs(t, <-deadline, context.DeadlineExceeded)
	q.Release()
}

func TestCancelJobAfterShutdown(t *testing.T) {
	m := mockMessage{
		message: "foo",
	}
	started := make(chan struct{}, 2)
	canceled := make(chan error, 2)
	w := NewRing(
		WithLogger(NewEmptyLogger()),
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			started <- struct{}{}
			<-ctx.Done()
			canceled <- ctx.Err()
			return ctx.Err()
		}),
	)
	q, err := NewQueue(
		WithWorker(w),
		WithWorkerCount(2),
	)
	assert.NoError(t, err)
	assert.NoError(t, q.Queue(m, job.AllowOption{Timeout: job.Time(time.Minute)}))
	assert.NoError(t, q.Queue(m, job.AllowOption{Timeout: job.Time(time.Minute)}))
	q.Start()
	waitForRingSignal(t, started)
	waitForRingSignal(t, started)
	assert.Equal(t, int64(2), q.BusyWorkers())
	q.Shutdown()
	assert.ErrorIs(t, <-canceled, context.Canceled)
	assert.ErrorIs(t, <-canceled, context.Canceled)
	q.Wait()
}

func TestGoroutineLeak(t *testing.T) {
	w := NewRing(
		WithLogger(NewEmptyLogger()),
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			return nil
		}),
	)
	q, err := NewQueue(
		WithLogger(NewEmptyLogger()),
		WithWorker(w),
		WithWorkerCount(10),
	)
	assert.NoError(t, err)
	for i := 0; i < 400; i++ {
		m := mockMessage{
			message: fmt.Sprintf("new message: %d", i+1),
		}

		assert.NoError(t, q.Queue(m))
	}

	q.Start()
	require.Eventually(t, func() bool { return q.CompletedTasks() == 400 }, 5*time.Second, time.Millisecond)
	q.Release()
}

func TestGoroutinePanic(t *testing.T) {
	m := mockMessage{
		message: "foo",
	}
	panicked := make(chan struct{}, 1)
	w := NewRing(
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			panicked <- struct{}{}
			panic("missing something")
		}),
	)
	q, err := NewQueue(
		WithWorker(w),
		WithWorkerCount(2),
	)
	assert.NoError(t, err)
	assert.NoError(t, q.Queue(m))
	q.Start()
	waitForRingSignal(t, panicked)
	q.Release()
	assert.Equal(t, uint64(1), q.FailureTasks())
}

func TestIncreaseWorkerCount(t *testing.T) {
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	w := NewRing(
		WithLogger(NewEmptyLogger()),
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			started <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}),
	)
	q, err := NewQueue(
		WithLogger(NewLogger()),
		WithWorker(w),
		WithWorkerCount(5),
	)
	assert.NoError(t, err)

	for i := 1; i <= 10; i++ {
		m := mockMessage{
			message: fmt.Sprintf("new message: %d", i),
		}
		assert.NoError(t, q.Queue(m))
	}

	q.Start()
	for range 5 {
		waitForRingSignal(t, started)
	}
	assert.Equal(t, int64(5), q.BusyWorkers())
	q.UpdateWorkerCount(10)
	for range 5 {
		waitForRingSignal(t, started)
	}
	assert.Equal(t, int64(10), q.BusyWorkers())
	close(release)
	q.Release()
}

func TestDecreaseWorkerCount(t *testing.T) {
	started := make(chan chan struct{}, 10)
	completed := make(chan struct{}, 10)
	w := NewRing(
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			release := make(chan struct{})
			started <- release
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}),
	)
	q, err := NewQueue(
		WithLogger(NewLogger()),
		WithWorker(w),
		WithWorkerCount(5),
		WithAfterFn(func() {
			completed <- struct{}{}
		}),
	)
	assert.NoError(t, err)

	for i := 1; i <= 10; i++ {
		m := mockMessage{
			message: fmt.Sprintf("test message: %d", i),
		}
		assert.NoError(t, q.Queue(m))
	}

	q.Start()
	waitForWorkers := func(count int) []chan struct{} {
		t.Helper()
		workers := make([]chan struct{}, 0, count)
		for range count {
			select {
			case release := <-started:
				workers = append(workers, release)
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for worker")
			}
		}

		return workers
	}

	running := waitForWorkers(5)
	assert.Equal(t, int64(5), q.BusyWorkers())
	q.UpdateWorkerCount(3)
	for _, release := range running {
		close(release)
	}
	for range 5 {
		waitForRingSignal(t, completed)
	}
	running = waitForWorkers(3)
	assert.Equal(t, int64(3), q.BusyWorkers())
	for _, release := range running {
		close(release)
	}
	for range 3 {
		waitForRingSignal(t, completed)
	}
	running = waitForWorkers(2)
	assert.Equal(t, int64(2), q.BusyWorkers())
	for _, release := range running {
		close(release)
	}
	q.Release()
}

func TestHandleAllJobBeforeShutdownRing(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	m := mocks.NewMockTaskMessage(controller)

	w := NewRing(
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			return nil
		}),
	)

	done := make(chan struct{})
	assert.NoError(t, w.Queue(m))
	assert.NoError(t, w.Queue(m))
	go func() {
		assert.NoError(t, w.Shutdown())
		done <- struct{}{}
	}()
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&w.stopFlag) == 1
	}, time.Second, time.Millisecond)
	task, err := w.Request()
	assert.NotNil(t, task)
	assert.NoError(t, err)
	task, err = w.Request()
	assert.NotNil(t, task)
	assert.NoError(t, err)
	task, err = w.Request()
	assert.Nil(t, task)
	assert.True(t, errors.Is(err, ErrQueueHasBeenClosed))
	<-done
}

func TestRingShutdownCompletionSignalIsDurable(t *testing.T) {
	worker := NewRing()
	require.NoError(t, worker.Queue(&mockMessage{}))

	atomic.StoreInt32(&worker.stopFlag, 1)
	task, err := worker.Request()
	require.NoError(t, err)
	require.NotNil(t, task)
	task, err = worker.Request()
	require.ErrorIs(t, err, ErrQueueHasBeenClosed)
	require.Nil(t, task)

	select {
	case <-worker.exit:
	default:
		t.Fatal("shutdown completion signal was lost before the waiter started")
	}
}

func TestHandleAllJobBeforeShutdownRingInQueue(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	m := mocks.NewMockTaskMessage(controller)
	m.EXPECT().Bytes().Return([]byte("test")).AnyTimes()
	m.EXPECT().Payload().Return([]byte("test")).AnyTimes()

	messages := make(chan string, 10)

	w := NewRing(
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			messages <- string(m.Payload())
			return nil
		}),
	)

	q, err := NewQueue(
		WithLogger(NewLogger()),
		WithWorker(w),
		WithWorkerCount(1),
	)
	assert.NoError(t, err)

	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	assert.Len(t, messages, 0)
	q.Start()
	q.Release()
	assert.Len(t, messages, 2)
}

func TestRetryCountWithNewMessage(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	m := mocks.NewMockQueuedMessage(controller)
	m.EXPECT().Bytes().Return([]byte("test")).AnyTimes()

	messages := make(chan string, 10)
	keep := make(chan struct{})
	count := 1

	w := NewRing(
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			if count%3 != 0 {
				count++
				return errors.New("count not correct")
			}
			close(keep)
			messages <- string(m.Payload())
			return nil
		}),
	)

	q, err := NewQueue(
		WithLogger(NewLogger()),
		WithWorker(w),
		WithWorkerCount(1),
	)
	assert.NoError(t, err)

	assert.NoError(t, q.Queue(
		m,
		job.AllowOption{
			RetryCount: job.Int64(3),
			RetryDelay: job.Time(50 * time.Millisecond),
		},
	))
	assert.Len(t, messages, 0)
	q.Start()
	// wait retry twice.
	<-keep
	q.Release()
	assert.Len(t, messages, 1)
}

func TestRetryCountWithNewTask(t *testing.T) {
	messages := make(chan string, 10)
	count := 1

	w := NewRing()

	q, err := NewQueue(
		WithLogger(NewLogger()),
		WithWorker(w),
		WithWorkerCount(1),
	)
	assert.NoError(t, err)

	keep := make(chan struct{})

	assert.NoError(t, q.QueueTask(
		func(ctx context.Context) error {
			if count%3 != 0 {
				count++
				return errors.New("count not correct")
			}
			close(keep)
			messages <- "foobar"
			return nil
		},
		job.AllowOption{
			RetryCount: job.Int64(3),
		},
	))
	assert.Len(t, messages, 0)
	q.Start()
	// wait retry twice.
	<-keep
	q.Release()
	assert.Len(t, messages, 1)
}

func TestCancelRetryCountWithNewTask(t *testing.T) {
	messages := make(chan string, 10)
	attempted := make(chan struct{}, 1)
	count := 1

	w := NewRing()

	q, err := NewQueue(
		WithLogger(NewLogger()),
		WithWorker(w),
		WithWorkerCount(1),
	)
	assert.NoError(t, err)

	assert.NoError(t, q.QueueTask(
		func(ctx context.Context) error {
			if count%3 != 0 {
				count++
				attempted <- struct{}{}
				q.logger.Info("add count")
				return errors.New("count not correct")
			}
			messages <- "foobar"
			return nil
		},
		job.AllowOption{
			RetryCount: job.Int64(3),
			RetryDelay: job.Time(100 * time.Millisecond),
		},
	))
	assert.Len(t, messages, 0)
	q.Start()
	waitForRingSignal(t, attempted)
	q.Release()
	assert.Len(t, messages, 0)
	assert.Equal(t, 2, count)
}

func TestCancelRetryCountWithNewMessage(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	m := mocks.NewMockQueuedMessage(controller)
	m.EXPECT().Bytes().Return([]byte("test")).AnyTimes()

	messages := make(chan string, 10)
	attempted := make(chan struct{}, 1)
	count := 1

	w := NewRing(
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			if count%3 != 0 {
				count++
				attempted <- struct{}{}
				return errors.New("count not correct")
			}
			messages <- string(m.Payload())
			return nil
		}),
	)

	q, err := NewQueue(
		WithLogger(NewLogger()),
		WithWorker(w),
		WithWorkerCount(1),
	)
	assert.NoError(t, err)

	assert.NoError(t, q.Queue(
		m,
		job.AllowOption{
			RetryCount: job.Int64(3),
			RetryDelay: job.Time(100 * time.Millisecond),
		},
	))
	assert.Len(t, messages, 0)
	q.Start()
	waitForRingSignal(t, attempted)
	q.Release()
	assert.Len(t, messages, 0)
	assert.Equal(t, 2, count)
}

func TestErrNoTaskInQueue(t *testing.T) {
	w := NewRing(
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			return nil
		}),
	)
	task, err := w.Request()
	assert.Nil(t, task)
	assert.Error(t, err)
	assert.Equal(t, ErrNoTaskInQueue, err)
}

func BenchmarkRingQueue(b *testing.B) {
	b.Run("queue and request operations", func(b *testing.B) {
		w := NewRing(WithQueueSize(1000))
		m := mockMessage{message: "test"}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = w.Queue(&m)
				_, _ = w.Request()
			}
		})
	})

	b.Run("concurrent queue operations", func(b *testing.B) {
		w := NewRing(WithQueueSize(1000))
		m := mockMessage{message: "test"}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = w.Queue(&m)
			}
		})
	})

	b.Run("resize operations", func(b *testing.B) {
		w := NewRing()
		m := mockMessage{message: "test"}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for j := 0; j < 100; j++ {
				_ = w.Queue(&m)
			}
			for j := 0; j < 100; j++ {
				_, _ = w.Request()
			}
		}
	})
}
