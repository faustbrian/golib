package management

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestStatusReaderListsBoundedValidatedProviderPages(t *testing.T) {
	t.Parallel()

	providers := []StatusProvider{
		&statusProviderStub{worker: providerWorker("worker-c"), queue: providerQueue("queue-c")},
		&statusProviderStub{worker: providerWorker("worker-a"), queue: providerQueue("queue-a")},
		&statusProviderStub{worker: providerWorker("worker-b"), queue: providerQueue("queue-b")},
	}
	reader, err := NewStatusReader(providerConfig(providers...))
	if err != nil {
		t.Fatalf("NewStatusReader() error = %v", err)
	}
	ctx := context.WithValue(context.Background(), statusReaderContextKey{}, "forwarded")
	workers, err := reader.ListWorkers(ctx, StatusPageRequest{Limit: 2})
	if err != nil || len(workers.Items) != 2 || workers.Items[0].ID != "worker-a" ||
		workers.Items[1].ID != "worker-b" || workers.NextCursor == "" {
		t.Fatalf("ListWorkers(first) = (%+v, %v)", workers, err)
	}
	nextWorkers, err := reader.ListWorkers(ctx, StatusPageRequest{Limit: 2, Cursor: workers.NextCursor})
	if err != nil || len(nextWorkers.Items) != 1 || nextWorkers.Items[0].ID != "worker-c" || nextWorkers.NextCursor != "" {
		t.Fatalf("ListWorkers(next) = (%+v, %v)", nextWorkers, err)
	}
	queues, err := reader.ListQueues(ctx, StatusPageRequest{Limit: 2})
	if err != nil || len(queues.Items) != 2 || queues.Items[0].Queue != "queue-a" ||
		queues.Items[1].Queue != "queue-b" || queues.NextCursor == "" {
		t.Fatalf("ListQueues(first) = (%+v, %v)", queues, err)
	}
	for _, provider := range providers {
		stub := provider.(*statusProviderStub)
		if stub.ctx.Value(statusReaderContextKey{}) != "forwarded" {
			t.Fatalf("provider context = %v, want forwarded", stub.ctx)
		}
	}
}

