package httpclient

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestBuiltInPolicyProfilesAreFiniteAndVersioned(t *testing.T) {
	t.Parallel()

	profiles := []PolicyProfileID{
		PolicyProfileInteractiveV1,
		PolicyProfileBatchV1,
		PolicyProfileStreamingV1,
		PolicyProfileWebhookDeliveryV1,
	}
	for _, id := range profiles {
		resolved, err := ResolvePolicy(id, PolicyOverrides{}, PolicyOverrides{})
		if err != nil {
			t.Fatalf("resolve %q: %v", id, err)
		}
		values := resolved.Values()
		if resolved.Profile() != id || resolved.Version() != 1 ||
			values.OperationTimeout <= 0 || values.RetryMaximumAttempts <= 0 ||
			values.RetryMaximumElapsed <= 0 || values.PoolConcurrency <= 0 ||
			values.PoolMaximumElapsed <= 0 || values.TransportMaximumConnections <= 0 ||
			values.LimiterMaximumWait <= 0 || values.BreakerOpenTimeout <= 0 ||
			values.CacheMaximumBodyBytes <= 0 || values.BodyMaximumBytes <= 0 ||
			values.ShutdownTimeout <= 0 {
			t.Fatalf("profile %q is not finite: %#v", id, values)
		}
		for _, field := range policyFields() {
			if got := resolved.Provenance(field); got != PolicySourceProfile {
				t.Fatalf("profile %q field %q source = %q", id, field, got)
			}
		}
	}
}

func TestPolicyOverridePrecedenceAndProvenance(t *testing.T) {
	t.Parallel()

	clientTimeout := 12 * time.Second
	clientConcurrency := 7
	requestTimeout := 3 * time.Second
	requestBody := int64(1024)
	resolved, err := ResolvePolicy(
		PolicyProfileBatchV1,
		PolicyOverrides{
			OperationTimeout: &clientTimeout,
			PoolConcurrency:  &clientConcurrency,
		},
		PolicyOverrides{
			OperationTimeout: &requestTimeout,
			BodyMaximumBytes: &requestBody,
		},
	)
	if err != nil {
		t.Fatalf("resolve policy: %v", err)
	}
	values := resolved.Values()
	if values.OperationTimeout != requestTimeout || values.PoolConcurrency != clientConcurrency ||
		values.BodyMaximumBytes != requestBody {
		t.Fatalf("resolved values = %#v", values)
	}
	if resolved.Provenance(PolicyFieldOperationTimeout) != PolicySourceRequest ||
		resolved.Provenance(PolicyFieldPoolConcurrency) != PolicySourceClient ||
		resolved.Provenance(PolicyFieldRetryMaximumAttempts) != PolicySourceProfile {
		t.Fatalf("resolved provenance = %#v", resolved.ProvenanceSnapshot())
	}

	snapshot := resolved.ProvenanceSnapshot()
	snapshot[PolicyFieldOperationTimeout] = PolicySourceProfile
	if resolved.Provenance(PolicyFieldOperationTimeout) != PolicySourceRequest {
		t.Fatal("provenance snapshot mutated resolved policy")
	}
}

