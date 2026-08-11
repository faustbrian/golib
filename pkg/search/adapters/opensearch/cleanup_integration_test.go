//go:build integration

package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
	official "github.com/opensearch-project/opensearch-go/v4"
)

func TestRealOpenSearchConformanceCleanupSafeguards(t *testing.T) {
	endpoint := os.Getenv("OPENSEARCH_URL")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	if endpoint == "" || expectedVersion == "" {
		t.Skip("OPENSEARCH_URL and OPENSEARCH_EXPECTED_VERSION are not configured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatal("OPENSEARCH_URL must name a disposable OpenSearch cluster")
	}
	limits := search.DefaultLimits()
	tenant := "cleanup-tenant"
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	alias := "golib-search-cleanup-alias-" + suffix
	activeName := "golib-search-cleanup-active-" + suffix
	inactiveName := "golib-search-cleanup-inactive-" + suffix
	active, err := search.NewIndexDefinition(activeName,
		json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0}`),
		json.RawMessage(`{"dynamic":"strict","properties":{"value":{"type":"keyword"}}}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	inactive, err := search.NewIndexDefinition(inactiveName,
		json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0}`),
		json.RawMessage(`{"dynamic":"strict","properties":{"legacy":{"type":"keyword"}}}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	direct := cleanupOfficialClient(t, endpoint)
	guard := &realCleanupEligibilityGuard{
		direct: direct,
		expected: search.LifecycleCleanupRequest{
			MigrationID: "cleanup-migration-" + suffix, Tenant: tenant, Alias: alias,
			ActiveIndex: activeName, ActiveFingerprint: active.Fingerprint(),
			InactiveIndex: inactiveName, InactiveFingerprint: inactive.Fingerprint(),
		},
		definitions: map[string]search.IndexDefinition{
			active.Fingerprint(): active, inactive.Fingerprint(): inactive,
		},
		retentionEligible: true,
		backupEligible:    true,
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		RequestTimeout: 30 * time.Second, MaximumResponseBytes: 16 << 20,
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: adapter.LifecycleAuthorizerFunc(func(_ context.Context, gotTenant string, resources []string) error {
				if gotTenant != tenant || len(resources) == 0 {
					return errors.New("cleanup lifecycle scope denied")
				}
				for _, resource := range resources {
					if resource != alias && resource != activeName && resource != inactiveName {
						return errors.New("cleanup lifecycle scope denied")
					}
				}
				return nil
			}),
			MutationGuard: allowLifecycleMutationGuard(),
			CleanupGuard:  guard,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if info, infoErr := client.Info(t.Context()); infoErr != nil || info.Version != expectedVersion {
		t.Fatalf("Info() = %#v/%v, expected %s", info, infoErr, expectedVersion)
	}
	for _, definition := range []search.IndexDefinition{active, inactive} {
		if err := client.CreateIndex(t.Context(), tenant, definition); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = deleteDisposableIndex(ctx, endpoint, activeName)
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, status, _ := directOpenSearchJSON(ctx, direct, http.MethodGet, "/"+inactiveName, nil)
		if status == http.StatusOK {
			_ = deleteDisposableIndex(ctx, endpoint, inactiveName)
		}
	})
	if err := client.AddAlias(t.Context(), tenant, alias, activeName, true); err != nil {
		t.Fatal(err)
	}
	guard.expectedUUIDs = map[string]string{
		activeName:   cleanupIndexUUID(t, direct, activeName),
		inactiveName: cleanupIndexUUID(t, direct, inactiveName),
	}

	pitID := openCleanupPointInTime(t, direct, inactiveName)
	guard.retainedReaders.Store(1)
	if err := client.CleanupIndex(t.Context(), guard.expected); !errors.Is(err, adapter.ErrLifecycleCleanupGuardRejected) {
		t.Fatalf("cleanup with retained PIT error = %v, want guard rejection", err)
	}
	if _, status, readErr := directOpenSearchJSON(t.Context(), direct, http.MethodGet, "/"+inactiveName, nil); readErr != nil || status != http.StatusOK {
		t.Fatalf("guard rejection lost inactive generation: status=%d error=%v", status, readErr)
	}
	closeCleanupPointInTime(t, direct, pitID)
	guard.retainedReaders.Store(0)
	if err := client.CleanupIndex(t.Context(), guard.expected); err != nil {
		t.Fatalf("eligible CleanupIndex() error = %v", err)
	}
	if _, status, readErr := directOpenSearchJSON(t.Context(), direct, http.MethodGet, "/"+inactiveName, nil); readErr != nil || status != http.StatusNotFound {
		t.Fatalf("inactive generation after cleanup: status=%d error=%v", status, readErr)
	}
	if _, status, readErr := directOpenSearchJSON(t.Context(), direct, http.MethodGet, "/"+activeName, nil); readErr != nil || status != http.StatusOK {
		t.Fatalf("active generation after cleanup: status=%d error=%v", status, readErr)
	}
	if guard.successfulChecks.Load() != 1 {
		t.Fatalf("successful cleanup eligibility checks = %d, want 1", guard.successfulChecks.Load())
	}
}

type realCleanupEligibilityGuard struct {
	direct            *official.Client
	expected          search.LifecycleCleanupRequest
	definitions       map[string]search.IndexDefinition
	expectedUUIDs     map[string]string
	retentionEligible bool
	backupEligible    bool
	retainedReaders   atomic.Int64
	successfulChecks  atomic.Int64
}

func (guard *realCleanupEligibilityGuard) WithCleanupEligible(ctx context.Context, request search.LifecycleCleanupRequest, operation func() error) error {
	if ctx == nil || ctx.Err() != nil || request != guard.expected || operation == nil {
		return errors.New("cleanup eligibility binding rejected")
	}
	if !guard.retentionEligible || !guard.backupEligible || guard.retainedReaders.Load() != 0 {
		return errors.New("cleanup retention, backup, or reader prerequisite rejected")
	}
	if err := guard.verifyIndexIdentity(ctx, request.ActiveIndex, request.ActiveFingerprint); err != nil {
		return err
	}
	if err := guard.verifyIndexIdentity(ctx, request.InactiveIndex, request.InactiveFingerprint); err != nil {
		return err
	}
	if err := guard.verifyAliases(ctx, request); err != nil {
		return err
	}
	guard.successfulChecks.Add(1)
	return operation()
}

func (guard *realCleanupEligibilityGuard) verifyIndexIdentity(ctx context.Context, index, fingerprint string) error {
	verifier := realLifecycleVerifier{
		client: guard.direct, pageSize: 1, maximumRecords: 1, maximumResponseBytes: 1 << 20,
		expectedDefinitions: guard.definitions,
	}
	if got, err := verifier.verifyTargetDefinition(ctx, index, fingerprint); err != nil || got != fingerprint {
		return errors.New("cleanup live definition rejected")
	}
	if cleanupIndexUUIDContext(ctx, guard.direct, index) != guard.expectedUUIDs[index] {
		return errors.New("cleanup physical generation identity changed")
	}
	return nil
}

func (guard *realCleanupEligibilityGuard) verifyAliases(ctx context.Context, request search.LifecycleCleanupRequest) error {
	body, status, err := directOpenSearchJSON(ctx, guard.direct, http.MethodGet, "/"+request.ActiveIndex+"/_alias/"+request.Alias, nil)
	if err != nil || status != http.StatusOK {
		return errors.New("cleanup active alias binding rejected")
	}
	var active map[string]struct {
		Aliases map[string]json.RawMessage `json:"aliases"`
	}
	if decodeStrictIntegrationJSON(body, &active) != nil || len(active) != 1 || active[request.ActiveIndex].Aliases[request.Alias] == nil {
		return errors.New("cleanup active alias binding rejected")
	}
	body, status, err = directOpenSearchJSON(ctx, guard.direct, http.MethodGet, "/"+request.InactiveIndex+"/_alias", nil)
	if err != nil || status != http.StatusOK {
		return errors.New("cleanup inactive alias inventory rejected")
	}
	var inactive map[string]struct {
		Aliases map[string]json.RawMessage `json:"aliases"`
	}
	if decodeStrictIntegrationJSON(body, &inactive) != nil || len(inactive) != 1 || len(inactive[request.InactiveIndex].Aliases) != 0 {
		return errors.New("cleanup inactive generation still has an alias")
	}
	return nil
}

func cleanupOfficialClient(t *testing.T, endpoint string) *official.Client {
	t.Helper()
	discover := false
	client, err := official.NewClient(official.Config{
		Addresses: []string{endpoint}, DisableRetry: true, DiscoverNodesOnStart: &discover,
		HealthCheckMaxRetries: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func cleanupIndexUUID(t *testing.T, client *official.Client, index string) string {
	t.Helper()
	uuid := cleanupIndexUUIDContext(t.Context(), client, index)
	if uuid == "" {
		t.Fatalf("index %q has no stable UUID", index)
	}
	return uuid
}

func cleanupIndexUUIDContext(ctx context.Context, client *official.Client, index string) string {
	body, status, err := directOpenSearchJSON(ctx, client, http.MethodGet, "/"+index+"/_settings?flat_settings=true&include_defaults=false", nil)
	if err != nil || status != http.StatusOK {
		return ""
	}
	var settings map[string]struct {
		Settings map[string]string `json:"settings"`
	}
	if decodeStrictIntegrationJSON(body, &settings) != nil || len(settings) != 1 {
		return ""
	}
	return settings[index].Settings["index.uuid"]
}

func openCleanupPointInTime(t *testing.T, client *official.Client, index string) string {
	t.Helper()
	body, status, err := directOpenSearchJSON(t.Context(), client, http.MethodPost, "/"+index+"/_search/point_in_time?keep_alive=1m", nil)
	if err != nil || status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("create retained cleanup PIT = status %d error %v", status, err)
	}
	var result struct {
		PITID string `json:"pit_id"`
	}
	if decodeStrictIntegrationJSON(body, &result) != nil || result.PITID == "" {
		t.Fatalf("create retained cleanup PIT response = %q", body)
	}
	return result.PITID
}

func closeCleanupPointInTime(t *testing.T, client *official.Client, pitID string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"pit_id": pitID})
	if err != nil {
		t.Fatal(err)
	}
	response := requireDirectOpenSearchJSON(t, client, http.MethodDelete, "/_search/point_in_time", body, http.StatusOK)
	var result struct {
		PITs []struct {
			PITID      string `json:"pit_id"`
			Successful bool   `json:"successful"`
		} `json:"pits"`
	}
	if decodeStrictIntegrationJSON(response, &result) != nil || len(result.PITs) != 1 || result.PITs[0].PITID != pitID || !result.PITs[0].Successful {
		t.Fatalf("delete retained cleanup PIT response = %q", response)
	}
}
