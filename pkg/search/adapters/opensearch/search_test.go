package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestSearchUsesPITSearchAfterAndSignedQueryBoundCursor(t *testing.T) {
	t.Parallel()

	var (
		mu                         sync.Mutex
		created, searched, deleted int
		bodies                     []map[string]any
	)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/tenant-a-locations-v3/_search/point_in_time":
			created++
			_, _ = io.WriteString(writer, `{"pit_id":"pit-opaque","_shards":{"total":1,"successful":1,"failed":0},"creation_time":1}`)
		case request.Method == http.MethodPost && request.URL.Path == "/_search":
			searched++
			var body map[string]any
			_ = json.NewDecoder(request.Body).Decode(&body)
			bodies = append(bodies, body)
			if searched <= 2 {
				id := "location-1"
				if searched == 2 {
					id = "location-2"
				}
				_, _ = io.WriteString(writer, `{"took":3,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":2,"relation":"eq"},"hits":[{"_index":"tenant-a-locations-v3","_id":"`+id+`","_version":7,"_score":4.25,"_source":{"name":"Helsinki","country":"FI"},"sort":[1200000,"`+id+`"],"highlight":{"name":["<em>Hel</em>sinki"]}}]},"aggregations":{"countries":{"buckets":[{"key":"FI","doc_count":2}]}}}`)
			} else {
				_, _ = io.WriteString(writer, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[]},"aggregations":{"countries":{"buckets":[]}}}`)
			}
		case request.Method == http.MethodDelete && request.URL.Path == "/_search/point_in_time":
			deleted++
			_, _ = io.WriteString(writer, `{"pits":[{"pit_id":"pit-opaque","successful":true}]}`)
		default:
			http.Error(writer, "unexpected", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	now := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	codec, _ := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 4096)
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{server.URL}, Transport: server.Client().Transport,
		TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout:     time.Second, MaximumResponseBytes: 16 << 10,
		Search: &adapter.SearchConfig{
			Limits: search.DefaultLimits(), CursorCodec: codec, Clock: func() time.Time { return now },
			Authorizer: allowSearchAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(_ context.Context, tenant, logical string, access adapter.IndexAccess) (adapter.IndexTarget, error) {
				if tenant != "tenant-a" || logical != "locations" || access != adapter.IndexRead {
					t.Fatalf("resolve = %q/%q/%q", tenant, logical, access)
				}
				return adapter.IndexTarget{Name: "tenant-a-locations-v3", PhysicalName: "tenant-a-locations-v3", Fingerprint: "mapping-v3-fingerprint"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	request := search.Request{
		Tenant: "tenant-a", Index: "locations",
		Query:        search.BoolQuery{Must: []search.Query{search.FullTextQuery{Fields: []string{"name^3", "address"}, Text: "Helsinki"}}, Filter: []search.Query{search.TermQuery{Field: "country", Value: search.StringValue("FI")}}},
		Sort:         []search.Sort{{Field: "population", Direction: search.Descending}, {Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page:         search.CursorPage{Size: 1, KeepAlive: time.Minute},
		Highlights:   map[string]search.Highlight{"name": {FragmentSize: 120, MaxFragments: 2}},
		Aggregations: map[string]search.Aggregation{"countries": search.TermsAggregation{Field: "country", Size: 10}},
	}
	first, err := client.Search(t.Context(), request)
	if err != nil {
		t.Fatalf("first Search() error = %v", err)
	}
	if len(first.Hits()) != 1 || first.NextCursor() == "" || first.Diagnostics().Backend != "opensearch" {
		t.Fatalf("first Search() = %#v", first)
	}
	fingerprint, err := search.RequestFingerprint(request, search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	binding := search.CursorBinding{Tenant: "tenant-a", Index: "locations", QueryFingerprint: fingerprint, IndexFingerprint: "mapping-v3-fingerprint"}
	firstState, err := codec.Decode(first.NextCursor(), binding, search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	request.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute, Cursor: first.NextCursor()}
	second, err := client.Search(t.Context(), request)
	if err != nil {
		t.Fatalf("second Search() error = %v", err)
	}
	if len(second.Hits()) != 1 || second.NextCursor() == "" {
		t.Fatalf("second Search() = %#v", second)
	}
	secondState, err := codec.Decode(second.NextCursor(), binding, search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !secondState.ExpiresAt.Equal(firstState.ExpiresAt) {
		t.Fatalf("cursor deadline slid from %s to %s", firstState.ExpiresAt, secondState.ExpiresAt)
	}
	request.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute, Cursor: second.NextCursor()}
	third, err := client.Search(t.Context(), request)
	if err != nil {
		t.Fatalf("third Search() error = %v", err)
	}
	if len(third.Hits()) != 0 || third.NextCursor() != "" {
		t.Fatalf("third Search() = %#v", third)
	}

	mu.Lock()
	defer mu.Unlock()
	if created != 1 || searched != 3 || deleted != 1 {
		t.Fatalf("PIT calls = %d/%d/%d", created, searched, deleted)
	}
	if _, exists := bodies[0]["search_after"]; exists {
		t.Fatal("first request unexpectedly had search_after")
	}
	if _, exists := bodies[1]["search_after"]; !exists {
		t.Fatal("continued request omitted search_after")
	}
	if _, exists := bodies[0]["query"]; !exists {
		t.Fatal("encoded request omitted query")
	}
}

func TestSearchBoundsAbandonedPointInTimesAndReclaimsCompletedLease(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		created int
		deleted int
	)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/_search/point_in_time"):
			mu.Lock()
			created++
			pitID := created
			mu.Unlock()
			_, _ = fmt.Fprintf(writer, `{"pit_id":"pit-%d"}`, pitID)
		case request.Method == http.MethodPost && request.URL.Path == "/_search":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			if _, continuation := body["search_after"]; continuation {
				_, _ = io.WriteString(writer, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[]}}`)
				return
			}
			_, _ = io.WriteString(writer, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"event-1","_version":1,"_source":{},"sort":["event-1"]}]}}`)
		case request.Method == http.MethodDelete && request.URL.Path == "/_search/point_in_time":
			var body struct {
				PITID string `json:"pit_id"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			deleted++
			mu.Unlock()
			_, _ = fmt.Fprintf(writer, `{"pits":[{"pit_id":%q,"successful":true}]}`, body.PITID)
		default:
			http.Error(writer, "unexpected", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{server.URL}, Transport: server.Client().Transport,
		TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout:     time.Second, MaximumResponseBytes: 16 << 10,
		Search: &adapter.SearchConfig{
			Limits: search.DefaultLimits(), CursorCodec: codec, Clock: time.Now,
			MaximumOpenPointInTimes: 2, Authorizer: allowSearchAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
				return adapter.IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "mapping-v1"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request := search.Request{
		Tenant: "tenant-a", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
	}
	first, err := client.Search(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(t.Context(), request); !errors.Is(err, adapter.ErrPointInTimeCapacity) || !errors.Is(err, adapter.ErrBackpressure) {
		t.Fatalf("third Search() error = %v, want PIT capacity backpressure", err)
	}
	if snapshot := client.PointInTimeSnapshot(); snapshot.Open != 2 || snapshot.Maximum != 2 {
		t.Fatalf("PointInTimeSnapshot() = %#v", snapshot)
	}

	request.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute, Cursor: first.NextCursor()}
	if _, err := client.Search(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	request.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute}
	if _, err := client.Search(t.Context(), request); err != nil {
		t.Fatalf("Search() after completed cursor error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if created != 3 || deleted != 1 {
		t.Fatalf("PIT calls = created %d, deleted %d", created, deleted)
	}
}

func TestSearchRequestsExternalVersionsFromOpenSearch(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		version := ""
		if body["version"] == true {
			version = `,"_version":7`
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"tenant-a-locations-v3","_id":"location-1"`+version+`,"_source":{"name":"Helsinki"},"sort":["location-1"]}]}}`)
	}))
	t.Cleanup(server.Close)

	client := newSearchClient(t, server, time.Now)
	result, err := client.Search(t.Context(), search.Request{
		Tenant: "tenant-a", Index: "locations", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.OffsetPage{Size: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hits := result.Hits(); len(hits) != 1 || hits[0].Version != 7 {
		t.Fatalf("Search() hits = %#v, want external version 7", hits)
	}
}

func TestSearchRejectsMoreHitsThanRequested(t *testing.T) {
	t.Parallel()
	response := `{"took":1,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":2,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"a","_version":1,"_source":{}},{"_index":"events-v1","_id":"b","_version":1,"_source":{}}]}}`
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, response)
	}))
	t.Cleanup(server.Close)
	client := newSearchClient(t, server, time.Now)
	request := search.Request{Tenant: "tenant-a", Index: "events", Query: search.MatchAllQuery{}, Page: search.OffsetPage{Size: 1}}
	if _, err := client.Search(t.Context(), request); !errors.Is(err, adapter.ErrMalformedResponse) {
		t.Fatalf("Search() error = %v, want ErrMalformedResponse", err)
	}
}

