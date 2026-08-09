package search

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMutationContractExactScalarBoundaries(t *testing.T) {
	limits := DefaultLimits()
	object := json.RawMessage(`{}`)
	for _, test := range []struct {
		name              string
		tenant, index, id string
		source            json.RawMessage
		want              error
	}{
		{"tenant exact", strings.Repeat("t", limits.MaxTenantBytes), "i", "id", object, nil},
		{"tenant over", strings.Repeat("t", limits.MaxTenantBytes+1), "i", "id", object, ErrTenantTooLarge},
		{"index exact", "t", strings.Repeat("i", limits.MaxIndexBytes), "id", object, nil},
		{"index over", "t", strings.Repeat("i", limits.MaxIndexBytes+1), "id", object, ErrIndexTooLarge},
		{"id exact", "t", "i", strings.Repeat("i", limits.MaxIDBytes), object, nil},
		{"id over", "t", "i", strings.Repeat("i", limits.MaxIDBytes+1), object, ErrIDTooLarge},
		{"source exact", "t", "i", "id", json.RawMessage("{" + strings.Repeat(" ", limits.MaxSourceBytes-2) + "}"), nil},
		{"source over", "t", "i", "id", json.RawMessage("{" + strings.Repeat(" ", limits.MaxSourceBytes-1) + "}"), ErrSourceTooLarge},
		{"source short", "t", "i", "id", json.RawMessage(`{`), ErrInvalidSource},
		{"source first", "t", "i", "id", json.RawMessage(`[]`), ErrInvalidSource},
		{"source last", "t", "i", "id", json.RawMessage(`{]`), ErrInvalidSource},
		{"source syntax", "t", "i", "id", json.RawMessage(`{"x":}`), ErrInvalidSource},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewDocument(test.tenant, test.index, test.id, 1, test.source, limits)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}

	validNumber := "1" + strings.Repeat("0", MaxNumberBytes-1)
	if _, err := NumberValue(validNumber); err != nil {
		t.Fatal(err)
	}
	if _, err := NumberValue(validNumber + "0"); err == nil {
		t.Fatal("oversized number accepted")
	}
	if _, err := NumberValue("+"); err == nil {
		t.Fatal("invalid number grammar accepted")
	}
	for _, point := range [][2]float64{{-90, -180}, {90, 180}} {
		if _, err := GeoValue(point[0], point[1]); err != nil {
			t.Fatal(point, err)
		}
	}
	for _, point := range [][2]float64{{math.NaN(), 0}, {math.Inf(1), 0}, {math.Nextafter(-90, math.Inf(-1)), 0}, {math.Nextafter(90, math.Inf(1)), 0}, {0, math.NaN()}, {0, math.Inf(1)}, {0, math.Nextafter(-180, math.Inf(-1))}, {0, math.Nextafter(180, math.Inf(1))}} {
		if _, err := GeoValue(point[0], point[1]); err == nil {
			t.Fatalf("invalid point accepted: %v", point)
		}
	}

	settings := json.RawMessage("{" + strings.Repeat(" ", 8) + "}")
	mappings := json.RawMessage(`{}`)
	schemaLimits := limits
	schemaLimits.MaxSourceBytes = len(settings) + len(mappings)
	if _, err := NewIndexDefinition("valid", settings, mappings, schemaLimits); err != nil {
		t.Fatal(err)
	}
	schemaLimits.MaxSourceBytes--
	if _, err := NewIndexDefinition("valid", settings, mappings, schemaLimits); !errors.Is(err, ErrSchemaLimit) {
		t.Fatal(err)
	}
	if validIndexName(strings.Repeat("a", 256)) || !validIndexName(strings.Repeat("a", 255)) {
		t.Fatal("index-name byte boundary changed")
	}
}

func TestMutationContractCursorBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	key := []byte(strings.Repeat("k", 32))
	if _, err := NewCursorCodec(key, func() time.Time { return now }, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCursorCodec(key[:31], func() time.Time { return now }, 1); !errors.Is(err, ErrInvalidCursorCodec) {
		t.Fatal(err)
	}
	if _, err := NewCursorCodec(key, func() time.Time { return now }, 0); !errors.Is(err, ErrInvalidCursorCodec) {
		t.Fatal(err)
	}
	codec, _ := NewCursorCodec(key, func() time.Time { return now }, 4096)
	binding := CursorBinding{Tenant: "t", Index: "i", QueryFingerprint: "q", IndexFingerprint: "f"}
	base := CursorState{PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`1`)}, ExpiresAt: now.Add(time.Nanosecond)}
	inputBytes := len(binding.Tenant) + len(binding.Index) + len(binding.QueryFingerprint) + len(binding.IndexFingerprint) + len(base.PointInTime) + len(base.SortValues) + len(base.SortValues[0])
	if !cursorInputsWithinBudget(binding, base, inputBytes) || cursorInputsWithinBudget(binding, base, inputBytes-1) {
		t.Fatal("cursor input byte boundary changed")
	}
	if cursorInputsWithinBudget(binding, base, 0) {
		t.Fatal("cursor fixed input over budget accepted")
	}
	if !cursorInputsWithinBudget(CursorBinding{Tenant: "x"}, CursorState{}, 1) {
		t.Fatal("exact fixed cursor input rejected")
	}
	twoSorts := base
	twoSorts.SortValues = []json.RawMessage{json.RawMessage(`1`), json.RawMessage(`22`)}
	fixedBytes := len(binding.Tenant) + len(binding.Index) + len(binding.QueryFingerprint) + len(binding.IndexFingerprint) + len(twoSorts.PointInTime) + len(twoSorts.SortValues)
	if cursorInputsWithinBudget(binding, twoSorts, fixedBytes+1) {
		t.Fatal("cursor remaining-byte accounting accepted overflow")
	}
	invalidSort := base
	invalidSort.SortValues = []json.RawMessage{json.RawMessage(`{`)}
	if cursorInputsWithinBudget(binding, invalidSort, 4096) {
		t.Fatal("invalid cursor sort value accepted")
	}
	token, err := codec.Encode(binding, base)
	if err != nil {
		t.Fatal(err)
	}
	exactCodec, _ := NewCursorCodec(key, func() time.Time { return now }, len(token))
	if _, err := exactCodec.Encode(binding, base); err != nil {
		t.Fatal(err)
	}
	if _, err := exactCodec.Decode(token, binding, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	shortCodec, _ := NewCursorCodec(key, func() time.Time { return now }, len(token)-1)
	if _, err := shortCodec.Encode(binding, base); !errors.Is(err, ErrInvalidCursor) {
		t.Fatal(err)
	}
	for _, state := range []CursorState{
		{PointInTime: "pit", SortValues: base.SortValues, Page: -1, ExpiresAt: base.ExpiresAt},
		{PointInTime: "pit", SortValues: base.SortValues, Items: -1, ExpiresAt: base.ExpiresAt},
		{PointInTime: "pit", SortValues: base.SortValues, Bytes: -1, ExpiresAt: base.ExpiresAt},
		{PointInTime: "pit", SortValues: base.SortValues, ExpiresAt: now},
	} {
		if _, err := codec.Encode(binding, state); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("state %#v accepted: %v", state, err)
		}
	}
	for name, invalid := range map[string]CursorBinding{
		"tenant":            {Index: "i", QueryFingerprint: "q", IndexFingerprint: "f"},
		"index":             {Tenant: "t", QueryFingerprint: "q", IndexFingerprint: "f"},
		"query":             {Tenant: "t", Index: "i", IndexFingerprint: "f"},
		"index fingerprint": {Tenant: "t", Index: "i", QueryFingerprint: "q"},
	} {
		if _, err := codec.Encode(invalid, base); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("%s binding accepted: %v", name, err)
		}
	}
	for name, changed := range map[string]CursorBinding{
		"tenant":            {Tenant: "other", Index: "i", QueryFingerprint: "q", IndexFingerprint: "f"},
		"index":             {Tenant: "t", Index: "other", QueryFingerprint: "q", IndexFingerprint: "f"},
		"query fingerprint": {Tenant: "t", Index: "i", QueryFingerprint: "other", IndexFingerprint: "f"},
	} {
		if _, err := codec.Decode(token, changed, DefaultLimits()); !errors.Is(err, ErrCursorBinding) {
			t.Fatalf("%s mismatch error = %v", name, err)
		}
	}

	limits := DefaultLimits()
	envelope := cursorEnvelope{Version: 1, Tenant: "t", Index: "i", QueryFingerprint: "q", IndexFingerprint: "f", PointInTime: "pit", SortValues: base.SortValues, Page: limits.MaxPages, Items: limits.MaxPages * limits.MaxPageItems, Bytes: limits.MaxResultBytes, ExpiresUnixNano: now.Add(limits.MaxCursorDuration).UnixNano()}
	exact := signedCursorForTest(codec, envelope)
	decoded, err := codec.Decode(exact, binding, limits)
	if err != nil || decoded.Page != limits.MaxPages || decoded.Items != limits.MaxPages*limits.MaxPageItems || decoded.Bytes != limits.MaxResultBytes {
		t.Fatal(decoded, err)
	}
	zeroEnvelope := envelope
	zeroEnvelope.Page, zeroEnvelope.Items, zeroEnvelope.Bytes = 0, 0, 0
	if _, err := codec.Decode(signedCursorForTest(codec, zeroEnvelope), binding, limits); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*cursorEnvelope){
		func(v *cursorEnvelope) { v.Page = -1 }, func(v *cursorEnvelope) { v.Items = -1 }, func(v *cursorEnvelope) { v.Bytes = -1 },
		func(v *cursorEnvelope) { v.Page = limits.MaxPages + 1 }, func(v *cursorEnvelope) { v.Items = limits.MaxPages*limits.MaxPageItems + 1 }, func(v *cursorEnvelope) { v.Bytes = limits.MaxResultBytes + 1 },
		func(v *cursorEnvelope) {
			v.ExpiresUnixNano = now.Add(limits.MaxCursorDuration + time.Nanosecond).UnixNano()
		},
	} {
		candidate := envelope
		mutate(&candidate)
		if _, err := codec.Decode(signedCursorForTest(codec, candidate), binding, limits); !errors.Is(err, ErrPageLimit) {
			t.Fatal(candidate, err)
		}
	}
	for _, malformed := range []string{"", ".x", "x.", "x.y.z", "=.x", "e30.=", "e30.invalid"} {
		if _, err := codec.Decode(malformed, binding, limits); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("cursor %q error = %v", malformed, err)
		}
	}
	payloadText, signatureText, _ := strings.Cut(token, ".")
	if len(payloadText)%4 == 0 {
		base.PointInTime += "x"
		token, err = codec.Encode(binding, base)
		if err != nil {
			t.Fatal(err)
		}
		payloadText, signatureText, _ = strings.Cut(token, ".")
	}
	for _, malformed := range []string{nonCanonicalRawBase64(t, payloadText) + "." + signatureText, payloadText + "." + nonCanonicalRawBase64(t, signatureText)} {
		if _, err := codec.Decode(malformed, binding, limits); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("non-canonical cursor accepted: %v", err)
		}
	}
}

