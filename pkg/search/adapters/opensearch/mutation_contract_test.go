package opensearch

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func mutationDocument(t *testing.T, id string, version uint64) search.Document {
	t.Helper()
	document, err := search.NewDocument("tenant", "events", id, version, json.RawMessage(`{"name":"value"}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestBulkWireContractAndResponseBoundaries(t *testing.T) {
	index := search.IndexDocument(mutationDocument(t, "index-id", 7))
	deleteOperation := search.DeleteDocument("tenant", "events", "delete-id", 8)
	targets := []IndexTarget{{Name: "events-v1"}, {Name: "events-v1"}}
	body := string(encodeBulkRequest([]search.WriteOperation{index, deleteOperation}, targets))
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], `"index"`) || !strings.Contains(lines[1], `"name":"value"`) || !strings.Contains(lines[2], `"delete"`) {
		t.Fatalf("unexpected NDJSON framing: %q", body)
	}
	oneBody := encodeBulkRequest([]search.WriteOperation{index}, targets[:1])
	exactClient := internalClient(t, func(*http.Request) (*http.Response, error) {
		return internalResponse(200, `{"took":0,"errors":false,"items":[{"index":{"_id":"index-id","_version":7,"status":200,"result":"updated"}}]}`), nil
	}, internalResolver{target: IndexTarget{Name: "events-v1", Fingerprint: "fingerprint"}}, nil)
	exactClient.search.Limits.MaxBulkBytes = len(oneBody)
	if _, err := exactClient.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{index}, Refresh: search.RefreshNone}); err != nil {
		t.Fatalf("exact encoded bulk body rejected: %v", err)
	}

	resolver := internalResolver{target: IndexTarget{Name: "events-v1", Fingerprint: "fingerprint"}}
	for _, test := range []struct {
		refresh search.RefreshPolicy
		query   string
	}{{search.RefreshNone, ""}, {search.RefreshWaitFor, "refresh=wait_for"}, {search.RefreshImmediate, "refresh=true"}} {
		seenQuery := "unset"
		seenPath := ""
		client := internalClient(t, func(request *http.Request) (*http.Response, error) {
			seenQuery = request.URL.RawQuery
			seenPath = request.URL.Path
			return internalResponse(200, `{"took":0,"errors":false,"items":[{"index":{"_id":"index-id","_version":7,"status":200,"result":"updated"}}]}`), nil
		}, resolver, nil)
		result, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{index}, Refresh: test.refresh})
		if err != nil || len(result.Items()) != 1 || seenQuery != test.query || seenPath != "/_bulk" {
			t.Fatalf("refresh=%q query=%q result=%#v err=%v", test.refresh, seenQuery, result, err)
		}
	}

	operation := []search.WriteOperation{index}
	invalidBodies := []string{
		`not-json`,
		`{"took":-1,"items":[{"index":{"_id":"index-id","status":200}}]}`,
		`{"took":0,"items":[]}`,
		`{"took":0,"items":[{},{}]}`,
		`{"took":0,"items":[{}]}`,
		`{"took":0,"items":[{"delete":{"_id":"index-id","status":200}}]}`,
		`{"took":0,"items":[{"index":{"_id":"other","status":200}}]}`,
		`{"took":0,"items":[{"index":{"_id":"index-id","_version":7,"status":199}}]}`,
		`{"took":0,"items":[{"index":{"_id":"index-id","status":600}}]}`,
	}
	for _, response := range invalidBodies {
		if _, err := decodeBulkResponse(operation, []byte(response)); !errors.Is(err, ErrMalformedResponse) {
			t.Fatalf("response accepted: %s (%v)", response, err)
		}
	}
	if _, err := decodeBulkResponse(nil, []byte(`not-json`)); !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("malformed empty bulk response accepted: %v", err)
	}
	for _, test := range []struct {
		status int
		state  search.OutcomeState
	}{{199, search.OutcomeUnknown}, {200, search.OutcomeApplied}, {299, search.OutcomeApplied}, {300, search.OutcomeUnknown}} {
		state, _ := classifyBulkItem(search.ActionIndex, test.status, nil)
		if state != test.state {
			t.Fatalf("status %d classified as %s", test.status, state)
		}
	}
	for _, status := range []int{200, 300, 599} {
		errorsValue := status >= 300
		version := ""
		if status < 300 {
			version = `,"_version":7`
		}
		response := `{"took":0,"errors":` + strconv.FormatBool(errorsValue) + `,"items":[{"index":{"_id":"index-id"` + version + `,"status":` + strconv.Itoa(status) + `}}]}`
		if _, err := decodeBulkResponse(operation, []byte(response)); err != nil {
			t.Fatalf("boundary status %d rejected: %v", status, err)
		}
	}
}

func TestConfigurationBoundaryContract(t *testing.T) {
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	validSearch := &SearchConfig{Limits: search.DefaultLimits(), CursorCodec: codec, Resolver: internalResolver{}, Clock: time.Now}
	if !validSearchConfig(validSearch) {
		t.Fatal("valid search configuration rejected")
	}
	invalidSearch := []*SearchConfig{}
	for index := 0; index < 4; index++ {
		candidate := *validSearch
		switch index {
		case 0:
			candidate.Limits = search.Limits{}
		case 1:
			candidate.CursorCodec = nil
		case 2:
			candidate.Resolver = nil
		case 3:
			candidate.Clock = nil
		}
		invalidSearch = append(invalidSearch, &candidate)
	}
	for _, locale := range []string{"", strings.Repeat("x", 65), "fi\n"} {
		candidate := *validSearch
		candidate.LocaleAnalyzers = map[string]string{locale: "standard"}
		invalidSearch = append(invalidSearch, &candidate)
	}
	candidate := *validSearch
	candidate.LocaleAnalyzers = map[string]string{"fi": "bad analyzer"}
	invalidSearch = append(invalidSearch, &candidate)
	for _, invalid := range invalidSearch {
		if validSearchConfig(invalid) {
			t.Fatalf("invalid search configuration accepted: %#v", invalid)
		}
	}
	candidate = *validSearch
	candidate.LocaleAnalyzers = map[string]string{strings.Repeat("x", 64): "standard"}
	if !validSearchConfig(&candidate) {
		t.Fatal("boundary locale rejected")
	}

	base := Config{Endpoints: []string{"https://host"}, RequestTimeout: time.Second, MaximumResponseBytes: 1}
	maximumEndpoints := make([]string, MaximumEndpoints)
	for index := range maximumEndpoints {
		maximumEndpoints[index] = "https://host" + strconv.Itoa(index) + ".example"
	}
	config := base
	config.Endpoints = maximumEndpoints
	if _, _, err := validateConfig(config); err != nil {
		t.Fatal(err)
	}
	config.Endpoints = append(append([]string(nil), maximumEndpoints...), "https://overflow.example")
	if _, _, err := validateConfig(config); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal(err)
	}
	for _, length := range []int{maximumEndpointBytes, maximumEndpointBytes + 1} {
		value := "https://" + strings.Repeat("a", length-len("https://"))
		_, _, err := validateEndpoint(value, false)
		if length == maximumEndpointBytes && err != nil {
			t.Fatalf("boundary endpoint rejected: %v", err)
		}
		if length > maximumEndpointBytes && !errors.Is(err, ErrUnsafeEndpoint) {
			t.Fatalf("oversized endpoint accepted: %v", err)
		}
	}
	if !loopbackHost("127.0.0.1") || loopbackHost("192.0.2.1") {
		t.Fatal("loopback classification changed")
	}

	for _, maximum := range []int{MaximumDiscoveredNodes, MaximumDiscoveredNodes + 1} {
		policy := DiscoveryPolicy{MaximumNodes: maximum, AllowedDNSSuffixes: []string{".example"}}
		err := validateDiscoveryPolicy(policy)
		if maximum == MaximumDiscoveredNodes && err != nil {
			t.Fatal(err)
		}
		if maximum > MaximumDiscoveredNodes && !errors.Is(err, ErrInvalidConfig) {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{".", ".a", ".example", ".Example", ".bad/path"} {
		err := validateDiscoveryPolicy(DiscoveryPolicy{MaximumNodes: 1, AllowedDNSSuffixes: []string{suffix}})
		valid := suffix == ".a" || suffix == ".example"
		if valid != (err == nil) {
			t.Fatalf("suffix %q validity changed: %v", suffix, err)
		}
	}

	for _, version := range []uint16{tls.VersionTLS11, tls.VersionTLS12, tls.VersionTLS13} {
		transport := configureHTTPTransport(&http.Transport{}, Config{TLS: &tls.Config{MinVersion: version}})
		expected := version
		if expected < tls.VersionTLS12 {
			expected = tls.VersionTLS12
		}
		if transport.TLSClientConfig.MinVersion != expected {
			t.Fatalf("TLS %x became %x", version, transport.TLSClientConfig.MinVersion)
		}
	}

	for _, credentials := range []BasicCredentials{
		{Username: "", Password: "p"}, {Username: "u", Password: ""},
		{Username: strings.Repeat("u", maximumCredentialBytes), Password: "p"},
		{Username: strings.Repeat("u", maximumCredentialBytes+1), Password: "p"},
		{Username: "u", Password: strings.Repeat("p", maximumCredentialBytes)},
		{Username: "u", Password: strings.Repeat("p", maximumCredentialBytes+1)},
		{Username: "u:bad", Password: "p"}, {Username: "u", Password: "p\n"},
	} {
		valid := credentials.Username != "" && credentials.Password != "" && len(credentials.Username) <= maximumCredentialBytes && len(credentials.Password) <= maximumCredentialBytes && !strings.ContainsAny(credentials.Username, ":\r\n") && !strings.ContainsAny(credentials.Password, "\r\n")
		if validCredentials(credentials) != valid {
			t.Fatalf("credential boundary changed: user=%d password=%d", len(credentials.Username), len(credentials.Password))
		}
	}
}

func TestConfigurationCollectionsAndConcurrencyAreAbsolutelyBounded(t *testing.T) {
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	locales := make(map[string]string, 257)
	for index := 0; index < 257; index++ {
		locales[fmt.Sprintf("locale-%d", index)] = "standard"
	}
	config := &SearchConfig{Limits: search.DefaultLimits(), CursorCodec: codec, Resolver: internalResolver{}, Clock: time.Now, LocaleAnalyzers: locales}
	if validSearchConfig(config) {
		t.Fatal("unbounded locale analyzer map accepted")
	}
	delete(locales, "locale-256")
	if !validSearchConfig(config) {
		t.Fatal("maximum locale analyzer map rejected")
	}
	rules := make([]string, 257)
	for index := range rules {
		rules[index] = ".example.test"
	}
	if validateDiscoveryPolicy(DiscoveryPolicy{MaximumNodes: 1, AllowedDNSSuffixes: rules}) == nil {
		t.Fatal("unbounded discovery allowlist accepted")
	}
	rules = rules[:MaximumDiscoveredNodes]
	if err := validateDiscoveryPolicy(DiscoveryPolicy{MaximumNodes: 1, AllowedDNSSuffixes: rules}); err != nil {
		t.Fatalf("maximum discovery allowlist rejected: %v", err)
	}
	mixed := DiscoveryPolicy{MaximumNodes: 1, AllowedDNSSuffixes: rules[:MaximumDiscoveredNodes/2]}
	for range MaximumDiscoveredNodes / 2 {
		mixed.AllowedCIDRs = append(mixed.AllowedCIDRs, netip.MustParsePrefix("10.0.0.0/8"))
	}
	if err := validateDiscoveryPolicy(mixed); err != nil {
		t.Fatalf("maximum mixed discovery allowlist rejected: %v", err)
	}
	cidrs := make([]netip.Prefix, MaximumDiscoveredNodes+1)
	for index := range cidrs {
		cidrs[index] = netip.MustParsePrefix("10.0.0.0/8")
	}
	if err := validateDiscoveryPolicy(DiscoveryPolicy{MaximumNodes: 1, AllowedCIDRs: cidrs[:MaximumDiscoveredNodes]}); err != nil {
		t.Fatalf("maximum CIDR allowlist rejected: %v", err)
	}
	if validateDiscoveryPolicy(DiscoveryPolicy{MaximumNodes: 1, AllowedCIDRs: cidrs}) == nil {
		t.Fatal("unbounded CIDR allowlist accepted")
	}
	mixed.AllowedDNSSuffixes = rules[:MaximumDiscoveredNodes/2+1]
	if validateDiscoveryPolicy(mixed) == nil {
		t.Fatal("unbounded mixed discovery allowlist accepted")
	}
	if controller, err := newResilienceController(ResilienceConfig{MaximumInFlight: 4097}); err == nil || controller != nil {
		t.Fatal("unbounded concurrency allocation accepted")
	}
	if controller, err := newResilienceController(ResilienceConfig{MaximumInFlight: maximumInFlight}); err != nil || controller == nil {
		t.Fatalf("maximum concurrency rejected: %v", err)
	}
	if controller, err := newResilienceController(ResilienceConfig{MaximumInFlight: 1, MaximumQueued: 65537, MaximumQueueWait: time.Second}); err == nil || controller != nil {
		t.Fatal("unbounded queue accepted")
	}
	if controller, err := newResilienceController(ResilienceConfig{MaximumInFlight: 1, MaximumQueued: maximumQueued, MaximumQueueWait: time.Second}); err != nil || controller == nil {
		t.Fatalf("maximum queue rejected: %v", err)
	}
}

func TestPITProjectionAndSearchResponsePredicatesAreIndependent(t *testing.T) {
	for _, body := range []string{"{", `{}`} {
		client := internalClient(t, routeBody(body, http.StatusCreated), internalResolver{}, nil)
		if _, err := client.createPIT(t.Context(), "events-v1", time.Second); err == nil {
			t.Fatalf("invalid PIT response accepted: %q", body)
		}
	}

	for _, projection := range []search.Projection{
		{Includes: []string{"name"}},
		{Excludes: []string{"secret"}},
	} {
		body, err := encodeSearchRequest(search.Request{Query: search.MatchAllQuery{}, Page: search.OffsetPage{Size: 1}, Projection: projection}, search.CursorState{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var encoded map[string]any
		if err := json.Unmarshal(body, &encoded); err != nil {
			t.Fatal(err)
		}
		if _, ok := encoded["_source"]; !ok {
			t.Fatalf("projection omitted from %s", body)
		}
	}

	maximumTook := math.MaxInt64 / int64(time.Millisecond)
	validBodies := []string{
		fmt.Sprintf(`{"took":%d,"_shards":{"total":0,"successful":0,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`, maximumTook),
		`{"took":0,"_shards":{"total":0,"successful":0,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"gte"},"hits":[]}}`,
	}
	for _, body := range validBodies {
		if _, err := decodeSearchResponse([]byte(body)); err != nil {
			t.Fatalf("valid search boundary rejected: %v", err)
		}
	}
	invalidBodies := []string{
		fmt.Sprintf(`{"took":%d,"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`, maximumTook+1),
		`{"_shards":{"total":-1},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		`{"_shards":{"successful":-1},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		`{"_shards":{"skipped":-1},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		`{"_shards":{"failed":-1},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		`{"hits":{"total":{"value":0,"relation":"other"},"hits":[]}}`,
		`{"hits":{"total":{"value":0,"relation":"eq"},"hits":[{"_index":"i","_id":"id","_version":1}]}}`,
	}
	for _, body := range invalidBodies {
		if _, err := decodeSearchResponse([]byte(body)); err == nil {
			t.Fatalf("invalid search predicate accepted: %s", body)
		}
	}
}

type validCredentialsWithError struct{}

func (validCredentialsWithError) Credentials(context.Context) (BasicCredentials, error) {
	return BasicCredentials{Username: "valid", Password: "valid"}, errors.New("rotation failed")
}

func TestPoolRotationAndEndpointOrder(t *testing.T) {
	resilience, _ := newResilienceController(ResilienceConfig{})
	telemetry, _ := newTelemetry(nil)
	first, _ := url.Parse("https://first.example")
	second, _ := url.Parse("https://second.example")
	third, _ := url.Parse("https://third.example")
	hosts := make([]string, 0, 3)
	pool := &poolTransport{
		endpoints: []*url.URL{first, second, third}, maximumResponseBytes: 1024, resilience: resilience, telemetry: telemetry,
		next: internalRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			hosts = append(hosts, request.URL.Host)
			return internalResponse(200, `{}`), nil
		}),
	}
	pool.cursor.Store(^uint64(0))
	for range 4 {
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		if _, err := pool.Stream(request); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(hosts, ",") != "first.example,second.example,third.example,first.example" {
		t.Fatal(hosts)
	}
	pool.basic = validCredentialsWithError{}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	if _, err := pool.Stream(request); !errors.Is(err, ErrCredentials) {
		t.Fatal(err)
	}
}

type trackingBody struct {
	io.Reader
	closed bool
}

func (body *trackingBody) Close() error { body.closed = true; return nil }

func TestDiscoveryBoundaryContract(t *testing.T) {
	policy := DiscoveryPolicy{MaximumNodes: 2, AllowedDNSSuffixes: []string{".example"}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}}
	body := &trackingBody{Reader: strings.NewReader(`{}`)}
	client := internalClient(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body}, errors.New("late")
	}, nil, nil)
	client.discovery = policy
	if _, err := client.Discover(t.Context()); !errors.Is(err, ErrTransport) || !body.closed {
		t.Fatal(err, body.closed)
	}
	for _, address := range []string{"host.example:", ":9200", "host.example:0", "host.example:1", "host.example:65535", "host.example:65536"} {
		_, err := client.discoveredEndpoint(address)
		valid := address == "host.example:1" || address == "host.example:65535"
		if valid != (err == nil) {
			t.Fatalf("address %q validity changed: %v", address, err)
		}
	}
	if client.discoveryAllows("example") || client.discoveryAllows(".example") || !client.discoveryAllows("a.example") {
		t.Fatal("DNS suffix boundary changed")
	}
}

func TestFailureDiagnosticAndClassificationContract(t *testing.T) {
	failure := &Failure{Operation: OperationSearch, Category: FailureRejected, Status: 418, Code: "safe_code"}
	if failure.Error() != "search/opensearch: search failed (rejected, HTTP 418, code safe_code)" {
		t.Fatal(failure.Error())
	}
	failure.Status, failure.Code = 0, ""
	if failure.Error() != "search/opensearch: search failed (rejected)" {
		t.Fatal(failure.Error())
	}
	for _, test := range []struct {
		cause    error
		category FailureCategory
	}{
		{context.Canceled, FailureCancelled}, {context.DeadlineExceeded, FailureCancelled},
		{ErrBackpressure, FailureBackpressure}, {ErrCircuitOpen, FailureCircuitOpen}, {errors.New("network"), FailureTransport},
	} {
		got := transportFailure(OperationInfo, test.cause)
		if got.Category != test.category {
			t.Fatalf("%v became %s", test.cause, got.Category)
		}
	}
	for _, test := range []struct {
		operation Operation
		status    int
		code      string
		category  FailureCategory
	}{
		{OperationInfo, 429, "unknown", FailureOverloaded},
		{OperationInfo, 503, "unknown", FailureOverloaded},
		{OperationInfo, 403, "cluster_block_exception", FailureClusterBlocked},
		{OperationInfo, 409, "unknown", FailureVersionConflict},
		{OperationInfo, 400, "version_conflict_engine_exception", FailureVersionConflict},
		{OperationInfo, 400, "mapper_parsing_exception", FailureMappingRejected},
		{OperationInfo, 400, "strict_dynamic_mapping_exception", FailureMappingRejected},
		{OperationSearch, 404, "resource_not_found_exception", FailurePITExpired},
		{OperationInfo, 404, "resource_not_found_exception", FailureRejected},
	} {
		body := []byte(`{"error":{"type":"` + test.code + `"}}`)
		got := responseFailure(test.operation, test.status, body)
		if got.Category != test.category {
			t.Fatalf("operation=%s status=%d code=%s became %s", test.operation, test.status, test.code, got.Category)
		}
	}
	for _, test := range []struct {
		code  string
		valid bool
	}{
		{"a", true}, {strings.Repeat("a", 128), true}, {strings.Repeat("a", 129), false},
		{"0", true}, {"9", true}, {"a", true}, {"z", true}, {"_", true}, {"a__b", false}, {"A", false}, {"-", false},
	} {
		if safeErrorCode(test.code) != test.valid {
			t.Fatalf("code %q validity changed", test.code)
		}
	}
}

func TestHealthValidationAndReadinessContract(t *testing.T) {
	valid := map[string]any{
		"cluster_name": "cluster", "status": "green", "timed_out": false,
		"number_of_nodes": 1, "number_of_data_nodes": 1, "active_primary_shards": 1,
		"active_shards": 1, "relocating_shards": 0, "initializing_shards": 0,
		"unassigned_shards": 0, "number_of_pending_tasks": 0, "active_shards_percent_as_number": 100,
	}
	invalid := []map[string]any{}
	for key, value := range map[string]any{
		"number_of_nodes": -1, "number_of_data_nodes": -1, "active_primary_shards": -1,
		"active_shards": -1, "relocating_shards": -1, "initializing_shards": -1,
		"unassigned_shards": -1, "number_of_pending_tasks": -1, "active_shards_percent_as_number": -1,
	} {
		copyValue := maps.Clone(valid)
		copyValue[key] = value
		invalid = append(invalid, copyValue)
	}
	overData := maps.Clone(valid)
	overData["number_of_data_nodes"] = 2
	invalid = append(invalid, overData)
	overPercent := maps.Clone(valid)
	overPercent["active_shards_percent_as_number"] = 101
	invalid = append(invalid, overPercent)
	for _, payload := range invalid {
		body, _ := json.Marshal(payload)
		client := internalClient(t, routeBody(string(body), 200), nil, nil)
		if _, err := client.Health(t.Context()); err == nil {
			t.Fatalf("invalid health accepted: %s", body)
		}
	}
	readiness := []struct {
		key   string
		value any
		ready bool
	}{
		{"status", "yellow", true}, {"status", "red", false}, {"timed_out", true, false},
		{"number_of_data_nodes", 0, false},
		{"active_primary_shards", 0, false}, {"initializing_shards", 1, false},
	}
	zero := maps.Clone(valid)
	zero["number_of_nodes"] = 0
	zero["number_of_data_nodes"] = 0
	zero["active_primary_shards"] = 0
	zeroBody, _ := json.Marshal(zero)
	zeroClient := internalClient(t, routeBody(string(zeroBody), 200), nil, nil)
	if report, err := zeroClient.Health(t.Context()); err != nil || report.Ready {
		t.Fatal(report, err)
	}
	for _, test := range readiness {
		payload := maps.Clone(valid)
		payload[test.key] = test.value
		body, _ := json.Marshal(payload)
		client := internalClient(t, routeBody(string(body), 200), nil, nil)
		report, err := client.Health(t.Context())
		if err != nil || report.Ready != test.ready {
			t.Fatalf("%s=%v ready=%v err=%v", test.key, test.value, report.Ready, err)
		}
	}
}

func TestCapacityValidationContract(t *testing.T) {
	cluster := map[string]any{
		"_nodes":  map[string]any{"total": 1, "successful": 1, "failed": 0},
		"indices": map[string]any{"count": 0, "shards": map[string]any{"total": 0, "primaries": 0}},
		"nodes":   map[string]any{"count": map[string]any{"total": 1, "data": 0}},
	}
	nodes := `{"_nodes":{"total":1,"successful":1,"failed":0},"nodes":{}}`
	paths := func(clusterBody []byte, nodesBody string) *Client {
		calls := 0
		return internalClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return internalResponse(200, string(clusterBody)), nil
			}
			return internalResponse(200, nodesBody), nil
		}, nil, nil)
	}
	mutations := []func(map[string]any){
		func(value map[string]any) { value["_nodes"].(map[string]any)["total"] = 0 },
		func(value map[string]any) { value["_nodes"].(map[string]any)["successful"] = 0 },
		func(value map[string]any) { value["_nodes"].(map[string]any)["failed"] = 1 },
		func(value map[string]any) { value["nodes"].(map[string]any)["count"].(map[string]any)["total"] = 0 },
		func(value map[string]any) { value["nodes"].(map[string]any)["count"].(map[string]any)["data"] = -1 },
		func(value map[string]any) { value["nodes"].(map[string]any)["count"].(map[string]any)["data"] = 2 },
		func(value map[string]any) { value["indices"].(map[string]any)["count"] = -1 },
		func(value map[string]any) { value["indices"].(map[string]any)["shards"].(map[string]any)["total"] = -1 },
		func(value map[string]any) {
			value["indices"].(map[string]any)["shards"].(map[string]any)["primaries"] = -1
		},
	}
	for _, mutate := range mutations {
		value := deepCloneMap(t, cluster)
		mutate(value)
		body, _ := json.Marshal(value)
		if _, err := paths(body, nodes).Capacity(t.Context()); err == nil {
			t.Fatalf("invalid capacity accepted: %s", body)
		}
	}
	validBody, _ := json.Marshal(cluster)
	validClient := paths(validBody, nodes)
	if report, err := validClient.Capacity(t.Context()); err != nil || report.Nodes != 1 || report.DataNodes != 0 {
		t.Fatal(report, err)
	}
	zeroCluster := deepCloneMap(t, cluster)
	zeroCluster["_nodes"] = map[string]any{"total": 0, "successful": 0, "failed": 0}
	zeroBody, _ := json.Marshal(zeroCluster)
	if _, err := paths(zeroBody, nodes).Capacity(t.Context()); err == nil {
		t.Fatal("zero cluster response accepted")
	}
	allData := deepCloneMap(t, cluster)
	allData["nodes"].(map[string]any)["count"].(map[string]any)["data"] = 1
	allDataBody, _ := json.Marshal(allData)
	if _, err := paths(allDataBody, nodes).Capacity(t.Context()); err != nil {
		t.Fatalf("all-data cluster rejected: %v", err)
	}
	malformedSuffix := append(append([]byte(nil), validBody...), byte('!'))
	if _, err := paths(malformedSuffix, nodes).Capacity(t.Context()); err == nil {
		t.Fatal("malformed cluster response accepted")
	}
	nodeBodies := []string{
		`{"_nodes":{"total":0,"successful":0,"failed":0},"nodes":{}}`,
		`{"_nodes":{"total":1,"successful":0,"failed":0},"nodes":{}}`,
		`{"_nodes":{"total":1,"successful":1,"failed":1},"nodes":{}}`,
	}
	for _, body := range nodeBodies {
		if _, err := paths(validBody, body).Capacity(t.Context()); err == nil {
			t.Fatalf("invalid node capacity accepted: %s", body)
		}
	}
	tooMany := make(map[string]any, MaximumDiscoveredNodes+1)
	for index := 0; index <= MaximumDiscoveredNodes; index++ {
		tooMany[strconv.Itoa(index)] = map[string]any{}
	}
	tooManyBody, _ := json.Marshal(map[string]any{"_nodes": map[string]any{"total": 1, "successful": 1}, "nodes": tooMany})
	if _, err := paths(validBody, string(tooManyBody)).Capacity(t.Context()); err == nil {
		t.Fatal("oversized node statistics accepted")
	}
	maximumNodes := make(map[string]any, MaximumDiscoveredNodes)
	for index := 0; index < MaximumDiscoveredNodes; index++ {
		maximumNodes[strconv.Itoa(index)] = map[string]any{}
	}
	maximumBody, _ := json.Marshal(map[string]any{"_nodes": map[string]any{"total": 1, "successful": 1}, "nodes": maximumNodes})
	if _, err := paths(validBody, string(maximumBody)).Capacity(t.Context()); err != nil {
		t.Fatal(err)
	}
	overflowBodies := []string{
		`{"_nodes":{"total":2,"successful":2,"failed":0},"nodes":{"a":{"thread_pool":{"search":{"rejected":18446744073709551615}}},"b":{"thread_pool":{"search":{"rejected":1}}}}}`,
		`{"_nodes":{"total":2,"successful":2,"failed":0},"nodes":{"a":{"breakers":{"request":{"tripped":18446744073709551615}}},"b":{"breakers":{"request":{"tripped":1}}}}}`,
	}
	for _, body := range overflowBodies {
		if _, err := paths(validBody, body).Capacity(t.Context()); err == nil {
			t.Fatalf("overflowing capacity accepted: %s", body)
		}
	}
	maximumMetrics := `{"_nodes":{"total":1,"successful":1,"failed":0},"nodes":{"a":{"thread_pool":{"search":{"rejected":18446744073709551615}},"breakers":{"request":{"tripped":18446744073709551615}}}}}`
	maximumReport, err := paths(validBody, maximumMetrics).Capacity(t.Context())
	if err != nil || maximumReport.ThreadPoolRejected["search"] != math.MaxUint64 || maximumReport.BreakerTripped["request"] != math.MaxUint64 {
		t.Fatal(maximumReport, err)
	}
	for _, metric := range []string{"a", "z", "0", "9", "_", strings.Repeat("a", 64)} {
		if !safeMetricName(metric) {
			t.Fatalf("safe metric %q rejected", metric)
		}
	}
}

