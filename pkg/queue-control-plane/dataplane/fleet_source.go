package dataplane

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

const maxFleetStatusPages = 5

var (
	// ErrInvalidFleetConfiguration reports a missing worker-status source.
	ErrInvalidFleetConfiguration = errors.New("dataplane: invalid fleet configuration")
	// ErrInvalidFleetOutput reports malformed or unbounded worker traversal.
	ErrInvalidFleetOutput = errors.New("dataplane: invalid fleet output")
)

// WorkerStatusSource reads bounded queue worker status pages.
type WorkerStatusSource interface {
	ListWorkers(
		context.Context,
		string,
		queue.StatusPageRequest,
	) (queue.WorkerStatusPage, error)
}

// FleetSource converts queue status into control-plane worker snapshots.
type FleetSource struct {
	source WorkerStatusSource
}

// NewFleetSource creates a bounded remote worker snapshot source.
func NewFleetSource(source WorkerStatusSource) (*FleetSource, error) {
	if nilInterface(source) {
		return nil, ErrInvalidFleetConfiguration
	}

	return &FleetSource{source: source}, nil
}

// SnapshotTenant returns one fail-safe tenant fleet observation.
func (s *FleetSource) SnapshotTenant(
	ctx context.Context,
	tenant string,
	now time.Time,
	staleAfter time.Duration,
) (fleet.RegistrySnapshot, error) {
	if strings.TrimSpace(tenant) == "" || len(tenant) > controlplane.MaxIdentityBytes ||
		now.IsZero() || staleAfter <= 0 {
		return fleet.RegistrySnapshot{}, ErrInvalidStatusRequest
	}

	snapshot := fleet.RegistrySnapshot{
		Workers: make([]fleet.WorkerSnapshot, 0, queue.MaxStatusPageSize),
	}
	cursor := ""
	for range maxFleetStatusPages {
		page, err := s.source.ListWorkers(ctx, tenant, queue.StatusPageRequest{
			Cursor: cursor,
			Limit:  queue.MaxStatusPageSize,
		})
		if err != nil {
			return fleet.RegistrySnapshot{}, err
		}
		if page.Validate() != nil {
			return fleet.RegistrySnapshot{}, ErrInvalidFleetOutput
		}
		for _, worker := range page.Items {
			heartbeat := managementHeartbeat(tenant, worker)
			snapshot.Workers = append(snapshot.Workers, fleet.WorkerSnapshot{
				Heartbeat: heartbeat,
				State:     heartbeat.EffectiveState(now, staleAfter),
			})
		}
		if page.NextCursor == "" {
			sortFleetWorkers(snapshot.Workers)

			return snapshot, nil
		}
		if page.NextCursor == cursor {
			return fleet.RegistrySnapshot{}, ErrInvalidFleetOutput
		}
		cursor = page.NextCursor
	}

	return fleet.RegistrySnapshot{}, ErrInvalidFleetOutput
}

func managementHeartbeat(tenant string, worker queue.WorkerStatus) fleet.Heartbeat {
	capabilities := make([]fleet.Capability, 0, len(worker.Capabilities))
	for _, capability := range worker.Capabilities {
		capabilities = append(capabilities, fleet.Capability(capability))
	}

	return fleet.Heartbeat{
		TenantID: tenant, WorkerID: worker.ID, Version: worker.Version,
		StartedAt: worker.StartedAt, ObservedAt: worker.HeartbeatAt,
		Queues: append([]string(nil), worker.Queues...), Concurrency: worker.Concurrency,
		State: fleet.State(worker.State), CurrentJobs: worker.CurrentJobs,
		DrainStatus: fleet.DrainState(worker.DrainStatus), Backend: worker.Backend,
		Protocol: fleet.ProtocolVersion{
			Major: worker.Protocol.Major,
			Minor: worker.Protocol.Minor,
		},
		Capabilities: capabilities,
	}
}

func sortFleetWorkers(workers []fleet.WorkerSnapshot) {
	sort.Slice(workers, func(i, j int) bool {
		return workers[i].WorkerID < workers[j].WorkerID
	})
}
