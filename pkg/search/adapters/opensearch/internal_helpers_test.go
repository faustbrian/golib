package opensearch

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

type internalRoundTripFunc func(*http.Request) (*http.Response, error)

func (f internalRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read") }

type errorAfterLimitReader struct{ read bool }

func (reader *errorAfterLimitReader) Read(buffer []byte) (int, error) {
	if reader.read {
		return 0, errors.New("read")
	}
	reader.read = true
	buffer[0] = 'x'
	return 1, nil
}

type closeErrorBody struct{ io.Reader }

func (closeErrorBody) Close() error { return errors.New("close") }

type internalObserverFunc func(context.Context, TelemetryEvent) error

func (f internalObserverFunc) Observe(ctx context.Context, e TelemetryEvent) error { return f(ctx, e) }

func internalResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func internalReindexCursorCodec(t *testing.T) *ReindexCursorCodec {
	t.Helper()
	codec, err := NewReindexCursorCodec(
		[]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096, time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func TestInternalConfigAndTransportHelpers(t *testing.T) {
	if normalized, insecure, err := validateEndpoint("http://localhost:9200/", true); err != nil || !insecure || normalized != "http://localhost:9200" {
		t.Fatal(normalized, insecure, err)
	}
	for _, value := range []string{"", strings.Repeat("x", maximumEndpointBytes+1), "://", "https://", "ftp://host", "http://example.com", "https://host/path", "https://host?q=1", "https://host#x", "https://user:pass@host"} {
		if _, _, err := validateEndpoint(value, true); err == nil {
			t.Fatalf("endpoint %q accepted", value)
		}
	}
	for _, host := range []string{"localhost", "LOCALHOST", "127.0.0.1", "::1"} {
		if !loopbackHost(host) {
			t.Fatalf("loopback %q rejected", host)
		}
	}
	if loopbackHost("example.com") {
		t.Fatal("remote loopback")
	}
	proxyURL, _ := url.Parse("http://proxy.example")
	for _, policy := range []ProxyPolicy{{Mode: ProxyDisabled, URL: proxyURL}, {Mode: ProxyExplicit}, {Mode: ProxyMode(99)}} {
		if validateProxy(policy) == nil {
			t.Fatal("proxy accepted")
		}
	}
	for _, raw := range []string{"ftp://proxy", "http://user:pass@proxy", "http://proxy/path", "http://proxy?q=1", "http://proxy#x"} {
		parsed, _ := url.Parse(raw)
		if validateProxy(ProxyPolicy{Mode: ProxyExplicit, URL: parsed}) == nil {
			t.Fatalf("proxy %q accepted", raw)
		}
	}
	if validateProxy(ProxyPolicy{Mode: ProxyEnvironment}) != nil || validateProxy(ProxyPolicy{Mode: ProxyExplicit, URL: proxyURL}) != nil {
		t.Fatal("valid proxy rejected")
	}

	for _, policy := range []DiscoveryPolicy{{AllowedDNSSuffixes: []string{".x"}}, {MaximumNodes: -1, AllowedDNSSuffixes: []string{".x"}}, {MaximumNodes: 1}, {MaximumNodes: 1, AllowedDNSSuffixes: []string{"x"}}, {MaximumNodes: 1, AllowedDNSSuffixes: []string{".UPPER"}}, {MaximumNodes: 1, AllowedDNSSuffixes: []string{"./bad"}}, {MaximumNodes: 1, AllowedCIDRs: []netip.Prefix{{}}}, {MaximumNodes: 1, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.1/24")}}} {
		if validateDiscoveryPolicy(policy) == nil {
			t.Fatalf("discovery %#v accepted", policy)
		}
	}
	validDiscovery := DiscoveryPolicy{MaximumNodes: 1, AllowedDNSSuffixes: []string{".example"}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}}
	if validateDiscoveryPolicy(validDiscovery) != nil {
		t.Fatal("valid discovery rejected")
	}
	cloned := cloneDiscoveryPolicy(validDiscovery)
	cloned.AllowedDNSSuffixes[0] = "changed"
	if validDiscovery.AllowedDNSSuffixes[0] == "changed" {
		t.Fatal("clone aliases")
	}

	borrowed := internalRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil })
	if _, err := configureTransport(Config{Transport: borrowed, TransportOwnership: TransportBorrowed, TLS: &tls.Config{}}); err == nil {
		t.Fatal("borrowed TLS accepted")
	}
	if got, err := configureTransport(Config{Transport: borrowed, TransportOwnership: TransportBorrowed}); err != nil || got == nil {
		t.Fatal(err)
	}
	if got, err := configureTransport(Config{Transport: borrowed}); err != nil || got == nil {
		t.Fatal(err)
	}
	if got, err := configureTransport(Config{Transport: &http.Transport{}}); err != nil || got == nil {
		t.Fatal(err)
	}
	if got, err := configureTransport(Config{}); err != nil || got == nil {
		t.Fatal(err)
	}
	for _, config := range []Config{{TLS: &tls.Config{MinVersion: tls.VersionTLS13}}, {Proxy: ProxyPolicy{Mode: ProxyEnvironment}}, {Proxy: ProxyPolicy{Mode: ProxyExplicit, URL: proxyURL}}} {
		transport := configureHTTPTransport(&http.Transport{}, config)
		if transport.TLSClientConfig == nil || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 {
			t.Fatal("TLS bound absent")
		}
	}
	template := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}
	originalTLS := template.TLSClientConfig
	if configureHTTPTransport(template, Config{}).TLSClientConfig == originalTLS {
		t.Fatal("TLS config not cloned")
	}

	base := Config{Endpoints: []string{"https://host"}, RequestTimeout: time.Second, MaximumResponseBytes: 1}
	for _, mutate := range []func(*Config){func(c *Config) { c.TransportOwnership = TransportBorrowed }, func(c *Config) { c.TLS = &tls.Config{InsecureSkipVerify: true} }, func(c *Config) { c.Endpoints = []string{"https://host", "https://host/"} }} {
		config := base
		mutate(&config)
		if _, _, err := validateConfig(config); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	for _, config := range []Config{
		{Endpoints: []string{"https://host"}, RequestTimeout: time.Second, MaximumResponseBytes: 1, Search: &SearchConfig{}},
		{Endpoints: []string{"https://host"}, RequestTimeout: time.Second, MaximumResponseBytes: 1, Lifecycle: &LifecycleConfig{}},
		{Endpoints: []string{"https://host"}, RequestTimeout: time.Second, MaximumResponseBytes: 1, Resilience: ResilienceConfig{MaximumInFlight: -1}},
		{Endpoints: []string{"https://host"}, RequestTimeout: time.Second, MaximumResponseBytes: 1, Telemetry: &TelemetryConfig{}},
		{Endpoints: []string{"https://host"}, RequestTimeout: time.Second, MaximumResponseBytes: 1, TransportOwnership: TransportBorrowed},
		{Endpoints: []string{"https://host"}, RequestTimeout: time.Second, MaximumResponseBytes: 1, Transport: internalRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil }), TransportOwnership: TransportBorrowed, TLS: &tls.Config{}},
	} {
		if _, err := New(config); err == nil {
			t.Fatalf("New(%#v) accepted", config)
		}
	}
	codec, _ := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 1024)
	for _, locales := range []map[string]string{{"": "standard"}, {strings.Repeat("x", 65): "standard"}, {string([]byte{0xff}): "standard"}, {"fi": "bad/name"}} {
		config := base
		config.Search = &SearchConfig{Limits: search.DefaultLimits(), CursorCodec: codec, Resolver: internalResolver{target: IndexTarget{Name: "i", PhysicalName: "i", Fingerprint: "f"}}, Clock: time.Now, LocaleAnalyzers: locales}
		if _, err := New(config); err == nil {
			t.Fatalf("locales %#v accepted", locales)
		}
	}
	if validLifecycleTenant(string([]byte{0xff})) {
		t.Fatal("invalid UTF-8 lifecycle tenant accepted")
	}
	var nilClient *Client
	if err := nilClient.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInternalReindexCursorAndPaginationDefensiveBranches(t *testing.T) {
	codec := internalReindexCursorCodec(t)
	codec.random = errorReader{}
	if _, err := codec.seal([]byte(`{"task":"node:123"}`)); !errors.Is(err, ErrInvalidReindexCursor) {
		t.Fatalf("seal() error = %v", err)
	}
	if _, err := codec.decode("", "tenant", "events-v1", "events-v2"); !errors.Is(err, ErrInvalidReindexCursor) {
		t.Fatalf("decode() error = %v", err)
	}
	unauthenticated := base64.RawURLEncoding.EncodeToString(make([]byte, codec.aead.NonceSize()+codec.aead.Overhead()))
	if _, err := codec.decode(unauthenticated, "tenant", "events-v1", "events-v2"); !errors.Is(err, ErrInvalidReindexCursor) {
		t.Fatalf("unauthenticated decode() error = %v", err)
	}
	if pagination := searchPaginationAuthorization(nil); pagination != (SearchPaginationAuthorization{}) {
		t.Fatalf("nil pagination authorization = %#v", pagination)
	}

	renewalCodec := internalReindexCursorCodec(t)
	renewalCursor, err := renewalCodec.encode("tenant", "events-v1", "events-v2", "node:123")
	if err != nil {
		t.Fatal(err)
	}
	authorize := LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil })
	client := internalClient(t, routeBody(`{"completed":false}`, http.StatusOK), nil, authorize)
	client.lifecycle.ReindexCursorCodec = renewalCodec
	renewalCodec.random = errorReader{}
	returned, done, err := client.Reindex(t.Context(), "tenant", "events-v1", "events-v2", renewalCursor)
	failure := new(Failure)
	if returned != renewalCursor || done || !errors.Is(err, ErrInvalidReindexCursor) ||
		!errors.As(err, &failure) || failure.Category != FailureMalformed {
		t.Fatalf("renewal encoding failure = %q/%t/%#v", returned, done, failure)
	}
}