func TestSearchRejectsInvalidAnalyzerBeforeNetworkExecution(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = io.WriteString(writer, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`)
	}))
	t.Cleanup(server.Close)
	client := newSearchClient(t, server, time.Now)
	_, err := client.Search(t.Context(), search.Request{
		Tenant: "tenant-a", Index: "locations",
		Query: search.FullTextQuery{Fields: []string{"name"}, Text: "helsinki", Analyzer: "invalid analyzer"},
		Page:  search.OffsetPage{Size: 10},
	})
	if !errors.Is(err, search.ErrInvalidQuery) || requests != 0 {
		t.Fatalf("Search() error/requests = %v/%d, want ErrInvalidQuery/0", err, requests)
	}
}

func TestCursorSearchRejectsAdapterUnsupportedQueriesBeforePITDispatch(t *testing.T) {
	t.Parallel()

	tests := map[string]search.Query{
		"unknown locale":        search.FullTextQuery{Fields: []string{"name"}, Text: "helsinki", Locale: "sv"},
		"foreign raw extension": search.RawExtensionQuery{Adapter: "another-adapter", Payload: json.RawMessage(`{"match_all":{}}`)},
	}
	for name, query := range tests {
		name, query := name, query
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			requests := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				requests++
				_, _ = io.WriteString(writer, `{"pit_id":"unexpected-pit"}`)
			}))
			t.Cleanup(server.Close)
			client := newSearchClient(t, server, time.Now)
			_, err := client.Search(t.Context(), search.Request{
				Tenant: "tenant-a", Index: "locations", Query: query,
				Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
				Page: search.CursorPage{Size: 10, KeepAlive: time.Minute},
			})
			if !errors.Is(err, search.ErrUnsupported) || requests != 0 {
				t.Fatalf("Search() error/requests = %v/%d, want ErrUnsupported/0", err, requests)
			}
		})
	}
}

func TestSearchTranslatesTypedCapabilitiesWithoutCrossIndexLeakage(t *testing.T) {
	t.Parallel()

	var encoded map[string]any
	events := make(chan adapter.TelemetryEvent, 4)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/tenant-a-locations-v3/_search" {
			t.Fatalf("search path = %q", request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&encoded); err != nil {
			t.Fatal(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"took":2,"timed_out":false,"_shards":{"total":2,"successful":1,"skipped":0,"failed":1,"failures":[{"index":"private-index","reason":{"type":"query_shard_exception","reason":"sensitive query"}}]},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]},"aggregations":{"countries":{"buckets":[]},"ranges":{"buckets":[]}},"suggest":{"names":[{"text":"hel","offset":0,"length":3,"options":[]}]}}`)
	}))
	t.Cleanup(server.Close)
	client := newSearchClientWithTelemetry(t, server, time.Now, &adapter.TelemetryConfig{
		Observer: adapter.TelemetryObserverFunc(func(_ context.Context, event adapter.TelemetryEvent) error {
			events <- event
			return nil
		}),
		Clock: time.Now,
	})
	number, _ := search.NumberValue("10.5")
	request := search.Request{
		Tenant: "tenant-a", Index: "locations",
		Query: search.BoolQuery{
			Must:   []search.Query{search.MatchAllQuery{}, search.FullTextQuery{Fields: []string{"name"}, Text: "hel", Locale: "fi"}},
			Should: []search.Query{search.PrefixQuery{Field: "name.keyword", Prefix: "hel"}}, MinimumShouldMatch: 1,
			Filter:  []search.Query{search.RangeQuery{Field: "population", GTE: &number}, search.ExistsQuery{Field: "country"}, search.GeoDistanceQuery{Field: "position", Origin: search.GeoPoint{Latitude: 60.17, Longitude: 24.94}, DistanceKM: number}},
			MustNot: []search.Query{search.TermQuery{Field: "closed", Value: search.BoolValue(true)}},
		},
		Sort:         []search.Sort{{Field: "population", Direction: search.Descending, Missing: search.MissingLast}},
		Page:         search.OffsetPage{Size: 25, Offset: 50},
		Projection:   search.Projection{Includes: []string{"name", "country"}, Excludes: []string{"private"}},
		Highlights:   map[string]search.Highlight{"name": {FragmentSize: 80, MaxFragments: 3, PreTag: "<mark>", PostTag: "</mark>"}},
		Aggregations: map[string]search.Aggregation{"countries": search.TermsAggregation{Field: "country", Size: 5}, "ranges": search.RangeAggregation{Field: "population", Buckets: []search.RangeBucket{{Key: "large", From: &number}}}},
		Suggestions:  map[string]search.Suggestion{"names": search.PrefixSuggestion{Field: "name.suggest", Text: "hel", Size: 4}},
	}
	result, err := client.Search(t.Context(), request)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if !result.Diagnostics().Partial || len(result.Diagnostics().Failures) != 1 || result.Diagnostics().Failures[0].Code != "query_shard_exception" || len(result.Suggestions()) != 1 {
		t.Fatalf("Search() diagnostics = %#v", result.Diagnostics())
	}
	close(events)
	if !containsTelemetrySignal(events, adapter.TelemetryPartialSearch) {
		t.Fatal("partial search telemetry signal was not emitted")
	}
	for _, key := range []string{"query", "sort", "from", "_source", "highlight", "aggs", "suggest"} {
		if _, ok := encoded[key]; !ok {
			t.Fatalf("encoded search omitted %q: %#v", key, encoded)
		}
	}
	encodedJSON, _ := json.Marshal(encoded)
	if !strings.Contains(string(encodedJSON), `"analyzer":"finnish"`) || !strings.Contains(string(encodedJSON), `"distance":"10.5km"`) {
		t.Fatalf("localized/geo translation = %s", encodedJSON)
	}
}

