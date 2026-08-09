package search_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func TestRequestValidatesTypedCompositionBeforeExecution(t *testing.T) {
	t.Parallel()

	distance, err := search.NumberValue("25.5")
	if err != nil {
		t.Fatalf("NumberValue() error = %v", err)
	}
	request := search.Request{
		Tenant: "tenant-a",
		Index:  "locations-read",
		Query: search.BoolQuery{
			Must: []search.Query{
				search.FullTextQuery{Fields: []string{"name^3", "address"}, Text: "Helsinki", Analyzer: "standard"},
			},
			Filter: []search.Query{
				search.TermQuery{Field: "country", Value: search.StringValue("FI")},
				search.ExistsQuery{Field: "position"},
				search.GeoDistanceQuery{Field: "position", Origin: search.GeoPoint{Latitude: 60.1699, Longitude: 24.9384}, DistanceKM: distance},
			},
		},
		Sort: []search.Sort{
			{Field: "population", Direction: search.Descending, Missing: search.MissingLast},
			{Field: "_id", Direction: search.Ascending},
		},
		Page:       search.CursorPage{Size: 25, KeepAlive: time.Minute},
		Projection: search.Projection{Includes: []string{"name", "country", "position"}},
		Highlights: map[string]search.Highlight{"name": {FragmentSize: 120, MaxFragments: 2}},
		Aggregations: map[string]search.Aggregation{
			"countries": search.TermsAggregation{Field: "country", Size: 20},
		},
		Suggestions: map[string]search.Suggestion{
			"place": search.PrefixSuggestion{Field: "name.suggest", Text: "hel", Size: 5},
		},
	}

	if err := request.Validate(search.AllCapabilities(), search.DefaultLimits()); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestRawExtensionQueryIsBoundedAndCapabilityGated(t *testing.T) {
	t.Parallel()

	request := search.Request{
		Tenant: "tenant-a",
		Index:  "events-read",
		Query: search.RawExtensionQuery{
			Adapter: "opensearch",
			Payload: json.RawMessage(`{"wildcard":{"tracking_code":{"value":"JJ*"}}}`),
		},
		Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}},
		Page: search.CursorPage{Size: 10, KeepAlive: time.Minute},
	}
	if err := request.Validate(search.AllCapabilities(), search.DefaultLimits()); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	capabilities := search.AllCapabilities()
	capabilities.RawExtensions = false
	if err := request.Validate(capabilities, search.DefaultLimits()); !errors.Is(err, search.ErrUnsupported) {
		t.Fatalf("Validate() error = %v, want ErrUnsupported", err)
	}

	limits := search.DefaultLimits()
	for _, query := range []search.RawExtensionQuery{
		{Payload: json.RawMessage(`{}`)},
		{Adapter: strings.Repeat("x", search.MaxFieldNameBytes+1), Payload: json.RawMessage(`{}`)},
		{Adapter: "opensearch"},
		{Adapter: "opensearch", Payload: json.RawMessage(`[]`)},
		{Adapter: "opensearch", Payload: json.RawMessage(`{} {}`)},
		{Adapter: "opensearch", Payload: json.RawMessage(strings.Repeat(" ", limits.MaxSourceBytes+1))},
	} {
		request.Query = query
		if err := request.Validate(search.AllCapabilities(), limits); !errors.Is(err, search.ErrInvalidQuery) {
			t.Fatalf("Validate(%#v) error = %v, want ErrInvalidQuery", query, err)
		}
	}
}

func TestRequestRejectsUnsupportedAndUnstablePagination(t *testing.T) {
	t.Parallel()

	request := search.Request{
		Tenant: "tenant-a",
		Index:  "events-read",
		Query:  search.PrefixQuery{Field: "tracking_code", Prefix: "JJ"},
		Sort:   []search.Sort{{Field: "created_at", Direction: search.Descending}},
		Page:   search.CursorPage{Size: 10, KeepAlive: time.Minute},
	}

	if err := request.Validate(search.AllCapabilities(), search.DefaultLimits()); !errors.Is(err, search.ErrUnstableSort) {
		t.Fatalf("Validate() error = %v, want ErrUnstableSort", err)
	}

	request.Sort = append(request.Sort, search.Sort{Field: "_id", Direction: search.Ascending})
	capabilities := search.AllCapabilities()
	capabilities.Prefix = false
	if err := request.Validate(capabilities, search.DefaultLimits()); !errors.Is(err, search.ErrUnsupported) {
		t.Fatalf("Validate() error = %v, want ErrUnsupported", err)
	}

	request.Query = search.MatchAllQuery{}
	request.Page = search.CursorPage{Size: search.DefaultLimits().MaxPageItems + 1, KeepAlive: time.Minute}
	if err := request.Validate(search.AllCapabilities(), search.DefaultLimits()); !errors.Is(err, search.ErrPageLimit) {
		t.Fatalf("Validate() error = %v, want ErrPageLimit", err)
	}
}