func TestInternalFailureWriteAndBulkClassifications(t *testing.T) {
	nestedPIT := []byte(`{"error":{"root_cause":[{"type":"search_context_missing_exception","reason":"redacted"}],"type":"search_phase_execution_exception","failed_shards":[{"reason":{"type":"search_context_missing_exception","reason":"redacted"}}]}}`)
	failure := responseFailure(OperationSearch, http.StatusNotFound, nestedPIT)
	if failure.Category != FailurePITExpired || !errors.Is(failure, ErrPITExpired) || failure.Code != "search_phase_execution_exception" {
		t.Fatalf("nested PIT response = %#v", failure)
	}
	writeFailure := responseFailure(OperationWrite, http.StatusNotFound, nestedPIT)
	if writeFailure.Category == FailurePITExpired || errors.Is(writeFailure, ErrPITExpired) {
		t.Fatalf("write response misclassified as PIT expiry: %#v", writeFailure)
	}
	for _, body := range []string{
		`{"error":{"type":"search_context_missing_exception"}}`,
		`{"error":{"root_cause":[{"type":"other"},{"type":"search_context_missing_exception"}]}}`,
		`{"error":{"failed_shards":[{"reason":{"type":"other"}},{"reason":{"type":"search_context_missing_exception"}}]}}`,
	} {
		if !responseHasErrorCode([]byte(body), "search_context_missing_exception") {
			t.Fatalf("nested code not found in %s", body)
		}
	}
	for _, body := range []string{
		"not-json",
		`{}`,
		`{"error":{"type":"other"}}`,
		`{"error":{"root_cause":[{"type":"other"}]}}`,
		`{"error":{"failed_shards":[{"reason":{"type":"other"}}]}}`,
	} {
		if responseHasErrorCode([]byte(body), "search_context_missing_exception") || responseHasErrorCode([]byte(body), "") {
			t.Fatalf("unexpected nested code in %s", body)
		}
	}

	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, ErrBackpressure, ErrCircuitOpen, errors.New("network")} {
		failure := transportFailure(OperationInfo, cause)
		if failure == nil || failure.Error() == "" || failure.Unwrap() == nil {
			t.Fatal("failure missing")
		}
	}
	_ = unknownTransportFailure(OperationBulk, errors.New("x"))
	_ = unknownWriteOutcome(search.WriteOperation{ID: "id", Action: search.ActionIndex})
	for _, test := range []struct {
		status   int
		body     string
		category FailureCategory
	}{{429, `{}`, FailureOverloaded}, {503, `{}`, FailureOverloaded}, {403, `{"error":{"type":"cluster_block_exception"}}`, FailureClusterBlocked}, {409, `{}`, FailureRejected}, {400, `{"error":{"type":"mapper_parsing_exception"}}`, FailureMappingRejected}, {404, `{"error":{"type":"resource_not_found_exception"}}`, FailureRejected}, {400, `{}`, FailureRejected}} {
		failure := responseFailure(OperationSearch, test.status, []byte(test.body))
		if failure.Category != test.category {
			t.Fatalf("%d=%s", test.status, failure.Category)
		}
	}
	for _, body := range []string{"", `[]`, `{"error":"text"}`, `{"error":{"type":"UPPER"}}`} {
		if responseErrorCode([]byte(body)) != "unknown" {
			t.Fatalf("body %q code", body)
		}
	}
	for _, code := range []string{"", strings.Repeat("a", 129), "Upper", "a__b", "a-b"} {
		if safeErrorCode(code) {
			t.Fatalf("code %q safe", code)
		}
	}
	if !safeErrorCode("rejected_1") {
		t.Fatal("safe code rejected")
	}

	failureType := &struct {
		Type string `json:"type"`
	}{Type: "x"}
	for _, test := range []struct {
		action search.WriteAction
		status int
		state  search.OutcomeState
		retry  bool
	}{{search.ActionIndex, 200, search.OutcomeApplied, false}, {search.ActionDelete, 404, search.OutcomeFailed, false}, {search.ActionIndex, 404, search.OutcomeFailed, false}, {search.ActionIndex, 409, search.OutcomeFailed, false}, {search.ActionIndex, 429, search.OutcomeRejected, true}, {search.ActionIndex, 503, search.OutcomeRejected, true}, {search.ActionIndex, 400, search.OutcomeFailed, false}, {search.ActionIndex, 418, search.OutcomeUnknown, false}} {
		failure := failureType
		if test.status == 418 {
			failure = nil
		}
		state, retry := classifyBulkItem(test.action, test.status, failure)
		if state != test.state || retry != test.retry {
			t.Fatal(test)
		}
	}
	for _, test := range []struct {
		action search.WriteAction
		status int
		state  search.OutcomeState
		retry  bool
	}{{search.ActionIndex, 200, search.OutcomeApplied, false}, {search.ActionDelete, 404, search.OutcomeFailed, false}, {search.ActionIndex, 409, search.OutcomeFailed, false}, {search.ActionIndex, 429, search.OutcomeRejected, true}, {search.ActionIndex, 503, search.OutcomeRejected, true}, {search.ActionIndex, 400, search.OutcomeFailed, false}, {search.ActionIndex, 418, search.OutcomeUnknown, false}} {
		failure := failureType
		if test.status == 418 {
			failure = nil
		}
		state, retry := classifyWriteStatus(test.action, test.status, failure)
		if state != test.state || retry != test.retry {
			t.Fatal(test)
		}
	}
	op := search.WriteOperation{Action: search.ActionIndex, ID: "id", Version: 2}
	for _, body := range []string{"not-json", `{"_id":"other"}`} {
		if _, err := decodeWriteResponse(op, "events-v1", 200, []byte(body)); err == nil {
			t.Fatalf("write %q accepted", body)
		}
	}
	if _, err := decodeWriteResponse(op, "events-v1", 200, []byte(`{"_index":"events-v1","_id":"id","_version":2,"result":"updated"}`)); err != nil {
		t.Fatal(err)
	}
	if outcome, err := decodeWriteResponse(op, "events-v1", 400, []byte(`{"_id":"id","error":{"type":"UPPER"}}`)); err != nil || outcome.Code != "unknown" {
		t.Fatal(outcome, err)
	}
	if outcome, err := decodeWriteResponse(op, "events-v1", 400, []byte(`{"error":{"type":"mapper_parsing_exception"}}`)); err != nil || outcome.State != search.OutcomeFailed {
		t.Fatal(outcome, err)
	}
	if outcome, err := decodeWriteResponse(op, "events-v1", 300, []byte(`{}`)); err != nil || outcome.State != search.OutcomeUnknown {
		t.Fatal(outcome, err)
	}
	if result := markUnknownOutcome(errors.New("x")); result == nil {
		t.Fatal("unknown mark nil")
	}
	if result := markUnknownOutcome(&Failure{Operation: OperationBulk, Category: FailureRejected, OutcomeKnown: true}); result == nil {
		t.Fatal("failure mark nil")
	}
	bulkOp := []search.WriteOperation{{Action: search.ActionIndex, ID: "id"}}
	for _, body := range []string{
		`{"took":1,"errors":true,"items":[{}]}`,
		`{"took":1,"errors":true,"items":[{"index":{},"delete":{}}]}`,
		`{"took":1,"errors":true,"items":[{"index":{"_id":"id","status":99}}]}`,
		`{"took":1,"errors":true,"items":[{"index":{"_id":"id","status":400,"error":{"type":"UPPER"}}}]}`,
		`{"took":1,"errors":true,"items":[{"index":{"_id":"id","_version":2,"status":200,"result":"updated"}}]}`,
		`{"took":1,"errors":false,"items":[{"index":{"_id":"id","status":400,"error":{"type":"mapping"}}}]}`,
	} {
		result, err := decodeBulkResponse(bulkOp, []byte(body))
		if strings.Contains(body, "UPPER") {
			if err != nil || result.Items()[0].Code != "unknown" {
				t.Fatal(result, err)
			}
		} else if err == nil {
			t.Fatalf("bulk %q accepted", body)
		}
	}
	deleteOp := []search.WriteOperation{{Action: search.ActionDelete, ID: "id"}}
	for _, body := range []string{
		`{"took":1,"errors":true,"items":[{"delete":{"_id":"id","status":404,"result":"deleted"}}]}`,
		`{"took":1,"errors":false,"items":[{"delete":{"_id":"id","_version":1,"status":200,"result":"not_found"}}]}`,
	} {
		if _, err := decodeBulkResponse(deleteOp, []byte(body)); err == nil {
			t.Fatalf("bulk %q accepted", body)
		}
	}
	if state, retryable := classifyBulkItem(search.ActionDelete, http.StatusNotFound, nil); state != search.OutcomeNotFound || retryable {
		t.Fatalf("classifyBulkItem(not found) = %q/%t", state, retryable)
	}
	if validAppliedWriteResult(search.ActionUpdate, http.StatusOK, "updated") {
		t.Fatal("unsupported update result accepted")
	}
}

