package dataplane

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goqueue "github.com/faustbrian/golib/pkg/queue"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	queue "github.com/faustbrian/golib/pkg/queue/management"
	"github.com/faustbrian/golib/pkg/queue/managementhttp"
	redisstream "github.com/faustbrian/golib/pkg/queue/redisstream"
)

func TestRedisDeadLettersFlowThroughControlPlaneContracts(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	protocol := queue.ProtocolVersion{Major: 1}
	worker, err := redisstream.NewWorkerE(
		redisstream.WithAddr(server.Addr()), redisstream.WithStreamName("jobs"),
		redisstream.WithGroup("workers"), redisstream.WithConsumer("worker-1"),
		redisstream.WithBlockTime(time.Millisecond),
		redisstream.WithRequestTimeout(time.Second),
		redisstream.WithFailureStream("jobs-failures"),
		redisstream.WithDeadLetter("jobs-dead", 5),
		redisstream.WithRecordRetention(100),
		redisstream.WithReplayDestinations("archive"),
		redisstream.WithManagementStatus(queue.StatusMetadata{
			ID: "worker-1", Version: "v1", Concurrency: 1, Protocol: protocol,
		}),
	)
	if err != nil {
		t.Fatalf("redisstream.NewWorkerE() error = %v", err)
	}
	t.Cleanup(func() { _ = worker.Shutdown() })
	message := job.NewMessage(controlPlanePayload("sensitive"))
	if err := worker.Queue(&message); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	delivery, err := worker.Request()
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if err := delivery.(*job.Message).NackFailure(queue.NewFailure(
		queue.ClassificationPermanent, "invalid_order", errors.New("invalid order"),
	)); err != nil {
		t.Fatalf("NackFailure() error = %v", err)
	}
	statusReader, err := queue.NewStatusReader(queue.StatusReaderConfig{
		Workers: []queue.WorkerStatusProvider{worker},
		Queues:  []queue.QueueStatusProvider{worker},
	})
	if err != nil {
		t.Fatalf("NewStatusReader() error = %v", err)
	}
	handler, err := managementhttp.NewHandler(managementhttp.HandlerConfig{
		Token: "integration-secret", Status: statusReader,
		Records: worker, Controller: worker,
	})
	if err != nil {
		t.Fatalf("managementhttp.NewHandler() error = %v", err)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	client, err := managementhttp.NewClient(managementhttp.ClientConfig{
		BaseURL: httpServer.URL, Token: "integration-secret", HTTPClient: httpServer.Client(),
	})
	if err != nil {
		t.Fatalf("managementhttp.NewClient() error = %v", err)
	}
	resolver := &goQueueIntegrationResolver{client: client}
	records, err := NewRecordSource(resolver)
	if err != nil {
		t.Fatalf("NewRecordSource() error = %v", err)
	}
	page, err := records.ListDeadLetters(t.Context(), "tenant-1", queue.PageRequest{
		Limit: 10, Sort: queue.SortOccurredAt, Direction: queue.SortAscending,
	})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("ListDeadLetters() = (%+v, %v), want one record", page, err)
	}
	if page.Items[0].Payload.Visibility != queue.PayloadHidden ||
		len(page.Items[0].Payload.Data) != 0 {
		t.Fatal("dead-letter list disclosed payload")
	}
	workers, err := client.ListWorkers(t.Context(), queue.StatusPageRequest{Limit: 1})
	if err != nil || len(workers.Items) != 1 ||
		!containsQueueCapability(workers.Items[0].Capabilities, queue.CapabilityReplay) ||
		!containsQueueCapability(
			workers.Items[0].Capabilities, queue.CapabilityRetentionCount,
		) {
		t.Fatalf(
			"ListWorkers() = (%+v, %v), want replay and retention capabilities",
			workers, err,
		)
	}
	dispatcher, err := NewControllerDispatcher(
		resolver, protocol, time.Minute, time.Now,
	)
	if err != nil {
		t.Fatalf("NewControllerDispatcher() error = %v", err)
	}
	now := time.Now().UTC()
	replay := controlplane.Command{
		CommandID:      "replay-command-1",
		IdempotencyKey: "replay-1", TenantID: "tenant-1", Actor: "operator-1",
		Reason: "replay corrected record", Action: controlplane.ActionReplay,
		Target:      controlplane.Target{Kind: controlplane.TargetDeadLetter, Name: page.Items[0].ID},
		RequestedAt: now, Confirmed: true, Replay: &controlplane.Replay{
			Destination:       "archive",
			IdempotencyPolicy: controlplane.ReplayRejectDuplicate,
		},
	}
	outcome, err := dispatcher.DispatchResult(t.Context(), replay)
	if err != nil || outcome.Status != controlplane.CommandSucceeded {
		t.Fatalf("replay DispatchResult() = (%+v, %v)", outcome, err)
	}
	retry := replay
	retry.IdempotencyKey = "retry-1"
	retry.Action = controlplane.ActionRetry
	retry.Confirmed = false
	retry.Replay = nil
	outcome, err = dispatcher.DispatchResult(t.Context(), retry)
	if err != nil || outcome.Status != controlplane.CommandSucceeded {
		t.Fatalf("retry DispatchResult() = (%+v, %v)", outcome, err)
	}
	page, err = records.ListDeadLetters(t.Context(), "tenant-1", queue.PageRequest{
		Limit: 10, Sort: queue.SortOccurredAt, Direction: queue.SortAscending,
	})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("ListDeadLetters() after retry = (%+v, %v), want empty", page, err)
	}
}

