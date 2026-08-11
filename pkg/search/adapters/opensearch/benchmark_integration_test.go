//go:build integration

package opensearch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
	"github.com/faustbrian/golib/pkg/search/searchtest"
	official "github.com/opensearch-project/opensearch-go/v4"
)

const (
	benchmarkFixtureDocuments = 128
	benchmarkPageSize         = 16
	benchmarkVersionStride    = 1_000_000
)

var benchmarkVersionBase atomic.Uint64

type sharedBenchmarkAdapter interface {
	search.Indexer
	search.Searcher
}

func BenchmarkSharedSearchSemantics(b *testing.B) {
	limits := search.DefaultLimits()
	b.Run("fake", func(b *testing.B) {
		fake, err := searchtest.NewFake(limits)
		if err != nil {
			b.Fatal(err)
		}
		seedSharedBenchmarkFixture(b, fake, limits, "benchmark", "documents")
		runSharedSearchBenchmarks(b, "fake", fake, limits, "benchmark", "documents")
	})
	client, direct, tenant, logicalIndex, directTarget, physicalTarget, version := realBenchmarkAdapter(b, limits)
	seedSharedBenchmarkFixture(b, client, limits, tenant, logicalIndex)
	b.Run("opensearch-adapter", func(b *testing.B) {
		runSharedSearchBenchmarks(b, "opensearch-"+version, client, limits, tenant, logicalIndex)
	})
	b.Run("direct-official-client", func(b *testing.B) {
		runDirectOfficialBenchmarks(b, direct, directTarget, physicalTarget, version)
	})
}

