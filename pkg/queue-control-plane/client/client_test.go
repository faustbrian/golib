package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	controlkubernetes "github.com/faustbrian/golib/pkg/queue-control-plane/kubernetes"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestClientExecutesTypedAuthenticatedCommand(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/tenants/tenant-1/commands" {
			t.Errorf("request = %s %s, want command endpoint", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer token-123" || request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("headers = %v, want bearer JSON", request.Header)
		}
		var input apihttp.CommandRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Errorf("decode command: %v", err)
		}
		if input.IdempotencyKey != "request-123" || input.Action != controlplane.ActionDrain {
			t.Errorf("command = %+v, want drain request-123", input)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(controlplane.CommandResult{
			IdempotencyKey: input.IdempotencyKey,
			TenantID:       "tenant-1",
			Status:         controlplane.CommandSucceeded,
			CompletedAt:    time.Unix(2, 0).UTC(),
		})
	}))
	defer server.Close()
	client, err := New(Config{
		BaseURL: server.URL,
		Tokens:  &tokenSourceStub{token: "token-123"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := client.ExecuteCommand(context.Background(), "tenant-1", apihttp.CommandRequest{
		IdempotencyKey: "request-123",
		Reason:         "maintenance",
		Action:         controlplane.ActionDrain,
		Target:         apihttp.TargetRequest(controlplane.Target{Kind: controlplane.TargetWorkerGroup, Name: "payments"}),
		RequestedAt:    time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("ExecuteCommand() error = %v", err)
	}
	if result.Status != controlplane.CommandSucceeded || result.TenantID != "tenant-1" {
		t.Fatalf("ExecuteCommand() = %+v, want tenant success", result)
	}
}

func TestClientExecutesRequestWithAPIKeyCredentials(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get(APIKeyIDHeader) != "operator-1" ||
			request.Header.Get(APIKeySecretHeader) != "secret-123" {
			t.Errorf("API key headers = %v", request.Header)
		}
		if request.Header.Get("Authorization") != "" {
			t.Errorf("Authorization = %q, want empty", request.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(writer).Encode(apihttp.WorkerPage{})
	}))
	defer server.Close()

	api, err := New(Config{
		BaseURL: server.URL,
		APIKeys: &apiKeySourceStub{id: "operator-1", secret: "secret-123"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := api.ListWorkers(context.Background(), "tenant-1", WorkerQuery{}); err != nil {
		t.Fatalf("ListWorkers() error = %v", err)
	}
}

func TestClientNeverForwardsCredentialsAcrossRedirects(t *testing.T) {
	t.Parallel()

	for name, credentials := range map[string]struct {
		tokens  TokenSource
		apiKeys APIKeySource
	}{
		"bearer token": {tokens: &tokenSourceStub{token: "token-123"}},
		"API key": {
			apiKeys: &apiKeySourceStub{id: "operator-1", secret: "secret-123"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var hostileCalls atomic.Int32
			hostile := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				hostileCalls.Add(1)
				if request.Header.Get("Authorization") != "" ||
					request.Header.Get(APIKeyIDHeader) != "" ||
					request.Header.Get(APIKeySecretHeader) != "" {
					t.Errorf("redirect leaked credentials: %v", request.Header)
				}
				_ = json.NewEncoder(writer).Encode(controlplane.CommandResult{
					IdempotencyKey: "request-1",
					TenantID:       "tenant-1",
					Status:         controlplane.CommandSucceeded,
					CompletedAt:    time.Unix(2, 0).UTC(),
				})
			}))
			defer hostile.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				http.Redirect(writer, request, hostile.URL, http.StatusTemporaryRedirect)
			}))
			defer origin.Close()

			transport := &http.Client{Timeout: time.Second}
			api, err := New(Config{
				BaseURL:    origin.URL,
				HTTPClient: transport,
				Tokens:     credentials.tokens,
				APIKeys:    credentials.apiKeys,
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if _, err := api.GetCommand(context.Background(), "tenant-1", "request-1"); err == nil {
				t.Fatal("GetCommand() error = nil, want redirect rejection")
			}
			if calls := hostileCalls.Load(); calls != 0 {
				t.Fatalf("hostile origin calls = %d, want 0", calls)
			}
			if transport.CheckRedirect != nil {
				t.Fatal("New() mutated the caller's HTTP client")
			}
		})
	}
}

func TestClientListsBoundedWorkerPage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.EscapedPath() != "/v1/tenants/tenant%20one/workers" {
			t.Errorf("path = %q, want escaped tenant", request.URL.EscapedPath())
		}
		query := request.URL.Query()
		if query.Get("limit") != "25" || query.Get("after") != "worker-a" ||
			query.Get("state") != "stale" || query.Get("queue") != "critical" {
			t.Errorf("query = %v, want bounded worker filters", query)
		}
		_ = json.NewEncoder(writer).Encode(apihttp.WorkerPage{
			Workers: []apihttp.Worker{{TenantID: "tenant one", WorkerID: "worker-b", State: fleet.StateStale}},
		})
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token-123"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	page, err := client.ListWorkers(context.Background(), "tenant one", WorkerQuery{
		Limit: 25,
		After: "worker-a",
		State: fleet.StateStale,
		Queue: "critical",
	})
	if err != nil {
		t.Fatalf("ListWorkers() error = %v", err)
	}
	if len(page.Workers) != 1 || page.Workers[0].WorkerID != "worker-b" {
		t.Fatalf("ListWorkers() = %+v, want worker-b", page)
	}
}

