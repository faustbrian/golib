package queue_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
)

type releaseMessage string

func (message releaseMessage) Bytes() []byte { return []byte(message) }

type cancelOnSecondDoneContext struct {
	context.Context
	calls atomic.Int32
	done  chan struct{}
	once  sync.Once
}

func (ctx *cancelOnSecondDoneContext) Done() <-chan struct{} {
	if ctx.calls.Add(1) == 2 {
		go func() {
			runtime.Gosched()
			ctx.once.Do(func() { close(ctx.done) })
		}()
	}

	return ctx.done
}

func (ctx *cancelOnSecondDoneContext) Err() error {
	select {
	case <-ctx.done:
		return context.Canceled
	default:
		return nil
	}
}

type releaseWorker struct {
	mu       sync.Mutex
	tasks    []core.TaskMessage
	handler  func(context.Context, core.TaskMessage) error
	shutdown chan struct{}
	stopErr  error
}

func (worker *releaseWorker) Run(ctx context.Context, task core.TaskMessage) error {
	return worker.handler(ctx, task)
}

func (worker *releaseWorker) Shutdown() error {
	close(worker.shutdown)

	return worker.stopErr
}

func (worker *releaseWorker) Queue(task core.TaskMessage) error {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	worker.tasks = append(worker.tasks, task)

	return nil
}

func (worker *releaseWorker) Request() (core.TaskMessage, error) {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if len(worker.tasks) == 0 {
		return nil, queue.ErrNoTaskInQueue
	}
	task := worker.tasks[0]
	worker.tasks = worker.tasks[1:]

	return task, nil
}

func TestReleaseContextStopsAdmissionAndDrainsHandlersBeforeShutdown(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	worker := &releaseWorker{
		shutdown: make(chan struct{}),
		handler: func(context.Context, core.TaskMessage) error {
			close(entered)
			<-release

			return nil
		},
	}
	coordinator, err := queue.NewQueue(
		queue.WithWorker(worker),
		queue.WithWorkerCount(1),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	if err := coordinator.Queue(releaseMessage("work")); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	coordinator.Start()
	<-entered

	stopped := make(chan error, 1)
	go func() { stopped <- coordinator.ReleaseContext(context.Background()) }()
	for {
		err = coordinator.Queue(releaseMessage("late"))
		if errors.Is(err, queue.ErrQueueShutdown) {
			break
		}
		if err != nil {
			t.Fatalf("Queue() during drain error = %v", err)
		}
	}
	select {
	case <-worker.shutdown:
		t.Fatal("worker shut down before the active handler completed")
	default:
	}

	close(release)
	if err = <-stopped; err != nil {
		t.Fatalf("ReleaseContext() error = %v", err)
	}
	select {
	case <-worker.shutdown:
	default:
		t.Fatal("worker was not shut down after the active handler completed")
	}
}

func TestReleaseContextCanResumeAfterCallerCancellation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	worker := &releaseWorker{
		shutdown: make(chan struct{}),
		handler: func(context.Context, core.TaskMessage) error {
			close(entered)
			<-release

			return nil
		},
	}
	coordinator, err := queue.NewQueue(
		queue.WithWorker(worker),
		queue.WithWorkerCount(1),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	if err = coordinator.Queue(releaseMessage("work")); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	coordinator.Start()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = coordinator.ReleaseContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReleaseContext() error = %v, want context cancellation", err)
	}
	select {
	case <-worker.shutdown:
		t.Fatal("worker shut down before the canceled drain completed")
	default:
	}

	close(release)
	if err = coordinator.ReleaseContext(context.Background()); err != nil {
		t.Fatalf("resumed ReleaseContext() error = %v", err)
	}
}

