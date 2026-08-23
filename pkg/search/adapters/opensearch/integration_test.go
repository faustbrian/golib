//go:build integration

package opensearch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
	"github.com/faustbrian/golib/pkg/search/searchtest"
	official "github.com/opensearch-project/opensearch-go/v4"
)

const realOpenSearchHealthConvergenceTimeout = 3 * time.Minute

func TestRealOpenSearchConformanceSharedSemantics(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_URL")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	if endpoint == "" || expectedVersion == "" {
		t.Skip("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are not configured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatal("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are required for a disposable test cluster")
	}

	limits := search.DefaultLimits()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantA, tenantB, logicalIndex := "conformance-a", "conformance-b", "documents"
	physicalA, physicalB := "golib-search-conformance-a-"+suffix, "golib-search-conformance-b-"+suffix
	aliasA, aliasB := physicalA+"-alias", physicalB+"-alias"
	mapping := json.RawMessage(`{"dynamic":"strict","properties":{"scenario":{"type":"keyword"},"keyword":{"type":"keyword"},"exists_value":{"type":"keyword"}}}`)
	definitionA, err := search.NewIndexDefinition(physicalA,
		json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0}`), mapping, limits)
	if err != nil {
		t.Fatal(err)
	}
	definitionB, err := search.NewIndexDefinition(physicalB, definitionA.Settings(), definitionA.Mappings(), limits)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]adapter.IndexTarget{
		tenantA: {Name: aliasA, PhysicalName: physicalA, Fingerprint: definitionA.Fingerprint()},
		tenantB: {Name: aliasB, PhysicalName: physicalB, Fingerprint: definitionB.Fingerprint()},
	}
	ownedResources := map[string]map[string]struct{}{
		tenantA: {physicalA: {}, aliasA: {}},
		tenantB: {physicalB: {}, aliasB: {}},
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Search: &adapter.SearchConfig{
			Limits: limits, CursorCodec: mustIntegrationCursorCodec(t), Clock: time.Now, Authorizer: allowSearchAuthorization(), WriteGuard: allowWriteAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(_ context.Context, tenant, index string, _ adapter.IndexAccess) (adapter.IndexTarget, error) {
				target, exists := targets[tenant]
				if !exists || index != logicalIndex {
					return adapter.IndexTarget{}, errors.New("conformance target denied")
				}
				return target, nil
			}),
		},
		Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(func(_ context.Context, tenant string, resources []string) error {
			owned, exists := ownedResources[tenant]
			if !exists {
				return errors.New("conformance lifecycle denied")
			}
			for _, resource := range resources {
				if _, allowed := owned[resource]; !allowed {
					return errors.New("conformance lifecycle denied")
				}
			}
			return nil
		}), MutationGuard: allowLifecycleMutationGuard()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	info, err := client.Info(t.Context())
	if err != nil || info.Version != expectedVersion {
		t.Fatalf("Info() = %#v/%v, expected %s", info, err, expectedVersion)
	}

	for _, fixture := range []struct {
		tenant     string
		definition search.IndexDefinition
		alias      string
		physical   string
	}{
		{tenant: tenantA, definition: definitionA, alias: aliasA, physical: physicalA},
		{tenant: tenantB, definition: definitionB, alias: aliasB, physical: physicalB},
	} {
		if err := client.CreateIndex(t.Context(), fixture.tenant, fixture.definition); err != nil {
			t.Fatal(err)
		}
		physical := fixture.physical
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = deleteDisposableIndex(ctx, endpoint, physical)
		})
		if err := client.AddAlias(t.Context(), fixture.tenant, fixture.alias, fixture.physical, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.Search(t.Context(), search.Request{
		Tenant: "conformance-denied", Index: logicalIndex, Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.OffsetPage{Size: 1},
	}); !errors.Is(err, adapter.ErrUnsafeIndexTarget) {
		t.Fatalf("cross-tenant Search() error = %v, want ErrUnsafeIndexTarget", err)
	}
	if err := client.DeleteIndex(t.Context(), tenantB, physicalA); !errors.Is(err, adapter.ErrLifecycleCleanupGuardRequired) {
		t.Fatalf("direct DeleteIndex() error = %v, want ErrLifecycleCleanupGuardRequired", err)
	}
	if resolved, err := client.ResolveAlias(t.Context(), tenantA, aliasA); err != nil || resolved != physicalA {
		t.Fatalf("tenant A alias after denied administration = %q/%v", resolved, err)
	}
	if err := searchtest.RunConformance(t.Context(), searchtest.ConformanceConfig{
		Adapter: client, Limits: limits,
		TenantA: tenantA, TenantB: tenantB, LogicalIndex: logicalIndex,
		Refresh: search.RefreshWaitFor,
	}); err != nil {
		t.Fatal(err)
	}
	assertRealPITSnapshotUnderConcurrentWrites(t, client, limits, tenantA, logicalIndex)
}

func assertRealPITSnapshotUnderConcurrentWrites(t *testing.T, client *adapter.Client, limits search.Limits, tenant, index string) {
	t.Helper()
	write := func(id string, version uint64) {
		t.Helper()
		document, err := search.NewDocument(tenant, index, id, version,
			json.RawMessage(`{"scenario":"pit-page"}`), limits)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := client.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor)
		if err != nil || outcome.State != search.OutcomeApplied {
			t.Fatalf("Write(%s) = %#v/%v", id, outcome, err)
		}
	}
	for _, id := range []string{"pit-page-a", "pit-page-b", "pit-page-c"} {
		write(id, 1)
	}

	request := search.Request{
		Tenant: tenant, Index: index,
		Query: search.TermQuery{Field: "scenario", Value: search.StringValue("pit-page")},
		Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page:  search.CursorPage{Size: 1, KeepAlive: time.Minute},
	}
	first, err := client.Search(t.Context(), request)
	if err != nil || len(first.Hits()) != 1 || first.Hits()[0].ID != "pit-page-a" || first.NextCursor() == "" {
		t.Fatalf("first PIT page = %#v/%q/%v", first.Hits(), first.NextCursor(), err)
	}
	ids := []string{first.Hits()[0].ID}

	write("pit-page-0", 1)
	deleted, err := client.Write(t.Context(), search.DeleteDocument(tenant, index, "pit-page-c", 2), search.RefreshWaitFor)
	if err != nil || deleted.State != search.OutcomeApplied {
		t.Fatalf("concurrent Delete(pit-page-c) = %#v/%v", deleted, err)
	}

	cursor := first.NextCursor()
	for page := 0; cursor != "" && page < 4; page++ {
		request.Page = search.CursorPage{Size: 1, Cursor: cursor, KeepAlive: time.Minute}
		result, searchErr := client.Search(t.Context(), request)
		if searchErr != nil {
			t.Fatal(searchErr)
		}
		for _, hit := range result.Hits() {
			ids = append(ids, hit.ID)
		}
		cursor = result.NextCursor()
	}
	if cursor != "" {
		t.Fatal("PIT traversal exceeded its bounded page budget")
	}
	want := []string{"pit-page-a", "pit-page-b", "pit-page-c"}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Fatalf("PIT snapshot IDs = %v, want each original ID once and no inserted ID: %v", ids, want)
	}
}

func TestRealOpenSearchConformanceGeneratedDSLDifferential(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_URL")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	if endpoint == "" || expectedVersion == "" {
		t.Skip("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are not configured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatal("OPENSEARCH_URL is invalid")
	}
	limits := search.DefaultLimits()
	tenant, logicalIndex := "dsl-tenant", "documents"
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	physical, alias := "golib-search-dsl-"+suffix, "golib-search-dsl-alias-"+suffix
	definition, err := search.NewIndexDefinition(physical,
		json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0}`),
		json.RawMessage(`{"dynamic":"strict","properties":{"name":{"type":"text","fields":{"keyword":{"type":"keyword"}}},"category":{"type":"keyword"},"age":{"type":"integer"},"location":{"type":"geo_point"},"secret":{"type":"keyword"},"suggest":{"type":"completion"}}}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Search: &adapter.SearchConfig{
			Limits: limits, CursorCodec: mustIntegrationCursorCodec(t), Clock: time.Now, Authorizer: allowSearchAuthorization(), WriteGuard: allowWriteAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(_ context.Context, gotTenant, gotIndex string, _ adapter.IndexAccess) (adapter.IndexTarget, error) {
				if gotTenant != tenant || gotIndex != logicalIndex {
					return adapter.IndexTarget{}, errors.New("DSL target denied")
				}
				return adapter.IndexTarget{Name: alias, PhysicalName: physical, Fingerprint: definition.Fingerprint()}, nil
			}),
		},
		Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(func(_ context.Context, gotTenant string, resources []string) error {
			if gotTenant != tenant {
				return errors.New("DSL lifecycle denied")
			}
			for _, resource := range resources {
				if resource != physical && resource != alias {
					return errors.New("DSL lifecycle denied")
				}
			}
			return nil
		}), MutationGuard: allowLifecycleMutationGuard()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	discover := false
	direct, err := official.NewClient(official.Config{Addresses: []string{endpoint}, DisableRetry: true, DiscoverNodesOnStart: &discover, HealthCheckMaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = direct.Close() })
	if err := client.CreateIndex(t.Context(), tenant, definition); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = deleteDisposableIndex(ctx, endpoint, physical)
	})
	if err := client.AddAlias(t.Context(), tenant, alias, physical, true); err != nil {
		t.Fatal(err)
	}
	fixtures := []struct {
		id     string
		source json.RawMessage
	}{
		{id: "a", source: json.RawMessage(`{"name":"Alpha Helsinki","category":"nordic","age":30,"location":{"lat":60.1699,"lon":24.9384},"secret":"red","suggest":{"input":["alpha"]}}`)},
		{id: "b", source: json.RawMessage(`{"name":"Beta Turku","category":"nordic","age":40,"location":{"lat":60.4518,"lon":22.2666},"secret":"blue","suggest":{"input":["beta"]}}`)},
		{id: "c", source: json.RawMessage(`{"name":"Gamma Berlin","category":"central","age":50,"location":{"lat":52.52,"lon":13.405},"secret":"green","suggest":{"input":["gamma"]}}`)},
	}
	operations := make([]search.WriteOperation, 0, len(fixtures))
	for _, fixture := range fixtures {
		document, documentErr := search.NewDocument(tenant, logicalIndex, fixture.id, 1, fixture.source, limits)
		if documentErr != nil {
			t.Fatal(documentErr)
		}
		operations = append(operations, search.IndexDocument(document))
	}
	seeded, err := client.Bulk(t.Context(), search.BulkRequest{Operations: operations, Refresh: search.RefreshWaitFor})
	if err != nil || seeded.Partial() {
		t.Fatalf("DSL fixtures = %#v/%v", seeded.Items(), err)
	}
	lower, _ := search.NumberValue("25")
	upper, _ := search.NumberValue("45")
	distance, _ := search.NumberValue("300")
	rangeFrom, _ := search.NumberValue("0")
	rangeTo, _ := search.NumberValue("40")
	tests := []struct {
		name    string
		request search.Request
		body    string
	}{
		{
			name: "boolean",
			request: search.Request{Tenant: tenant, Index: logicalIndex,
				Query: search.BoolQuery{
					Filter:             []search.Query{search.TermQuery{Field: "category", Value: search.StringValue("nordic")}},
					Should:             []search.Query{search.PrefixQuery{Field: "name.keyword", Prefix: "Alpha"}},
					MinimumShouldMatch: 1,
				},
				Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 10}},
			body: `{"query":{"bool":{"filter":[{"term":{"category":"nordic"}}],"should":[{"prefix":{"name.keyword":{"value":"Alpha"}}}],"minimum_should_match":1}},"size":10,"from":0,"version":true,"sort":[{"_id":{"order":"asc"}}]}`,
		},
		{
			name: "full-text projection and highlight",
			request: search.Request{Tenant: tenant, Index: logicalIndex,
				Query: search.FullTextQuery{Fields: []string{"name"}, Text: "alpha"},
				Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 10},
				Projection: search.Projection{Includes: []string{"name", "category"}, Excludes: []string{"secret"}},
				Highlights: map[string]search.Highlight{"name": {FragmentSize: 64, MaxFragments: 1, PreTag: "<mark>", PostTag: "</mark>"}},
			},
			body: `{"query":{"multi_match":{"query":"alpha","fields":["name"]}},"size":10,"from":0,"version":true,"sort":[{"_id":{"order":"asc"}}],"_source":{"includes":["name","category"],"excludes":["secret"]},"highlight":{"fields":{"name":{"fragment_size":64,"number_of_fragments":1,"pre_tags":["<mark>"],"post_tags":["</mark>"]}}}}`,
		},
		{
			name: "range",
			request: search.Request{Tenant: tenant, Index: logicalIndex,
				Query: search.RangeQuery{Field: "age", GTE: &lower, LT: &upper},
				Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 10}},
			body: `{"query":{"range":{"age":{"gte":25,"lt":45}}},"size":10,"from":0,"version":true,"sort":[{"_id":{"order":"asc"}}]}`,
		},
		{
			name: "geo",
			request: search.Request{Tenant: tenant, Index: logicalIndex,
				Query: search.GeoDistanceQuery{Field: "location", Origin: search.GeoPoint{Latitude: 60.1699, Longitude: 24.9384}, DistanceKM: distance},
				Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 10}},
			body: `{"query":{"geo_distance":{"distance":"300km","location":{"lat":60.1699,"lon":24.9384}}},"size":10,"from":0,"version":true,"sort":[{"_id":{"order":"asc"}}]}`,
		},
		{
			name: "aggregations and suggestion",
			request: search.Request{Tenant: tenant, Index: logicalIndex, Query: search.MatchAllQuery{},
				Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 10},
				Aggregations: map[string]search.Aggregation{
					"categories": search.TermsAggregation{Field: "category", Size: 10},
					"ages": search.RangeAggregation{Field: "age", Buckets: []search.RangeBucket{
						{Key: "young", From: &rangeFrom, To: &rangeTo}, {Key: "older", From: &rangeTo},
					}},
				},
				Suggestions: map[string]search.Suggestion{"names": search.PrefixSuggestion{Field: "suggest", Text: "al", Size: 5}},
			},
			body: `{"query":{"match_all":{}},"size":10,"from":0,"version":true,"sort":[{"_id":{"order":"asc"}}],"aggs":{"categories":{"terms":{"field":"category","size":10}},"ages":{"range":{"field":"age","ranges":[{"key":"young","from":0,"to":40},{"key":"older","from":40}]}}},"suggest":{"names":{"prefix":"al","completion":{"field":"suggest","size":5,"skip_duplicates":true}}}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapterResult, searchErr := client.Search(t.Context(), test.request)
			if searchErr != nil {
				t.Fatal(searchErr)
			}
			directResult := directSearchDifferential(t, direct, "/"+alias+"/_search", []byte(test.body))
			if got := adapterDifferential(adapterResult); !reflect.DeepEqual(got, directResult) {
				t.Fatalf("adapter/direct differential mismatch\nadapter: %#v\ndirect:  %#v", got, directResult)
			}
		})
	}
	assertRealPITSearchAfterDifferential(t, client, direct, tenant, logicalIndex, alias, limits)
}