func TestInternalSearchEncodingAndDecodingHelpers(t *testing.T) {
	if _, err := encodeSearchRequest(search.Request{
		Query: search.RawExtensionQuery{Adapter: "other", Payload: json.RawMessage(`{"match_all":{}}`)},
		Page:  search.OffsetPage{Size: 1},
	}, search.CursorState{}, nil); !errors.Is(err, search.ErrUnsupported) {
		t.Fatalf("encodeSearchRequest() error = %v, want ErrUnsupported", err)
	}
	encodedRaw, err := encodeQuery(search.RawExtensionQuery{Adapter: "opensearch", Payload: json.RawMessage(`{"wildcard":{"tracking_code":{"value":"JJ*"}}}`)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(encodedRaw)
	if err != nil || !strings.Contains(string(payload), `"wildcard"`) {
		t.Fatal(string(payload), err)
	}
	if _, err := encodeQuery(search.RawExtensionQuery{Adapter: "other", Payload: json.RawMessage(`{}`)}, nil); !errors.Is(err, search.ErrUnsupported) {
		t.Fatal(err)
	}
	for _, raw := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`{`), json.RawMessage(`{} {}`)} {
		if _, err := encodeQuery(search.RawExtensionQuery{Adapter: "opensearch", Payload: raw}, nil); !errors.Is(err, search.ErrInvalidQuery) {
			t.Fatalf("raw %q accepted: %v", raw, err)
		}
	}

	number, _ := search.NumberValue("2.5")
	queries := []search.Query{search.MatchAllQuery{}, search.BoolQuery{Must: []search.Query{search.MatchAllQuery{}}, Should: []search.Query{search.TermQuery{Field: "f", Value: search.StringValue("v")}}, MinimumShouldMatch: 1}, search.TermQuery{Field: "f", Value: search.StringValue("v")}, search.FullTextQuery{Fields: []string{"name"}, Text: "x", Analyzer: "standard"}, search.FullTextQuery{Fields: []string{"name"}, Text: "x", Locale: "fi"}, search.PrefixQuery{Field: "f", Prefix: "x"}, search.RangeQuery{Field: "n", GT: &number, GTE: &number, LT: &number, LTE: &number}, search.ExistsQuery{Field: "f"}, search.GeoDistanceQuery{Field: "p", Origin: search.GeoPoint{}, DistanceKM: number}}
	for _, query := range queries {
		if _, err := encodeQuery(query, map[string]string{"fi": "finnish"}); err != nil {
			t.Fatalf("%T: %v", query, err)
		}
	}
	for _, query := range []search.Query{search.FullTextQuery{Fields: []string{"name"}, Text: "x", Locale: "sv"}, search.FullTextQuery{Fields: []string{"name"}, Text: "x", Locale: "fi", Analyzer: "other"}} {
		if _, err := encodeQuery(query, map[string]string{"fi": "finnish"}); !errors.Is(err, search.ErrUnsupported) {
			t.Fatal(err)
		}
	}
	if _, err := encodeQuery(search.BoolQuery{Must: []search.Query{search.FullTextQuery{Fields: []string{"name"}, Text: "x", Locale: "sv"}}}, map[string]string{}); !errors.Is(err, search.ErrUnsupported) {
		t.Fatal(err)
	}
	request := search.Request{Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: "f", Direction: search.Ascending, Missing: search.MissingFirst}}, Page: search.OffsetPage{Size: 2, Offset: 1}, Projection: search.Projection{Includes: []string{"f"}, Excludes: []string{"x"}}, Highlights: map[string]search.Highlight{"f": {FragmentSize: 1, MaxFragments: 1, PreTag: "<b>", PostTag: "</b>"}}, Aggregations: map[string]search.Aggregation{"terms": search.TermsAggregation{Field: "f", Size: 1}, "range": search.RangeAggregation{Field: "n", Buckets: []search.RangeBucket{{Key: "k", From: &number, To: &number}}}}, Suggestions: map[string]search.Suggestion{"s": search.PrefixSuggestion{Field: "f", Text: "x", Size: 1}}}
	if _, err := encodeSearchRequest(request, search.CursorState{}, nil); err != nil {
		t.Fatal(err)
	}
	request.Page = search.CursorPage{Size: 2, KeepAlive: time.Second}
	if _, err := encodeSearchRequest(request, search.CursorState{PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`1`)}}, nil); err != nil {
		t.Fatal(err)
	}
	if searchPageSize(struct{}{}) != 0 || searchPageSize(search.OffsetPage{Size: 2}) != 2 || searchPageSize(search.CursorPage{Size: 3}) != 3 {
		t.Fatal("page size")
	}
	for _, body := range []string{
		"not-json", `{} {}`, `{"took":-1}`, `{"took":9223372036855}`, `{"_shards":{"total":-1}}`,
		`{"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"i","_id":"id"}]}}`,
		`{"hits":{"total":{"value":1},"hits":[]}}`,
		`{"hits":{"total":{"value":0,"relation":"eq"},"hits":[{"_index":"i","_id":"id","_version":1}]}}`,
	} {
		if _, err := decodeSearchResponse([]byte(body)); err == nil {
			t.Fatalf("search body %q accepted", body)
		}
	}
	valid := `{"took":1,"timed_out":true,"pit_id":"p","_shards":{"total":1,"successful":0,"skipped":0,"failed":1,"failures":[{"reason":{"type":"UPPER"}}]},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"i","_id":"id","_version":1,"_source":{},"sort":[1]}]},"aggregations":{"a":{}},"suggest":{"s":[]}}`
	decoded, err := decodeSearchResponse([]byte(valid))
	if err != nil || !decoded.Diagnostics.Partial || decoded.Diagnostics.Failures[0].Code != "unknown" {
		t.Fatal(err, decoded)
	}
}

