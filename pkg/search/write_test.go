package search_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/search"
)

func TestBulkRequestIsTenantIsolatedBoundedAndExternallyVersioned(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	document, err := search.NewDocument("tenant-a", "events-write", "event-1", 9, json.RawMessage(`{"status":"delivered"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	request := search.BulkRequest{
		Operations: []search.WriteOperation{
			search.IndexDocument(document),
			search.DeleteDocument("tenant-a", "events-write", "event-2", 4),
		},
		Refresh: search.RefreshWaitFor,
	}
	if err := request.Validate(search.AllCapabilities(), limits); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := request.Validate(search.AllCapabilities(), search.Limits{}); !errors.Is(err, search.ErrBulkLimit) {
		t.Fatalf("Validate() invalid limits error = %v, want ErrBulkLimit", err)
	}

	request.Operations[1] = search.DeleteDocument("tenant-b", "events-write", "event-2", 4)
	if err := request.Validate(search.AllCapabilities(), limits); !errors.Is(err, search.ErrTenantMismatch) {
		t.Fatalf("Validate() error = %v, want ErrTenantMismatch", err)
	}

	request.Operations = make([]search.WriteOperation, limits.MaxBulkItems+1)
	if err := request.Validate(search.AllCapabilities(), limits); !errors.Is(err, search.ErrBulkLimit) {
		t.Fatalf("Validate() error = %v, want ErrBulkLimit", err)
	}
}

func TestBulkRequestRejectsUpdateWhenUpdateExistingIsUnsupported(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	document, err := search.NewDocument("tenant-a", "events-write", "event-1", 9, json.RawMessage(`{"status":"delivered"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := search.AllCapabilities()
	capabilities.UpdateExisting = false
	request := search.BulkRequest{
		Operations: []search.WriteOperation{search.UpdateDocument(document)},
		Refresh:    search.RefreshNone,
	}

	if err := request.Validate(capabilities, limits); !errors.Is(err, search.ErrUnsupported) {
		t.Fatalf("Validate() error = %v, want ErrUnsupported", err)
	}
}

func TestBulkRequestRejectsHostileDirectWriteOperationSources(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		configure func(*search.Limits)
		source    json.RawMessage
		want      error
	}{
		{"excessive depth", func(limits *search.Limits) { limits.MaxJSONDepth = 2 }, json.RawMessage(`{"outer":{"too":{"deep":true}}}`), search.ErrJSONDepthLimit},
		{"excessive nodes", func(limits *search.Limits) { limits.MaxJSONNodes = 2 }, json.RawMessage(`{"first":1,"second":2,"third":3}`), search.ErrJSONNodeLimit},
		{"duplicate keys", func(*search.Limits) {}, json.RawMessage(`{"same":1,"s\u0061me":2}`), search.ErrDuplicateJSONKey},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := search.DefaultLimits()
			test.configure(&limits)
			request := search.BulkRequest{Operations: []search.WriteOperation{{
				Action: search.ActionIndex, Tenant: "tenant-a", Index: "events", ID: "event-1", Version: 1, Source: test.source,
			}}, Refresh: search.RefreshNone}
			if err := request.Validate(search.AllCapabilities(), limits); !errors.Is(err, search.ErrInvalidOperation) || !errors.Is(err, test.want) {
				t.Fatalf("Validate() error = %v, want ErrInvalidOperation and %v", err, test.want)
			}
		})
	}
}

func TestBulkResultRetainsEveryPartialAndUnknownOutcome(t *testing.T) {
	t.Parallel()

	result, err := search.NewBulkResult([]search.ItemOutcome{
		{Position: 0, ID: "event-1", Action: search.ActionIndex, State: search.OutcomeApplied, Version: 9},
		{Position: 1, ID: "event-2", Action: search.ActionUpdate, State: search.OutcomeRejected, Code: "mapping_error", Retryable: false},
		{Position: 2, ID: "event-3", Action: search.ActionDelete, State: search.OutcomeUnknown, Retryable: true},
	})
	if err != nil {
		t.Fatalf("NewBulkResult() error = %v", err)
	}
	if !result.Partial() || !result.HasUnknown() || len(result.Items()) != 3 {
		t.Fatalf("bulk result = %#v", result)
	}
	items := result.Items()
	items[0].ID = "changed"
	if result.Items()[0].ID != "event-1" {
		t.Fatal("Items() exposed internal result ownership")
	}

	if _, err := search.NewBulkResult([]search.ItemOutcome{{Position: 1, ID: "bad", State: search.OutcomeApplied}}); !errors.Is(err, search.ErrInvalidBulkResult) {
		t.Fatalf("NewBulkResult() error = %v", err)
	}
}

func TestBulkResultValidatesRequestAttribution(t *testing.T) {
	t.Parallel()

	if err := (search.BulkResult{}).ValidateRequest(search.BulkRequest{}); !errors.Is(err, search.ErrInvalidBulkResult) {
		t.Fatalf("zero-value ValidateRequest() error = %v, want ErrInvalidBulkResult", err)
	}

	request := search.BulkRequest{Operations: []search.WriteOperation{
		search.DeleteDocument("tenant-a", "events", "event-1", 1),
		search.DeleteDocument("tenant-a", "events", "event-2", 2),
	}}
	result, err := search.NewBulkResult([]search.ItemOutcome{
		{Position: 0, ID: "event-1", Action: search.ActionDelete, State: search.OutcomeApplied, Version: 1},
		{Position: 1, ID: "event-2", Action: search.ActionDelete, State: search.OutcomeUnknown},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := result.ValidateRequest(request); err != nil {
		t.Fatalf("ValidateRequest() error = %v", err)
	}

	for _, invalid := range []search.BulkRequest{
		{Operations: request.Operations[:1]},
		{Operations: []search.WriteOperation{request.Operations[1], request.Operations[0]}},
		{Operations: []search.WriteOperation{search.DeleteDocument("tenant-a", "events", "event-1", 1), {Action: search.ActionIndex, Tenant: "tenant-a", Index: "events", ID: "event-2", Version: 2, Source: json.RawMessage(`{}`)}}},
		{Operations: []search.WriteOperation{search.DeleteDocument("tenant-a", "events", "event-1", 9), request.Operations[1]}},
	} {
		if err := result.ValidateRequest(invalid); !errors.Is(err, search.ErrInvalidBulkResult) {
			t.Fatalf("ValidateRequest() error = %v, want ErrInvalidBulkResult for %#v", err, invalid)
		}
	}
}

func TestBulkResultPreservesCallerBoundedDocumentIdentifiers(t *testing.T) {
	t.Parallel()

	id := strings.Repeat("i", search.DefaultLimits().MaxIDBytes+1)
	limits := search.DefaultLimits()
	limits.MaxIDBytes = len(id)
	request := search.BulkRequest{
		Operations: []search.WriteOperation{search.DeleteDocument("tenant-a", "events", id, 1)},
		Refresh:    search.RefreshNone,
	}
	if err := request.Validate(search.AllCapabilities(), limits); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	result, err := search.NewBulkResult([]search.ItemOutcome{{
		Position: 0, ID: id, Action: search.ActionDelete, State: search.OutcomeApplied, Version: 1,
	}})
	if err != nil {
		t.Fatalf("NewBulkResult() error = %v", err)
	}
	if err := result.ValidateRequest(request); err != nil {
		t.Fatalf("ValidateRequest() error = %v", err)
	}
}