type searchDifferential struct {
	IDs          []string
	Sources      []any
	Highlights   []map[string][]string
	Aggregations map[string]any
	Suggestions  map[string]any
}

func adapterDifferential(result search.Result) searchDifferential {
	hits := result.Hits()
	differential := searchDifferential{
		IDs: make([]string, len(hits)), Sources: make([]any, len(hits)), Highlights: make([]map[string][]string, len(hits)),
		Aggregations: decodeDifferentialRawMap(result.Aggregations()), Suggestions: decodeDifferentialRawMap(result.Suggestions()),
	}
	for index, hit := range hits {
		differential.IDs[index] = hit.ID
		_ = json.Unmarshal(hit.Source, &differential.Sources[index])
		differential.Highlights[index] = hit.Highlights
	}
	return differential
}

func directSearchDifferential(t *testing.T, client *official.Client, path string, body []byte) searchDifferential {
	t.Helper()
	responseBody := requireDirectOpenSearchJSON(t, client, http.MethodPost, path, body, http.StatusOK)
	var payload struct {
		Hits struct {
			Hits []struct {
				ID        string              `json:"_id"`
				Source    any                 `json:"_source"`
				Highlight map[string][]string `json:"highlight"`
			} `json:"hits"`
		} `json:"hits"`
		Aggregations map[string]any `json:"aggregations"`
		Suggestions  map[string]any `json:"suggest"`
	}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		t.Fatal(err)
	}
	differential := searchDifferential{
		IDs: make([]string, len(payload.Hits.Hits)), Sources: make([]any, len(payload.Hits.Hits)), Highlights: make([]map[string][]string, len(payload.Hits.Hits)),
		Aggregations: payload.Aggregations, Suggestions: payload.Suggestions,
	}
	for index, hit := range payload.Hits.Hits {
		differential.IDs[index], differential.Sources[index], differential.Highlights[index] = hit.ID, hit.Source, hit.Highlight
	}
	return differential
}

func decodeDifferentialRawMap(values map[string]json.RawMessage) map[string]any {
	if values == nil {
		return nil
	}
	result := make(map[string]any, len(values))
	for name, value := range values {
		var decoded any
		_ = json.Unmarshal(value, &decoded)
		result[name] = decoded
	}
	return result
}

