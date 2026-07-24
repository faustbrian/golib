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
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestHandlerGetsAuthorizedDesiredStateForWorkerConvergence(t *testing.T) {
	t.Parallel()

	changedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	source := &desiredStateSourceStub{record: control.DesiredRecord{
		TenantID: "tenant-1",
		Target:   controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"},
		State:    control.DesiredPaused, Revision: 3,
		ChangedAt: changedAt, CommandKey: "pause-critical-3",
	}}
	viewer := &viewerStub{}
	handler, err := NewHandler(Config{
		Commands: &commandExecutorStub{}, DesiredState: source, Viewer: viewer,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(
		t, http.MethodGet,
		"/v1/tenants/tenant-1/desired-state/queue/critical", "",
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var record queue.DesiredRecord
	if err := json.NewDecoder(response.Body).Decode(&record); err != nil {
		t.Fatalf("decode desired state: %v", err)
	}
	want := queue.DesiredRecord{
		Target: queue.Target{Kind: queue.TargetQueue, Name: "critical"},
		State:  queue.DesiredPaused, Revision: 3,
		ChangedAt: changedAt, CommandID: "pause-critical-3",
	}
	if record != want || source.tenant != "tenant-1" ||
		source.target != (controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"}) ||
		viewer.permission != controlplane.PermissionView || viewer.target != source.target {
		t.Fatalf("record = %+v, source = %q %+v, authorization = %q %+v", record, source.tenant, source.target, viewer.permission, viewer.target)
	}
}

func TestHandlerRejectsUnsafeDesiredStateReads(t *testing.T) {
	t.Parallel()

	readErr := errors.New("database password=secret")
	tests := map[string]struct {
		target        string
		authenticated bool
		viewerErr     error
		sourceErr     error
		status        int
		wantCalls     int
	}{
		"unauthenticated": {target: "/v1/tenants/tenant-1/desired-state/queue/critical", status: http.StatusUnauthorized},
		"invalid tenant":  {target: "/v1/tenants/" + strings.Repeat("x", controlplane.MaxIdentityBytes+1) + "/desired-state/queue/critical", authenticated: true, status: http.StatusBadRequest},
		"invalid kind":    {target: "/v1/tenants/tenant-1/desired-state/failure/critical", authenticated: true, status: http.StatusBadRequest},
		"invalid name":    {target: "/v1/tenants/tenant-1/desired-state/queue/%20", authenticated: true, status: http.StatusBadRequest},
		"unknown query":   {target: "/v1/tenants/tenant-1/desired-state/queue/critical?watch=true", authenticated: true, status: http.StatusBadRequest},
		"denied":          {target: "/v1/tenants/tenant-1/desired-state/queue/critical", authenticated: true, viewerErr: authz.ErrDenied, status: http.StatusForbidden},
		"missing":         {target: "/v1/tenants/tenant-1/desired-state/queue/critical", authenticated: true, sourceErr: controlpostgres.ErrDesiredStateNotFound, status: http.StatusNotFound, wantCalls: 1},
		"source":          {target: "/v1/tenants/tenant-1/desired-state/queue/critical", authenticated: true, sourceErr: readErr, status: http.StatusInternalServerError, wantCalls: 1},
		"invalid output":  {target: "/v1/tenants/tenant-1/desired-state/queue/critical", authenticated: true, status: http.StatusInternalServerError, wantCalls: 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := &desiredStateSourceStub{err: tt.sourceErr}
			handler, err := NewHandler(Config{
				Commands: &commandExecutorStub{}, DesiredState: source,
				Viewer: &viewerStub{err: tt.viewerErr},
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
				t.Fatalf("response = %d %s, calls = %d", response.Code, response.Body.String(), source.calls)
			}
		})
	}
}

func TestNewHandlerRequiresViewerForDesiredState(t *testing.T) {
	t.Parallel()

	source := &desiredStateSourceStub{}
	var typedNil *desiredStateSourceStub
	for _, config := range []Config{
		{Commands: &commandExecutorStub{}, DesiredState: source},
		{Commands: &commandExecutorStub{}, DesiredState: typedNil, Viewer: &viewerStub{}},
	} {
		if _, err := NewHandler(config); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("NewHandler() error = %v", err)
		}
	}
}

type desiredStateSourceStub struct {
	record control.DesiredRecord
	err    error
	tenant string
	target controlplane.Target
	calls  int
}

func (s *desiredStateSourceStub) Get(
	_ context.Context,
	tenant string,
	target controlplane.Target,
) (control.DesiredRecord, error) {
	s.calls++
	s.tenant = tenant
	s.target = target

	return s.record, s.err
}
