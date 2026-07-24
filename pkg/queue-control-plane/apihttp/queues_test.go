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
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestHandlerListsAuthorizedQueueMeasurements(t *testing.T) {
	t.Parallel()

	status := queue.QueueStatus{
		Backend: "valkey-streams", Queue: "critical", ObservedAt: time.Unix(2, 0),
		Metrics: queue.QueueMetrics{
			Depth:            queue.Measurement[int64]{Value: 99},
			Lag:              queue.Measurement[int64]{Value: 3, Supported: true},
			Pending:          queue.Measurement[int64]{Value: 2, Supported: true},
			OldestAge:        queue.Measurement[time.Duration]{Value: 5 * time.Second, Supported: true},
			Throughput:       queue.Measurement[float64]{Value: 1.5, Supported: true},
			Runtime:          queue.Measurement[time.Duration]{Value: 250 * time.Millisecond},
			Succeeded:        queue.Measurement[uint64]{Value: 10, Supported: true},
			Failed:           queue.Measurement[uint64]{Value: 1, Supported: true},
			Retried:          queue.Measurement[uint64]{Value: 2, Supported: true},
			Reclaimed:        queue.Measurement[uint64]{Value: 3, Supported: true},
			DeadLettered:     queue.Measurement[uint64]{Value: 4, Supported: true},
			SettlementErrors: queue.Measurement[uint64]{Value: 5, Supported: true},
		},
	}
	source := &queueSourceStub{page: queue.QueueStatusPage{
		Items: []queue.QueueStatus{status}, NextCursor: "next",
	}}
	viewer := &viewerStub{}
	handler, err := NewHandler(Config{
		Commands: &commandExecutorStub{}, Queues: source, Viewer: viewer,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(
		t, http.MethodGet, "/v1/tenants/tenant-1/queues?cursor=current&limit=25", "",
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var page QueuePage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Queues) != 1 || page.NextCursor != "next" ||
		page.Queues[0].Metrics.Depth.Supported || page.Queues[0].Metrics.Depth.Value != 0 ||
		page.Queues[0].Metrics.Lag.Value != 3 ||
		page.Queues[0].Metrics.OldestAgeSeconds.Value != 5 ||
		page.Queues[0].Metrics.RuntimeSeconds.Supported ||
		page.Queues[0].Metrics.RuntimeSeconds.Value != 0 ||
		page.Queues[0].Metrics.SettlementErrors.Value != 5 {
		t.Fatalf("page = %+v", page)
	}
	if source.tenant != "tenant-1" || source.request != (queue.StatusPageRequest{
		Cursor: "current", Limit: 25,
	}) || viewer.permission != controlplane.PermissionView ||
		viewer.target != (controlplane.Target{Kind: controlplane.TargetQueue, Name: "queues"}) {
		t.Fatalf("source = %q %+v, authorization = %q %+v", source.tenant, source.request, viewer.permission, viewer.target)
	}
}

func TestHandlerRejectsUnsafeQueueReads(t *testing.T) {
	t.Parallel()

	readErr := errors.New("backend password=secret")
	tests := map[string]struct {
		target        string
		authenticated bool
		viewerErr     error
		sourceErr     error
		status        int
		wantCalls     int
	}{
		"unauthenticated": {target: "/v1/tenants/tenant-1/queues", status: http.StatusUnauthorized},
		"invalid tenant":  {target: "/v1/tenants/" + strings.Repeat("x", controlplane.MaxIdentityBytes+1) + "/queues", authenticated: true, status: http.StatusBadRequest},
		"denied":          {target: "/v1/tenants/tenant-1/queues", authenticated: true, viewerErr: authz.ErrDenied, status: http.StatusForbidden},
		"source":          {target: "/v1/tenants/tenant-1/queues", authenticated: true, sourceErr: readErr, status: http.StatusInternalServerError, wantCalls: 1},
		"unknown query":   {target: "/v1/tenants/tenant-1/queues?search=x", authenticated: true, status: http.StatusBadRequest},
		"repeated query":  {target: "/v1/tenants/tenant-1/queues?limit=1&limit=2", authenticated: true, status: http.StatusBadRequest},
		"empty cursor":    {target: "/v1/tenants/tenant-1/queues?cursor=", authenticated: true, status: http.StatusBadRequest},
		"large cursor":    {target: "/v1/tenants/tenant-1/queues?cursor=" + strings.Repeat("x", queue.MaxCursorBytes+1), authenticated: true, status: http.StatusBadRequest},
		"zero limit":      {target: "/v1/tenants/tenant-1/queues?limit=0", authenticated: true, status: http.StatusBadRequest},
		"large limit":     {target: "/v1/tenants/tenant-1/queues?limit=201", authenticated: true, status: http.StatusBadRequest},
		"invalid limit":   {target: "/v1/tenants/tenant-1/queues?limit=many", authenticated: true, status: http.StatusBadRequest},
		"empty limit":     {target: "/v1/tenants/tenant-1/queues?limit=", authenticated: true, status: http.StatusBadRequest},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := &queueSourceStub{err: tt.sourceErr}
			handler, err := NewHandler(Config{
				Commands: &commandExecutorStub{}, Queues: source, Viewer: &viewerStub{err: tt.viewerErr},
			})
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, tt.target, nil)
			if tt.authenticated {
				request = authenticatedRequest(t, http.MethodGet, tt.target, "")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.status || source.calls != tt.wantCalls ||
				strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("response = %d %s, calls %d", response.Code, response.Body.String(), source.calls)
			}
		})
	}
}

func TestHandlerDefaultsQueuePageAndRequiresViewer(t *testing.T) {
	t.Parallel()

	source := &queueSourceStub{}
	handler, err := NewHandler(Config{
		Commands: &commandExecutorStub{}, Queues: source, Viewer: &viewerStub{},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(
		t, http.MethodGet, "/v1/tenants/tenant-1/queues", "",
	))
	if response.Code != http.StatusOK || source.request.Limit != defaultQueuePageSize {
		t.Fatalf("response = %d, request = %+v", response.Code, source.request)
	}

	var typedNil *queueSourceStub
	for _, config := range []Config{
		{Commands: &commandExecutorStub{}, Queues: source},
		{Commands: &commandExecutorStub{}, Queues: typedNil, Viewer: &viewerStub{}},
	} {
		if _, err := NewHandler(config); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("NewHandler() error = %v", err)
		}
	}
}

type queueSourceStub struct {
	page    queue.QueueStatusPage
	err     error
	tenant  string
	request queue.StatusPageRequest
	calls   int
}

func (s *queueSourceStub) ListQueues(
	_ context.Context,
	tenant string,
	request queue.StatusPageRequest,
) (queue.QueueStatusPage, error) {
	s.calls++
	s.tenant = tenant
	s.request = request

	return s.page, s.err
}
