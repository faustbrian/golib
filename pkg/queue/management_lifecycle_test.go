package queue

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
)

func TestQueueManagementLifecyclePausesAndResumesAdmission(t *testing.T) {
	t.Parallel()

	worker := newManagedQueueWorker()
	lifecycle := managedQueueLifecycle(t)
	after := make(chan struct{}, 1)
	queue, err := NewQueue(
		WithWorker(worker), WithWorkerCount(1), WithRetryInterval(time.Millisecond),
		WithWorkerLifecycle(lifecycle), WithAfterFn(func() { after <- struct{}{} }),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	queue.Start()
	if call := <-worker.requested; call != 1 {
		t.Fatalf("first request call = %d", call)
	}
	pauseDone := make(chan management.CommandResult, 1)
	go func() {
		result, _ := queue.Execute(
			context.Background(), managedQueueCommand("pause-1", management.CommandPause, management.TargetQueue, "critical"),
		)
		pauseDone <- result
	}()
	worker.tasks <- managedJob("job-1")
	if result := <-pauseDone; result.Status != management.CommandAcknowledged {
		t.Fatalf("pause result = %+v", result)
	}
	<-worker.runStarted
	close(worker.finishRun)
	<-after
	if calls := worker.requestCalls.Load(); calls != 1 {
		t.Fatalf("request calls while paused = %d, want 1", calls)
	}
	status, err := queue.ObserveWorker(context.Background())
	if err != nil || status.State != management.WorkerPaused || status.CurrentJobs != 0 {
		t.Fatalf("ObserveWorker(paused) = (%+v, %v)", status, err)
	}
	result, err := queue.Execute(
		context.Background(), managedQueueCommand("resume-1", management.CommandResume, management.TargetQueue, "critical"),
	)
	if err != nil || result.Status != management.CommandAcknowledged {
		t.Fatalf("Execute(resume) = (%+v, %v)", result, err)
	}
	if call := <-worker.requested; call != 2 {
		t.Fatalf("second request call = %d", call)
	}
	queue.Shutdown()
}

func TestQueueManagementLifecyclePausesAnEmptyAdmission(t *testing.T) {
	t.Parallel()

	worker := newManagedQueueWorker()
	lifecycle := managedQueueLifecycle(t)
	queue, err := NewQueue(
		WithWorker(worker), WithWorkerCount(1),
		WithRetryInterval(time.Millisecond), WithWorkerLifecycle(lifecycle),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	queue.Start()
	<-worker.requested
	pauseDone := make(chan error, 1)
	go func() {
		pauseDone <- queue.ApplyDesiredState(context.Background(), management.DesiredRecord{
			Target: management.Target{Kind: management.TargetQueue, Name: "critical"},
			State:  management.DesiredPaused, Revision: 1,
			ChangedAt: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
			CommandID: "pause-empty",
		})
	}()
	waitForManagedQueueState(t, queue, management.WorkerPaused)
	worker.tasks <- nil
	if err := <-pauseDone; err != nil {
		t.Fatalf("ApplyDesiredState(pause) error = %v", err)
	}
	if calls := worker.requestCalls.Load(); calls != 1 {
		t.Fatalf("request calls while paused = %d, want 1", calls)
	}
	queue.Shutdown()
}

func TestQueueManagementLifecycleReleasesStaleReadinessAdmission(t *testing.T) {
	t.Parallel()

	worker := newManagedQueueWorker()
	lifecycle := managedQueueLifecycle(t)
	queue, err := NewQueue(
		WithWorker(worker), WithWorkerCount(1),
		WithRetryInterval(time.Millisecond), WithWorkerLifecycle(lifecycle),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	t.Cleanup(func() {
		atomic.StoreInt64(&queue.activeWorkers, 0)
		queue.Shutdown()
		queue.Wait()
	})
	if !lifecycle.BeginAdmission() {
		t.Fatal("initial lifecycle admission rejected")
	}
	queue.ready <- struct{}{}
	atomic.StoreInt64(&queue.activeWorkers, 1)
	queue.Start()

	pauseDone := make(chan error, 1)
	go func() {
		pauseDone <- queue.ApplyDesiredState(context.Background(), management.DesiredRecord{
			Target: management.Target{Kind: management.TargetQueue, Name: "critical"},
			State:  management.DesiredPaused, Revision: 1,
			ChangedAt: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC),
			CommandID: "pause-stale-readiness",
		})
	}()
	select {
	case err := <-pauseDone:
		if err != nil {
			t.Fatalf("ApplyDesiredState(pause) error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stale readiness admission was not released")
	}

	atomic.StoreInt64(&queue.activeWorkers, 0)
	queue.Shutdown()
}

func TestQueueManagementLifecycleDrainsAndTerminatesAfterSettlement(t *testing.T) {
	t.Parallel()

	worker := newManagedQueueWorker()
	lifecycle := managedQueueLifecycle(t)
	queue, err := NewQueue(
		WithWorker(worker), WithWorkerCount(1), WithRetryInterval(time.Millisecond),
		WithWorkerLifecycle(lifecycle),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	queue.Start()
	<-worker.requested
	worker.tasks <- managedJob("job-1")
	<-worker.runStarted
	drainDone := make(chan management.CommandResult, 1)
	go func() {
		result, _ := queue.Execute(
			context.Background(), managedQueueCommand("drain-1", management.CommandDrain, management.TargetWorker, "worker-1"),
		)
		drainDone <- result
	}()
	waitForManagedQueueState(t, queue, management.WorkerDraining)
	select {
	case result := <-drainDone:
		t.Fatalf("drain completed before settlement: %+v", result)
	default:
	}
	close(worker.finishRun)
	if result := <-drainDone; result.Status != management.CommandAcknowledged {
		t.Fatalf("drain result = %+v", result)
	}
	status, err := queue.ObserveWorker(context.Background())
	if err != nil || status.DrainStatus != management.DrainCompleted || status.CurrentJobs != 0 {
		t.Fatalf("ObserveWorker(drained) = (%+v, %v)", status, err)
	}
	if calls := worker.requestCalls.Load(); calls != 1 {
		t.Fatalf("request calls while draining = %d, want 1", calls)
	}

	result, err := queue.Execute(
		context.Background(), managedQueueCommand("terminate-1", management.CommandTerminate, management.TargetWorker, "worker-1"),
	)
	if err != nil || result.Status != management.CommandAcknowledged {
		t.Fatalf("Execute(terminate) = (%+v, %v)", result, err)
	}
	if worker.shutdownCalls.Load() != 1 || atomic.LoadInt32(&queue.stopFlag) != 1 {
		t.Fatalf("termination = shutdown calls %d, stop flag %d", worker.shutdownCalls.Load(), queue.stopFlag)
	}
}

func TestQueueAppliesDesiredStateAndDelegatesNativeStatus(t *testing.T) {
	t.Parallel()

	worker := newManagedQueueWorker()
	lifecycle := managedQueueLifecycle(t)
	queue, err := NewQueue(WithWorker(worker), WithWorkerLifecycle(lifecycle))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	record := management.DesiredRecord{
		Target: management.Target{Kind: management.TargetQueue, Name: "critical"},
		State:  management.DesiredPaused, Revision: 1,
		ChangedAt: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
		CommandID: "pause-1",
	}
	if err := queue.ApplyDesiredState(context.Background(), record); err != nil {
		t.Fatalf("ApplyDesiredState() error = %v", err)
	}
	workerStatus, err := queue.ObserveWorker(context.Background())
	if err != nil || workerStatus.State != management.WorkerPaused ||
		!containsCapability(workerStatus.Capabilities, management.CapabilityPause) {
		t.Fatalf("ObserveWorker() = (%+v, %v)", workerStatus, err)
	}
	queueStatus, err := queue.ObserveQueue(context.Background())
	if err != nil || queueStatus.Queue != "critical" {
		t.Fatalf("ObserveQueue() = (%+v, %v)", queueStatus, err)
	}
	active := record
	active.State = management.DesiredActive
	active.Revision = 2
	active.CommandID = "resume-1"
	if err := queue.ApplyDesiredState(context.Background(), active); err != nil {
		t.Fatalf("ApplyDesiredState(active) error = %v", err)
	}
	queue.Start()
	if call := <-worker.requested; call != 1 {
		t.Fatalf("request call after active state = %d", call)
	}
	queue.Shutdown()
}

func TestQueueDesiredTerminationAndManagementErrorsFailClosed(t *testing.T) {
	t.Parallel()

	worker := newManagedQueueWorker()
	lifecycle := managedQueueLifecycle(t)
	queue, err := NewQueue(WithWorker(worker), WithWorkerLifecycle(lifecycle))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	wrongTarget := management.DesiredRecord{
		Target: management.Target{Kind: management.TargetQueue, Name: "other"},
		State:  management.DesiredPaused, Revision: 1,
		ChangedAt: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
		CommandID: "wrong-target",
	}
	if err := queue.ApplyDesiredState(context.Background(), wrongTarget); err == nil {
		t.Fatal("ApplyDesiredState(wrong target) error = nil")
	}
	invalid := managedQueueCommand(
		"invalid", management.CommandResume, management.TargetQueue, "other",
	)
	result, err := queue.Execute(context.Background(), invalid)
	if err != nil || result.Status != management.CommandRejected {
		t.Fatalf("Execute(invalid target) = (%+v, %v)", result, err)
	}
	if _, err := queue.Execute(context.Background(), management.Command{}); err == nil {
		t.Fatal("Execute(invalid command) error = nil")
	}
	worker.statusErr = errors.New("status unavailable")
	if _, err := queue.ObserveWorker(context.Background()); !errors.Is(err, worker.statusErr) {
		t.Fatalf("ObserveWorker() error = %v", err)
	}
	worker.statusErr = nil
	terminating := wrongTarget
	terminating.Target = management.Target{Kind: management.TargetWorker, Name: "worker-1"}
	terminating.State = management.DesiredTerminating
	terminating.CommandID = "terminate-1"
	if err := queue.ApplyDesiredState(context.Background(), terminating); err != nil {
		t.Fatalf("ApplyDesiredState(terminating) error = %v", err)
	}
	if worker.shutdownCalls.Load() != 1 || atomic.LoadInt32(&queue.stopFlag) != 1 {
		t.Fatalf("termination = shutdown calls %d, stop flag %d", worker.shutdownCalls.Load(), queue.stopFlag)
	}
}

func TestQueueDelegatesNativeMutationCommands(t *testing.T) {
	t.Parallel()

	worker := newManagedQueueWorker()
	lifecycle := managedQueueLifecycle(t)
	queue, err := NewQueue(WithWorker(worker), WithWorkerLifecycle(lifecycle))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	command := managedQueueCommand(
		"retry-1", management.CommandRetry, management.TargetFailure, "failure-1",
	)
	worker.controlResult = management.CommandResult{
		CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
		WorkerID: "worker-1", Protocol: command.Protocol,
		Status: management.CommandAcknowledged, CompletedAt: time.Now().UTC(),
	}
	result, err := queue.Execute(context.Background(), command)
	if err != nil || result != worker.controlResult || worker.controlCalls.Load() != 1 {
		t.Fatalf("Execute(native mutation) = (%+v, %v), calls=%d", result, err, worker.controlCalls.Load())
	}

	worker.controlErr = errors.New("native unavailable")
	command = managedQueueCommand(
		"delete-1", management.CommandDelete, management.TargetFailure, "failure-1",
	)
	if _, err = queue.Execute(context.Background(), command); !errors.Is(err, worker.controlErr) {
		t.Fatalf("Execute(native error) error = %v", err)
	}

	plain := &statusOnlyManagedWorker{managedQueueWorker: newManagedQueueWorker()}
	queue, err = NewQueue(WithWorker(plain), WithWorkerLifecycle(managedQueueLifecycle(t)))
	if err != nil {
		t.Fatalf("NewQueue(status-only) error = %v", err)
	}
	result, err = queue.Execute(context.Background(), command)
	if err != nil || result.Status != management.CommandUnsupported {
		t.Fatalf("Execute(unsupported mutation) = (%+v, %v)", result, err)
	}
}

func TestQueueRejectsIncompleteOrDisabledManagementLifecycle(t *testing.T) {
	t.Parallel()

	lifecycle := managedQueueLifecycle(t)
	queue, err := NewQueue(WithWorker(&plainManagedWorker{}), WithWorkerLifecycle(lifecycle))
	if queue != nil || !errors.Is(err, ErrInvalidManagementLifecycle) {
		t.Fatalf("NewQueue(non-status worker) = (%v, %v)", queue, err)
	}
	queue, err = NewQueue(WithWorker(newManagedQueueWorker()))
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	if _, err := queue.Execute(context.Background(), managedQueueCommand("pause-1", management.CommandPause, management.TargetQueue, "critical")); !errors.Is(err, ErrManagementLifecycleDisabled) {
		t.Fatalf("Execute(disabled) error = %v", err)
	}
	if err := queue.ApplyDesiredState(context.Background(), management.DesiredRecord{}); !errors.Is(err, ErrManagementLifecycleDisabled) {
		t.Fatalf("ApplyDesiredState(disabled) error = %v", err)
	}
	if _, err := queue.ObserveWorker(context.Background()); !errors.Is(err, ErrManagementLifecycleDisabled) {
		t.Fatalf("ObserveWorker(disabled) error = %v", err)
	}
	if _, err := queue.ObserveQueue(context.Background()); !errors.Is(err, ErrManagementLifecycleDisabled) {
		t.Fatalf("ObserveQueue(disabled) error = %v", err)
	}
}

type managedQueueWorker struct {
	tasks         chan core.TaskMessage
	requested     chan int32
	runStarted    chan struct{}
	finishRun     chan struct{}
	shutdown      chan struct{}
	shutdownOnce  sync.Once
	requestCalls  atomic.Int32
	shutdownCalls atomic.Int32
	controlCalls  atomic.Int32
	statusErr     error
	controlErr    error
	controlResult management.CommandResult
}

func newManagedQueueWorker() *managedQueueWorker {
	return &managedQueueWorker{
		tasks: make(chan core.TaskMessage, 1), requested: make(chan int32, 8),
		runStarted: make(chan struct{}, 1), finishRun: make(chan struct{}),
		shutdown: make(chan struct{}),
	}
}

func (w *managedQueueWorker) Run(context.Context, core.TaskMessage) error {
	w.runStarted <- struct{}{}
	<-w.finishRun
	return nil
}

func (w *managedQueueWorker) Shutdown() error {
	w.shutdownCalls.Add(1)
	w.shutdownOnce.Do(func() { close(w.shutdown) })
	return nil
}

func (w *managedQueueWorker) Queue(core.TaskMessage) error { return nil }

func (w *managedQueueWorker) Request() (core.TaskMessage, error) {
	call := w.requestCalls.Add(1)
	w.requested <- call
	select {
	case task := <-w.tasks:
		return task, nil
	case <-w.shutdown:
		return nil, ErrQueueShutdown
	}
}

func (w *managedQueueWorker) ObserveWorker(context.Context) (management.WorkerStatus, error) {
	if w.statusErr != nil {
		return management.WorkerStatus{}, w.statusErr
	}
	return management.WorkerStatus{
		ID: "worker-1", Version: "v1.0.0", StartedAt: time.Unix(1, 0).UTC(),
		HeartbeatAt: time.Now().UTC(), Queues: []string{"critical"}, Concurrency: 1,
		State: management.WorkerRunning, DrainStatus: management.DrainNotRequested,
		Backend: "test", Protocol: management.ProtocolVersion{Major: 1},
		Capabilities: []management.Capability{management.CapabilityQueueStatus},
	}, nil
}

func (w *managedQueueWorker) Execute(
	context.Context, management.Command,
) (management.CommandResult, error) {
	w.controlCalls.Add(1)
	return w.controlResult, w.controlErr
}

func (*managedQueueWorker) ObserveQueue(context.Context) (management.QueueStatus, error) {
	return management.QueueStatus{
		Backend: "test", Queue: "critical", ObservedAt: time.Now().UTC(),
	}, nil
}

type plainManagedWorker struct{}

type statusOnlyManagedWorker struct{ managedQueueWorker *managedQueueWorker }

func (w *statusOnlyManagedWorker) Run(ctx context.Context, task core.TaskMessage) error {
	return w.managedQueueWorker.Run(ctx, task)
}
func (w *statusOnlyManagedWorker) Shutdown() error { return w.managedQueueWorker.Shutdown() }
func (w *statusOnlyManagedWorker) Queue(task core.TaskMessage) error {
	return w.managedQueueWorker.Queue(task)
}
func (w *statusOnlyManagedWorker) Request() (core.TaskMessage, error) {
	return w.managedQueueWorker.Request()
}
func (w *statusOnlyManagedWorker) ObserveWorker(ctx context.Context) (management.WorkerStatus, error) {
	return w.managedQueueWorker.ObserveWorker(ctx)
}
func (w *statusOnlyManagedWorker) ObserveQueue(ctx context.Context) (management.QueueStatus, error) {
	return w.managedQueueWorker.ObserveQueue(ctx)
}

func (*plainManagedWorker) Run(context.Context, core.TaskMessage) error { return nil }
func (*plainManagedWorker) Shutdown() error                             { return nil }
func (*plainManagedWorker) Queue(core.TaskMessage) error                { return nil }
func (*plainManagedWorker) Request() (core.TaskMessage, error)          { return nil, ErrNoTaskInQueue }

func managedQueueLifecycle(t *testing.T) *management.WorkerLifecycle {
	t.Helper()
	lifecycle, err := management.NewWorkerLifecycle(management.WorkerLifecycleConfig{
		Metadata: management.StatusMetadata{
			ID: "worker-1", Version: "v1.0.0", Concurrency: 1,
			Protocol: management.ProtocolVersion{Major: 1},
		},
		WorkerGroup: "payments", Queue: "critical", MaxCommandResults: 32,
		Now: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatalf("NewWorkerLifecycle() error = %v", err)
	}
	return lifecycle
}

func managedQueueCommand(
	id string,
	action management.CommandAction,
	kind management.TargetKind,
	name string,
) management.Command {
	requested := time.Now().UTC()
	return management.Command{
		ID: id, IdempotencyKey: id, Actor: "operator-1", Reason: "maintenance",
		Protocol: management.ProtocolVersion{Major: 1}, Action: action,
		Target: management.Target{Kind: kind, Name: name}, RequestedAt: requested,
		Deadline: requested.Add(time.Minute),
	}
}

func managedJob(body string) *job.Message {
	return &job.Message{Body: []byte(body), Timeout: time.Minute}
}

func waitForManagedQueueState(t *testing.T, queue *Queue, wanted management.WorkerState) {
	t.Helper()
	for {
		status, err := queue.ObserveWorker(context.Background())
		if err != nil {
			t.Fatalf("ObserveWorker() error = %v", err)
		}
		if status.State == wanted {
			return
		}
		select {
		case <-t.Context().Done():
			t.Fatalf("waiting for worker state %q: %v", wanted, t.Context().Err())
		default:
			runtime.Gosched()
		}
	}
}

func containsCapability(values []management.Capability, wanted management.Capability) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