func TestSearchSurfacesFinalPITCleanupFailure(t *testing.T) {
	t.Parallel()
	events := make(chan adapter.TelemetryEvent, 8)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/tenant-a-locations-v3/_search/point_in_time":
			_, _ = io.WriteString(writer, `{"pit_id":"pit-cleanup"}`)
		case "/_search":
			_, _ = io.WriteString(writer, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`)
		case "/_search/point_in_time":
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(writer, `{"error":{"type":"rejected_execution_exception"}}`)
		}
	}))
	t.Cleanup(server.Close)
	client := newSearchClientWithTelemetry(t, server, time.Now, &adapter.TelemetryConfig{
		Observer: adapter.TelemetryObserverFunc(func(_ context.Context, event adapter.TelemetryEvent) error {
			events <- event
			return nil
		}),
		Clock: time.Now,
	})
	_, err := client.Search(t.Context(), search.Request{Tenant: "tenant-a", Index: "locations", Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.CursorPage{Size: 10, KeepAlive: time.Minute}})
	var failure *adapter.Failure
	if !errors.As(err, &failure) || failure.Operation != adapter.OperationDeletePIT {
		t.Fatalf("Search() cleanup error = %v", err)
	}
	close(events)
	if !containsTelemetrySignal(events, adapter.TelemetryPITCleanupFailure) {
		t.Fatal("PIT cleanup failure telemetry signal was not emitted")
	}
}

func TestSearchPreservesPrimaryAndPITCleanupFailures(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/tenant-a-locations-v3/_search/point_in_time":
			_, _ = io.WriteString(writer, `{"pit_id":"pit-cleanup"}`)
		case "/_search":
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":{"type":"illegal_argument_exception"}}`)
		case "/_search/point_in_time":
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(writer, `{"error":{"type":"rejected_execution_exception"}}`)
		}
	}))
	t.Cleanup(server.Close)
	client := newSearchClient(t, server, time.Now)
	_, err := client.Search(t.Context(), search.Request{Tenant: "tenant-a", Index: "locations", Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.CursorPage{Size: 10, KeepAlive: time.Minute}})
	if !errors.Is(err, adapter.ErrRejected) || !errors.Is(err, adapter.ErrOverloaded) {
		t.Fatalf("Search() error = %v", err)
	}
}

func newSearchClient(t *testing.T, server *httptest.Server, clock func() time.Time) *adapter.Client {
	return newSearchClientWithTelemetry(t, server, clock, nil)
}

func newSearchClientWithTelemetry(t *testing.T, server *httptest.Server, clock func() time.Time, telemetry *adapter.TelemetryConfig) *adapter.Client {
	t.Helper()
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), clock, 4096)
	if err != nil {
		t.Fatal(err)
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{server.URL}, Transport: server.Client().Transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: time.Second, MaximumResponseBytes: 32 << 10,
		Telemetry: telemetry,
		Search: &adapter.SearchConfig{Limits: search.DefaultLimits(), CursorCodec: codec, Clock: clock, LocaleAnalyzers: map[string]string{"fi": "finnish"}, Authorizer: allowSearchAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
				return adapter.IndexTarget{Name: "tenant-a-locations-v3", PhysicalName: "tenant-a-locations-v3", Fingerprint: "mapping-v3"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func containsTelemetrySignal(events <-chan adapter.TelemetryEvent, signal adapter.TelemetrySignal) bool {
	for event := range events {
		if event.Signal == signal {
			return true
		}
	}
	return false
}
