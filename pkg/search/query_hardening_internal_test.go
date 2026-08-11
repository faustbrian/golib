package search

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type hostileAggregation struct{}

func (hostileAggregation) aggregationNode() {}

type hostileSuggestion struct{}

func (hostileSuggestion) suggestionNode() {}

func TestQueryNodeInventoryAndHostileUTF8AreClosedAndRejected(t *testing.T) {
	invalid := string([]byte{0xff})
	number, _ := NumberValue("1")
	invalidValue := StringValue(invalid)
	nodes := []Query{
		MatchAllQuery{}, BoolQuery{}, TermQuery{}, FullTextQuery{}, PrefixQuery{},
		RangeQuery{}, ExistsQuery{}, GeoDistanceQuery{}, RawExtensionQuery{},
	}
	for _, node := range nodes {
		switch value := node.(type) {
		case MatchAllQuery:
			value.queryNode()
		case BoolQuery:
			value.queryNode()
		case TermQuery:
			value.queryNode()
		case FullTextQuery:
			value.queryNode()
		case PrefixQuery:
			value.queryNode()
		case RangeQuery:
			value.queryNode()
		case ExistsQuery:
			value.queryNode()
		case GeoDistanceQuery:
			value.queryNode()
		case RawExtensionQuery:
			value.queryNode()
		default:
			t.Fatalf("unhandled query node %T", node)
		}
	}
	TermsAggregation{}.aggregationNode()
	RangeAggregation{}.aggregationNode()
	PrefixSuggestion{}.suggestionNode()

	base := Request{Tenant: "tenant", Index: "index", Query: MatchAllQuery{}, Sort: []Sort{{Field: DocumentIDSortField, Direction: Ascending}}, Page: OffsetPage{Size: 1}}
	requests := []Request{
		withQueryMutation(base, func(request *Request) { request.Sort[0].Field = invalid }),
		withQueryMutation(base, func(request *Request) { request.Page = CursorPage{Size: 1, Cursor: invalid, KeepAlive: time.Second} }),
		withQueryMutation(base, func(request *Request) { request.Projection.Includes = []string{invalid} }),
		withQueryMutation(base, func(request *Request) { request.Projection.Excludes = []string{invalid} }),
		withQueryMutation(base, func(request *Request) {
			request.Highlights = map[string]Highlight{invalid: {FragmentSize: 1, MaxFragments: 1}}
		}),
		withQueryMutation(base, func(request *Request) {
			request.Highlights = map[string]Highlight{"field": {FragmentSize: 1, MaxFragments: 1, PostTag: invalid}}
		}),
		withQueryMutation(base, func(request *Request) {
			request.Aggregations = map[string]Aggregation{invalid: TermsAggregation{Field: "field", Size: 1}}
		}),
		withQueryMutation(base, func(request *Request) {
			request.Aggregations = map[string]Aggregation{"a": TermsAggregation{Field: invalid, Size: 1}}
		}),
		withQueryMutation(base, func(request *Request) {
			request.Aggregations = map[string]Aggregation{"a": RangeAggregation{Field: invalid, Buckets: []RangeBucket{{Key: "k", From: &number}}}}
		}),
		withQueryMutation(base, func(request *Request) {
			request.Aggregations = map[string]Aggregation{"a": RangeAggregation{Field: "field", Buckets: []RangeBucket{{Key: invalid, From: &number}}}}
		}),
		withQueryMutation(base, func(request *Request) {
			request.Aggregations = map[string]Aggregation{"a": RangeAggregation{Field: "field", Buckets: []RangeBucket{{Key: "k", From: &invalidValue}}}}
		}),
		withQueryMutation(base, func(request *Request) { request.Aggregations = map[string]Aggregation{"a": hostileAggregation{}} }),
		withQueryMutation(base, func(request *Request) {
			request.Suggestions = map[string]Suggestion{invalid: PrefixSuggestion{Field: "field", Text: "x", Size: 1}}
		}),
		withQueryMutation(base, func(request *Request) {
			request.Suggestions = map[string]Suggestion{"s": PrefixSuggestion{Field: invalid, Text: "x", Size: 1}}
		}),
		withQueryMutation(base, func(request *Request) {
			request.Suggestions = map[string]Suggestion{"s": PrefixSuggestion{Field: "field", Text: invalid, Size: 1}}
		}),
		withQueryMutation(base, func(request *Request) { request.Suggestions = map[string]Suggestion{"s": hostileSuggestion{}} }),
		withQueryMutation(base, func(request *Request) {
			request.Query = BoolQuery{Must: []Query{PrefixQuery{Field: "field", Prefix: invalid}}}
		}),
		withQueryMutation(base, func(request *Request) { request.Query = FullTextQuery{Fields: []string{invalid}, Text: "x"} }),
		withQueryMutation(base, func(request *Request) {
			request.Query = FullTextQuery{Fields: []string{"field"}, Text: "x", Analyzer: invalid}
		}),
		withQueryMutation(base, func(request *Request) { request.Query = RangeQuery{Field: invalid, GT: &number} }),
		withQueryMutation(base, func(request *Request) { request.Query = RangeQuery{Field: "field", GT: &invalidValue} }),
		withQueryMutation(base, func(request *Request) {
			request.Query = RawExtensionQuery{Adapter: invalid, Payload: json.RawMessage(`{}`)}
		}),
		withQueryMutation(base, func(request *Request) { request.Query = nil }),
	}
	for position, request := range requests {
		if err := request.Validate(AllCapabilities(), DefaultLimits()); !errors.Is(err, ErrInvalidQuery) {
			t.Fatalf("request %d error = %v, want ErrInvalidQuery", position, err)
		}
	}
	if queryInputsValidUTF8(nil) {
		t.Fatal("nil query reported valid UTF-8")
	}
	if aggregationInputsValidUTF8(hostileAggregation{}) {
		t.Fatal("unknown aggregation reported valid UTF-8")
	}
	validValue := StringValue("value")
	for _, query := range []Query{
		TermQuery{Field: invalid, Value: validValue},
		TermQuery{Field: "field", Value: invalidValue},
		GeoDistanceQuery{Field: invalid, DistanceKM: number},
		GeoDistanceQuery{Field: "field", DistanceKM: invalidValue},
	} {
		if queryInputsValidUTF8(query) {
			t.Fatalf("query with one hostile UTF-8 component reported valid: %#v", query)
		}
	}
}

