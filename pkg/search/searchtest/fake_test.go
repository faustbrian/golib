package searchtest_test

import (
	"encoding/json"
	"errors"
	"slices"
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

func TestFakeKeepsCompositeDocumentIdentitiesDistinct(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	fake, err := searchtest.NewFake(limits)
	if err != nil {
		t.Fatal(err)
	}
	documents := []search.Document{
		mustDocument(t, "a", "b\x00c", "d", 1, `{"owner":"index"}`, limits),
		mustDocument(t, "a\x00b", "c", "d", 1, `{"owner":"tenant"}`, limits),
		mustDocument(t, "a", "b", "c\x00d", 1, `{"owner":"id"}`, limits),
	}
	for _, document := range documents {
		outcome, writeErr := fake.Write(t.Context(), search.IndexDocument(document), search.RefreshNone)
		if writeErr != nil || outcome.State != search.OutcomeApplied {
			t.Fatalf("Write(%q, %q, %q) = %#v, %v", document.Tenant, document.Index, document.ID, outcome, writeErr)
		}
	}

	for _, document := range documents {
		result, searchErr := fake.Search(t.Context(), search.Request{
			Tenant: document.Tenant,
			Index:  document.Index,
			Query:  search.MatchAllQuery{},
			Sort:   []search.Sort{{Field: "_id", Direction: search.Ascending}},
			Page:   search.OffsetPage{Size: 10},
		})
		if searchErr != nil {
			t.Fatalf("Search(%q, %q) error = %v", document.Tenant, document.Index, searchErr)
		}
		hits := result.Hits()
		if len(hits) != 1 || hits[0].ID != document.ID || string(hits[0].Source) != string(document.Source) {
			t.Fatalf("Search(%q, %q) = %#v", document.Tenant, document.Index, hits)
		}
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

func TestFakeRetainsDeleteVersionTombstones(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	fake, err := searchtest.NewFake(limits)
	if err != nil {
		t.Fatal(err)
	}
	original := mustDocument(t, "tenant-a", "events", "event-1", 10, `{"value":"original"}`, limits)
	if outcome, writeErr := fake.Write(t.Context(), search.IndexDocument(original), search.RefreshNone); writeErr != nil || outcome.State != search.OutcomeApplied {
		t.Fatalf("initial Write() = %#v, %v", outcome, writeErr)
	}
	if outcome, deleteErr := fake.Write(t.Context(), search.DeleteDocument("tenant-a", "events", "event-1", 11), search.RefreshNone); deleteErr != nil || outcome.State != search.OutcomeApplied {
		t.Fatalf("Delete() = %#v, %v", outcome, deleteErr)
	}

	for _, version := range []uint64{10, 11} {
		stale := mustDocument(t, "tenant-a", "events", "event-1", version, `{"value":"stale"}`, limits)
		outcome, writeErr := fake.Write(t.Context(), search.IndexDocument(stale), search.RefreshNone)
		if writeErr != nil || outcome.State != search.OutcomeVersionConflict {
			t.Fatalf("stale Write(version=%d) = %#v, %v", version, outcome, writeErr)
		}
	}

	newer := mustDocument(t, "tenant-a", "events", "event-1", 12, `{"value":"newer"}`, limits)
	outcome, err := fake.Write(t.Context(), search.IndexDocument(newer), search.RefreshNone)
	if err != nil || outcome.State != search.OutcomeApplied {
		t.Fatalf("newer Write() = %#v, %v", outcome, err)
	}
}

func TestFakeMatchesOpenSearchMinimumShouldMatchDefaults(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	fake, err := searchtest.NewFake(limits)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range []search.Document{
		mustDocument(t, "tenant-a", "events", "a", 1, `{"country":"FI","name":"alpha"}`, limits),
		mustDocument(t, "tenant-a", "events", "b", 1, `{"country":"SE","name":"beta"}`, limits),
		mustDocument(t, "tenant-a", "events", "c", 1, `{"name":"gamma"}`, limits),
	} {
		if outcome, writeErr := fake.Write(t.Context(), search.IndexDocument(document), search.RefreshNone); writeErr != nil || outcome.State != search.OutcomeApplied {
			t.Fatalf("Write(%q) = %#v, %v", document.ID, outcome, writeErr)
		}
	}

	tests := []struct {
		name  string
		query search.BoolQuery
		want  []string
	}{
		{
			name:  "should only defaults to one required clause",
			query: search.BoolQuery{Should: []search.Query{search.PrefixQuery{Field: "name", Prefix: "a"}}},
			want:  []string{"a"},
		},
		{
			name:  "must keeps should clauses optional",
			query: search.BoolQuery{Must: []search.Query{search.ExistsQuery{Field: "name"}}, Should: []search.Query{search.PrefixQuery{Field: "name", Prefix: "z"}}},
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "filter keeps should clauses optional",
			query: search.BoolQuery{Filter: []search.Query{search.ExistsQuery{Field: "country"}}, Should: []search.Query{search.PrefixQuery{Field: "name", Prefix: "z"}}},
			want:  []string{"a", "b"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, searchErr := fake.Search(t.Context(), search.Request{
				Tenant: "tenant-a",
				Index:  "events",
				Query:  test.query,
				Sort:   []search.Sort{{Field: "_id", Direction: search.Ascending}},
				Page:   search.OffsetPage{Size: 10},
			})
			if searchErr != nil {
				t.Fatal(searchErr)
			}
			hits := result.Hits()
			got := make([]string, len(hits))
			for index, hit := range hits {
				got[index] = hit.ID
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("Search() IDs = %v, want %v", got, test.want)
			}
		})
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

func mustDocument(t *testing.T, tenant, index, id string, version uint64, source string, limits search.Limits) search.Document {
	t.Helper()
	document, err := search.NewDocument(tenant, index, id, version, json.RawMessage(source), limits)
	if err != nil {
		t.Fatal(err)
	}
	return document
}