func TestInternalProjectionValidationCoversNestedObjectsAndArrays(t *testing.T) {
	tests := []struct {
		source     string
		projection search.Projection
		want       bool
	}{
		{source: `{}`, want: true},
		{source: `not-json`, projection: search.Projection{Includes: []string{"public"}}},
		{source: `null`, projection: search.Projection{Includes: []string{"public"}}},
		{source: `{} {}`, projection: search.Projection{Includes: []string{"public"}}},
		{source: `{"public":{}}`, projection: search.Projection{Includes: []string{"public"}}, want: true},
		{source: `{"public":{"name":"x"}}`, projection: search.Projection{Includes: []string{"public"}}, want: true},
		{source: `{"public":{"name":"x"}}`, projection: search.Projection{Includes: []string{"public.name"}}, want: true},
		{source: `{"public":[]}`, projection: search.Projection{Includes: []string{"public"}}, want: true},
		{source: `{"public":[{"name":"x"}]}`, projection: search.Projection{Includes: []string{"public"}}, want: true},
		{source: `{"public":[{"name":"x"},{"private":"x"}]}`, projection: search.Projection{Includes: []string{"public.name"}}},
		{source: `{"public":"x"}`, projection: search.Projection{Excludes: []string{"private"}}, want: true},
		{source: `{"private":"x"}`, projection: search.Projection{Excludes: []string{"private"}}},
		{source: `{"public":"x"}`, projection: search.Projection{Includes: []string{"other"}}},
		{source: `{"public0":"leak"}`, projection: search.Projection{Includes: []string{"public[0]"}}},
		{source: `{"public0":"leak"}`, projection: search.Projection{Includes: []string{"public?"}}},
		{source: `{"public0":"allowed"}`, projection: search.Projection{Includes: []string{"public*"}}, want: true},
	}
	for _, test := range tests {
		if got := sourceWithinProjection(json.RawMessage(test.source), test.projection); got != test.want {
			t.Fatalf("sourceWithinProjection(%s, %#v) = %t, want %t", test.source, test.projection, got, test.want)
		}
	}
}