func runSharedSearchBenchmarks(b *testing.B, backendName string, backend sharedBenchmarkAdapter, limits search.Limits, tenant, index string) {
	b.Helper()
	verifySharedBenchmarkFixture(b, backend, tenant, index)
	if backendName == "fake" {
		b.Logf("comparability: backend=fake environment=in-memory fixtures=%d field-contract=bucket-keyword refresh-request=wait_for visibility=immediate mapping-enforcement=unsupported cursor-pit=unsupported sort=document-id", benchmarkFixtureDocuments)
	} else {
		b.Logf("comparability: backend=%s environment=real-network fixtures=%d field-contract=bucket-keyword refresh-request=wait_for visibility=backend-acknowledged mapping-enforcement=strict cursor-pit=supported sort=document-id", backendName, benchmarkFixtureDocuments)
	}

	b.Run("indexing", func(b *testing.B) {
		document, documentErr := search.NewDocument(tenant, index, "indexed-document", 1,
			json.RawMessage(`{"bucket":"indexed"}`), limits)
		if documentErr != nil {
			b.Fatal(documentErr)
		}
		operation := search.IndexDocument(document)
		version := nextBenchmarkVersionBase()
		b.ReportAllocs()
		for b.Loop() {
			operation.Version = version
			outcome, writeErr := backend.Write(b.Context(), operation, search.RefreshWaitFor)
			if writeErr != nil || outcome.Position != 0 || outcome.ID != operation.ID || outcome.Action != operation.Action ||
				outcome.State != search.OutcomeApplied || outcome.Version != operation.Version {
				b.Fatalf("Write() = %#v/%v", outcome, writeErr)
			}
			version++
		}
	})

	query := search.Request{
		Tenant: tenant, Index: index,
		Query: search.TermQuery{Field: "bucket", Value: search.StringValue("fixture")},
		Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page:  search.OffsetPage{Size: 32},
	}
	b.Run("query", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(32, "hits/op")
		for b.Loop() {
			result, searchErr := backend.Search(b.Context(), query)
			if searchErr != nil || result.Total() != (search.Total{Value: benchmarkFixtureDocuments, Relation: search.TotalExact}) ||
				result.Diagnostics().TimedOut || result.Diagnostics().Partial || result.Diagnostics().Shards.Failed != 0 ||
				result.Diagnostics().Shards.Successful != result.Diagnostics().Shards.Total {
				b.Fatalf("Search() = hits %d, total %d, error %v", len(result.Hits()), result.Total().Value, searchErr)
			}
			verifySharedHits(b, result.Hits(), index, 0, 32)
		}
	})

	b.Run("bulk_indexing", func(b *testing.B) {
		const bulkItems = 16
		bulk := make([]search.WriteOperation, bulkItems)
		for position := range bulkItems {
			document, documentErr := search.NewDocument(tenant, index, fmt.Sprintf("fixture-%03d", position), 2,
				json.RawMessage(`{"bucket":"fixture"}`), limits)
			if documentErr != nil {
				b.Fatal(documentErr)
			}
			bulk[position] = search.IndexDocument(document)
		}
		version := nextBenchmarkVersionBase()
		b.ReportAllocs()
		b.ReportMetric(bulkItems, "items/op")
		for b.Loop() {
			for position := range bulk {
				bulk[position].Version = version
			}
			request := search.BulkRequest{Operations: bulk, Refresh: search.RefreshWaitFor}
			result, bulkErr := backend.Bulk(b.Context(), request)
			if bulkErr != nil || result.Partial() || result.ValidateRequest(request) != nil || len(result.Items()) != len(bulk) {
				b.Fatalf("Bulk() = partial %t, error %v", result.Partial(), bulkErr)
			}
			version++
		}
	})

	b.Run("pagination", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(benchmarkFixtureDocuments, "hits/op")
		b.ReportMetric(benchmarkFixtureDocuments/benchmarkPageSize, "pages/op")
		for b.Loop() {
			seen := 0
			for offset := 0; offset < benchmarkFixtureDocuments; offset += benchmarkPageSize {
				request := query
				request.Page = search.OffsetPage{Size: benchmarkPageSize, Offset: offset}
				result, searchErr := backend.Search(b.Context(), request)
				if searchErr != nil {
					b.Fatal(searchErr)
				}
				verifySharedHits(b, result.Hits(), index, offset, benchmarkPageSize)
				seen += len(result.Hits())
			}
			if seen != benchmarkFixtureDocuments {
				b.Fatalf("pagination observed %d hits, want %d", seen, benchmarkFixtureDocuments)
			}
		}
	})

	b.Run("cursor_pagination", func(b *testing.B) {
		capabilities, capabilityErr := backend.Capabilities(b.Context())
		if capabilityErr != nil {
			b.Fatal(capabilityErr)
		}
		if !capabilities.Cursor || !capabilities.PointInTime {
			b.Skip("not comparable: backend declares cursor and point-in-time semantics unsupported")
		}
		b.ReportAllocs()
		b.ReportMetric(benchmarkFixtureDocuments, "hits/op")
		b.ReportMetric(benchmarkFixtureDocuments/benchmarkPageSize+1, "pages/op")
		for b.Loop() {
			cursor := ""
			seen := 0
			for {
				result, searchErr := backend.Search(b.Context(), search.Request{
					Tenant: tenant, Index: index,
					Query: search.TermQuery{Field: "bucket", Value: search.StringValue("fixture")},
					Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
					Page:  search.CursorPage{Size: benchmarkPageSize, Cursor: cursor, KeepAlive: time.Minute},
				})
				if searchErr != nil {
					b.Fatal(searchErr)
				}
				want := min(benchmarkPageSize, benchmarkFixtureDocuments-seen)
				verifySharedHits(b, result.Hits(), index, seen, want)
				seen += len(result.Hits())
				cursor = result.NextCursor()
				if cursor == "" {
					break
				}
			}
			if seen != benchmarkFixtureDocuments {
				b.Fatalf("cursor pagination observed %d hits, want %d", seen, benchmarkFixtureDocuments)
			}
		}
	})

}

func seedSharedBenchmarkFixture(b *testing.B, backend sharedBenchmarkAdapter, limits search.Limits, tenant, index string) {
	b.Helper()
	operations := make([]search.WriteOperation, benchmarkFixtureDocuments)
	for position := range benchmarkFixtureDocuments {
		document, err := search.NewDocument(tenant, index, fmt.Sprintf("fixture-%03d", position), 1,
			json.RawMessage(`{"bucket":"fixture"}`), limits)
		if err != nil {
			b.Fatal(err)
		}
		operations[position] = search.IndexDocument(document)
	}
	request := search.BulkRequest{Operations: operations, Refresh: search.RefreshWaitFor}
	result, err := backend.Bulk(b.Context(), request)
	if err != nil || result.Partial() || result.ValidateRequest(request) != nil || len(result.Items()) != len(operations) {
		b.Fatalf("fixture bulk = partial %t, error %v", result.Partial(), err)
	}
}

func nextBenchmarkVersionBase() uint64 {
	return benchmarkVersionBase.Add(benchmarkVersionStride)
}

