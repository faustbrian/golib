package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func TestAAAACutoverCompletesHappyPathWithinBound(t *testing.T) {
	client := internalClient(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/_aliases" {
			return internalResponse(http.StatusOK, `{"acknowledged":true}`), nil
		}
		return internalResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
	}, nil, LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }))
	client.lifecycle.CutoverGuard = LifecycleCutoverGuardFunc(func(_ context.Context, _ LifecycleCutoverRequest, operation func() error) error {
		return operation()
	})

	type outcome struct {
		report search.VerificationReport
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		report, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
		completed <- outcome{report: report, err: err}
	}()
	select {
	case result := <-completed:
		if result.err != nil || !result.report.Verified || result.report.SourceCount != 2 || result.report.TargetCount != 2 || result.report.Drift != 0 {
			t.Fatalf("CutoverAlias() report/error = %#v/%v", result.report, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CutoverAlias() did not complete within two seconds")
	}
}

func TestAAAACutoverRejectsOverlappingAliasGenerationsBeforeAuthorization(t *testing.T) {
	authorizations := 0
	denied := errors.New("authorization reached")
	client := internalClient(t, nil, nil,
		LifecycleAuthorizerFunc(func(context.Context, string, []string) error {
			authorizations++
			return denied
		}))

	for _, test := range []struct {
		name   string
		alias  string
		source string
		target string
	}{
		{name: "same generation", alias: "events-read", source: "events-v1", target: "events-v1"},
		{name: "alias is source", alias: "events-v1", source: "events-v1", target: "events-v2"},
		{name: "alias is target", alias: "events-v2", source: "events-v1", target: "events-v2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := client.CutoverAlias(t.Context(), "tenant", test.alias, test.source, test.target, "definition-v2"); !errors.Is(err, ErrUnsafeIndexTarget) {
				t.Fatalf("CutoverAlias() error = %v, want ErrUnsafeIndexTarget", err)
			}
		})
	}
	if authorizations != 0 {
		t.Fatalf("lifecycle authorizations = %d, want zero", authorizations)
	}
}

func TestAAAACutoverAuthorizationSuccessRequiresGuard(t *testing.T) {
	client := internalClient(t, nil, nil,
		LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }))

	if _, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2"); !errors.Is(err, ErrLifecycleCutoverGuardRequired) {
		t.Fatalf("CutoverAlias() error = %v, want ErrLifecycleCutoverGuardRequired", err)
	}
}

func TestAAAACleanupAcceptsMigrationIDAtByteLimit(t *testing.T) {
	denied := errors.New("authorization reached")
	client := internalClient(t, nil, nil,
		LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return denied }))
	request := cleanupBranchRequest()
	request.MigrationID = strings.Repeat("m", search.DefaultLimits().MaxIDBytes)

	if err := client.CleanupIndex(t.Context(), request); !errors.Is(err, ErrLifecycleDenied) {
		t.Fatalf("CleanupIndex() error = %v, want ErrLifecycleDenied", err)
	}
}

func TestAAAACleanupCompletesHappyPathWithinBound(t *testing.T) {
	deletions := 0
	client := internalClient(t, func(*http.Request) (*http.Response, error) {
		deletions++
		return internalResponse(http.StatusOK, `{"acknowledged":true}`), nil
	}, nil, LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }))
	client.lifecycle.CleanupGuard = LifecycleCleanupGuardFunc(func(_ context.Context, _ search.LifecycleCleanupRequest, operation func() error) error {
		return operation()
	})

	completed := make(chan error, 1)
	go func() { completed <- client.CleanupIndex(t.Context(), cleanupBranchRequest()) }()
	select {
	case err := <-completed:
		if err != nil || deletions != 1 {
			t.Fatalf("CleanupIndex() error/deletions = %v/%d, want nil/1", err, deletions)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CleanupIndex() happy path did not complete within two seconds")
	}
}