func TestInternalWriteAuthorizationChecksCancellationBeforeAndAfterGuard(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	client := &Client{search: &SearchConfig{WriteGuard: WriteGuardFunc(func(context.Context, WriteAuthorization) error { return nil })}}
	if err := client.authorizeWrite(cancelled, OperationWrite, nil, search.RefreshNone); !errors.Is(err, context.Canceled) {
		t.Fatalf("authorizeWrite(pre-cancelled) error = %v", err)
	}

	active, cancelActive := context.WithCancel(t.Context())
	client.search.WriteGuard = WriteGuardFunc(func(context.Context, WriteAuthorization) error {
		cancelActive()
		return nil
	})
	if err := client.authorizeWrite(active, OperationBulk, nil, search.RefreshNone); !errors.Is(err, context.Canceled) {
		t.Fatalf("authorizeWrite(cancelled by guard) error = %v", err)
	}
}

func TestInternalTelemetryResilienceAndMetricHelpers(t *testing.T) {
	if _, err := newTelemetry(&TelemetryConfig{}); err == nil {
		t.Fatal("telemetry accepted")
	}
	telemetry, _ := newTelemetry(nil)
	telemetry.observe(t.Context(), TelemetryEvent{})
	telemetry, _ = newTelemetry(&TelemetryConfig{Clock: time.Now, Observer: internalObserverFunc(func(context.Context, TelemetryEvent) error { panic("x") })})
	telemetry.observe(t.Context(), TelemetryEvent{})
	for _, test := range []struct {
		err      error
		status   int
		category TelemetryCategory
	}{{ErrCredentials, 0, TelemetryCredentials}, {ErrBackpressure, 0, TelemetryBackpressure}, {ErrCircuitOpen, 0, TelemetryCircuitOpen}, {errors.New("x"), 0, TelemetryTransport}, {nil, 429, TelemetryOverloaded}, {nil, 400, TelemetryHTTPFailure}, {nil, 200, TelemetrySuccess}} {
		event := telemetry.event(OperationInfo, time.Now().Add(time.Second), test.status, test.err, ResilienceSnapshot{})
		if event.Category != test.category || event.Duration < 0 {
			t.Fatal(test, event)
		}
	}
	if operationFromContext(t.Context()) != "unknown" || operationFromContext(withOperation(t.Context(), OperationInfo)) != OperationInfo {
		t.Fatal("operation context")
	}
	for _, config := range []ResilienceConfig{{MaximumInFlight: -1}, {MaximumQueued: 1}, {MaximumQueueWait: time.Second}, {CircuitFailureThreshold: -1}, {CircuitOpenDuration: -1}} {
		if _, err := newResilienceController(config); err == nil {
			t.Fatalf("resilience %#v accepted", config)
		}
	}
	controller, _ := newResilienceController(ResilienceConfig{MaximumInFlight: 1, MaximumQueued: 1, MaximumQueueWait: time.Millisecond, CircuitFailureThreshold: 1, CircuitOpenDuration: time.Second, Clock: time.Now})
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := controller.acquire(nil); !errors.Is(err, ErrContextRequired) { //nolint:staticcheck // nil-context validation is the contract under test.
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := controller.acquire(ctx); err == nil {
		t.Fatal("cancel admitted")
	}
	permit, err := controller.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.acquire(t.Context()); !errors.Is(err, ErrBackpressure) {
		t.Fatal(err)
	}
	permit.complete(internalResponse(503, "{}"), nil, true)
	if _, err := controller.acquire(t.Context()); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal(err)
	}
	if snapshot := controller.snapshot(); snapshot.MaximumInFlight != 1 {
		t.Fatal(snapshot)
	}
	if snapshot := (*Client)(nil).ResilienceSnapshot(); snapshot.MaximumInFlight != 0 {
		t.Fatal(snapshot)
	}
	queueController, _ := newResilienceController(ResilienceConfig{MaximumInFlight: 1, MaximumQueued: 1, MaximumQueueWait: time.Second, CircuitFailureThreshold: 2, CircuitOpenDuration: time.Second, Clock: time.Now})
	first, _ := queueController.acquire(t.Context())
	queued := make(chan *resiliencePermit, 1)
	go func() { permit, _ := queueController.acquire(context.Background()); queued <- permit }()
	time.Sleep(time.Millisecond)
	first.complete(internalResponse(200, "{}"), nil, true)
	second := <-queued
	second.complete(nil, nil, false)
	timeoutController, _ := newResilienceController(ResilienceConfig{MaximumInFlight: 1, MaximumQueued: 1, MaximumQueueWait: time.Millisecond, CircuitFailureThreshold: 2, CircuitOpenDuration: time.Second, Clock: time.Now})
	held, _ := timeoutController.acquire(t.Context())
	if _, err := timeoutController.acquire(t.Context()); !errors.Is(err, ErrBackpressure) {
		t.Fatal(err)
	}
	held.complete(internalResponse(200, "{}"), nil, true)
	cancelController, _ := newResilienceController(ResilienceConfig{MaximumInFlight: 1, MaximumQueued: 1, MaximumQueueWait: time.Second, CircuitFailureThreshold: 2, CircuitOpenDuration: time.Second, Clock: time.Now})
	held, _ = cancelController.acquire(t.Context())
	waitCtx, stop := context.WithCancel(t.Context())
	waitResult := make(chan error, 1)
	go func() { _, err := cancelController.acquire(waitCtx); waitResult <- err }()
	time.Sleep(time.Millisecond)
	stop()
	if err := <-waitResult; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	held.complete(nil, nil, false)
	now := time.Now()
	probeController, _ := newResilienceController(ResilienceConfig{MaximumInFlight: 1, CircuitFailureThreshold: 1, CircuitOpenDuration: time.Second, Clock: func() time.Time { return now }})
	probePermit, _ := probeController.acquire(t.Context())
	probePermit.complete(nil, errors.New("downstream"), true)
	now = now.Add(2 * time.Second)
	probePermit, err = probeController.acquire(t.Context())
	if err != nil || !probePermit.probe {
		t.Fatal(err)
	}
	probePermit.complete(nil, nil, false)
	if !safeMetricName("search_1") || safeMetricName("") || safeMetricName(strings.Repeat("a", 65)) || safeMetricName("bad-name") {
		t.Fatal("metric names")
	}
}

