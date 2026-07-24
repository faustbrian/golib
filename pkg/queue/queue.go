package queue

// Package queue provides a high-performance, extensible message queue implementation
// supporting multiple workers, job retries, dynamic scaling, and graceful shutdown.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"

	"github.com/jpillora/backoff"
)

/*
ErrQueueShutdown is returned when an operation is attempted on a queue
that has already been closed and released.
*/
var ErrQueueShutdown = errors.New("queue has been closed and released")

type (
	// Queue represents a message queue with worker management, job scheduling,
	// retry logic, and graceful shutdown capabilities.
	Queue struct {
		sync.Mutex                  // Mutex to protect concurrent access to queue state
		metric        Metric        // Metrics collector for tracking queue and worker stats
		logger        Logger        // Logger for queue events and errors
		workerCount   int64         // Number of worker goroutines to process jobs
		routineGroup  *routineGroup // Group to manage and wait for goroutines
		quit          chan struct{} // Channel to signal shutdown to all goroutines
		ready         chan struct{} // Channel to signal worker readiness
		notify        chan struct{} // Channel to notify workers of new jobs
		worker        core.Worker   // The worker implementation that processes jobs
		startOnce     sync.Once     // Ensures the scheduler starts only once
		stopOnce      sync.Once     // Ensures shutdown is only performed once
		activeWorkers int64         // Authoritative internal worker count
		started       int32         // Atomic flag indicating the scheduler has started
		stopFlag      int32         // Atomic flag indicating if shutdown has started
		afterFn       func()        // Optional callback after each job execution
		observer      Observer      // Queue lifecycle observer
		retryInterval time.Duration // Interval for retrying job requests
		lifecycle     *management.WorkerLifecycle
	}
)

/*
ErrMissingWorker is returned when a queue is created without a worker implementation.
*/
var ErrMissingWorker = errors.New("missing worker module")

// NewQueue creates and returns a new Queue instance with the provided options.
// Returns an error if no worker is specified.
func NewQueue(opts ...Option) (*Queue, error) {
	o := NewOptions(opts...)
	q := &Queue{
		routineGroup:  newRoutineGroup(),      // Manages all goroutines spawned by the queue
		quit:          make(chan struct{}),    // Signals shutdown to all goroutines
		ready:         make(chan struct{}, 1), // Signals when a worker is ready to process a job
		notify:        make(chan struct{}, 1), // Notifies workers of new jobs
		workerCount:   o.workerCount,          // Number of worker goroutines
		logger:        o.logger,               // Logger for queue events
		worker:        o.worker,               // Worker implementation
		metric:        o.metric,               // Metrics collector
		afterFn:       o.afterFn,              // Optional post-job callback
		retryInterval: o.retryInterval,        // Interval for retrying job requests
		observer:      o.observer,             // Lifecycle event observer
		lifecycle:     o.lifecycle,
	}

	if q.worker == nil {
		return nil, ErrMissingWorker
	}
	if o.lifecycleConfigured && (q.lifecycle == nil || !workerProvidesStatus(q.worker)) {
		return nil, ErrInvalidManagementLifecycle
	}
	if o.retryInterval <= 0 {
		return nil, fmt.Errorf("%w: retry interval must be positive", ErrInvalidConfiguration)
	}
	if o.queueSize < 0 {
		return nil, fmt.Errorf("%w: queue size cannot be negative", ErrInvalidConfiguration)
	}

	return q, nil
}

// Start launches all worker goroutines and begins processing jobs.
// If workerCount is zero, Start is a no-op.
func (q *Queue) Start() {
	if atomic.LoadInt32(&q.stopFlag) == 1 {
		return
	}
	q.Lock()
	count := q.workerCount
	q.Unlock()
	if count == 0 {
		return
	}
	q.startOnce.Do(func() {
		atomic.StoreInt32(&q.started, 1)
		q.routineGroup.Run(func() {
			q.start()
		})
	})
}

// Shutdown initiates a graceful shutdown of the queue.
// It signals all goroutines to stop, shuts down the worker, and closes the quit channel.
// Shutdown is idempotent and safe to call multiple times.
func (q *Queue) Shutdown() {
	if !atomic.CompareAndSwapInt32(&q.stopFlag, 0, 1) {
		return
	}

	q.stopOnce.Do(func() {
		q.observe(Event{Kind: EventShutdownStarted})
		if busy := q.BusyWorkers(); busy > 0 {
			q.safeLogInfof("shutdown all tasks: %d workers", busy)
		}

		if err := q.worker.Shutdown(); err != nil {
			q.safeLogError(err)
		}
		close(q.quit)
		q.observe(Event{Kind: EventShutdownCompleted})
	})
}

