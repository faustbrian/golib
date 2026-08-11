package opensearch_test

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestVerifyIndexRequiresSemanticVerifierAfterCountPreflight(t *testing.T) {
	t.Parallel()

	requests := 0
	client := newVerificationClient(t, nil, roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return countResponse(2), nil
	}))
	report, err := client.VerifyIndex(t.Context(), "tenant-a", "events-v1", "events-v2", "definition-v2")
	if !errors.Is(err, adapter.ErrLifecycleVerifierRequired) {
		t.Fatalf("VerifyIndex() error = %v, want ErrLifecycleVerifierRequired", err)
	}
	if report.SourceCount != 2 || report.TargetCount != 2 || report.Verified || report.Drift != 0 {
		t.Fatalf("VerifyIndex() report = %#v", report)
	}
	if requests != 2 {
		t.Fatalf("VerifyIndex() count requests = %d, want 2", requests)
	}
}

func TestVerifyIndexUsesSemanticDriftWhenCountsAreEqual(t *testing.T) {
	t.Parallel()

	wantRequest := adapter.LifecycleVerificationRequest{
		Tenant: "tenant-a", Source: "events-v1", Target: "events-v2",
		SourceCount: 2, TargetCount: 2, ExpectedTargetFingerprint: "definition-v2",
	}
	var gotRequest adapter.LifecycleVerificationRequest
	client := newVerificationClient(t, adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
		gotRequest = request
		return adapter.LifecycleVerificationResult{Drift: 1, TargetFingerprint: request.ExpectedTargetFingerprint}, nil
	}), roundTripFunc(func(*http.Request) (*http.Response, error) {
		return countResponse(2), nil
	}))
	report, err := client.VerifyIndex(t.Context(), "tenant-a", "events-v1", "events-v2", "definition-v2")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotRequest, wantRequest) {
		t.Fatalf("verification request = %#v, want %#v", gotRequest, wantRequest)
	}
	if report.SourceCount != 2 || report.TargetCount != 2 || report.Verified || report.Drift != 1 {
		t.Fatalf("VerifyIndex() report = %#v", report)
	}
}

func TestVerifyIndexRejectsInvalidOrFailedSemanticVerification(t *testing.T) {
	t.Parallel()

	verificationErr := errors.New("semantic comparison failed")
	tests := []struct {
		name         string
		sourceCount  int
		targetCount  int
		verification adapter.LifecycleVerificationResult
		verifierErr  error
		wantErr      error
	}{
		{name: "missing live target fingerprint", sourceCount: 2, targetCount: 2, verification: adapter.LifecycleVerificationResult{}, wantErr: adapter.ErrLifecycleRejected},
		{name: "wrong live target fingerprint", sourceCount: 2, targetCount: 2, verification: adapter.LifecycleVerificationResult{TargetFingerprint: "definition-v1"}, wantErr: adapter.ErrLifecycleRejected},
		{name: "drift below count delta", sourceCount: 1, targetCount: 2, verification: adapter.LifecycleVerificationResult{TargetFingerprint: "definition-v2"}, wantErr: adapter.ErrLifecycleRejected},
		{name: "drift above record union", sourceCount: 2, targetCount: 2, verification: adapter.LifecycleVerificationResult{Drift: 5, TargetFingerprint: "definition-v2"}, wantErr: adapter.ErrLifecycleRejected},
		{name: "verifier failure", sourceCount: 2, targetCount: 2, verifierErr: verificationErr, wantErr: adapter.ErrLifecycleRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := 0
			client := newVerificationClient(t, adapter.LifecycleVerifierFunc(func(context.Context, adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
				return test.verification, test.verifierErr
			}), roundTripFunc(func(*http.Request) (*http.Response, error) {
				request++
				if request == 1 {
					return countResponse(test.sourceCount), nil
				}
				return countResponse(test.targetCount), nil
			}))
			report, err := client.VerifyIndex(t.Context(), "tenant-a", "events-v1", "events-v2", "definition-v2")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("VerifyIndex() error = %v, want %v", err, test.wantErr)
			}
			if test.verifierErr != nil && (errors.Is(err, test.verifierErr) || strings.Contains(err.Error(), test.verifierErr.Error())) {
				t.Fatalf("VerifyIndex() exposed verifier error: %v", err)
			}
			if report.SourceCount != uint64(test.sourceCount) || report.TargetCount != uint64(test.targetCount) || report.Verified {
				t.Fatalf("VerifyIndex() report = %#v", report)
			}
		})
	}
}

func TestVerifyIndexAcceptsExactAndOverflowingRecordUnions(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name                  string
		source, target, drift uint64
	}{
		{name: "exact union", source: 2, target: 3, drift: 5},
		{name: "overflowing union", source: math.MaxUint64, target: 1, drift: math.MaxUint64},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := 0
			client := newVerificationClient(t, adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
				return adapter.LifecycleVerificationResult{Drift: test.drift, TargetFingerprint: request.ExpectedTargetFingerprint}, nil
			}), roundTripFunc(func(*http.Request) (*http.Response, error) {
				request++
				if request == 1 {
					return countResponseUint(test.source), nil
				}
				return countResponseUint(test.target), nil
			}))

			report, err := client.VerifyIndex(t.Context(), "tenant-a", "events-v1", "events-v2", "definition-v2")
			if err != nil || report.Drift != test.drift || report.SourceCount != test.source || report.TargetCount != test.target {
				t.Fatalf("VerifyIndex() report/error = %#v/%v", report, err)
			}
		})
	}
}

func TestVerifyIndexRedactsVerifierFailuresAndPreservesCancellationClass(t *testing.T) {
	t.Parallel()

	private := errors.New("backend credential secret")
	for _, test := range []struct {
		name     string
		verifier error
		category adapter.FailureCategory
		want     error
	}{
		{name: "private failure", verifier: private, category: adapter.FailureRejected, want: adapter.ErrLifecycleRejected},
		{name: "cancelled", verifier: errors.Join(private, context.Canceled), category: adapter.FailureCancelled, want: context.Canceled},
		{name: "deadline", verifier: errors.Join(private, context.DeadlineExceeded), category: adapter.FailureCancelled, want: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := newVerificationClient(t, adapter.LifecycleVerifierFunc(func(context.Context, adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
				return adapter.LifecycleVerificationResult{}, test.verifier
			}), roundTripFunc(func(*http.Request) (*http.Response, error) {
				return countResponse(2), nil
			}))

			_, err := client.VerifyIndex(t.Context(), "tenant-a", "events-v1", "events-v2", "definition-v2")
			var failure *adapter.Failure
			if !errors.Is(err, test.want) || !errors.As(err, &failure) || failure.Operation != adapter.OperationVerifyIndex ||
				failure.Category != test.category || !failure.OutcomeKnown || errors.Is(err, private) || strings.Contains(err.Error(), "credential") {
				t.Fatalf("VerifyIndex() error = %v", err)
			}
		})
	}
}

func newVerificationClient(t *testing.T, verifier adapter.LifecycleVerifier, transport http.RoundTripper) *adapter.Client {
	t.Helper()

	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, Transport: transport,
		TransportOwnership: adapter.TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 4 << 10,
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }),
			Verifier:   verifier,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func countResponse(count int) *http.Response {
	return countResponseUint(uint64(count))
}

func countResponseUint(count uint64) *http.Response {
	body := `{"count":` + strconv.FormatUint(count, 10) + `,"_shards":{"total":1,"successful":1,"failed":0}}`
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
