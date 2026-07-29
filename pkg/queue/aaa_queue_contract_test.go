package queue

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
)

func TestWithWorkerCountPreservesPositiveCount(t *testing.T) {
	requested := defaultWorkerCount + 1
	if actual := NewOptions(WithWorkerCount(requested)).workerCount; actual != requested {
		t.Fatalf("worker count = %d, want %d", actual, requested)
	}
}

func TestQueueShutdownClosesIdleRing(t *testing.T) {
	ring := NewRing()
	queue, err := NewQueue(WithWorker(ring))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	shutdown := make(chan struct{})
	go func() {
		queue.Shutdown()
		close(shutdown)
	}()
	select {
	case <-shutdown:
	case <-time.After(time.Second):
		ring.signalExit()
		<-shutdown
		t.Fatal("Queue.Shutdown() did not return for an empty Ring")
	}
	if stopped := atomic.LoadInt32(&ring.stopFlag); stopped != 1 {
		t.Fatalf("Ring stop flag = %d, want 1", stopped)
	}
}

func TestRingMaintainsFIFOAcrossGrowth(t *testing.T) {
	ring := NewRing()
	messages := make([]job.Message, 3)
	for i := range messages {
		messages[i] = job.NewTask(func(context.Context) error { return nil })
		if err := ring.Queue(&messages[i]); err != nil {
			t.Fatalf("Queue(%d) error = %v", i, err)
		}
	}
	for i := range messages {
		actual, err := ring.Request()
		if err != nil {
			t.Fatalf("Request(%d) error = %v", i, err)
		}
		if actual != &messages[i] {
			t.Fatalf("Request(%d) = %p, want %p", i, actual, &messages[i])
		}
	}
	if _, err := ring.Request(); !errors.Is(err, ErrNoTaskInQueue) {
		t.Fatalf("Request(empty) error = %v, want %v", err, ErrNoTaskInQueue)
	}
}

func TestRingShrinksAfterDequeue(t *testing.T) {
	ring := NewRing()
	messages := make([]job.Message, 3)
	for i := range messages {
		if err := ring.Queue(&messages[i]); err != nil {
			t.Fatalf("Queue(%d) error = %v", i, err)
		}
	}
	if _, err := ring.Request(); err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if size := len(ring.taskQueue); size != 2 {
		t.Fatalf("Ring buffer size = %d, want 2", size)
	}
}

func TestStackCaptureReturnsWithinBound(t *testing.T) {
	const helperEnvironment = "GOLIB_QUEUE_STACK_HELPER"
	if os.Getenv(helperEnvironment) == "1" {
		if len(stack(0)) == 0 {
			t.Fatal("stack() returned an empty trace")
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStackCaptureReturnsWithinBound$")
	command.Env = append(os.Environ(), helperEnvironment+"=1")
	if err := command.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatal("stack() did not return within five seconds")
		}
		t.Fatalf("stack helper error = %v", err)
	}
}

func TestRoutineGroupWaitsForFirstActiveRoutine(t *testing.T) {
	group := newRoutineGroup()
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	group.Run(func() {
		defer close(finished)
		close(started)
		<-release
	})
	<-started
	group.mu.Lock()
	active := group.active
	group.mu.Unlock()
	if active != 1 {
		close(release)
		<-finished
		t.Fatalf("active routines = %d, want 1", active)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	if err := group.WaitContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		cancel()
		close(release)
		<-finished
		t.Fatalf("WaitContext(active) error = %v, want %v", err, context.DeadlineExceeded)
	}
	cancel()
	close(release)
	<-finished
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := group.WaitContext(ctx); err != nil {
		t.Fatalf("WaitContext(idle) error = %v", err)
	}
}

func TestQueueReleaseDoesNotStartExternalWorker(t *testing.T) {
	worker := newStartContractWorker()
	queue, err := NewQueue(WithWorker(worker), WithWorkerCount(1))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}

	released := make(chan struct{})
	go func() {
		queue.Release()
		close(released)
	}()
	select {
	case <-released:
		if started := atomic.LoadInt32(&queue.started); started != 0 {
			t.Fatalf("Release() started external worker: flag = %d", started)
		}
	case <-time.After(time.Second):
		t.Fatal("Release() did not return for an idle external worker")
	}
	select {
	case <-worker.shutdown:
	default:
		t.Fatal("Release() did not shut down the external worker")
	}
}

