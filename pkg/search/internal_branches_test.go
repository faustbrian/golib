package search

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

type invalidQuery struct{}

func (invalidQuery) queryNode() {}

type invalidAggregation struct{}

func (invalidAggregation) aggregationNode() {}

type invalidSuggestion struct{}

func (invalidSuggestion) suggestionNode() {}

func TestInternalMarkerAndFingerprintModels(t *testing.T) {
	MatchAllQuery{}.queryNode()
	BoolQuery{}.queryNode()
	TermQuery{}.queryNode()
	FullTextQuery{}.queryNode()
	PrefixQuery{}.queryNode()
	RangeQuery{}.queryNode()
	ExistsQuery{}.queryNode()
	GeoDistanceQuery{}.queryNode()
	RawExtensionQuery{}.queryNode()
	TermsAggregation{}.aggregationNode()
	RangeAggregation{}.aggregationNode()
	PrefixSuggestion{}.suggestionNode()

	number, _ := NumberValue("2.5")
	queries := []Query{
		MatchAllQuery{}, BoolQuery{Must: []Query{MatchAllQuery{}}},
		TermQuery{Field: "kind", Value: StringValue("city")},
		FullTextQuery{Fields: []string{"name"}, Text: "hel", Analyzer: "standard", Locale: "fi"},
		PrefixQuery{Field: "name", Prefix: "hel"}, RangeQuery{Field: "population", GTE: &number},
		ExistsQuery{Field: "position"}, GeoDistanceQuery{Field: "position", Origin: GeoPoint{}, DistanceKM: number},
		RawExtensionQuery{Adapter: "opensearch", Payload: json.RawMessage(`{"match_all":{}}`)}, invalidQuery{},
	}
	for _, query := range queries {
		if queryFingerprintModel(query) == nil {
			t.Fatalf("nil model for %T", query)
		}
	}
	_ = queryModels(nil)
	_ = pageFingerprintModel(CursorPage{Size: 1, KeepAlive: time.Second})
	_ = pageFingerprintModel(OffsetPage{Size: 1})
	_ = pageFingerprintModel(struct{}{})
	_ = aggregationFingerprintModel(nil)
	_ = aggregationFingerprintModel(map[string]Aggregation{"terms": TermsAggregation{Field: "kind", Size: 1}, "range": RangeAggregation{Field: "n", Buckets: []RangeBucket{{Key: "x", From: &number}}}, "bad": invalidAggregation{}})
	_ = suggestionFingerprintModel(nil)
	_ = suggestionFingerprintModel(map[string]Suggestion{"prefix": PrefixSuggestion{Field: "name", Text: "h", Size: 1}, "bad": invalidSuggestion{}})
	request := Request{Tenant: "tenant", Index: "index", Query: invalidQuery{}, Page: struct{}{}}
	if _, err := RequestFingerprint(request, DefaultLimits()); !errors.Is(err, ErrInvalidQuery) {
		t.Fatal(err)
	}
	request.Query = RawExtensionQuery{Adapter: "opensearch", Payload: json.RawMessage(`{`)}
	if _, err := RequestFingerprint(request, DefaultLimits()); !errors.Is(err, ErrInvalidQuery) {
		t.Fatal("unencodable fingerprint accepted")
	}
}

