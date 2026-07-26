package apihttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/authz"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
)

func TestHandlerListsAuthorizedTenantWorkersWithBoundedCursor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	source := &workerSourceStub{snapshot: fleet.RegistrySnapshot{
		Workers: []fleet.WorkerSnapshot{
			{Heartbeat: workerHeartbeat("tenant-1", "worker-a", now.Add(-time.Second), []string{"critical"}), State: fleet.StateRunning},
			{Heartbeat: workerHeartbeat("tenant-1", "worker-b", now.Add(-time.Minute), []string{"critical"}), State: fleet.StateStale},
			{Heartbeat: workerHeartbeat("tenant-1", "worker-c", now.Add(-time.Second), []string{"default"}), State: fleet.StateRunning},
		},
		Rejected: 2,
	}}
	viewer := &viewerStub{}
	handler, err := NewHandler(Config{
		Commands:           &commandExecutorStub{},
		Workers:            source,
		Viewer:             viewer,
		Now:                func() time.Time { return now },
		StaleAfter:         30 * time.Second,
		Protocol:           fleet.ProtocolRange{Minimum: fleet.ProtocolVersion{Major: 1}, Maximum: fleet.ProtocolVersion{Major: 1, Minor: 2}},
		WorkerCapabilities: []fleet.Capability{fleet.CapabilityDrain, fleet.CapabilityPause},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	request := authenticatedRequest(t, http.MethodGet, "/v1/tenants/tenant-1/workers?queue=critical&limit=1", "")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	var page WorkerPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Workers) != 1 || page.Workers[0].WorkerID != "worker-a" || page.NextCursor != "worker-a" || page.Rejected != 2 {
		t.Fatalf("page = %+v, want worker-a and next cursor", page)
	}
	if page.Workers[0].Compatibility.State != fleet.CompatibilityCompatible ||
		len(page.Workers[0].Compatibility.Enabled) != 1 ||
		page.Workers[0].Compatibility.Enabled[0] != fleet.CapabilityDrain {
		t.Fatalf("compatibility = %+v, want compatible drain", page.Workers[0].Compatibility)
	}
	if source.tenant != "tenant-1" || source.now != now || source.staleAfter != 30*time.Second {
		t.Fatalf("SnapshotTenant() = (%q, %s, %s), want tenant-1 and configured clock", source.tenant, source.now, source.staleAfter)
	}
	if viewer.permission != controlplane.PermissionView || viewer.target.Name != "fleet" || viewer.actor != "operator-1" {
		t.Fatalf("Authorize() = permission %q target %+v actor %q", viewer.permission, viewer.target, viewer.actor)
	}

	next := authenticatedRequest(t, http.MethodGet, "/v1/tenants/tenant-1/workers?queue=critical&limit=1&after=worker-a", "")
	nextResponse := httptest.NewRecorder()
	handler.ServeHTTP(nextResponse, next)
	page = WorkerPage{}
	if err := json.NewDecoder(nextResponse.Body).Decode(&page); err != nil {
		t.Fatalf("decode next page: %v", err)
	}
	if len(page.Workers) != 1 || page.Workers[0].WorkerID != "worker-b" || page.Workers[0].State != fleet.StateStale || page.NextCursor != "" {
		t.Fatalf("next page = %+v, want stale worker-b without cursor", page)
	}
}

func TestHandlerFiltersWorkerState(t *testing.T) {
	t.Parallel()

	source := &workerSourceStub{snapshot: fleet.RegistrySnapshot{Workers: []fleet.WorkerSnapshot{
		{Heartbeat: fleet.Heartbeat{TenantID: "tenant-1", WorkerID: "worker-a"}, State: fleet.StateRunning},
		{Heartbeat: fleet.Heartbeat{TenantID: "tenant-1", WorkerID: "worker-b"}, State: fleet.StateUnknown},
	}}}
	handler, err := NewHandler(Config{Commands: &commandExecutorStub{}, Workers: source, Viewer: &viewerStub{}})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(t, http.MethodGet, "/v1/tenants/tenant-1/workers?state=unknown", ""))
	var page WorkerPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Workers) != 1 || page.Workers[0].WorkerID != "worker-b" {
		t.Fatalf("workers = %+v, want only worker-b", page.Workers)
	}
}

