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
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
)

func TestHandlerGetsAuthorizedTenantCommandResult(t *testing.T) {
	t.Parallel()

	want := controlplane.CommandResult{
		IdempotencyKey: "request-1", TenantID: "tenant-1",
		Status: controlplane.CommandSucceeded, CompletedAt: time.Unix(2, 0).UTC(),
	}
	source := &commandResultSourceStub{result: want}
	viewer := &viewerStub{}
	handler, err := NewHandler(Config{
		Commands: &commandExecutorStub{}, CommandResults: source, Viewer: viewer,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(t, http.MethodGet, "/v1/tenants/tenant-1/commands/request-1", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	var result controlplane.CommandResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || result != want {
		t.Fatalf("result = (%+v, %v), want %+v", result, err, want)
	}
	if source.tenant != "tenant-1" || source.key != "request-1" ||
		viewer.permission != controlplane.PermissionView || viewer.target.Name != "commands" {
		t.Fatalf("source/viewer = (%q, %q, %q, %+v)", source.tenant, source.key, viewer.permission, viewer.target)
	}
}

func TestHandlerListsAuthorizedTenantCommandHistory(t *testing.T) {
	t.Parallel()

	command := controlplane.Command{
		IdempotencyKey: "request-1",
		TenantID:       "tenant-1",
		Actor:          "operator-1",
		Reason:         "scheduled maintenance",
		Action:         controlplane.ActionPause,
		Target:         controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"},
		RequestedAt:    time.Unix(1, 0).UTC(),
	}
	result := controlplane.CommandResult{
		IdempotencyKey: command.IdempotencyKey,
		TenantID:       command.TenantID,
		Status:         controlplane.CommandSucceeded,
		CompletedAt:    time.Unix(2, 0).UTC(),
	}
	source := &commandResultSourceStub{page: controlpostgres.CommandPage{
		Records:    []controlpostgres.CommandRecord{{Command: command, Result: result}},
		NextCursor: "next-page",
	}}
	viewer := &viewerStub{}
	handler, err := NewHandler(Config{
		Commands: &commandExecutorStub{}, CommandResults: source, Viewer: viewer,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(
		t,
		http.MethodGet,
		"/v1/tenants/tenant-1/commands?cursor=current-page&limit=25",
		"",
	))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if source.tenant != "tenant-1" || source.cursor != "current-page" || source.limit != 25 ||
		viewer.permission != controlplane.PermissionView || viewer.target.Name != "commands" {
		t.Fatalf("source/viewer = (%q, %q, %d, %q, %+v)", source.tenant, source.cursor, source.limit, viewer.permission, viewer.target)
	}
	for _, want := range []string{`"idempotency_key":"request-1"`, `"actor":"operator-1"`, `"next_cursor":"next-page"`} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("body = %s, want %s", response.Body.String(), want)
		}
	}
}

func TestHandlerUsesDefaultBoundForCommandHistory(t *testing.T) {
	t.Parallel()

	source := &commandResultSourceStub{}
	handler, err := NewHandler(Config{
		Commands: &commandExecutorStub{}, CommandResults: source, Viewer: &viewerStub{},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(
		t, http.MethodGet, "/v1/tenants/tenant-1/commands", "",
	))

	if response.Code != http.StatusOK || source.limit != defaultCommandPageSize || source.cursor != "" {
		t.Fatalf("response/source = (%d, %d, %q)", response.Code, source.limit, source.cursor)
	}
}

func TestHandlerPreservesPointCommandSourcesWithoutHistoryCapability(t *testing.T) {
	t.Parallel()

	source := &pointCommandResultSourceStub{result: controlplane.CommandResult{
		IdempotencyKey: "request-1",
		TenantID:       "tenant-1",
		Status:         controlplane.CommandAccepted,
	}}
	handler, err := NewHandler(Config{
		Commands: &commandExecutorStub{}, CommandResults: source, Viewer: &viewerStub{},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, authenticatedRequest(
		t, http.MethodGet, "/v1/tenants/tenant-1/commands", "",
	))
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, authenticatedRequest(
		t, http.MethodGet, "/v1/tenants/tenant-1/commands/request-1", "",
	))

	if listResponse.Code != http.StatusMethodNotAllowed || getResponse.Code != http.StatusOK {
		t.Fatalf("statuses = list:%d get:%d", listResponse.Code, getResponse.Code)
	}
}

func TestHandlerRejectsUnauthorizedInvalidOrFailedCommandHistoryReads(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		target        string
		authenticated bool
		viewerErr     error
		sourceErr     error
		status        int
	}{
		"unauthenticated": {
			target: "/v1/tenants/tenant-1/commands", status: http.StatusUnauthorized,
		},
		"invalid tenant": {
			target:        "/v1/tenants/" + strings.Repeat("x", controlplane.MaxIdentityBytes+1) + "/commands",
			authenticated: true,
			status:        http.StatusBadRequest,
		},
		"denied": {
			target: "/v1/tenants/tenant-1/commands", authenticated: true,
			viewerErr: authz.ErrDenied, status: http.StatusForbidden,
		},
		"unknown query": {
			target: "/v1/tenants/tenant-1/commands?search=all", authenticated: true,
			status: http.StatusBadRequest,
		},
		"repeated query": {
			target: "/v1/tenants/tenant-1/commands?limit=1&limit=2", authenticated: true,
			status: http.StatusBadRequest,
		},
		"oversized cursor": {
			target:        "/v1/tenants/tenant-1/commands?cursor=" + strings.Repeat("x", controlpostgres.MaxCommandCursorBytes+1),
			authenticated: true,
			status:        http.StatusBadRequest,
		},
		"zero limit": {
			target: "/v1/tenants/tenant-1/commands?limit=0", authenticated: true,
			status: http.StatusBadRequest,
		},
		"large limit": {
			target: "/v1/tenants/tenant-1/commands?limit=1001", authenticated: true,
			status: http.StatusBadRequest,
		},
		"invalid limit": {
			target: "/v1/tenants/tenant-1/commands?limit=many", authenticated: true,
			status: http.StatusBadRequest,
		},
		"invalid cursor": {
			target: "/v1/tenants/tenant-1/commands?cursor=!", authenticated: true,
			sourceErr: controlpostgres.ErrInvalidCommandRequest, status: http.StatusBadRequest,
		},
		"source failure": {
			target: "/v1/tenants/tenant-1/commands", authenticated: true,
			sourceErr: errors.New("database unavailable"), status: http.StatusInternalServerError,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			handler, err := NewHandler(Config{
				Commands:       &commandExecutorStub{},
				CommandResults: &commandResultSourceStub{err: tt.sourceErr},
				Viewer:         &viewerStub{err: tt.viewerErr},
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
		})
	}
}

func TestPublicCommandRecordCopiesOptionalCommandDetails(t *testing.T) {
	t.Parallel()

	command := controlplane.Command{
		Selection: &controlplane.Selection{Limit: 25},
		Replay: &controlplane.Replay{
			Destination: "recovery", IdempotencyPolicy: controlplane.ReplayRejectDuplicate,
		},
		Scale: &controlplane.Scale{Replicas: 5},
	}
	entry := publicCommandRecord(controlpostgres.CommandRecord{Command: command})
	if entry.Selection == nil || entry.Selection.Limit != 25 ||
		entry.Replay == nil || entry.Replay.Destination != "recovery" ||
		entry.Replay.IdempotencyPolicy != controlplane.ReplayRejectDuplicate ||
		entry.Scale == nil || entry.Scale.Replicas != 5 {
		t.Fatalf("publicCommandRecord() = %+v", entry)
	}
}

func TestHandlerRejectsUnauthorizedInvalidOrFailedCommandReads(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		target        string
		authenticated bool
		viewerErr     error
		sourceErr     error
		status        int
	}{
		"unauthenticated":  {target: "/v1/tenants/tenant-1/commands/request-1", status: http.StatusUnauthorized},
		"oversized tenant": {target: "/v1/tenants/" + strings.Repeat("x", controlplane.MaxIdentityBytes+1) + "/commands/request-1", authenticated: true, status: http.StatusBadRequest},
		"oversized key":    {target: "/v1/tenants/tenant-1/commands/" + strings.Repeat("x", controlplane.MaxIdentityBytes+1), authenticated: true, status: http.StatusBadRequest},
		"denied":           {target: "/v1/tenants/tenant-1/commands/request-1", authenticated: true, viewerErr: authz.ErrDenied, status: http.StatusForbidden},
		"missing":          {target: "/v1/tenants/tenant-1/commands/request-1", authenticated: true, sourceErr: controlpostgres.ErrCommandNotFound, status: http.StatusNotFound},
		"database":         {target: "/v1/tenants/tenant-1/commands/request-1", authenticated: true, sourceErr: context.DeadlineExceeded, status: http.StatusInternalServerError},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := &commandResultSourceStub{err: tt.sourceErr}
			handler, err := NewHandler(Config{
				Commands: &commandExecutorStub{}, CommandResults: source,
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
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.status, response.Body.String())
			}
		})
	}
}

type commandResultSourceStub struct {
	result controlplane.CommandResult
	page   controlpostgres.CommandPage
	err    error
	tenant string
	key    string
	cursor string
	limit  uint32
	calls  int
}

type pointCommandResultSourceStub struct {
	result controlplane.CommandResult
}

func (s *pointCommandResultSourceStub) Get(
	context.Context,
	string,
	string,
) (controlplane.CommandResult, error) {
	return s.result, nil
}

func (s *commandResultSourceStub) Get(_ context.Context, tenant string, key string) (controlplane.CommandResult, error) {
	s.calls++
	s.tenant = tenant
	s.key = key
	return s.result, s.err
}

func (s *commandResultSourceStub) ListTenant(
	_ context.Context,
	tenant string,
	cursor string,
	limit uint32,
) (controlpostgres.CommandPage, error) {
	s.calls++
	s.tenant = tenant
	s.cursor = cursor
	s.limit = limit

	return s.page, s.err
}