func TestInternalQueryValidationBranches(t *testing.T) {
	limits := DefaultLimits()
	number, _ := NumberValue("1")
	base := Request{Tenant: "tenant", Index: "index", Query: MatchAllQuery{}, Sort: []Sort{{Field: "_id", Direction: Ascending}}, Page: OffsetPage{Size: 1}}
	if err := base.Validate(AllCapabilities(), Limits{}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("invalid limits error = %v", err)
	}
	if err := base.Validate(AllCapabilities(), limits); err != nil {
		t.Fatal(err)
	}

	invalidRequests := []Request{
		{}, {Tenant: "t", Index: "i", Query: nil, Page: OffsetPage{Size: 1}},
		{Tenant: strings.Repeat("x", limits.MaxTenantBytes+1), Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 1}},
		{Tenant: "t", Index: strings.Repeat("x", limits.MaxIndexBytes+1), Query: MatchAllQuery{}, Page: OffsetPage{Size: 1}},
		{Tenant: "t", Index: "i", Query: BoolQuery{Must: []Query{nil}}, Page: OffsetPage{Size: 1}},
		{Tenant: "t", Index: "i", Query: BoolQuery{Should: []Query{MatchAllQuery{}}, MinimumShouldMatch: 2}, Page: OffsetPage{Size: 1}},
		{Tenant: "t", Index: "i", Query: FullTextQuery{Fields: []string{""}, Text: "x"}, Page: OffsetPage{Size: 1}},
		{Tenant: "t", Index: "i", Query: ExistsQuery{Field: ""}, Page: OffsetPage{Size: 1}},
		{Tenant: "t", Index: "i", Query: GeoDistanceQuery{Field: "p", DistanceKM: Value{kind: KindNumber, text: "not-a-number"}}, Page: OffsetPage{Size: 1}},
		{Tenant: "t", Index: "i", Query: GeoDistanceQuery{Field: "p", DistanceKM: Value{kind: KindNumber, text: "0"}}, Page: OffsetPage{Size: 1}},
		{Tenant: "t", Index: "i", Query: invalidQuery{}, Page: OffsetPage{Size: 1}},
		{Tenant: "t", Index: "i", Query: RangeQuery{Field: "n", GT: &number}, Sort: []Sort{{Field: "", Direction: Ascending}}, Page: OffsetPage{Size: 1}},
		{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 0}},
		{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 1, Offset: -1}},
		{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: struct{}{}},
		{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 1}, Highlights: map[string]Highlight{"bad": {}}},
		{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 1}, Aggregations: map[string]Aggregation{"": TermsAggregation{Field: "f", Size: 1}}},
		{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 1}, Aggregations: map[string]Aggregation{"a": RangeAggregation{Field: "f"}}},
		{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 1}, Aggregations: map[string]Aggregation{"a": RangeAggregation{Field: "f", Buckets: []RangeBucket{{}}}}},
		{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 1}, Suggestions: map[string]Suggestion{"": PrefixSuggestion{Field: "f", Text: "x", Size: 1}}},
		{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 1}, Suggestions: map[string]Suggestion{"a": invalidSuggestion{}}},
	}
	for i, request := range invalidRequests {
		if request.Validate(AllCapabilities(), limits) == nil {
			t.Fatalf("request %d accepted", i)
		}
	}

	capabilityCases := []struct {
		query   Query
		disable func(*Capabilities)
	}{
		{BoolQuery{Must: []Query{MatchAllQuery{}}}, func(c *Capabilities) { c.Boolean = false }},
		{TermQuery{Field: "f", Value: StringValue("x")}, func(c *Capabilities) { c.Term = false }},
		{FullTextQuery{Fields: []string{"f"}, Text: "x"}, func(c *Capabilities) { c.FullText = false }},
		{PrefixQuery{Field: "f", Prefix: "x"}, func(c *Capabilities) { c.Prefix = false }},
		{RangeQuery{Field: "f", GT: &number}, func(c *Capabilities) { c.Range = false }},
		{ExistsQuery{Field: "f"}, func(c *Capabilities) { c.Exists = false }},
		{GeoDistanceQuery{Field: "p", DistanceKM: number}, func(c *Capabilities) { c.Geo = false }},
		{RawExtensionQuery{Adapter: "opensearch", Payload: json.RawMessage(`{}`)}, func(c *Capabilities) { c.RawExtensions = false }},
	}
	for _, test := range capabilityCases {
		capabilities := AllCapabilities()
		test.disable(&capabilities)
		request := base
		request.Query = test.query
		if !errors.Is(request.Validate(capabilities, limits), ErrUnsupported) {
			t.Fatalf("%T capability accepted", test.query)
		}
	}
	capabilities := AllCapabilities()
	capabilities.Offset = false
	if !errors.Is(base.Validate(capabilities, limits), ErrUnsupported) {
		t.Fatal("offset capability accepted")
	}
	capabilities = AllCapabilities()
	capabilities.Cursor = false
	cursorRequest := base
	cursorRequest.Page = CursorPage{Size: 1, KeepAlive: time.Second}
	if !errors.Is(cursorRequest.Validate(capabilities, limits), ErrUnsupported) {
		t.Fatal("cursor capability accepted")
	}
	if !validAggregation(RangeAggregation{Field: "f", Buckets: []RangeBucket{{Key: "k", From: &number}}}, limits) {
		t.Fatal("valid range aggregation rejected")
	}
	if validAggregation(invalidAggregation{}, limits) {
		t.Fatal("invalid aggregation accepted")
	}
	for _, mutate := range []func(*Capabilities){func(c *Capabilities) { c.Projection = false }, func(c *Capabilities) { c.Highlight = false }, func(c *Capabilities) { c.Aggregation = false }, func(c *Capabilities) { c.Suggestion = false }} {
		capabilities = AllCapabilities()
		mutate(&capabilities)
		request := base
		request.Projection.Includes = []string{"f"}
		request.Highlights = map[string]Highlight{"f": {FragmentSize: 1, MaxFragments: 1}}
		request.Aggregations = map[string]Aggregation{"a": TermsAggregation{Field: "f", Size: 1}}
		request.Suggestions = map[string]Suggestion{"s": PrefixSuggestion{Field: "f", Text: "x", Size: 1}}
		if !errors.Is(request.Validate(capabilities, limits), ErrUnsupported) {
			t.Fatal("optional capability accepted")
		}
	}
	deep := Query(MatchAllQuery{})
	for range limits.MaxQueryDepth {
		deep = BoolQuery{Must: []Query{deep}}
	}
	request := base
	request.Query = deep
	if request.Validate(AllCapabilities(), limits) == nil {
		t.Fatal("deep query accepted")
	}
}