// Release performs a graceful shutdown and waits for all goroutines to finish.
func (q *Queue) Release() {
	if _, isRing := q.worker.(*Ring); isRing && atomic.LoadInt32(&q.started) == 0 {
		q.Start()
	}
	q.Shutdown()
	q.Wait()
}

// BusyWorkers returns the number of workers currently processing jobs.
func (q *Queue) BusyWorkers() int64 {
	return atomic.LoadInt64(&q.activeWorkers)
}

// SuccessTasks returns the number of successfully completed tasks.
func (q *Queue) SuccessTasks() uint64 {
	return q.safeMetricValue("read successful task count", q.metric.SuccessTasks)
}

// FailureTasks returns the number of failed tasks.
func (q *Queue) FailureTasks() uint64 {
	return q.safeMetricValue("read failed task count", q.metric.FailureTasks)
}

// SubmittedTasks returns the number of tasks submitted to the queue.
func (q *Queue) SubmittedTasks() uint64 {
	return q.safeMetricValue("read submitted task count", q.metric.SubmittedTasks)
}

// CompletedTasks returns the total number of completed tasks (success + failure).
func (q *Queue) CompletedTasks() uint64 {
	return q.safeMetricValue("read completed task count", q.metric.CompletedTasks)
}

// Wait blocks until all goroutines in the routine group have finished.
func (q *Queue) Wait() {
	q.routineGroup.Wait()
}

// Queue enqueues a single job (core.QueuedMessage) into the queue.
// Accepts job options for customization.
func (q *Queue) Queue(message core.QueuedMessage, opts ...job.AllowOption) error {
	data := job.NewMessage(message, opts...)

	return q.queue(&data)
}

// QueueTask enqueues a single task function into the queue.
// Accepts job options for customization.
func (q *Queue) QueueTask(task job.TaskFunc, opts ...job.AllowOption) error {
	data := job.NewTask(task, opts...)
	return q.queue(&data)
}

// queue is an internal helper to enqueue a job.Message into the worker.
// It increments the submitted task metric and notifies workers if possible.
func (q *Queue) queue(m *job.Message) error {
	if atomic.LoadInt32(&q.stopFlag) == 1 {
		return ErrQueueShutdown
	}
	if err := m.Validate(); err != nil {
		return err
	}

	if err := q.worker.Queue(m); err != nil {
		return err
	}

	q.safeMetricUpdate("increment submitted task count", q.metric.IncSubmittedTask)
	q.observe(Event{Kind: EventEnqueued})
	// Notify a worker that a new job is available.
	// If the notify channel is full, the worker is busy and we avoid blocking.
	select {
	case q.notify <- struct{}{}:
	default:
	}

	return nil
}

// work executes a single task, handling panics and updating metrics accordingly.
// After execution, it schedules the next worker if needed.
func (q *Queue) work(task core.TaskMessage) {
	var err error
	startedAt := time.Now()
	q.observe(Event{Kind: EventHandlerStarted})
	// Defer block to handle panics, update metrics, and run afterFn callback.
	defer func() {
		atomic.AddInt64(&q.activeWorkers, -1)
		q.safeMetricUpdate("decrement busy worker count", q.metric.DecBusyWorker)
		e := recover()
		if e != nil {
			panicFailure := management.NewFailure(
				management.ClassificationPermanent,
				"handler_panic",
				ErrHandlerPanic,
			)
			err = q.settle(task, panicFailure)
			q.safeLogError(ErrHandlerPanic)
		}
		q.schedule()

		// Update success or failure metrics based on execution result.
		if err == nil && e == nil {
			q.safeMetricUpdate("increment successful task count", q.metric.IncSuccessTask)
			q.observe(Event{Kind: EventHandlerSucceeded, Duration: time.Since(startedAt)})
		} else {
			q.safeMetricUpdate("increment failed task count", q.metric.IncFailureTask)
			q.observe(failureEvent(Event{
				Kind: EventHandlerFailed, Duration: time.Since(startedAt), Err: err,
			}, err))
		}
		if q.afterFn != nil {
			if panicValue := invokeSafely(q.afterFn); panicValue != nil {
				q.safeLogErrorf("after callback panic: %v", panicValue)
			}
		}
	}()

	if err = q.run(task); err != nil {
		err = normalizeHandlerFailure(err)
		q.safeLogError(err)
	}
	err = q.settle(task, err)
}

func (q *Queue) workManaged(task core.TaskMessage, tracked bool) {
	if tracked {
		defer func() {
			_ = q.lifecycle.EndJob()
		}()
	}
	q.work(task)
}