func nonCanonicalRawBase64(t *testing.T, value string) string {
	t.Helper()
	if _, err := base64.RawURLEncoding.DecodeString(value); err != nil {
		t.Fatal(err)
	}
	remainder := len(value) % 4
	if remainder != 2 && remainder != 3 {
		t.Fatalf("base64 value has no unused tail bits: %d", remainder)
	}
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := strings.IndexByte(alphabet, value[len(value)-1])
	return value[:len(value)-1] + string(alphabet[last+1])
}

func TestMutationContractRequestBoundaries(t *testing.T) {
	limits := DefaultLimits()
	capabilities := AllCapabilities()
	base := Request{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Sort: []Sort{{Field: "_id", Direction: Ascending}}, Page: CursorPage{Size: 1, KeepAlive: time.Nanosecond}}
	valid := []Request{base}
	tenantExact := base
	tenantExact.Tenant = strings.Repeat("t", limits.MaxTenantBytes)
	valid = append(valid, tenantExact)
	indexExact := base
	indexExact.Index = strings.Repeat("i", limits.MaxIndexBytes)
	valid = append(valid, indexExact)
	pageExact := base
	pageExact.Page = CursorPage{Size: limits.MaxPageItems, KeepAlive: time.Nanosecond}
	valid = append(valid, pageExact)
	durationExact := base
	durationExact.Page = CursorPage{Size: 1, KeepAlive: limits.MaxCursorDuration}
	valid = append(valid, durationExact)
	offsetExact := base
	offsetExact.Page = OffsetPage{Size: 2, Offset: limits.MaxPages*limits.MaxPageItems - 2}
	valid = append(valid, offsetExact)
	offsetSizeExact := base
	offsetSizeExact.Page = OffsetPage{Size: limits.MaxPageItems, Offset: 0}
	valid = append(valid, offsetSizeExact)
	for i, request := range valid {
		if err := request.Validate(capabilities, limits); err != nil {
			t.Fatalf("valid request %d: %v", i, err)
		}
	}
	for _, page := range []any{CursorPage{Size: 0, KeepAlive: time.Nanosecond}, CursorPage{Size: limits.MaxPageItems + 1, KeepAlive: time.Nanosecond}, CursorPage{Size: 1}, CursorPage{Size: 1, KeepAlive: limits.MaxCursorDuration + time.Nanosecond}, OffsetPage{Size: 0}, OffsetPage{Size: limits.MaxPageItems + 1}, OffsetPage{Size: 1, Offset: -1}, OffsetPage{Size: 2, Offset: limits.MaxPages*limits.MaxPageItems - 1}, OffsetPage{Size: 2, Offset: int(^uint(0) >> 1)}} {
		request := base
		request.Page = page
		if err := request.Validate(capabilities, limits); !errors.Is(err, ErrPageLimit) {
			t.Fatalf("page %#v error = %v", page, err)
		}
	}
	payload, err := requestFingerprintPayload(base)
	if err != nil {
		t.Fatal(err)
	}
	exactQuery := limits
	exactQuery.MaxQueryBytes = len(payload)
	if err := base.Validate(capabilities, exactQuery); err != nil {
		t.Fatal(err)
	}
	exactQuery.MaxQueryBytes--
	if err := base.Validate(capabilities, exactQuery); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("query byte boundary error = %v", err)
	}
	for _, highlights := range []map[string]Highlight{{"": {FragmentSize: 1, MaxFragments: 1}}, {"f": {FragmentSize: 0, MaxFragments: 1}}, {"f": {FragmentSize: 1, MaxFragments: 0}}} {
		request := base
		request.Highlights = highlights
		if err := request.Validate(capabilities, limits); !errors.Is(err, ErrInvalidQuery) {
			t.Fatal(highlights, err)
		}
	}
	request := base
	request.Highlights = map[string]Highlight{"f": {FragmentSize: limits.MaxSourceBytes, MaxFragments: limits.MaxPageItems}}
	if err := request.Validate(capabilities, limits); err != nil {
		t.Fatal(err)
	}
	for _, sort := range []Sort{{Field: "f", Direction: Ascending, Missing: MissingDefault}, {Field: "f", Direction: Descending, Missing: MissingFirst}, {Field: "f", Direction: Descending, Missing: MissingLast}} {
		if err := validateSort([]Sort{sort}); err != nil {
			t.Fatal(sort, err)
		}
	}
	for _, sort := range []Sort{{Direction: Ascending}, {Field: "f"}, {Field: "f", Direction: "sideways"}, {Field: "f", Direction: Ascending, Missing: "middle"}} {
		if err := validateSort([]Sort{sort}); !errors.Is(err, ErrInvalidQuery) {
			t.Fatal(sort, err)
		}
	}
	for _, projection := range []Projection{{Includes: []string{"f"}}, {Excludes: []string{"f"}}} {
		request := base
		request.Projection = projection
		if err := request.Validate(capabilities, limits); err != nil {
			t.Fatal(projection, err)
		}
		withoutProjection := capabilities
		withoutProjection.Projection = false
		if err := request.Validate(withoutProjection, limits); !errors.Is(err, ErrUnsupported) {
			t.Fatal(projection, err)
		}
	}
	if !validField(strings.Repeat("f", MaxFieldNameBytes)) || validField(strings.Repeat("f", MaxFieldNameBytes+1)) || validField("^2") || validField("f^2") || validField("f\n") {
		t.Fatal("field boundary changed")
	}
	exactBoosted := strings.Repeat("f", MaxFieldNameBytes-2) + "^1"
	if !validFullTextField("f") || !validFullTextField("f^1") || !validFullTextField(exactBoosted) || validFullTextField(exactBoosted+"0") {
		t.Fatal("full-text field size boundary changed")
	}
	for _, field := range []string{"^2", "f^", "f^0", "f^-1", "f^NaN", "f^Inf", "f^1^2"} {
		if validFullTextField(field) {
			t.Fatalf("invalid boosted field accepted: %q", field)
		}
	}
}