func verifySharedBenchmarkFixture(b *testing.B, backend sharedBenchmarkAdapter, tenant, index string) {
	b.Helper()
	result, err := backend.Search(b.Context(), search.Request{
		Tenant: tenant, Index: index,
		Query: search.TermQuery{Field: "bucket", Value: search.StringValue("fixture")},
		Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page:  search.OffsetPage{Size: benchmarkFixtureDocuments},
	})
	if err != nil || len(result.Hits()) != benchmarkFixtureDocuments ||
		result.Total() != (search.Total{Value: benchmarkFixtureDocuments, Relation: search.TotalExact}) ||
		result.Diagnostics().TimedOut || result.Diagnostics().Partial || result.Diagnostics().Shards.Failed != 0 ||
		result.Diagnostics().Shards.Successful != result.Diagnostics().Shards.Total {
		b.Fatalf("fixture query = hits %d, total %d, error %v", len(result.Hits()), result.Total().Value, err)
	}
	verifySharedHits(b, result.Hits(), index, 0, benchmarkFixtureDocuments)
}

func verifySharedHits(b *testing.B, hits []search.Hit, logicalIndex string, start, count int) {
	b.Helper()
	if len(hits) != count {
		b.Fatalf("shared Search() hits = %d, want %d", len(hits), count)
	}
	for position, hit := range hits {
		expectedID := fmt.Sprintf("fixture-%03d", start+position)
		expectedSort, _ := json.Marshal(expectedID)
		if hit.ID != expectedID || hit.Index != logicalIndex || hit.Version == 0 || !bytes.Equal(hit.Source, []byte(`{"bucket":"fixture"}`)) ||
			len(hit.SortValues) != 1 || !bytes.Equal(hit.SortValues[0], expectedSort) {
			b.Fatalf("shared Search() hit %d = %#v, want logical index %q ID %q, nonzero version, exact source, and exact ID sort", position, hit, logicalIndex, expectedID)
		}
	}
}