func deepCloneMap(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(body, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func TestInfoAndBoundedReadContract(t *testing.T) {
	valid := map[string]any{
		"name": "node", "cluster_name": "cluster", "cluster_uuid": "uuid",
		"version": map[string]any{"number": "3.6.0"},
	}
	for _, missing := range []string{"name", "cluster_name", "cluster_uuid", "version"} {
		payload := deepCloneMap(t, valid)
		delete(payload, missing)
		body, _ := json.Marshal(payload)
		client := internalClient(t, routeBody(string(body), 200), nil, nil)
		if _, err := client.Info(t.Context()); err == nil {
			t.Fatalf("missing %s accepted", missing)
		}
	}
	for _, test := range []struct {
		body    string
		maximum int64
		valid   bool
	}{{"a", 1, true}, {"ab", 1, false}, {"", 0, true}, {"a", 0, false}} {
		body, err := readBounded(strings.NewReader(test.body), test.maximum)
		if test.valid && (err != nil || string(body) != test.body) {
			t.Fatal(test, string(body), err)
		}
		if !test.valid && !errors.Is(err, ErrResponseTooLarge) {
			t.Fatal(test, err)
		}
	}
}

func TestLifecycleResponseBoundaryContract(t *testing.T) {
	authorize := LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil })
	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, response := range []string{
		`{"acknowledged":true,"shards_acknowledged":false}`,
		`{"acknowledged":false,"shards_acknowledged":true}`,
		`{"acknowledged":true,"shards_acknowledged":true}`,
	} {
		client := internalClient(t, routeBody(response, 200), nil, authorize)
		err := client.CreateIndex(t.Context(), "tenant", definition)
		valid := response == `{"acknowledged":true,"shards_acknowledged":true}`
		if valid != (err == nil) {
			t.Fatalf("create response %s: %v", response, err)
		}
	}

	for _, length := range []int{512, 513} {
		task := strings.Repeat("t", length)
		client := internalClient(t, routeBody(`{"task":"`+task+`"}`, 200), nil, authorize)
		cursor, _, err := client.Reindex(t.Context(), "tenant", "source", "target", "")
		if length == 512 && (err != nil || cursor != task) {
			t.Fatal(length, len(cursor), err)
		}
		if length == 513 && err == nil {
			t.Fatal("oversized task accepted")
		}
	}
	for _, length := range []int{512, 513} {
		cursor := strings.Repeat("t", length)
		client := internalClient(t, routeBody(`{"completed":false}`, 200), nil, authorize)
		returned, done, err := client.Reindex(t.Context(), "tenant", "source", "target", cursor)
		if length == 512 && (err != nil || done || returned != cursor) {
			t.Fatal(length, done, err)
		}
		if length == 513 && !errors.Is(err, ErrLifecycleRejected) {
			t.Fatal("oversized cursor accepted", err)
		}
	}

	for _, response := range []string{
		`{"completed":true,"response":{"total":3,"created":1,"updated":2,"version_conflicts":0,"failures":[]}}`,
		`{"completed":true,"response":{"total":4,"created":1,"updated":2,"version_conflicts":0,"failures":[]}}`,
		`{"completed":true,"response":{"total":3,"created":1,"updated":2,"version_conflicts":1,"failures":[]}}`,
		`{"completed":true,"response":{"total":3,"created":1,"updated":2,"version_conflicts":0,"failures":[{}]}}`,
		`{"completed":true,"error":{},"response":{"total":3,"created":1,"updated":2}}`,
		`{"completed":true,"response":{"total":0,"created":18446744073709551615,"updated":1,"version_conflicts":0,"failures":[]}}`,
	} {
		client := internalClient(t, routeBody(response, 200), nil, authorize)
		_, done, err := client.Reindex(t.Context(), "tenant", "source", "target", "task")
		valid := strings.Contains(response, `"total":3`) && strings.Contains(response, `"version_conflicts":0`) && strings.Contains(response, `"failures":[]`) && !strings.Contains(response, `"error"`)
		if valid != (err == nil && done) {
			t.Fatalf("reindex response %s: done=%v err=%v", response, done, err)
		}
	}

	for _, response := range []string{
		`{"count":1,"_shards":{"total":1,"successful":1,"failed":0}}`,
		`{"count":1,"_shards":{"total":0,"successful":0,"failed":0}}`,
		`{"count":1,"_shards":{"total":1,"successful":0,"failed":0}}`,
		`{"count":1,"_shards":{"total":1,"successful":1,"failed":1}}`,
	} {
		client := internalClient(t, routeBody(response, 200), nil, authorize)
		count, err := client.countIndex(t.Context(), "index")
		valid := response == `{"count":1,"_shards":{"total":1,"successful":1,"failed":0}}`
		if valid && (err != nil || count != 1) {
			t.Fatal(response, count, err)
		}
		if !valid && err == nil {
			t.Fatal("invalid count accepted", response)
		}
	}

	for _, response := range []string{
		`{"index":{"aliases":{"alias":{}}}}`,
		`{"index":{"aliases":{"other":{}}}}`,
		`{"a":{"aliases":{"alias":{}}},"b":{"aliases":{"alias":{}}}}`,
	} {
		client := internalClient(t, routeBody(response, 200), nil, authorize)
		index, err := client.ResolveAlias(t.Context(), "tenant", "alias")
		valid := response == `{"index":{"aliases":{"alias":{}}}}`
		if valid && (err != nil || index != "index") {
			t.Fatal(response, index, err)
		}
		if !valid && err == nil {
			t.Fatal("invalid alias accepted", response)
		}
	}

	client := internalClient(t, routeBody(`{"acknowledged":true}`, 200), nil, authorize)
	for _, call := range []func() error{
		func() error { return client.SwapAlias(t.Context(), "", "alias", "a", "b") },
		func() error { return client.SwapAlias(t.Context(), "tenant", "bad/name", "a", "b") },
		func() error { return client.SwapAlias(t.Context(), "tenant", "alias", "bad/name", "b") },
		func() error { return client.SwapAlias(t.Context(), "tenant", "alias", "a", "bad/name") },
		func() error { return client.SwapAlias(t.Context(), "tenant", "alias", "a", "a") },
		func() error { return client.AddAlias(t.Context(), "", "alias", "index", false) },
		func() error { return client.AddAlias(t.Context(), "tenant", "bad/name", "index", false) },
		func() error { return client.AddAlias(t.Context(), "tenant", "alias", "bad/name", false) },
	} {
		if !errors.Is(call(), ErrUnsafeIndexTarget) {
			t.Fatal("unsafe lifecycle target accepted")
		}
	}
	counts := 0
	equal := internalClient(t, func(*http.Request) (*http.Response, error) {
		counts++
		return internalResponse(200, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
	}, nil, authorize)
	if report, err := equal.VerifyIndex(t.Context(), "tenant", "source", "target"); err != nil || !report.Verified || report.Drift != 0 || counts != 2 {
		t.Fatal(report, err)
	}
	failedAdd := internalClient(t, routeBody(`{}`, 500), nil, authorize)
	if err := failedAdd.AddAlias(t.Context(), "tenant", "alias", "index", false); !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatal(err)
	}
}