func TestInternalRequestInputBudgetBoundaries(t *testing.T) {
	limits := DefaultLimits()
	capabilities := AllCapabilities()
	tooMany := make([]Query, limits.MaxQueryClauses+1)
	for index := range tooMany {
		tooMany[index] = MatchAllQuery{}
	}
	for _, query := range []Query{BoolQuery{Must: tooMany}, BoolQuery{Must: []Query{nil}}, BoolQuery{Must: []Query{invalidQuery{}}}, invalidQuery{}} {
		clauses := 0
		if err := validateQuery(query, capabilities, limits, 1, &clauses); !errors.Is(err, ErrInvalidQuery) {
			t.Fatal(query, err)
		}
	}

	base := Request{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 1}}
	tiny := limits
	tiny.MaxQueryBytes = 2
	for _, mutate := range []func(*Request){
		func(request *Request) { request.Sort = []Sort{{Field: "f", Direction: Ascending}} },
		func(request *Request) {
			request.Page = CursorPage{Size: 1, Cursor: "x", KeepAlive: time.Second}
		},
		func(request *Request) { request.Projection.Includes = []string{"f"} },
		func(request *Request) {
			request.Highlights = map[string]Highlight{"f": {FragmentSize: 1, MaxFragments: 1}}
		},
		func(request *Request) {
			request.Aggregations = map[string]Aggregation{"a": TermsAggregation{Field: "f", Size: 1}}
		},
		func(request *Request) {
			request.Suggestions = map[string]Suggestion{"s": PrefixSuggestion{Field: "f", Text: "x", Size: 1}}
		},
	} {
		request := base
		mutate(&request)
		if requestInputsWithinBudget(request, tiny) {
			t.Fatalf("input budget accepted %#v", request)
		}
	}

	budget := requestInputBudget{bytes: 1, nodes: 1, maxCollection: 1}
	if budget.query(BoolQuery{Must: []Query{MatchAllQuery{}}}, 1) {
		t.Fatal("depth exhaustion accepted")
	}
	budget = requestInputBudget{bytes: 1, nodes: 0, maxCollection: 1}
	if budget.query(MatchAllQuery{}, 1) {
		t.Fatal("node exhaustion accepted")
	}
	budget = requestInputBudget{bytes: 0, nodes: 1, maxCollection: 1}
	if budget.query(FullTextQuery{Fields: []string{"f"}}, 1) {
		t.Fatal("full-text field over budget accepted")
	}
	budget = requestInputBudget{bytes: 0, nodes: 1, maxCollection: 1}
	if budget.query(RangeQuery{Field: "f"}, 1) {
		t.Fatal("range field over budget accepted")
	}
	budget = requestInputBudget{bytes: 1, nodes: 1, maxCollection: 1}
	large := StringValue("xx")
	if budget.query(RangeQuery{Field: "f", GT: &large}, 1) {
		t.Fatal("range value over budget accepted")
	}
	budget = requestInputBudget{bytes: 0}
	if budget.aggregation(RangeAggregation{Field: "f"}) {
		t.Fatal("range aggregation field over budget accepted")
	}
	budget = requestInputBudget{bytes: 1}
	if budget.aggregation(RangeAggregation{Buckets: []RangeBucket{{Key: "xx"}}}) {
		t.Fatal("range bucket key over budget accepted")
	}
	budget = requestInputBudget{bytes: 1}
	if budget.aggregation(RangeAggregation{Buckets: []RangeBucket{{Key: "k", From: &large}}}) {
		t.Fatal("range bucket value over budget accepted")
	}
	budget = requestInputBudget{bytes: 1}
	if budget.aggregation(invalidAggregation{}) {
		t.Fatal("invalid aggregation accepted")
	}
	zeroBytes := limits
	zeroBytes.MaxQueryBytes = 0
	if requestInputsWithinBudget(Request{Tenant: "t"}, zeroBytes) {
		t.Fatal("tenant over budget accepted")
	}
	if requestInputsWithinBudget(Request{Index: "i"}, zeroBytes) {
		t.Fatal("index over budget accepted")
	}
	for _, request := range []Request{
		{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 1}, Highlights: map[string]Highlight{"f": {PreTag: "x"}}},
		{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 1}, Highlights: map[string]Highlight{"f": {PreTag: "x", PostTag: "x"}}},
	} {
		tinyHighlight := limits
		tinyHighlight.MaxQueryBytes = 3
		if request.Highlights["f"].PostTag != "" {
			tinyHighlight.MaxQueryBytes = 4
		}
		if requestInputsWithinBudget(request, tinyHighlight) {
			t.Fatal("highlight tag over budget accepted")
		}
	}
	invalidAggregationRequest := base
	invalidAggregationRequest.Aggregations = map[string]Aggregation{"a": invalidAggregation{}}
	if requestInputsWithinBudget(invalidAggregationRequest, limits) {
		t.Fatal("invalid aggregation input accepted")
	}
	for _, query := range []Query{
		TermQuery{Field: "f"},
		FullTextQuery{Analyzer: "x"},
		PrefixQuery{Field: "f"},
		GeoDistanceQuery{Field: "f"},
		RawExtensionQuery{Adapter: "x"},
	} {
		budget = requestInputBudget{bytes: 0, nodes: 1, maxCollection: 1}
		if budget.query(query, 1) {
			t.Fatalf("query input over budget accepted: %T", query)
		}
	}
	budget = requestInputBudget{bytes: 1, nodes: 1, maxCollection: 1}
	if budget.query(FullTextQuery{Analyzer: "x", Locale: "x"}, 1) {
		t.Fatal("locale over budget accepted")
	}
	budget = requestInputBudget{bytes: 0}
	if budget.suggestion(PrefixSuggestion{Field: "f"}) {
		t.Fatal("suggestion field over budget accepted")
	}
	if validTermValue(NullValue()) {
		t.Fatal("null term accepted")
	}
}