func (q *Queue) settle(task core.TaskMessage, handlerErr error) error {
	delivery, ok := task.(core.Acknowledger)
	if !ok || !delivery.AcknowledgementRequired() {
		return handlerErr
	}
	if handlerErr == nil {
		if err := invokeSettlement("acknowledge delivery", delivery.Ack); err != nil {
			failure := management.NewFailure(
				management.ClassificationInfrastructure,
				"acknowledgement_failed",
				err,
			)
			q.observe(failureEvent(Event{Kind: EventAckFailed, Err: failure}, failure))
			return failure
		}
		q.observe(Event{Kind: EventAcknowledged})
		return nil
	}
	reject := delivery.Nack
	if failureDelivery, supported := task.(core.FailureAcknowledger); supported {
		reject = func() error { return failureDelivery.NackFailure(handlerErr) }
	}
	if err := invokeSettlement("reject delivery", reject); err != nil {
		failure := management.NewFailure(
			management.ClassificationInfrastructure,
			"failure_settlement_failed",
			err,
		)
		q.observe(failureEvent(Event{Kind: EventRejectFailed, Err: failure}, failure))
		return errors.Join(handlerErr, failure)
	}
	q.observe(failureEvent(Event{Kind: EventRejected, Err: handlerErr}, handlerErr))
	return handlerErr
}

func invokeSettlement(operation string, settle func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("%s: %w", operation, ErrSettlementPanic)
		}
	}()
	return settle()
}

// run dispatches the task to the appropriate handler based on its type.
// Returns an error if the task type is invalid.
func (q *Queue) run(task core.TaskMessage) error {
	switch t := task.(type) {
	case *job.Message:
		return q.handle(t)
	default:
		return errors.New("invalid task type")
	}
}

// handle executes a job.Message, supporting retries, timeouts, and panic recovery.
// Returns an error if the job fails or times out.
func (q *Queue) handle(m *job.Message) error {
	// done: receives the result of the job execution
	// panicChan: receives any panic that occurs in the job goroutine
	done := make(chan error, 1)
	panicChan := make(chan any, 1)
	startTime := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), m.Timeout)
	defer func() {
		cancel()
	}()

	// Run the job in a separate goroutine to support timeout and panic recovery.
	go func() {
		// Defer block to catch panics and send to panicChan
		defer func() {
			if p := recover(); p != nil {
				panicChan <- p
			}
		}()

		var err error

		// Set up backoff for retry logic
		b := &backoff.Backoff{
			Min:    m.RetryMin,
			Max:    m.RetryMax,
			Factor: m.RetryFactor,
			Jitter: m.Jitter,
		}
		delay := m.RetryDelay
	loop:
		for {
			// If a custom Task function is provided, use it; otherwise, use the worker's Run method.
			if m.Task != nil {
				err = m.Task(ctx)
			} else {
				err = q.worker.Run(ctx, m)
			}

			// If no error or no retries left, exit loop.
			if err == nil || m.RetryCount == 0 {
				break
			}
			m.RetryCount--

			// If no fixed retry delay, use backoff.
			if m.RetryDelay == 0 {
				delay = b.Duration()
			}
			retryFailure := normalizeHandlerFailure(err)
			q.observe(failureEvent(Event{
				Kind:           EventRetryScheduled,
				RetryRemaining: m.RetryCount,
				RetryDelay:     delay,
				Err:            retryFailure,
			}, retryFailure))

			select {
			case <-time.After(delay): // Wait before retrying
				q.safeLogInfof("retry remaining times: %d, delay time: %s", m.RetryCount, delay)
			case <-ctx.Done(): // Timeout reached
				err = ctx.Err()
				break loop
			}
		}

		done <- err
	}()

	select {
	case p := <-panicChan:
		panic(p)
	case <-ctx.Done(): // Timeout reached
		return ctx.Err()
	case <-q.quit: // Queue is shutting down
		// Cancel job and wait for remaining time or job completion
		cancel()
		leftTime := m.Timeout - time.Since(startTime)
		select {
		case <-time.After(leftTime):
			return context.DeadlineExceeded
		case err := <-done: // Job finished
			return err
		case p := <-panicChan:
			panic(p)
		}
	case err := <-done: // Job finished
		return err
	}
}

func normalizeHandlerFailure(err error) error {
	if err == nil {
		return nil
	}
	resolution := management.ResolveFailure(err)
	code := resolution.Code
	if code == "" {
		code = "handler_failed"
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		code = "handler_deadline_exceeded"
	case errors.Is(err, context.Canceled):
		code = "handler_canceled"
	}

	return management.NewFailure(resolution.Classification, code, err)
}

func failureEvent(event Event, err error) Event {
	resolution := management.ResolveFailure(err)
	event.Classification = resolution.Classification
	event.FailureCode = resolution.Code

	return event
}

func (q *Queue) observe(event Event) {
	panicValue := invokeSafely(func() {
		if metadata, ok := q.worker.(core.WorkerMetadata); ok {
			if event.Backend == "" {
				event.Backend = metadata.BackendName()
			}
			if event.Queue == "" {
				event.Queue = metadata.QueueName()
			}
		}
		if event.OccurredAt.IsZero() {
			event.OccurredAt = time.Now()
		}
		q.observer.Observe(event)
	})
	if panicValue != nil {
		q.safeLogErrorf("observer panic: %v", panicValue)
	}
}