type controlPlanePayload string

func (p controlPlanePayload) Bytes() []byte { return []byte(p) }

func containsQueueCapability(capabilities []queue.Capability, want queue.Capability) bool {
	for _, capability := range capabilities {
		if capability == want {
			return true
		}
	}

	return false
}

func TestControllerDispatcherEnforcesLifecycleThroughGoQueueHTTP(t *testing.T) {
	t.Parallel()

	requestedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	worker := &managedRingWorker{Ring: goqueue.NewRing(), now: requestedAt}
	lifecycle, err := queue.NewWorkerLifecycle(queue.WorkerLifecycleConfig{
		Metadata: queue.StatusMetadata{
			ID: "worker-1", Version: "v1.2.0", Concurrency: 1,
			Protocol: queue.ProtocolVersion{Major: 1},
		},
		WorkerGroup: "payments", Queue: "critical", MaxCommandResults: 8,
		Now: func() time.Time { return requestedAt.Add(time.Second) },
	})
	if err != nil {
		t.Fatalf("NewWorkerLifecycle() error = %v", err)
	}
	managedQueue, err := goqueue.NewQueue(
		goqueue.WithWorker(worker), goqueue.WithWorkerLifecycle(lifecycle),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	t.Cleanup(managedQueue.Release)
	statusReader, err := queue.NewStatusReader(queue.StatusReaderConfig{
		Workers: []queue.WorkerStatusProvider{managedQueue},
		Queues:  []queue.QueueStatusProvider{managedQueue},
	})
	if err != nil {
		t.Fatalf("NewStatusReader() error = %v", err)
	}
	handler, err := managementhttp.NewHandler(managementhttp.HandlerConfig{
		Token: "integration-secret", Status: statusReader, Controller: managedQueue,
	})
	if err != nil {
		t.Fatalf("managementhttp.NewHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := managementhttp.NewClient(managementhttp.ClientConfig{
		BaseURL: server.URL, Token: "integration-secret", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("managementhttp.NewClient() error = %v", err)
	}
	dispatcher, err := NewControllerDispatcher(
		&goQueueIntegrationResolver{client: client},
		queue.ProtocolVersion{Major: 1},
		time.Minute,
		func() time.Time { return requestedAt.Add(time.Second) },
	)
	if err != nil {
		t.Fatalf("NewControllerDispatcher() error = %v", err)
	}

	pause := lifecycleControlCommand("pause-1", controlplane.ActionPause, requestedAt)
	outcome, err := dispatcher.DispatchResult(context.Background(), pause)
	if err != nil || outcome.Status != controlplane.CommandSucceeded {
		t.Fatalf("pause DispatchResult() = (%+v, %v)", outcome, err)
	}
	assertManagedWorkerState(t, client, queue.WorkerPaused)

	resume := lifecycleControlCommand("resume-1", controlplane.ActionResume, requestedAt)
	outcome, err = dispatcher.DispatchResult(context.Background(), resume)
	if err != nil || outcome.Status != controlplane.CommandSucceeded {
		t.Fatalf("resume DispatchResult() = (%+v, %v)", outcome, err)
	}
	assertManagedWorkerState(t, client, queue.WorkerRunning)

	outcome, err = dispatcher.DispatchResult(context.Background(), pause)
	if err != nil || outcome.Status != controlplane.CommandSucceeded {
		t.Fatalf("duplicate pause DispatchResult() = (%+v, %v)", outcome, err)
	}
	assertManagedWorkerState(t, client, queue.WorkerRunning)
}

func TestControlPlaneOutageCannotStopOrMutateQueueDelivery(t *testing.T) {
	t.Parallel()

	requestedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	delivered := make(chan struct{}, 1)
	worker := &managedRingWorker{Ring: goqueue.NewRing(), now: requestedAt}
	lifecycle, err := queue.NewWorkerLifecycle(queue.WorkerLifecycleConfig{
		Metadata: queue.StatusMetadata{
			ID: "worker-1", Version: "v1.2.0", Concurrency: 1,
			Protocol: queue.ProtocolVersion{Major: 1},
		},
		WorkerGroup: "payments", Queue: "critical", MaxCommandResults: 8,
		Now: func() time.Time { return requestedAt.Add(time.Second) },
	})
	if err != nil {
		t.Fatalf("NewWorkerLifecycle() error = %v", err)
	}
	managedQueue, err := goqueue.NewQueue(
		goqueue.WithWorker(worker), goqueue.WithWorkerLifecycle(lifecycle),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	t.Cleanup(managedQueue.Release)
	managedQueue.Start()

	handler, err := managementhttp.NewHandler(managementhttp.HandlerConfig{
		Token: "integration-secret", Controller: managedQueue,
	})
	if err != nil {
		t.Fatalf("managementhttp.NewHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	client, err := managementhttp.NewClient(managementhttp.ClientConfig{
		BaseURL: server.URL, Token: "integration-secret", HTTPClient: server.Client(),
	})
	if err != nil {
		server.Close()
		t.Fatalf("managementhttp.NewClient() error = %v", err)
	}
	server.Close()
	dispatcher, err := NewControllerDispatcher(
		&goQueueIntegrationResolver{client: client},
		queue.ProtocolVersion{Major: 1}, time.Minute,
		func() time.Time { return requestedAt.Add(time.Second) },
	)
	if err != nil {
		t.Fatalf("NewControllerDispatcher() error = %v", err)
	}

	outcome, err := dispatcher.DispatchResult(
		context.Background(), lifecycleControlCommand("pause-outage", controlplane.ActionPause, requestedAt),
	)
	if err != nil || outcome.Status != controlplane.CommandUnknown ||
		outcome.Failure != controlplane.FailureOutcomeUnknown {
		t.Fatalf("DispatchResult(outage) = (%+v, %v), want explicit unknown", outcome, err)
	}
	if err := managedQueue.QueueTask(func(context.Context) error {
		delivered <- struct{}{}

		return nil
	}); err != nil {
		t.Fatalf("QueueTask() error = %v", err)
	}
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("ordinary delivery stopped during control-plane outage")
	}
	status, err := managedQueue.ObserveWorker(context.Background())
	if err != nil {
		t.Fatalf("ObserveWorker() error = %v", err)
	}
	if status.State != queue.WorkerRunning {
		t.Fatalf("worker state = %q, want unchanged running", status.State)
	}
}

func lifecycleControlCommand(
	id string,
	action controlplane.Action,
	requestedAt time.Time,
) controlplane.Command {
	return controlplane.Command{
		CommandID:      id + "-command",
		IdempotencyKey: id, TenantID: "tenant-1", Actor: "operator-1",
		Reason: "Integration lifecycle verification", Action: action,
		Target:      controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"},
		RequestedAt: requestedAt,
	}
}

func assertManagedWorkerState(
	t *testing.T,
	client *managementhttp.Client,
	want queue.WorkerState,
) {
	t.Helper()
	page, err := client.ListWorkers(t.Context(), queue.StatusPageRequest{Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].State != want {
		t.Fatalf("ListWorkers() = (%+v, %v), want state %q", page, err, want)
	}
}

type managedRingWorker struct {
	*goqueue.Ring
	now time.Time
}

func (w *managedRingWorker) ObserveWorker(context.Context) (queue.WorkerStatus, error) {
	return queue.WorkerStatus{
		ID: "worker-1", Version: "v1.2.0",
		StartedAt: w.now.Add(-time.Hour), HeartbeatAt: w.now,
		Queues: []string{"critical"}, Concurrency: 1,
		State: queue.WorkerRunning, DrainStatus: queue.DrainNotRequested,
		Backend: "memory", Protocol: queue.ProtocolVersion{Major: 1},
		Capabilities: []queue.Capability{
			queue.CapabilityPause, queue.CapabilityResume,
		},
	}, nil
}

func (w *managedRingWorker) ObserveQueue(context.Context) (queue.QueueStatus, error) {
	return queue.QueueStatus{
		Backend: "memory", Queue: "critical", ObservedAt: w.now,
	}, nil
}

var _ core.Worker = (*managedRingWorker)(nil)

type goQueueIntegrationResolver struct {
	client *managementhttp.Client
}

func (r *goQueueIntegrationResolver) ResolveRecordReader(
	context.Context,
	string,
) (queue.RecordReader, error) {
	return r.client, nil
}

func (r *goQueueIntegrationResolver) ResolveController(
	context.Context,
	string,
) (queue.Controller, error) {
	return r.client, nil
}