func TestMutationContractRequestInputBudgetExactBoundaries(t *testing.T) {
	limits := DefaultLimits()
	base := Request{Tenant: "t", Index: "i", Query: MatchAllQuery{}, Page: OffsetPage{Size: 1}}
	fields := make([]string, limits.MaxQueryClauses)
	sorts := make([]Sort, limits.MaxQueryClauses)
	highlights := make(map[string]Highlight, limits.MaxQueryClauses)
	aggregations := make(map[string]Aggregation, limits.MaxQueryClauses)
	suggestions := make(map[string]Suggestion, limits.MaxQueryClauses)
	for index := 0; index < limits.MaxQueryClauses; index++ {
		fields[index] = "f"
		sorts[index] = Sort{Field: "f", Direction: Ascending}
		name := "item_" + strconv.Itoa(index)
		highlights[name] = Highlight{FragmentSize: 1, MaxFragments: 1}
		aggregations[name] = TermsAggregation{Field: "f", Size: 1}
		suggestions[name] = PrefixSuggestion{Field: "f", Text: "x", Size: 1}
	}
	requests := []Request{
		func() Request { request := base; request.Sort = sorts; return request }(),
		func() Request { request := base; request.Projection.Includes = fields; return request }(),
		func() Request { request := base; request.Projection.Excludes = fields; return request }(),
		func() Request { request := base; request.Highlights = highlights; return request }(),
		func() Request { request := base; request.Aggregations = aggregations; return request }(),
		func() Request { request := base; request.Suggestions = suggestions; return request }(),
		func() Request {
			request := base
			request.Query = FullTextQuery{Fields: fields, Text: "x"}
			return request
		}(),
	}
	for index, request := range requests {
		if !requestInputsWithinBudget(request, limits) {
			t.Fatalf("exact collection boundary %d rejected", index)
		}
	}

	children := make([]Query, limits.MaxQueryClauses-1)
	for index := range children {
		children[index] = MatchAllQuery{}
	}
	exactNodes := base
	exactNodes.Query = BoolQuery{Must: children}
	if !requestInputsWithinBudget(exactNodes, limits) {
		t.Fatal("exact query node boundary rejected")
	}
	children = append(children, MatchAllQuery{})
	overNodes := base
	overNodes.Query = BoolQuery{Must: children}
	if requestInputsWithinBudget(overNodes, limits) {
		t.Fatal("query node overflow accepted")
	}

	query := Query(MatchAllQuery{})
	for range limits.MaxQueryDepth - 1 {
		query = BoolQuery{Must: []Query{query}}
	}
	exactDepthQuery := query
	exactDepthRequest := base
	exactDepthRequest.Query = exactDepthQuery
	if !requestInputsWithinBudget(exactDepthRequest, limits) {
		t.Fatal("exact input depth rejected")
	}
	clauses := 0
	if err := validateQuery(query, AllCapabilities(), limits, 1, &clauses); err != nil {
		t.Fatal("exact query depth rejected", err)
	}
	query = BoolQuery{Must: []Query{query}}
	overDepthRequest := base
	overDepthRequest.Query = query
	if requestInputsWithinBudget(overDepthRequest, limits) {
		t.Fatal("input depth overflow accepted")
	}
	clauses = 0
	if err := validateQuery(query, AllCapabilities(), limits, 1, &clauses); !errors.Is(err, ErrInvalidQuery) {
		t.Fatal("query depth overflow accepted", err)
	}

	payload, err := requestFingerprintPayload(base)
	if err != nil {
		t.Fatal(err)
	}
	exactBytes := limits
	exactBytes.MaxQueryBytes = len(payload)
	if _, err := RequestFingerprint(base, exactBytes); err != nil {
		t.Fatal("exact fingerprint byte boundary rejected", err)
	}
	exactBytes.MaxQueryBytes--
	if _, err := RequestFingerprint(base, exactBytes); !errors.Is(err, ErrInvalidQuery) {
		t.Fatal("fingerprint byte overflow accepted", err)
	}
	budget := requestInputBudget{bytes: 1, nodes: 1, maxCollection: 1}
	if !budget.consume(1) || budget.consume(1) {
		t.Fatal("exact input consumption boundary changed")
	}
	budget = requestInputBudget{bytes: 1, nodes: 1, maxCollection: 1}
	if budget.query(MatchAllQuery{}, 0) {
		t.Fatal("zero query depth accepted")
	}
}