func TestAAAACleanupRejectsRepeatedGuardCallbackWithinBound(t *testing.T) {
	deletions := 0
	client := internalClient(t, func(*http.Request) (*http.Response, error) {
		deletions++
		return internalResponse(http.StatusOK, `{"acknowledged":true}`), nil
	}, nil, LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }))
	client.lifecycle.CleanupGuard = LifecycleCleanupGuardFunc(func(_ context.Context, _ search.LifecycleCleanupRequest, operation func() error) error {
		firstErr := operation()
		if deletions != 1 {
			return errors.New("first cleanup callback did not own deletion")
		}
		_ = operation()
		return firstErr
	})

	completed := make(chan error, 1)
	go func() { completed <- client.CleanupIndex(t.Context(), cleanupBranchRequest()) }()
	select {
	case err := <-completed:
		if !errors.Is(err, ErrLifecycleCleanupGuardRejected) || deletions != 1 {
			t.Fatalf("CleanupIndex() error/deletions = %v/%d, want guard rejection/1", err, deletions)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CleanupIndex() did not reject repeated callback within two seconds")
	}
}

func TestAAAALifecycleMutationRejectsRepeatedCallbackWithinBound(t *testing.T) {
	operations := 0
	client := internalClient(t, nil, nil,
		LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }))
	client.lifecycle.MutationGuard = LifecycleMutationGuardFunc(func(_ context.Context, _ LifecycleMutationRequest, operation func() error) error {
		firstErr := operation()
		if operations != 1 {
			return errors.New("first mutation callback did not own operation")
		}
		_ = operation()
		return firstErr
	})

	completed := make(chan error, 1)
	go func() {
		completed <- client.withLifecycleMutation(t.Context(), LifecycleMutationRequest{
			Tenant: "tenant", Operation: OperationCreateIndex, Resources: []string{"events-v1"},
		}, func(context.Context) error {
			operations++
			return nil
		})
	}()
	select {
	case err := <-completed:
		if !errors.Is(err, ErrLifecycleMutationGuardRejected) || operations != 1 {
			t.Fatalf("withLifecycleMutation() error/operations = %v/%d, want guard rejection/1", err, operations)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("withLifecycleMutation() did not reject repeated callback within two seconds")
	}
}

func TestAAAAPointInTimeReleasePermitsIdentifierReacquisition(t *testing.T) {
	now := time.Now()
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 4096)
	if err != nil {
		t.Fatal(err)
	}
	tracker := newPointInTimeTracker(codec, 1)
	expiresAt := now.Add(time.Minute)
	first, err := tracker.acquire("pit", expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	tracker.release(first)
	second, err := tracker.acquire("pit", expiresAt)
	if err != nil || second == first {
		t.Fatalf("reacquire released PIT = %#v/%v", second, err)
	}
	tracker.release(second)
}

func TestAAAAProjectionWildcardMatcherTracksEveryOrderedSegment(t *testing.T) {
	for _, test := range []struct {
		pattern string
		field   string
		want    bool
	}{
		{pattern: "a**b*c", field: "aXXc", want: false},
		{pattern: "*a*b", field: "ab", want: true},
		{pattern: "*ab*b", field: "ab", want: false},
	} {
		if got := projectionPatternMatches(test.pattern, test.field); got != test.want {
			t.Fatalf("projectionPatternMatches(%q, %q) = %t, want %t", test.pattern, test.field, got, test.want)
		}
	}
}

func TestClientOwnsExplicitProxyURLConfiguration(t *testing.T) {
	proxy, err := url.Parse("https://proxy-a.example")
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		Endpoints: []string{"https://search.example"}, RequestTimeout: time.Second,
		MaximumResponseBytes: 4 << 10, Transport: &http.Transport{},
		Proxy: ProxyPolicy{Mode: ProxyExplicit, URL: proxy},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	proxy.Host = "proxy-b.example"
	proxy.User = url.UserPassword("backend-user", "backend-secret")
	owned, ok := client.transport.next.(*ownedTransport)
	if !ok {
		t.Fatalf("configured transport wrapper = %T", client.transport.next)
	}
	configured, ok := owned.next.(*http.Transport)
	if !ok {
		t.Fatalf("configured transport = %T", owned.next)
	}
	selected, err := configured.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "search.example"}})
	if err != nil {
		t.Fatal(err)
	}
	if selected == nil || selected.Host != "proxy-a.example" || selected.User != nil {
		t.Fatalf("selected proxy = %#v, want owned credential-free proxy-a.example", selected)
	}
}

