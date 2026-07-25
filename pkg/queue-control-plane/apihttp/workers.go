package apihttp

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
)

const (
	defaultWorkerPageSize uint32 = 100
	MaxWorkerPageSize     uint32 = 1_000
)

// WorkerPage is one deterministic bounded worker result page.
type WorkerPage struct {
	Workers    []Worker `json:"workers"`
	Rejected   uint64   `json:"rejected"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// Worker is the public tenant-scoped worker status representation.
type Worker struct {
	TenantID      string                `json:"tenant_id"`
	WorkerID      string                `json:"worker_id"`
	Version       string                `json:"version"`
	StartedAt     time.Time             `json:"started_at"`
	ObservedAt    time.Time             `json:"observed_at"`
	Queues        []string              `json:"queues"`
	Concurrency   uint32                `json:"concurrency"`
	State         fleet.State           `json:"state"`
	CurrentJobs   uint32                `json:"current_jobs"`
	DrainStatus   fleet.DrainState      `json:"drain_status"`
	Backend       string                `json:"backend"`
	Protocol      fleet.ProtocolVersion `json:"protocol"`
	Capabilities  []fleet.Capability    `json:"capabilities"`
	Compatibility fleet.Compatibility   `json:"compatibility"`
}

type workerQuery struct {
	limit uint32
	after string
	state fleet.State
	queue string
}

func (h *handler) listWorkers(writer http.ResponseWriter, request *http.Request) {
	principal, ok := authentication.PrincipalFromContext(request.Context())
	if !ok || principal.IsAnonymous() {
		writeProblem(writer, http.StatusUnauthorized, "unauthenticated")
		return
	}
	tenant := request.PathValue("tenant")
	if !validIdentity(tenant) {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := h.viewer.Authorize(
		request.Context(),
		tenant,
		principal.Subject(),
		controlplane.PermissionView,
		controlplane.Target{Kind: controlplane.TargetWorkload, Name: "fleet"},
	); err != nil {
		writeCommandError(writer, err)
		return
	}

	query, err := parseWorkerQuery(request.URL.Query())
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var snapshot fleet.RegistrySnapshot
	if h.remoteWorkers != nil {
		snapshot, err = h.remoteWorkers.SnapshotTenant(
			request.Context(), tenant, h.now(), h.staleAfter,
		)
		if err != nil {
			writeProblem(writer, http.StatusServiceUnavailable, "worker_status_unavailable")
			return
		}
	} else {
		snapshot = h.workers.SnapshotTenant(tenant, h.now(), h.staleAfter)
	}
	workers := append([]fleet.WorkerSnapshot(nil), snapshot.Workers...)
	sort.Slice(workers, func(i, j int) bool {
		return workers[i].WorkerID < workers[j].WorkerID
	})

	page := WorkerPage{Workers: make([]Worker, 0, query.limit), Rejected: snapshot.Rejected}
	for _, snapshot := range workers {
		if !query.matches(snapshot) {
			continue
		}
		if len(page.Workers) == int(query.limit) {
			page.NextCursor = page.Workers[len(page.Workers)-1].WorkerID
			break
		}
		page.Workers = append(page.Workers, h.worker(snapshot))
	}

	writeJSON(writer, http.StatusOK, page)
}

func (h *handler) worker(snapshot fleet.WorkerSnapshot) Worker {
	return Worker{
		TenantID:     snapshot.TenantID,
		WorkerID:     snapshot.WorkerID,
		Version:      snapshot.Version,
		StartedAt:    snapshot.StartedAt,
		ObservedAt:   snapshot.ObservedAt,
		Queues:       append([]string(nil), snapshot.Queues...),
		Concurrency:  snapshot.Concurrency,
		State:        snapshot.State,
		CurrentJobs:  snapshot.CurrentJobs,
		DrainStatus:  snapshot.DrainStatus,
		Backend:      snapshot.Backend,
		Protocol:     snapshot.Protocol,
		Capabilities: append([]fleet.Capability(nil), snapshot.Capabilities...),
		Compatibility: fleet.Negotiate(
			h.protocol,
			snapshot.Protocol,
			snapshot.Capabilities,
			h.workerCapabilities,
		),
	}
}

func parseWorkerQuery(values url.Values) (workerQuery, error) {
	query := workerQuery{limit: defaultWorkerPageSize}
	allowed := map[string]bool{"limit": true, "after": true, "state": true, "queue": true}
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return workerQuery{}, ErrInvalidConfiguration
		}
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || limit == 0 || limit > uint64(MaxWorkerPageSize) {
			return workerQuery{}, ErrInvalidConfiguration
		}
		query.limit = uint32(limit)
	}
	query.after = values.Get("after")
	if _, exists := values["after"]; exists && strings.TrimSpace(query.after) == "" {
		return workerQuery{}, ErrInvalidConfiguration
	}
	query.queue = values.Get("queue")
	if _, exists := values["queue"]; exists && strings.TrimSpace(query.queue) == "" {
		return workerQuery{}, ErrInvalidConfiguration
	}
	if raw := values.Get("state"); raw != "" {
		query.state = fleet.State(raw)
		if !publicWorkerState(query.state) {
			return workerQuery{}, ErrInvalidConfiguration
		}
	} else if _, exists := values["state"]; exists {
		return workerQuery{}, ErrInvalidConfiguration
	}

	return query, nil
}

func (q workerQuery) matches(snapshot fleet.WorkerSnapshot) bool {
	if snapshot.WorkerID <= q.after || (q.state != "" && snapshot.State != q.state) {
		return false
	}
	if q.queue == "" {
		return true
	}
	for _, queue := range snapshot.Queues {
		if queue == q.queue {
			return true
		}
	}

	return false
}

func publicWorkerState(state fleet.State) bool {
	switch state {
	case fleet.StateRunning,
		fleet.StatePaused,
		fleet.StateDraining,
		fleet.StateStopped,
		fleet.StateStale,
		fleet.StateUnknown:
		return true
	default:
		return false
	}
}

func validIdentity(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= controlplane.MaxIdentityBytes
}
