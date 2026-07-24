package dataplane

import (
	"context"
	"errors"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestStatusSourceListsValidatedTenantWorkersAndQueues(t *testing.T) {
	t.Parallel()

	request := queue.StatusPageRequest{Cursor: "current", Limit: 25}
	reader := &statusReaderStub{
		workers: queue.WorkerStatusPage{
			Items: []queue.WorkerStatus{validManagementWorker()}, NextCursor: "workers-next",
		},
		queues: queue.QueueStatusPage{
			Items: []queue.QueueStatus{validManagementQueue()}, NextCursor: "queues-next",
		},
	}
	resolver := &statusResolverStub{reader: reader}
	source, err := NewStatusSource(resolver)
	if err != nil {
		t.Fatalf("NewStatusSource() error = %v", err)
	}
	workers, err := source.ListWorkers(context.Background(), "tenant-1", request)
	if err != nil || len(workers.Items) != 1 || workers.NextCursor != "workers-next" {
		t.Fatalf("ListWorkers() = (%+v, %v)", workers, err)
	}
	queues, err := source.ListQueues(context.Background(), "tenant-1", request)
	if err != nil || len(queues.Items) != 1 || queues.NextCursor != "queues-next" {
		t.Fatalf("ListQueues() = (%+v, %v)", queues, err)
	}
	if resolver.tenant != "tenant-1" || resolver.calls != 2 ||
		reader.workerRequest != request || reader.queueRequest != request {
		t.Fatalf("resolution = tenant %q calls %d requests %+v/%+v", resolver.tenant, resolver.calls, reader.workerRequest, reader.queueRequest)
	}
}

func TestStatusSourceFailsClosedAtEveryBoundary(t *testing.T) {
	t.Parallel()

	var typedNilResolver *statusResolverStub
	for _, resolver := range []StatusReaderResolver{nil, typedNilResolver} {
		source, err := NewStatusSource(resolver)
		if source != nil || !errors.Is(err, ErrInvalidStatusConfiguration) {
			t.Fatalf("NewStatusSource() = (%v, %v)", source, err)
		}
	}

	request := queue.StatusPageRequest{Limit: 1}
	resolverErr := errors.New("status transport unavailable")
	source := mustStatusSource(t, &statusResolverStub{err: resolverErr})
	if _, err := source.ListWorkers(context.Background(), "tenant-1", request); !errors.Is(err, resolverErr) {
		t.Fatalf("resolver ListWorkers() error = %v", err)
	}
	var typedNilReader *statusReaderStub
	source = mustStatusSource(t, &statusResolverStub{reader: typedNilReader})
	if _, err := source.ListQueues(context.Background(), "tenant-1", request); !errors.Is(err, ErrStatusReaderUnavailable) {
		t.Fatalf("nil reader ListQueues() error = %v", err)
	}

	readerErr := errors.New("status read unavailable")
	reader := &statusReaderStub{err: readerErr}
	source = mustStatusSource(t, &statusResolverStub{reader: reader})
	if _, err := source.ListWorkers(context.Background(), "tenant-1", request); !errors.Is(err, readerErr) {
		t.Fatalf("reader ListWorkers() error = %v", err)
	}
	if _, err := source.ListQueues(context.Background(), "tenant-1", request); !errors.Is(err, readerErr) {
		t.Fatalf("reader ListQueues() error = %v", err)
	}

	reader.err = nil
	reader.workers = queue.WorkerStatusPage{Items: []queue.WorkerStatus{{ID: "invalid"}}}
	if _, err := source.ListWorkers(context.Background(), "tenant-1", request); !errors.Is(err, ErrInvalidStatusOutput) {
		t.Fatalf("invalid workers error = %v", err)
	}
	reader.queues = queue.QueueStatusPage{Items: []queue.QueueStatus{{Queue: "invalid"}}}
	if _, err := source.ListQueues(context.Background(), "tenant-1", request); !errors.Is(err, ErrInvalidStatusOutput) {
		t.Fatalf("invalid queues error = %v", err)
	}

	resolver := &statusResolverStub{reader: reader}
	source = mustStatusSource(t, resolver)
	invalidRequest := queue.StatusPageRequest{}
	if _, err := source.ListWorkers(context.Background(), "tenant-1", invalidRequest); err == nil || resolver.calls != 0 {
		t.Fatalf("invalid worker request = error %v calls %d", err, resolver.calls)
	}
	if _, err := source.ListQueues(context.Background(), "tenant-1", invalidRequest); err == nil || resolver.calls != 0 {
		t.Fatalf("invalid queue request = error %v calls %d", err, resolver.calls)
	}
	if _, err := source.ListWorkers(context.Background(), "", request); !errors.Is(err, ErrInvalidStatusRequest) || resolver.calls != 0 {
		t.Fatalf("invalid tenant = error %v calls %d", err, resolver.calls)
	}
}

func mustStatusSource(t *testing.T, resolver StatusReaderResolver) *StatusSource {
	t.Helper()

	source, err := NewStatusSource(resolver)
	if err != nil {
		t.Fatalf("NewStatusSource() error = %v", err)
	}

	return source
}

func validManagementWorker() queue.WorkerStatus {
	return queue.WorkerStatus{
		ID: "worker-1", Version: "v1.2.3",
		StartedAt:   time.Date(2026, time.July, 16, 11, 0, 0, 0, time.UTC),
		HeartbeatAt: time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
		Queues:      []string{"critical"}, Concurrency: 10, State: queue.WorkerRunning,
		CurrentJobs: 2, DrainStatus: queue.DrainNotRequested, Backend: "valkey-streams",
		Protocol:     queue.ProtocolVersion{Major: 1},
		Capabilities: []queue.Capability{queue.CapabilityWorkerStatus},
	}
}

func validManagementQueue() queue.QueueStatus {
	return queue.QueueStatus{
		Backend: "valkey-streams", Queue: "critical",
		ObservedAt: time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
		Metrics:    queue.QueueMetrics{Depth: queue.Measurement[int64]{Value: 5, Supported: true}},
	}
}

type statusResolverStub struct {
	reader queue.StatusReader
	err    error
	tenant string
	calls  int
}

func (s *statusResolverStub) ResolveStatusReader(
	_ context.Context,
	tenant string,
) (queue.StatusReader, error) {
	s.calls++
	s.tenant = tenant

	return s.reader, s.err
}

type statusReaderStub struct {
	workers       queue.WorkerStatusPage
	queues        queue.QueueStatusPage
	err           error
	workerRequest queue.StatusPageRequest
	queueRequest  queue.StatusPageRequest
}

func (s *statusReaderStub) ListWorkers(
	_ context.Context,
	request queue.StatusPageRequest,
) (queue.WorkerStatusPage, error) {
	s.workerRequest = request

	return s.workers, s.err
}

func (s *statusReaderStub) ListQueues(
	_ context.Context,
	request queue.StatusPageRequest,
) (queue.QueueStatusPage, error) {
	s.queueRequest = request

	return s.queues, s.err
}