func TestInternalReadBoundedStatusAndPoolPerform(t *testing.T) {
	if _, err := readBounded(strings.NewReader("x"), math.MaxInt64); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("unsafe max-bound read error = %v", err)
	}
	if _, err := readBounded(errorReader{}, 10); !errors.Is(err, ErrMalformedResponse) {
		t.Fatal(err)
	}
	if _, err := readBounded(&errorAfterLimitReader{}, 1); !errors.Is(err, ErrMalformedResponse) {
		t.Fatal(err)
	}
	status := &statusError{status: 418}
	if status.Error() == "" || !errors.Is(status, ErrUnexpectedStatus) {
		t.Fatal(status)
	}
	resilience, _ := newResilienceController(ResilienceConfig{})
	telemetry, _ := newTelemetry(nil)
	endpoint, _ := url.Parse("https://host")
	for index, next := range []http.RoundTripper{
		internalRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("x") }),
		internalRoundTripFunc(func(*http.Request) (*http.Response, error) { return &http.Response{StatusCode: 200, Body: nil}, nil }),
		internalRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: closeErrorBody{Reader: strings.NewReader("ok")}}, nil
		}),
		internalRoundTripFunc(func(*http.Request) (*http.Response, error) { return internalResponse(200, "ok"), nil }),
		internalRoundTripFunc(func(*http.Request) (*http.Response, error) { return internalResponse(200, "too-large"), nil }),
		internalRoundTripFunc(func(*http.Request) (*http.Response, error) { return internalResponse(200, "ok"), errors.New("late") }),
	} {
		pool := &poolTransport{endpoints: []*url.URL{endpoint}, next: next, maximumResponseBytes: 10, resilience: resilience, telemetry: telemetry}
		if index == 4 {
			pool.maximumResponseBytes = 1
		}
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		_, _ = pool.Perform(request)
	}
}