func TestRangeAggregationBudgetRejectsEachOversizedComponent(t *testing.T) {
	number := StringValue("x")
	aggregation := RangeAggregation{Field: "field", Buckets: []RangeBucket{{Key: "key", From: &number}}}
	for _, budget := range []requestInputBudget{
		{bytes: 0, nodes: 2, maxCollection: 2},
		{bytes: len(aggregation.Field), nodes: 2, maxCollection: 2},
		{bytes: len(aggregation.Field) + len(aggregation.Buckets[0].Key), nodes: 2, maxCollection: 2},
	} {
		if budget.aggregation(aggregation) {
			t.Fatalf("aggregation accepted with budget %#v", budget)
		}
	}
}

func TestCanonicalObjectRejectsNonObjectAndTrailingValues(t *testing.T) {
	for _, raw := range []json.RawMessage{json.RawMessage(`[]`), json.RawMessage(`{} {}`)} {
		if _, err := canonicalJSONObject(raw); err == nil {
			t.Fatalf("canonicalJSONObject(%q) accepted", raw)
		}
	}
	if SourceDigest(json.RawMessage(`[]`)) != "" {
		t.Fatal("SourceDigest accepted a non-object source")
	}
}

func TestReconciliationReservationRejectsDocumentBeyondRemainingBudget(t *testing.T) {
	document := Document{Tenant: "t", Index: "i", ID: "d", Source: json.RawMessage(`{}`)}
	record := ReconciliationRecord{ID: "a", Digest: "b", Document: &document}
	remaining := int64(258)
	if reserveReconciliationRecord(record, &remaining) {
		t.Fatal("record exceeding remaining reconciliation budget was reserved")
	}
}

func withQueryMutation(base Request, mutate func(*Request)) Request {
	request := base
	request.Sort = append([]Sort(nil), base.Sort...)
	mutate(&request)
	return request
}