func TestMutationContractQueryNodeBoundaries(t *testing.T) {
	limits := DefaultLimits()
	capabilities := AllCapabilities()
	number, _ := NumberValue("1")
	valid := []Query{
		BoolQuery{Should: []Query{MatchAllQuery{}}, MinimumShouldMatch: 1},
		RangeQuery{Field: "f", GT: &number}, RangeQuery{Field: "f", GTE: &number}, RangeQuery{Field: "f", LT: &number}, RangeQuery{Field: "f", LTE: &number},
		GeoDistanceQuery{Field: "p", Origin: GeoPoint{Latitude: -90, Longitude: -180}, DistanceKM: number},
		GeoDistanceQuery{Field: "p", Origin: GeoPoint{Latitude: 90, Longitude: 180}, DistanceKM: number},
	}
	for _, query := range valid {
		clauses := 0
		if err := validateQuery(query, capabilities, limits, 1, &clauses); err != nil {
			t.Fatal(query, err)
		}
	}
	for _, query := range []Query{
		RangeQuery{Field: "f"},
		RangeQuery{Field: "f", GT: &number, GTE: &number},
		RangeQuery{Field: "f", LT: &number, LTE: &number},
		GeoDistanceQuery{Field: "p", Origin: GeoPoint{Latitude: math.NaN()}, DistanceKM: number},
		GeoDistanceQuery{Field: "p", Origin: GeoPoint{Latitude: math.Inf(1)}, DistanceKM: number},
		GeoDistanceQuery{Field: "p", Origin: GeoPoint{Longitude: math.NaN()}, DistanceKM: number},
		GeoDistanceQuery{Field: "p", Origin: GeoPoint{Longitude: math.Inf(1)}, DistanceKM: number},
		GeoDistanceQuery{Field: "p", Origin: GeoPoint{Latitude: math.Nextafter(-90, math.Inf(-1))}, DistanceKM: number},
		GeoDistanceQuery{Field: "p", Origin: GeoPoint{Latitude: math.Nextafter(90, math.Inf(1))}, DistanceKM: number},
		GeoDistanceQuery{Field: "p", Origin: GeoPoint{Longitude: math.Nextafter(-180, math.Inf(-1))}, DistanceKM: number},
		GeoDistanceQuery{Field: "p", Origin: GeoPoint{Longitude: math.Nextafter(180, math.Inf(1))}, DistanceKM: number},
	} {
		clauses := 0
		if err := validateQuery(query, capabilities, limits, 1, &clauses); !errors.Is(err, ErrInvalidQuery) {
			t.Fatal(query, err)
		}
	}
	if !validAggregation(TermsAggregation{Field: "f", Size: 1}, limits) || validAggregation(TermsAggregation{Field: "", Size: 1}, limits) || validAggregation(TermsAggregation{Field: "f", Size: 0}, limits) {
		t.Fatal("terms aggregation boundary changed")
	}
	if !validAggregation(TermsAggregation{Field: "f", Size: limits.MaxPageItems}, limits) {
		t.Fatal("maximum terms aggregation rejected")
	}
	from := number
	to := number
	maximumBuckets := make([]RangeBucket, limits.MaxQueryClauses)
	for index := range maximumBuckets {
		maximumBuckets[index] = RangeBucket{Key: "k", From: &from}
	}
	if !validAggregation(RangeAggregation{Field: "f", Buckets: maximumBuckets}, limits) {
		t.Fatal("maximum range aggregation rejected")
	}
	for _, aggregation := range []Aggregation{RangeAggregation{Field: "f", Buckets: []RangeBucket{{Key: "k", From: &from}}}, RangeAggregation{Field: "f", Buckets: []RangeBucket{{Key: "k", To: &to}}}} {
		if !validAggregation(aggregation, limits) {
			t.Fatal(aggregation)
		}
	}
	for _, aggregation := range []Aggregation{RangeAggregation{Buckets: []RangeBucket{{Key: "k", From: &from}}}, RangeAggregation{Field: "f"}, RangeAggregation{Field: "f", Buckets: []RangeBucket{{From: &from}}}, RangeAggregation{Field: "f", Buckets: []RangeBucket{{Key: "k"}}}} {
		if validAggregation(aggregation, limits) {
			t.Fatal(aggregation)
		}
	}
	if !validSuggestion(PrefixSuggestion{Field: "f", Text: "x", Size: 1}, limits) || !validSuggestion(PrefixSuggestion{Field: "f", Text: "x", Size: limits.MaxPageItems}, limits) || validSuggestion(invalidSuggestion{}, limits) || validSuggestion(PrefixSuggestion{Text: "x", Size: 1}, limits) || validSuggestion(PrefixSuggestion{Field: "f", Size: 1}, limits) || validSuggestion(PrefixSuggestion{Field: "f", Text: "x"}, limits) {
		t.Fatal("suggestion boundary changed")
	}
	rawSize := limits.MaxSourceBytes
	raw := json.RawMessage("{\"x\":\"" + strings.Repeat("a", rawSize-8) + "\"}")
	if len(raw) != rawSize {
		t.Fatal(len(raw))
	}
	clauses := 0
	if err := validateQuery(RawExtensionQuery{Adapter: "opensearch", Payload: raw}, capabilities, limits, 1, &clauses); err != nil {
		t.Fatal(err)
	}
	clauses = 0
	if err := validateQuery(RawExtensionQuery{Adapter: "opensearch", Payload: append(raw, ' ')}, capabilities, limits, 1, &clauses); !errors.Is(err, ErrInvalidQuery) {
		t.Fatal(err)
	}
	clauses = limits.MaxQueryClauses - 1
	if err := validateQuery(MatchAllQuery{}, capabilities, limits, limits.MaxQueryDepth, &clauses); err != nil || clauses != limits.MaxQueryClauses {
		t.Fatal(clauses, err)
	}
	clauses = limits.MaxQueryClauses
	if err := validateQuery(MatchAllQuery{}, capabilities, limits, limits.MaxQueryDepth, &clauses); !errors.Is(err, ErrInvalidQuery) {
		t.Fatal(err)
	}
	clauses = 0
	if err := validateQuery(MatchAllQuery{}, capabilities, limits, limits.MaxQueryDepth+1, &clauses); !errors.Is(err, ErrInvalidQuery) {
		t.Fatal(err)
	}
}