func TestInfoBoundsAndSanitizesReturnedIdentityFields(t *testing.T) {
	infoBody := func(node, cluster, uuid, version string) string {
		body, err := json.Marshal(map[string]any{
			"name": node, "cluster_name": cluster, "cluster_uuid": uuid,
			"version": map[string]string{"number": version},
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	exact := strings.Repeat("x", 1_024)
	client := internalClient(t, routeBody(infoBody(exact, exact, exact, exact), http.StatusOK), nil, nil)
	if _, err := client.Info(t.Context()); err != nil {
		t.Fatalf("Info(exact identity limit) error = %v", err)
	}

	oversized := strings.Repeat("x", 1_025)
	for name, body := range map[string]string{
		"node":         infoBody(oversized, "cluster", "uuid", "3.8.0"),
		"cluster":      infoBody("node", oversized, "uuid", "3.8.0"),
		"cluster UUID": infoBody("node", "cluster", oversized, "3.8.0"),
		"version":      infoBody("node", "cluster", "uuid", oversized),
		"control":      infoBody("node\nforged", "cluster", "uuid", "3.8.0"),
	} {
		t.Run(name, func(t *testing.T) {
			client := internalClient(t, routeBody(body, http.StatusOK), nil, nil)
			if _, err := client.Info(t.Context()); !errors.Is(err, ErrMalformedResponse) {
				t.Fatalf("Info() error = %v, want ErrMalformedResponse", err)
			}
		})
	}
}

func TestAAAMutationLifecycleVerificationAcceptsValidDriftBounds(t *testing.T) {
	requests := 0
	client := internalClient(t, func(*http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return internalResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}
		return internalResponse(http.StatusOK, `{"count":3,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
	}, nil, LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }))
	client.lifecycle.Verifier = LifecycleVerifierFunc(func(_ context.Context, request LifecycleVerificationRequest) (LifecycleVerificationResult, error) {
		return LifecycleVerificationResult{
			TargetFingerprint: request.ExpectedTargetFingerprint,
			Drift:             1,
		}, nil
	})

	report, err := client.VerifyIndex(t.Context(), "tenant", "events-v1", "events-v2", "definition-v2")
	if err != nil || report.Drift != 1 || report.SourceCount != 2 || report.TargetCount != 3 {
		t.Fatalf("VerifyIndex() report/error = %#v/%v", report, err)
	}
}

func TestLifecycleVerificationRejectsSameGenerationBeforeAuthorization(t *testing.T) {
	authorizations := 0
	client := internalClient(t, routeBody(`{"acknowledged":true}`, http.StatusOK), nil,
		LifecycleAuthorizerFunc(func(context.Context, string, []string) error {
			authorizations++
			return nil
		}))

	if _, err := client.VerifyIndex(t.Context(), "tenant", "events-v1", "events-v1", "definition-v1"); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatalf("VerifyIndex(same generation) error = %v, want ErrUnsafeIndexTarget", err)
	}
	if authorizations != 0 {
		t.Fatalf("lifecycle authorizations = %d, want zero", authorizations)
	}
}

func TestLifecycleCleanupRejectsAliasGenerationCollisionsBeforeAuthorization(t *testing.T) {
	authorizations := 0
	client := internalClient(t, routeBody(`{"acknowledged":true}`, http.StatusOK), nil,
		LifecycleAuthorizerFunc(func(context.Context, string, []string) error {
			authorizations++
			return nil
		}))

	base := search.LifecycleCleanupRequest{
		MigrationID: "migration-1", Tenant: "tenant", Alias: "events-read",
		ActiveIndex: "events-v2", ActiveFingerprint: "definition-v2",
		InactiveIndex: "events-v1", InactiveFingerprint: "definition-v1",
	}
	for _, alias := range []string{base.ActiveIndex, base.InactiveIndex} {
		request := base
		request.Alias = alias
		if err := client.CleanupIndex(t.Context(), request); !errors.Is(err, ErrUnsafeIndexTarget) {
			t.Fatalf("CleanupIndex(alias %q) error = %v, want ErrUnsafeIndexTarget", alias, err)
		}
	}
	if authorizations != 0 {
		t.Fatalf("lifecycle authorizations = %d, want zero", authorizations)
	}
}