func TestInternalValueSchemaDocumentAndLimitBranches(t *testing.T) {
	limits := DefaultLimits()
	if limits.Validate() != nil {
		t.Fatal("default limits invalid")
	}
	for i := range 16 {
		invalid := limits
		switch i {
		case 0:
			invalid.MaxTenantBytes = 0
		case 1:
			invalid.MaxIndexBytes = 0
		case 2:
			invalid.MaxIDBytes = 0
		case 3:
			invalid.MaxSourceBytes = 0
		case 4:
			invalid.MaxQueryBytes = 0
		case 5:
			invalid.MaxBulkItems = 0
		case 6:
			invalid.MaxBulkBytes = 0
		case 7:
			invalid.MaxPageItems = 0
		case 8:
			invalid.MaxPages = 0
		case 9:
			invalid.MaxResultBytes = 0
		case 10:
			invalid.MaxCursorDuration = 0
		case 11:
			invalid.MaxQueryDepth = 0
		case 12:
			invalid.MaxQueryClauses = 0
		case 13:
			invalid.MaxJSONDepth = 0
		case 14:
			invalid.MaxJSONNodes = 0
		case 15:
			invalid.MaxPages = int(^uint(0) >> 1)
			invalid.MaxPageItems = 2
		}
		if invalid.Validate() == nil {
			t.Fatalf("limit %d accepted", i)
		}
	}
	maximumProduct := limits
	maximumProduct.MaxPageItems = 2
	maximumProduct.MaxPages = int(^uint(0)>>1) / maximumProduct.MaxPageItems
	if err := maximumProduct.Validate(); err != nil {
		t.Fatalf("largest non-overflowing traversal product rejected: %v", err)
	}
	values := []Value{NullValue(), StringValue(""), BoolValue(true), TimeValue(time.Now()), ArrayValue(nil), ObjectValue(nil)}
	number, _ := NumberValue("-10.25")
	geo, _ := GeoValue(90, -180)
	values = append(values, number, geo)
	for _, value := range values {
		_ = value.Kind()
		if _, err := json.Marshal(value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (Value{kind: 255}).MarshalJSON(); err == nil {
		t.Fatal("unknown kind accepted")
	}
	_ = cloneFields(nil)
	_ = cloneValues(nil)

	docCases := []struct {
		tenant, index, id string
		version           uint64
		source            json.RawMessage
	}{
		{"", "i", "id", 1, json.RawMessage(`{}`)}, {strings.Repeat("t", limits.MaxTenantBytes+1), "i", "id", 1, json.RawMessage(`{}`)}, {"t", "", "id", 1, json.RawMessage(`{}`)}, {"t", strings.Repeat("i", limits.MaxIndexBytes+1), "id", 1, json.RawMessage(`{}`)}, {"t", "i", "", 1, json.RawMessage(`{}`)}, {"t", "i", strings.Repeat("i", limits.MaxIDBytes+1), 1, json.RawMessage(`{}`)}, {"t", "i", "id", 0, json.RawMessage(`{}`)}, {"t", "i", "id", 1, nil}, {"t", "i", "id", 1, json.RawMessage(`[]`)}, {"t", "i", "id", 1, json.RawMessage(`{`)},
	}
	for i, c := range docCases {
		if _, err := NewDocument(c.tenant, c.index, c.id, c.version, c.source, limits); err == nil {
			t.Fatalf("document %d accepted", i)
		}
	}
	small := limits
	small.MaxSourceBytes = 1
	if _, err := NewDocument("t", "i", "id", 1, json.RawMessage(`{}`), small); !errors.Is(err, ErrSourceTooLarge) {
		t.Fatal(err)
	}

	valid, _ := NewIndexDefinition("index-v1", json.RawMessage(`{"b":2,"a":1}`), json.RawMessage(`{}`), limits)
	_ = valid.Mappings()
	_ = valid.Settings()
	_ = valid.Name()
	_ = valid.Fingerprint()
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`[]`), json.RawMessage(`{} {}`), json.RawMessage(`{`)} {
		if _, err := NewIndexDefinition("index-v1", raw, json.RawMessage(`{}`), limits); err == nil {
			t.Fatalf("schema %q accepted", raw)
		}
	}
	if _, err := NewIndexDefinition("index-v1", json.RawMessage(`{}`), json.RawMessage(`[]`), limits); err == nil {
		t.Fatal("invalid mappings accepted")
	}
	other, _ := NewIndexDefinition("index-v2", json.RawMessage(`{"x":1}`), json.RawMessage(`{"y":1}`), limits)
	compatibility := CompareDefinitions(valid, other)
	if len(compatibility.Reasons) != 2 {
		t.Fatalf("reasons=%v", compatibility.Reasons)
	}
}

