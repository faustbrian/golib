package httpclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestPolicyScopeKeysAreDeterministicResourceSpecificAndOpaque(t *testing.T) {
	t.Parallel()

	scope, err := NewPolicyScope(PolicyScopeOptions{
		Endpoint: "widgets.list", Credential: "credential-secret",
		Tenant: "tenant-secret", Account: "account-secret",
		Custom: map[string]string{"region": "north-secret"},
	})
	if err != nil {
		t.Fatalf("construct policy scope: %v", err)
	}
	ctx, err := WithPolicyScope(context.Background(), scope)
	if err != nil {
		t.Fatalf("attach policy scope: %v", err)
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.test/widgets?cursor=query-secret", nil)
	request.Header.Set("Authorization", "Bearer header-secret")

	first, err := ResolvePolicyScope(request, PolicyResourceCache)
	if err != nil {
		t.Fatalf("resolve cache scope: %v", err)
	}
	second, _ := ResolvePolicyScope(request, PolicyResourceCache)
	if first.String() != second.String() || first.Resource() != PolicyResourceCache {
		t.Fatalf("cache scopes = %#v, %#v", first, second)
	}
	for _, secret := range []string{"credential-secret", "tenant-secret", "account-secret", "query-secret", "header-secret"} {
		if strings.Contains(first.String(), secret) {
			t.Fatalf("scope key rendered %q", secret)
		}
	}
	wantDimensions := []ScopeDimension{
		ScopeOrigin, ScopeCredential, ScopeTenant, ScopeAccount,
	}
	if got := first.Dimensions(); len(got) != len(wantDimensions) {
		t.Fatalf("cache dimensions = %#v", got)
	} else {
		for index := range got {
			if got[index] != wantDimensions[index] {
				t.Fatalf("cache dimensions = %#v", got)
			}
		}
	}

	metrics, err := ResolvePolicyScope(request, PolicyResourceMetrics)
	if err != nil {
		t.Fatalf("resolve metrics scope: %v", err)
	}
	changed, _ := NewPolicyScope(PolicyScopeOptions{
		Endpoint: "widgets.list", Credential: "other", Tenant: "other", Account: "other",
	})
	changedContext, _ := WithPolicyScope(context.Background(), changed)
	changedRequest := request.Clone(changedContext)
	changedMetrics, _ := ResolvePolicyScope(changedRequest, PolicyResourceMetrics)
	if metrics.String() != changedMetrics.String() {
		t.Fatalf("metrics keys included identity: %q, %q", metrics, changedMetrics)
	}
}

func TestPolicyScopeSupportsCustomDimensionsAndSnapshotsInput(t *testing.T) {
	t.Parallel()

	custom := map[string]string{"region": "north"}
	scope, err := NewPolicyScope(PolicyScopeOptions{Tenant: "tenant", Custom: custom})
	if err != nil {
		t.Fatalf("construct custom scope: %v", err)
	}
	custom["region"] = "mutated"
	dimension, err := CustomScopeDimension("region")
	if err != nil {
		t.Fatalf("construct custom dimension: %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test/widgets", nil)
	request = request.WithContext(mustPolicyScopeContext(t, scope))
	key, err := ResolvePolicyScope(request, PolicyResourceRateLimiter, ScopeOrigin, ScopeTenant, dimension)
	if err != nil {
		t.Fatalf("resolve custom scope: %v", err)
	}
	if got := key.Dimensions(); len(got) != 3 || got[2] != dimension {
		t.Fatalf("custom dimensions = %#v", got)
	}

	mutated, _ := NewPolicyScope(PolicyScopeOptions{
		Tenant: "tenant", Custom: map[string]string{"region": "mutated"},
	})
	mutatedRequest := request.Clone(mustPolicyScopeContext(t, mutated))
	mutatedKey, _ := ResolvePolicyScope(mutatedRequest, PolicyResourceRateLimiter, ScopeOrigin, ScopeTenant, dimension)
	if key.String() == mutatedKey.String() {
		t.Fatalf("custom scope mutation did not change key: %q", key)
	}
}

func TestCacheAndCoalescingKeysAreIdentityScoped(t *testing.T) {
	t.Parallel()

	policy := &cachePolicy{namespace: "scope-test"}
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test/widgets", nil)
	request.Header.Set("Authorization", "Bearer first")
	first, err := policy.cacheKey(request)
	if err != nil {
		t.Fatalf("first cache key: %v", err)
	}
	request.Header.Set("Authorization", "Bearer second")
	second, _ := policy.cacheKey(request)
	if first == second {
		t.Fatalf("credential cache keys matched: %q", first)
	}

	tenantA, _ := NewPolicyScope(PolicyScopeOptions{Tenant: "a"})
	tenantB, _ := NewPolicyScope(PolicyScopeOptions{Tenant: "b"})
	request.Header.Del("Authorization")
	requestA := request.Clone(mustPolicyScopeContext(t, tenantA))
	requestB := request.Clone(mustPolicyScopeContext(t, tenantB))
	keyA, _ := policy.cacheKey(requestA)
	keyB, _ := policy.cacheKey(requestB)
	if keyA == keyB {
		t.Fatalf("tenant cache keys matched: %q", keyA)
	}
}

func TestPolicyScopeRejectsMalformedPolicyAndState(t *testing.T) {
	t.Parallel()

	for _, options := range []PolicyScopeOptions{
		{Tenant: "bad\nvalue"},
		{Account: strings.Repeat("a", maximumPolicyScopeValueBytes+1)},
		{Custom: map[string]string{"": "value"}},
		{Custom: map[string]string{"bad name": "value"}},
		{Custom: map[string]string{"region": ""}},
	} {
		if _, err := NewPolicyScope(options); !errors.Is(err, ErrInvalidPolicyScope) {
			t.Fatalf("invalid scope %#v error = %v", options, err)
		}
	}
	if _, err := CustomScopeDimension(""); !errors.Is(err, ErrInvalidPolicyScope) {
		t.Fatalf("empty custom dimension = %v", err)
	}
	var nilContext context.Context
	if _, err := WithPolicyScope(nilContext, PolicyScope{}); !errors.Is(err, ErrInvalidPolicyScope) {
		t.Fatalf("nil context = %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test/", nil)
	for _, resource := range []PolicyResource{PolicyResource(255)} {
		if _, err := ResolvePolicyScope(request, resource); !errors.Is(err, ErrInvalidPolicyScope) {
			t.Fatalf("invalid resource = %v", err)
		}
	}
	if _, err := ResolvePolicyScope(nil, PolicyResourceCache); !errors.Is(err, ErrInvalidPolicyScope) {
		t.Fatalf("nil request = %v", err)
	}
	if _, err := ResolvePolicyScope(request, PolicyResourceCache, ScopeDimension("unknown")); !errors.Is(err, ErrInvalidPolicyScope) {
		t.Fatalf("invalid dimension = %v", err)
	}
	if _, ok := PolicyScopeFromContext(context.Background()); ok {
		t.Fatal("empty context returned a scope")
	}
	if _, ok := PolicyScopeFromContext(nilContext); ok {
		t.Fatal("nil context returned a scope")
	}
	invalidContext := context.WithValue(context.Background(), policyScopeContextKey{}, PolicyScope{})
	if _, ok := PolicyScopeFromContext(invalidContext); ok {
		t.Fatal("invalid context returned a scope")
	}
	if _, err := ResolvePolicyScope(request, PolicyResourceCache, ScopeOrigin, ScopeOrigin); !errors.Is(err, ErrInvalidPolicyScope) {
		t.Fatalf("duplicate dimensions = %v", err)
	}
	relative, _ := http.NewRequest(http.MethodGet, "https://api.example.test/", nil)
	relative.URL.Scheme = ""
	if _, err := ResolvePolicyScope(relative, PolicyResourceTransport); !errors.Is(err, ErrInvalidPolicyScope) {
		t.Fatalf("invalid origin = %v", err)
	}
	missingHost, _ := http.NewRequest(http.MethodGet, "https://api.example.test/", nil)
	missingHost.URL.Host = ""
	if _, err := ResolvePolicyScope(missingHost, PolicyResourceTransport, ScopeHost); !errors.Is(err, ErrInvalidPolicyScope) {
		t.Fatalf("invalid host = %v", err)
	}
	if _, err := resolveScopeDimension(request, PolicyScope{}, ScopeDimension("invalid")); !errors.Is(err, ErrInvalidPolicyScope) {
		t.Fatalf("internal invalid dimension = %v", err)
	}
	if dimensions := defaultScopeDimensions(PolicyResource(255)); dimensions != nil {
		t.Fatalf("invalid defaults = %#v", dimensions)
	}
	for resource := PolicyResourceTransport; resource < policyResourceCount; resource++ {
		if _, err := ResolvePolicyScope(request, resource); err != nil {
			t.Fatalf("resource %d defaults = %v", resource, err)
		}
	}
	if key, err := ResolvePolicyScope(request, PolicyResourceTransport, ScopeHost); err != nil || len(key.Dimensions()) != 1 {
		t.Fatalf("host scope = %#v, %v", key, err)
	}
	if _, err := (&cachePolicy{namespace: "scope"}).cacheKey(nil); !errors.Is(err, ErrInvalidPolicyScope) {
		t.Fatalf("cache scope error = %v", err)
	}
	if _, err := (&cachePolicy{namespace: "scope"}).cacheKeyFor(http.MethodGet, nil); !errors.Is(err, ErrInvalidPolicyScope) {
		t.Fatalf("cache scoped material error = %v", err)
	}
	if (&PolicyScopeKey{}).String() == "" {
		t.Fatal("zero scope key rendered empty text")
	}
}

func mustPolicyScopeContext(t *testing.T, scope PolicyScope) context.Context {
	t.Helper()
	ctx, err := WithPolicyScope(context.Background(), scope)
	if err != nil {
		t.Fatalf("attach scope: %v", err)
	}
	return ctx
}