func assertRealPITSearchAfterDifferential(t *testing.T, client *adapter.Client, direct *official.Client, tenant, index, alias string, limits search.Limits) {
	t.Helper()
	request := search.Request{Tenant: tenant, Index: index, Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.CursorPage{Size: 2, KeepAlive: time.Minute}}
	adapterIDs := make([]string, 0, 3)
	for pages := 0; pages < limits.MaxPages; pages++ {
		result, err := client.Search(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range result.Hits() {
			adapterIDs = append(adapterIDs, hit.ID)
		}
		if result.NextCursor() == "" {
			break
		}
		request.Page = search.CursorPage{Size: 2, KeepAlive: time.Minute, Cursor: result.NextCursor()}
	}
	created := requireDirectOpenSearchJSON(t, direct, http.MethodPost, "/"+alias+"/_search/point_in_time?keep_alive=1m", nil, http.StatusOK)
	var pit struct {
		ID string `json:"pit_id"`
	}
	if json.Unmarshal(created, &pit) != nil || pit.ID == "" {
		t.Fatalf("direct PIT create = %q", created)
	}
	directIDs := make([]string, 0, 3)
	var searchAfter []json.RawMessage
	for pages := 0; pages < limits.MaxPages; pages++ {
		body := map[string]any{
			"query": map[string]any{"match_all": map[string]any{}}, "size": 2, "version": true,
			"pit":  map[string]any{"id": pit.ID, "keep_alive": "60000ms"},
			"sort": []any{map[string]any{"_id": map[string]any{"order": "asc"}}},
		}
		if len(searchAfter) > 0 {
			body["search_after"] = searchAfter
		}
		encoded, _ := json.Marshal(body)
		responseBody := requireDirectOpenSearchJSON(t, direct, http.MethodPost, "/_search", encoded, http.StatusOK)
		var page struct {
			PITID string `json:"pit_id"`
			Hits  struct {
				Hits []struct {
					ID   string            `json:"_id"`
					Sort []json.RawMessage `json:"sort"`
				} `json:"hits"`
			} `json:"hits"`
		}
		if json.Unmarshal(responseBody, &page) != nil || len(page.Hits.Hits) > 2 {
			t.Fatalf("direct PIT page = %q", responseBody)
		}
		if page.PITID != "" {
			pit.ID = page.PITID
		}
		for _, hit := range page.Hits.Hits {
			directIDs = append(directIDs, hit.ID)
		}
		if len(page.Hits.Hits) < 2 {
			break
		}
		searchAfter = page.Hits.Hits[len(page.Hits.Hits)-1].Sort
	}
	deleteBody, _ := json.Marshal(map[string]string{"pit_id": pit.ID})
	requireDirectOpenSearchJSON(t, direct, http.MethodDelete, "/_search/point_in_time", deleteBody, http.StatusOK)
	if !reflect.DeepEqual(adapterIDs, directIDs) {
		t.Fatalf("adapter/direct PIT traversal = %v/%v", adapterIDs, directIDs)
	}
}

func TestRealOpenSearchConformanceRebuildReconciliationRollbackAndConcurrentApplications(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_URL")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	if endpoint == "" || expectedVersion == "" {
		t.Skip("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are not configured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatal("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are required for a disposable test cluster")
	}

	limits := search.DefaultLimits()
	tenant, logicalIndex := "migration-tenant", "documents"
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	source, target := "golib-search-rebuild-v1-"+suffix, "golib-search-rebuild-v2-"+suffix
	alias, repairAlias := "golib-search-rebuild-"+suffix, "golib-search-repair-"+suffix
	sourceRepairAlias := "golib-search-source-repair-" + suffix
	sourceMapping := json.RawMessage(`{"dynamic":"strict","properties":{"name":{"type":"keyword"}}}`)
	targetMapping := json.RawMessage(`{"dynamic":"strict","properties":{"name":{"type":"keyword"},"category":{"type":"keyword"}}}`)
	settings := json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0}`)
	sourceDefinition, err := search.NewIndexDefinition(source, settings, sourceMapping, limits)
	if err != nil {
		t.Fatal(err)
	}
	targetDefinition, err := search.NewIndexDefinition(target, settings, targetMapping, limits)
	if err != nil {
		t.Fatal(err)
	}

	targetReaderClient := newBoundIntegrationSearchClient(t, endpoint, tenant, logicalIndex, target, target, targetDefinition.Fingerprint(), limits)
	repairClient := newBoundIntegrationSearchClient(t, endpoint, tenant, logicalIndex, repairAlias, target, targetDefinition.Fingerprint(), limits)
	sourceRepairClient := newBoundIntegrationSearchClient(t, endpoint, tenant, logicalIndex, sourceRepairAlias, source, sourceDefinition.Fingerprint(), limits)
	direct, err := official.NewClient(official.Config{Addresses: []string{endpoint}, DisableRetry: true, HealthCheckMaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = direct.Close() })
	verifier := &realLifecycleVerifier{
		client: direct, pageSize: 128, maximumRecords: 4_096, maximumResponseBytes: 16 << 20,
		expectedDefinitions: map[string]search.IndexDefinition{
			sourceDefinition.Fingerprint(): sourceDefinition,
			targetDefinition.Fingerprint(): targetDefinition,
		},
	}
	var currentPhysical atomic.Value
	currentPhysical.Store(source)
	writeFence := newIntegrationWriteFence(&currentPhysical)
	cutoverVerificationStarted := make(chan struct{})
	continueCutoverVerification := make(chan struct{})
	var pauseFirstCutover atomic.Bool
	pauseFirstCutover.Store(true)
	guardedVerifier := adapter.LifecycleVerifierFunc(func(ctx context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
		if writeFence.Quiesced() && pauseFirstCutover.CompareAndSwap(true, false) {
			close(cutoverVerificationStarted)
			select {
			case <-continueCutoverVerification:
			case <-ctx.Done():
				return adapter.LifecycleVerificationResult{}, ctx.Err()
			}
		}
		return verifier.Verify(ctx, request)
	})
	ownedResources := map[string]struct{}{source: {}, target: {}, alias: {}, repairAlias: {}, sourceRepairAlias: {}}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Search: &adapter.SearchConfig{
			Limits: limits, CursorCodec: mustIntegrationCursorCodec(t), Clock: time.Now, Authorizer: allowSearchAuthorization(), WriteGuard: allowWriteAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(_ context.Context, gotTenant, gotIndex string, _ adapter.IndexAccess) (adapter.IndexTarget, error) {
				if gotTenant != tenant || gotIndex != logicalIndex {
					return adapter.IndexTarget{}, errors.New("migration target denied")
				}
				return adapter.IndexTarget{Name: alias, PhysicalName: currentPhysical.Load().(string), Fingerprint: targetDefinition.Fingerprint()}, nil
			}),
		},
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: adapter.LifecycleAuthorizerFunc(func(_ context.Context, gotTenant string, resources []string) error {
				if gotTenant != tenant {
					return errors.New("migration lifecycle denied")
				}
				for _, resource := range resources {
					if _, allowed := ownedResources[resource]; !allowed {
						return errors.New("migration lifecycle denied")
					}
				}
				return nil
			}),
			Verifier:           guardedVerifier,
			CutoverGuard:       writeFence,
			MutationGuard:      allowLifecycleMutationGuard(),
			ReindexCursorCodec: mustIntegrationReindexCursorCodec(t),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if info, infoErr := client.Info(t.Context()); infoErr != nil || info.Version != expectedVersion {
		t.Fatalf("Info() = %#v/%v, expected %s", info, infoErr, expectedVersion)
	}
	for _, definition := range []search.IndexDefinition{sourceDefinition, targetDefinition} {
		if err := client.CreateIndex(t.Context(), tenant, definition); err != nil {
			t.Fatal(err)
		}
		physical := definition.Name()
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = deleteDisposableIndex(ctx, endpoint, physical)
		})
	}
	if err := client.AddAlias(t.Context(), tenant, alias, source, true); err != nil {
		t.Fatal(err)
	}
	if err := client.AddAlias(t.Context(), tenant, repairAlias, target, true); err != nil {
		t.Fatal(err)
	}
	if err := client.AddAlias(t.Context(), tenant, sourceRepairAlias, source, true); err != nil {
		t.Fatal(err)
	}

	write := func(writer *adapter.Client, id string, version uint64, sourceJSON json.RawMessage) search.Document {
		t.Helper()
		if beginErr := writeFence.BeginWrite(t.Context()); beginErr != nil {
			t.Fatal(beginErr)
		}
		defer writeFence.EndWrite()
		document, documentErr := search.NewDocument(tenant, logicalIndex, id, version, sourceJSON, limits)
		if documentErr != nil {
			t.Fatal(documentErr)
		}
		outcome, writeErr := writer.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor)
		if writeErr != nil || outcome.State != search.OutcomeApplied || outcome.Version != version {
			t.Fatalf("Write(%s) = %#v/%v", id, outcome, writeErr)
		}
		return document
	}
	authoritativeDocuments := []search.Document{
		write(client, "source-a", 7, json.RawMessage(`{"name":"alpha"}`)),
		write(client, "source-b", 11, json.RawMessage(`{"name":"beta"}`)),
	}

	cursor, done, err := client.Reindex(t.Context(), tenant, source, target, "")
	if err != nil || done || cursor == "" {
		t.Fatalf("start Reindex() = %q/%t/%v", cursor, done, err)
	}
	authoritativeDocuments = append(authoritativeDocuments,
		write(client, "source-c", 13, json.RawMessage(`{"name":"concurrent"}`)),
	)
	for attempt := 0; attempt < 100 && !done; attempt++ {
		cursor, done, err = client.Reindex(t.Context(), tenant, source, target, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if !done {
			time.Sleep(25 * time.Millisecond)
		}
	}
	if !done {
		t.Fatal("Reindex() did not complete within the bounded poll budget")
	}
	write(repairClient, "target-orphan", 17, json.RawMessage(`{"name":"orphan"}`))
	verification, err := client.VerifyIndex(t.Context(), tenant, source, target, targetDefinition.Fingerprint())
	if err != nil || verification.Verified || verification.Drift < 1 || verification.Drift > 2 {
		t.Fatalf("VerifyIndex() before reconciliation = %#v/%v, want one or two semantic drifts", verification, err)
	}
	driftBeforeReconciliation := verification.Drift
	authoritativeStore, err := newDurableAuthoritativeStore(t.TempDir(), tenant, logicalIndex, authoritativeDocuments, limits)
	if err != nil {
		t.Fatal(err)
	}
	reconciler, err := search.NewReconcilerWithDeletionGuard(
		authoritativeStore,
		targetReaderClient,
		repairClient,
		authoritativeStore,
		limits,
	)
	if err != nil {
		t.Fatal(err)
	}
	reconciliation, err := reconciler.Run(t.Context(), search.ReconciliationRequest{
		Tenant: tenant, Index: logicalIndex, PageSize: 1,
		MaxRecords: 16, Repair: true,
	})
	if err != nil || !reconciliation.Complete || reconciliation.Repaired != int(driftBeforeReconciliation) || len(reconciliation.Drift) != int(driftBeforeReconciliation) {
		t.Fatalf("reconciliation = %#v/%v", reconciliation, err)
	}
	verification, err = client.VerifyIndex(t.Context(), tenant, source, target, targetDefinition.Fingerprint())
	if err != nil || !verification.Verified || verification.Drift != 0 {
		t.Fatalf("VerifyIndex() after reconciliation = %#v/%v", verification, err)
	}
	recreated, err := search.NewDocument(tenant, logicalIndex, "target-orphan", 18, json.RawMessage(`{"name":"recreated"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := authoritativeStore.Put(t.Context(), recreated); err == nil {
		t.Fatal("source store accepted recreation at the reserved tombstone version")
	}
	recreated, err = search.NewDocument(tenant, logicalIndex, "target-orphan", 19, recreated.Source, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := authoritativeStore.Put(t.Context(), recreated); err != nil {
		t.Fatal(err)
	}
	write(sourceRepairClient, recreated.ID, recreated.Version, recreated.Source)
	recreatedReport, err := reconciler.Run(t.Context(), search.ReconciliationRequest{
		Tenant: tenant, Index: logicalIndex, PageSize: 1,
		MaxRecords: 16, Repair: true,
	})
	if err != nil || !recreatedReport.Complete || recreatedReport.Repaired != 1 || len(recreatedReport.Drift) != 1 || recreatedReport.Drift[0].Kind != search.DriftMissing {
		t.Fatalf("recreated source reconciliation = %#v/%v", recreatedReport, err)
	}
	authoritativeDocuments = append(authoritativeDocuments, recreated)
	verification, err = client.VerifyIndex(t.Context(), tenant, source, target, targetDefinition.Fingerprint())
	if err != nil || !verification.Verified || verification.Drift != 0 {
		t.Fatalf("VerifyIndex() after guarded recreation = %#v/%v", verification, err)
	}
	requireDirectOpenSearchJSON(t, direct, http.MethodPut, "/"+target+"/_settings",
		[]byte(`{"index":{"refresh_interval":"17s"}}`), http.StatusOK)
	if _, err := client.VerifyIndex(t.Context(), tenant, source, target, targetDefinition.Fingerprint()); !errors.Is(err, adapter.ErrLifecycleRejected) {
		t.Fatalf("VerifyIndex() accepted live settings drift: %v", err)
	}
	requireDirectOpenSearchJSON(t, direct, http.MethodPut, "/"+target+"/_settings",
		[]byte(`{"index":{"refresh_interval":null}}`), http.StatusOK)
	verification, err = client.VerifyIndex(t.Context(), tenant, source, target, targetDefinition.Fingerprint())
	if err != nil || !verification.Verified || verification.Drift != 0 {
		t.Fatalf("VerifyIndex() after settings restore = %#v/%v", verification, err)
	}
	reindexedRecords, err := readAllRealIndexRecords(t.Context(), targetReaderClient, tenant, logicalIndex, limits)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range authoritativeDocuments {
		record, exists := reindexedRecords[document.ID]
		if !exists || record.Version != document.Version || record.Digest != search.SourceDigest(document.Source) {
			t.Fatalf("reconciled record %q = %#v, want version %d and source digest", document.ID, record, document.Version)
		}
	}

	type cutoverOutcome struct {
		report search.VerificationReport
		err    error
	}
	cutoverResults := make(chan cutoverOutcome, 1)
	go func() {
		report, cutoverErr := client.CutoverAlias(t.Context(), tenant, alias, source, target, targetDefinition.Fingerprint())
		cutoverResults <- cutoverOutcome{report: report, err: cutoverErr}
	}()
	select {
	case <-cutoverVerificationStarted:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	fencedDocument, err := search.NewDocument(tenant, logicalIndex, "fenced-write", 29, json.RawMessage(`{"name":"after-cutover"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	type writeOutcome struct {
		outcome search.ItemOutcome
		err     error
	}
	writeResults := make(chan writeOutcome, 1)
	go func() {
		if beginErr := writeFence.BeginWrite(t.Context()); beginErr != nil {
			writeResults <- writeOutcome{err: beginErr}
			return
		}
		defer writeFence.EndWrite()
		outcome, writeErr := client.Write(t.Context(), search.IndexDocument(fencedDocument), search.RefreshWaitFor)
		writeResults <- writeOutcome{outcome: outcome, err: writeErr}
	}()
	select {
	case <-writeFence.BlockedWriter():
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	close(continueCutoverVerification)
	cutover := <-cutoverResults
	if cutover.err != nil || !cutover.report.Verified {
		t.Fatalf("CutoverAlias() = %#v/%v", cutover.report, cutover.err)
	}
	fencedWrite := <-writeResults
	if fencedWrite.err != nil || fencedWrite.outcome.State != search.OutcomeApplied || fencedWrite.outcome.Version != fencedDocument.Version {
		t.Fatalf("write released after cutover = %#v/%v", fencedWrite.outcome, fencedWrite.err)
	}
	authoritativeDocuments = append(authoritativeDocuments, fencedDocument)
	if resolved, resolveErr := client.ResolveAlias(t.Context(), tenant, alias); resolveErr != nil || resolved != target {
		t.Fatalf("ResolveAlias() after cutover = %q/%v", resolved, resolveErr)
	}
	if copied, copyErr := sourceRepairClient.Write(t.Context(), search.IndexDocument(fencedDocument), search.RefreshWaitFor); copyErr != nil || copied.State != search.OutcomeApplied {
		t.Fatalf("rollback reconciliation write = %#v/%v", copied, copyErr)
	}
	rollback, err := client.CutoverAlias(t.Context(), tenant, alias, target, source, sourceDefinition.Fingerprint())
	if err != nil || !rollback.Verified {
		t.Fatalf("rollback CutoverAlias() = %#v/%v", rollback, err)
	}
	if resolved, resolveErr := client.ResolveAlias(t.Context(), tenant, alias); resolveErr != nil || resolved != source {
		t.Fatalf("ResolveAlias() after alias rollback = %q/%v", resolved, resolveErr)
	}
	highWaterDocuments := append([]search.Document(nil), authoritativeDocuments...)
	outboxDocument := write(client, "source-after-high-water", 31, json.RawMessage(`{"name":"durable-outbox"}`))
	authoritativeDocuments = append(authoritativeDocuments, outboxDocument)
	deletedAfterHighWater := highWaterDocuments[len(highWaterDocuments)-1]
	deleteVersion := deletedAfterHighWater.Version + 1
	if beginErr := writeFence.BeginWrite(t.Context()); beginErr != nil {
		t.Fatal(beginErr)
	}
	deletedOutcome, deleteErr := client.Write(t.Context(), search.DeleteDocument(tenant, logicalIndex, deletedAfterHighWater.ID, deleteVersion), search.RefreshWaitFor)
	writeFence.EndWrite()
	if deleteErr != nil || deletedOutcome.State != search.OutcomeApplied || deletedOutcome.Version != deleteVersion {
		t.Fatalf("source tombstone after high-water = %#v/%v", deletedOutcome, deleteErr)
	}
	for position := range authoritativeDocuments {
		if authoritativeDocuments[position].ID == deletedAfterHighWater.ID {
			authoritativeDocuments = append(authoritativeDocuments[:position], authoritativeDocuments[position+1:]...)
			break
		}
	}

	if err := deleteDisposableIndex(t.Context(), endpoint, target); err != nil {
		t.Fatal(err)
	}
	if err := client.CreateIndex(t.Context(), tenant, targetDefinition); err != nil {
		t.Fatal(err)
	}
	if err := client.AddAlias(t.Context(), tenant, repairAlias, target, true); err != nil {
		t.Fatal(err)
	}
	rebuildDirectory := t.TempDir()
	rebuildSource, err := newDurableAuthoritativeStore(rebuildDirectory, tenant, logicalIndex, highWaterDocuments, limits)
	if err != nil {
		t.Fatal(err)
	}
	snapshotID := sourceDefinition.Fingerprint() + ":source-revision-at-high-water"
	checkpointPath := filepath.Join(rebuildDirectory, "rebuild-checkpoint.json")
	checkpoint := durableRebuildCheckpoint{SnapshotID: snapshotID}
	if err := saveDurableRebuildCheckpoint(checkpointPath, checkpoint); err != nil {
		t.Fatal(err)
	}
	copyPage := func(reader *durableAuthoritativeStore, current durableRebuildCheckpoint) (durableRebuildCheckpoint, error) {
		page, readErr := reader.Read(t.Context(), tenant, logicalIndex, current.SourceCursor, 1)
		if readErr != nil {
			return current, readErr
		}
		operations := make([]search.WriteOperation, len(page.Records))
		for position, record := range page.Records {
			operations[position] = search.IndexDocument(*record.Document)
		}
		if len(operations) != 0 {
			result, bulkErr := repairClient.Bulk(t.Context(), search.BulkRequest{Operations: operations, Refresh: search.RefreshWaitFor})
			if bulkErr != nil || result.Partial() {
				return current, fmt.Errorf("rebuild source page = %#v/%v", result.Items(), bulkErr)
			}
		}
		current.SourceCursor = page.Cursor
		current.SourceComplete = page.Done
		if saveErr := saveDurableRebuildCheckpoint(checkpointPath, current); saveErr != nil {
			return current, saveErr
		}
		return current, nil
	}
	checkpoint, err = copyPage(rebuildSource, checkpoint)
	if err != nil || checkpoint.SourceComplete {
		t.Fatalf("interrupted rebuild first checkpoint = %#v/%v", checkpoint, err)
	}
	checkpoint, err = loadDurableRebuildCheckpoint(checkpointPath, snapshotID)
	if err != nil {
		t.Fatal(err)
	}
	restartedSource := &durableAuthoritativeStore{
		path: rebuildSource.path, tenant: tenant, index: logicalIndex, limits: limits,
	}
	for !checkpoint.SourceComplete {
		checkpoint, err = copyPage(restartedSource, checkpoint)
		if err != nil {
			t.Fatal(err)
		}
	}
	checkpoint, err = loadDurableRebuildCheckpoint(checkpointPath, snapshotID)
	if err != nil || !checkpoint.SourceComplete || checkpoint.SourceCursor != "" || checkpoint.OutboxCursor != 0 {
		t.Fatalf("resumed source checkpoint = %#v/%v", checkpoint, err)
	}
	outbox := []search.WriteOperation{
		search.IndexDocument(outboxDocument),
		search.DeleteDocument(tenant, logicalIndex, deletedAfterHighWater.ID, deleteVersion),
	}
	for checkpoint.OutboxCursor < len(outbox) {
		outcome, replayErr := repairClient.Write(t.Context(), outbox[checkpoint.OutboxCursor], search.RefreshWaitFor)
		if replayErr != nil || outcome.State != search.OutcomeApplied {
			t.Fatalf("outbox replay %d = %#v/%v", checkpoint.OutboxCursor, outcome, replayErr)
		}
		checkpoint.OutboxCursor++
		if err := saveDurableRebuildCheckpoint(checkpointPath, checkpoint); err != nil {
			t.Fatal(err)
		}
		checkpoint, err = loadDurableRebuildCheckpoint(checkpointPath, snapshotID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := rebuildSource.Put(t.Context(), outboxDocument); err != nil {
		t.Fatal(err)
	}
	if err := rebuildSource.Delete(t.Context(), deletedAfterHighWater.ID, deleteVersion); err != nil {
		t.Fatal(err)
	}
	rebuildReconciler, err := search.NewReconciler(rebuildSource, targetReaderClient, repairClient, limits)
	if err != nil {
		t.Fatal(err)
	}
	for pass := 1; pass <= 2; pass++ {
		rebuilt, reconcileErr := rebuildReconciler.Run(t.Context(), search.ReconciliationRequest{
			Tenant: tenant, Index: logicalIndex, PageSize: 1, MaxRecords: 32,
		})
		if reconcileErr != nil || !rebuilt.Complete || rebuilt.Repaired != 0 || len(rebuilt.Drift) != 0 {
			t.Fatalf("post-rebuild zero-drift pass %d = %#v/%v", pass, rebuilt, reconcileErr)
		}
	}
	verification, err = client.VerifyIndex(t.Context(), tenant, source, target, targetDefinition.Fingerprint())
	if err != nil || !verification.Verified || verification.Drift != 0 {
		t.Fatalf("VerifyIndex() after full rebuild = %#v/%v", verification, err)
	}
	rebuildCutover, err := client.CutoverAlias(t.Context(), tenant, alias, source, target, targetDefinition.Fingerprint())
	if err != nil || !rebuildCutover.Verified {
		t.Fatalf("rebuild CutoverAlias() = %#v/%v", rebuildCutover, err)
	}

	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	baseTransport.Proxy = nil
	t.Cleanup(baseTransport.CloseIdleConnections)
	probe := newOverlapProbeTransport(baseTransport)
	appA := newBoundIntegrationSearchClientWithTransport(t, endpoint, tenant, logicalIndex, alias, target, "concurrent-instance-a", limits, probe)
	appB := newBoundIntegrationSearchClientWithTransport(t, endpoint, tenant, logicalIndex, alias, target, "concurrent-instance-b", limits, probe)
	writeErrors := make(chan error, 2)
	start := make(chan struct{})
	var applications sync.WaitGroup
	applications.Add(2)
	go func() {
		defer applications.Done()
		<-start
		result, queryErr := appA.Search(t.Context(), search.Request{
			Tenant: tenant, Index: logicalIndex,
			Query: search.TermQuery{Field: "name", Value: search.StringValue("alpha")},
			Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
			Page:  search.OffsetPage{Size: 10},
		})
		if queryErr != nil || len(result.Hits()) != 1 || result.Hits()[0].ID != "source-a" {
			writeErrors <- fmt.Errorf("v1 query during v2 write = %#v/%v", result.Hits(), queryErr)
			return
		}
		document, documentErr := search.NewDocument(tenant, logicalIndex, "app-v1", 19, json.RawMessage(`{"name":"legacy"}`), limits)
		if documentErr != nil {
			writeErrors <- documentErr
			return
		}
		outcome, writeErr := appA.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor)
		if writeErr != nil || outcome.State != search.OutcomeApplied || outcome.Version != 19 {
			writeErrors <- fmt.Errorf("v1 write during v2 query = %#v/%v", outcome, writeErr)
		}
	}()
	go func() {
		defer applications.Done()
		<-start
		document, documentErr := search.NewDocument(tenant, logicalIndex, "app-v2", 23, json.RawMessage(`{"name":"current","category":"modern"}`), limits)
		if documentErr != nil {
			writeErrors <- documentErr
			return
		}
		outcome, writeErr := appB.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor)
		if writeErr != nil || outcome.State != search.OutcomeApplied || outcome.Version != 23 {
			writeErrors <- fmt.Errorf("v2 write during v1 query = %#v/%v", outcome, writeErr)
			return
		}
		result, queryErr := appB.Search(t.Context(), search.Request{
			Tenant: tenant, Index: logicalIndex,
			Query: search.TermQuery{Field: "category", Value: search.StringValue("modern")},
			Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
			Page:  search.OffsetPage{Size: 10},
		})
		if queryErr != nil || len(result.Hits()) != 1 || result.Hits()[0].ID != "app-v2" {
			writeErrors <- fmt.Errorf("v2 query during v1 write = %#v/%v", result.Hits(), queryErr)
		}
	}()
	close(start)
	applications.Wait()
	close(writeErrors)
	for writeErr := range writeErrors {
		if writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if !probe.Overlapped() {
		t.Fatal("concurrent application instances did not overlap at the real transport boundary")
	}
	legacyCursorRequest := search.Request{
		Tenant: tenant, Index: logicalIndex, Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
	}
	legacyCursorPage, err := appA.Search(t.Context(), legacyCursorRequest)
	if err != nil || legacyCursorPage.NextCursor() == "" {
		t.Fatalf("undeclared-version cursor = %#v/%v", legacyCursorPage.Hits(), err)
	}
	legacyCursorRequest.Page = search.CursorPage{Size: 1, Cursor: legacyCursorPage.NextCursor(), KeepAlive: time.Minute}
	if _, err := appB.Search(t.Context(), legacyCursorRequest); !errors.Is(err, search.ErrIndexChanged) {
		t.Fatalf("undeclared mixed-generation cursor error = %v, want ErrIndexChanged", err)
	}
	for legacyCursorRequest.Page.(search.CursorPage).Cursor != "" {
		legacyCursorPage, err = appA.Search(t.Context(), legacyCursorRequest)
		if err != nil {
			t.Fatal(err)
		}
		legacyCursorRequest.Page = search.CursorPage{Size: 1, Cursor: legacyCursorPage.NextCursor(), KeepAlive: time.Minute}
	}
	legacyResult, err := appA.Search(t.Context(), search.Request{
		Tenant: tenant, Index: logicalIndex,
		Query: search.TermQuery{Field: "name", Value: search.StringValue("legacy")},
		Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page:  search.OffsetPage{Size: 10},
	})
	if err != nil || len(legacyResult.Hits()) != 1 || legacyResult.Hits()[0].ID != "app-v1" {
		t.Fatalf("legacy application query = %#v/%v", legacyResult.Hits(), err)
	}
	currentResult, err := appB.Search(t.Context(), search.Request{
		Tenant: tenant, Index: logicalIndex,
		Query: search.TermQuery{Field: "category", Value: search.StringValue("modern")},
		Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page:  search.OffsetPage{Size: 10},
	})
	if err != nil || len(currentResult.Hits()) != 1 || currentResult.Hits()[0].ID != "app-v2" {
		t.Fatalf("current application query = %#v/%v", currentResult.Hits(), err)
	}
	requireDirectOpenSearchJSON(t, direct, http.MethodPut, "/"+target+"/_mapping",
		[]byte(`{"properties":{"drift_only":{"type":"keyword"}}}`), http.StatusOK)
	if _, err := client.VerifyIndex(t.Context(), tenant, source, target, targetDefinition.Fingerprint()); !errors.Is(err, adapter.ErrLifecycleRejected) {
		t.Fatalf("VerifyIndex() accepted live mapping drift with unchanged document counts: %v", err)
	}
}

func TestRealOpenSearchSnapshotRestore(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_URL")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	repositoryRoot := os.Getenv("OPENSEARCH_SNAPSHOT_REPOSITORY_PATH")
	if endpoint == "" || expectedVersion == "" || repositoryRoot == "" {
		t.Skip("OPENSEARCH_URL, OPENSEARCH_EXPECTED_VERSION, and OPENSEARCH_SNAPSHOT_REPOSITORY_PATH are not configured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || !strings.HasPrefix(repositoryRoot, "/") {
		t.Fatal("snapshot restore requires a disposable cluster and an absolute repository path")
	}
	for _, segment := range strings.Split(repositoryRoot, "/") {
		if segment == ".." {
			t.Fatal("snapshot repository path must not contain parent traversal")
		}
	}

	limits := search.DefaultLimits()
	tenant, logicalIndex := "snapshot-tenant", "documents"
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	physical, alias := "golib-search-snapshot-"+suffix, "golib-search-snapshot-alias-"+suffix
	repository, snapshot := "golib-search-repository-"+suffix, "golib-search-snapshot-"+suffix
	definition, err := search.NewIndexDefinition(physical,
		json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0}`),
		json.RawMessage(`{"dynamic":"strict","properties":{"name":{"type":"keyword"}}}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	ownedResources := map[string]struct{}{physical: {}, alias: {}}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Search: &adapter.SearchConfig{
			Limits: limits, CursorCodec: mustIntegrationCursorCodec(t), Clock: time.Now, Authorizer: allowSearchAuthorization(), WriteGuard: allowWriteAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(_ context.Context, gotTenant, gotIndex string, _ adapter.IndexAccess) (adapter.IndexTarget, error) {
				if gotTenant != tenant || gotIndex != logicalIndex {
					return adapter.IndexTarget{}, errors.New("snapshot target denied")
				}
				return adapter.IndexTarget{Name: alias, PhysicalName: physical, Fingerprint: definition.Fingerprint()}, nil
			}),
		},
		Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(func(_ context.Context, gotTenant string, resources []string) error {
			if gotTenant != tenant {
				return errors.New("snapshot lifecycle denied")
			}
			for _, resource := range resources {
				if _, allowed := ownedResources[resource]; !allowed {
					return errors.New("snapshot lifecycle denied")
				}
			}
			return nil
		}), MutationGuard: allowLifecycleMutationGuard()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = deleteDisposableIndex(ctx, endpoint, physical)
	})
	if info, infoErr := client.Info(t.Context()); infoErr != nil || info.Version != expectedVersion {
		t.Fatalf("Info() = %#v/%v, expected %s", info, infoErr, expectedVersion)
	}
	if err := client.CreateIndex(t.Context(), tenant, definition); err != nil {
		t.Fatal(err)
	}
	if err := client.AddAlias(t.Context(), tenant, alias, physical, true); err != nil {
		t.Fatal(err)
	}

	want := map[string]search.ReconciliationRecord{}
	for _, fixture := range []struct {
		id      string
		version uint64
		source  json.RawMessage
	}{
		{id: "snapshot-a", version: 7, source: json.RawMessage(`{"name":"alpha"}`)},
		{id: "snapshot-b", version: 11, source: json.RawMessage(`{"name":"beta"}`)},
	} {
		document, documentErr := search.NewDocument(tenant, logicalIndex, fixture.id, fixture.version, fixture.source, limits)
		if documentErr != nil {
			t.Fatal(documentErr)
		}
		outcome, writeErr := client.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor)
		if writeErr != nil || outcome.State != search.OutcomeApplied || outcome.Version != fixture.version {
			t.Fatalf("Write(%s) = %#v/%v", fixture.id, outcome, writeErr)
		}
		want[fixture.id] = search.IndexRecord(fixture.id, fixture.version, search.SourceDigest(fixture.source))
	}

	direct, err := official.NewClient(official.Config{
		Addresses: []string{endpoint}, DisableRetry: true,
		HealthCheckMaxRetries: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = direct.Close() })
	repositoryBody, _ := json.Marshal(map[string]any{
		"type": "fs",
		"settings": map[string]any{
			"location": repositoryRoot + "/" + repository,
			"compress": true,
		},
	})
	repositoryResponse := requireDirectOpenSearchJSON(t, direct, http.MethodPut, "/_snapshot/"+repository, repositoryBody, http.StatusOK)
	var acknowledged struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if json.Unmarshal(repositoryResponse, &acknowledged) != nil || !acknowledged.Acknowledged {
		t.Fatalf("register snapshot repository response = %q", repositoryResponse)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, _ = directOpenSearchJSON(ctx, direct, http.MethodDelete, "/_snapshot/"+repository+"/"+snapshot, nil)
		_, _, _ = directOpenSearchJSON(ctx, direct, http.MethodDelete, "/_snapshot/"+repository, nil)
	})

	snapshotBody, _ := json.Marshal(map[string]any{"indices": physical, "include_global_state": false})
	snapshotResponse := requireDirectOpenSearchJSON(t, direct, http.MethodPut,
		"/_snapshot/"+repository+"/"+snapshot+"?wait_for_completion=true", snapshotBody, http.StatusOK)
	var snapshotResult struct {
		Snapshot struct {
			State   string   `json:"state"`
			Indices []string `json:"indices"`
			Shards  struct {
				Total, Failed, Successful int
			} `json:"shards"`
		} `json:"snapshot"`
	}
	if json.Unmarshal(snapshotResponse, &snapshotResult) != nil || snapshotResult.Snapshot.State != "SUCCESS" ||
		len(snapshotResult.Snapshot.Indices) != 1 || snapshotResult.Snapshot.Indices[0] != physical ||
		snapshotResult.Snapshot.Shards.Total <= 0 || snapshotResult.Snapshot.Shards.Failed != 0 ||
		snapshotResult.Snapshot.Shards.Successful != snapshotResult.Snapshot.Shards.Total {
		t.Fatalf("completed snapshot response = %q", snapshotResponse)
	}

	if err := deleteDisposableIndex(t.Context(), endpoint, physical); err != nil {
		t.Fatal(err)
	}
	restoreBody, _ := json.Marshal(map[string]any{"indices": physical, "include_global_state": false})
	restoreResponse := requireDirectOpenSearchJSON(t, direct, http.MethodPost,
		"/_snapshot/"+repository+"/"+snapshot+"/_restore?wait_for_completion=true", restoreBody, http.StatusOK)
	var restoreResult struct {
		Snapshot struct {
			Shards struct {
				Total, Failed, Successful int
			} `json:"shards"`
		} `json:"snapshot"`
	}
	if json.Unmarshal(restoreResponse, &restoreResult) != nil || restoreResult.Snapshot.Shards.Total <= 0 ||
		restoreResult.Snapshot.Shards.Failed != 0 || restoreResult.Snapshot.Shards.Successful != restoreResult.Snapshot.Shards.Total {
		t.Fatalf("completed restore response = %q", restoreResponse)
	}
	if err := client.AddAlias(t.Context(), tenant, alias, physical, true); err != nil {
		t.Fatal(err)
	}

	result, err := client.Search(t.Context(), search.Request{
		Tenant: tenant, Index: logicalIndex, Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.OffsetPage{Size: 10},
	})
	if err != nil || len(result.Hits()) != len(want) {
		t.Fatalf("Search() after snapshot restore = %#v/%v", result.Hits(), err)
	}
	for _, hit := range result.Hits() {
		record, exists := want[hit.ID]
		if !exists || hit.Version != record.Version || search.SourceDigest(hit.Source) != record.Digest {
			t.Fatalf("restored hit = %#v, want version and source digest %#v", hit, record)
		}
	}
}

func requireDirectOpenSearchJSON(t *testing.T, client *official.Client, method, target string, body []byte, status int) []byte {
	t.Helper()
	responseBody, responseStatus, err := directOpenSearchJSON(t.Context(), client, method, target, body)
	if err != nil || responseStatus != status {
		t.Fatalf("direct OpenSearch %s %s = status %d body %q error %v", method, target, responseStatus, responseBody, err)
	}
	return responseBody
}

func directOpenSearchJSON(ctx context.Context, client *official.Client, method, target string, body []byte) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Stream(request)
	if err != nil {
		return nil, 0, err
	}
	if response == nil {
		return nil, 0, errors.New("OpenSearch returned no response")
	}
	defer func() { _ = response.Body.Close() }()
	const maximumBodyBytes = 1 << 20
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maximumBodyBytes+1))
	if err != nil {
		return nil, response.StatusCode, err
	}
	if len(responseBody) > maximumBodyBytes {
		return nil, response.StatusCode, errors.New("OpenSearch response exceeded test bound")
	}
	return responseBody, response.StatusCode, nil
}

const maximumDurableAuthoritativeFixtureBytes = 16 << 20

type durableAuthoritativeDocument struct {
	ID      string          `json:"id"`
	Version uint64          `json:"version"`
	Source  json.RawMessage `json:"source"`
}

type durableAuthoritativeState struct {
	Revision   uint64                         `json:"revision"`
	Tenant     string                         `json:"tenant"`
	Index      string                         `json:"index"`
	Documents  []durableAuthoritativeDocument `json:"documents"`
	Tombstones map[string]uint64              `json:"tombstones"`
}

// durableAuthoritativeStore is a real fsync-backed source fixture. Its reader
// and deletion reservation share one lock and one persisted revision so the
// integration test cannot substitute snapshot absence for deletion authority.
type durableAuthoritativeStore struct {
	mu                  sync.Mutex
	path, tenant, index string
	limits              search.Limits
}

func newDurableAuthoritativeStore(directory, tenant, index string, documents []search.Document, limits search.Limits) (*durableAuthoritativeStore, error) {
	store := &durableAuthoritativeStore{path: filepath.Join(directory, "authoritative-source.json"), tenant: tenant, index: index, limits: limits}
	state := durableAuthoritativeState{Revision: 1, Tenant: tenant, Index: index, Tombstones: make(map[string]uint64)}
	state.Documents = make([]durableAuthoritativeDocument, len(documents))
	for position, document := range documents {
		validated, err := search.NewDocument(document.Tenant, document.Index, document.ID, document.Version, document.Source, limits)
		if err != nil || validated.Tenant != tenant || validated.Index != index {
			return nil, errors.New("durable authoritative source seed rejected")
		}
		state.Documents[position] = durableAuthoritativeDocument{ID: validated.ID, Version: validated.Version, Source: validated.Source}
	}
	sort.Slice(state.Documents, func(left, right int) bool { return state.Documents[left].ID < state.Documents[right].ID })
	for position := 1; position < len(state.Documents); position++ {
		if state.Documents[position-1].ID == state.Documents[position].ID {
			return nil, errors.New("durable authoritative source duplicate seed")
		}
	}
	if err := store.saveLocked(state); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *durableAuthoritativeStore) Read(ctx context.Context, tenant, index, cursor string, pageSize int) (search.ReconciliationPage, error) {
	if ctx == nil || ctx.Err() != nil || tenant != store.tenant || index != store.index || pageSize <= 0 || pageSize > store.limits.MaxPageItems {
		return search.ReconciliationPage{}, errors.New("durable authoritative source read rejected")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	state, err := store.loadLocked()
	if err != nil {
		return search.ReconciliationPage{}, err
	}
	start := 0
	if cursor != "" {
		var revision uint64
		if _, err := fmt.Sscanf(cursor, "%d:%d", &revision, &start); err != nil || cursor != fmt.Sprintf("%d:%d", revision, start) || revision != state.Revision || start < 0 || start >= len(state.Documents) {
			return search.ReconciliationPage{}, errors.New("durable authoritative source cursor rejected")
		}
	}
	end := min(start+pageSize, len(state.Documents))
	records := make([]search.ReconciliationRecord, end-start)
	for position, persisted := range state.Documents[start:end] {
		document, err := search.NewDocument(tenant, index, persisted.ID, persisted.Version, persisted.Source, store.limits)
		if err != nil {
			return search.ReconciliationPage{}, err
		}
		records[position] = search.SourceRecord(document)
	}
	done := end == len(state.Documents)
	next := ""
	if !done {
		next = fmt.Sprintf("%d:%d", state.Revision, end)
	}
	return search.ReconciliationPage{Records: records, Cursor: next, Done: done}, nil
}

func (store *durableAuthoritativeStore) ReserveDeletion(ctx context.Context, deletion search.ReconciliationDeletion) (uint64, error) {
	if ctx == nil || ctx.Err() != nil || deletion.Tenant != store.tenant || deletion.Index != store.index || deletion.ID == "" || len(deletion.ID) > store.limits.MaxIDBytes {
		return 0, errors.New("durable authoritative deletion scope rejected")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	state, err := store.loadLocked()
	if err != nil {
		return 0, err
	}
	position := sort.Search(len(state.Documents), func(position int) bool { return state.Documents[position].ID >= deletion.ID })
	if position < len(state.Documents) && state.Documents[position].ID == deletion.ID {
		return 0, errors.New("authoritative source identity still exists")
	}
	reserved := state.Tombstones[deletion.ID]
	if reserved > deletion.ObservedIndexVersion {
		return reserved, nil
	}
	base := deletion.ObservedIndexVersion
	if base >= math.MaxUint64-1 {
		return 0, errors.New("authoritative source version space exhausted")
	}
	version := base + 1
	state.Tombstones[deletion.ID] = version
	if state.Revision == math.MaxUint64 {
		return 0, errors.New("authoritative source revision space exhausted")
	}
	state.Revision++
	if err := store.saveLocked(state); err != nil {
		return 0, err
	}
	return version, nil
}

func (store *durableAuthoritativeStore) Put(ctx context.Context, document search.Document) error {
	if ctx == nil || ctx.Err() != nil {
		return errors.New("durable authoritative source write rejected")
	}
	validated, err := search.NewDocument(document.Tenant, document.Index, document.ID, document.Version, document.Source, store.limits)
	if err != nil || validated.Tenant != store.tenant || validated.Index != store.index {
		return errors.New("durable authoritative source write rejected")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	state, err := store.loadLocked()
	if err != nil {
		return err
	}
	if validated.Version <= state.Tombstones[validated.ID] {
		return errors.New("durable authoritative source write is not newer than tombstone")
	}
	position := sort.Search(len(state.Documents), func(position int) bool { return state.Documents[position].ID >= validated.ID })
	persisted := durableAuthoritativeDocument{ID: validated.ID, Version: validated.Version, Source: validated.Source}
	if position < len(state.Documents) && state.Documents[position].ID == validated.ID {
		if validated.Version <= state.Documents[position].Version {
			return errors.New("durable authoritative source write is stale")
		}
		state.Documents[position] = persisted
	} else {
		state.Documents = append(state.Documents, durableAuthoritativeDocument{})
		copy(state.Documents[position+1:], state.Documents[position:])
		state.Documents[position] = persisted
	}
	delete(state.Tombstones, validated.ID)
	if state.Revision == math.MaxUint64 {
		return errors.New("authoritative source revision space exhausted")
	}
	state.Revision++
	return store.saveLocked(state)
}

func (store *durableAuthoritativeStore) Delete(ctx context.Context, id string, version uint64) error {
	if ctx == nil || ctx.Err() != nil || id == "" || len(id) > store.limits.MaxIDBytes || version == 0 || version == math.MaxUint64 {
		return errors.New("durable authoritative source delete rejected")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	state, err := store.loadLocked()
	if err != nil {
		return err
	}
	position := sort.Search(len(state.Documents), func(position int) bool { return state.Documents[position].ID >= id })
	if position >= len(state.Documents) || state.Documents[position].ID != id {
		if state.Tombstones[id] == version {
			return nil
		}
		return errors.New("durable authoritative source identity is absent")
	}
	if version <= state.Documents[position].Version || version <= state.Tombstones[id] {
		return errors.New("durable authoritative source delete is stale")
	}
	copy(state.Documents[position:], state.Documents[position+1:])
	state.Documents = state.Documents[:len(state.Documents)-1]
	state.Tombstones[id] = version
	if state.Revision == math.MaxUint64 {
		return errors.New("authoritative source revision space exhausted")
	}
	state.Revision++
	return store.saveLocked(state)
}

func (store *durableAuthoritativeStore) AuthorizeWrite(ctx context.Context, authorization adapter.WriteAuthorization) error {
	if ctx == nil {
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	operations := authorization.Operations()
	if len(operations) == 0 || len(operations) > store.limits.MaxBulkItems {
		return errors.New("durable authoritative write authorization rejected")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	state, err := store.loadLocked()
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if operation.Tenant != store.tenant || operation.Index != store.index {
			return errors.New("durable authoritative write scope rejected")
		}
		position := sort.Search(len(state.Documents), func(position int) bool { return state.Documents[position].ID >= operation.ID })
		live := position < len(state.Documents) && state.Documents[position].ID == operation.ID
		switch operation.Action {
		case search.ActionIndex, search.ActionUpsert:
			if !live || state.Documents[position].Version != operation.Version || !bytes.Equal(state.Documents[position].Source, operation.Source) {
				return errors.New("durable authoritative current document rejected")
			}
		case search.ActionDelete:
			if live || state.Tombstones[operation.ID] != operation.Version {
				return errors.New("durable authoritative tombstone rejected")
			}
		default:
			return errors.New("durable authoritative write action rejected")
		}
	}
	return nil
}

func (store *durableAuthoritativeStore) loadLocked() (durableAuthoritativeState, error) {
	file, err := os.Open(store.path)
	if err != nil {
		return durableAuthoritativeState{}, err
	}
	defer func() { _ = file.Close() }()
	encoded, err := io.ReadAll(io.LimitReader(file, maximumDurableAuthoritativeFixtureBytes+1))
	if err != nil || len(encoded) > maximumDurableAuthoritativeFixtureBytes {
		return durableAuthoritativeState{}, errors.New("durable authoritative source file rejected")
	}
	var state durableAuthoritativeState
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || decoder.Decode(&struct{}{}) != io.EOF || state.Revision == 0 || state.Tenant != store.tenant || state.Index != store.index || state.Tombstones == nil || len(state.Documents) > store.limits.MaxPages*store.limits.MaxPageItems {
		return durableAuthoritativeState{}, errors.New("durable authoritative source state rejected")
	}
	for position, persisted := range state.Documents {
		if _, err := search.NewDocument(state.Tenant, state.Index, persisted.ID, persisted.Version, persisted.Source, store.limits); err != nil || position > 0 && state.Documents[position-1].ID >= persisted.ID {
			return durableAuthoritativeState{}, errors.New("durable authoritative source document rejected")
		}
		if _, tombstoned := state.Tombstones[persisted.ID]; tombstoned {
			return durableAuthoritativeState{}, errors.New("durable authoritative source identity is both live and deleted")
		}
	}
	for id, version := range state.Tombstones {
		if id == "" || len(id) > store.limits.MaxIDBytes || version == 0 || version == math.MaxUint64 {
			return durableAuthoritativeState{}, errors.New("durable authoritative source tombstone rejected")
		}
	}
	return state, nil
}

func (store *durableAuthoritativeStore) saveLocked(state durableAuthoritativeState) error {
	encoded, err := json.Marshal(state)
	if err != nil || len(encoded) > maximumDurableAuthoritativeFixtureBytes {
		return errors.New("durable authoritative source encoding rejected")
	}
	directory := filepath.Dir(store.path)
	temporary, err := os.CreateTemp(directory, ".authoritative-source-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if _, err := temporary.Write(encoded); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, store.path); err != nil {
		return err
	}
	committed = true
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer func() { _ = directoryHandle.Close() }()
	return directoryHandle.Sync()
}

type authoritativeFixtureReader struct {
	tenant, index string
	records       []search.ReconciliationRecord
}

func (reader authoritativeFixtureReader) Read(_ context.Context, tenant, index, cursor string, pageSize int) (search.ReconciliationPage, error) {
	if tenant != reader.tenant || index != reader.index || pageSize <= 0 {
		return search.ReconciliationPage{}, errors.New("authoritative fixture scope denied")
	}
	start := 0
	if cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 || parsed >= len(reader.records) {
			return search.ReconciliationPage{}, errors.New("authoritative fixture cursor rejected")
		}
		start = parsed
	}
	end := min(start+pageSize, len(reader.records))
	done := end == len(reader.records)
	next := ""
	if !done {
		next = strconv.Itoa(end)
	}
	return search.ReconciliationPage{
		Records: append([]search.ReconciliationRecord(nil), reader.records[start:end]...), Cursor: next, Done: done,
	}, nil
}

type overlapProbeTransport struct {
	next       http.RoundTripper
	released   chan struct{}
	release    sync.Once
	entered    atomic.Int32
	overlapped atomic.Bool
}

func newOverlapProbeTransport(next http.RoundTripper) *overlapProbeTransport {
	return &overlapProbeTransport{next: next, released: make(chan struct{})}
}

func (transport *overlapProbeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport.entered.Add(1) >= 2 {
		transport.overlapped.Store(true)
		transport.release.Do(func() { close(transport.released) })
	}
	select {
	case <-transport.released:
	case <-request.Context().Done():
		return nil, request.Context().Err()
	}
	return transport.next.RoundTrip(request)
}

func (transport *overlapProbeTransport) Overlapped() bool { return transport.overlapped.Load() }

type integrationWriteFence struct {
	mu          sync.Mutex
	quiesced    bool
	active      int
	changed     chan struct{}
	blocked     chan struct{}
	blockedOnce sync.Once
	physical    *atomic.Value
}

func newIntegrationWriteFence(physical *atomic.Value) *integrationWriteFence {
	return &integrationWriteFence{changed: make(chan struct{}), blocked: make(chan struct{}), physical: physical}
}

func (fence *integrationWriteFence) BeginWrite(ctx context.Context) error {
	for {
		fence.mu.Lock()
		if !fence.quiesced {
			fence.active++
			fence.mu.Unlock()
			return nil
		}
		fence.blockedOnce.Do(func() { close(fence.blocked) })
		changed := fence.changed
		fence.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (fence *integrationWriteFence) EndWrite() {
	fence.mu.Lock()
	fence.active--
	fence.notifyLocked()
	fence.mu.Unlock()
}

func (fence *integrationWriteFence) WithWritesQuiesced(ctx context.Context, request adapter.LifecycleCutoverRequest, operation func() error) error {
	fence.mu.Lock()
	if fence.quiesced {
		fence.mu.Unlock()
		return errors.New("write fence is already active")
	}
	fence.quiesced = true
	fence.notifyLocked()
	for fence.active > 0 {
		changed := fence.changed
		fence.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			fence.mu.Lock()
			fence.quiesced = false
			fence.notifyLocked()
			fence.mu.Unlock()
			return ctx.Err()
		}
		fence.mu.Lock()
	}
	fence.mu.Unlock()
	defer func() {
		fence.mu.Lock()
		fence.quiesced = false
		fence.notifyLocked()
		fence.mu.Unlock()
	}()
	if err := operation(); err != nil {
		return err
	}
	fence.physical.Store(request.Target)
	return nil
}

func (fence *integrationWriteFence) Quiesced() bool {
	fence.mu.Lock()
	defer fence.mu.Unlock()
	return fence.quiesced
}

func (fence *integrationWriteFence) BlockedWriter() <-chan struct{} { return fence.blocked }

func (fence *integrationWriteFence) notifyLocked() {
	close(fence.changed)
	fence.changed = make(chan struct{})
}

func newBoundIntegrationSearchClient(t *testing.T, endpoint, tenant, logicalIndex, target, physical, fingerprint string, limits search.Limits) *adapter.Client {
	t.Helper()
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Search: &adapter.SearchConfig{
			Limits: limits, CursorCodec: mustIntegrationCursorCodec(t), Clock: time.Now, Authorizer: allowSearchAuthorization(), WriteGuard: allowWriteAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(_ context.Context, gotTenant, gotIndex string, _ adapter.IndexAccess) (adapter.IndexTarget, error) {
				if gotTenant != tenant || gotIndex != logicalIndex {
					return adapter.IndexTarget{}, errors.New("integration target denied")
				}
				return adapter.IndexTarget{Name: target, PhysicalName: physical, Fingerprint: fingerprint}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func newBoundIntegrationSearchClientWithTransport(
	t *testing.T,
	endpoint, tenant, logicalIndex, target, physical, fingerprint string,
	limits search.Limits,
	transport http.RoundTripper,
) *adapter.Client {
	t.Helper()
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		Transport: transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Search: &adapter.SearchConfig{
			Limits: limits, CursorCodec: mustIntegrationCursorCodec(t), Clock: time.Now, Authorizer: allowSearchAuthorization(), WriteGuard: allowWriteAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(_ context.Context, gotTenant, gotIndex string, _ adapter.IndexAccess) (adapter.IndexTarget, error) {
				if gotTenant != tenant || gotIndex != logicalIndex {
					return adapter.IndexTarget{}, errors.New("integration target denied")
				}
				return adapter.IndexTarget{Name: target, PhysicalName: physical, Fingerprint: fingerprint}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func compareRealIndexRecords(ctx context.Context, source, target *adapter.Client, tenant, index string, limits search.Limits) (uint64, error) {
	sourceRecords, err := readAllRealIndexRecords(ctx, source, tenant, index, limits)
	if err != nil {
		return 0, err
	}
	targetRecords, err := readAllRealIndexRecords(ctx, target, tenant, index, limits)
	if err != nil {
		return 0, err
	}
	var drift uint64
	for id, sourceRecord := range sourceRecords {
		targetRecord, exists := targetRecords[id]
		if !exists || sourceRecord.Version != targetRecord.Version || sourceRecord.Digest != targetRecord.Digest {
			drift++
		}
	}
	for id := range targetRecords {
		if _, exists := sourceRecords[id]; !exists {
			drift++
		}
	}
	return drift, nil
}

func readAllRealIndexRecords(ctx context.Context, client *adapter.Client, tenant, index string, limits search.Limits) (map[string]search.ReconciliationRecord, error) {
	records := make(map[string]search.ReconciliationRecord)
	cursor := ""
	pageSize := min(limits.MaxPageItems, 64)
	for pageNumber := 0; pageNumber < limits.MaxPages; pageNumber++ {
		page, err := client.Read(ctx, tenant, index, cursor, pageSize)
		if err != nil {
			return nil, err
		}
		for _, record := range page.Records {
			if _, duplicate := records[record.ID]; duplicate {
				return nil, fmt.Errorf("duplicate reconciliation record %q", record.ID)
			}
			records[record.ID] = record
		}
		if page.Done {
			return records, nil
		}
		if page.Cursor == "" || page.Cursor == cursor {
			return nil, errors.New("non-progressing reconciliation cursor")
		}
		cursor = page.Cursor
	}
	return nil, errors.New("reconciliation traversal exceeded page bound")
}

func mustIntegrationCursorCodec(t *testing.T) *search.CursorCodec {
	t.Helper()
	codec, err := search.NewCursorCodec([]byte("integration-cursor-key-32-bytes!!"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func mustIntegrationReindexCursorCodec(t *testing.T) *adapter.ReindexCursorCodec {
	t.Helper()
	codec, err := adapter.NewReindexCursorCodec(
		[]byte("integration-reindex-key-32-bytes!"), time.Now, 4096, time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func TestRealOpenSearchBoundedLoad(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_URL")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	if endpoint == "" || expectedVersion == "" {
		t.Skip("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are not configured")
	}
	duration := time.Duration(0)
	if configured := os.Getenv("OPENSEARCH_SOAK_DURATION"); configured != "" {
		var err error
		duration, err = time.ParseDuration(configured)
		if err != nil || duration <= 0 || duration > 5*time.Minute {
			t.Fatal("OPENSEARCH_SOAK_DURATION must be greater than zero and at most 5m")
		}
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatal("OPENSEARCH_URL is invalid")
	}
	limits := search.DefaultLimits()
	tenant, logicalIndex := "load-tenant", "documents"
	physical := fmt.Sprintf("golib-search-load-%d", time.Now().UnixNano())
	alias := physical + "-alias"
	definition, err := search.NewIndexDefinition(physical,
		json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0,"refresh_interval":"1s"}`),
		json.RawMessage(`{"dynamic":"strict","properties":{"sequence":{"type":"long"},"cardinality":{"type":"keyword"},"payload":{"type":"keyword"}}}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	baseTransport.Proxy = nil
	t.Cleanup(baseTransport.CloseIdleConnections)
	network := &integrationCountingTransport{base: baseTransport}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		Transport: network, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Resilience: adapter.ResilienceConfig{MaximumInFlight: 16, MaximumQueued: 16, MaximumQueueWait: time.Second},
		Search: &adapter.SearchConfig{
			Limits: limits, CursorCodec: mustIntegrationCursorCodec(t), Clock: time.Now, Authorizer: allowSearchAuthorization(), WriteGuard: allowWriteAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(_ context.Context, gotTenant, gotIndex string, _ adapter.IndexAccess) (adapter.IndexTarget, error) {
				if gotTenant != tenant || gotIndex != logicalIndex {
					return adapter.IndexTarget{}, errors.New("load target denied")
				}
				return adapter.IndexTarget{Name: alias, PhysicalName: physical, Fingerprint: definition.Fingerprint()}, nil
			}),
		},
		Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(func(_ context.Context, gotTenant string, resources []string) error {
			if gotTenant != tenant || len(resources) == 0 {
				return errors.New("load lifecycle denied")
			}
			for _, resource := range resources {
				if resource != physical && resource != alias {
					return errors.New("load lifecycle denied")
				}
			}
			return nil
		}), MutationGuard: allowLifecycleMutationGuard()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.CreateIndex(t.Context(), tenant, definition); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = deleteDisposableIndex(ctx, endpoint, physical)
	})
	if err := client.AddAlias(t.Context(), tenant, alias, physical, true); err != nil {
		t.Fatal(err)
	}
	const seedDocuments = 256
	seed := make([]search.WriteOperation, 0, seedDocuments)
	for index := range seedDocuments {
		document, documentErr := search.NewDocument(tenant, logicalIndex, fmt.Sprintf("seed-%04d", index), 1,
			json.RawMessage(fmt.Sprintf(`{"sequence":%d,"cardinality":"card-%04d","payload":"seed"}`, index, index)), limits)
		if documentErr != nil {
			t.Fatal(documentErr)
		}
		seed = append(seed, search.IndexDocument(document))
	}
	seedResult, err := client.Bulk(t.Context(), search.BulkRequest{Operations: seed, Refresh: search.RefreshWaitFor})
	if err != nil || seedResult.Partial() {
		t.Fatalf("load seed = %#v/%v", seedResult.Items(), err)
	}
	deadline := time.Now().Add(duration)
	const workers = 8
	const minimumRequestsPerWorker = 32
	const boundedDocumentIDsPerWorker = 256
	errors := make(chan error, workers)
	var cycles atomic.Uint64
	var peakHeap atomic.Uint64
	var maximumCycleLatency atomic.Int64
	runtime.GC()
	var baselineMemory runtime.MemStats
	runtime.ReadMemStats(&baselineMemory)
	peakHeap.Store(baselineMemory.HeapAlloc)
	baselineUsage := integrationProcessUsage(t)
	loadStarted := time.Now()
	var wait sync.WaitGroup
	for worker := range workers {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			lower, _ := search.NumberValue(strconv.Itoa(worker * (seedDocuments / workers)))
			upper, _ := search.NumberValue(strconv.Itoa((worker + 1) * (seedDocuments / workers)))
			for requestNumber := 0; requestNumber < minimumRequestsPerWorker || duration > 0 && time.Now().Before(deadline); requestNumber++ {
				cycleStarted := time.Now()
				result, searchErr := client.Search(t.Context(), search.Request{
					Tenant: tenant, Index: logicalIndex,
					Query: search.BoolQuery{Filter: []search.Query{
						search.RangeQuery{Field: "sequence", GTE: &lower, LT: &upper},
						search.ExistsQuery{Field: "cardinality"},
					}},
					Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
					Page: search.OffsetPage{Size: 16},
					Aggregations: map[string]search.Aggregation{
						"high_cardinality": search.TermsAggregation{Field: "cardinality", Size: 64},
					},
				})
				if searchErr != nil || len(result.Hits()) == 0 || len(result.Aggregations()) != 1 {
					errors <- fmt.Errorf("bounded search worker %d = %d hits/%v", worker, len(result.Hits()), searchErr)
					return
				}
				document, documentErr := search.NewDocument(tenant, logicalIndex,
					fmt.Sprintf("worker-%02d-%04d", worker, requestNumber%boundedDocumentIDsPerWorker), uint64(requestNumber/boundedDocumentIDsPerWorker+1),
					json.RawMessage(fmt.Sprintf(`{"sequence":%d,"cardinality":"worker-%02d-%04d","payload":"load"}`, worker, worker, requestNumber)), limits)
				if documentErr != nil {
					errors <- documentErr
					return
				}
				bulk, bulkErr := client.Bulk(t.Context(), search.BulkRequest{
					Operations: []search.WriteOperation{search.IndexDocument(document)}, Refresh: search.RefreshNone,
				})
				if bulkErr != nil || bulk.Partial() {
					errors <- fmt.Errorf("bounded bulk worker %d = %#v/%v", worker, bulk.Items(), bulkErr)
					return
				}
				cycles.Add(1)
				updateAtomicMaximumInt64(&maximumCycleLatency, int64(time.Since(cycleStarted)))
				var memory runtime.MemStats
				runtime.ReadMemStats(&memory)
				updateAtomicMaximumUint64(&peakHeap, memory.HeapAlloc)
			}
		}(worker)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	elapsed := time.Since(loadStarted)
	finalUsage := integrationProcessUsage(t)
	clientCPU := finalUsage.cpu - baselineUsage.cpu
	cycleCount := cycles.Load()
	if duration > 0 && elapsed < duration {
		t.Fatalf("bounded load elapsed = %s, want at least configured %s", elapsed, duration)
	}
	if cycleCount < workers*minimumRequestsPerWorker {
		t.Fatalf("bounded load cycles = %d, want at least %d", cycleCount, workers*minimumRequestsPerWorker)
	}
	if maximum := time.Duration(maximumCycleLatency.Load()); maximum > 10*time.Second {
		t.Fatalf("bounded load maximum search+write cycle latency = %s, want at most 10s", maximum)
	}
	if clientCPU < 0 || clientCPU > elapsed*4+2*time.Second {
		t.Fatalf("bounded load client CPU = %s over %s wall time, want at most four cores plus 2s", clientCPU, elapsed)
	}
	if peak := peakHeap.Load(); peak > baselineMemory.HeapAlloc+64<<20 {
		t.Fatalf("bounded load peak Go heap = %d bytes, baseline %d, want growth at most %d", peak, baselineMemory.HeapAlloc, 64<<20)
	}
	if finalUsage.maximumRSS > 1<<30 {
		t.Fatalf("bounded load process peak RSS = %d bytes, want at most %d", finalUsage.maximumRSS, 1<<30)
	}
	networkBytes := network.requestBytes.Load() + network.responseBytes.Load()
	networkBudget := int64(32<<20) + int64(cycleCount)*(512<<10)
	if networkBytes <= 0 || networkBytes > networkBudget {
		t.Fatalf("bounded load application network bytes = %d, want within 1..%d", networkBytes, networkBudget)
	}
	info, err := client.Info(t.Context())
	if err != nil || info.Version != expectedVersion {
		t.Fatalf("bounded load Info() = %#v/%v", info, err)
	}
	waitForIntegrationHealth(t, client)
	capacity, err := client.Capacity(t.Context())
	if err != nil || capacity.Documents < seedDocuments || capacity.HeapMaxBytes == 0 || capacity.DiskAvailableBytes == 0 {
		t.Fatalf("bounded load Capacity() = %#v/%v", capacity, err)
	}
}

func waitForIntegrationHealth(t *testing.T, client *adapter.Client) {
	t.Helper()

	deadline := time.Now().Add(realOpenSearchHealthConvergenceTimeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		health, err := client.Health(t.Context())
		if err != nil {
			t.Fatalf("bounded load Health() = %#v/%v", health, err)
		}
		if health.Ready {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"bounded load health did not become ready within %s: %#v",
				realOpenSearchHealthConvergenceTimeout,
				health,
			)
		}
		select {
		case <-ticker.C:
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
	}
}

type integrationCountingTransport struct {
	base                        http.RoundTripper
	requestBytes, responseBytes atomic.Int64
}

func (transport *integrationCountingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.ContentLength > 0 {
		transport.requestBytes.Add(request.ContentLength)
	}
	response, err := transport.base.RoundTrip(request)
	if err == nil && response != nil && response.Body != nil {
		response.Body = &integrationCountingBody{ReadCloser: response.Body, bytes: &transport.responseBytes}
	}
	return response, err
}

type integrationCountingBody struct {
	io.ReadCloser
	bytes *atomic.Int64
}

func (body *integrationCountingBody) Read(buffer []byte) (int, error) {
	count, err := body.ReadCloser.Read(buffer)
	body.bytes.Add(int64(count))
	return count, err
}

type integrationResourceUsage struct {
	cpu        time.Duration
	maximumRSS uint64
}

func integrationProcessUsage(t *testing.T) integrationResourceUsage {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatal(err)
	}
	cpu := time.Duration(usage.Utime.Sec+usage.Stime.Sec)*time.Second +
		time.Duration(usage.Utime.Usec+usage.Stime.Usec)*time.Microsecond
	maximumRSS := uint64(usage.Maxrss)
	if runtime.GOOS != "darwin" {
		maximumRSS *= 1024
	}
	return integrationResourceUsage{cpu: cpu, maximumRSS: maximumRSS}
}

func updateAtomicMaximumInt64(value *atomic.Int64, candidate int64) {
	for current := value.Load(); candidate > current && !value.CompareAndSwap(current, candidate); current = value.Load() {
	}
}

func updateAtomicMaximumUint64(value *atomic.Uint64, candidate uint64) {
	for current := value.Load(); candidate > current && !value.CompareAndSwap(current, candidate); current = value.Load() {
	}
}

func TestRealOpenSearchConformance(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_URL")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	if endpoint == "" || expectedVersion == "" {
		t.Skip("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are not configured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatal("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are required for a disposable test cluster")
	}
	physical := fmt.Sprintf("golib-search-%d", time.Now().UnixNano())
	alias := physical + "-alias"
	limits := search.DefaultLimits()
	codec, err := search.NewCursorCodec([]byte("integration-cursor-key-32-bytes!!"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	discover := false
	direct, err := official.NewClient(official.Config{
		Addresses: []string{endpoint}, DisableRetry: true, DiscoverNodesOnStart: &discover,
		HealthCheckMaxRetries: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = direct.Close() })
	verifier := &realLifecycleVerifier{client: direct, pageSize: 128, maximumRecords: 4_096, maximumResponseBytes: 16 << 20}
	var currentPhysical atomic.Value
	currentPhysical.Store(physical)
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Search: &adapter.SearchConfig{Limits: limits, CursorCodec: codec, Clock: time.Now, Authorizer: allowSearchAuthorization(), WriteGuard: allowWriteAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
				return adapter.IndexTarget{Name: alias, PhysicalName: currentPhysical.Load().(string), Fingerprint: "integration-v1"}, nil
			}),
		},
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }),
			Verifier:   verifier, MutationGuard: allowLifecycleMutationGuard(),
			CutoverGuard: adapter.LifecycleCutoverGuardFunc(func(_ context.Context, request adapter.LifecycleCutoverRequest, operation func() error) error {
				if err := operation(); err != nil {
					return err
				}
				currentPhysical.Store(request.Target)
				return nil
			}),
			ReindexCursorCodec: mustIntegrationReindexCursorCodec(t),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	info, err := client.Info(t.Context())
	if err != nil || info.Version != expectedVersion {
		t.Fatalf("Info() = %#v/%v, expected %s", info, err, expectedVersion)
	}

	definition, err := search.NewIndexDefinition(physical,
		json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0}`),
		json.RawMessage(`{"dynamic":"strict","properties":{"name":{"type":"keyword"}}}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	verifier.expectedDefinitions = map[string]search.IndexDefinition{definition.Fingerprint(): definition}
	if err := client.CreateIndex(t.Context(), "integration", definition); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = deleteDisposableIndex(context.Background(), endpoint, physical) })
	if err := client.AddAlias(t.Context(), "integration", alias, physical, true); err != nil {
		t.Fatal(err)
	}

	a, _ := search.NewDocument("integration", "documents", "a", 1, json.RawMessage(`{"name":"alpha"}`), limits)
	b, _ := search.NewDocument("integration", "documents", "b", 1, json.RawMessage(`{"name":"beta"}`), limits)
	if outcome, err := client.Write(t.Context(), search.IndexDocument(a), search.RefreshWaitFor); err != nil || outcome.State != search.OutcomeApplied {
		t.Fatalf("Write() = %#v/%v", outcome, err)
	}
	bulk, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{search.IndexDocument(b), search.DeleteDocument("integration", "documents", "a", 2)}, Refresh: search.RefreshWaitFor})
	if err != nil || bulk.Partial() {
		t.Fatalf("Bulk() = %#v/%v", bulk.Items(), err)
	}
	invalid, _ := search.NewDocument("integration", "documents", "invalid", 1, json.RawMessage(`{"unexpected":"field"}`), limits)
	partial, err := client.Bulk(t.Context(), search.BulkRequest{Operations: []search.WriteOperation{search.IndexDocument(b), search.IndexDocument(invalid)}, Refresh: search.RefreshWaitFor})
	if err != nil || !partial.Partial() || partial.Items()[0].State != search.OutcomeVersionConflict || partial.Items()[1].State != search.OutcomeFailed {
		t.Fatalf("partial Bulk() = %#v/%v", partial.Items(), err)
	}

	result, err := client.Search(t.Context(), search.Request{Tenant: "integration", Index: "documents", Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.CursorPage{Size: 10, KeepAlive: time.Minute}})
	if err != nil || len(result.Hits()) != 1 || result.Hits()[0].ID != "b" || result.NextCursor() != "" {
		t.Fatalf("Search() = %#v/%v", result.Hits(), err)
	}

	directRequest, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "/"+alias+"/_search", bytes.NewBufferString(`{"query":{"match_all":{}},"sort":[{"_id":{"order":"asc"}}]}`))
	directRequest.Header.Set("Content-Type", "application/json")
	directResponse, err := direct.Stream(directRequest)
	if err != nil {
		t.Fatal(err)
	}
	directBody, readErr := io.ReadAll(directResponse.Body)
	closeErr := directResponse.Body.Close()
	var directResult struct {
		Hits struct {
			Hits []struct {
				ID string `json:"_id"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if readErr != nil || closeErr != nil || directResponse.StatusCode != http.StatusOK || json.Unmarshal(directBody, &directResult) != nil || len(directResult.Hits.Hits) != 1 || directResult.Hits.Hits[0].ID != result.Hits()[0].ID {
		t.Fatalf("direct official-client search diverged: status=%d body=%q read=%v close=%v", directResponse.StatusCode, directBody, readErr, closeErr)
	}

	cursorRequest := search.Request{Tenant: "integration", Index: "documents", Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.CursorPage{Size: 1, KeepAlive: time.Minute}}
	firstPage, err := client.Search(t.Context(), cursorRequest)
	if err != nil || firstPage.NextCursor() == "" {
		t.Fatalf("cursor Search() = %#v/%v", firstPage.Hits(), err)
	}
	queryFingerprint, err := search.RequestFingerprint(cursorRequest, limits)
	if err != nil {
		t.Fatal(err)
	}
	state, err := codec.Decode(firstPage.NextCursor(), search.CursorBinding{
		Tenant: "integration", Index: "documents",
		QueryFingerprint: queryFingerprint, IndexFingerprint: "integration-v1",
	}, limits)
	if err != nil {
		t.Fatal(err)
	}
	deleteBody, err := json.Marshal(map[string]string{"pit_id": state.PointInTime})
	if err != nil {
		t.Fatal(err)
	}
	deletePIT, _ := http.NewRequestWithContext(t.Context(), http.MethodDelete, "/_search/point_in_time", bytes.NewReader(deleteBody))
	deletePIT.Header.Set("Content-Type", "application/json")
	deleteResponse, err := direct.Stream(deletePIT)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr = io.Copy(io.Discard, deleteResponse.Body)
	closeErr = deleteResponse.Body.Close()
	if readErr != nil || closeErr != nil || deleteResponse.StatusCode != http.StatusOK {
		t.Fatalf("delete owned PIT: status=%d read=%v close=%v", deleteResponse.StatusCode, readErr, closeErr)
	}
	cursorRequest.Page = search.CursorPage{Size: 1, Cursor: firstPage.NextCursor(), KeepAlive: time.Minute}
	_, err = client.Search(t.Context(), cursorRequest)
	if !errors.Is(err, adapter.ErrPITExpired) {
		t.Fatalf("expired PIT error = %v", err)
	}

	target := physical + "-v2"
	targetDefinition, err := search.NewIndexDefinition(target,
		json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0}`),
		json.RawMessage(`{"dynamic":"strict","properties":{"name":{"type":"keyword"}}}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	verifier.expectedDefinitions[targetDefinition.Fingerprint()] = targetDefinition
	if err := client.CreateIndex(t.Context(), "integration", targetDefinition); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = deleteDisposableIndex(context.Background(), endpoint, target) })
	cursor, done, err := client.Reindex(t.Context(), "integration", physical, target, "")
	if err != nil || done || cursor == "" {
		t.Fatalf("start Reindex() = %q/%t/%v", cursor, done, err)
	}
	for attempt := 0; attempt < 100 && !done; attempt++ {
		cursor, done, err = client.Reindex(t.Context(), "integration", physical, target, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if !done {
			time.Sleep(25 * time.Millisecond)
		}
	}
	if !done {
		t.Fatal("Reindex() did not complete within the bounded poll budget")
	}
	verification, err := client.VerifyIndex(t.Context(), "integration", physical, target, targetDefinition.Fingerprint())
	if err != nil || !verification.Verified || verification.Drift != 0 {
		t.Fatalf("VerifyIndex() = %#v/%v", verification, err)
	}
	cutover, err := client.CutoverAlias(t.Context(), "integration", alias, physical, target, targetDefinition.Fingerprint())
	if err != nil || !cutover.Verified {
		t.Fatalf("CutoverAlias() = %#v/%v", cutover, err)
	}
	resolved, err := client.ResolveAlias(t.Context(), "integration", alias)
	if err != nil || resolved != target {
		t.Fatalf("ResolveAlias() after cutover = %q/%v", resolved, err)
	}
	page, err := client.Read(t.Context(), "integration", "documents", "", 10)
	if err != nil || !page.Done || len(page.Records) != 1 || page.Records[0].ID != "b" {
		t.Fatalf("Read() after cutover = %#v/%v", page, err)
	}
	rollback, err := client.CutoverAlias(t.Context(), "integration", alias, target, physical, definition.Fingerprint())
	if err != nil || !rollback.Verified {
		t.Fatalf("rollback CutoverAlias() = %#v/%v", rollback, err)
	}
	resolved, err = client.ResolveAlias(t.Context(), "integration", alias)
	if err != nil || resolved != physical {
		t.Fatalf("ResolveAlias() after rollback = %q/%v", resolved, err)
	}
	healthDeadline := time.Now().Add(realOpenSearchHealthConvergenceTimeout)
	var health adapter.HealthReport
	for {
		health, err = client.Health(t.Context())
		if err != nil || health.Ready {
			break
		}
		if time.Now().After(healthDeadline) {
			t.Fatalf(
				"Health() did not become ready within %s: %#v",
				realOpenSearchHealthConvergenceTimeout,
				health,
			)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Health() = %#v/%v", health, err)
	}
	capacity, err := client.Capacity(t.Context())
	if err != nil || capacity.Nodes < 1 || capacity.Documents < 1 {
		t.Fatalf("Capacity() = %#v/%v", capacity, err)
	}
	if err := client.PutIndexTemplate(t.Context(), "integration", physical+"-template", []string{physical + "-*"}, 100, definition); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteIndexTemplate(t.Context(), "integration", physical+"-template"); err != nil {
		t.Fatal(err)
	}
}

func TestRealOpenSearchDurableGuardSurvivesDeleteVersionGC(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_URL")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	if endpoint == "" || expectedVersion == "" {
		t.Skip("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are required for a disposable test cluster")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatal("OPENSEARCH_URL is invalid")
	}

	limits := search.DefaultLimits()
	tenant, logicalIndex := "write-guard-tenant", "documents"
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	physical, alias := "golib-search-write-guard-"+suffix, "golib-search-write-guard-alias-"+suffix
	definition, err := search.NewIndexDefinition(physical,
		json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0}`),
		json.RawMessage(`{"dynamic":"strict","properties":{"name":{"type":"keyword"}}}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	guardedV1, err := search.NewDocument(tenant, logicalIndex, "guarded", 1, json.RawMessage(`{"name":"guarded"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	store, err := newDurableAuthoritativeStore(t.TempDir(), tenant, logicalIndex, []search.Document{guardedV1}, limits)
	if err != nil {
		t.Fatal(err)
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		RequestTimeout: 30 * time.Second, MaximumResponseBytes: 16 << 20,
		Search: &adapter.SearchConfig{
			Limits: limits, CursorCodec: mustIntegrationCursorCodec(t), Clock: time.Now,
			Authorizer: allowSearchAuthorization(), WriteGuard: store,
			Resolver: adapter.IndexResolverFunc(func(_ context.Context, gotTenant, gotIndex string, _ adapter.IndexAccess) (adapter.IndexTarget, error) {
				if gotTenant != tenant || gotIndex != logicalIndex {
					return adapter.IndexTarget{}, errors.New("write guard target denied")
				}
				return adapter.IndexTarget{Name: alias, PhysicalName: physical, Fingerprint: definition.Fingerprint()}, nil
			}),
		},
		Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(func(_ context.Context, gotTenant string, resources []string) error {
			if gotTenant != tenant {
				return errors.New("write guard lifecycle denied")
			}
			for _, resource := range resources {
				if resource != physical && resource != alias {
					return errors.New("write guard lifecycle denied")
				}
			}
			return nil
		}), MutationGuard: allowLifecycleMutationGuard()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = deleteDisposableIndex(ctx, endpoint, physical)
	})
	if info, infoErr := client.Info(t.Context()); infoErr != nil || info.Version != expectedVersion {
		t.Fatalf("Info() = %#v/%v, expected %s", info, infoErr, expectedVersion)
	}
	if err := client.CreateIndex(t.Context(), tenant, definition); err != nil {
		t.Fatal(err)
	}
	if err := client.AddAlias(t.Context(), tenant, alias, physical, true); err != nil {
		t.Fatal(err)
	}
	direct, err := official.NewClient(official.Config{Addresses: []string{endpoint}, DisableRetry: true, HealthCheckMaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = direct.Close() })
	requireDirectOpenSearchJSON(t, direct, http.MethodPut, "/"+physical+"/_settings",
		[]byte(`{"index":{"gc_deletes":"0s"}}`), http.StatusOK)

	if outcome, writeErr := client.Write(t.Context(), search.IndexDocument(guardedV1), search.RefreshWaitFor); writeErr != nil || outcome.State != search.OutcomeApplied {
		t.Fatalf("guarded initial Write() = %#v/%v", outcome, writeErr)
	}
	if err := store.Delete(t.Context(), guardedV1.ID, 3); err != nil {
		t.Fatal(err)
	}
	if outcome, deleteErr := client.Write(t.Context(), search.DeleteDocument(tenant, logicalIndex, guardedV1.ID, 3), search.RefreshWaitFor); deleteErr != nil || outcome.State != search.OutcomeApplied {
		t.Fatalf("guarded Delete() = %#v/%v", outcome, deleteErr)
	}

	directWrite := func(id string, version uint64, source []byte, wantStatus int) {
		t.Helper()
		path := "/" + physical + "/_doc/" + id + "?refresh=wait_for&version=" + strconv.FormatUint(version, 10) + "&version_type=external"
		requireDirectOpenSearchJSON(t, direct, http.MethodPut, path, source, wantStatus)
	}
	directDelete := func(id string, version uint64) {
		t.Helper()
		path := "/" + physical + "/_doc/" + id + "?refresh=wait_for&version=" + strconv.FormatUint(version, 10) + "&version_type=external"
		requireDirectOpenSearchJSON(t, direct, http.MethodDelete, path, nil, http.StatusOK)
	}
	directWrite("backend-only", 1, []byte(`{"name":"backend-only"}`), http.StatusCreated)
	directDelete("backend-only", 3)
	directWrite("backend-only", 2, []byte(`{"name":"resurrected"}`), http.StatusCreated)

	stale, err := search.NewDocument(tenant, logicalIndex, guardedV1.ID, 2, guardedV1.Source, limits)
	if err != nil {
		t.Fatal(err)
	}
	if outcome, writeErr := client.Write(t.Context(), search.IndexDocument(stale), search.RefreshWaitFor); !errors.Is(writeErr, adapter.ErrWriteDenied) || outcome != (search.ItemOutcome{}) {
		t.Fatalf("stale guarded Write() = %#v/%v, want pre-dispatch denial", outcome, writeErr)
	}
	_, status, err := directOpenSearchJSON(t.Context(), direct, http.MethodGet, "/"+physical+"/_doc/"+guardedV1.ID, nil)
	if err != nil || status != http.StatusNotFound {
		t.Fatalf("guarded document after stale replay = status %d error %v", status, err)
	}
}

type realLifecycleRecord struct {
	id, digest string
	version    uint64
}

type realLifecycleVerifier struct {
	client               *official.Client
	pageSize             int
	maximumRecords       uint64
	maximumResponseBytes int64
	expectedDefinitions  map[string]search.IndexDefinition
}

func TestRealLifecycleVerifierUsesOnePointInTimeForBothGenerations(t *testing.T) {
	t.Parallel()

	definition, err := search.NewIndexDefinition("target", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	created, deleted := 0, 0
	searched := map[string]int{}
	direct, err := official.NewClient(official.Config{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch {
			case request.Method == http.MethodGet && request.URL.Path == "/target/_mapping":
				return integrationHTTPResponse(http.StatusOK, `{"target":{"mappings":{}}}`), nil
			case request.Method == http.MethodGet && request.URL.Path == "/target/_settings":
				return integrationHTTPResponse(http.StatusOK, `{"target":{"settings":{"index":{"creation_date":"1","provided_name":"target","uuid":"uuid","version":{"created":"1"}}}}}`), nil
			case request.Method == http.MethodPost && request.URL.Path == "/source,target/_search/point_in_time":
				created++
				return integrationHTTPResponse(http.StatusOK, `{"pit_id":"shared-pit"}`), nil
			case request.Method == http.MethodPost && request.URL.Path == "/_search":
				var body struct {
					Query map[string]map[string]string `json:"query"`
					PIT   struct {
						ID string `json:"id"`
					} `json:"pit"`
				}
				if decodeErr := json.NewDecoder(request.Body).Decode(&body); decodeErr != nil {
					t.Fatal(decodeErr)
				}
				index := body.Query["term"]["_index"]
				if body.PIT.ID != "shared-pit" || index != "source" && index != "target" {
					t.Fatalf("verification search body = %#v", body)
				}
				searched[index]++
				return integrationHTTPResponse(http.StatusOK, `{"_shards":{"total":1,"successful":1,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`), nil
			case request.Method == http.MethodDelete && request.URL.Path == "/_search/point_in_time":
				deleted++
				return integrationHTTPResponse(http.StatusOK, `{"pits":[{"pit_id":"shared-pit","successful":true}]}`), nil
			default:
				t.Fatalf("unexpected lifecycle verification request: %s %s", request.Method, request.URL.String())
				return nil, errors.New("unexpected lifecycle verification request")
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	verifier := &realLifecycleVerifier{
		client: direct, pageSize: 10, maximumRecords: 100, maximumResponseBytes: 4096,
		expectedDefinitions: map[string]search.IndexDefinition{definition.Fingerprint(): definition},
	}
	result, err := verifier.Verify(t.Context(), adapter.LifecycleVerificationRequest{
		Tenant: "tenant", Source: "source", Target: "target",
		ExpectedTargetFingerprint: definition.Fingerprint(),
	})
	if err != nil || result.TargetFingerprint != definition.Fingerprint() || result.Drift != 0 {
		t.Fatalf("Verify() = %#v/%v", result, err)
	}
	if created != 1 || deleted != 1 || searched["source"] != 1 || searched["target"] != 1 {
		t.Fatalf("PIT lifecycle/searches = %d/%d/%v", created, deleted, searched)
	}
}

func TestRealLifecycleVerifierCleansRotatedPointInTimeAfterScanFailure(t *testing.T) {
	t.Parallel()

	definition, err := search.NewIndexDefinition("target", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	deleted := ""
	direct, err := official.NewClient(official.Config{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch {
			case request.Method == http.MethodGet && request.URL.Path == "/target/_mapping":
				return integrationHTTPResponse(http.StatusOK, `{"target":{"mappings":{}}}`), nil
			case request.Method == http.MethodGet && request.URL.Path == "/target/_settings":
				return integrationHTTPResponse(http.StatusOK, `{"target":{"settings":{"index":{"creation_date":"1","provided_name":"target","uuid":"uuid","version":{"created":"1"}}}}}`), nil
			case request.Method == http.MethodPost && request.URL.Path == "/source,target/_search/point_in_time":
				return integrationHTTPResponse(http.StatusOK, `{"pit_id":"initial-pit"}`), nil
			case request.Method == http.MethodPost && request.URL.Path == "/_search":
				return integrationHTTPResponse(http.StatusOK, `{"pit_id":"rotated-pit","_shards":{"total":1,"successful":1,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_id":"","_version":1,"_source":{},"sort":[""]}]}}`), nil
			case request.Method == http.MethodDelete && request.URL.Path == "/_search/point_in_time":
				var body struct {
					PITID string `json:"pit_id"`
				}
				if decodeErr := json.NewDecoder(request.Body).Decode(&body); decodeErr != nil {
					t.Fatal(decodeErr)
				}
				deleted = body.PITID
				return integrationHTTPResponse(http.StatusOK, `{"pits":[{"pit_id":"`+body.PITID+`","successful":true}]}`), nil
			default:
				t.Fatalf("unexpected lifecycle verification request: %s %s", request.Method, request.URL.String())
				return nil, errors.New("unexpected lifecycle verification request")
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	verifier := &realLifecycleVerifier{
		client: direct, pageSize: 10, maximumRecords: 100, maximumResponseBytes: 4096,
		expectedDefinitions: map[string]search.IndexDefinition{definition.Fingerprint(): definition},
	}
	_, err = verifier.Verify(t.Context(), adapter.LifecycleVerificationRequest{
		Tenant: "tenant", Source: "source", Target: "target", SourceCount: 1,
		ExpectedTargetFingerprint: definition.Fingerprint(),
	})
	if err == nil {
		t.Fatal("malformed verification hit was accepted")
	}
	if deleted != "rotated-pit" {
		t.Fatalf("deleted PIT = %q, want rotated-pit", deleted)
	}
}

// deleteDisposableIndex is deliberately test-only. Production lifecycle
// deletion must pass through CleanupIndex and its durable eligibility guard;
// disposable fixture teardown has no migration record to attest.
func deleteDisposableIndex(ctx context.Context, endpoint, index string) error {
	if endpoint == "" || index == "" || url.PathEscape(index) != index {
		return errors.New("invalid disposable OpenSearch index target")
	}
	discover := false
	client, err := official.NewClient(official.Config{
		Addresses: []string{endpoint}, DisableRetry: true, DiscoverNodesOnStart: &discover,
		HealthCheckMaxRetries: -1,
	})
	if err != nil {
		return errors.New("create disposable OpenSearch cleanup client")
	}
	defer func() { _ = client.Close() }()
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, "/"+index, nil)
	if err != nil {
		return errors.New("create disposable OpenSearch cleanup request")
	}
	response, err := client.Stream(request)
	if err != nil {
		return errors.New("dispatch disposable OpenSearch cleanup request")
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
	closeErr := response.Body.Close()
	var acknowledged struct {
		Acknowledged *bool `json:"acknowledged"`
	}
	if readErr != nil || closeErr != nil || len(body) > 4096 || response.StatusCode != http.StatusOK ||
		json.Unmarshal(body, &acknowledged) != nil || acknowledged.Acknowledged == nil || !*acknowledged.Acknowledged {
		return errors.New("disposable OpenSearch index cleanup was not acknowledged")
	}
	return nil
}

func integrationHTTPResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func (verifier *realLifecycleVerifier) Verify(ctx context.Context, request adapter.LifecycleVerificationRequest) (result adapter.LifecycleVerificationResult, err error) {
	if verifier == nil || verifier.client == nil || verifier.pageSize <= 0 || verifier.maximumRecords == 0 || verifier.maximumResponseBytes <= 0 ||
		request.SourceCount > verifier.maximumRecords || request.TargetCount > verifier.maximumRecords {
		return adapter.LifecycleVerificationResult{}, errors.New("integration lifecycle verification bound exceeded")
	}
	targetFingerprint, err := verifier.verifyTargetDefinition(ctx, request.Target, request.ExpectedTargetFingerprint)
	if err != nil {
		return adapter.LifecycleVerificationResult{}, err
	}
	pitID, err := verifier.createPointInTime(ctx, request.Source, request.Target)
	if err != nil {
		return adapter.LifecycleVerificationResult{}, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if cleanupErr := verifier.deletePointInTime(cleanupCtx, pitID); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()
	source, nextPIT, err := verifier.scan(ctx, request.Source, request.SourceCount, pitID)
	pitID = nextPIT
	if err != nil {
		return adapter.LifecycleVerificationResult{}, err
	}
	target, nextPIT, err := verifier.scan(ctx, request.Target, request.TargetCount, pitID)
	pitID = nextPIT
	if err != nil {
		return adapter.LifecycleVerificationResult{}, err
	}
	var drift uint64
	for left, right := 0, 0; left < len(source) || right < len(target); {
		switch {
		case right >= len(target) || left < len(source) && source[left].id < target[right].id:
			drift++
			left++
		case left >= len(source) || target[right].id < source[left].id:
			drift++
			right++
		default:
			if source[left].version != target[right].version || source[left].digest != target[right].digest {
				drift++
			}
			left++
			right++
		}
	}
	return adapter.LifecycleVerificationResult{TargetFingerprint: targetFingerprint, Drift: drift}, nil
}

func (verifier *realLifecycleVerifier) createPointInTime(ctx context.Context, source, target string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"/"+source+","+target+"/_search/point_in_time?keep_alive=1m", nil)
	if err != nil {
		return "", err
	}
	response, err := verifier.client.Stream(request)
	if err != nil {
		return "", errors.New("integration lifecycle PIT creation transport failed")
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, verifier.maximumResponseBytes+1))
	closeErr := response.Body.Close()
	var payload struct {
		PITID string `json:"pit_id"`
	}
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || int64(len(body)) > verifier.maximumResponseBytes ||
		decodeStrictIntegrationJSON(body, &payload) != nil || payload.PITID == "" {
		return "", errors.New("integration lifecycle PIT creation response rejected")
	}
	return payload.PITID, nil
}

func (verifier *realLifecycleVerifier) deletePointInTime(ctx context.Context, pitID string) error {
	body, err := json.Marshal(map[string]string{"pit_id": pitID})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, "/_search/point_in_time", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := verifier.client.Stream(request)
	if err != nil {
		return errors.New("integration lifecycle PIT cleanup transport failed")
	}
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, verifier.maximumResponseBytes+1))
	closeErr := response.Body.Close()
	var payload struct {
		PITs []struct {
			PITID      string `json:"pit_id"`
			Successful bool   `json:"successful"`
		} `json:"pits"`
	}
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || int64(len(responseBody)) > verifier.maximumResponseBytes ||
		decodeStrictIntegrationJSON(responseBody, &payload) != nil || len(payload.PITs) != 1 ||
		payload.PITs[0].PITID != pitID || !payload.PITs[0].Successful {
		return errors.New("integration lifecycle PIT cleanup response rejected")
	}
	return nil
}

func (verifier *realLifecycleVerifier) verifyTargetDefinition(ctx context.Context, target, expectedFingerprint string) (string, error) {
	expected, exists := verifier.expectedDefinitions[expectedFingerprint]
	if !exists || expected.Fingerprint() != expectedFingerprint {
		return "", errors.New("integration lifecycle target definition unavailable")
	}
	mappingBody, err := verifier.readJSON(ctx, "/"+target+"/_mapping")
	if err != nil {
		return "", err
	}
	var mappingPayload map[string]struct {
		Mappings json.RawMessage `json:"mappings"`
	}
	if decodeStrictIntegrationJSON(mappingBody, &mappingPayload) != nil || len(mappingPayload) != 1 || len(mappingPayload[target].Mappings) == 0 {
		return "", errors.New("integration lifecycle mapping response malformed")
	}
	liveMappingDigest, err := canonicalIntegrationSourceDigest(mappingPayload[target].Mappings)
	if err != nil {
		return "", errors.New("integration lifecycle mapping response malformed")
	}
	expectedMappingDigest, err := canonicalIntegrationSourceDigest(expected.Mappings())
	if err != nil || liveMappingDigest != expectedMappingDigest {
		return "", errors.New("integration lifecycle target mapping drift")
	}

	settingsBody, err := verifier.readJSON(ctx, "/"+target+"/_settings?flat_settings=false&include_defaults=false")
	if err != nil {
		return "", err
	}
	var settingsPayload map[string]struct {
		Settings map[string]any `json:"settings"`
	}
	if decodeStrictIntegrationJSON(settingsBody, &settingsPayload) != nil || len(settingsPayload) != 1 {
		return "", errors.New("integration lifecycle settings response malformed")
	}
	liveSettings, ok := settingsPayload[target].Settings["index"].(map[string]any)
	if !ok {
		return "", errors.New("integration lifecycle settings response malformed")
	}
	liveSettings = cloneIntegrationObject(liveSettings)
	for _, generated := range []string{"creation_date", "provided_name", "uuid", "version"} {
		delete(liveSettings, generated)
	}
	var expectedSettings map[string]any
	if decodeStrictIntegrationJSON(expected.Settings(), &expectedSettings) != nil {
		return "", errors.New("integration lifecycle expected settings malformed")
	}
	if nested, nestedOK := expectedSettings["index"].(map[string]any); nestedOK && len(expectedSettings) == 1 {
		expectedSettings = nested
	}
	removeKnownOpenSearchDefaults(liveSettings, expectedSettings)
	if !reflect.DeepEqual(normalizeIntegrationSettings(liveSettings), normalizeIntegrationSettings(expectedSettings)) {
		return "", errors.New("integration lifecycle target settings drift")
	}
	return expected.Fingerprint(), nil
}

func (verifier *realLifecycleVerifier) readJSON(ctx context.Context, target string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	response, err := verifier.client.Stream(request)
	if err != nil {
		return nil, errors.New("integration lifecycle definition transport failed")
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, verifier.maximumResponseBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || int64(len(body)) > verifier.maximumResponseBytes {
		return nil, errors.New("integration lifecycle definition response rejected")
	}
	return body, nil
}

func decodeStrictIntegrationJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("integration JSON has trailing data")
	}
	return nil
}

func cloneIntegrationObject(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func removeKnownOpenSearchDefaults(live, expected map[string]any) {
	if _, configured := expected["replication"]; !configured {
		if replication, ok := live["replication"].(map[string]any); ok && len(replication) == 1 && replication["type"] == "DOCUMENT" {
			delete(live, "replication")
		}
	}
}

func normalizeIntegrationSettings(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, nested := range typed {
			normalized[key] = normalizeIntegrationSettings(nested)
		}
		return normalized
	case []any:
		normalized := make([]any, len(typed))
		for index, nested := range typed {
			normalized[index] = normalizeIntegrationSettings(nested)
		}
		return normalized
	case json.Number:
		return typed.String()
	case bool:
		return strconv.FormatBool(typed)
	default:
		return typed
	}
}

func (verifier *realLifecycleVerifier) scan(ctx context.Context, index string, expected uint64, pitID string) ([]realLifecycleRecord, string, error) {
	records := make([]realLifecycleRecord, 0, int(expected))
	after := ""
	maximumPages := int(verifier.maximumRecords)/verifier.pageSize + 2
	for page := 0; page < maximumPages; page++ {
		requestBody := map[string]any{
			"query": map[string]any{"term": map[string]any{"_index": index}}, "size": verifier.pageSize,
			"sort":             []any{map[string]any{"_id": map[string]any{"order": "asc"}}},
			"track_total_hits": true, "version": true,
			"pit": map[string]any{"id": pitID, "keep_alive": "1m"},
		}
		if after != "" {
			requestBody["search_after"] = []string{after}
		}
		body, err := json.Marshal(requestBody)
		if err != nil {
			return nil, pitID, err
		}
		httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, "/_search", bytes.NewReader(body))
		if err != nil {
			return nil, pitID, err
		}
		httpRequest.Header.Set("Content-Type", "application/json")
		response, err := verifier.client.Stream(httpRequest)
		if err != nil {
			return nil, pitID, errors.New("integration lifecycle verification transport failed")
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, verifier.maximumResponseBytes+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || int64(len(responseBody)) > verifier.maximumResponseBytes {
			return nil, pitID, errors.New("integration lifecycle verification response rejected")
		}
		var payload struct {
			PITID  string                                  `json:"pit_id"`
			Shards struct{ Total, Successful, Failed int } `json:"_shards"`
			Hits   struct {
				Total struct {
					Value    uint64 `json:"value"`
					Relation string `json:"relation"`
				} `json:"total"`
				Hits []struct {
					ID      string            `json:"_id"`
					Version uint64            `json:"_version"`
					Source  json.RawMessage   `json:"_source"`
					Sort    []json.RawMessage `json:"sort"`
				} `json:"hits"`
			} `json:"hits"`
		}
		decoder := json.NewDecoder(bytes.NewReader(responseBody))
		if decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			return nil, pitID, errors.New("integration lifecycle verification response malformed")
		}
		if payload.PITID != "" {
			pitID = payload.PITID
		}
		if payload.Shards.Total <= 0 ||
			payload.Shards.Failed != 0 || payload.Shards.Successful != payload.Shards.Total ||
			payload.Hits.Total.Relation != "eq" || payload.Hits.Total.Value != expected || len(payload.Hits.Hits) > verifier.pageSize {
			return nil, pitID, errors.New("integration lifecycle verification response malformed")
		}
		for _, hit := range payload.Hits.Hits {
			if hit.ID == "" || hit.Version == 0 || len(hit.Sort) != 1 {
				return nil, pitID, errors.New("integration lifecycle verification hit malformed")
			}
			var sortedID string
			if json.Unmarshal(hit.Sort[0], &sortedID) != nil || sortedID != hit.ID || after != "" && sortedID <= after {
				return nil, pitID, errors.New("integration lifecycle verification order malformed")
			}
			digest, err := canonicalIntegrationSourceDigest(hit.Source)
			if err != nil {
				return nil, pitID, err
			}
			records = append(records, realLifecycleRecord{id: hit.ID, version: hit.Version, digest: digest})
			after = sortedID
		}
		if uint64(len(records)) > expected || uint64(len(records)) > verifier.maximumRecords {
			return nil, pitID, errors.New("integration lifecycle verification record bound exceeded")
		}
		if len(payload.Hits.Hits) < verifier.pageSize {
			if uint64(len(records)) != expected {
				return nil, pitID, errors.New("integration lifecycle verification count changed")
			}
			return records, pitID, nil
		}
	}
	return nil, pitID, errors.New("integration lifecycle verification page bound exceeded")
}

func canonicalIntegrationSourceDigest(source json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	var value map[string]any
	if decoder.Decode(&value) != nil || value == nil || decoder.Decode(&struct{}{}) != io.EOF {
		return "", errors.New("integration lifecycle verification source malformed")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return search.SourceDigest(canonical), nil
}