func TestClientListsBoundedQueuePage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/tenants/tenant-1/queues" ||
			request.URL.Query().Get("cursor") != "current" ||
			request.URL.Query().Get("limit") != "25" {
			t.Errorf("request = %s?%s", request.URL.Path, request.URL.RawQuery)
		}
		_ = json.NewEncoder(writer).Encode(apihttp.QueuePage{
			Queues: []apihttp.Queue{{Name: "critical"}}, NextCursor: "next",
		})
	}))
	defer server.Close()
	api, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token-123"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	page, err := api.ListQueues(context.Background(), "tenant-1", QueueQuery{
		Cursor: "current", Limit: 25,
	})
	if err != nil || len(page.Queues) != 1 || page.Queues[0].Name != "critical" ||
		page.NextCursor != "next" {
		t.Fatalf("ListQueues() = (%+v, %v)", page, err)
	}
}

func TestClientListsBoundedWorkloadPage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/tenants/tenant-1/workloads" ||
			request.URL.Query().Get("limit") != "25" ||
			request.URL.Query().Get("continue") != "current/page" {
			t.Errorf("request = %s?%s", request.URL.Path, request.URL.RawQuery)
		}
		_ = json.NewEncoder(writer).Encode(controlkubernetes.Page{
			Items:    []controlkubernetes.Status{{Name: "billing-workers"}},
			Continue: "next/page",
		})
	}))
	defer server.Close()
	api, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token-123"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	page, err := api.ListWorkloads(context.Background(), "tenant-1", WorkloadQuery{
		Limit: 25, Continue: "current/page",
	})
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "billing-workers" || page.Continue != "next/page" {
		t.Fatalf("ListWorkloads() = (%#v, %v)", page, err)
	}
}

func TestClientListsBoundedAuditPage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/tenants/tenant-1/audit" || request.URL.Query().Get("after") != "4" || request.URL.Query().Get("limit") != "25" {
			t.Errorf("request = %s?%s", request.URL.Path, request.URL.RawQuery)
		}
		_ = json.NewEncoder(writer).Encode(apihttp.AuditPage{NextSequence: 5})
	}))
	defer server.Close()
	api, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token-123"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	page, err := api.ListAudit(context.Background(), "tenant-1", AuditQuery{After: 4, Limit: 25})
	if err != nil || page.NextSequence != 5 {
		t.Fatalf("ListAudit() = (%+v, %v), want cursor 5", page, err)
	}
}