func TestQueryValidationRejectsUnsafeFieldsAndRanges(t *testing.T) {
	t.Parallel()

	tests := []search.Query{
		search.TermQuery{Field: "", Value: search.StringValue("FI")},
		search.FullTextQuery{Fields: nil, Text: "Helsinki"},
		search.FullTextQuery{Fields: []string{"name^invalid"}, Text: "Helsinki"},
		search.FullTextQuery{Fields: []string{"name^2^3"}, Text: "Helsinki"},
		search.PrefixQuery{Field: "name", Prefix: ""},
		search.RangeQuery{Field: "updated_at"},
		search.RangeQuery{Field: "updated_at", GT: valuePointer(t, "1"), GTE: valuePointer(t, "2")},
		search.RangeQuery{Field: "updated_at", LT: valuePointer(t, "1"), LTE: valuePointer(t, "2")},
		search.GeoDistanceQuery{Field: "position", Origin: search.GeoPoint{Latitude: 91}, DistanceKM: search.StringValue("25")},
		search.GeoDistanceQuery{Field: "position", Origin: search.GeoPoint{Latitude: math.NaN()}, DistanceKM: valuePointerValue(t, "25")},
		search.BoolQuery{},
	}
	for _, query := range tests {
		request := search.Request{
			Tenant: "tenant-a",
			Index:  "locations-read",
			Query:  query,
			Sort:   []search.Sort{{Field: "_id", Direction: search.Ascending}},
			Page:   search.CursorPage{Size: 10, KeepAlive: time.Minute},
		}
		if err := request.Validate(search.AllCapabilities(), search.DefaultLimits()); !errors.Is(err, search.ErrInvalidQuery) {
			t.Fatalf("Validate(%T) error = %v, want ErrInvalidQuery", query, err)
		}
	}
	request := search.Request{Tenant: "tenant-a", Index: "locations-read", Query: search.MatchAllQuery{}, Page: search.OffsetPage{Size: 10}, Projection: search.Projection{Includes: []string{"name", ""}}}
	if err := request.Validate(search.AllCapabilities(), search.DefaultLimits()); !errors.Is(err, search.ErrInvalidQuery) {
		t.Fatalf("invalid projection error = %v, want ErrInvalidQuery", err)
	}
}

func TestRequestRejectsUnboundedCursorTimeQueryBytesAndOffsetOverflow(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	request := search.Request{
		Tenant: "tenant-a", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: limits.MaxCursorDuration + time.Nanosecond},
	}
	if err := request.Validate(search.AllCapabilities(), limits); !errors.Is(err, search.ErrPageLimit) {
		t.Fatalf("cursor duration error = %v, want ErrPageLimit", err)
	}

	request.Page = search.OffsetPage{Size: 2, Offset: math.MaxInt}
	if err := request.Validate(search.AllCapabilities(), limits); !errors.Is(err, search.ErrPageLimit) {
		t.Fatalf("overflowing offset error = %v, want ErrPageLimit", err)
	}

	request.Page = search.OffsetPage{Size: 1}
	request.Query = search.FullTextQuery{Fields: []string{"message"}, Text: strings.Repeat("x", limits.MaxQueryBytes)}
	if err := request.Validate(search.AllCapabilities(), limits); !errors.Is(err, search.ErrInvalidQuery) {
		t.Fatalf("oversized query error = %v, want ErrInvalidQuery", err)
	}
}

