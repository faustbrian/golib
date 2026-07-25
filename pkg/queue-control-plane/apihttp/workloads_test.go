package apihttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/authz"
	controlkubernetes "github.com/faustbrian/golib/pkg/queue-control-plane/kubernetes"
)

func TestHandlerListsAuthorizedTenantWorkloads(t *testing.T) {
	t.Parallel()

	source := &workloadSourceStub{page: controlkubernetes.Page{
		Items: []controlkubernetes.Status{{
			Namespace:       "tenant-1-queues",
			Name:            "billing-workers",
			DesiredReplicas: 5,
			ReadyReplicas:   4,
		}},
		Continue:  "next-page",
		Remaining: 2,
	}}
	viewer := &viewerStub{}
	handler, err := NewHandler(Config{
		Commands:  &commandExecutorStub{},
		Workloads: source,
		Viewer:    viewer,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	request := authenticatedRequest(
		t,
		http.MethodGet,
		"/v1/tenants/tenant-1/workloads?limit=25&continue=current-page",
		"",
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"items"`) {
		t.Fatalf("body = %s, want stable JSON field names", response.Body.String())
	}
	var page controlkubernetes.Page
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Name != "billing-workers" || page.Continue != "next-page" {
		t.Fatalf("page = %#v", page)
	}
	if source.tenant != "tenant-1" || source.limit != 25 || source.continueToken != "current-page" {
		t.Fatalf("ListTenantWorkloads() = (%q, %d, %q)", source.tenant, source.limit, source.continueToken)
	}
	if viewer.permission != controlplane.PermissionView ||
		viewer.target != (controlplane.Target{Kind: controlplane.TargetWorkload, Name: "kubernetes"}) {
		t.Fatalf("Authorize() = permission %q target %#v", viewer.permission, viewer.target)
	}
}

func TestHandlerRejectsUnauthorizedOrInvalidWorkloadQueries(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		target        string
		authenticated bool
		viewerErr     error
		sourceErr     error
		status        int
		wantCalls     int
	}{
		"missing principal": {
			target: "/v1/tenants/tenant-1/workloads", status: http.StatusUnauthorized,
		},
		"invalid tenant": {
			target:        "/v1/tenants/" + strings.Repeat("x", controlplane.MaxIdentityBytes+1) + "/workloads",
			authenticated: true, status: http.StatusBadRequest,
		},
		"denied": {
			target: "/v1/tenants/tenant-1/workloads", authenticated: true,
			viewerErr: authz.ErrDenied, status: http.StatusForbidden,
		},
		"malformed limit": {
			target: "/v1/tenants/tenant-1/workloads?limit=many", authenticated: true, status: http.StatusBadRequest,
		},
		"zero limit": {
			target: "/v1/tenants/tenant-1/workloads?limit=0", authenticated: true, status: http.StatusBadRequest,
		},
		"oversized limit": {
			target: "/v1/tenants/tenant-1/workloads?limit=501", authenticated: true, status: http.StatusBadRequest,
		},
		"unknown filter": {
			target: "/v1/tenants/tenant-1/workloads?watch=true", authenticated: true, status: http.StatusBadRequest,
		},
		"repeated filter": {
			target: "/v1/tenants/tenant-1/workloads?limit=1&limit=2", authenticated: true, status: http.StatusBadRequest,
		},
		"empty continuation": {
			target: "/v1/tenants/tenant-1/workloads?continue=", authenticated: true, status: http.StatusBadRequest,
		},
		"oversized continuation": {
			target:        "/v1/tenants/tenant-1/workloads?continue=" + strings.Repeat("x", controlkubernetes.MaxContinueTokenBytes+1),
			authenticated: true, status: http.StatusBadRequest,
		},
		"source failure": {
			target: "/v1/tenants/tenant-1/workloads", authenticated: true,
			sourceErr: errors.New("cluster token=secret"), status: http.StatusInternalServerError, wantCalls: 1,
		},
	}

	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := &workloadSourceStub{err: test.sourceErr}
			handler, err := NewHandler(Config{
				Commands:  &commandExecutorStub{},
				Workloads: source,
				Viewer:    &viewerStub{err: test.viewerErr},
			})
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			if test.authenticated {
				request = authenticatedRequest(t, http.MethodGet, test.target, "")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || source.calls != test.wantCalls {
				t.Fatalf("response = %d, source calls = %d, want %d and %d", response.Code, source.calls, test.status, test.wantCalls)
			}
			if strings.Contains(response.Body.String(), "token=secret") {
				t.Fatalf("response leaked source error: %s", response.Body.String())
			}
		})
	}
}

func TestHandlerUsesDefaultWorkloadPageAndRequiresViewer(t *testing.T) {
	t.Parallel()

	source := &workloadSourceStub{page: controlkubernetes.Page{Items: []controlkubernetes.Status{}}}
	handler, err := NewHandler(Config{
		Commands:  &commandExecutorStub{},
		Workloads: source,
		Viewer:    &viewerStub{},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(t, http.MethodGet, "/v1/tenants/tenant-1/workloads", ""))
	if response.Code != http.StatusOK || source.limit != defaultWorkloadPageSize {
		t.Fatalf("response = %d, limit = %d", response.Code, source.limit)
	}

	if _, err := NewHandler(Config{Commands: &commandExecutorStub{}, Workloads: source}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewHandler() error = %v, want ErrInvalidConfiguration", err)
	}
	var nilSource *workloadSourceStub
	if _, err := NewHandler(Config{
		Commands: &commandExecutorStub{}, Workloads: nilSource, Viewer: &viewerStub{},
	}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewHandler(typed nil) error = %v, want ErrInvalidConfiguration", err)
	}
}

type workloadSourceStub struct {
	page          controlkubernetes.Page
	err           error
	tenant        string
	limit         int64
	continueToken string
	calls         int
}

func (source *workloadSourceStub) ListTenantWorkloads(
	_ context.Context,
	tenant string,
	limit int64,
	continueToken string,
) (controlkubernetes.Page, error) {
	source.calls++
	source.tenant = tenant
	source.limit = limit
	source.continueToken = continueToken

	return source.page, source.err
}
