package searchtest_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/search"
	"github.com/faustbrian/golib/pkg/search/searchtest"
)

func TestFakeProvidesDeterministicTenantIsolatedContractBehavior(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	fake, err := searchtest.NewFake(limits)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := search.NewDocument("tenant-a", "events", "same-id", 1, json.RawMessage(`{"status":"delivered"}`), limits)
	b, _ := search.NewDocument("tenant-b", "events", "same-id", 1, json.RawMessage(`{"status":"pending"}`), limits)
	if _, err := fake.Write(t.Context(), search.IndexDocument(a), search.RefreshWaitFor); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.Write(t.Context(), search.IndexDocument(b), search.RefreshWaitFor); err != nil {
		t.Fatal(err)
	}

	request := search.Request{Tenant: "tenant-a", Index: "events", Query: search.TermQuery{Field: "status", Value: search.StringValue("delivered")}, Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}}, Page: search.OffsetPage{Size: 10}}
	searchResult, err := fake.Search(t.Context(), request)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(searchResult.Hits()) != 1 || string(searchResult.Hits()[0].Source) != `{"status":"delivered"}` {
		t.Fatalf("Search() = %#v", searchResult.Hits())
	}
	request.Tenant = "tenant-b"
	if got, err := fake.Search(t.Context(), request); err != nil || len(got.Hits()) != 0 {
		t.Fatalf("cross-tenant Search() = %#v/%v", got.Hits(), err)
	}
}

func TestFakeEnforcesExternalVersionsAndReportsPerItemOutcomes(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	fake, _ := searchtest.NewFake(limits)
	document, _ := search.NewDocument("tenant-a", "events", "event-1", 3, json.RawMessage(`{"value":1}`), limits)
	_, _ = fake.Write(t.Context(), search.IndexDocument(document), search.RefreshNone)

	result, err := fake.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{
		search.IndexDocument(document),
		search.DeleteDocument("tenant-a", "events", "missing", 1),
	}, Refresh: search.RefreshNone})
	if err != nil {
		t.Fatal(err)
	}
	items := result.Items()
	if items[0].State != search.OutcomeVersionConflict || items[1].State != search.OutcomeNotFound || !result.Partial() {
		t.Fatalf("Bulk() items = %#v", items)
	}

	capabilities, err := fake.Capabilities(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.FullText || capabilities.Cursor || !capabilities.Term || !capabilities.ExternalVersion {
		t.Fatalf("Capabilities() = %#v", capabilities)
	}
	request := search.Request{Tenant: "tenant-a", Index: "events", Query: search.FullTextQuery{Fields: []string{"value"}, Text: "1"}, Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}}, Page: search.OffsetPage{Size: 1}}
	if _, err := fake.Search(t.Context(), request); !errors.Is(err, search.ErrUnsupported) {
		t.Fatalf("Search() error = %v, want ErrUnsupported", err)
	}
}

func TestFakeRejectsDocumentsBeyondItsConfiguredCapacity(t *testing.T) {
	t.Parallel()
	limits := search.DefaultLimits()
	limits.MaxPages = 1
	limits.MaxPageItems = 1
	fake, err := searchtest.NewFake(limits)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := search.NewDocument("tenant", "events", "a", 1, json.RawMessage(`{}`), limits)
	second, _ := search.NewDocument("tenant", "events", "b", 1, json.RawMessage(`{}`), limits)
	if _, err := fake.Write(t.Context(), search.IndexDocument(first), search.RefreshNone); err != nil {
		t.Fatal(err)
	}
	outcome, err := fake.Write(t.Context(), search.IndexDocument(second), search.RefreshNone)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State != search.OutcomeRejected || outcome.Retryable || outcome.Code != "fake_capacity" {
		t.Fatalf("outcome = %#v", outcome)
	}
}