func waitForQueueDepth(t *testing.T, controller *resilienceController, depth int) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for controller.snapshot().Queued != depth {
		select {
		case <-deadline.C:
			t.Fatalf("queue depth did not reach %d: %#v", depth, controller.snapshot())
		case <-poll.C:
		}
	}
}

func TestResilienceCountersAndQueueBoundaries(t *testing.T) {
	controller, err := newResilienceController(ResilienceConfig{
		MaximumInFlight: 1, MaximumQueued: 1, MaximumQueueWait: time.Minute,
		CircuitFailureThreshold: 2, CircuitOpenDuration: time.Second, Clock: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := controller.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	secondResult := make(chan *resiliencePermit, 1)
	secondError := make(chan error, 1)
	go func() {
		permit, acquireErr := controller.acquire(context.Background())
		secondResult <- permit
		secondError <- acquireErr
	}()
	waitForQueueDepth(t, controller, 1)
	overflowContext, cancelOverflow := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancelOverflow()
	if _, err := controller.acquire(overflowContext); !errors.Is(err, ErrBackpressure) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("full queue outcome = %v", err)
	}
	if snapshot := controller.snapshot(); snapshot.Admissions != 1 || snapshot.Rejections != 1 || snapshot.Queued != 1 || snapshot.InFlight != 1 {
		t.Fatal(snapshot)
	}
	first.complete(internalResponse(200, `{}`), nil, true)
	second := <-secondResult
	if err := <-secondError; err != nil || second == nil {
		t.Fatal(err)
	}
	if snapshot := controller.snapshot(); snapshot.Admissions != 2 || snapshot.Rejections != 1 || snapshot.Queued != 0 || snapshot.InFlight != 1 {
		t.Fatal(snapshot)
	}
	second.complete(internalResponse(200, `{}`), nil, true)

	timeout, _ := newResilienceController(ResilienceConfig{
		MaximumInFlight: 1, MaximumQueued: 1, MaximumQueueWait: time.Millisecond,
		CircuitFailureThreshold: 2, CircuitOpenDuration: time.Second, Clock: time.Now,
	})
	held, _ := timeout.acquire(t.Context())
	if _, err := timeout.acquire(t.Context()); !errors.Is(err, ErrBackpressure) {
		t.Fatal(err)
	}
	if snapshot := timeout.snapshot(); snapshot.Queued != 0 || snapshot.Admissions != 1 || snapshot.Rejections != 1 {
		t.Fatal(snapshot)
	}
	held.complete(internalResponse(200, `{}`), nil, true)
	deadlineController, _ := newResilienceController(ResilienceConfig{
		MaximumInFlight: 1, MaximumQueued: 1, MaximumQueueWait: time.Second,
		CircuitFailureThreshold: 2, CircuitOpenDuration: time.Second, Clock: time.Now,
	})
	deadlineHeld, _ := deadlineController.acquire(t.Context())
	deadlineContext, cancelDeadline := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancelDeadline()
	if _, err := deadlineController.acquire(deadlineContext); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrBackpressure) {
		t.Fatalf("parent deadline classified as %v", err)
	}
	deadlineHeld.complete(internalResponse(200, `{}`), nil, true)

	now := time.Now()
	circuit, _ := newResilienceController(ResilienceConfig{
		MaximumInFlight: 1, CircuitFailureThreshold: 1, CircuitOpenDuration: time.Minute,
		Clock: func() time.Time { return now },
	})
	permit, _ := circuit.acquire(t.Context())
	permit.complete(internalResponse(503, `{}`), nil, true)
	if snapshot := circuit.snapshot(); !snapshot.CircuitOpen || snapshot.ConsecutiveFailures != 1 || snapshot.Admissions != 1 {
		t.Fatal(snapshot)
	}
	if _, err := circuit.acquire(t.Context()); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal(err)
	}
	if snapshot := circuit.snapshot(); snapshot.Rejections != 1 {
		t.Fatal(snapshot)
	}
	now = now.Add(2 * time.Minute)
	if circuit.snapshot().CircuitOpen {
		t.Fatal("expired circuit reported open")
	}
	defaultCircuit, err := newResilienceController(ResilienceConfig{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	for range DefaultCircuitFailureThreshold {
		permit, acquireErr := defaultCircuit.acquire(t.Context())
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		permit.complete(nil, errors.New("downstream"), true)
	}
	now = now.Add(time.Nanosecond)
	if _, err := defaultCircuit.acquire(t.Context()); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("default circuit reopened early: %v", err)
	}
}