func TestClientListsAndInspectsQueueRecords(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/tenants/tenant-1/failures", "/v1/tenants/tenant-1/dead-letters":
			query := request.URL.Query()
			if query.Get("cursor") != "current" || query.Get("limit") != "25" ||
				query.Get("search") != "critical" || query.Get("sort") != "queue" ||
				query.Get("direction") != "asc" {
				t.Errorf("query = %v", query)
			}
			_ = json.NewEncoder(writer).Encode(apihttp.RecordPage{NextCursor: "next"})
		case "/v1/tenants/tenant-1/failures/failure-1":
			if request.URL.Query().Get("payload") != "revealed" ||
				request.URL.Query().Get("diagnostics") != "revealed" {
				t.Errorf("query = %v", request.URL.Query())
			}
			_ = json.NewEncoder(writer).Encode(apihttp.Record{ID: "failure-1"})
		case "/v1/tenants/tenant-1/dead-letters/dead-1":
			if request.URL.RawQuery != "" {
				t.Errorf("query = %q, want empty", request.URL.RawQuery)
			}
			_ = json.NewEncoder(writer).Encode(apihttp.Record{ID: "dead-1"})
		default:
			t.Errorf("unexpected path %q", request.URL.Path)
		}
	}))
	defer server.Close()
	api, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token-123"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	query := RecordQuery{
		Cursor: "current", Limit: 25, Search: "critical",
		Sort: queue.SortQueue, Direction: queue.SortAscending,
	}
	if page, err := api.ListFailures(context.Background(), "tenant-1", query); err != nil || page.NextCursor != "next" {
		t.Fatalf("ListFailures() = (%+v, %v)", page, err)
	}
	if page, err := api.ListDeadLetters(context.Background(), "tenant-1", query); err != nil || page.NextCursor != "next" {
		t.Fatalf("ListDeadLetters() = (%+v, %v)", page, err)
	}
	if record, err := api.InspectFailureWithOptions(
		context.Background(), "tenant-1", "failure-1", RecordInspectOptions{
			Payload: queue.PayloadRevealed, RevealDiagnostics: true,
		},
	); err != nil || record.ID != "failure-1" {
		t.Fatalf("InspectFailure() = (%+v, %v)", record, err)
	}
	if record, err := api.InspectDeadLetter(
		context.Background(), "tenant-1", "dead-1", queue.PayloadHidden,
	); err != nil || record.ID != "dead-1" {
		t.Fatalf("InspectDeadLetter() = (%+v, %v)", record, err)
	}
}

func TestClientGetsTenantCommandResult(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/tenants/tenant-1/commands/request-1" {
			t.Errorf("path = %q", request.URL.Path)
		}
		_ = json.NewEncoder(writer).Encode(controlplane.CommandResult{
			IdempotencyKey: "request-1", TenantID: "tenant-1", Status: controlplane.CommandAccepted,
		})
	}))
	defer server.Close()
	api, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token-123"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := api.GetCommand(context.Background(), "tenant-1", "request-1")
	if err != nil || result.Status != controlplane.CommandAccepted {
		t.Fatalf("GetCommand() = (%+v, %v)", result, err)
	}
}

func TestClientListsTenantCommandHistory(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/tenants/tenant-1/commands" ||
			request.URL.Query().Get("cursor") != "current-page" ||
			request.URL.Query().Get("limit") != "25" {
			t.Errorf("request = %s?%s", request.URL.Path, request.URL.RawQuery)
		}
		_ = json.NewEncoder(writer).Encode(apihttp.CommandHistoryPage{NextCursor: "next-page"})
	}))
	defer server.Close()
	api, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token-123"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	page, err := api.ListCommands(context.Background(), "tenant-1", CommandQuery{
		Cursor: "current-page", Limit: 25,
	})
	if err != nil || page.NextCursor != "next-page" {
		t.Fatalf("ListCommands() = (%+v, %v), want next-page", page, err)
	}
}

func TestClientListsCommandHistoryWithServerDefaults(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.RawQuery != "" {
			t.Errorf("query = %q, want empty", request.URL.RawQuery)
		}
		_ = json.NewEncoder(writer).Encode(apihttp.CommandHistoryPage{})
	}))
	defer server.Close()
	api, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token-123"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := api.ListCommands(context.Background(), "tenant-1", CommandQuery{}); err != nil {
		t.Fatalf("ListCommands() error = %v", err)
	}
}

func TestClientReturnsStableAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(writer).Encode(apihttp.Problem{Code: "forbidden"})
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token-123"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.ListWorkers(context.Background(), "tenant-1", WorkerQuery{})
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Status != http.StatusForbidden || apiError.Code != "forbidden" {
		t.Fatalf("ListWorkers() error = %v, want forbidden APIError", err)
	}
	if apiError.Error() != "control-plane API: status 403 code forbidden" {
		t.Fatalf("APIError.Error() = %q", apiError.Error())
	}
	if _, err := client.ListWorkloads(context.Background(), "tenant-1", WorkloadQuery{}); !errors.As(err, &apiError) {
		t.Fatalf("ListWorkloads() error = %v, want APIError", err)
	}
}

func TestClientRejectsInvalidWorkloadRequest(t *testing.T) {
	t.Parallel()

	api, err := New(Config{BaseURL: "https://example.test", Tokens: valueTokenSource{token: "token"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, input := range []struct {
		tenant string
		query  WorkloadQuery
	}{
		{},
		{tenant: "tenant-1", query: WorkloadQuery{Limit: -1}},
		{tenant: "tenant-1", query: WorkloadQuery{Limit: controlkubernetes.MaxPageSize + 1}},
		{tenant: "tenant-1", query: WorkloadQuery{Continue: strings.Repeat("x", controlkubernetes.MaxContinueTokenBytes+1)}},
	} {
		if _, err := api.ListWorkloads(context.Background(), input.tenant, input.query); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("ListWorkloads() error = %v, want ErrInvalidRequest", err)
		}
	}
}

func TestClientRejectsInvalidOperationScope(t *testing.T) {
	t.Parallel()

	client, err := New(Config{BaseURL: "https://example.test", Tokens: valueTokenSource{token: "token"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := client.ExecuteCommand(context.Background(), "", apihttp.CommandRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ExecuteCommand() error = %v, want ErrInvalidRequest", err)
	}
	for _, operation := range []func() error{
		func() error {
			_, err := client.ListQueues(context.Background(), "", QueueQuery{})
			return err
		},
		func() error {
			_, err := client.ListQueues(context.Background(), "tenant-1", QueueQuery{Limit: queue.MaxStatusPageSize + 1})
			return err
		},
		func() error {
			_, err := client.ListQueues(context.Background(), "tenant-1", QueueQuery{Cursor: strings.Repeat("x", queue.MaxCursorBytes+1)})
			return err
		},
		func() error {
			_, err := client.ListFailures(context.Background(), "", RecordQuery{})
			return err
		},
		func() error {
			_, err := client.ListDeadLetters(context.Background(), "tenant-1", RecordQuery{Limit: queue.MaxPageSize + 1})
			return err
		},
		func() error {
			_, err := client.ListFailures(context.Background(), "tenant-1", RecordQuery{Cursor: strings.Repeat("x", queue.MaxCursorBytes+1)})
			return err
		},
		func() error {
			_, err := client.ListFailures(context.Background(), "tenant-1", RecordQuery{Search: strings.Repeat("x", queue.MaxSearchBytes+1)})
			return err
		},
		func() error {
			_, err := client.ListFailures(context.Background(), "tenant-1", RecordQuery{Sort: queue.SortField("payload")})
			return err
		},
		func() error {
			_, err := client.ListFailures(context.Background(), "tenant-1", RecordQuery{Direction: queue.SortDirection("sideways")})
			return err
		},
		func() error {
			_, err := client.InspectFailure(context.Background(), "tenant-1", "", queue.PayloadHidden)
			return err
		},
		func() error {
			_, err := client.InspectDeadLetter(context.Background(), "tenant-1", "dead-1", queue.PayloadVisibility("raw"))
			return err
		},
		func() error {
			_, err := client.ListCommands(context.Background(), "", CommandQuery{})
			return err
		},
		func() error {
			_, err := client.ListCommands(context.Background(), "tenant-1", CommandQuery{Limit: apihttp.MaxCommandPageSize + 1})
			return err
		},
		func() error {
			_, err := client.ListCommands(context.Background(), "tenant-1", CommandQuery{Cursor: strings.Repeat("x", apihttp.MaxCommandCursorBytes+1)})
			return err
		},
		func() error {
			_, err := client.ListWorkers(context.Background(), "", WorkerQuery{})
			return err
		},
		func() error {
			_, err := client.ListWorkers(context.Background(), "tenant-1", WorkerQuery{Limit: apihttp.MaxWorkerPageSize + 1})
			return err
		},
		func() error {
			_, err := client.ListAudit(context.Background(), "", AuditQuery{})
			return err
		},
		func() error {
			_, err := client.ListAudit(context.Background(), "tenant-1", AuditQuery{Limit: apihttp.MaxAuditPageSize + 1})
			return err
		},
		func() error {
			_, err := client.GetCommand(context.Background(), "", "request-1")
			return err
		},
		func() error {
			_, err := client.GetCommand(context.Background(), "tenant-1", strings.Repeat("x", controlplane.MaxIdentityBytes+1))
			return err
		},
	} {
		if err := operation(); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("ListWorkers() error = %v, want ErrInvalidRequest", err)
		}
	}
}

func TestClientFailsClosedOnTokenTransportAndResponseErrors(t *testing.T) {
	t.Parallel()

	tokenErr := errors.New("token unavailable")
	transportErr := errors.New("transport unavailable")
	tests := map[string]struct {
		config  Config
		wantErr error
	}{
		"token": {
			config:  Config{BaseURL: "https://example.test", Tokens: &tokenSourceStub{err: tokenErr}},
			wantErr: tokenErr,
		},
		"transport": {
			config: Config{
				BaseURL: "https://example.test",
				Tokens:  &tokenSourceStub{token: "token"},
				HTTPClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
					return nil, transportErr
				})},
			},
			wantErr: transportErr,
		},
		"empty token": {
			config:  Config{BaseURL: "https://example.test", Tokens: &tokenSourceStub{}},
			wantErr: ErrInvalidToken,
		},
		"API key source": {
			config:  Config{BaseURL: "https://example.test", APIKeys: &apiKeySourceStub{err: tokenErr}},
			wantErr: tokenErr,
		},
		"empty API key": {
			config:  Config{BaseURL: "https://example.test", APIKeys: &apiKeySourceStub{id: "operator-1"}},
			wantErr: ErrInvalidAPIKey,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client, err := New(tt.config)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, err = client.ListWorkers(context.Background(), "tenant-1", WorkerQuery{})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ListWorkers() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestClientPropagatesCommandTransportFailure(t *testing.T) {
	t.Parallel()

	tokenErr := errors.New("token unavailable")
	client, err := New(Config{BaseURL: "https://example.test", Tokens: &tokenSourceStub{err: tokenErr}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = client.ExecuteCommand(context.Background(), "tenant-1", apihttp.CommandRequest{})
	if !errors.Is(err, tokenErr) {
		t.Fatalf("ExecuteCommand() error = %v, want %v", err, tokenErr)
	}
	_, err = client.ListAudit(context.Background(), "tenant-1", AuditQuery{})
	if !errors.Is(err, tokenErr) {
		t.Fatalf("ListAudit() error = %v, want %v", err, tokenErr)
	}
	_, err = client.ListCommands(context.Background(), "tenant-1", CommandQuery{})
	if !errors.Is(err, tokenErr) {
		t.Fatalf("ListCommands() error = %v, want %v", err, tokenErr)
	}
	_, err = client.GetCommand(context.Background(), "tenant-1", "request-1")
	if !errors.Is(err, tokenErr) {
		t.Fatalf("GetCommand() error = %v, want %v", err, tokenErr)
	}
	_, err = client.ListQueues(context.Background(), "tenant-1", QueueQuery{})
	if !errors.Is(err, tokenErr) {
		t.Fatalf("ListQueues() error = %v, want %v", err, tokenErr)
	}
	_, err = client.ListFailures(context.Background(), "tenant-1", RecordQuery{})
	if !errors.Is(err, tokenErr) {
		t.Fatalf("ListFailures() error = %v, want %v", err, tokenErr)
	}
	_, err = client.InspectFailure(
		context.Background(), "tenant-1", "failure-1", queue.PayloadHidden,
	)
	if !errors.Is(err, tokenErr) {
		t.Fatalf("InspectFailure() error = %v, want %v", err, tokenErr)
	}
}

func TestClientFailsOnRequestEncodingAndResponseRead(t *testing.T) {
	t.Parallel()

	t.Run("encoding", func(t *testing.T) {
		t.Parallel()

		client, err := New(Config{BaseURL: "https://example.test", Tokens: &tokenSourceStub{token: "token"}})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		err = client.do(context.Background(), http.MethodPost, client.baseURL, make(chan int), &struct{}{})
		if err == nil {
			t.Fatal("do() error = nil, want encoding failure")
		}
	})

	t.Run("request context", func(t *testing.T) {
		t.Parallel()

		client, err := New(Config{BaseURL: "https://example.test", Tokens: &tokenSourceStub{token: "token"}})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		var requestContext context.Context
		err = client.do(requestContext, http.MethodGet, client.baseURL, nil, &struct{}{})
		if err == nil {
			t.Fatal("do(nil context) error = nil, want request creation failure")
		}
	})

	t.Run("response read", func(t *testing.T) {
		t.Parallel()

		readErr := errors.New("read failed")
		client, err := New(Config{
			BaseURL: "https://example.test",
			Tokens:  &tokenSourceStub{token: "token"},
			HTTPClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       &errorReadCloser{err: readErr},
					Header:     make(http.Header),
				}, nil
			})},
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		_, err = client.ListWorkers(context.Background(), "tenant-1", WorkerQuery{})
		if !errors.Is(err, readErr) {
			t.Fatalf("ListWorkers() error = %v, want %v", err, readErr)
		}
	})
}

func TestClientUsesFallbackCodeForMalformedAPIProblem(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte("not-json"))
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = client.ListWorkers(context.Background(), "tenant-1", WorkerQuery{})
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Code != "http_error" {
		t.Fatalf("ListWorkers() error = %v, want fallback APIError", err)
	}
}

func TestClientBoundsAndValidatesResponses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
	}{
		"oversized": {body: strings.Repeat("x", 65)},
		"malformed": {body: `{not-json}`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(tt.body))
			}))
			defer server.Close()
			client, err := New(Config{
				BaseURL:          server.URL,
				Tokens:           &tokenSourceStub{token: "token"},
				MaxResponseBytes: 64,
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, err = client.ListWorkers(context.Background(), "tenant-1", WorkerQuery{})
			if err == nil {
				t.Fatal("ListWorkers() error = nil, want bounded decode failure")
			}
		})
	}
}

func TestNewClientRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	var typedNil *tokenSourceStub
	var typedNilAPIKey *apiKeySourceStub
	tests := []Config{
		{},
		{BaseURL: "https://example.test"},
		{BaseURL: "://invalid", Tokens: &tokenSourceStub{}},
		{BaseURL: "ftp://example.test", Tokens: &tokenSourceStub{}},
		{BaseURL: "https://user@example.test", Tokens: &tokenSourceStub{}},
		{BaseURL: "https://example.test/path", Tokens: &tokenSourceStub{}},
		{BaseURL: "https://example.test", Tokens: typedNil},
		{BaseURL: "https://example.test", APIKeys: typedNilAPIKey},
		{
			BaseURL: "https://example.test",
			Tokens:  &tokenSourceStub{},
			APIKeys: &apiKeySourceStub{},
		},
		{BaseURL: "https://example.test", Tokens: &tokenSourceStub{}, MaxResponseBytes: -1},
	}
	for _, config := range tests {
		if _, err := New(config); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("New() error = %v, want ErrInvalidConfiguration", err)
		}
	}
}

type tokenSourceStub struct {
	token string
	err   error
}

type apiKeySourceStub struct {
	id     string
	secret string
	err    error
}

func (s *apiKeySourceStub) APIKey(context.Context) (string, string, error) {
	return s.id, s.secret, s.err
}

type valueTokenSource struct {
	token string
}

func (s valueTokenSource) Token(context.Context) (string, error) {
	return s.token, nil
}

func (s *tokenSourceStub) Token(context.Context) (string, error) {
	return s.token, s.err
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type errorReadCloser struct {
	err error
}

func (r *errorReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (*errorReadCloser) Close() error {
	return nil
}
