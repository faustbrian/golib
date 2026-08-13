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

func TestRealOpenSearchCleanupGuardHoldsCompleteEligibilityAcrossDeletion(t *testing.T) {
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
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenant, alias := "cleanup-tenant", "golib-search-cleanup-"+suffix
	active, inactive, retainedAlias := alias+"-v2", alias+"-v1", alias+"-retained"
	settings := json.RawMessage(`{"number_of_shards":1,"number_of_replicas":0}`)
	mappings := json.RawMessage(`{"dynamic":"strict","properties":{"name":{"type":"keyword"}}}`)
	activeDefinition, err := search.NewIndexDefinition(active, settings, mappings, limits)
	if err != nil {
		t.Fatal(err)
	}
	inactiveDefinition, err := search.NewIndexDefinition(inactive, settings, mappings, limits)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := official.NewClient(official.Config{Addresses: []string{endpoint}, DisableRetry: true, HealthCheckMaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = direct.Close() })
	verifier := &realLifecycleVerifier{
		client: direct, pageSize: 1, maximumRecords: 1, maximumResponseBytes: 1 << 20,
		expectedDefinitions: map[string]search.IndexDefinition{
			activeDefinition.Fingerprint():   activeDefinition,
			inactiveDefinition.Fingerprint(): inactiveDefinition,
		},
	}
	request := search.LifecycleCleanupRequest{
		MigrationID: "cleanup-" + suffix, Tenant: tenant, Alias: alias,
		ActiveIndex: active, ActiveFingerprint: activeDefinition.Fingerprint(),
		InactiveIndex: inactive, InactiveFingerprint: inactiveDefinition.Fingerprint(),
	}
	var retentionSatisfied atomic.Bool
	var deletionDispatched atomic.Bool
	guard := adapter.LifecycleCleanupGuardFunc(func(ctx context.Context, got search.LifecycleCleanupRequest, operation func() error) error {
		if got != request || !retentionSatisfied.Load() {
			return errors.New("cleanup eligibility denied")
		}
		aliasBody, _, aliasErr := directOpenSearchJSON(ctx, direct, http.MethodGet, "/_alias/"+got.Alias, nil)
		var aliases map[string]json.RawMessage
		if aliasErr != nil || json.Unmarshal(aliasBody, &aliases) != nil || len(aliases) != 1 || aliases[got.ActiveIndex] == nil {
			return errors.New("active alias membership changed")
		}
		inactiveAliasBody, inactiveAliasStatus, inactiveAliasErr := directOpenSearchJSON(ctx, direct, http.MethodGet, "/"+got.InactiveIndex+"/_alias", nil)
		var inactiveAliases map[string]struct {
			Aliases map[string]json.RawMessage `json:"aliases"`
		}
		if inactiveAliasErr != nil || inactiveAliasStatus != http.StatusOK ||
			decodeStrictIntegrationJSON(inactiveAliasBody, &inactiveAliases) != nil || len(inactiveAliases) != 1 ||
			len(inactiveAliases[got.InactiveIndex].Aliases) != 0 {
			return errors.New("inactive generation still has an alias")
		}
		if fingerprint, verifyErr := verifier.verifyTargetDefinition(ctx, got.ActiveIndex, got.ActiveFingerprint); verifyErr != nil || fingerprint != got.ActiveFingerprint {
			return errors.New("active definition changed")
		}
		if fingerprint, verifyErr := verifier.verifyTargetDefinition(ctx, got.InactiveIndex, got.InactiveFingerprint); verifyErr != nil || fingerprint != got.InactiveFingerprint {
			return errors.New("inactive definition changed")
		}
		deletionDispatched.Store(true)
		return operation()
	})
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{endpoint}, AllowInsecureHTTP: parsed.Scheme == "http",
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: adapter.LifecycleAuthorizerFunc(func(_ context.Context, gotTenant string, resources []string) error {
				if gotTenant != tenant {
					return errors.New("cleanup tenant denied")
				}
				for _, resource := range resources {
					if resource != alias && resource != active && resource != inactive && resource != retainedAlias {
						return errors.New("cleanup resource denied")
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
	for _, definition := range []search.IndexDefinition{activeDefinition, inactiveDefinition} {
		if err := client.CreateIndex(t.Context(), tenant, definition); err != nil {
			t.Fatal(err)
		}
		index := definition.Name()
		t.Cleanup(func() { _ = deleteDisposableIndex(context.Background(), endpoint, index) })
	}
	if err := client.AddAlias(t.Context(), tenant, alias, active, true); err != nil {
		t.Fatal(err)
	}
	if err := client.AddAlias(t.Context(), tenant, retainedAlias, inactive, false); err != nil {
		t.Fatal(err)
	}
	retentionSatisfied.Store(true)
	if err := client.CleanupIndex(t.Context(), request); !errors.Is(err, adapter.ErrLifecycleCleanupGuardRejected) || deletionDispatched.Load() {
		t.Fatalf("guarded cleanup with retained alias = %v/dispatched=%t", err, deletionDispatched.Load())
	}
	removeAlias, _ := json.Marshal(map[string]any{"actions": []any{map[string]any{"remove": map[string]any{"index": inactive, "alias": retainedAlias, "must_exist": true}}}})
	requireDirectOpenSearchJSON(t, direct, http.MethodPost, "/_aliases", removeAlias, http.StatusOK)
	if err := client.CleanupIndex(t.Context(), request); err != nil || !deletionDispatched.Load() {
		t.Fatalf("eligible CleanupIndex() = %v/dispatched=%t", err, deletionDispatched.Load())
	}
	_, status, requestErr := directOpenSearchJSON(t.Context(), direct, http.MethodGet, "/"+inactive, nil)
	if requestErr != nil || status != http.StatusNotFound {
		t.Fatalf("inactive index after cleanup status/error = %d/%v", status, requestErr)
	}
}
