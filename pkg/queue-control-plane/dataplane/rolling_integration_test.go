package dataplane

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	queue "github.com/faustbrian/golib/pkg/queue/management"
	"github.com/faustbrian/golib/pkg/queue/managementhttp"
)

func TestRollingGoQueueHTTPCompatibilityIntegration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	resolver := &rollingStatusResolver{}
	server, client := rollingManagementEndpoint(t, now, []queue.WorkerStatus{
		rollingWorker("worker-older", "v0.9.0", queue.ProtocolVersion{Major: 1}, now),
		rollingWorker("worker-current", "v1.2.0", queue.ProtocolVersion{Major: 1, Minor: 2}, now),
		rollingWorker("worker-newer", "v2.0.0", queue.ProtocolVersion{Major: 2}, now),
	})
	resolver.set(client)
	statusSource, err := NewStatusSource(resolver)
	if err != nil {
		t.Fatalf("NewStatusSource() error = %v", err)
	}
	source, err := NewFleetSource(statusSource)
	if err != nil {
		t.Fatalf("NewFleetSource() error = %v", err)
	}

	snapshot, err := source.SnapshotTenant(
		context.Background(), "tenant-1", now, 30*time.Second,
	)
	if err != nil || len(snapshot.Workers) != 3 {
		t.Fatalf("SnapshotTenant() = (%+v, %v), want three workers", snapshot, err)
	}
	supported := fleet.ProtocolRange{
		Minimum: fleet.ProtocolVersion{Major: 1, Minor: 1},
		Maximum: fleet.ProtocolVersion{Major: 1, Minor: 3},
	}
	wantStates := map[string]fleet.CompatibilityState{
		"worker-older":   fleet.CompatibilityWorkerOlder,
		"worker-current": fleet.CompatibilityCompatible,
		"worker-newer":   fleet.CompatibilityWorkerNewer,
	}
	for _, worker := range snapshot.Workers {
		compatibility := fleet.Negotiate(
			supported,
			worker.Protocol,
			worker.Capabilities,
			[]fleet.Capability{
				fleet.CapabilityPause, fleet.CapabilityReplay,
				fleet.CapabilityRetentionCount,
			},
		)
		if compatibility.State != wantStates[worker.WorkerID] {
			t.Fatalf("%s compatibility = %+v", worker.WorkerID, compatibility)
		}
		if worker.WorkerID == "worker-current" {
			assertRollingCapabilities(
				t, compatibility.Enabled,
				[]fleet.Capability{
					fleet.CapabilityPause, fleet.CapabilityReplay,
					fleet.CapabilityRetentionCount,
				},
			)
		} else if len(compatibility.Enabled) != 0 {
			t.Fatalf("%s enabled capabilities = %v, want none", worker.WorkerID, compatibility.Enabled)
		}
	}

	server.Close()
	if _, err := source.SnapshotTenant(
		context.Background(), "tenant-1", now.Add(time.Minute), 30*time.Second,
	); err == nil {
		t.Fatal("partitioned SnapshotTenant() error = nil")
	}

	reconnectedAt := now.Add(61 * time.Second)
	_, reconnectedClient := rollingManagementEndpoint(t, reconnectedAt, []queue.WorkerStatus{
		rollingWorker(
			"worker-current", "v1.3.0",
			queue.ProtocolVersion{Major: 1, Minor: 3}, reconnectedAt,
		),
	})
	resolver.set(reconnectedClient)
	reconnected, err := source.SnapshotTenant(
		context.Background(), "tenant-1", reconnectedAt, 30*time.Second,
	)
	if err != nil || len(reconnected.Workers) != 1 ||
		reconnected.Workers[0].State != fleet.StateRunning {
		t.Fatalf("reconnected SnapshotTenant() = (%+v, %v)", reconnected, err)
	}
	compatibility := fleet.Negotiate(
		supported,
		reconnected.Workers[0].Protocol,
		reconnected.Workers[0].Capabilities,
		[]fleet.Capability{
			fleet.CapabilityPause, fleet.CapabilityReplay,
			fleet.CapabilityRetentionCount,
		},
	)
	if compatibility.State != fleet.CompatibilityCompatible {
		t.Fatalf("reconnected compatibility = %+v", compatibility)
	}
}

func rollingManagementEndpoint(
	t *testing.T,
	now time.Time,
	workers []queue.WorkerStatus,
) (*httptest.Server, *managementhttp.Client) {
	t.Helper()
	provider := &rollingStatusProvider{workers: workers, now: now}
	handler, err := managementhttp.NewHandler(managementhttp.HandlerConfig{
		Token: "rolling-secret", Status: provider,
	})
	if err != nil {
		t.Fatalf("managementhttp.NewHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := managementhttp.NewClient(managementhttp.ClientConfig{
		BaseURL:    server.URL,
		Token:      "rolling-secret",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("managementhttp.NewClient() error = %v", err)
	}

	return server, client
}

func rollingWorker(
	id string,
	version string,
	protocol queue.ProtocolVersion,
	now time.Time,
) queue.WorkerStatus {
	return queue.WorkerStatus{
		ID: id, Version: version,
		StartedAt: now.Add(-time.Hour), HeartbeatAt: now,
		Queues: []string{"critical"}, Concurrency: 4,
		State: queue.WorkerRunning, DrainStatus: queue.DrainNotRequested,
		Backend: "valkeystream", Protocol: protocol,
		Capabilities: []queue.Capability{
			queue.CapabilityPause, queue.CapabilityReplay,
			queue.CapabilityRetentionCount,
		},
	}
}

func assertRollingCapabilities(
	t *testing.T,
	got []fleet.Capability,
	want []fleet.Capability,
) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("capabilities = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("capabilities = %v, want %v", got, want)
		}
	}
}

type rollingStatusProvider struct {
	workers []queue.WorkerStatus
	now     time.Time
}

func (p *rollingStatusProvider) ListWorkers(
	context.Context,
	queue.StatusPageRequest,
) (queue.WorkerStatusPage, error) {
	return queue.WorkerStatusPage{Items: p.workers}, nil
}

func (p *rollingStatusProvider) ListQueues(
	context.Context,
	queue.StatusPageRequest,
) (queue.QueueStatusPage, error) {
	return queue.QueueStatusPage{
		Items: []queue.QueueStatus{{
			Backend: "valkeystream", Queue: "critical", ObservedAt: p.now,
		}},
	}, nil
}

type rollingStatusResolver struct {
	mu     sync.RWMutex
	reader queue.StatusReader
}

func (r *rollingStatusResolver) set(reader queue.StatusReader) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reader = reader
}

func (r *rollingStatusResolver) ResolveStatusReader(
	context.Context,
	string,
) (queue.StatusReader, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.reader == nil {
		return nil, errors.New("rolling status reader unavailable")
	}

	return r.reader, nil
}
