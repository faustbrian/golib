package search_test

import (
	"encoding/json"
	"errors"
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
