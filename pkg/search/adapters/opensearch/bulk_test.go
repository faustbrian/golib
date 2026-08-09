package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestBulkEncodesExternalVersionsAndPreservesPartialOutcomes(t *testing.T) {
	t.Parallel()

	var requestBody string
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requestBody = string(body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"took":4,"errors":true,"items":[{"index":{"_index":"tenant-a-events-v2","_id":"a","_version":3,"status":201,"result":"created"}},{"delete":{"_index":"tenant-a-events-v2","_id":"b","status":409,"error":{"type":"version_conflict_engine_exception","reason":"sensitive"}}},{"index":{"_index":"tenant-a-events-v2","_id":"c","status":429,"error":{"type":"rejected_execution_exception","reason":"sensitive"}}}]}`)
	}))
	t.Cleanup(server.Close)
	client := newWriteClient(t, server.URL, server.Client().Transport)
	limits := search.DefaultLimits()
	a, _ := search.NewDocument("tenant-a", "events", "a", 3, json.RawMessage(`{"value":"a"}`), limits)
	c, _ := search.NewDocument("tenant-a", "events", "c", 5, json.RawMessage(`{"value":"c"}`), limits)
	result, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{
		search.IndexDocument(a), search.DeleteDocument("tenant-a", "events", "b", 4), search.IndexDocument(c),
	}, Refresh: search.RefreshWaitFor})
	if err != nil {
		t.Fatalf("Bulk() error = %v", err)
	}
	items := result.Items()
	if len(items) != 3 || items[0].State != search.OutcomeApplied || items[1].State != search.OutcomeVersionConflict || items[2].State != search.OutcomeRejected || !items[2].Retryable || !result.Partial() {
		t.Fatalf("Bulk() = %#v", items)
	}
	lines := strings.Split(strings.TrimSpace(requestBody), "\n")
	if len(lines) != 5 || !strings.Contains(lines[0], `"version":3`) || !strings.Contains(lines[0], `"version_type":"external"`) || !strings.Contains(lines[2], `"version":4`) {
		t.Fatalf("bulk NDJSON = %q", requestBody)
	}
}

func TestBulkReturnsPerItemUnknownOutcomesWhenTransportOutcomeIsAmbiguous(t *testing.T) {
	t.Parallel()

	client := newWriteClient(t, "https://search.example.test", &observedTransport{err: errors.New("connection reset")})
	limits := search.DefaultLimits()
	document, _ := search.NewDocument("tenant-a", "events", "a", 3, json.RawMessage(`{"value":"a"}`), limits)
	result, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{search.IndexDocument(document)}, Refresh: search.RefreshNone})
	var failure *adapter.Failure
	if !errors.As(err, &failure) || failure.OutcomeKnown || len(result.Items()) != 1 || result.Items()[0].State != search.OutcomeUnknown || !result.HasUnknown() {
		t.Fatalf("Bulk() result/error = %#v/%#v", result.Items(), err)
	}
}

func TestBulkRejectsEncodedBodyAboveConfiguredByteLimit(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = io.WriteString(writer, `{"took":1,"errors":false,"items":[{"index":{"_id":"a","_version":1,"status":201,"result":"created"}}]}`)
	}))
	t.Cleanup(server.Close)
	limits := search.DefaultLimits()
	document, err := search.NewDocument("tenant-a", "events", "a", 1, json.RawMessage(`{"value":"a"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	operation := search.IndexDocument(document)
	limits.MaxBulkBytes = len(operation.Tenant) + len(operation.Index) + len(operation.ID) + len(operation.Source) + 64
	client := newWriteClientWithLimits(t, server.URL, server.Client().Transport, limits)
	_, err = client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{operation}, Refresh: search.RefreshNone})
	if !errors.Is(err, search.ErrBulkLimit) || requests != 0 {
		t.Fatalf("Bulk() error/requests = %v/%d, want ErrBulkLimit/0", err, requests)
	}
}

func TestBulkRejectsMisattributedResponseActions(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"took":1,"errors":false,"items":[{"delete":{"_id":"a","_version":3,"status":200,"result":"deleted"}}]}`)
	}))
	t.Cleanup(server.Close)
	client := newWriteClient(t, server.URL, server.Client().Transport)
	document, _ := search.NewDocument("tenant-a", "events", "a", 3, json.RawMessage(`{"value":"a"}`), search.DefaultLimits())
	result, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{search.IndexDocument(document)}, Refresh: search.RefreshNone})
	var failure *adapter.Failure
	if !errors.As(err, &failure) || failure.Category != adapter.FailureMalformed || failure.OutcomeKnown || !result.HasUnknown() {
		t.Fatalf("Bulk() misattribution = %#v/%v", result.Items(), err)
	}
}

func TestBulkRejectsInconsistentSuccessVersionAndErrorsFlag(t *testing.T) {
	t.Parallel()
	responses := []string{
		`{"took":1,"errors":false,"items":[{"index":{"_id":"a","_version":2,"status":201,"result":"created"}}]}`,
		`{"took":1,"errors":true,"items":[{"index":{"_id":"a","_version":3,"status":201,"result":"created"}}]}`,
	}
	for _, response := range responses {
		response := response
		t.Run(response, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(writer, response)
			}))
			t.Cleanup(server.Close)
			client := newWriteClient(t, server.URL, server.Client().Transport)
			document, _ := search.NewDocument("tenant-a", "events", "a", 3, json.RawMessage(`{"value":"a"}`), search.DefaultLimits())
			result, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{search.IndexDocument(document)}, Refresh: search.RefreshNone})
			var failure *adapter.Failure
			if !errors.As(err, &failure) || failure.Category != adapter.FailureMalformed || failure.OutcomeKnown || !result.HasUnknown() {
				t.Fatalf("Bulk() result/error = %#v/%v", result.Items(), err)
			}
		})
	}
}

func newWriteClient(t *testing.T, endpoint string, transport http.RoundTripper) *adapter.Client {
	return newWriteClientWithLimits(t, endpoint, transport, search.DefaultLimits())
}

func newWriteClientWithLimits(t *testing.T, endpoint string, transport http.RoundTripper, limits search.Limits) *adapter.Client {
	t.Helper()
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, Transport: transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: time.Second, MaximumResponseBytes: 16 << 10,
		Search: &adapter.SearchConfig{Limits: limits, CursorCodec: mustCursorCodec(t), Clock: time.Now,
			Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
				return adapter.IndexTarget{Name: "tenant-a-events-v2", Fingerprint: "mapping-v2-fingerprint"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func mustCursorCodec(t *testing.T) *search.CursorCodec {
	t.Helper()
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}