func TestQueueReleaseContextAcceptsLiveContext(t *testing.T) {
	queue, err := NewQueue(WithWorker(newStartContractWorker()))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := queue.ReleaseContext(ctx); err != nil {
		t.Fatalf("ReleaseContext() error = %v", err)
	}
}

func TestQueueReleaseContextDrainsQueuedRingWork(t *testing.T) {
	var handled atomic.Int64
	queue, err := NewQueue(
		WithWorker(NewRing()),
		WithWorkerCount(1),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	if err := queue.QueueTask(func(context.Context) error {
		handled.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("QueueTask() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := queue.ReleaseContext(ctx); err != nil {
		t.Fatalf("ReleaseContext() error = %v", err)
	}
	if actual := handled.Load(); actual != 1 {
		t.Fatalf("handled tasks = %d, want 1", actual)
	}
}

func TestQueueRejectsInvalidMessageBeforeEnqueue(t *testing.T) {
	queue, err := NewQueue(WithWorker(newStartContractWorker()))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	message := job.Message{Timeout: -time.Second}
	if err := queue.queue(&message); !errors.Is(err, job.ErrInvalidMessage) {
		t.Fatalf("queue(invalid) error = %v, want %v", err, job.ErrInvalidMessage)
	}
}

func TestQueueAdmissionCounterClosesDrainWaiter(t *testing.T) {
	queue, err := NewQueue(WithWorker(newStartContractWorker()))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	queue.admissionsDone = make(chan struct{})
	if !queue.beginAdmission() {
		t.Fatal("beginAdmission() rejected an open queue")
	}
	if actual := queue.admissions; actual != 1 {
		t.Fatalf("active admissions after begin = %d, want 1", actual)
	}
	queue.endAdmission()
	if actual := queue.admissions; actual != 0 {
		t.Fatalf("active admissions after end = %d, want 0", actual)
	}
	select {
	case <-queue.admissionsDone:
	default:
		t.Fatal("endAdmission() did not release the drain waiter")
	}
}

func TestQueueInvokesConfiguredAfterCallback(t *testing.T) {
	after := make(chan struct{}, 1)
	queue, err := NewQueue(
		WithWorker(NewRing()),
		WithAfterFn(func() { after <- struct{}{} }),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	message := job.NewTask(func(context.Context) error { return nil })
	atomic.StoreInt64(&queue.activeWorkers, 1)
	queue.metric.IncBusyWorker()
	queue.work(&message)
	select {
	case <-after:
	default:
		t.Fatal("configured after callback was not invoked")
	}
}

func TestQueuePropagatesHandlerPanicWithinTaskTimeout(t *testing.T) {
	queue, err := NewQueue(WithWorker(NewRing()), WithLogger(NewEmptyLogger()))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	message := job.NewTask(
		func(context.Context) error { panic("handler panic") },
		job.AllowOption{Timeout: job.Time(100 * time.Millisecond)},
	)

	var panicValue any
	func() {
		defer func() { panicValue = recover() }()
		_ = queue.handle(&message)
	}()
	if panicValue == nil {
		t.Fatal("handler panic was not propagated")
	}
}

func TestQueueStopsRetryingWhenBudgetIsExhausted(t *testing.T) {
	handlerErr := errors.New("handler failed")
	var attempts atomic.Int64
	message := job.NewTask(
		func(context.Context) error {
			attempts.Add(1)
			return handlerErr
		},
		job.AllowOption{
			RetryCount: job.Int64(0),
			RetryDelay: job.Time(time.Nanosecond),
			Timeout:    job.Time(100 * time.Millisecond),
		},
	)
	queue, err := NewQueue(WithWorker(NewRing()), WithLogger(NewEmptyLogger()))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}

	if err := queue.handle(&message); !errors.Is(err, handlerErr) {
		t.Fatalf("handle() error = %v, want %v", err, handlerErr)
	}
	if actual := attempts.Load(); actual != 1 {
		t.Fatalf("handler attempts = %d, want 1", actual)
	}
}

func TestQueueStopsAfterSuccessfulAttempt(t *testing.T) {
	var attempts atomic.Int64
	message := job.NewTask(
		func(context.Context) error {
			attempts.Add(1)
			return nil
		},
		job.AllowOption{
			RetryCount: job.Int64(1),
			Timeout:    job.Time(100 * time.Millisecond),
		},
	)
	queue, err := NewQueue(WithWorker(NewRing()), WithLogger(NewEmptyLogger()))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}

	if err := queue.handle(&message); err != nil {
		t.Fatalf("handle() error = %v", err)
	}
	if actual := attempts.Load(); actual != 1 {
		t.Fatalf("handler attempts = %d, want 1", actual)
	}
}

func TestQueueDecrementsRetryBudget(t *testing.T) {
	handlerErr := errors.New("handler failed")
	var attempts atomic.Int64
	message := job.NewTask(
		func(context.Context) error {
			attempts.Add(1)
			return handlerErr
		},
		job.AllowOption{
			RetryCount: job.Int64(1),
			RetryDelay: job.Time(time.Nanosecond),
			Timeout:    job.Time(100 * time.Millisecond),
		},
	)
	queue, err := NewQueue(WithWorker(NewRing()), WithLogger(NewEmptyLogger()))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}

	if err := queue.handle(&message); !errors.Is(err, handlerErr) {
		t.Fatalf("handle() error = %v, want %v", err, handlerErr)
	}
	if actual := attempts.Load(); actual != 2 {
		t.Fatalf("handler attempts = %d, want 2", actual)
	}
}

func TestQueueDrainStopsSchedulerWithoutTask(t *testing.T) {
	worker := newEmptyContractWorker()
	queue, err := NewQueue(
		WithWorker(worker),
		WithWorkerCount(1),
		WithRetryInterval(time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	defer func() {
		queue.Shutdown()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := queue.WaitContext(ctx); err != nil {
			t.Errorf("WaitContext() error = %v", err)
		}
	}()

	queue.Start()
	select {
	case <-worker.requested:
	case <-time.After(time.Second):
		t.Fatal("Start() did not request an empty task")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := queue.ReleaseContext(ctx); err != nil {
		t.Fatalf("ReleaseContext() error = %v", err)
	}
}

func TestQueueDelaysTaskReturnedWithRequestErrorUntilNotification(t *testing.T) {
	executed := make(chan struct{}, 1)
	message := job.NewTask(func(context.Context) error {
		executed <- struct{}{}
		return nil
	})
	worker := newTaskErrorContractWorker(&message)
	queue, err := NewQueue(
		WithWorker(worker),
		WithWorkerCount(1),
		WithRetryInterval(time.Hour),
		WithLogger(NewEmptyLogger()),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	defer func() {
		queue.Shutdown()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := queue.WaitContext(ctx); err != nil {
			t.Errorf("WaitContext() error = %v", err)
		}
	}()

	queue.Start()
	select {
	case <-worker.returned:
	case <-time.After(time.Second):
		t.Fatal("worker request did not return its task")
	}
	select {
	case <-executed:
		t.Fatal("task executed before retry notification")
	case <-time.After(100 * time.Millisecond):
	}
	queue.notify <- struct{}{}
	select {
	case <-executed:
	case <-time.After(time.Second):
		t.Fatal("task did not execute after retry notification")
	}
}

func TestQueueStartAdmitsConfiguredWorker(t *testing.T) {
	worker := newStartContractWorker()
	queue, err := NewQueue(WithWorker(worker), WithWorkerCount(1))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	defer func() {
		queue.Shutdown()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := queue.WaitContext(ctx); err != nil {
			t.Errorf("WaitContext() error = %v", err)
		}
	}()

	queue.Start()
	select {
	case <-worker.requested:
	case <-time.After(time.Second):
		t.Fatal("Start() did not admit the configured worker")
	}
}

func TestQueueManagementLifecycleAppliesAcknowledgedResultsAndConfiguredState(t *testing.T) {
	worker := newManagedQueueWorker()
	queue, err := NewQueue(
		WithWorker(worker), WithWorkerLifecycle(managedQueueLifecycle(t)),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	result, err := queue.Execute(
		context.Background(),
		managedQueueCommand(
			"terminate-fast",
			management.CommandTerminate,
			management.TargetWorker,
			"worker-1",
		),
	)
	if err != nil || result.Status != management.CommandAcknowledged {
		t.Fatalf("Execute(terminate) = (%+v, %v)", result, err)
	}
	if worker.shutdownCalls.Load() != 1 || atomic.LoadInt32(&queue.stopFlag) != 1 {
		t.Fatalf(
			"acknowledged termination = shutdown calls %d, stop flag %d",
			worker.shutdownCalls.Load(),
			queue.stopFlag,
		)
	}

	configured, err := NewQueue(
		WithWorker(newManagedQueueWorker()),
		WithWorkerLifecycle(managedQueueLifecycle(t)),
	)
	if err != nil {
		t.Fatalf("NewQueue(configured) error = %v", err)
	}
	err = configured.ApplyDesiredState(context.Background(), management.DesiredRecord{
		Target: management.Target{Kind: management.TargetQueue, Name: "critical"},
		State:  management.DesiredPaused, Revision: 1,
		ChangedAt: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
		CommandID: "pause-fast",
	})
	if err != nil {
		t.Fatalf("ApplyDesiredState(configured) error = %v", err)
	}
	status, err := configured.ObserveWorker(context.Background())
	if err != nil || status.State != management.WorkerPaused {
		t.Fatalf("ObserveWorker(configured) = (%+v, %v)", status, err)
	}
	configured.Shutdown()
}

type startContractWorker struct {
	requested    chan struct{}
	shutdown     chan struct{}
	shutdownOnce sync.Once
}

type emptyContractWorker struct {
	requested chan struct{}
}

type taskErrorContractWorker struct {
	task         core.TaskMessage
	returned     chan struct{}
	shutdown     chan struct{}
	shutdownOnce sync.Once
	delivered    atomic.Int32
}

func newTaskErrorContractWorker(task core.TaskMessage) *taskErrorContractWorker {
	return &taskErrorContractWorker{
		task: task, returned: make(chan struct{}), shutdown: make(chan struct{}),
	}
}

func (*taskErrorContractWorker) Run(context.Context, core.TaskMessage) error { return nil }
func (*taskErrorContractWorker) Queue(core.TaskMessage) error                { return nil }
func (worker *taskErrorContractWorker) Shutdown() error {
	worker.shutdownOnce.Do(func() { close(worker.shutdown) })
	return nil
}
func (worker *taskErrorContractWorker) Request() (core.TaskMessage, error) {
	if worker.delivered.CompareAndSwap(0, 1) {
		close(worker.returned)
		return worker.task, errors.New("request failed after delivery")
	}
	<-worker.shutdown
	return nil, ErrQueueShutdown
}

func newEmptyContractWorker() *emptyContractWorker {
	return &emptyContractWorker{requested: make(chan struct{}, 1)}
}

func (*emptyContractWorker) Run(context.Context, core.TaskMessage) error { return nil }
func (*emptyContractWorker) Shutdown() error                             { return nil }
func (*emptyContractWorker) Queue(core.TaskMessage) error                { return nil }
func (worker *emptyContractWorker) Request() (core.TaskMessage, error) {
	select {
	case worker.requested <- struct{}{}:
	default:
	}
	return nil, ErrNoTaskInQueue
}

func newStartContractWorker() *startContractWorker {
	return &startContractWorker{
		requested: make(chan struct{}, 1),
		shutdown:  make(chan struct{}),
	}
}

func (*startContractWorker) Run(context.Context, core.TaskMessage) error { return nil }

func (worker *startContractWorker) Shutdown() error {
	worker.shutdownOnce.Do(func() { close(worker.shutdown) })

	return nil
}

func (*startContractWorker) Queue(core.TaskMessage) error { return nil }

func (worker *startContractWorker) Request() (core.TaskMessage, error) {
	select {
	case worker.requested <- struct{}{}:
	default:
	}
	<-worker.shutdown

	return nil, ErrQueueShutdown
}