func TestTelemetryAndTemplateConfigurationBoundaries(t *testing.T) {
	observer := internalObserverFunc(func(context.Context, TelemetryEvent) error { return nil })
	for _, config := range []*TelemetryConfig{{Observer: observer}, {Clock: time.Now}} {
		if _, err := newTelemetry(config); !errors.Is(err, ErrInvalidConfig) {
			t.Fatal(err)
		}
	}
	clock := time.Unix(10, 0)
	telemetry, err := newTelemetry(&TelemetryConfig{Observer: observer, Clock: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	if event := telemetry.event(OperationInfo, clock.Add(time.Second), 200, nil, ResilienceSnapshot{}); event.Duration != 0 {
		t.Fatal(event)
	}

	authorize := LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil })
	definition, _ := search.NewIndexDefinition("index", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	client := internalClient(t, routeBody(`{"acknowledged":true}`, 200), nil, authorize)
	patterns := make([]string, MaximumIndexPatterns)
	for index := range patterns {
		patterns[index] = "events-" + strconv.Itoa(index) + "-*"
	}
	if err := client.PutIndexTemplate(t.Context(), "tenant", "template", patterns, 0, definition); err != nil {
		t.Fatal(err)
	}
	invalidCalls := []func() error{
		func() error {
			return client.PutIndexTemplate(t.Context(), "", "template", []string{"events-*"}, 0, definition)
		},
		func() error {
			return client.PutIndexTemplate(t.Context(), "tenant", "bad/name", []string{"events-*"}, 0, definition)
		},
		func() error { return client.PutIndexTemplate(t.Context(), "tenant", "template", nil, 0, definition) },
		func() error {
			return client.PutIndexTemplate(t.Context(), "tenant", "template", append(patterns, "overflow-*"), 0, definition)
		},
		func() error {
			return client.PutIndexTemplate(t.Context(), "tenant", "template", []string{"events-*"}, -1, definition)
		},
		func() error {
			return client.PutIndexTemplate(t.Context(), "tenant", "template", []string{"events-*"}, 0, search.IndexDefinition{})
		},
	}
	for _, call := range invalidCalls {
		if !errors.Is(call(), ErrUnsafeIndexTarget) {
			t.Fatal("invalid template accepted")
		}
	}
	if err := client.DeleteIndexTemplate(t.Context(), "", "template"); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}
}

