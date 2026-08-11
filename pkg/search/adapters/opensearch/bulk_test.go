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

func TestBulkClassifies429ClusterBlockAsNonRetryable(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"took":1,"errors":true,"items":[{"index":{"_index":"tenant-a-events-v2","_id":"event-1","status":429,"error":{"type":"cluster_block_exception"}}}]}`)
	}))
	t.Cleanup(server.Close)
	client := newWriteClient(t, server.URL, server.Client().Transport)
	document, err := search.NewDocument("tenant-a", "events", "event-1", 7, json.RawMessage(`{"value":"a"}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.Bulk(t.Context(), search.BulkRequest{
		Operations: []search.WriteOperation{search.IndexDocument(document)}, Refresh: search.RefreshNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	items := result.Items()
	if len(items) != 1 || items[0].State != search.OutcomeFailed || items[0].Retryable || items[0].Code != "cluster_block_exception" {
		t.Fatalf("Bulk() cluster block = %#v", items)
	}
}

func TestBulkAcceptsEverySupportedSuccessStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, responseAction, result string
		action                       search.WriteAction
		status                       int
	}{
		{name: "delete ok", action: search.ActionDelete, responseAction: "delete", status: http.StatusOK, result: "deleted"},
		{name: "index ok", action: search.ActionIndex, responseAction: "index", status: http.StatusOK, result: "updated"},
		{name: "index created", action: search.ActionIndex, responseAction: "index", status: http.StatusCreated, result: "created"},
		{name: "upsert ok", action: search.ActionUpsert, responseAction: "index", status: http.StatusOK, result: "updated"},
		{name: "upsert created", action: search.ActionUpsert, responseAction: "index", status: http.StatusCreated, result: "created"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(writer).Encode(map[string]any{
					"took": 1, "errors": false,
					"items": []any{map[string]any{test.responseAction: map[string]any{
						"_index": "tenant-a-events-v2", "_id": "event-1", "_version": 7, "status": test.status, "result": test.result,
					}}},
				})
			}))
			t.Cleanup(server.Close)
			client := newWriteClient(t, server.URL, server.Client().Transport)
			document, err := search.NewDocument("tenant-a", "events", "event-1", 7, json.RawMessage(`{"value":"a"}`), search.DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			operation := search.DeleteDocument("tenant-a", "events", "event-1", 7)
			switch test.action {
			case search.ActionIndex:
				operation = search.IndexDocument(document)
			case search.ActionUpsert:
				operation = search.UpsertDocument(document)
			}

			result, err := client.Bulk(t.Context(), search.BulkRequest{
				Operations: []search.WriteOperation{operation}, Refresh: search.RefreshNone,
			})
			items := result.Items()
			if err != nil || len(items) != 1 || items[0].Action != test.action ||
				items[0].State != search.OutcomeApplied || items[0].Version != 7 || result.Partial() {
				t.Fatalf("Bulk() result/error = %#v/%v", items, err)
			}
		})
	}
}

