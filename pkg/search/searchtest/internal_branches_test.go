package searchtest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/search"
)

func fakeBranchOperation(action search.WriteAction, id string, version uint64, source string) search.WriteOperation {
	return search.WriteOperation{Action: action, Tenant: "t", Index: "i", ID: id, Version: version, Source: json.RawMessage(source)}
}

func TestInternalFakeConstructionCancellationAndWriteBranches(t *testing.T) {
	if _, err := NewFake(search.Limits{}); !errors.Is(err, ErrInvalidFake) {
		t.Fatal(err)
	}
	fake, err := NewFake(search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fake.Capabilities(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := fake.Bulk(ctx, search.BulkRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := fake.Search(ctx, search.Request{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := fake.Write(t.Context(), fakeBranchOperation(search.ActionIndex, "a", 1, `{}`), search.RefreshNone); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.Write(t.Context(), search.WriteOperation{}, search.RefreshNone); err == nil {
		t.Fatal("invalid write accepted")
	}
	if _, err := fake.Bulk(t.Context(), search.BulkRequest{}); err == nil {
		t.Fatal("invalid bulk accepted")
	}
	operations := []search.WriteOperation{
		fakeBranchOperation(search.ActionIndex, "a", 1, `{}`),
		fakeBranchOperation(search.ActionUpdate, "missing", 1, `{}`),
		fakeBranchOperation(search.ActionDelete, "absent", 1, ""),
		fakeBranchOperation(search.ActionUpdate, "a", 2, `{"name":"updated"}`),
		fakeBranchOperation(search.ActionUpsert, "b", 1, `{"name":"beta"}`),
		fakeBranchOperation(search.ActionDelete, "a", 3, ""),
	}
	result, err := fake.Bulk(t.Context(), search.BulkRequest{Operations: operations, Refresh: search.RefreshNone})
	if err != nil {
		t.Fatal(err)
	}
	items := result.Items()
	if items[0].State != search.OutcomeVersionConflict || items[1].State != search.OutcomeNotFound || items[2].State != search.OutcomeNotFound || items[3].State != search.OutcomeApplied || items[5].State != search.OutcomeApplied {
		t.Fatalf("items=%#v", items)
	}
}

func TestInternalFakeRejectsNewDeleteTombstoneAtCapacity(t *testing.T) {
	limits := search.DefaultLimits()
	limits.MaxPages, limits.MaxPageItems = 1, 1
	fake, err := NewFake(limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fake.Write(t.Context(), fakeBranchOperation(search.ActionIndex, "existing", 1, `{}`), search.RefreshNone); err != nil {
		t.Fatal(err)
	}
	outcome, err := fake.Write(t.Context(), fakeBranchOperation(search.ActionDelete, "new-tombstone", 1, ""), search.RefreshNone)
	if err != nil || outcome.State != search.OutcomeRejected || outcome.Code != "fake_capacity" {
		t.Fatalf("capacity delete = %#v/%v", outcome, err)
	}
}

func TestInternalFakeSearchAndMatchingBranches(t *testing.T) {
	fake, _ := NewFake(search.DefaultLimits())
	operations := []search.WriteOperation{
		fakeBranchOperation(search.ActionIndex, "a", 1, `{"name":"alpha","country":"FI","nested":{"code":"A"}}`),
		fakeBranchOperation(search.ActionIndex, "b", 1, `{"name":"beta","country":"SE"}`),
		fakeBranchOperation(search.ActionIndex, "c", 1, `{"name":3,"country":"FI"}`),
	}
	if _, err := fake.Bulk(t.Context(), search.BulkRequest{Operations: operations, Refresh: search.RefreshNone}); err != nil {
		t.Fatal(err)
	}
	base := search.Request{Tenant: "t", Index: "i", Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 10}}
	for _, test := range []struct {
		query search.Query
		want  int
	}{
		{search.TermQuery{Field: "country", Value: search.StringValue("FI")}, 2},
		{search.TermQuery{Field: "missing", Value: search.StringValue("x")}, 0},
		{search.PrefixQuery{Field: "name", Prefix: "a"}, 1},
		{search.PrefixQuery{Field: "name", Prefix: "3"}, 0},
		{search.ExistsQuery{Field: "nested.code"}, 1},
		{search.BoolQuery{Must: []search.Query{search.ExistsQuery{Field: "country"}}, Filter: []search.Query{search.TermQuery{Field: "country", Value: search.StringValue("FI")}}, MustNot: []search.Query{search.PrefixQuery{Field: "name", Prefix: "z"}}, Should: []search.Query{search.PrefixQuery{Field: "name", Prefix: "a"}}, MinimumShouldMatch: 1}, 1},
		{search.BoolQuery{Must: []search.Query{search.ExistsQuery{Field: "missing"}}}, 0},
		{search.BoolQuery{Must: []search.Query{search.ExistsQuery{Field: "country"}}, MustNot: []search.Query{search.TermQuery{Field: "country", Value: search.StringValue("FI")}}}, 1},
	} {
		request := base
		request.Query = test.query
		result, err := fake.Search(t.Context(), request)
		if err != nil {
			t.Fatalf("%T: %v", test.query, err)
		}
		if len(result.Hits()) != test.want {
			t.Fatalf("%T hits=%d", test.query, len(result.Hits()))
		}
	}
	desc := base
	desc.Sort[0].Direction = search.Descending
	desc.Page = search.OffsetPage{Size: 1, Offset: 99}
	result, err := fake.Search(t.Context(), desc)
	if err != nil || len(result.Hits()) != 0 {
		t.Fatal(err)
	}
	badSort := base
	badSort.Sort = []search.Sort{{Field: "name", Direction: search.Ascending}}
	if _, err := fake.Search(t.Context(), badSort); !errors.Is(err, search.ErrUnsupported) {
		t.Fatal(err)
	}
	cursor := base
	cursor.Page = search.CursorPage{Size: 1}
	if _, err := fake.Search(t.Context(), cursor); err == nil {
		t.Fatal("cursor accepted")
	}
	if _, err := matches(search.MatchAllQuery{}, json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed source accepted")
	}
	fake.documents[documentKey("t", "i", "broken")] = search.Document{Tenant: "t", Index: "i", ID: "broken", Version: 1, Source: json.RawMessage(`{`)}
	if _, err := fake.Search(t.Context(), base); err == nil {
		t.Fatal("malformed stored source accepted")
	}
	if _, err := matchesFields(search.FullTextQuery{}, map[string]any{}); !errors.Is(err, search.ErrUnsupported) {
		t.Fatal(err)
	}
	for _, query := range []search.Query{
		search.BoolQuery{Must: []search.Query{search.FullTextQuery{}}},
		search.BoolQuery{MustNot: []search.Query{search.FullTextQuery{}}},
		search.BoolQuery{Should: []search.Query{search.FullTextQuery{}}, MinimumShouldMatch: 1},
	} {
		if _, err := matchesFields(query, map[string]any{}); !errors.Is(err, search.ErrUnsupported) {
			t.Fatal(err)
		}
	}
	if _, ok := lookup(map[string]any{"nested": "text"}, "nested.code"); ok {
		t.Fatal("non-object path found")
	}
}