func TestHandlerRejectsUnauthorizedOrInvalidWorkerQueries(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		viewerErr error
		target    string
		status    int
	}{
		"denied": {
			viewerErr: authz.ErrDenied,
			target:    "/v1/tenants/tenant-1/workers",
			status:    http.StatusForbidden,
		},
		"invalid limit": {
			target: "/v1/tenants/tenant-1/workers?limit=1001",
			status: http.StatusBadRequest,
		},
		"malformed limit": {
			target: "/v1/tenants/tenant-1/workers?limit=many",
			status: http.StatusBadRequest,
		},
		"invalid state": {
			target: "/v1/tenants/tenant-1/workers?state=healthy",
			status: http.StatusBadRequest,
		},
		"unknown filter": {
			target: "/v1/tenants/tenant-1/workers?search=unbounded",
			status: http.StatusBadRequest,
		},
		"repeated filter": {
			target: "/v1/tenants/tenant-1/workers?queue=a&queue=b",
			status: http.StatusBadRequest,
		},
		"empty cursor": {
			target: "/v1/tenants/tenant-1/workers?after=",
			status: http.StatusBadRequest,
		},
		"empty queue": {
			target: "/v1/tenants/tenant-1/workers?queue=",
			status: http.StatusBadRequest,
		},
		"empty state": {
			target: "/v1/tenants/tenant-1/workers?state=",
			status: http.StatusBadRequest,
		},
		"oversized tenant": {
			target: "/v1/tenants/" + strings.Repeat("x", controlplane.MaxIdentityBytes+1) + "/workers",
			status: http.StatusBadRequest,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := &workerSourceStub{}
			handler, err := NewHandler(Config{
				Commands: &commandExecutorStub{},
				Workers:  source,
				Viewer:   &viewerStub{err: tt.viewerErr},
			})
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, authenticatedRequest(t, http.MethodGet, tt.target, ""))
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.status, response.Body.String())
			}
			if source.calls != 0 {
				t.Fatalf("SnapshotTenant() calls = %d, want 0", source.calls)
			}
		})
	}
}

func TestHandlerRequiresPrincipalForWorkerVisibility(t *testing.T) {
	t.Parallel()

	source := &workerSourceStub{}
	handler, err := NewHandler(Config{
		Commands: &commandExecutorStub{},
		Workers:  source,
		Viewer:   &viewerStub{},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-1/workers", nil))
	if response.Code != http.StatusUnauthorized || source.calls != 0 {
		t.Fatalf("response = %d, source calls = %d, want 401 and 0", response.Code, source.calls)
	}
}

func TestNewHandlerRequiresViewerForWorkerSource(t *testing.T) {
	t.Parallel()

	_, err := NewHandler(Config{Commands: &commandExecutorStub{}, Workers: &workerSourceStub{}})
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewHandler() error = %v, want ErrInvalidConfiguration", err)
	}
}

func TestHandlerUsesCancellableRemoteWorkerSourceAndFailsClosed(t *testing.T) {
	t.Parallel()

	readErr := errors.New("worker transport unavailable")
	source := &remoteWorkerSourceStub{err: readErr}
	handler, err := NewHandler(Config{
		Commands:      &commandExecutorStub{},
		RemoteWorkers: source,
		Viewer:        &viewerStub{},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	request := authenticatedRequest(t, http.MethodGet, "/v1/tenants/tenant-1/workers", "")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || source.ctx != request.Context() {
		t.Fatalf("response = %d, context forwarded = %t", response.Code, source.ctx == request.Context())
	}

	source.err = nil
	source.snapshot = fleet.RegistrySnapshot{Workers: []fleet.WorkerSnapshot{{
		Heartbeat: workerHeartbeat("tenant-1", "worker-1", time.Now(), []string{"critical"}),
		State:     fleet.StateRunning,
	}}}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
}

func workerHeartbeat(tenant string, worker string, observedAt time.Time, queues []string) fleet.Heartbeat {
	return fleet.Heartbeat{
		TenantID:     tenant,
		WorkerID:     worker,
		Version:      "1.0.0",
		StartedAt:    observedAt.Add(-time.Hour),
		ObservedAt:   observedAt,
		Queues:       queues,
		Concurrency:  4,
		State:        fleet.StateRunning,
		CurrentJobs:  1,
		DrainStatus:  "not_requested",
		Backend:      "redis",
		Protocol:     fleet.ProtocolVersion{Major: 1},
		Capabilities: []fleet.Capability{fleet.CapabilityDrain},
	}
}

type workerSourceStub struct {
	snapshot   fleet.RegistrySnapshot
	tenant     string
	now        time.Time
	staleAfter time.Duration
	calls      int
}

type remoteWorkerSourceStub struct {
	snapshot fleet.RegistrySnapshot
	err      error
	ctx      context.Context
}

func (s *remoteWorkerSourceStub) SnapshotTenant(
	ctx context.Context,
	_ string,
	_ time.Time,
	_ time.Duration,
) (fleet.RegistrySnapshot, error) {
	s.ctx = ctx

	return s.snapshot, s.err
}

func (s *workerSourceStub) SnapshotTenant(
	tenant string,
	now time.Time,
	staleAfter time.Duration,
) fleet.RegistrySnapshot {
	s.calls++
	s.tenant = tenant
	s.now = now
	s.staleAfter = staleAfter

	return s.snapshot
}

type viewerStub struct {
	err        error
	tenant     string
	actor      string
	permission controlplane.Permission
	target     controlplane.Target
}

func (s *viewerStub) Authorize(
	_ context.Context,
	tenant string,
	actor string,
	permission controlplane.Permission,
	target controlplane.Target,
) error {
	s.tenant = tenant
	s.actor = actor
	s.permission = permission
	s.target = target

	return s.err
}