func TestBulkClassifiesNotFoundOnlyForDelete(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"took":4,"errors":true,"items":[{"index":{"_index":"tenant-a-events-v2","_id":"a","status":404,"error":{"type":"index_not_found_exception"}}},{"index":{"_index":"tenant-a-events-v2","_id":"b","status":404}},{"delete":{"_index":"tenant-a-events-v2","_id":"c","status":404,"result":"not_found"}}]}`)
	}))
	t.Cleanup(server.Close)
	client := newWriteClient(t, server.URL, server.Client().Transport)
	limits := search.DefaultLimits()
	a, _ := search.NewDocument("tenant-a", "events", "a", 3, json.RawMessage(`{"value":"a"}`), limits)
	b, _ := search.NewDocument("tenant-a", "events", "b", 4, json.RawMessage(`{"value":"b"}`), limits)

	result, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{
		search.IndexDocument(a), search.UpsertDocument(b), search.DeleteDocument("tenant-a", "events", "c", 5),
	}, Refresh: search.RefreshNone})
	if err != nil {
		t.Fatalf("Bulk() error = %v", err)
	}
	items := result.Items()
	if len(items) != 3 || items[0].State != search.OutcomeFailed || items[1].State != search.OutcomeUnknown || items[2].State != search.OutcomeNotFound {
		t.Fatalf("Bulk() states = %#v", items)
	}
	if !result.Partial() || !result.HasUnknown() {
		t.Fatalf("Bulk() partial/unknown = %v/%v, want true/true", result.Partial(), result.HasUnknown())
	}
}

func TestBulkDeleteRequiresExactNotFoundEvidence(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		item        string
		wantState   search.OutcomeState
		wantCode    string
		wantFailure bool
	}{
		"matching document not found": {
			item:      `{"_index":"tenant-a-events-v2","_id":"event-1","status":404,"result":"not_found"}`,
			wantState: search.OutcomeNotFound,
		},
		"missing index": {
			item:      `{"_index":"tenant-a-events-v2","_id":"event-1","status":404,"error":{"type":"index_not_found_exception"}}`,
			wantState: search.OutcomeFailed,
			wantCode:  "index_not_found_exception",
		},
		"missing id": {
			item:        `{"_index":"tenant-a-events-v2","status":404,"result":"not_found"}`,
			wantFailure: true,
		},
		"missing result": {
			item:        `{"_index":"tenant-a-events-v2","_id":"event-1","status":404}`,
			wantFailure: true,
		},
		"wrong result": {
			item:        `{"_index":"tenant-a-events-v2","_id":"event-1","status":404,"result":"deleted"}`,
			wantFailure: true,
		},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(writer, `{"took":1,"errors":true,"items":[{"delete":`+test.item+`}]}`)
			}))
			t.Cleanup(server.Close)
			client := newWriteClient(t, server.URL, server.Client().Transport)

			result, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{
				search.DeleteDocument("tenant-a", "events", "event-1", 9),
			}, Refresh: search.RefreshNone})
			if test.wantFailure {
				var failure *adapter.Failure
				if !errors.As(err, &failure) || failure.Category != adapter.FailureMalformed ||
					failure.OutcomeKnown || !result.HasUnknown() {
					t.Fatalf("Bulk() result/error = %#v/%v, want malformed unknown outcome", result.Items(), err)
				}
				return
			}
			items := result.Items()
			if err != nil || len(items) != 1 || items[0].State != test.wantState || items[0].Code != test.wantCode {
				t.Fatalf("Bulk() result/error = %#v/%v", items, err)
			}
		})
	}
}

func TestBulkClassifiesVersionConflictOnlyFromExactBackendCode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		item      string
		wantState search.OutcomeState
	}{
		"external version conflict": {
			item:      `{"_index":"tenant-a-events-v2","_id":"event-1","status":409,"error":{"type":"version_conflict_engine_exception"}}`,
			wantState: search.OutcomeVersionConflict,
		},
		"unattributed conflict status": {
			item:      `{"_index":"tenant-a-events-v2","_id":"event-1","status":409}`,
			wantState: search.OutcomeUnknown,
		},
		"different conflict": {
			item:      `{"_index":"tenant-a-events-v2","_id":"event-1","status":409,"error":{"type":"resource_already_exists_exception"}}`,
			wantState: search.OutcomeFailed,
		},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(writer, `{"took":1,"errors":true,"items":[{"index":`+test.item+`}]}`)
			}))
			t.Cleanup(server.Close)
			client := newWriteClient(t, server.URL, server.Client().Transport)

			result, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{
				mustWriteOperation(t, search.ActionIndex),
			}, Refresh: search.RefreshNone})
			items := result.Items()
			if err != nil || len(items) != 1 || items[0].State != test.wantState {
				t.Fatalf("Bulk() result/error = %#v/%v", items, err)
			}
		})
	}
}

func TestBulkRejectsItemAttributedToAnotherPhysicalIndex(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"took":1,"errors":false,"items":[{"index":{"_index":"tenant-b-events-v2","_id":"event-1","_version":9,"status":201,"result":"created"}}]}`)
	}))
	t.Cleanup(server.Close)
	client := newWriteClient(t, server.URL, server.Client().Transport)
	document, err := search.NewDocument("tenant-a", "events", "event-1", 9, json.RawMessage(`{"value":"safe"}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{search.IndexDocument(document)}, Refresh: search.RefreshNone})
	failure := new(adapter.Failure)
	if !errors.As(err, &failure) || failure.Category != adapter.FailureMalformed || failure.OutcomeKnown || !result.HasUnknown() {
		t.Fatalf("Bulk() = %#v/%#v, want malformed unknown outcome", result.Items(), failure)
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
		_, _ = io.WriteString(writer, `{"took":1,"errors":false,"items":[{"index":{"_index":"tenant-a-events-v2","_id":"a","_version":1,"status":201,"result":"created"}}]}`)
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
		_, _ = io.WriteString(writer, `{"took":1,"errors":false,"items":[{"delete":{"_index":"tenant-a-events-v2","_id":"a","_version":3,"status":200,"result":"deleted"}}]}`)
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
		`{"errors":false,"items":[{"index":{"_index":"tenant-a-events-v2","_id":"a","_version":3,"status":201,"result":"created"}}]}`,
		`{"took":1,"items":[{"index":{"_index":"tenant-a-events-v2","_id":"a","_version":3,"status":201,"result":"created"}}]}`,
		`{"took":1,"errors":false,"items":[{"index":{"_index":"tenant-a-events-v2","_id":"a","_version":2,"status":201,"result":"created"}}]}`,
		`{"took":1,"errors":true,"items":[{"index":{"_index":"tenant-a-events-v2","_id":"a","_version":3,"status":201,"result":"created"}}]}`,
		`{"took":1,"errors":false,"items":[{"index":{"_index":"tenant-a-events-v2","_id":"a","_version":3,"status":201}}]}`,
		`{"took":1,"errors":false,"items":[{"index":{"_index":"tenant-a-events-v2","_id":"a","_version":3,"status":201,"result":"deleted"}}]}`,
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
		Search: &adapter.SearchConfig{Limits: limits, CursorCodec: mustCursorCodec(t), Clock: time.Now, WriteGuard: allowWriteAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
				return adapter.IndexTarget{Name: "tenant-a-events-v2", PhysicalName: "tenant-a-events-v2", Fingerprint: "mapping-v2-fingerprint"}, nil
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