func runDirectOfficialBenchmarks(b *testing.B, client *official.Client, index, physicalIndex, version string) {
	b.Helper()
	queryBody := []byte(`{"query":{"term":{"bucket":"fixture"}},"size":32,"from":0,"version":true,"sort":[{"_id":{"order":"asc"}}]}`)
	verifyDirectSearchResult(b, directOfficialRequest(b, client, http.MethodPost, "/"+index+"/_search", queryBody), physicalIndex, 0, 32)
	b.Logf("comparability: backend=direct-official-%s environment=real-network fixtures=%d field-contract=bucket-keyword refresh-request=wait_for visibility=backend-acknowledged mapping-enforcement=strict sort=document-id", version, benchmarkFixtureDocuments)

	b.Run("indexing", func(b *testing.B) {
		body := []byte(`{"bucket":"indexed"}`)
		documentVersion := nextBenchmarkVersionBase()
		b.ReportAllocs()
		for b.Loop() {
			path := fmt.Sprintf("/%s/_doc/direct-indexed-document?version=%d&version_type=external&refresh=wait_for&require_alias=true", index, documentVersion)
			verifyDirectWriteResult(b, directOfficialRequest(b, client, http.MethodPut, path, body), physicalIndex, "direct-indexed-document", documentVersion)
			documentVersion++
		}
	})

	b.Run("query", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(32, "hits/op")
		for b.Loop() {
			verifyDirectSearchResult(b, directOfficialRequest(b, client, http.MethodPost, "/"+index+"/_search", queryBody), physicalIndex, 0, 32)
		}
	})

	b.Run("bulk_indexing", func(b *testing.B) {
		const bulkItems = 16
		documentVersion := nextBenchmarkVersionBase()
		b.ReportAllocs()
		b.ReportMetric(bulkItems, "items/op")
		for b.Loop() {
			var body bytes.Buffer
			for position := range bulkItems {
				metadata, _ := json.Marshal(map[string]any{"index": map[string]any{
					"_index": index, "_id": fmt.Sprintf("fixture-%03d", position),
					"version": documentVersion, "version_type": "external", "require_alias": true,
				}})
				body.Write(metadata)
				body.WriteByte('\n')
				body.WriteString(`{"bucket":"fixture"}`)
				body.WriteByte('\n')
			}
			response := directOfficialRequestWithContentType(b, client, http.MethodPost, "/_bulk?refresh=wait_for", body.Bytes(), "application/x-ndjson")
			verifyDirectBulkResult(b, response, physicalIndex, bulkItems, documentVersion)
			documentVersion++
		}
	})

	b.Run("pagination", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(benchmarkFixtureDocuments, "hits/op")
		b.ReportMetric(benchmarkFixtureDocuments/benchmarkPageSize, "pages/op")
		for b.Loop() {
			seen := 0
			for offset := 0; offset < benchmarkFixtureDocuments; offset += benchmarkPageSize {
				body := []byte(fmt.Sprintf(`{"query":{"term":{"bucket":"fixture"}},"size":%d,"from":%d,"version":true,"sort":[{"_id":{"order":"asc"}}]}`, benchmarkPageSize, offset))
				response := directOfficialRequest(b, client, http.MethodPost, "/"+index+"/_search", body)
				seen += verifyDirectSearchResult(b, response, physicalIndex, offset, benchmarkPageSize)
			}
			if seen != benchmarkFixtureDocuments {
				b.Fatalf("direct pagination observed %d hits, want %d", seen, benchmarkFixtureDocuments)
			}
		}
	})

	b.Run("cursor_pagination", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(benchmarkFixtureDocuments, "hits/op")
		b.ReportMetric(benchmarkFixtureDocuments/benchmarkPageSize+1, "pages/op")
		for b.Loop() {
			created := directOfficialRequest(b, client, http.MethodPost, "/"+index+"/_search/point_in_time?keep_alive=1m", nil)
			var pit struct {
				ID string `json:"pit_id"`
			}
			if decodeDirectResponse(created, &pit) != nil || pit.ID == "" {
				b.Fatalf("direct PIT create = %q", created)
			}
			seen := 0
			var searchAfter []json.RawMessage
			for {
				requestBody := map[string]any{
					"query": map[string]any{"term": map[string]any{"bucket": "fixture"}},
					"size":  benchmarkPageSize, "version": true,
					"pit":  map[string]any{"id": pit.ID, "keep_alive": "60000ms"},
					"sort": []any{map[string]any{"_id": map[string]any{"order": "asc"}}},
				}
				if len(searchAfter) > 0 {
					requestBody["search_after"] = searchAfter
				}
				encoded, _ := json.Marshal(requestBody)
				response := directOfficialRequest(b, client, http.MethodPost, "/_search", encoded)
				var page struct {
					PITID    string `json:"pit_id"`
					TimedOut bool   `json:"timed_out"`
					Shards   struct {
						Total, Successful, Failed int
					} `json:"_shards"`
					Hits struct {
						Total struct {
							Value    uint64 `json:"value"`
							Relation string `json:"relation"`
						} `json:"total"`
						Hits []struct {
							Index   string            `json:"_index"`
							ID      string            `json:"_id"`
							Version uint64            `json:"_version"`
							Source  json.RawMessage   `json:"_source"`
							Sort    []json.RawMessage `json:"sort"`
						} `json:"hits"`
					} `json:"hits"`
				}
				if decodeDirectResponse(response, &page) != nil || page.TimedOut || page.Shards.Failed != 0 || page.Shards.Successful != page.Shards.Total ||
					page.Hits.Total.Value != benchmarkFixtureDocuments || page.Hits.Total.Relation != "eq" || len(page.Hits.Hits) > benchmarkPageSize {
					b.Fatalf("direct cursor page = total %d, hits %d", page.Hits.Total.Value, len(page.Hits.Hits))
				}
				if page.PITID != "" {
					pit.ID = page.PITID
				}
				for position, hit := range page.Hits.Hits {
					expectedID := fmt.Sprintf("fixture-%03d", seen+position)
					expectedSort, _ := json.Marshal(expectedID)
					if hit.Index != physicalIndex || hit.ID != expectedID || hit.Version == 0 || !bytes.Equal(hit.Source, []byte(`{"bucket":"fixture"}`)) ||
						len(hit.Sort) != 1 || !bytes.Equal(hit.Sort[0], expectedSort) {
						b.Fatalf("direct cursor hit %d = %#v, want physical index %q ID %q, nonzero version, exact source, and exact ID sort", seen+position, hit, physicalIndex, expectedID)
					}
				}
				seen += len(page.Hits.Hits)
				if len(page.Hits.Hits) < benchmarkPageSize {
					break
				}
				searchAfter = page.Hits.Hits[len(page.Hits.Hits)-1].Sort
			}
			deleteBody, _ := json.Marshal(map[string]string{"pit_id": pit.ID})
			verifyDirectPointInTimeDeletion(b, directOfficialRequest(b, client, http.MethodDelete, "/_search/point_in_time", deleteBody), pit.ID)
			if seen != benchmarkFixtureDocuments {
				b.Fatalf("direct cursor pagination observed %d hits, want %d", seen, benchmarkFixtureDocuments)
			}
		}
	})
}

