//go:build integration

package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
	official "github.com/opensearch-project/opensearch-go/v4"
)

func integrationEndpoints(t *testing.T) []string {
	t.Helper()
	value := os.Getenv("OPENSEARCH_URLS")
	if value == "" {
		t.Skip("OPENSEARCH_URLS is not configured")
	}
	endpoints := strings.Split(value, ",")
	if len(endpoints) < 2 || endpoints[0] == "" || endpoints[1] == "" {
		t.Fatal("OPENSEARCH_URLS must contain at least two comma-separated endpoints")
	}
	return endpoints
}

func integrationClient(t *testing.T, endpoints []string) *adapter.Client {
	t.Helper()
	client, err := adapter.New(adapter.Config{
		Endpoints: endpoints, AllowInsecureHTTP: true,
		RequestTimeout: 5 * time.Second, MaximumResponseBytes: 8 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestRealOpenSearchMultiNodeRotation(t *testing.T) {
	endpoints := integrationEndpoints(t)
	expectedNodes := strings.Split(os.Getenv("OPENSEARCH_EXPECTED_NODES"), ",")
	allowedVersions := strings.Split(os.Getenv("OPENSEARCH_ALLOWED_VERSIONS"), ",")
	if len(expectedNodes) != len(endpoints) || len(allowedVersions) == 0 || allowedVersions[0] == "" {
		t.Fatal("OPENSEARCH_EXPECTED_NODES and OPENSEARCH_ALLOWED_VERSIONS are required")
	}
	client := integrationClient(t, endpoints)
	clusters := make(map[string]struct{}, len(endpoints))
	nodes := make(map[string]struct{}, len(endpoints))
	versions := make(map[string]struct{}, len(allowedVersions))
	for _, version := range allowedVersions {
		versions[version] = struct{}{}
	}
	for range len(endpoints) * 2 {
		info, err := client.Info(t.Context())
		if err != nil {
			t.Fatal(info, err)
		}
		if _, allowed := versions[info.Version]; !allowed {
			t.Fatalf("node %q returned unsupported mixed version %q", info.Node, info.Version)
		}
		clusters[info.ClusterUUID] = struct{}{}
		nodes[info.Node] = struct{}{}
	}
	if len(clusters) != 1 {
		t.Fatalf("endpoints did not identify one cluster: %#v", clusters)
	}
	for _, expectedNode := range expectedNodes {
		if _, observed := nodes[expectedNode]; !observed {
			t.Fatalf("endpoint rotation did not reach %q: %#v", expectedNode, nodes)
		}
	}
	if len(nodes) != len(expectedNodes) {
		t.Fatalf("endpoint rotation reached unexpected nodes: %#v", nodes)
	}
}

func TestRealOpenSearchEndpointFailoverBudget(t *testing.T) {
	endpoints := integrationEndpoints(t)
	client := integrationClient(t, endpoints[:2])
	if _, err := client.Info(t.Context()); err == nil || !errors.Is(err, adapter.ErrTransport) {
		t.Fatalf("dead first endpoint did not produce one transport failure: %v", err)
	}
	if _, err := client.Info(context.Background()); err != nil {
		t.Fatalf("next operation did not advance to the live endpoint: %v", err)
	}
}

func TestRealOpenSearchCompleteOutageIsBounded(t *testing.T) {
	endpoints := integrationEndpoints(t)
	outageAlias := os.Getenv("OPENSEARCH_OUTAGE_ALIAS")
	outagePhysical := os.Getenv("OPENSEARCH_OUTAGE_PHYSICAL")
	if outageAlias == "" || outagePhysical == "" {
		t.Fatal("OPENSEARCH_OUTAGE_ALIAS and OPENSEARCH_OUTAGE_PHYSICAL are required")
	}
	codec, err := search.NewCursorCodec([]byte("outage-cursor-key-32-bytes!!!!!!"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: endpoints[:2], AllowInsecureHTTP: true,
		RequestTimeout: 5 * time.Second, MaximumResponseBytes: 8 << 20,
		Search: &adapter.SearchConfig{
			Limits: search.DefaultLimits(), CursorCodec: codec,
			Authorizer: adapter.SearchAuthorizerFunc(func(context.Context, adapter.SearchAuthorization) error { return nil }),
			WriteGuard: adapter.WriteGuardFunc(func(context.Context, adapter.WriteAuthorization) error { return nil }),
			Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
				return adapter.IndexTarget{Name: outageAlias, PhysicalName: outagePhysical, Fingerprint: "outage-definition"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for attempt := range 2 {
		started := time.Now()
		_, err := client.Info(t.Context())
		var failure *adapter.Failure
		if !errors.As(err, &failure) || failure.Category != adapter.FailureTransport ||
			!failure.Retryable || !failure.OutcomeKnown || time.Since(started) > 6*time.Second {
			t.Fatalf("complete-outage attempt %d = %#v after %s", attempt, failure, time.Since(started))
		}
	}
	request := search.Request{
		Tenant: "outage-tenant", Index: "documents", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.OffsetPage{Size: 1},
	}
	if _, err := client.Search(t.Context(), request); !knownRetryableTransportFailure(err, adapter.OperationSearch) {
		t.Fatalf("outage search error = %#v", err)
	}
	document, err := search.NewDocument("outage-tenant", "documents", "id", 1, json.RawMessage(`{"value":"outage"}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	operation := search.IndexDocument(document)
	outcome, err := client.Write(t.Context(), operation, search.RefreshNone)
	if outcome.State != search.OutcomeUnknown || !unknownRetryableTransportFailure(err, adapter.OperationWrite) {
		t.Fatalf("outage write = %#v/%#v", outcome, err)
	}
	bulkDocument, err := search.NewDocument("outage-tenant", "documents", "bulk-id", 1, json.RawMessage(`{"value":"outage-bulk"}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	bulkRequest := search.BulkRequest{Operations: []search.WriteOperation{search.IndexDocument(bulkDocument)}, Refresh: search.RefreshNone}
	bulk, err := client.Bulk(t.Context(), bulkRequest)
	if items := bulk.Items(); len(items) != 1 || items[0].State != search.OutcomeUnknown ||
		!unknownRetryableTransportFailure(err, adapter.OperationBulk) {
		t.Fatalf("outage bulk = %#v/%#v", items, err)
	}
}

func TestRealOpenSearchOutageRecoveryReconcilesUnknownWrites(t *testing.T) {
	endpoint, expectedVersion, _, physical := rollingFixtureEnvironment(t)
	alias := os.Getenv("OPENSEARCH_OUTAGE_ALIAS")
	if alias == "" {
		t.Fatal("OPENSEARCH_OUTAGE_ALIAS is required")
	}
	client := newBoundIntegrationSearchClient(t, endpoint, "outage-tenant", "documents", alias, physical, "outage-definition", search.DefaultLimits())
	if info, err := client.Info(t.Context()); err != nil || info.Version != expectedVersion {
		t.Fatalf("recovered Info() = %#v/%v", info, err)
	}
	result, err := client.Search(t.Context(), search.Request{
		Tenant: "outage-tenant", Index: "documents", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 10},
	})
	if err != nil || len(result.Hits()) != 1 || result.Hits()[0].ID != "persistent" {
		t.Fatalf("authoritative post-outage read = %#v/%v", result.Hits(), err)
	}
	for _, id := range []string{"id", "bulk-id"} {
		document, documentErr := search.NewDocument("outage-tenant", "documents", id, 1, json.RawMessage(`{"value":"reconciled"}`), search.DefaultLimits())
		if documentErr != nil {
			t.Fatal(documentErr)
		}
		outcome, writeErr := client.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor)
		if writeErr != nil || outcome.State != search.OutcomeApplied || outcome.Version != 1 {
			t.Fatalf("reconciled write %q = %#v/%v", id, outcome, writeErr)
		}
	}
	result, err = client.Search(t.Context(), search.Request{
		Tenant: "outage-tenant", Index: "documents", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 10},
	})
	if err != nil || len(result.Hits()) != 3 || result.Hits()[0].ID != "bulk-id" || result.Hits()[1].ID != "id" || result.Hits()[2].ID != "persistent" {
		t.Fatalf("reconciled post-outage state = %#v/%v", result.Hits(), err)
	}
	for _, id := range []string{"id", "bulk-id"} {
		outcome, deleteErr := client.Write(t.Context(), search.DeleteDocument("outage-tenant", "documents", id, 2), search.RefreshWaitFor)
		if deleteErr != nil || outcome.State != search.OutcomeApplied || outcome.Version != 2 {
			t.Fatalf("reconciled cleanup %q = %#v/%v", id, outcome, deleteErr)
		}
	}
}

func knownRetryableTransportFailure(err error, operation adapter.Operation) bool {
	var failure *adapter.Failure
	return errors.As(err, &failure) && failure.Operation == operation &&
		failure.Category == adapter.FailureTransport && failure.Retryable && failure.OutcomeKnown
}

func unknownRetryableTransportFailure(err error, operation adapter.Operation) bool {
	var failure *adapter.Failure
	return errors.As(err, &failure) && failure.Operation == operation &&
		failure.Category == adapter.FailureTransport && failure.Retryable && !failure.OutcomeKnown
}

func TestRealOpenSearchRollingFixtureSeed(t *testing.T) {
	endpoint, expectedVersion, expectedNode, fixture := rollingFixtureEnvironment(t)
	assertRollingEndpointIdentity(t, endpoint, expectedVersion, expectedNode)
	direct := rollingOfficialClient(t, endpoint)
	requireDirectOpenSearchJSON(t, direct, http.MethodPut, "/"+fixture,
		[]byte(`{"settings":{"number_of_shards":1,"number_of_replicas":1},"mappings":{"dynamic":"strict","properties":{"value":{"type":"keyword"}}}}`), http.StatusOK)
	requireDirectOpenSearchJSON(t, direct, http.MethodPut, "/"+fixture+"/_doc/persistent?refresh=wait_for&version=7&version_type=external",
		[]byte(`{"value":"survives-rolling-upgrade"}`), http.StatusCreated)
	requireDirectOpenSearchJSON(t, direct, http.MethodPost, "/_aliases",
		[]byte(`{"actions":[{"add":{"index":"`+fixture+`","alias":"`+fixture+`-alias","is_write_index":true}}]}`), http.StatusOK)
}

func TestRealOpenSearchRollingFixtureVerify(t *testing.T) {
	endpoint, expectedVersion, expectedNode, fixture := rollingFixtureEnvironment(t)
	assertRollingEndpointIdentity(t, endpoint, expectedVersion, expectedNode)
	direct := rollingOfficialClient(t, endpoint)
	body := requireDirectOpenSearchJSON(t, direct, http.MethodGet, "/"+fixture+"/_doc/persistent", nil, http.StatusOK)
	var document struct {
		Found   bool            `json:"found"`
		Version uint64          `json:"_version"`
		Source  json.RawMessage `json:"_source"`
	}
	if json.Unmarshal(body, &document) != nil || !document.Found || document.Version != 7 || string(document.Source) != `{"value":"survives-rolling-upgrade"}` {
		t.Fatalf("rolling fixture = %#v body %q", document, body)
	}
}

func rollingFixtureEnvironment(t *testing.T) (string, string, string, string) {
	t.Helper()
	endpoint := os.Getenv("OPENSEARCH_URL")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	expectedNode := os.Getenv("OPENSEARCH_EXPECTED_NODE")
	fixture := os.Getenv("OPENSEARCH_ROLLING_FIXTURE")
	if endpoint == "" || expectedVersion == "" || expectedNode == "" || fixture == "" {
		t.Skip("rolling fixture environment is not configured")
	}
	return endpoint, expectedVersion, expectedNode, fixture
}

func assertRollingEndpointIdentity(t *testing.T, endpoint, expectedVersion, expectedNode string) {
	t.Helper()
	client := integrationClient(t, []string{endpoint})
	info, err := client.Info(t.Context())
	if err != nil || info.Version != expectedVersion || info.Node != expectedNode {
		t.Fatalf("rolling endpoint identity = %#v/%v, want node %q version %q", info, err, expectedNode, expectedVersion)
	}
}

func rollingOfficialClient(t *testing.T, endpoint string) *official.Client {
	t.Helper()
	discover := false
	client, err := official.NewClient(official.Config{Addresses: []string{endpoint}, DisableRetry: true, DiscoverNodesOnStart: &discover, HealthCheckMaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
