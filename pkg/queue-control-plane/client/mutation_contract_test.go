package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	controlkubernetes "github.com/faustbrian/golib/pkg/queue-control-plane/kubernetes"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestClientUsesExactDefaultTimeout(t *testing.T) {
	t.Parallel()

	client, err := New(Config{BaseURL: "https://example.test", Tokens: &tokenSourceStub{token: "token"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if client.httpClient.Timeout != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", client.httpClient.Timeout)
	}
}

func TestClientAcceptsExactQueryBounds(t *testing.T) {
	t.Parallel()

	tenant := strings.Repeat("t", controlplane.MaxIdentityBytes)
	tests := map[string]struct {
		wantQuery map[string]string
		call      func(*Client) error
	}{
		"commands": {
			wantQuery: map[string]string{"cursor": strings.Repeat("c", apihttp.MaxCommandCursorBytes), "limit": "1000"},
			call: func(client *Client) error {
				_, err := client.ListCommands(context.Background(), tenant, CommandQuery{Cursor: strings.Repeat("c", apihttp.MaxCommandCursorBytes), Limit: apihttp.MaxCommandPageSize})
				return err
			},
		},
		"workers": {
			wantQuery: map[string]string{"limit": "1000"},
			call: func(client *Client) error {
				_, err := client.ListWorkers(context.Background(), tenant, WorkerQuery{Limit: apihttp.MaxWorkerPageSize})
				return err
			},
		},
		"queues": {
			wantQuery: map[string]string{"cursor": strings.Repeat("c", queue.MaxCursorBytes), "limit": "200"},
			call: func(client *Client) error {
				_, err := client.ListQueues(context.Background(), tenant, QueueQuery{Cursor: strings.Repeat("c", queue.MaxCursorBytes), Limit: queue.MaxStatusPageSize})
				return err
			},
		},
		"workloads": {
			wantQuery: map[string]string{"continue": strings.Repeat("c", controlkubernetes.MaxContinueTokenBytes), "limit": "500"},
			call: func(client *Client) error {
				_, err := client.ListWorkloads(context.Background(), tenant, WorkloadQuery{Continue: strings.Repeat("c", controlkubernetes.MaxContinueTokenBytes), Limit: controlkubernetes.MaxPageSize})
				return err
			},
		},
		"audit": {
			wantQuery: map[string]string{"after": "1", "limit": "1000"},
			call: func(client *Client) error {
				_, err := client.ListAudit(context.Background(), tenant, AuditQuery{After: 1, Limit: apihttp.MaxAuditPageSize})
				return err
			},
		},
		"records": {
			wantQuery: map[string]string{
				"cursor": strings.Repeat("c", queue.MaxCursorBytes), "limit": "200",
				"search": strings.Repeat("s", queue.MaxSearchBytes), "sort": "attempts", "direction": "desc",
			},
			call: func(client *Client) error {
				_, err := client.ListFailures(context.Background(), tenant, RecordQuery{
					Cursor: strings.Repeat("c", queue.MaxCursorBytes), Limit: queue.MaxPageSize,
					Search: strings.Repeat("s", queue.MaxSearchBytes), Sort: queue.SortAttempts,
					Direction: queue.SortDescending,
				})
				return err
			},
		},
		"identity": {
			wantQuery: map[string]string{},
			call: func(client *Client) error {
				_, err := client.GetCommand(context.Background(), tenant, strings.Repeat("i", controlplane.MaxIdentityBytes))
				return err
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				for key, want := range test.wantQuery {
					if got := request.URL.Query().Get(key); got != want {
						t.Errorf("query %s = %q, want %q", key, got, want)
					}
				}
				_, _ = writer.Write([]byte("{}"))
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token"}})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if err := test.call(client); err != nil {
				t.Fatalf("call error = %v", err)
			}
		})
	}
}

func TestClientOmitsZeroValuedQueries(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.RawQuery != "" {
			t.Errorf("query = %q, want empty", request.URL.RawQuery)
		}
		_, _ = writer.Write([]byte("{}"))
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for name, call := range map[string]func() error{
		"commands": func() error {
			_, err := client.ListCommands(context.Background(), "tenant", CommandQuery{})
			return err
		},
		"workers": func() error { _, err := client.ListWorkers(context.Background(), "tenant", WorkerQuery{}); return err },
		"queues":  func() error { _, err := client.ListQueues(context.Background(), "tenant", QueueQuery{}); return err },
		"workloads": func() error {
			_, err := client.ListWorkloads(context.Background(), "tenant", WorkloadQuery{})
			return err
		},
		"audit":   func() error { _, err := client.ListAudit(context.Background(), "tenant", AuditQuery{}); return err },
		"records": func() error { _, err := client.ListFailures(context.Background(), "tenant", RecordQuery{}); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err != nil {
				t.Fatalf("call error = %v", err)
			}
		})
	}
}

func TestClientEncodesInspectionOptionsIndependently(t *testing.T) {
	t.Parallel()

	wants := []string{"payload=redacted", "diagnostics=revealed"}
	var call int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.RawQuery != wants[call] {
			t.Errorf("query = %q, want %q", request.URL.RawQuery, wants[call])
		}
		call++
		_, _ = writer.Write([]byte("{}"))
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := client.InspectFailure(context.Background(), "tenant", "failure", queue.PayloadRedacted); err != nil {
		t.Fatalf("InspectFailure() error = %v", err)
	}
	if _, err := client.InspectFailureWithOptions(context.Background(), "tenant", "failure", RecordInspectOptions{Payload: queue.PayloadHidden, RevealDiagnostics: true}); err != nil {
		t.Fatalf("InspectFailureWithOptions() error = %v", err)
	}
}

func TestClientEnforcesExactResponseAndStatusBoundaries(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		status   int
		body     string
		maxBytes int64
		wantErr  error
	}{
		"exact bytes":   {status: http.StatusOK, body: "{}", maxBytes: 2},
		"one byte over": {status: http.StatusOK, body: "{} ", maxBytes: 2, wantErr: ErrResponseTooLarge},
		"status 299":    {status: 299, body: "{}", maxBytes: 2},
		"status 300":    {status: http.StatusMultipleChoices, body: "{}", maxBytes: 2, wantErr: &APIError{Status: http.StatusMultipleChoices, Code: ""}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL, Tokens: &tokenSourceStub{token: "token"}, MaxResponseBytes: test.maxBytes})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, err = client.ListWorkers(context.Background(), "tenant", WorkerQuery{})
			if test.wantErr == nil && err != nil {
				t.Fatalf("ListWorkers() error = %v", err)
			}
			if errors.Is(test.wantErr, ErrResponseTooLarge) && !errors.Is(err, ErrResponseTooLarge) {
				t.Fatalf("ListWorkers() error = %v, want ErrResponseTooLarge", err)
			}
			var wantAPI *APIError
			if errors.As(test.wantErr, &wantAPI) {
				var gotAPI *APIError
				if !errors.As(err, &gotAPI) || gotAPI.Status != wantAPI.Status {
					t.Fatalf("ListWorkers() error = %v, want status %d", err, wantAPI.Status)
				}
			}
		})
	}
}