func TestRequestRejectsUnboundedResultFanout(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	number := valuePointer(t, "1")
	base := search.Request{Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{}, Page: search.OffsetPage{Size: 1}}
	tests := []search.Request{
		func() search.Request {
			request := base
			request.Highlights = map[string]search.Highlight{"message": {FragmentSize: limits.MaxSourceBytes + 1, MaxFragments: 1}}
			return request
		}(),
		func() search.Request {
			request := base
			request.Highlights = map[string]search.Highlight{"message": {FragmentSize: 1, MaxFragments: limits.MaxPageItems + 1}}
			return request
		}(),
		func() search.Request {
			request := base
			request.Aggregations = map[string]search.Aggregation{"terms": search.TermsAggregation{Field: "kind", Size: limits.MaxPageItems + 1}}
			return request
		}(),
		func() search.Request {
			request := base
			buckets := make([]search.RangeBucket, limits.MaxQueryClauses+1)
			for index := range buckets {
				buckets[index] = search.RangeBucket{Key: "bucket", From: number}
			}
			request.Aggregations = map[string]search.Aggregation{"ranges": search.RangeAggregation{Field: "count", Buckets: buckets}}
			return request
		}(),
		func() search.Request {
			request := base
			request.Suggestions = map[string]search.Suggestion{"names": search.PrefixSuggestion{Field: "name", Text: "h", Size: limits.MaxPageItems + 1}}
			return request
		}(),
	}
	for index, request := range tests {
		if err := request.Validate(search.AllCapabilities(), limits); !errors.Is(err, search.ErrInvalidQuery) {
			t.Fatalf("request %d error = %v, want ErrInvalidQuery", index, err)
		}
	}
}

func TestRequestRejectsUnboundedOrBackendInvalidInputShape(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	base := search.Request{Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{}, Page: search.OffsetPage{Size: 1}}
	manyFields := make([]string, limits.MaxQueryClauses+1)
	for index := range manyFields {
		manyFields[index] = "field"
	}
	manySorts := make([]search.Sort, limits.MaxQueryClauses+1)
	for index := range manySorts {
		manySorts[index] = search.Sort{Field: "field", Direction: search.Ascending}
	}
	manyHighlights := make(map[string]search.Highlight, limits.MaxQueryClauses+1)
	manyAggregations := make(map[string]search.Aggregation, limits.MaxQueryClauses+1)
	manySuggestions := make(map[string]search.Suggestion, limits.MaxQueryClauses+1)
	for index := 0; index <= limits.MaxQueryClauses; index++ {
		name := fmt.Sprintf("item_%d", index)
		manyHighlights[name] = search.Highlight{FragmentSize: 1, MaxFragments: 1}
		manyAggregations[name] = search.TermsAggregation{Field: "field", Size: 1}
		manySuggestions[name] = search.PrefixSuggestion{Field: "field", Text: "x", Size: 1}
	}
	tests := []search.Request{
		func() search.Request {
			request := base
			request.Query = search.TermQuery{Field: "field", Value: search.ArrayValue([]search.Value{search.StringValue("x")})}
			return request
		}(),
		func() search.Request {
			request := base
			request.Query = search.RangeQuery{Field: "field", GT: valueReference(search.BoolValue(true))}
			return request
		}(),
		func() search.Request {
			request := base
			request.Query = search.FullTextQuery{Fields: manyFields, Text: "x"}
			return request
		}(),
		func() search.Request { request := base; request.Sort = manySorts; return request }(),
		func() search.Request { request := base; request.Projection.Includes = manyFields; return request }(),
		func() search.Request { request := base; request.Highlights = manyHighlights; return request }(),
		func() search.Request { request := base; request.Aggregations = manyAggregations; return request }(),
		func() search.Request { request := base; request.Suggestions = manySuggestions; return request }(),
	}
	for index, request := range tests {
		if err := request.Validate(search.AllCapabilities(), limits); !errors.Is(err, search.ErrInvalidQuery) {
			t.Fatalf("request %d error = %v, want ErrInvalidQuery", index, err)
		}
	}
}

func valueReference(value search.Value) *search.Value { return &value }

func TestRequestRejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	request := search.Request{
		Tenant: "tenant-a",
		Index:  "events",
		Query:  search.MatchAllQuery{},
		Sort:   []search.Sort{{Field: "_id", Direction: search.Ascending}},
		Page:   search.OffsetPage{Size: 1},
	}
	if err := request.Validate(search.AllCapabilities(), search.Limits{}); !errors.Is(err, search.ErrInvalidQuery) {
		t.Fatalf("Validate() error = %v, want ErrInvalidQuery", err)
	}
}

func valuePointer(t *testing.T, text string) *search.Value {
	t.Helper()
	value := valuePointerValue(t, text)
	return &value
}

func valuePointerValue(t *testing.T, text string) search.Value {
	t.Helper()
	value, err := search.NumberValue(text)
	if err != nil {
		t.Fatalf("NumberValue(%q) error = %v", text, err)
	}
	return value
}