func signedCursorForTest(codec *CursorCodec, envelope cursorEnvelope) string {
	payload, _ := json.Marshal(envelope)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(codec.sign(payload))
}

func TestInternalCursorBranches(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	codec, _ := NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 4096)
	binding := CursorBinding{Tenant: "t", Index: "i", QueryFingerprint: "q", IndexFingerprint: "f"}
	for _, state := range []CursorState{{}, {PointInTime: "p", SortValues: []json.RawMessage{json.RawMessage(`1`)}, ExpiresAt: now}, {PointInTime: "p", SortValues: []json.RawMessage{json.RawMessage(`1`)}, Page: -1, ExpiresAt: now.Add(time.Second)}} {
		if _, err := codec.Encode(binding, state); err == nil {
			t.Fatal("invalid state encoded")
		}
	}
	if _, err := codec.Encode(CursorBinding{}, CursorState{PointInTime: "p", SortValues: []json.RawMessage{json.RawMessage(`1`)}, ExpiresAt: now.Add(time.Second)}); err == nil {
		t.Fatal("invalid binding encoded")
	}
	for _, token := range []string{"a.b.c", ".a", "a.", "!x.YQ", "YQ.!x"} {
		if _, err := codec.Decode(token, binding, DefaultLimits()); err == nil {
			t.Fatalf("token %q accepted", token)
		}
	}
	base := cursorEnvelope{Version: 1, Tenant: "t", Index: "i", QueryFingerprint: "q", IndexFingerprint: "f", PointInTime: "p", SortValues: []json.RawMessage{json.RawMessage(`1`)}, ExpiresUnixNano: now.Add(time.Second).UnixNano()}
	cases := []cursorEnvelope{base}
	cases[0].Version = 2
	bad := base
	bad.PointInTime = ""
	cases = append(cases, bad)
	bad = base
	bad.SortValues = nil
	cases = append(cases, bad)
	bad = base
	bad.Items = -1
	cases = append(cases, bad)
	bad = base
	bad.Bytes = -1
	cases = append(cases, bad)
	for _, envelope := range cases {
		if _, err := codec.Decode(signedCursorForTest(codec, envelope), binding, DefaultLimits()); err == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
	invalidRaw := CursorState{PointInTime: "p", SortValues: []json.RawMessage{json.RawMessage(`{`)}, ExpiresAt: now.Add(time.Second)}
	if _, err := codec.Encode(binding, invalidRaw); err == nil {
		t.Fatal("invalid raw sort encoded")
	}
	smallCodec, _ := NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 8)
	if _, err := smallCodec.Encode(binding, CursorState{PointInTime: "p", SortValues: []json.RawMessage{json.RawMessage(`1`)}, ExpiresAt: now.Add(time.Second)}); err == nil {
		t.Fatal("oversized cursor encoded")
	}
}

