package dataplane

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestFleetSourceBuildsBoundedTenantSnapshotAcrossStatusPages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	first := validManagementWorker()
	first.ID = "worker-b"
	first.HeartbeatAt = now.Add(-time.Minute)
	second := validManagementWorker()
	second.ID = "worker-a"
	second.HeartbeatAt = now.Add(-time.Second)
	reader := &pagedWorkerStatusStub{pages: []queue.WorkerStatusPage{
		{Items: []queue.WorkerStatus{first}, NextCursor: "next"},
		{Items: []queue.WorkerStatus{second}},
	}}
	source, err := NewFleetSource(reader)
	if err != nil {
		t.Fatalf("NewFleetSource() error = %v", err)
	}
	snapshot, err := source.SnapshotTenant(
		context.Background(), "tenant-1", now, 30*time.Second,
	)
	if err != nil || len(snapshot.Workers) != 2 {
		t.Fatalf("SnapshotTenant() = (%+v, %v)", snapshot, err)
	}
	if snapshot.Workers[0].WorkerID != "worker-a" ||
		snapshot.Workers[0].State != fleet.StateRunning ||
		snapshot.Workers[1].WorkerID != "worker-b" ||
		snapshot.Workers[1].State != fleet.StateStale {
		t.Fatalf("workers = %+v, want sorted running/stale observations", snapshot.Workers)
	}
	if snapshot.Workers[0].TenantID != "tenant-1" ||
		snapshot.Workers[0].ObservedAt != second.HeartbeatAt ||
		snapshot.Workers[0].Protocol != (fleet.ProtocolVersion{Major: 1}) ||
		len(snapshot.Workers[0].Capabilities) != 1 {
		t.Fatalf("converted worker = %+v", snapshot.Workers[0])
	}
	if len(reader.requests) != 2 || reader.requests[0].Limit != queue.MaxStatusPageSize ||
		reader.requests[0].Cursor != "" || reader.requests[1].Cursor != "next" {
		t.Fatalf("requests = %+v, want bounded cursor traversal", reader.requests)
	}
}

func TestFleetSourceFailsClosedForInvalidDependenciesAndStatusTraversal(t *testing.T) {
	t.Parallel()

	var typedNil *pagedWorkerStatusStub
	for _, reader := range []WorkerStatusSource{nil, typedNil} {
		source, err := NewFleetSource(reader)
		if source != nil || !errors.Is(err, ErrInvalidFleetConfiguration) {
			t.Fatalf("NewFleetSource() = (%v, %v)", source, err)
		}
	}

	transportErr := errors.New("transport unavailable")
	reader := &pagedWorkerStatusStub{err: transportErr}
	source, err := NewFleetSource(reader)
	if err != nil {
		t.Fatalf("NewFleetSource() error = %v", err)
	}
	if _, err := source.SnapshotTenant(context.Background(), "tenant-1", time.Now(), time.Minute); !errors.Is(err, transportErr) {
		t.Fatalf("transport error = %v, want %v", err, transportErr)
	}

	reader.err = nil
	reader.pages = []queue.WorkerStatusPage{{NextCursor: "same"}, {NextCursor: "same"}}
	if _, err := source.SnapshotTenant(context.Background(), "tenant-1", time.Now(), time.Minute); !errors.Is(err, ErrInvalidFleetOutput) {
		t.Fatalf("repeated cursor error = %v", err)
	}

	reader.pages = []queue.WorkerStatusPage{{Items: []queue.WorkerStatus{{ID: "invalid"}}}}
	if _, err := source.SnapshotTenant(context.Background(), "tenant-1", time.Now(), time.Minute); !errors.Is(err, ErrInvalidFleetOutput) {
		t.Fatalf("malformed page error = %v", err)
	}

	reader.pages = []queue.WorkerStatusPage{
		{NextCursor: "1"}, {NextCursor: "2"}, {NextCursor: "3"},
		{NextCursor: "4"}, {NextCursor: "5"},
	}
	if _, err := source.SnapshotTenant(context.Background(), "tenant-1", time.Now(), time.Minute); !errors.Is(err, ErrInvalidFleetOutput) {
		t.Fatalf("fleet limit error = %v", err)
	}

	reader.pages = nil
	reader.requests = nil
	if _, err := source.SnapshotTenant(context.Background(), "", time.Now(), time.Minute); !errors.Is(err, ErrInvalidStatusRequest) {
		t.Fatalf("invalid tenant error = %v", err)
	}
}

type pagedWorkerStatusStub struct {
	pages    []queue.WorkerStatusPage
	err      error
	requests []queue.StatusPageRequest
}

func (s *pagedWorkerStatusStub) ListWorkers(
	_ context.Context,
	_ string,
	request queue.StatusPageRequest,
) (queue.WorkerStatusPage, error) {
	s.requests = append(s.requests, request)
	if s.err != nil {
		return queue.WorkerStatusPage{}, s.err
	}
	if len(s.pages) == 0 {
		return queue.WorkerStatusPage{}, nil
	}
	page := s.pages[0]
	s.pages = s.pages[1:]

	return page, nil
}