func directOfficialRequest(b *testing.B, client *official.Client, method, path string, body []byte) []byte {
	b.Helper()
	return directOfficialRequestWithContentType(b, client, method, path, body, "application/json")
}

func directOfficialRequestWithContentType(b *testing.B, client *official.Client, method, path string, body []byte, contentType string) []byte {
	b.Helper()
	request, err := http.NewRequestWithContext(b.Context(), method, path, bytes.NewReader(body))
	if err != nil {
		b.Fatal(err)
	}
	request.Header.Set("Content-Type", contentType)
	response, err := client.Stream(request)
	if err != nil {
		b.Fatal(err)
	}
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || int64(len(responseBody)) > 16<<20 || response.StatusCode < 200 || response.StatusCode >= 300 {
		b.Fatalf("direct official request status=%d read=%v close=%v bytes=%d", response.StatusCode, readErr, closeErr, len(responseBody))
	}
	return responseBody
}

func verifyDirectSearchResult(b *testing.B, body []byte, expectedIndex string, start, expectedHits int) int {
	b.Helper()
	var result struct {
		TimedOut bool `json:"timed_out"`
		Shards   struct {
			Total, Successful, Failed int
		} `json:"_shards"`
		Hits struct {
			Total struct {
				Value    uint64 `json:"value"`
				Relation string `json:"relation"`
			} `json:"total"`
			Hits []struct {
				Index      string            `json:"_index"`
				ID         string            `json:"_id"`
				Version    uint64            `json:"_version"`
				Source     json.RawMessage   `json:"_source"`
				SortValues []json.RawMessage `json:"sort"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if decodeDirectResponse(body, &result) != nil || result.TimedOut || result.Shards.Failed != 0 || result.Shards.Successful != result.Shards.Total ||
		result.Hits.Total.Value != benchmarkFixtureDocuments || result.Hits.Total.Relation != "eq" {
		b.Fatalf("direct Search() total = %d, want %d", result.Hits.Total.Value, benchmarkFixtureDocuments)
	}
	if len(result.Hits.Hits) != expectedHits {
		b.Fatalf("direct Search() hits = %d, want %d", len(result.Hits.Hits), expectedHits)
	}
	for position, hit := range result.Hits.Hits {
		expectedID := fmt.Sprintf("fixture-%03d", start+position)
		expectedSort, _ := json.Marshal(expectedID)
		if hit.Index != expectedIndex || hit.ID != expectedID || hit.Version == 0 || !bytes.Equal(hit.Source, []byte(`{"bucket":"fixture"}`)) ||
			len(hit.SortValues) != 1 || !bytes.Equal(hit.SortValues[0], expectedSort) {
			b.Fatalf("direct Search() hit %d = %#v, want physical index %q ID %q, nonzero version, exact source, and exact ID sort", position, hit, expectedIndex, expectedID)
		}
	}
	return len(result.Hits.Hits)
}

func verifyDirectWriteResult(b *testing.B, body []byte, expectedIndex, expectedID string, expectedVersion uint64) {
	b.Helper()
	var result struct {
		Index   string `json:"_index"`
		ID      string `json:"_id"`
		Version uint64 `json:"_version"`
		Result  string `json:"result"`
	}
	if decodeDirectResponse(body, &result) != nil || result.Index != expectedIndex || result.ID != expectedID || result.Version != expectedVersion || result.Result != "created" && result.Result != "updated" {
		b.Fatalf("direct Write() = %#v, want ID %q version %d acknowledged create/update", result, expectedID, expectedVersion)
	}
}

func verifyDirectBulkResult(b *testing.B, body []byte, expectedIndex string, expectedItems int, expectedVersion uint64) {
	b.Helper()
	var result struct {
		Errors bool                         `json:"errors"`
		Items  []map[string]json.RawMessage `json:"items"`
	}
	if decodeDirectResponse(body, &result) != nil || result.Errors || len(result.Items) != expectedItems {
		b.Fatalf("direct Bulk() = errors %t, items %d", result.Errors, len(result.Items))
	}
	for position, envelope := range result.Items {
		raw, exists := envelope["index"]
		if !exists || len(envelope) != 1 {
			b.Fatalf("direct Bulk() item %d action envelope = %#v", position, envelope)
		}
		var item struct {
			Index   string `json:"_index"`
			ID      string `json:"_id"`
			Version uint64 `json:"_version"`
			Status  int    `json:"status"`
			Result  string `json:"result"`
		}
		expectedID := fmt.Sprintf("fixture-%03d", position)
		if decodeDirectResponse(raw, &item) != nil || item.Index != expectedIndex || item.ID != expectedID || item.Version != expectedVersion ||
			(item.Status != http.StatusOK && item.Status != http.StatusCreated) || item.Result != "created" && item.Result != "updated" {
			b.Fatalf("direct Bulk() item %d = %#v, want attributed version %d acknowledgement", position, item, expectedVersion)
		}
	}
}

func verifyDirectPointInTimeDeletion(b *testing.B, body []byte, expectedID string) {
	b.Helper()
	var result struct {
		PITs []struct {
			ID         string `json:"pit_id"`
			Successful bool   `json:"successful"`
		} `json:"pits"`
	}
	if decodeDirectResponse(body, &result) != nil || len(result.PITs) != 1 || result.PITs[0].ID != expectedID || !result.PITs[0].Successful {
		b.Fatalf("direct PIT deletion = %#v, want one acknowledged deletion for %q", result, expectedID)
	}
}

func decodeDirectResponse(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("direct official response has trailing data")
	}
	return nil
}

func realBenchmarkAdapter(b *testing.B, limits search.Limits) (*adapter.Client, *official.Client, string, string, string, string, string) {
	b.Helper()
	endpoint := os.Getenv("OPENSEARCH_URL")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	if endpoint == "" || expectedVersion == "" {
		b.Skip("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are not configured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		b.Fatal("OPENSEARCH_URL must name a disposable OpenSearch cluster")
	}
	discover := false
	direct, err := official.NewClient(official.Config{
		Addresses: []string{endpoint}, DisableRetry: true, DiscoverNodesOnStart: &discover,
		HealthCheckMaxRetries: -1,
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = direct.Close() })
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenant, logicalIndex := "benchmark", "documents"
	physical, alias := "golib-search-benchmark-"+suffix, "golib-search-benchmark-"+suffix+"-alias"
	definition, err := search.NewIndexDefinition(physical,
		json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0}`),
		json.RawMessage(`{"dynamic":"strict","properties":{"bucket":{"type":"keyword"}}}`), limits)
	if err != nil {
		b.Fatal(err)
	}
	codec, err := search.NewCursorCodec([]byte("benchmark-cursor-key-32-bytes!!!"), time.Now, 4096)
	if err != nil {
		b.Fatal(err)
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Search: &adapter.SearchConfig{
			Limits: limits, CursorCodec: codec, Clock: time.Now, Authorizer: allowSearchAuthorization(), WriteGuard: allowWriteAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(_ context.Context, resolvedTenant, resolvedIndex string, _ adapter.IndexAccess) (adapter.IndexTarget, error) {
				if resolvedTenant != tenant || resolvedIndex != logicalIndex {
					return adapter.IndexTarget{}, errors.New("benchmark target denied")
				}
				return adapter.IndexTarget{Name: alias, PhysicalName: physical, Fingerprint: definition.Fingerprint()}, nil
			}),
		},
		Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(func(_ context.Context, resolvedTenant string, _ []string) error {
			if resolvedTenant != tenant {
				return errors.New("benchmark lifecycle denied")
			}
			return nil
		}), MutationGuard: allowLifecycleMutationGuard()},
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = client.Close() })
	if info, infoErr := client.Info(b.Context()); infoErr != nil || info.Version != expectedVersion {
		b.Fatalf("Info() = %#v/%v, expected %s", info, infoErr, expectedVersion)
	}
	if err := client.CreateIndex(b.Context(), tenant, definition); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = deleteDisposableIndex(ctx, endpoint, physical)
	})
	if err := client.AddAlias(b.Context(), tenant, alias, physical, true); err != nil {
		b.Fatal(err)
	}
	return client, direct, tenant, logicalIndex, alias, physical, expectedVersion
}