func invokeSafely(fn func()) (panicValue any) {
	defer func() { panicValue = recover() }()
	fn()
	return nil
}

func valueSafely[T any](fn func() T) (value T, panicValue any) {
	defer func() { panicValue = recover() }()
	return fn(), nil
}

func (q *Queue) safeMetricUpdate(operation string, update func()) {
	if panicValue := invokeSafely(update); panicValue != nil {
		q.safeLogErrorf("metric panic during %s: %v", operation, panicValue)
	}
}

func (q *Queue) safeMetricValue(operation string, read func() uint64) uint64 {
	value, panicValue := valueSafely(read)
	if panicValue != nil {
		q.safeLogErrorf("metric panic during %s: %v", operation, panicValue)
	}
	return value
}

func (q *Queue) safeLogInfof(format string, args ...any) {
	_ = invokeSafely(func() { q.logger.Infof(format, args...) })
}

func (q *Queue) safeLogErrorf(format string, args ...any) {
	_ = invokeSafely(func() { q.logger.Errorf(format, args...) })
}

func (q *Queue) safeLogError(args ...any) {
	_ = invokeSafely(func() { q.logger.Error(args...) })
}

// UpdateWorkerCount dynamically updates the number of worker goroutines.
// Triggers scheduling to adjust to the new worker count.
func (q *Queue) UpdateWorkerCount(num int64) {
	q.Lock()
	q.workerCount = num
	q.Unlock()
	q.schedule()
}

// schedule checks if more workers can be started based on the current busy count.
// If so, it signals readiness to start a new worker.
func (q *Queue) schedule() {
	q.Lock()
	defer q.Unlock()
	if q.BusyWorkers() >= q.workerCount {
		return
	}
	reserved := false
	if q.lifecycle != nil {
		if !q.lifecycle.BeginAdmission() {
			return
		}
		reserved = true
	}

	select {
	case q.ready <- struct{}{}:
	default:
		if reserved {
			_ = q.lifecycle.EndAdmission()
		}
	}
}

func (q *Queue) hasWorkerCapacity() bool {
	q.Lock()
	defer q.Unlock()

	return q.BusyWorkers() < q.workerCount
}

/*
start launches the main worker loop, which manages job scheduling and execution.

- It uses a ticker to periodically retry job requests if the queue is empty.
- For each available worker slot, it requests a new task from the worker.
- If a task is available, it is sent to the tasks channel and processed by a new goroutine.
- The loop exits when the quit channel is closed.
*/
func (q *Queue) start() {
	type admittedTask struct {
		task    core.TaskMessage
		tracked bool
	}
	tasks := make(chan admittedTask, 1)
	ticker := time.NewTicker(q.retryInterval)
	defer ticker.Stop()

	for {
		// Ensure the number of busy workers does not exceed the configured worker count.
		q.schedule()

		select {
		case <-q.ready: // Wait for a worker slot to become available
			if !q.hasWorkerCapacity() {
				if q.lifecycle != nil {
					_ = q.lifecycle.EndAdmission()
				}
				continue
			}
		case <-q.quit: // Shutdown signal received
			return
		}

		// Request a task from the worker in a background goroutine.
		q.routineGroup.Run(func() {
			for {
				t, err := q.worker.Request()
				tracked := false
				if q.lifecycle != nil {
					if t == nil {
						_ = q.lifecycle.EndAdmission()
					} else {
						_ = q.lifecycle.PromoteAdmissionToJob()
						tracked = true
					}
				}
				if t == nil || err != nil {
					if err != nil {
						select {
						case <-q.quit:
						case <-ticker.C:
						case <-q.notify:
						}
					}
				}
				if t != nil {
					tasks <- admittedTask{task: t, tracked: tracked}
					return
				}
				if q.lifecycle != nil {
					if q.lifecycle.Snapshot().State != management.WorkerRunning ||
						!q.lifecycle.BeginAdmission() {
						tasks <- admittedTask{}
						return
					}
				}

				select {
				case <-q.quit:
					if !errors.Is(err, ErrNoTaskInQueue) {
						close(tasks)
						return
					}
				default:
				}
			}
		})

		admitted, ok := <-tasks
		if !ok {
			return
		}
		if admitted.task == nil {
			continue
		}

		// Start processing the new task in a separate goroutine.
		atomic.AddInt64(&q.activeWorkers, 1)
		q.safeMetricUpdate("increment busy worker count", q.metric.IncBusyWorker)
		q.routineGroup.Run(func() {
			q.workManaged(admitted.task, admitted.tracked)
		})
	}
}
