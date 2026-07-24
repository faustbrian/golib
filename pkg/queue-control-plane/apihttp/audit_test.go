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
	"github.com/faustbrian/golib/pkg/queue-control-plane/history"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
)

func TestHandlerListsAuthorizedTenantAuditHistory(t *testing.T) {
	t.Parallel()

	previous := history.HashBytes([]byte("previous"))
	entry := history.Seal(previous, history.Event{
		Sequence: 5, OccurredAt: time.Unix(5, 0).UTC(), IdempotencyKey: "request-1",
		Actor: "operator-1", Action: "pause", Target: "queue:critical", Result: "succeeded",
	})
	source := &auditSourceStub{page: controlpostgres.AuditPage{Entries: []history.Entry{entry}, NextSequence: 5}}
	viewer := &viewerStub{}
	handler, err := NewHandler(Config{Commands: &commandExecutorStub{}, Audit: source, Viewer: viewer})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(t, http.MethodGet, "/v1/tenants/tenant-1/audit?after=4&limit=1", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	var page AuditPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Sequence != 5 || page.NextSequence != 5 ||
		page.Entries[0].Hash == "" || page.Entries[0].PreviousHash == "" {
		t.Fatalf("page = %+v, want sequence 5 with hashes", page)
	}
	if source.tenant != "tenant-1" || source.after != 4 || source.limit != 1 {
		t.Fatalf("ListTenant() = (%q, %d, %d)", source.tenant, source.after, source.limit)
	}
	if viewer.permission != controlplane.PermissionAuditView || viewer.target.Name != "audit" {
		t.Fatalf("Authorize() = %q %+v", viewer.permission, viewer.target)
	}
}

func TestHandlerRejectsUnauthorizedInvalidOrFailedAuditReads(t *testing.T) {
	t.Parallel()

	readErr := errors.New("audit unavailable")
	tests := map[string]struct {
		target        string
		authenticated bool
		viewerErr     error
		sourceErr     error
		status        int
	}{
		"unauthenticated":  {target: "/v1/tenants/tenant-1/audit", status: http.StatusUnauthorized},
		"denied":           {target: "/v1/tenants/tenant-1/audit", authenticated: true, viewerErr: authz.ErrDenied, status: http.StatusForbidden},
		"source":           {target: "/v1/tenants/tenant-1/audit", authenticated: true, sourceErr: readErr, status: http.StatusInternalServerError},
		"unknown query":    {target: "/v1/tenants/tenant-1/audit?search=x", authenticated: true, status: http.StatusBadRequest},
		"repeated query":   {target: "/v1/tenants/tenant-1/audit?after=1&after=2", authenticated: true, status: http.StatusBadRequest},
		"invalid cursor":   {target: "/v1/tenants/tenant-1/audit?after=bad", authenticated: true, status: http.StatusBadRequest},
		"empty cursor":     {target: "/v1/tenants/tenant-1/audit?after=", authenticated: true, status: http.StatusBadRequest},
		"zero limit":       {target: "/v1/tenants/tenant-1/audit?limit=0", authenticated: true, status: http.StatusBadRequest},
		"large limit":      {target: "/v1/tenants/tenant-1/audit?limit=1001", authenticated: true, status: http.StatusBadRequest},
		"invalid limit":    {target: "/v1/tenants/tenant-1/audit?limit=many", authenticated: true, status: http.StatusBadRequest},
		"oversized tenant": {target: "/v1/tenants/" + strings.Repeat("x", controlplane.MaxIdentityBytes+1) + "/audit", authenticated: true, status: http.StatusBadRequest},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := &auditSourceStub{err: tt.sourceErr}
			handler, err := NewHandler(Config{
				Commands: &commandExecutorStub{}, Audit: source, Viewer: &viewerStub{err: tt.viewerErr},
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
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.status, response.Body.String())
			}
			if tt.status != http.StatusOK && tt.sourceErr == nil && source.calls != 0 {
				t.Fatalf("ListTenant() calls = %d, want 0", source.calls)
			}
		})
	}
}

func TestHandlerUsesDefaultAuditPage(t *testing.T) {
	t.Parallel()

	source := &auditSourceStub{}
	handler, err := NewHandler(Config{Commands: &commandExecutorStub{}, Audit: source, Viewer: &viewerStub{}})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(t, http.MethodGet, "/v1/tenants/tenant-1/audit", ""))
	if response.Code != http.StatusOK || source.after != 0 || source.limit != defaultAuditPageSize {
		t.Fatalf("response = %d, query = (%d, %d)", response.Code, source.after, source.limit)
	}
}

type auditSourceStub struct {
	page   controlpostgres.AuditPage
	err    error
	tenant string
	after  uint64
	limit  uint32
	calls  int
}

func (s *auditSourceStub) ListTenant(_ context.Context, tenant string, after uint64, limit uint32) (controlpostgres.AuditPage, error) {
	s.calls++
	s.tenant = tenant
	s.after = after
	s.limit = limit
	return s.page, s.err
}
