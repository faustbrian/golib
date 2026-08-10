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
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
	"github.com/faustbrian/golib/pkg/search/searchtest"
	official "github.com/opensearch-project/opensearch-go/v4"
)

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
		tenantA: {Name: aliasA, Fingerprint: definitionA.Fingerprint()},
		tenantB: {Name: aliasB, Fingerprint: definitionB.Fingerprint()},
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Search: &adapter.SearchConfig{
			Limits: limits, CursorCodec: mustIntegrationCursorCodec(t), Clock: time.Now,
			Resolver: adapter.IndexResolverFunc(func(_ context.Context, tenant, index string, _ adapter.IndexAccess) (adapter.IndexTarget, error) {
				target, exists := targets[tenant]
				if !exists || index != logicalIndex {
					return adapter.IndexTarget{}, errors.New("conformance target denied")
				}
				return target, nil
			}),
		},
		Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(func(_ context.Context, tenant string, _ []string) error {
			if tenant != tenantA && tenant != tenantB {
				return errors.New("conformance lifecycle denied")
			}
			return nil
		})},
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
		tenant, physical := fixture.tenant, fixture.physical
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = client.DeleteIndex(ctx, tenant, physical)
		})
		if err := client.AddAlias(t.Context(), fixture.tenant, fixture.alias, fixture.physical, true); err != nil {
			t.Fatal(err)
		}
	}

	if err := searchtest.RunConformance(t.Context(), searchtest.ConformanceConfig{
		Adapter: client, Limits: limits,
		TenantA: tenantA, TenantB: tenantB, LogicalIndex: logicalIndex,
		Refresh: search.RefreshWaitFor,
	}); err != nil {
		t.Fatal(err)
	}
}

func mustIntegrationCursorCodec(t *testing.T) *search.CursorCodec {
	t.Helper()
	codec, err := search.NewCursorCodec([]byte("integration-cursor-key-32-bytes!!"), time.Now, 4096)
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
	client := integrationClient(t, []string{endpoint})
	deadline := time.Now().Add(duration)
	const workers = 8
	const minimumRequestsPerWorker = 32
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for requests := 0; requests < minimumRequestsPerWorker || duration > 0 && time.Now().Before(deadline); requests++ {
				info, err := client.Info(t.Context())
				if err != nil {
					errors <- err
					return
				}
				if info.Version != expectedVersion {
					errors <- fmt.Errorf("version %q does not match %q", info.Version, expectedVersion)
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
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
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Search: &adapter.SearchConfig{Limits: limits, CursorCodec: codec, Clock: time.Now,
			Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
				return adapter.IndexTarget{Name: alias, Fingerprint: "integration-v1"}, nil
			}),
		},
		Lifecycle: &adapter.LifecycleConfig{Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil })},
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
	if err := client.CreateIndex(t.Context(), "integration", definition); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.DeleteIndex(context.Background(), "integration", physical) })
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

	result, err := client.Search(t.Context(), search.Request{Tenant: "integration", Index: "documents", Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}}, Page: search.CursorPage{Size: 10, KeepAlive: time.Minute}})
	if err != nil || len(result.Hits()) != 1 || result.Hits()[0].ID != "b" || result.NextCursor() != "" {
		t.Fatalf("Search() = %#v/%v", result.Hits(), err)
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

	firstPage, err := client.Search(t.Context(), search.Request{Tenant: "integration", Index: "documents", Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}}, Page: search.CursorPage{Size: 1, KeepAlive: time.Minute}})
	if err != nil || firstPage.NextCursor() == "" {
		t.Fatalf("cursor Search() = %#v/%v", firstPage.Hits(), err)
	}
	deletePIT, _ := http.NewRequestWithContext(t.Context(), http.MethodDelete, "/_search/point_in_time/_all", nil)
	deleteResponse, err := direct.Stream(deletePIT)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr = io.Copy(io.Discard, deleteResponse.Body)
	closeErr = deleteResponse.Body.Close()
	if readErr != nil || closeErr != nil || deleteResponse.StatusCode != http.StatusOK {
		t.Fatalf("delete all PITs: status=%d read=%v close=%v", deleteResponse.StatusCode, readErr, closeErr)
	}
	_, err = client.Search(t.Context(), search.Request{Tenant: "integration", Index: "documents", Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}}, Page: search.CursorPage{Size: 1, Cursor: firstPage.NextCursor(), KeepAlive: time.Minute}})
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
	if err := client.CreateIndex(t.Context(), "integration", targetDefinition); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.DeleteIndex(context.Background(), "integration", target) })
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
	verification, err := client.VerifyIndex(t.Context(), "integration", physical, target)
	if err != nil || !verification.Verified || verification.Drift != 0 {
		t.Fatalf("VerifyIndex() = %#v/%v", verification, err)
	}
	if err := client.SwapAlias(t.Context(), "integration", alias, physical, target); err != nil {
		t.Fatal(err)
	}
	resolved, err := client.ResolveAlias(t.Context(), "integration", alias)
	if err != nil || resolved != target {
		t.Fatalf("ResolveAlias() after cutover = %q/%v", resolved, err)
	}
	page, err := client.Read(t.Context(), "integration", "documents", "", 10)
	if err != nil || !page.Done || len(page.Records) != 1 || page.Records[0].ID != "b" {
		t.Fatalf("Read() after cutover = %#v/%v", page, err)
	}
	if err := client.SwapAlias(t.Context(), "integration", alias, target, physical); err != nil {
		t.Fatal(err)
	}
	resolved, err = client.ResolveAlias(t.Context(), "integration", alias)
	if err != nil || resolved != physical {
		t.Fatalf("ResolveAlias() after rollback = %q/%v", resolved, err)
	}
	healthDeadline := time.Now().Add(30 * time.Second)
	var health adapter.HealthReport
	for {
		health, err = client.Health(t.Context())
		if err != nil || health.Ready {
			break
		}
		if time.Now().After(healthDeadline) {
			t.Fatalf("Health() did not become ready within 30s: %#v", health)
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