func TestRequestPolicyOverridesReachOperationsAndAttempts(t *testing.T) {
	t.Parallel()

	requestTimeout := 250 * time.Millisecond
	attachedTimeout := requestTimeout
	requestBody := int64(2048)
	ctx, err := WithPolicyOverrides(context.Background(), PolicyOverrides{
		OperationTimeout: &attachedTimeout,
		BodyMaximumBytes: &requestBody,
	})
	if err != nil {
		t.Fatalf("attach request overrides: %v", err)
	}
	attachedTimeout = time.Hour
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test", nil)

	client, err := New(Config{
		Profile: PolicyProfileInteractiveV1,
		Transport: roundTripFunc(func(attempt *http.Request) (*http.Response, error) {
			resolved, ok := ResolvedPolicyFromContext(attempt.Context())
			if !ok || resolved.Values().OperationTimeout != requestTimeout ||
				resolved.Values().BodyMaximumBytes != requestBody {
				t.Fatalf("attempt policy = %#v, %t", resolved.Values(), ok)
			}
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Header:     make(http.Header), Body: http.NoBody, Request: attempt,
			}, nil
		}),
		Middleware: []Middleware{mustProfileInspectionMiddleware(t, requestTimeout)},
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	inspection, err := client.InspectPolicy(request)
	if err != nil || inspection.Values().OperationTimeout != requestTimeout {
		t.Fatalf("inspect policy = %#v, %v", inspection.Values(), err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	_ = response.Body.Close()
}

func TestClientPolicyConfigDrivesTimeoutAndOwnedTransportPool(t *testing.T) {
	t.Parallel()

	timeout := 4 * time.Second
	connections := 17
	client, err := New(Config{
		Profile: PolicyProfileStreamingV1,
		Policy: PolicyOverrides{
			OperationTimeout:            &timeout,
			TransportMaximumConnections: &connections,
		},
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if client.HTTPClient().Timeout != timeout {
		t.Fatalf("client timeout = %v", client.HTTPClient().Timeout)
	}
	transport, ok := client.transport.(*http.Transport)
	if !ok || transport.MaxConnsPerHost != connections ||
		transport.MaxIdleConns != connections ||
		transport.MaxIdleConnsPerHost != defaultMaxIdleConnsPerHost {
		t.Fatalf("transport pool = %#v", client.transport)
	}
}

func TestLegacyTimeoutOverridesPolicyAndOverrideInputsAreSnapshotted(t *testing.T) {
	t.Parallel()

	policyTimeout := 9 * time.Second
	legacyTimeout := 2 * time.Second
	client, err := New(Config{
		Policy:  PolicyOverrides{OperationTimeout: &policyTimeout},
		Timeout: legacyTimeout,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNoContent, Header: make(http.Header),
				Body: http.NoBody, Request: request,
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct legacy-timeout client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	policyTimeout = time.Hour
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	resolved, err := client.InspectPolicy(request)
	if err != nil || resolved.Values().OperationTimeout != legacyTimeout ||
		resolved.Provenance(PolicyFieldOperationTimeout) != PolicySourceClient {
		t.Fatalf("legacy timeout policy = %#v, %v", resolved, err)
	}
	if _, err := client.InspectPolicy(nil); !errors.Is(err, ErrNilRequest) {
		t.Fatalf("nil inspection error = %v", err)
	}
}

func TestPolicyProfilesRejectUnknownAndInvalidOverrides(t *testing.T) {
	t.Parallel()

	zeroDuration := time.Duration(0)
	excessiveDuration := maximumProfileDuration + time.Nanosecond
	zeroInt := 0
	excessiveAttempts := maximumRetryAttempts + 1
	excessiveConcurrency := maximumPoolConcurrency + 1
	excessiveConnections := maximumPoolPending + 1
	zeroBytes := int64(0)
	excessiveBytes := maximumProfileBodyBytes + 1
	invalid := []PolicyOverrides{
		{OperationTimeout: &zeroDuration},
		{OperationTimeout: &excessiveDuration},
		{RetryMaximumAttempts: &zeroInt},
		{RetryMaximumAttempts: &excessiveAttempts},
		{RetryMaximumElapsed: &zeroDuration},
		{PoolConcurrency: &zeroInt},
		{PoolConcurrency: &excessiveConcurrency},
		{PoolMaximumElapsed: &zeroDuration},
		{TransportMaximumConnections: &zeroInt},
		{TransportMaximumConnections: &excessiveConnections},
		{LimiterMaximumWait: &zeroDuration},
		{BreakerOpenTimeout: &zeroDuration},
		{CacheMaximumBodyBytes: &zeroBytes},
		{CacheMaximumBodyBytes: &excessiveBytes},
		{BodyMaximumBytes: &zeroBytes},
		{ShutdownTimeout: &zeroDuration},
	}
	for _, overrides := range invalid {
		if _, err := ResolvePolicy(PolicyProfileInteractiveV1, overrides, PolicyOverrides{}); !errors.Is(err, ErrInvalidPolicyProfile) {
			t.Fatalf("invalid overrides %#v error = %v", overrides, err)
		}
	}
	if _, err := ResolvePolicy(PolicyProfileID("unknown/v1"), PolicyOverrides{}, PolicyOverrides{}); !errors.Is(err, ErrInvalidPolicyProfile) {
		t.Fatalf("unknown profile error = %v", err)
	}
	if _, err := New(Config{Policy: PolicyOverrides{OperationTimeout: &zeroDuration}}); !errors.Is(err, ErrInvalidPolicyProfile) {
		t.Fatalf("invalid client policy error = %v", err)
	}
	var nilContext context.Context
	if _, err := WithPolicyOverrides(nilContext, PolicyOverrides{}); !errors.Is(err, ErrInvalidPolicyProfile) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := WithPolicyOverrides(context.Background(), PolicyOverrides{OperationTimeout: &zeroDuration}); !errors.Is(err, ErrInvalidPolicyProfile) {
		t.Fatalf("invalid context overrides error = %v", err)
	}
	if _, ok := ResolvedPolicyFromContext(context.Background()); ok {
		t.Fatal("empty context returned resolved policy")
	}
	if _, ok := ResolvedPolicyFromContext(nilContext); ok {
		t.Fatal("nil context returned resolved policy")
	}
	if overrides := requestPolicyOverrides(nilContext); overrides.OperationTimeout != nil {
		t.Fatalf("nil request overrides = %#v", overrides)
	}
	invalidContext := context.WithValue(
		context.Background(),
		policyOverridesContextKey{},
		PolicyOverrides{OperationTimeout: &zeroDuration},
	)
	request, _ := http.NewRequestWithContext(invalidContext, http.MethodGet, "https://example.test", nil)
	client, err := New(Config{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid request policy reached transport")
		return nil, nil
	})})
	if err != nil {
		t.Fatalf("construct boundary client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Do(request); !errors.Is(err, ErrInvalidPolicyProfile) {
		t.Fatalf("invalid request policy error = %v", err)
	}
}

func mustProfileInspectionMiddleware(t *testing.T, timeout time.Duration) Middleware {
	t.Helper()
	middleware, err := NewRequestMiddleware(MiddlewareOptions{
		Name: "profile-inspection", Layer: MiddlewareRequest,
	}, func(request *http.Request, next Next) (*http.Response, error) {
		resolved, ok := ResolvedPolicyFromContext(request.Context())
		if !ok || resolved.Values().OperationTimeout != timeout {
			t.Fatalf("operation policy = %#v, %t", resolved.Values(), ok)
		}
		return next(request)
	})
	if err != nil {
		t.Fatalf("construct inspection middleware: %v", err)
	}
	return middleware
}
