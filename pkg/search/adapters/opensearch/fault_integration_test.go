//go:build integration

package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
	official "github.com/opensearch-project/opensearch-go/v4"
)

func TestRealOpenSearchConformanceLiveFaults(t *testing.T) {
	endpoint, direct := realFaultEnvironment(t)
	t.Run("cluster block classification and recovery", func(t *testing.T) {
		testRealClusterBlock(t, endpoint, direct)
	})
	t.Run("partial shard disclosure", func(t *testing.T) {
		testRealPartialShards(t, endpoint, direct)
	})
	t.Run("malformed response rejection", func(t *testing.T) {
		testRealMalformedResponse(t, endpoint)
	})
	t.Run("ambiguous applied write reconciliation", func(t *testing.T) {
		testRealAmbiguousAppliedWrite(t, endpoint, direct)
	})
}

func testRealClusterBlock(t *testing.T, endpoint string, direct *official.Client) {
	limits := search.DefaultLimits()
	tenant, logicalIndex := "live-fault-tenant", "documents"
	physical, alias := realFaultNames("cluster-block")
	createRealFaultIndex(t, direct, physical, alias,
		`{"dynamic":"strict","properties":{"value":{"type":"keyword"}}}`)
	client := newBoundIntegrationSearchClient(t, endpoint, tenant, logicalIndex, alias, physical, "live-fault-fingerprint", limits)

	requireDirectOpenSearchJSON(t, direct, http.MethodPut, "/"+physical+"/_settings",
		[]byte(`{"index.blocks.read_only_allow_delete":true}`), http.StatusOK)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, _ = directOpenSearchJSON(ctx, direct, http.MethodPut, "/"+physical+"/_settings",
			[]byte(`{"index.blocks.read_only_allow_delete":null}`))
	})
	document, err := search.NewDocument(tenant, logicalIndex, "blocked", 1, json.RawMessage(`{"value":"blocked"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	outcome, writeErr := client.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor)
	var failure *adapter.Failure
	if !errors.As(writeErr, &failure) || failure.Category != adapter.FailureClusterBlocked || failure.Retryable || !failure.OutcomeKnown ||
		outcome.State != search.OutcomeFailed || outcome.Retryable || outcome.Code != "cluster_block_exception" {
		t.Fatalf("live cluster block = %#v/%#v", outcome, failure)
	}

	requireDirectOpenSearchJSON(t, direct, http.MethodPut, "/"+physical+"/_settings",
		[]byte(`{"index.blocks.read_only_allow_delete":null}`), http.StatusOK)
	outcome, writeErr = client.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor)
	if writeErr != nil || outcome.State != search.OutcomeApplied || outcome.Version != document.Version {
		t.Fatalf("live cluster-block recovery = %#v/%v", outcome, writeErr)
	}
}

func testRealPartialShards(t *testing.T, endpoint string, direct *official.Client) {
	limits := search.DefaultLimits()
	tenant, logicalIndex := "live-partial-tenant", "documents"
	good, alias := realFaultNames("partial-good")
	bad, _ := realFaultNames("partial-bad")
	createRealFaultPhysical(t, direct, good,
		`{"dynamic":"strict","properties":{"value":{"type":"keyword"}}}`)
	createRealFaultPhysical(t, direct, bad,
		`{"dynamic":"strict","properties":{"value":{"type":"text"}}}`)
	aliasBody, _ := json.Marshal(map[string]any{"actions": []any{
		map[string]any{"add": map[string]any{"index": good, "alias": alias}},
		map[string]any{"add": map[string]any{"index": bad, "alias": alias}},
	}})
	requireDirectOpenSearchJSON(t, direct, http.MethodPost, "/_aliases", aliasBody, http.StatusOK)
	for physical, value := range map[string]string{good: "safe", bad: "unsafe"} {
		requireDirectOpenSearchJSON(t, direct, http.MethodPut,
			"/"+physical+"/_doc/fixture?version=1&version_type=external&refresh=wait_for",
			[]byte(`{"value":"`+value+`"}`), http.StatusCreated)
	}
	client := newBoundIntegrationSearchClient(t, endpoint, tenant, logicalIndex, alias, alias, "live-partial-fingerprint", limits)
	result, err := client.Search(t.Context(), search.Request{
		Tenant: tenant, Index: logicalIndex,
		Query: search.TermQuery{Field: "value", Value: search.StringValue("does-not-match")},
		Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page:  search.OffsetPage{Size: 1},
		Aggregations: map[string]search.Aggregation{
			"by_value": search.TermsAggregation{Field: "value", Size: 10},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := result.Diagnostics()
	if !diagnostics.Partial || diagnostics.TimedOut || diagnostics.Shards.Total != 2 || diagnostics.Shards.Successful != 1 ||
		diagnostics.Shards.Failed != 1 || len(diagnostics.Failures) != 1 || diagnostics.Failures[0].Scope != "shard" ||
		diagnostics.Failures[0].Code != "illegal_argument_exception" || diagnostics.Failures[0].Retryable {
		t.Fatalf("live partial-shard diagnostics = %#v", diagnostics)
	}
	if len(result.Hits()) != 0 || result.Total().Value != 0 || len(result.Aggregations()["by_value"]) == 0 {
		t.Fatalf("live partial-shard result = hits %d total %d aggregations %#v", len(result.Hits()), result.Total().Value, result.Aggregations())
	}
}

func testRealMalformedResponse(t *testing.T, endpoint string) {
	target, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	var forwarded atomic.Int32
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(response *http.Response) error {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(body) == 0 || len(body) > 1<<20 || response.StatusCode != http.StatusOK {
			return errors.New("live upstream response was invalid")
		}
		forwarded.Add(1)
		malformed := `{"version":`
		response.Body = io.NopCloser(strings.NewReader(malformed))
		response.ContentLength = int64(len(malformed))
		response.Header.Set("Content-Length", strconv.Itoa(len(malformed)))
		return nil
	}
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{server.URL}, AllowInsecureHTTP: true,
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	_, infoErr := client.Info(t.Context())
	var failure *adapter.Failure
	if !errors.As(infoErr, &failure) || failure.Category != adapter.FailureMalformed || !failure.OutcomeKnown || forwarded.Load() != 1 {
		t.Fatalf("live malformed response = %#v forwarded=%d", failure, forwarded.Load())
	}
}

func testRealAmbiguousAppliedWrite(t *testing.T, endpoint string, direct *official.Client) {
	limits := search.DefaultLimits()
	tenant, logicalIndex := "live-ambiguous-tenant", "documents"
	physical, alias := realFaultNames("ambiguous")
	createRealFaultIndex(t, direct, physical, alias,
		`{"dynamic":"strict","properties":{"value":{"type":"keyword"}}}`)
	target, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	upstreamApplied := make(chan struct{}, 1)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(response *http.Response) error {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(body) == 0 || len(body) > 1<<20 ||
			(response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated) {
			return errors.New("live upstream write response was invalid")
		}
		upstreamApplied <- struct{}{}
		return errors.New("injected response loss after backend acknowledgement")
	}
	proxy.ErrorHandler = func(http.ResponseWriter, *http.Request, error) { panic(http.ErrAbortHandler) }
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	client := newBoundIntegrationSearchClient(t, server.URL, tenant, logicalIndex, alias, physical, "live-ambiguous-fingerprint", limits)
	document, err := search.NewDocument(tenant, logicalIndex, "ambiguous", 7, json.RawMessage(`{"value":"applied"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	outcome, writeErr := client.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor)
	var failure *adapter.Failure
	if !errors.As(writeErr, &failure) || failure.Category != adapter.FailureTransport || failure.OutcomeKnown || !failure.Retryable ||
		outcome.State != search.OutcomeUnknown || !outcome.Retryable {
		t.Fatalf("live ambiguous write = %#v/%#v", outcome, failure)
	}
	select {
	case <-upstreamApplied:
	case <-time.After(time.Second):
		t.Fatal("fault proxy did not observe the backend acknowledgement")
	}
	body := requireDirectOpenSearchJSON(t, direct, http.MethodGet, "/"+physical+"/_doc/"+document.ID, nil, http.StatusOK)
	var stored struct {
		Found   bool            `json:"found"`
		Version uint64          `json:"_version"`
		Source  json.RawMessage `json:"_source"`
	}
	if json.Unmarshal(body, &stored) != nil || !stored.Found || stored.Version != document.Version ||
		search.SourceDigest(stored.Source) != search.SourceDigest(document.Source) {
		t.Fatalf("reconciled live ambiguous write = %#v body %q", stored, body)
	}
}

func realFaultEnvironment(t *testing.T) (string, *official.Client) {
	t.Helper()
	endpoint := os.Getenv("OPENSEARCH_URL")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	if endpoint == "" || expectedVersion == "" {
		t.Skip("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are not configured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatal("OPENSEARCH_URL is invalid")
	}
	direct, err := official.NewClient(official.Config{Addresses: []string{endpoint}, DisableRetry: true, HealthCheckMaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = direct.Close() })
	body := requireDirectOpenSearchJSON(t, direct, http.MethodGet, "/", nil, http.StatusOK)
	var identity struct {
		Version struct {
			Number string `json:"number"`
		} `json:"version"`
	}
	if json.Unmarshal(body, &identity) != nil || identity.Version.Number != expectedVersion {
		t.Fatalf("live fault backend version = %q, want %q", identity.Version.Number, expectedVersion)
	}
	return endpoint, direct
}

func realFaultNames(label string) (string, string) {
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	physical := fmt.Sprintf("golib-search-%s-%s", label, suffix)
	return physical, physical + "-alias"
}

func createRealFaultIndex(t *testing.T, direct *official.Client, physical, alias, mapping string) {
	t.Helper()
	createRealFaultPhysical(t, direct, physical, mapping)
	body, _ := json.Marshal(map[string]any{"actions": []any{map[string]any{"add": map[string]any{
		"index": physical, "alias": alias, "is_write_index": true,
	}}}})
	requireDirectOpenSearchJSON(t, direct, http.MethodPost, "/_aliases", body, http.StatusOK)
}

func createRealFaultPhysical(t *testing.T, direct *official.Client, physical, mapping string) {
	t.Helper()
	body := []byte(`{"settings":{"number_of_shards":1,"number_of_replicas":0},"mappings":` + mapping + `}`)
	requireDirectOpenSearchJSON(t, direct, http.MethodPut, "/"+physical, body, http.StatusOK)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, _ = directOpenSearchJSON(ctx, direct, http.MethodDelete, "/"+physical, nil)
	})
}