func TestMutationContractWriteAndResultBoundaries(t *testing.T) {
	limits := DefaultLimits()
	object := json.RawMessage(`{}`)
	doc, _ := NewDocument("t", "i", "id", 1, json.RawMessage(`{}`), limits)
	op := IndexDocument(doc)
	bulk := BulkRequest{Operations: []WriteOperation{op}, Refresh: RefreshNone}
	exact := limits
	exact.MaxBulkItems = 1
	exact.MaxBulkBytes = len(op.Tenant) + len(op.Index) + len(op.ID) + len(op.Source) + 64
	if err := bulk.Validate(AllCapabilities(), exact); err != nil {
		t.Fatal(err)
	}
	two := BulkRequest{Operations: []WriteOperation{op, op}, Refresh: RefreshNone}
	twoLimits := limits
	twoLimits.MaxBulkBytes = 2 * (len(op.Tenant) + len(op.Index) + len(op.ID) + len(op.Source) + 64)
	if err := two.Validate(AllCapabilities(), twoLimits); err != nil {
		t.Fatal(err)
	}
	twoLimits.MaxBulkBytes--
	if err := two.Validate(AllCapabilities(), twoLimits); !errors.Is(err, ErrBulkLimit) {
		t.Fatal(err)
	}
	exact.MaxBulkBytes--
	if err := bulk.Validate(AllCapabilities(), exact); !errors.Is(err, ErrBulkLimit) {
		t.Fatal(err)
	}
	many := make([]WriteOperation, limits.MaxBulkItems)
	for i := range many {
		many[i] = op
	}
	largeLimits := limits
	largeLimits.MaxBulkBytes = math.MaxInt
	if err := (BulkRequest{Operations: many, Refresh: RefreshNone}).Validate(AllCapabilities(), largeLimits); err != nil {
		t.Fatal(err)
	}
	if err := (BulkRequest{Operations: append(many, op), Refresh: RefreshNone}).Validate(AllCapabilities(), largeLimits); !errors.Is(err, ErrBulkLimit) {
		t.Fatal(err)
	}
	for _, value := range []string{" ", "\n", "\r", "\t", " \n\r\t "} {
		if len(bytesTrimSpace([]byte(value))) != 0 {
			t.Fatalf("whitespace %q retained", value)
		}
	}
	for _, operation := range []WriteOperation{
		{Action: ActionIndex, Tenant: "", Index: "i", ID: "id", Version: 1, Source: object},
		{Action: ActionIndex, Tenant: "t", Index: "", ID: "id", Version: 1, Source: object},
		{Action: ActionIndex, Tenant: "t", Index: "i", ID: "", Version: 1, Source: object},
		{Action: ActionIndex, Tenant: "t", Index: "i", ID: "id", Source: object},
		{Action: ActionIndex, Tenant: "t", Index: "i", ID: "id", Version: 1},
		{Action: ActionIndex, Tenant: "t", Index: "i", ID: "id", Version: 1, Source: json.RawMessage(`{`)},
		{Action: ActionIndex, Tenant: "t", Index: "i", ID: "id", Version: 1, Source: json.RawMessage(`[]`)},
		{Action: ActionIndex, Tenant: "t", Index: "i", ID: "id", Version: 1, Source: json.RawMessage(`{]`)},
		{Action: ActionIndex, Tenant: "t", Index: "i", ID: "id", Version: 1, Source: json.RawMessage(`{"x":}`)},
	} {
		if err := operation.validate(limits); !errors.Is(err, ErrInvalidOperation) {
			t.Fatal(operation, err)
		}
	}
	for _, operation := range []WriteOperation{
		{Action: ActionIndex, Tenant: strings.Repeat("t", limits.MaxTenantBytes), Index: "i", ID: "id", Version: 1, Source: object},
		{Action: ActionIndex, Tenant: "t", Index: strings.Repeat("i", limits.MaxIndexBytes), ID: "id", Version: 1, Source: object},
		{Action: ActionIndex, Tenant: "t", Index: "i", ID: strings.Repeat("i", limits.MaxIDBytes), Version: 1, Source: object},
		{Action: ActionIndex, Tenant: "t", Index: "i", ID: "id", Version: 1, Source: json.RawMessage("{" + strings.Repeat(" ", limits.MaxSourceBytes-2) + "}")},
	} {
		if err := operation.validate(limits); err != nil {
			t.Fatal(operation, err)
		}
	}
	for _, operation := range []WriteOperation{
		{Action: ActionIndex, Tenant: strings.Repeat("t", limits.MaxTenantBytes+1), Index: "i", ID: "id", Version: 1, Source: object},
		{Action: ActionIndex, Tenant: "t", Index: strings.Repeat("i", limits.MaxIndexBytes+1), ID: "id", Version: 1, Source: object},
		{Action: ActionIndex, Tenant: "t", Index: "i", ID: strings.Repeat("i", limits.MaxIDBytes+1), Version: 1, Source: object},
		{Action: ActionIndex, Tenant: "t", Index: "i", ID: "id", Version: 1, Source: json.RawMessage("{" + strings.Repeat(" ", limits.MaxSourceBytes-1) + "}")},
	} {
		if err := operation.validate(limits); !errors.Is(err, ErrInvalidOperation) {
			t.Fatal(operation, err)
		}
	}
	for _, hit := range []Hit{{Index: "", ID: "id"}, {Index: "i", ID: ""}, {Index: "i", ID: "id", Source: json.RawMessage(`{`)}} {
		if _, err := NewResult([]Hit{hit}, Total{}, nil, nil, Diagnostics{}, ""); !errors.Is(err, ErrInvalidResult) {
			t.Fatal(hit, err)
		}
	}
	if _, err := NewResult([]Hit{{Index: "i", ID: "id"}}, Total{}, nil, nil, Diagnostics{}, ""); err != nil {
		t.Fatal(err)
	}
	for _, diagnostics := range []Diagnostics{
		{Shards: ShardDiagnostics{Total: 1, Successful: 1}},
		{Shards: ShardDiagnostics{Total: 2, Successful: 1}},
		{Shards: ShardDiagnostics{Total: 3, Successful: 1, Skipped: 1, Failed: 1}},
	} {
		_, err := NewResult(nil, Total{}, nil, nil, diagnostics, "")
		if diagnostics.Shards.Total == 2 && !errors.Is(err, ErrInvalidResult) || diagnostics.Shards.Total != 2 && err != nil {
			t.Fatal(diagnostics, err)
		}
	}
}

