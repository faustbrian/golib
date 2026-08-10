package opensearch_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestWriteUsesExternalVersioningForSupportedDocumentActions(t *testing.T) {
	t.Parallel()

	type observed struct{ method, target, body string }
	requests := make(chan observed, 3)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests <- observed{method: request.Method, target: request.URL.RequestURI(), body: string(body)}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"_index":"tenant-a-events-v2","_id":"event-1","_version":9,"result":"updated"}`)
	}))
	t.Cleanup(server.Close)
	client := newWriteClient(t, server.URL, server.Client().Transport)
	limits := search.DefaultLimits()
	document, _ := search.NewDocument("tenant-a", "events", "event-1", 9, json.RawMessage(`{"value":"new"}`), limits)

	tests := []struct {
		operation    search.WriteOperation
		method, path string
		hasBody      bool
	}{
		{search.IndexDocument(document), http.MethodPut, "/tenant-a-events-v2/_doc/event-1?refresh=wait_for&require_alias=true&version=9&version_type=external", true},
		{search.UpsertDocument(document), http.MethodPut, "/tenant-a-events-v2/_doc/event-1?refresh=wait_for&require_alias=true&version=9&version_type=external", true},
		{search.DeleteDocument("tenant-a", "events", "event-1", 9), http.MethodDelete, "/tenant-a-events-v2/_doc/event-1?refresh=wait_for&version=9&version_type=external", false},
	}
	for _, test := range tests {
		outcome, err := client.Write(t.Context(), test.operation, search.RefreshWaitFor)
		if err != nil {
			t.Fatalf("Write(%s) error = %v", test.operation.Action, err)
		}
		if outcome.Action != test.operation.Action || outcome.State != search.OutcomeApplied || outcome.Version != 9 {
			t.Fatalf("Write(%s) = %#v", test.operation.Action, outcome)
		}
		got := <-requests
		if got.method != test.method || got.target != test.path || (got.body != "") != test.hasBody || test.hasBody && !strings.Contains(got.body, `"value":"new"`) {
			t.Fatalf("Write(%s) request = %#v", test.operation.Action, got)
		}
	}
}

func TestWriteAndBulkRejectUpdateExistingBeforeIO(t *testing.T) {
	t.Parallel()

	transport := &observedTransport{err: errors.New("unexpected request")}
	client := newWriteClient(t, "https://search.example.test", transport)
	capabilities, err := client.Capabilities(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.UpdateExisting {
		t.Fatal("Capabilities().UpdateExisting = true, want false")
	}
	document, err := search.NewDocument("tenant-a", "events", "event-1", 9, json.RawMessage(`{"value":"new"}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	operation := search.UpdateDocument(document)

	if _, err := client.Write(t.Context(), operation, search.RefreshNone); !errors.Is(err, search.ErrUnsupported) {
		t.Fatalf("Write(update) error = %v, want ErrUnsupported", err)
	}
	if _, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{operation}, Refresh: search.RefreshNone}); !errors.Is(err, search.ErrUnsupported) {
		t.Fatalf("Bulk(update) error = %v, want ErrUnsupported", err)
	}
	if transport.requests != 0 {
		t.Fatalf("update requests = %d, want 0", transport.requests)
	}
}

func TestWriteDeleteOmitsUnsupportedRequireAliasParameter(t *testing.T) {
	t.Parallel()

	type observed struct{ method, target string }
	requests := make(chan observed, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- observed{method: request.Method, target: request.URL.RequestURI()}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Query().Has("require_alias") {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":{"type":"illegal_argument_exception"}}`)
			return
		}
		_, _ = io.WriteString(writer, `{"_index":"tenant-a-events-v2","_id":"event-1","_version":9,"result":"deleted"}`)
	}))
	t.Cleanup(server.Close)
	client := newWriteClient(t, server.URL, server.Client().Transport)

	outcome, err := client.Write(t.Context(), search.DeleteDocument("tenant-a", "events", "event-1", 9), search.RefreshWaitFor)
	if err != nil || outcome.State != search.OutcomeApplied || outcome.Version != 9 {
		t.Fatalf("Write(delete) outcome/error = %#v/%v", outcome, err)
	}
	request := <-requests
	wantTarget := "/tenant-a-events-v2/_doc/event-1?refresh=wait_for&version=9&version_type=external"
	if request.method != http.MethodDelete || request.target != wantTarget {
		t.Fatalf("Write(delete) request = %#v, want DELETE %s", request, wantTarget)
	}
}

func TestWriteRejectsSuccessfulResponseWithWrongExternalVersion(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"_id":"event-1","_version":8,"result":"updated"}`)
	}))
	t.Cleanup(server.Close)
	client := newWriteClient(t, server.URL, server.Client().Transport)
	document, _ := search.NewDocument("tenant-a", "events", "event-1", 9, json.RawMessage(`{"value":"new"}`), search.DefaultLimits())
	outcome, err := client.Write(t.Context(), search.IndexDocument(document), search.RefreshNone)
	var failure *adapter.Failure
	if !errors.As(err, &failure) || failure.Category != adapter.FailureMalformed || failure.OutcomeKnown || outcome.State != search.OutcomeUnknown {
		t.Fatalf("Write() outcome/error = %#v/%v", outcome, err)
	}
}
