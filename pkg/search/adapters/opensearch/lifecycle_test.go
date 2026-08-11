package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestLifecycleImplementsCreateResumableReindexVerifyCutoverAndCleanup(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/events-v2":
			_, _ = io.WriteString(writer, `{"acknowledged":true,"shards_acknowledged":true,"index":"events-v2"}`)
		case "/events-v1":
			_, _ = io.WriteString(writer, `{"acknowledged":true}`)
		case "/_reindex":
			_, _ = io.WriteString(writer, `{"task":"node:task-1"}`)
		case "/_tasks/node:task-1":
			_, _ = io.WriteString(writer, `{"completed":true,"response":{"total":10,"created":10,"updated":0,"version_conflicts":0,"failures":[]}}`)
		case "/events-v1/_count", "/events-v2/_count":
			_, _ = io.WriteString(writer, `{"count":10,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0}}`)
		case "/_alias/events-read":
			_, _ = io.WriteString(writer, `{"events-v1":{"aliases":{"events-read":{}}}}`)
		case "/_aliases":
			_, _ = io.WriteString(writer, `{"acknowledged":true}`)
		default:
			http.Error(writer, "unexpected", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client, err := adapter.New(adapter.Config{Endpoints: []string{server.URL}, Transport: server.Client().Transport, TransportOwnership: adapter.TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 16 << 10,
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }),
			MutationGuard: adapter.LifecycleMutationGuardFunc(func(_ context.Context, _ adapter.LifecycleMutationRequest, operation func() error) error {
				return operation()
			}),
			CleanupGuard: adapter.LifecycleCleanupGuardFunc(func(_ context.Context, _ search.LifecycleCleanupRequest, operation func() error) error {
				return operation()
			}),
			ReindexCursorCodec: mustReindexCursorCodec(t),
			Verifier: adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
				return adapter.LifecycleVerificationResult{
					TargetFingerprint: request.ExpectedTargetFingerprint,
					Drift:             max(request.SourceCount, request.TargetCount) - min(request.SourceCount, request.TargetCount),
				}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	definition, _ := search.NewIndexDefinition("events-v2", json.RawMessage(`{"index":{"number_of_shards":1}}`), json.RawMessage(`{"properties":{"status":{"type":"keyword"}}}`), search.DefaultLimits())
	if err := client.CreateIndex(t.Context(), "tenant-a", definition); err != nil {
		t.Fatal(err)
	}
	cursor, done, err := client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", "")
	if err != nil || done || cursor == "" || cursor == "node:task-1" {
		t.Fatalf("start Reindex() = %q/%v/%v", cursor, done, err)
	}
	cursor, done, err = client.Reindex(t.Context(), "tenant-a", "events-v1", "events-v2", cursor)
	if err != nil || !done || cursor == "" || cursor == "node:task-1" {
		t.Fatalf("poll Reindex() = %q/%v/%v", cursor, done, err)
	}
	report, err := client.VerifyIndex(t.Context(), "tenant-a", "events-v1", "events-v2", "definition-v2")
	if err != nil || !report.Verified || report.SourceCount != 10 || report.TargetCount != 10 {
		t.Fatalf("VerifyIndex() = %#v/%v", report, err)
	}
	current, err := client.ResolveAlias(t.Context(), "tenant-a", "events-read")
	if err != nil || current != "events-v1" {
		t.Fatalf("ResolveAlias() = %q/%v", current, err)
	}
	if err := client.SwapAlias(t.Context(), "tenant-a", "events-read", "events-v1", "events-v2"); err != nil {
		t.Fatal(err)
	}
	if err := client.CleanupIndex(t.Context(), search.LifecycleCleanupRequest{
		MigrationID: "migration", Tenant: "tenant-a", Alias: "events-read",
		ActiveIndex: "events-v2", ActiveFingerprint: "definition-v2",
		InactiveIndex: "events-v1", InactiveFingerprint: "definition-v1",
	}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"PUT /events-v2", "POST /_reindex?refresh=true&wait_for_completion=false", "GET /_tasks/node:task-1", "GET /events-v1/_count", "GET /events-v2/_count", "GET /_alias/events-read", "POST /_aliases", "DELETE /events-v1"}
	if fmt.Sprint(requests) != fmt.Sprint(want) {
		t.Fatalf("requests = %q, want %q", requests, want)
	}
}

func TestLifecycleRequiresAuthorizationBeforeNetworkAccess(t *testing.T) {
	t.Parallel()
	called := false
	client, err := adapter.New(adapter.Config{
		Endpoints:          []string{"https://search.example.test"},
		Transport:          roundTripFunc(func(*http.Request) (*http.Response, error) { called = true; return nil, errors.New("unexpected") }),
		TransportOwnership: adapter.TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return errors.New("private authorization detail") })},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.CleanupIndex(t.Context(), search.LifecycleCleanupRequest{
		MigrationID: "migration", Tenant: "tenant-a", Alias: "events-read",
		ActiveIndex: "events-v2", ActiveFingerprint: "definition-v2",
		InactiveIndex: "events-v1", InactiveFingerprint: "definition-v1",
	}); !errors.Is(err, adapter.ErrLifecycleDenied) || strings.Contains(err.Error(), "private") {
		t.Fatalf("CleanupIndex() error = %v", err)
	}
	if called {
		t.Fatal("denied lifecycle operation reached transport")
	}
}

func TestAddAliasCreatesAnAuthorizedReadWriteBoundary(t *testing.T) {
	t.Parallel()
	var body map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewDecoder(request.Body).Decode(&body)
		_, _ = io.WriteString(writer, `{"acknowledged":true}`)
	}))
	t.Cleanup(server.Close)
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{server.URL}, Transport: server.Client().Transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }),
			MutationGuard: adapter.LifecycleMutationGuardFunc(func(_ context.Context, _ adapter.LifecycleMutationRequest, operation func() error) error {
				return operation()
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.AddAlias(t.Context(), "tenant-a", "events-write", "events-v1", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["actions"]; !ok {
		t.Fatalf("alias body = %#v", body)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