func TestMutationContractProjectionLifecycleAndFingerprints(t *testing.T) {
	limits := DefaultLimits()
	for _, key := range []string{"k", strings.Repeat("k", limits.MaxIDBytes)} {
		if _, err := NewProjectionEvent("t", "i", "id", 1, ProjectionDelete, nil, key, limits); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewProjectionEvent("t", "i", "id", 1, ProjectionDelete, nil, strings.Repeat("k", limits.MaxIDBytes+1), limits); !errors.Is(err, ErrInvalidProjectionEvent) {
		t.Fatal(err)
	}
	for _, values := range [][3]string{{strings.Repeat("t", limits.MaxTenantBytes), "i", "id"}, {"t", strings.Repeat("i", limits.MaxIndexBytes), "id"}, {"t", "i", strings.Repeat("i", limits.MaxIDBytes)}} {
		if _, err := NewProjectionEvent(values[0], values[1], values[2], 1, ProjectionDelete, nil, "k", limits); err != nil {
			t.Fatal(values, err)
		}
	}
	for _, values := range [][3]string{{strings.Repeat("t", limits.MaxTenantBytes+1), "i", "id"}, {"t", strings.Repeat("i", limits.MaxIndexBytes+1), "id"}, {"t", "i", strings.Repeat("i", limits.MaxIDBytes+1)}} {
		if _, err := NewProjectionEvent(values[0], values[1], values[2], 1, ProjectionDelete, nil, "k", limits); !errors.Is(err, ErrInvalidProjectionEvent) {
			t.Fatal(values, err)
		}
	}
	if _, err := NewProjectionEvent("t", "i", "id", 1, ProjectionDelete, json.RawMessage(`{}`), "k", limits); !errors.Is(err, ErrInvalidProjectionEvent) {
		t.Fatal(err)
	}
	validDelete := func() (string, string, string, uint64) { return "t", "i", "id", 1 }
	for _, mutate := range []func(*string, *string, *string, *uint64){func(t, _, _ *string, _ *uint64) { *t = "" }, func(_, i, _ *string, _ *uint64) { *i = "" }, func(_, _, id *string, _ *uint64) { *id = "" }, func(_, _, _ *string, v *uint64) { *v = 0 }} {
		tenant, index, id, version := validDelete()
		mutate(&tenant, &index, &id, &version)
		if _, err := NewProjectionEvent(tenant, index, id, version, ProjectionDelete, nil, "k", limits); !errors.Is(err, ErrInvalidProjectionEvent) {
			t.Fatal(err)
		}
	}
	plan, _ := lifecycleBranchPlan(t)
	if _, err := validateMigrationPlan(plan); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*MigrationPlan){func(p *MigrationPlan) { p.ID = "" }, func(p *MigrationPlan) { p.Tenant = "" }, func(p *MigrationPlan) { p.Alias = "" }, func(p *MigrationPlan) { p.SourceIndex = "" }, func(p *MigrationPlan) { p.Target = IndexDefinition{} }, func(p *MigrationPlan) { p.MaxReindexSteps = 0 }, func(p *MigrationPlan) { p.SourceIndex = "UPPER" }, func(p *MigrationPlan) { p.Alias = "UPPER" }} {
		candidate := plan
		mutate(&candidate)
		if _, err := validateMigrationPlan(candidate); !errors.Is(err, ErrInvalidMigrationPlan) {
			t.Fatal(candidate, err)
		}
	}
	for _, target := range []IndexDefinition{{name: "events-v2"}, {fingerprint: "fingerprint"}} {
		candidate := plan
		candidate.Target = target
		if _, err := validateMigrationPlan(candidate); !errors.Is(err, ErrInvalidMigrationPlan) {
			t.Fatal(candidate, err)
		}
	}
	if queryModels(nil) != nil || aggregationFingerprintModel(nil) != nil || suggestionFingerprintModel(nil) != nil {
		t.Fatal("nil fingerprint collections changed")
	}
	if len(queryModels([]Query{})) != 0 || len(aggregationFingerprintModel(map[string]Aggregation{})) != 0 || len(suggestionFingerprintModel(map[string]Suggestion{})) != 0 {
		t.Fatal("empty fingerprint collections changed")
	}
}