type recordingIndexer struct {
	outcome ItemOutcome
	err     error
}

func (r recordingIndexer) Write(context.Context, WriteOperation, RefreshPolicy) (ItemOutcome, error) {
	return r.outcome, r.err
}
func (r recordingIndexer) Bulk(context.Context, BulkRequest) (BulkResult, error) {
	return BulkResult{}, r.err
}

func TestInternalWriteProjectionAndResultBranches(t *testing.T) {
	limits := DefaultLimits()
	doc, _ := NewDocument("t", "i", "id", 1, json.RawMessage(`{}`), limits)
	for _, operation := range []WriteOperation{IndexDocument(doc), UpdateDocument(doc), UpsertDocument(doc), DeleteDocument("t", "i", "id", 2)} {
		if operation.Action == "" {
			t.Fatal("empty action")
		}
	}
	invalidOps := []WriteOperation{{}, {Action: ActionIndex, Tenant: "t", Index: "i", ID: "id", Version: 1, Source: json.RawMessage(`[]`)}, {Action: ActionDelete, Tenant: "t", Index: "i", ID: "id", Version: 1, Source: json.RawMessage(`{}`)}, {Action: "bad", Tenant: "t", Index: "i", ID: "id", Version: 1}}
	for _, op := range invalidOps {
		if (BulkRequest{Operations: []WriteOperation{op}, Refresh: RefreshNone}).Validate(AllCapabilities(), limits) == nil {
			t.Fatal("invalid op accepted")
		}
	}
	request := BulkRequest{Operations: []WriteOperation{IndexDocument(doc)}, Refresh: "bad"}
	if request.Validate(AllCapabilities(), limits) == nil {
		t.Fatal("refresh accepted")
	}
	capabilities := AllCapabilities()
	capabilities.ExternalVersion = false
	if !errors.Is((BulkRequest{Operations: []WriteOperation{IndexDocument(doc)}}).Validate(capabilities, limits), ErrUnsupported) {
		t.Fatal("capability accepted")
	}
	small := limits
	small.MaxBulkBytes = 1
	if !errors.Is((BulkRequest{Operations: []WriteOperation{IndexDocument(doc)}, Refresh: RefreshNone}).Validate(AllCapabilities(), small), ErrBulkLimit) {
		t.Fatal("bulk byte limit accepted")
	}
	spaced := IndexDocument(doc)
	spaced.Source = json.RawMessage(" \n{}\t ")
	if err := (BulkRequest{Operations: []WriteOperation{spaced}, Refresh: RefreshNone}).Validate(AllCapabilities(), limits); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBulkResult(nil); err == nil {
		t.Fatal("empty result accepted")
	}
	for _, state := range []OutcomeState{OutcomeApplied, OutcomeNotFound, OutcomeVersionConflict, OutcomeRejected, OutcomeFailed, OutcomeUnknown} {
		if _, err := NewBulkResult([]ItemOutcome{{Position: 0, ID: "id", Action: ActionUpsert, State: state}}); err != nil {
			t.Fatal(err)
		}
	}
	allApplied, _ := NewBulkResult([]ItemOutcome{{Position: 0, ID: "id", Action: ActionIndex, State: OutcomeApplied}})
	if allApplied.Partial() || allApplied.HasUnknown() {
		t.Fatal("applied marked partial")
	}
	for _, item := range []ItemOutcome{{Position: 0, ID: "", Action: ActionIndex, State: OutcomeApplied}, {Position: 0, ID: "id", Action: "bad", State: OutcomeApplied}, {Position: 0, ID: "id", Action: ActionIndex, State: "bad"}} {
		if _, err := NewBulkResult([]ItemOutcome{item}); err == nil {
			t.Fatal("invalid outcome accepted")
		}
	}

	if _, err := NewProjectionEvent("t", "i", "id", 1, ProjectionDelete, json.RawMessage(`{}`), "key", limits); err == nil {
		t.Fatal("delete source accepted")
	}
	if _, err := NewProjectionEvent("t", "i", "id", 1, ProjectionDelete, nil, "", limits); err == nil {
		t.Fatal("empty idempotency accepted")
	}
	if _, err := NewProjectionEvent("t", "i", "id", 1, ProjectionKind("bad"), nil, "key", limits); err == nil {
		t.Fatal("projection kind accepted")
	}
	event, _ := NewProjectionEvent("t", "i", "id", 1, ProjectionUpsert, json.RawMessage(`{}`), "key", limits)
	if event.IdempotencyKey() != "key" {
		t.Fatal("key mismatch")
	}
	if _, err := NewProjectionConsumer(nil); err == nil {
		t.Fatal("nil indexer accepted")
	}
	consumer, _ := NewProjectionConsumer(recordingIndexer{outcome: ItemOutcome{ID: "id"}})
	if outcome, err := consumer.Handle(t.Context(), event, RefreshNone); err != nil || outcome.ID != "id" {
		t.Fatal(err)
	}

	validResult, _ := NewResult(nil, Total{}, nil, map[string]json.RawMessage{"s": json.RawMessage(`{}`)}, Diagnostics{Warnings: []string{"w"}}, "")
	_ = validResult.Suggestions()
	_ = validResult.Diagnostics()
	inf := math.Inf(1)
	invalidHits := []Hit{{}, {Index: "i", ID: "id", Score: &inf}, {Index: "i", ID: "id", Source: json.RawMessage(`{`)}, {Index: "i", ID: "id", SortValues: []json.RawMessage{json.RawMessage(`{`)}}}
	for _, hit := range invalidHits {
		if _, err := NewResult([]Hit{hit}, Total{}, nil, nil, Diagnostics{}, ""); err == nil {
			t.Fatal("invalid hit accepted")
		}
	}
	for _, diagnostics := range []Diagnostics{{Took: -1}, {Shards: ShardDiagnostics{Total: -1}}, {Shards: ShardDiagnostics{Successful: -1}}, {Shards: ShardDiagnostics{Skipped: -1}}, {Shards: ShardDiagnostics{Failed: -1}}, {Shards: ShardDiagnostics{Total: 2, Successful: 1}}} {
		if _, err := NewResult(nil, Total{}, nil, nil, diagnostics, ""); err == nil {
			t.Fatal("invalid diagnostics accepted")
		}
	}
	if _, err := NewResult(nil, Total{Relation: "bad"}, nil, nil, Diagnostics{}, ""); err == nil {
		t.Fatal("invalid relation accepted")
	}
}