func assertJSONEqual(t *testing.T, actual []byte, expected string) {
	t.Helper()
	var actualValue, expectedValue any
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(expected), &expectedValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualValue, expectedValue) {
		t.Fatalf("JSON mismatch\nactual: %s\nexpected: %s", actual, expected)
	}
}

func TestSearchEncodingWireContract(t *testing.T) {
	number, _ := search.NumberValue("2.5")
	request := search.Request{
		Query: search.BoolQuery{
			Must:   []search.Query{search.TermQuery{Field: "kind", Value: search.StringValue("event")}},
			Should: []search.Query{search.PrefixQuery{Field: "name", Prefix: "pre"}}, MinimumShouldMatch: 1,
		},
		Sort:       []search.Sort{{Field: "created_at", Direction: search.Descending, Missing: search.MissingLast}},
		Page:       search.OffsetPage{Size: 2, Offset: 1},
		Projection: search.Projection{Includes: []string{"name"}, Excludes: []string{"secret"}},
		Highlights: map[string]search.Highlight{"name": {FragmentSize: 20, MaxFragments: 2, PreTag: "<em>", PostTag: "</em>"}},
		Aggregations: map[string]search.Aggregation{
			"kinds":  search.TermsAggregation{Field: "kind", Size: 3},
			"ranges": search.RangeAggregation{Field: "score", Buckets: []search.RangeBucket{{Key: "middle", From: &number, To: &number}}},
		},
		Suggestions: map[string]search.Suggestion{"names": search.PrefixSuggestion{Field: "name_suggest", Text: "pre", Size: 4}},
	}
	body, err := encodeSearchRequest(request, search.CursorState{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, body, `{
		"query":{"bool":{"must":[{"term":{"kind":"event"}}],"should":[{"prefix":{"name":{"value":"pre"}}}],"minimum_should_match":1}},
		"size":2,"from":1,"version":true,
		"sort":[{"created_at":{"order":"desc","missing":"_last"}}],
		"_source":{"includes":["name"],"excludes":["secret"]},
		"highlight":{"fields":{"name":{"fragment_size":20,"number_of_fragments":2,"pre_tags":["<em>"],"post_tags":["</em>"]}}},
		"aggs":{"kinds":{"terms":{"field":"kind","size":3}},"ranges":{"range":{"field":"score","ranges":[{"key":"middle","from":2.5,"to":2.5}]}}},
		"suggest":{"names":{"prefix":"pre","completion":{"field":"name_suggest","size":4,"skip_duplicates":true}}}
	}`)

	request = search.Request{Query: search.MatchAllQuery{}, Page: search.CursorPage{Size: 1, KeepAlive: 1500 * time.Millisecond}}
	body, err = encodeSearchRequest(request, search.CursorState{PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"id"`)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, body, `{"query":{"match_all":{}},"size":1,"version":true,"pit":{"id":"pit","keep_alive":"1500ms"},"search_after":["id"]}`)

	for _, query := range []search.Query{
		search.BoolQuery{},
		search.BoolQuery{Should: []search.Query{search.MatchAllQuery{}}, MinimumShouldMatch: 0},
		search.RangeQuery{Field: "score"},
	} {
		if _, err := encodeQuery(query, nil); err != nil {
			t.Fatal(err)
		}
	}
	zeroMinimum, err := encodeQuery(search.BoolQuery{Should: []search.Query{search.MatchAllQuery{}}, MinimumShouldMatch: 0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	zeroMinimumBody, _ := json.Marshal(zeroMinimum)
	assertJSONEqual(t, zeroMinimumBody, `{"bool":{"should":[{"match_all":{}}]}}`)
}

func TestSearchCursorStateAndResponseBoundaries(t *testing.T) {
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", Fingerprint: "fingerprint"}}
	response := `{"took":2,"pit_id":"rotated","_shards":{"total":0,"successful":0,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"]}]}}`
	decoded, err := decodeSearchResponse([]byte(response))
	if err != nil || decoded.Diagnostics.Took != 2*time.Millisecond || len(decoded.Hits) != 1 {
		t.Fatal(decoded, err)
	}

	client := internalClient(t, routeBody(response, 200), resolver, nil)
	client.search.Limits.MaxPages = 3
	client.search.Limits.MaxPageItems = 2
	client.search.Limits.MaxResultBytes = int64(len(response)) + 100
	request := search.Request{Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}}, Page: search.CursorPage{Size: 1, KeepAlive: time.Minute}}
	fingerprint, err := search.RequestFingerprint(request, client.search.Limits)
	if err != nil {
		t.Fatal(err)
	}
	binding := search.CursorBinding{Tenant: "tenant", Index: "events", QueryFingerprint: fingerprint, IndexFingerprint: "fingerprint"}
	initialBytes := client.search.Limits.MaxResultBytes - int64(len(response))
	cursor, err := client.search.CursorCodec.Encode(binding, search.CursorState{PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"previous"`)}, Page: 2, Items: 5, Bytes: initialBytes, ExpiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	request.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute, Cursor: cursor}
	result, err := client.Search(t.Context(), request)
	if err != nil || result.NextCursor() == "" {
		t.Fatal(result, err)
	}
	state, err := client.search.CursorCodec.Decode(result.NextCursor(), binding, client.search.Limits)
	if err != nil || state.PointInTime != "rotated" || state.Page != 3 || state.Items != 6 || state.Bytes != client.search.Limits.MaxResultBytes || len(state.SortValues) != 1 {
		t.Fatal(state, err)
	}

	offset := search.Request{Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{}, Page: search.OffsetPage{Size: 1}}
	offsetResponse := `{"took":0,"_shards":{"total":0,"successful":0,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`
	bounded := internalClient(t, routeBody(offsetResponse, 200), resolver, nil)
	bounded.search.Limits.MaxResultBytes = int64(len(offsetResponse))
	if _, err := bounded.Search(t.Context(), offset); err != nil {
		t.Fatal(err)
	}
	bounded.search.Limits.MaxResultBytes--
	if _, err := bounded.Search(t.Context(), offset); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatal(err)
	}
}

func TestCapabilitiesReflectLifecycleConfiguration(t *testing.T) {
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", Fingerprint: "fingerprint"}}
	without := internalClient(t, routeBody(`{}`, 200), resolver, nil)
	capabilities, err := without.Capabilities(t.Context())
	if err != nil || capabilities.Lifecycle || capabilities.Templates {
		t.Fatal(capabilities, err)
	}
	with := internalClient(t, routeBody(`{}`, 200), resolver, LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }))
	capabilities, err = with.Capabilities(t.Context())
	if err != nil || !capabilities.Lifecycle || !capabilities.Templates {
		t.Fatal(capabilities, err)
	}
}

func TestContentTypeOwnershipAndWriteStatusBoundaries(t *testing.T) {
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", Fingerprint: "fingerprint"}}
	type observedRequest struct{ method, contentType string }
	observed := make([]observedRequest, 0, 4)
	client := internalClient(t, func(request *http.Request) (*http.Response, error) {
		observed = append(observed, observedRequest{request.Method, request.Header.Get("Content-Type")})
		return internalResponse(200, `{}`), nil
	}, resolver, nil)
	if _, err := client.executeContent(t.Context(), OperationInfo, http.MethodGet, "/", nil, "application/custom", 200); err != nil {
		t.Fatal(err)
	}
	if _, err := client.executeContent(t.Context(), OperationInfo, http.MethodPost, "/", []byte(`{}`), "application/custom", 200); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.executeWrite(t.Context(), http.MethodDelete, "/", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.executeWrite(t.Context(), http.MethodPut, "/", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	expected := []observedRequest{{http.MethodGet, ""}, {http.MethodPost, "application/custom"}, {http.MethodDelete, ""}, {http.MethodPut, "application/json"}}
	if !reflect.DeepEqual(observed, expected) {
		t.Fatal(observed)
	}

	for _, test := range []struct {
		status int
		action search.WriteAction
		state  search.OutcomeState
	}{
		{199, search.ActionIndex, search.OutcomeUnknown}, {200, search.ActionIndex, search.OutcomeApplied},
		{299, search.ActionIndex, search.OutcomeApplied}, {300, search.ActionIndex, search.OutcomeUnknown},
		{404, search.ActionDelete, search.OutcomeNotFound}, {404, search.ActionIndex, search.OutcomeUnknown},
	} {
		state, _ := classifyWriteStatus(test.action, test.status, nil)
		if state != test.state {
			t.Fatalf("status=%d action=%s state=%s", test.status, test.action, state)
		}
	}
	index := search.IndexDocument(mutationDocument(t, "id", 1))
	for _, status := range []int{200, 299, 300} {
		writeClient := internalClient(t, routeBody(`{"_id":"id","_version":1}`, status), resolver, nil)
		_, err := writeClient.Write(t.Context(), index, search.RefreshNone)
		if status < 300 && err != nil {
			t.Fatal(status, err)
		}
		if status == 300 && err == nil {
			t.Fatal("non-success status accepted")
		}
	}
	deleteOperation := search.DeleteDocument("tenant", "events", "id", 1)
	for _, test := range []struct {
		operation search.WriteOperation
		valid     bool
	}{{deleteOperation, true}, {index, false}} {
		writeClient := internalClient(t, routeBody(`{"_id":"id","result":"not_found"}`, 404), resolver, nil)
		outcome, err := writeClient.Write(t.Context(), test.operation, search.RefreshNone)
		if test.valid && (err != nil || outcome.State != search.OutcomeNotFound) {
			t.Fatal(outcome, err)
		}
		if !test.valid && err == nil {
			t.Fatal("index 404 accepted")
		}
	}
}