func TestReleaseContextDrainsPendingInMemoryWork(t *testing.T) {
	var (
		mu      sync.Mutex
		handled []string
	)
	coordinator := queue.NewPool(1, queue.WithFn(func(
		_ context.Context,
		task core.TaskMessage,
	) error {
		mu.Lock()
		handled = append(handled, string(task.Payload()))
		mu.Unlock()

		return nil
	}))
	if err := coordinator.Queue(releaseMessage("first")); err != nil {
		t.Fatalf("Queue(first) error = %v", err)
	}
	if err := coordinator.Queue(releaseMessage("second")); err != nil {
		t.Fatalf("Queue(second) error = %v", err)
	}

	if err := coordinator.ReleaseContext(context.Background()); err != nil {
		t.Fatalf("ReleaseContext() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(handled) != 2 || handled[0] != "first" || handled[1] != "second" {
		t.Fatalf("handled = %v, want [first second]", handled)
	}
}

func TestReleaseContextWaitsForAnAcceptedPublishBeforeShutdown(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	publishDone := make(chan struct{})
	earlyShutdown := errors.New("shutdown before publish completed")
	worker := &releaseWorker{
		shutdown: make(chan struct{}),
		handler:  func(context.Context, core.TaskMessage) error { return nil },
	}
	coordinator, err := queue.NewQueue(
		queue.WithWorker(&blockingQueueWorker{
			releaseWorker: worker,
			entered:       entered,
			release:       release,
			publishDone:   publishDone,
			earlyShutdown: earlyShutdown,
		}),
		queue.WithWorkerCount(0),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}

	published := make(chan error, 1)
	go func() { published <- coordinator.Queue(releaseMessage("work")) }()
	<-entered
	stopped := make(chan error, 1)
	go func() { stopped <- coordinator.ReleaseContext(context.Background()) }()
	for {
		err = coordinator.Queue(releaseMessage("late"))
		if errors.Is(err, queue.ErrQueueShutdown) {
			break
		}
		if err != nil {
			t.Fatalf("Queue() during drain error = %v", err)
		}
	}
	select {
	case <-worker.shutdown:
		t.Fatal("worker shut down while an accepted publish was active")
	default:
	}

	close(release)
	if err = <-published; err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	if err = <-stopped; err != nil {
		t.Fatalf("ReleaseContext() error = %v", err)
	}
}

type blockingQueueWorker struct {
	*releaseWorker
	entered       chan struct{}
	release       chan struct{}
	publishDone   chan struct{}
	earlyShutdown error
}

func (worker *blockingQueueWorker) Queue(task core.TaskMessage) error {
	if string(task.Payload()) == "work" {
		close(worker.entered)
		<-worker.release
		close(worker.publishDone)
	}

	return worker.releaseWorker.Queue(task)
}

func (worker *blockingQueueWorker) Shutdown() error {
	select {
	case <-worker.publishDone:
		return worker.releaseWorker.Shutdown()
	default:
		close(worker.shutdown)

		return worker.earlyShutdown
	}
}

type taskWithRequestErrorWorker struct {
	*releaseWorker
	requestEntered chan struct{}
	releaseRequest chan struct{}
	requestErr     error
}

func (worker *taskWithRequestErrorWorker) Request() (core.TaskMessage, error) {
	close(worker.requestEntered)
	<-worker.releaseRequest
	task, _ := worker.releaseWorker.Request()

	return task, worker.requestErr
}

func TestReleaseContextFinishesATaskReturnedWithARequestError(t *testing.T) {
	handled := make(chan struct{})
	worker := &taskWithRequestErrorWorker{
		releaseWorker: &releaseWorker{
			shutdown: make(chan struct{}),
			handler: func(context.Context, core.TaskMessage) error {
				close(handled)

				return nil
			},
		},
		requestEntered: make(chan struct{}),
		releaseRequest: make(chan struct{}),
		requestErr:     errors.New("request completed with a warning"),
	}
	coordinator, err := queue.NewQueue(
		queue.WithWorker(worker),
		queue.WithWorkerCount(1),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	if err = coordinator.Queue(releaseMessage("work")); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	coordinator.Start()
	<-worker.requestEntered

	stopped := make(chan error, 1)
	go func() { stopped <- coordinator.ReleaseContext(context.Background()) }()
	for {
		err = coordinator.Queue(releaseMessage("late"))
		if errors.Is(err, queue.ErrQueueShutdown) {
			break
		}
		if err != nil {
			t.Fatalf("Queue() during drain error = %v", err)
		}
	}
	close(worker.releaseRequest)
	if err = <-stopped; err != nil {
		t.Fatalf("ReleaseContext() error = %v", err)
	}
	select {
	case <-handled:
	default:
		t.Fatal("task returned with a request error was not handled")
	}
}

func TestReleaseContextReturnsWorkerShutdownFailure(t *testing.T) {
	shutdownErr := errors.New("shutdown failed")
	worker := &releaseWorker{
		shutdown: make(chan struct{}),
		stopErr:  shutdownErr,
		handler:  func(context.Context, core.TaskMessage) error { return nil },
	}
	coordinator, err := queue.NewQueue(
		queue.WithWorker(worker),
		queue.WithWorkerCount(0),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}

	if err = coordinator.ReleaseContext(context.Background()); !errors.Is(err, shutdownErr) {
		t.Fatalf("ReleaseContext() error = %v, want shutdown failure", err)
	}
}

func TestReleaseContextHonorsCancellationWhileAnAdmissionIsActive(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	publishDone := make(chan struct{})
	worker := &blockingQueueWorker{
		releaseWorker: &releaseWorker{
			shutdown: make(chan struct{}),
			handler:  func(context.Context, core.TaskMessage) error { return nil },
		},
		entered:       entered,
		release:       release,
		publishDone:   publishDone,
		earlyShutdown: errors.New("shutdown before publish completed"),
	}
	coordinator, err := queue.NewQueue(
		queue.WithWorker(worker),
		queue.WithWorkerCount(0),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	published := make(chan error, 1)
	go func() { published <- coordinator.Queue(releaseMessage("work")) }()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = coordinator.ReleaseContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReleaseContext() error = %v, want context cancellation", err)
	}
	var nilContext context.Context
	if err = coordinator.WaitContext(nilContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitContext(nil) error = %v, want context cancellation", err)
	}
	close(release)
	if err = <-published; err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	if err = coordinator.ReleaseContext(context.Background()); err != nil {
		t.Fatalf("resumed ReleaseContext() error = %v", err)
	}
}

func TestReleaseContextRejectsNilContext(t *testing.T) {
	worker := &releaseWorker{
		shutdown: make(chan struct{}),
		handler:  func(context.Context, core.TaskMessage) error { return nil },
	}
	coordinator, err := queue.NewQueue(queue.WithWorker(worker))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	var nilContext context.Context
	if err = coordinator.ReleaseContext(nilContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReleaseContext(nil) error = %v, want context cancellation", err)
	}
}

func TestReleaseContextStartsAnUnstartedInMemoryQueue(t *testing.T) {
	handled := 0
	ring := queue.NewRing(queue.WithFn(func(
		context.Context,
		core.TaskMessage,
	) error {
		handled++

		return nil
	}))
	coordinator, err := queue.NewQueue(
		queue.WithWorker(ring),
		queue.WithWorkerCount(1),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	if err = coordinator.Queue(releaseMessage("work")); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	if err = coordinator.ReleaseContext(context.Background()); err != nil {
		t.Fatalf("ReleaseContext() error = %v", err)
	}
	if handled != 1 {
		t.Fatalf("handled = %d, want 1", handled)
	}
}

func TestReleaseContextBoundsAZeroWorkerInMemoryDrainAndCanResume(t *testing.T) {
	previousMaxProcs := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousMaxProcs) })
	handled := 0
	ring := queue.NewRing(queue.WithFn(func(
		context.Context,
		core.TaskMessage,
	) error {
		handled++

		return nil
	}))
	coordinator, err := queue.NewQueue(queue.WithWorker(ring))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	coordinator.UpdateWorkerCount(0)
	if err = coordinator.Queue(releaseMessage("work")); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	ctx := &cancelOnSecondDoneContext{
		Context: context.Background(),
		done:    make(chan struct{}),
	}
	if err = coordinator.ReleaseContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReleaseContext() error = %v, want context cancellation", err)
	}
	if handled != 0 {
		t.Fatalf("handled tasks after canceled zero-worker drain = %d, want 0", handled)
	}

	coordinator.UpdateWorkerCount(1)
	coordinator.Start()
	if err := coordinator.ReleaseContext(context.Background()); err != nil {
		t.Fatalf("resumed ReleaseContext() error = %v", err)
	}
	if handled != 1 {
		t.Fatalf("handled tasks after resumed drain = %d, want 1", handled)
	}
}