func TestInternalExecuteAndDiscoveryHelpers(t *testing.T) {
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	client := internalClient(t, routeBody(`{}`, 200), resolver, nil)
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := client.executeContent(nil, OperationInfo, http.MethodGet, "/", nil, "application/json", 200); !errors.Is(err, ErrContextRequired) { //nolint:staticcheck // nil-context validation is the contract under test.
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.executeContent(ctx, OperationInfo, http.MethodGet, "/", nil, "application/json", 200); err == nil {
		t.Fatal("cancel accepted")
	}
	closed := internalClient(t, routeBody(`{}`, 200), resolver, nil)
	_ = closed.Close()
	if _, err := closed.executeContent(t.Context(), OperationInfo, http.MethodGet, "/", nil, "application/json", 200); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := client.executeContent(t.Context(), OperationInfo, http.MethodGet, "%", nil, "application/json", 200); err == nil {
		t.Fatal("malformed request target accepted")
	}
	responseError := internalClient(t, func(*http.Request) (*http.Response, error) { return internalResponse(200, `{}`), errors.New("late") }, resolver, nil)
	if _, err := responseError.executeContent(t.Context(), OperationInfo, http.MethodGet, "/", nil, "application/json", 200); err == nil {
		t.Fatal("response error accepted")
	}
	oversized := internalClient(t, routeBody(strings.Repeat("x", 20), 200), resolver, nil)
	oversized.maximumResponseBytes = 1
	if _, err := oversized.executeContent(t.Context(), OperationInfo, http.MethodGet, "/", nil, "application/json", 200); err == nil {
		t.Fatal("oversize accepted")
	}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, _, err := client.executeWrite(nil, http.MethodGet, "/", nil); !errors.Is(err, ErrContextRequired) { //nolint:staticcheck // nil-context validation is the contract under test.
		t.Fatal(err)
	}
	if _, _, err := client.executeWrite(ctx, http.MethodGet, "/", nil); err == nil {
		t.Fatal("cancel write accepted")
	}
	if _, _, err := responseError.executeWrite(t.Context(), http.MethodGet, "/", nil); err == nil {
		t.Fatal("late write accepted")
	}
	oversized.maximumResponseBytes = 1
	if _, _, err := oversized.executeWrite(t.Context(), http.MethodGet, "/", nil); err == nil {
		t.Fatal("oversize write accepted")
	}

	policy := DiscoveryPolicy{MaximumNodes: 2, AllowedDNSSuffixes: []string{".example"}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}}
	discoveryClient := client
	discoveryClient.discovery = policy
	for _, address := range []string{"bad", "host.example", "host.example:notaport", "user@host.example:9200", "other.test:9200"} {
		if _, err := discoveryClient.discoveredEndpoint(address); err == nil {
			t.Fatalf("address %q accepted", address)
		}
	}
	if endpoint, err := discoveryClient.discoveredEndpoint("host.example:9200"); err != nil || endpoint.Host == "" {
		t.Fatal(endpoint, err)
	}
	if endpoint, err := discoveryClient.discoveredEndpoint("10.0.0.2:9200"); err != nil || endpoint.Host == "" {
		t.Fatal(endpoint, err)
	}
	for _, host := range []string{"host.example", "sub.host.example", "10.0.0.2"} {
		if !discoveryClient.discoveryAllows(host) {
			t.Fatalf("host %q denied", host)
		}
	}
	if discoveryClient.discoveryAllows("other.test") {
		t.Fatal("untrusted host allowed")
	}
}
