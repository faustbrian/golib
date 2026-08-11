package search_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func FuzzDocumentSource(f *testing.F) {
	f.Add([]byte(`{"name":"Helsinki"}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"nested":[1,null,"ä"]}`))
	f.Add([]byte(`{"same":1,"s\u0061me":2}`))
	f.Add([]byte(`{"a":{"b":{"c":{"d":true}}}}`))
	f.Fuzz(func(t *testing.T, source []byte) {
		limits := search.DefaultLimits()
		limits.MaxSourceBytes = 4096
		_, _ = search.NewDocument("tenant", "locations", "id", 1, json.RawMessage(source), limits)
	})
}

func FuzzIndexDefinitionJSON(f *testing.F) {
	f.Add([]byte(`{"index":{"refresh_interval":"1s"}}`), []byte(`{"properties":{"näme":{"type":"keyword"}}}`))
	f.Add([]byte(`{"same":1,"same":2}`), []byte(`{}`))
	f.Add([]byte(`{}`), []byte(`{"fields":[1,2,3]}`))
	f.Fuzz(func(t *testing.T, settings, mappings []byte) {
		if len(settings)+len(mappings) > 4096 {
			t.Skip()
		}
		limits := search.DefaultLimits()
		limits.MaxSourceBytes = 4096
		limits.MaxJSONDepth = 16
		limits.MaxJSONNodes = 256
		_, _ = search.NewIndexDefinition("haku-ä", json.RawMessage(settings), json.RawMessage(mappings), limits)
	})
}

func FuzzCursorDecode(f *testing.F) {
	now := time.Unix(1_800_000_000, 0)
	codec, _ := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 4096)
	binding := search.CursorBinding{Tenant: "tenant", Index: "locations", QueryFingerprint: "query", IndexFingerprint: "mapping"}
	valid, _ := codec.Encode(binding, search.CursorState{PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"id"`)}, ExpiresAt: now.Add(time.Minute)})
	f.Add(valid)
	f.Add("")
	f.Add("not-a-cursor")
	f.Fuzz(func(t *testing.T, token string) {
		_, _ = codec.Decode(token, binding, search.DefaultLimits())
	})
}

func FuzzRequestValidateAndFingerprint(f *testing.F) {
	for selector := byte(0); selector < 10; selector++ {
		f.Add([]byte{selector, 'a'})
	}
	f.Add([]byte(`country\x00name\r\n^boost`))
	f.Add([]byte(`{"script":{"source":"while(true){}"}}`))
	f.Add([]byte(`tenant-a/index-a/_search?size=2147483647`))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 4096 {
			t.Skip()
		}
		request := hostileRequest(input)
		limits := search.DefaultLimits()
		limits.MaxQueryBytes = 4096
		limits.MaxSourceBytes = 4096

		validationErr := request.Validate(search.AllCapabilities(), limits)
		first, firstErr := search.RequestFingerprint(request, limits)
		second, secondErr := search.RequestFingerprint(request, limits)
		if (firstErr == nil) != (secondErr == nil) || !errors.Is(firstErr, secondErr) || first != second {
			t.Fatalf("fingerprint is nondeterministic: %q/%v != %q/%v", first, firstErr, second, secondErr)
		}
		if validationErr == nil && (firstErr != nil || len(first) != 64) {
			t.Fatalf("validated request has no bounded fingerprint: %q/%v", first, firstErr)
		}
		if page, ok := request.Page.(search.CursorPage); ok && firstErr == nil {
			page.Cursor = "tampered:" + string(input)
			request.Page = page
			continued, err := search.RequestFingerprint(request, limits)
			if err != nil || continued != first {
				t.Fatalf("cursor token changed query binding: %q/%v != %q", continued, err, first)
			}
		}
	})
}

func hostileRequest(input []byte) search.Request {
	selector := byte(0)
	if len(input) > 0 {
		selector = input[0]
	}
	field := string(input)
	if len(field) == 0 {
		field = "field"
	}
	var query search.Query
	switch selector % 10 {
	case 0:
		query = search.MatchAllQuery{}
	case 1:
		query = search.TermQuery{Field: field, Value: search.StringValue(string(input))}
	case 2:
		query = search.PrefixQuery{Field: field, Prefix: string(input)}
	case 3:
		query = search.FullTextQuery{Fields: []string{field, field + "^2"}, Text: string(input), Analyzer: field, Locale: field}
	case 4:
		query = search.RawExtensionQuery{Adapter: field, Payload: append(json.RawMessage(nil), input...)}
	case 5:
		children := make([]search.Query, min(len(input), 64))
		for index := range children {
			children[index] = search.TermQuery{Field: field, Value: search.StringValue(string(input[index]))}
		}
		query = search.BoolQuery{Should: children, MinimumShouldMatch: int(selector % 4)}
	case 6:
		bound, _ := search.NumberValue("1")
		query = search.RangeQuery{Field: field, GTE: &bound}
	case 7:
		query = search.ExistsQuery{Field: field}
	case 8:
		distance, _ := search.NumberValue("1")
		query = search.GeoDistanceQuery{
			Field: field, Origin: search.GeoPoint{Latitude: float64(int8(selector)), Longitude: float64(int8(selector))},
			DistanceKM: distance,
		}
	default:
		query = search.MatchAllQuery{}
		for range min(len(input), 64) {
			query = search.BoolQuery{Filter: []search.Query{query}}
		}
	}
	bound, _ := search.NumberValue("1")
	upperBound, _ := search.NumberValue("2")
	buckets := make([]search.RangeBucket, min(len(input), 64))
	for index := range buckets {
		buckets[index] = search.RangeBucket{Key: field, From: &bound, To: &upperBound}
	}
	aggregation := search.Aggregation(search.TermsAggregation{Field: field, Size: int(selector)})
	if selector%2 == 0 {
		aggregation = search.RangeAggregation{Field: field, Buckets: buckets}
	}

	request := search.Request{
		Tenant: string(input), Index: field, Query: query,
		Sort:         []search.Sort{{Field: field, Direction: search.Ascending}, {Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Projection:   search.Projection{Includes: []string{field}, Excludes: []string{field + ".secret"}},
		Highlights:   map[string]search.Highlight{field: {FragmentSize: int(selector), MaxFragments: int(selector % 8)}},
		Aggregations: map[string]search.Aggregation{field: aggregation},
		Suggestions:  map[string]search.Suggestion{field: search.PrefixSuggestion{Field: field, Text: string(input), Size: int(selector)}},
	}
	if selector%2 == 0 {
		request.Page = search.CursorPage{Size: int(selector), Cursor: string(input), KeepAlive: time.Duration(selector) * time.Second}
	} else {
		request.Page = search.OffsetPage{Size: int(selector), Offset: len(input) - int(selector)}
	}
	return request
}
