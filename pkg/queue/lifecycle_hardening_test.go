package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReleaseBeforeStartDrainsRing(t *testing.T) {
	var handled atomic.Int64
	q, err := NewQueue(
		WithWorker(NewRing(WithFn(func(context.Context, core.TaskMessage) error {
			handled.Add(1)
			return nil
		}))),
		WithWorkerCount(1),
		WithRetryInterval(time.Millisecond),
	)
	require.NoError(t, err)
	require.NoError(t, q.Queue(mockMessage{message: "queued-before-start"}))
	released := make(chan struct{})
	go func() {
		q.Release()
		close(released)
	}()

	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("Release blocked with queued Ring work before Start")
	}
	assert.Equal(t, int64(1), handled.Load())
}

type blockingRequestWorker struct {
	requests atomic.Int64
	entered  chan struct{}
	release  chan struct{}
	stopOnce sync.Once
}

func newBlockingRequestWorker() *blockingRequestWorker {
	return &blockingRequestWorker{
		entered: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
}

func (*blockingRequestWorker) Run(context.Context, core.TaskMessage) error { return nil }
func (*blockingRequestWorker) Queue(core.TaskMessage) error                { return nil }

func (w *blockingRequestWorker) Request() (core.TaskMessage, error) {
	w.requests.Add(1)
	w.entered <- struct{}{}
	<-w.release
	return nil, ErrQueueHasBeenClosed
}

func (w *blockingRequestWorker) Shutdown() error {
	w.stopOnce.Do(func() { close(w.release) })
	return nil
}

func TestStartIsIdempotent(t *testing.T) {
	worker := newBlockingRequestWorker()
	q, err := NewQueue(
		WithWorker(worker),
		WithWorkerCount(1),
	)
	require.NoError(t, err)

	q.Start()
	q.Start()
	select {
	case <-worker.entered:
	case <-time.After(time.Second):
		t.Fatal("worker request did not start")
	}
	select {
	case <-worker.entered:
		t.Fatal("repeated Start launched a second scheduler")
	case <-time.After(20 * time.Millisecond):
	}

	q.Release()
	q.Start()
	assert.Equal(t, int64(1), worker.requests.Load())
}

type panickingLogger struct{}

func (panickingLogger) Infof(string, ...any)  { panic("logger") }
func (panickingLogger) Errorf(string, ...any) { panic("logger") }
func (panickingLogger) Fatalf(string, ...any) { panic("logger") }
func (panickingLogger) Info(...any)           { panic("logger") }
func (panickingLogger) Error(...any)          { panic("logger") }
func (panickingLogger) Fatal(...any)          { panic("logger") }

type panickingMetric struct{}

func (panickingMetric) IncBusyWorker()     { panic("metric") }
func (panickingMetric) DecBusyWorker()     { panic("metric") }
func (panickingMetric) BusyWorkers() int64 { panic("metric") }
func (panickingMetric) IncSuccessTask()    { panic("metric") }
func (panickingMetric) IncFailureTask()    { panic("metric") }
func (panickingMetric) IncSubmittedTask()  { panic("metric") }
func (panickingMetric) SuccessTasks() uint64 {
	panic("metric")
}
func (panickingMetric) FailureTasks() uint64 {
	panic("metric")
}
func (panickingMetric) SubmittedTasks() uint64 {
	panic("metric")
}
func (panickingMetric) CompletedTasks() uint64 {
	panic("metric")
}

func TestObserverPanicDoesNotCorruptWorkerAccounting(t *testing.T) {
	q, err := NewQueue(
		WithWorker(NewRing()),
		WithObserver(ObserverFunc(func(Event) { panic("observer") })),
		WithLogger(NewEmptyLogger()),
	)
	require.NoError(t, err)
	atomic.StoreInt64(&q.activeWorkers, 1)
	q.metric.IncBusyWorker()
	message := job.NewTask(func(context.Context) error { return nil })

	assert.NotPanics(t, func() { q.work(&message) })
	assert.Equal(t, int64(0), q.BusyWorkers())
	assert.Equal(t, uint64(1), q.SuccessTasks())
	q.Release()
}

func TestLoggerAndAfterCallbackPanicsDoNotEscape(t *testing.T) {
	q, err := NewQueue(
		WithWorker(NewRing()),
		WithLogger(panickingLogger{}),
		WithAfterFn(func() { panic("after") }),
	)
	require.NoError(t, err)
	atomic.StoreInt64(&q.activeWorkers, 1)
	q.metric.IncBusyWorker()
	message := job.NewTask(func(context.Context) error { return errors.New("handler") })

	assert.NotPanics(t, func() { q.work(&message) })
	assert.Equal(t, int64(0), q.BusyWorkers())
	assert.Equal(t, uint64(1), q.FailureTasks())
}

func TestMetricPanicsDoNotEscapeQueueLifecycle(t *testing.T) {
	q, err := NewQueue(
		WithWorker(&controlledWorker{}),
		WithMetric(panickingMetric{}),
		WithLogger(NewEmptyLogger()),
	)
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		require.NoError(t, q.Queue(mockMessage{message: "payload"}))
	})
	assert.NotPanics(t, func() {
		assert.Zero(t, q.BusyWorkers())
		assert.Zero(t, q.SuccessTasks())
		assert.Zero(t, q.FailureTasks())
		assert.Zero(t, q.SubmittedTasks())
		assert.Zero(t, q.CompletedTasks())
	})
	assert.NotPanics(t, q.Shutdown)
}