func TestStatusReaderFailsClosedAtConstructionAndProviderBoundaries(t *testing.T) {
	t.Parallel()

	var typedNil *statusProviderStub
	tooMany := make([]StatusProvider, MaxStatusProviders+1)
	for index := range tooMany {
		tooMany[index] = &statusProviderStub{}
	}
	for _, providers := range [][]StatusProvider{nil, {nil}, {typedNil}, tooMany} {
		reader, err := NewStatusReader(providerConfig(providers...))
		if reader != nil || !errors.Is(err, ErrInvalidStatusProviders) {
			t.Fatalf("NewStatusReader(%d) = (%v, %v)", len(providers), reader, err)
		}
	}
	reader, err := NewStatusReader(StatusReaderConfig{
		Queues: []QueueStatusProvider{typedNil},
	})
	if reader != nil || !errors.Is(err, ErrInvalidStatusProviders) {
		t.Fatalf("NewStatusReader(nil queue) = (%v, %v)", reader, err)
	}

	providerErr := errors.New("provider unavailable")
	provider := &statusProviderStub{err: providerErr}
	reader, err = NewStatusReader(providerConfig(provider))
	if err != nil {
		t.Fatalf("NewStatusReader() error = %v", err)
	}
	if _, err := reader.ListWorkers(context.Background(), StatusPageRequest{Limit: 1}); !errors.Is(err, providerErr) {
		t.Fatalf("worker provider error = %v", err)
	}
	if _, err := reader.ListQueues(context.Background(), StatusPageRequest{Limit: 1}); !errors.Is(err, providerErr) {
		t.Fatalf("queue provider error = %v", err)
	}

	provider.err = nil
	provider.worker = WorkerStatus{ID: "invalid"}
	provider.queue = QueueStatus{Queue: "invalid"}
	if _, err := reader.ListWorkers(context.Background(), StatusPageRequest{Limit: 1}); !errors.Is(err, ErrInvalidStatusProviderOutput) {
		t.Fatalf("invalid worker error = %v", err)
	}
	if _, err := reader.ListQueues(context.Background(), StatusPageRequest{Limit: 1}); !errors.Is(err, ErrInvalidStatusProviderOutput) {
		t.Fatalf("invalid queue error = %v", err)
	}

	duplicate := &statusProviderStub{
		worker: providerWorker("duplicate"), queue: providerQueue("duplicate"),
	}
	reader, err = NewStatusReader(providerConfig(duplicate, duplicate))
	if err != nil {
		t.Fatalf("NewStatusReader(duplicates) error = %v", err)
	}
	if _, err := reader.ListWorkers(context.Background(), StatusPageRequest{Limit: 1}); !errors.Is(err, ErrInvalidStatusProviderOutput) {
		t.Fatalf("duplicate worker error = %v", err)
	}
	if _, err := reader.ListQueues(context.Background(), StatusPageRequest{Limit: 1}); !errors.Is(err, ErrInvalidStatusProviderOutput) {
		t.Fatalf("duplicate queue error = %v", err)
	}

	otherBackend := &statusProviderStub{
		worker: providerWorker("other"), queue: providerQueue("duplicate"),
	}
	otherBackend.queue.Backend = "redis-streams"
	reader, err = NewStatusReader(providerConfig(duplicate, otherBackend))
	if err != nil {
		t.Fatalf("NewStatusReader(backends) error = %v", err)
	}
	queues, err := reader.ListQueues(context.Background(), StatusPageRequest{Limit: 2})
	if err != nil || queues.Items[0].Backend != "redis-streams" {
		t.Fatalf("backend ordering = (%+v, %v)", queues, err)
	}

	for _, request := range []StatusPageRequest{
		{},
		{Limit: 1, Cursor: "%"},
		{Limit: 1, Cursor: "not-a-cursor"},
		{Limit: 1, Cursor: base64.RawURLEncoding.EncodeToString([]byte("1001"))},
		{Limit: 1, Cursor: base64.RawURLEncoding.EncodeToString([]byte("3"))},
	} {
		if _, err := reader.ListWorkers(context.Background(), request); err == nil {
			t.Fatalf("ListWorkers(%+v) error = nil", request)
		}
		if _, err := reader.ListQueues(context.Background(), request); err == nil {
			t.Fatalf("ListQueues(%+v) error = nil", request)
		}
	}

	valueReader, err := NewStatusReader(providerConfig(valueStatusProvider{}))
	if err != nil || valueReader == nil {
		t.Fatalf("NewStatusReader(value) = (%v, %v)", valueReader, err)
	}
}

func providerConfig(providers ...StatusProvider) StatusReaderConfig {
	config := StatusReaderConfig{
		Workers: make([]WorkerStatusProvider, 0, len(providers)),
		Queues:  make([]QueueStatusProvider, 0, len(providers)),
	}
	for _, provider := range providers {
		config.Workers = append(config.Workers, provider)
		config.Queues = append(config.Queues, provider)
	}

	return config
}

func providerWorker(id string) WorkerStatus {
	return WorkerStatus{
		ID: id, Version: "v1.0.0", StartedAt: time.Unix(1, 0).UTC(),
		HeartbeatAt: time.Unix(2, 0).UTC(), Queues: []string{"critical"},
		Concurrency: 1, State: WorkerRunning, DrainStatus: DrainNotRequested,
		Backend: "valkey-streams", Protocol: ProtocolVersion{Major: 1},
		Capabilities: []Capability{CapabilityWorkerStatus, CapabilityQueueStatus},
	}
}

func providerQueue(name string) QueueStatus {
	return QueueStatus{
		Backend: "valkey-streams", Queue: name, ObservedAt: time.Unix(2, 0).UTC(),
	}
}

type statusReaderContextKey struct{}

type statusProviderStub struct {
	worker WorkerStatus
	queue  QueueStatus
	err    error
	ctx    context.Context
}

type valueStatusProvider struct{}

func (valueStatusProvider) ObserveWorker(context.Context) (WorkerStatus, error) {
	return providerWorker("value-worker"), nil
}

func (valueStatusProvider) ObserveQueue(context.Context) (QueueStatus, error) {
	return providerQueue("value-queue"), nil
}

func (s *statusProviderStub) ObserveWorker(ctx context.Context) (WorkerStatus, error) {
	s.ctx = ctx
	return s.worker, s.err
}

func (s *statusProviderStub) ObserveQueue(ctx context.Context) (QueueStatus, error) {
	s.ctx = ctx
	return s.queue, s.err
}