func TestMutationContractReconciliationBoundaries(t *testing.T) {
	limits := DefaultLimits()
	validRequest := ReconciliationRequest{Tenant: "t", Index: "i", PageSize: limits.MaxPageItems, MaxRecords: 1}
	for _, mutate := range []func(*ReconciliationRequest){func(r *ReconciliationRequest) { r.Tenant = "" }, func(r *ReconciliationRequest) { r.Index = "" }, func(r *ReconciliationRequest) { r.PageSize = 0 }, func(r *ReconciliationRequest) { r.PageSize = limits.MaxPageItems + 1 }, func(r *ReconciliationRequest) { r.MaxRecords = 0 }} {
		request := validRequest
		mutate(&request)
		reconciler, _ := NewReconciler(&branchReader{pages: []ReconciliationPage{{Done: true}}}, &branchReader{pages: []ReconciliationPage{{Done: true}}}, branchRepair{}, limits)
		if _, err := reconciler.Run(t.Context(), request); !errors.Is(err, ErrInvalidReconciliation) {
			t.Fatal(request, err)
		}
	}
	reconciler, _ := NewReconciler(&branchReader{pages: []ReconciliationPage{{Done: true}}}, &branchReader{pages: []ReconciliationPage{{Done: true}}}, branchRepair{}, limits)
	if _, err := reconciler.Run(t.Context(), validRequest); err != nil {
		t.Fatal(err)
	}

	request := ReconciliationRequest{Tenant: "t", Index: "i", PageSize: 2, MaxRecords: 2}
	malformedPages := [][]ReconciliationPage{
		{{Cursor: "next"}},
		{{Records: []ReconciliationRecord{IndexRecord("a", 1, "d")}}},
		{{Records: []ReconciliationRecord{IndexRecord("a", 1, "d")}, Cursor: "same"}, {Records: []ReconciliationRecord{IndexRecord("b", 1, "d")}, Cursor: "same"}},
	}
	for _, pages := range malformedPages {
		if _, err := readReconciliation(t.Context(), &branchReader{pages: pages}, request, false); !errors.Is(err, ErrMalformedReconciliation) {
			t.Fatal(pages, err)
		}
	}
	for _, record := range []ReconciliationRecord{{Version: 1, Digest: "d"}, {ID: "a", Digest: "d"}, {ID: "a", Version: 1}, {ID: "a", Version: 1, Digest: "d"}} {
		requireDocument := record.ID == "a" && record.Version == 1 && record.Digest == "d"
		if _, err := readReconciliation(t.Context(), &branchReader{pages: []ReconciliationPage{{Records: []ReconciliationRecord{record}, Done: true}}}, request, requireDocument); !errors.Is(err, ErrMalformedReconciliation) {
			t.Fatal(record, err)
		}
	}
	valid := validBranchRecord(t, "a", 1)
	for _, mutate := range []func(*Document){func(d *Document) { d.Tenant = "other" }, func(d *Document) { d.Index = "other" }, func(d *Document) { d.ID = "other" }, func(d *Document) { d.Version = 2 }} {
		record := valid
		document := *record.Document
		mutate(&document)
		record.Document = &document
		if _, err := readReconciliation(t.Context(), &branchReader{pages: []ReconciliationPage{{Records: []ReconciliationRecord{record}, Done: true}}}, request, true); !errors.Is(err, ErrMalformedReconciliation) {
			t.Fatal(record, err)
		}
	}
	exactRecords := []ReconciliationRecord{IndexRecord("a", 1, "d"), IndexRecord("b", 1, "d")}
	if records, err := readReconciliation(t.Context(), &branchReader{pages: []ReconciliationPage{{Records: exactRecords, Done: true}}}, request, false); err != nil || len(records) != request.MaxRecords {
		t.Fatal(records, err)
	}
	oneRecord := request
	oneRecord.MaxRecords = 1
	if _, err := readReconciliation(t.Context(), &branchReader{pages: []ReconciliationPage{{Records: exactRecords, Done: true}}}, oneRecord, false); !errors.Is(err, ErrReconciliationLimit) {
		t.Fatal(err)
	}
	unsorted := []ReconciliationRecord{IndexRecord("b", 1, "d"), IndexRecord("a", 1, "d")}
	if _, err := readReconciliation(t.Context(), &branchReader{pages: []ReconciliationPage{{Records: unsorted, Done: true}}}, request, false); !errors.Is(err, ErrMalformedReconciliation) {
		t.Fatal(err)
	}
}
