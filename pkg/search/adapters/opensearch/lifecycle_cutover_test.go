package opensearch_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestCutoverAliasHoldsApplicationFenceAcrossFinalVerificationAndSwap(t *testing.T) {
	t.Parallel()

	fenced := false
	requests := make([]string, 0, 3)
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, request adapter.LifecycleCutoverRequest, operation func() error) error {
			if request.Tenant != "tenant-a" || request.Alias != "events-read" || request.Source != "events-v1" ||
				request.Target != "events-v2" || request.ExpectedTargetFingerprint != "definition-v2" {
				t.Fatalf("cutover request = %#v", request)
			}
			fenced = true
			defer func() { fenced = false }()
			return operation()
		}),
		adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
			if !fenced {
				t.Fatal("semantic verification ran outside the application write fence")
			}
			requests = append(requests, "verify")
			return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint}, nil
		}),
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if !fenced {
				t.Fatalf("%s %s ran outside the application write fence", request.Method, request.URL.Path)
			}
			switch request.URL.Path {
			case "/events-v1/_count":
				requests = append(requests, "count-source")
				return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
			case "/events-v2/_count":
				requests = append(requests, "count-target")
				return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
			case "/_aliases":
				requests = append(requests, "swap")
				return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
			default:
				t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
				return nil, errors.New("unexpected request")
			}
		}),
	)

	report, err := client.CutoverAlias(t.Context(), "tenant-a", "events-read", "events-v1", "events-v2", "definition-v2")
	if err != nil || !report.Verified || report.Drift != 0 || report.SourceCount != 2 || report.TargetCount != 2 {
		t.Fatalf("CutoverAlias() = %#v/%v", report, err)
	}
	if fenced {
		t.Fatal("application write fence remained active after cutover")
	}
	if got, want := strings.Join(requests, ","), "count-source,count-target,verify,swap"; got != want {
		t.Fatalf("cutover sequence = %q, want %q", got, want)
	}
}

func TestCutoverAliasRequiresFenceAndVerifiedTargetBeforeSwap(t *testing.T) {
	t.Parallel()

	t.Run("missing guard", func(t *testing.T) {
		t.Parallel()
		requests := 0
		client := newCutoverClient(t, nil,
			adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
				return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint}, nil
			}),
			roundTripFunc(func(*http.Request) (*http.Response, error) {
				requests++
				return cursorResponse(http.StatusOK, `{}`), nil
			}),
		)
		if _, err := client.CutoverAlias(t.Context(), "tenant-a", "events-read", "events-v1", "events-v2", "definition-v2"); !errors.Is(err, adapter.ErrLifecycleCutoverGuardRequired) {
			t.Fatalf("CutoverAlias() error = %v", err)
		}
		if requests != 0 {
			t.Fatalf("missing guard dispatched %d requests", requests)
		}
	})

	t.Run("semantic drift", func(t *testing.T) {
		t.Parallel()
		swaps := 0
		client := newCutoverClient(t,
			adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
				return operation()
			}),
			adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
				return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint, Drift: 1}, nil
			}),
			roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/_aliases" {
					swaps++
				}
				return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
			}),
		)
		report, err := client.CutoverAlias(t.Context(), "tenant-a", "events-read", "events-v1", "events-v2", "definition-v2")
		if !errors.Is(err, adapter.ErrLifecycleCutoverUnverified) || report.Verified || report.Drift != 1 {
			t.Fatalf("CutoverAlias() = %#v/%v", report, err)
		}
		if swaps != 0 {
			t.Fatalf("unverified target dispatched %d alias swaps", swaps)
		}
	})
}

func TestLifecycleRejectsAliasAndPhysicalGenerationCollisionsBeforeDispatch(t *testing.T) {
	t.Parallel()

	requests := 0
	client := newCutoverClient(t, nil, nil, roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return cursorResponse(http.StatusOK, `{}`), nil
	}))
	for name, operation := range map[string]func() error{
		"cutover source": func() error {
			_, err := client.CutoverAlias(t.Context(), "tenant", "events-v1", "events-v1", "events-v2", "definition-v2")
			return err
		},
		"cutover target": func() error {
			_, err := client.CutoverAlias(t.Context(), "tenant", "events-v2", "events-v1", "events-v2", "definition-v2")
			return err
		},
		"swap source": func() error {
			return client.SwapAlias(t.Context(), "tenant", "events-v1", "events-v1", "events-v2")
		},
		"swap target": func() error {
			return client.SwapAlias(t.Context(), "tenant", "events-v2", "events-v1", "events-v2")
		},
		"add": func() error {
			return client.AddAlias(t.Context(), "tenant", "events-v1", "events-v1", true)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := operation(); !errors.Is(err, adapter.ErrUnsafeIndexTarget) {
				t.Fatalf("operation error = %v, want ErrUnsafeIndexTarget", err)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("colliding lifecycle names dispatched %d requests", requests)
	}
}

func TestCutoverAliasHidesGuardFailureDetails(t *testing.T) {
	t.Parallel()

	private := errors.New("private fence credential")
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(context.Context, adapter.LifecycleCutoverRequest, func() error) error {
			return private
		}),
		nil,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("failed guard reached transport")
			return nil, errors.New("unexpected transport")
		}),
	)
	_, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
	if !errors.Is(err, adapter.ErrLifecycleCutoverGuardRejected) || errors.Is(err, private) || strings.Contains(err.Error(), "credential") {
		t.Fatalf("CutoverAlias() error = %v", err)
	}
}

func TestCutoverAliasRejectsLateGuardCallbackWithoutDispatch(t *testing.T) {
	t.Parallel()

	var late func() error
	requests := 0
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			late = operation
			return nil
		}),
		adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
			return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint}, nil
		}),
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}),
	)

	report, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
	var failure *adapter.Failure
	if !errors.Is(err, adapter.ErrLifecycleCutoverGuardRejected) || !errors.As(err, &failure) ||
		failure.Operation != adapter.OperationSwapAlias || failure.Category != adapter.FailureRejected || !failure.OutcomeKnown || report.Verified {
		t.Fatalf("CutoverAlias() report/error = %#v/%v", report, err)
	}
	if late == nil {
		t.Fatal("guard did not receive cutover callback")
	}
	if err := late(); !errors.Is(err, adapter.ErrLifecycleCutoverGuardRejected) {
		t.Fatalf("late callback error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("late callback dispatched %d requests", requests)
	}
}

func TestCutoverAliasRejectsGuardReturningDuringCallbackWithoutSwap(t *testing.T) {
	t.Parallel()

	verificationStarted := make(chan struct{})
	continueVerification := make(chan struct{})
	callbackDone := make(chan error, 1)
	callbackFinished := make(chan struct{})
	swaps := 0
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			go func() {
				callbackDone <- operation()
				close(callbackFinished)
			}()
			select {
			case <-verificationStarted:
			case <-callbackFinished:
			}
			return nil
		}),
		adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
			close(verificationStarted)
			<-continueVerification
			return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint}, nil
		}),
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/_aliases" {
				swaps++
				return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
			}
			return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}),
	)

	type cutoverCall struct {
		report search.VerificationReport
		err    error
	}
	cutoverDone := make(chan cutoverCall, 1)
	go func() {
		report, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
		cutoverDone <- cutoverCall{report: report, err: err}
	}()
	select {
	case result := <-cutoverDone:
		close(continueVerification)
		<-callbackDone
		t.Fatalf("CutoverAlias returned before its started callback: %#v/%v", result.report, result.err)
	case <-time.After(100 * time.Millisecond):
	}
	close(continueVerification)
	if err := <-callbackDone; !errors.Is(err, adapter.ErrLifecycleCutoverGuardRejected) {
		t.Fatalf("concurrent callback error = %v", err)
	}
	result := <-cutoverDone
	var failure *adapter.Failure
	if !errors.Is(result.err, adapter.ErrLifecycleCutoverGuardRejected) || !errors.As(result.err, &failure) ||
		failure.Operation != adapter.OperationSwapAlias || failure.Category != adapter.FailureRejected || !failure.OutcomeKnown || !result.report.Verified {
		t.Fatalf("CutoverAlias() report/error = %#v/%v", result.report, result.err)
	}
	if swaps != 0 {
		t.Fatalf("callback outlived guard and dispatched %d alias swaps", swaps)
	}
}

func TestCutoverAliasReportsGuardViolationWhenInternalCancellationStopsVerifier(t *testing.T) {
	t.Parallel()

	verificationStarted := make(chan struct{})
	callbackDone := make(chan error, 1)
	callbackFinished := make(chan struct{})
	swaps := 0
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			go func() {
				callbackDone <- operation()
				close(callbackFinished)
			}()
			select {
			case <-verificationStarted:
			case <-callbackFinished:
			}
			return nil
		}),
		adapter.LifecycleVerifierFunc(func(ctx context.Context, _ adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
			close(verificationStarted)
			<-ctx.Done()
			return adapter.LifecycleVerificationResult{}, ctx.Err()
		}),
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/_aliases" {
				swaps++
				return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
			}
			return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}),
	)

	report, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
	var failure *adapter.Failure
	if !errors.Is(err, adapter.ErrLifecycleCutoverGuardRejected) || errors.Is(err, context.Canceled) || !errors.As(err, &failure) ||
		failure.Operation != adapter.OperationSwapAlias || failure.Category != adapter.FailureRejected || !failure.OutcomeKnown ||
		report.Verified || t.Context().Err() != nil || swaps != 0 {
		t.Fatalf("CutoverAlias() report/error/context/swaps = %#v/%v/%v/%d", report, err, t.Context().Err(), swaps)
	}
	if callbackErr := <-callbackDone; !errors.Is(callbackErr, context.Canceled) {
		t.Fatalf("callback error = %v", callbackErr)
	}
}

func TestCutoverAliasRejectsRepeatedGuardCallback(t *testing.T) {
	t.Parallel()

	var repeatedErr error
	requests := 0
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			if err := operation(); err != nil {
				return err
			}
			repeatedErr = operation()
			return nil
		}),
		adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
			return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint}, nil
		}),
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			if request.URL.Path == "/_aliases" {
				return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
			}
			return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}),
	)

	report, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
	var failure *adapter.Failure
	if !errors.Is(err, adapter.ErrLifecycleCutoverGuardRejected) || !errors.As(err, &failure) ||
		failure.Operation != adapter.OperationSwapAlias || failure.Category != adapter.FailureRejected || !failure.OutcomeKnown || !report.Verified {
		t.Fatalf("CutoverAlias() report/error = %#v/%v", report, err)
	}
	if !errors.Is(repeatedErr, adapter.ErrLifecycleCutoverGuardRejected) || requests != 3 {
		t.Fatalf("repeated callback error/requests = %v/%d", repeatedErr, requests)
	}
}

func TestCutoverAliasConcurrentRepeatedGuardCallbackDoesNotBlockPrimary(t *testing.T) {
	t.Parallel()

	verifierEntered := make(chan struct{})
	releaseVerifier := make(chan struct{})
	primaryDone := make(chan error, 1)
	var repeatedErr error
	requests := 0
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			go func() { primaryDone <- operation() }()
			<-verifierEntered
			repeatedErr = operation()
			close(releaseVerifier)
			return <-primaryDone
		}),
		adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
			close(verifierEntered)
			<-releaseVerifier
			return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint}, nil
		}),
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			if request.URL.Path == "/_aliases" {
				return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
			}
			return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}),
	)

	type outcome struct {
		report search.VerificationReport
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		report, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
		finished <- outcome{report: report, err: err}
	}()

	select {
	case result := <-finished:
		var failure *adapter.Failure
		if !errors.Is(result.err, adapter.ErrLifecycleCutoverGuardRejected) || !errors.As(result.err, &failure) ||
			failure.Operation != adapter.OperationSwapAlias || failure.Category != adapter.FailureRejected ||
			!failure.OutcomeKnown || !result.report.Verified {
			t.Fatalf("CutoverAlias() report/error = %#v/%v", result.report, result.err)
		}
		if !errors.Is(repeatedErr, adapter.ErrLifecycleCutoverGuardRejected) || requests != 3 {
			t.Fatalf("repeated callback error/requests = %v/%d", repeatedErr, requests)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CutoverAlias blocked after a concurrent repeated guard callback")
	}
}

func TestCutoverAliasPreservesFirstFailureAndReportsPostFailureGuardViolation(t *testing.T) {
	t.Parallel()

	private := errors.New("private fence credential")
	for _, test := range []struct {
		name string
	}{
		{name: "repeated callback"},
		{name: "distinct guard error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			requests := 0
			var firstErr, repeatedErr error
			client := newCutoverClient(t,
				adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
					firstErr = operation()
					if test.name == "repeated callback" {
						repeatedErr = operation()
						return firstErr
					}
					return private
				}),
				nil,
				roundTripFunc(func(*http.Request) (*http.Response, error) {
					requests++
					return cursorResponse(http.StatusServiceUnavailable, `{"error":{"type":"rejected_execution_exception"},"status":503}`), nil
				}),
			)

			_, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
			var failure *adapter.Failure
			if !errors.Is(err, adapter.ErrLifecycleCutoverGuardRejected) || !errors.Is(err, adapter.ErrOverloaded) ||
				!errors.As(err, &failure) || failure.Operation != adapter.OperationVerifyIndex ||
				failure.Category != adapter.FailureOverloaded || !failure.OutcomeKnown || requests != 1 ||
				errors.Is(err, private) || strings.Contains(err.Error(), "credential") {
				t.Fatalf("CutoverAlias() error/requests = %v/%d", err, requests)
			}
			if firstErr == nil || test.name == "repeated callback" && !errors.Is(repeatedErr, adapter.ErrLifecycleCutoverGuardRejected) {
				t.Fatalf("callback errors = %v/%v", firstErr, repeatedErr)
			}
		})
	}
}

func TestCutoverAliasRedactsVerifierFailure(t *testing.T) {
	t.Parallel()

	private := errors.New("backend verifier credential secret")
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			return operation()
		}),
		adapter.LifecycleVerifierFunc(func(context.Context, adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
			return adapter.LifecycleVerificationResult{}, private
		}),
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}),
	)

	_, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
	var failure *adapter.Failure
	if !errors.Is(err, adapter.ErrLifecycleRejected) || errors.Is(err, adapter.ErrLifecycleCutoverGuardRejected) || !errors.As(err, &failure) ||
		failure.Operation != adapter.OperationVerifyIndex || failure.Category != adapter.FailureRejected || !failure.OutcomeKnown ||
		errors.Is(err, private) || strings.Contains(err.Error(), "credential") {
		t.Fatalf("CutoverAlias() error = %v", err)
	}
}

func TestCutoverAliasPreservesOperationFailureWhenGuardReturnsNil(t *testing.T) {
	t.Parallel()

	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			_ = operation()
			return nil
		}),
		nil,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			return cursorResponse(http.StatusServiceUnavailable, `{"error":{"type":"rejected_execution_exception"},"status":503}`), nil
		}),
	)

	_, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
	var failure *adapter.Failure
	if !errors.Is(err, adapter.ErrOverloaded) || errors.Is(err, adapter.ErrLifecycleCutoverGuardRejected) ||
		!errors.As(err, &failure) || failure.Operation != adapter.OperationVerifyIndex ||
		failure.Category != adapter.FailureOverloaded || !failure.OutcomeKnown {
		t.Fatalf("CutoverAlias() error = %v", err)
	}
}

func TestCutoverAliasHidesGuardFailureAfterCompletedCallback(t *testing.T) {
	t.Parallel()

	private := errors.New("private fence credential")
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			if err := operation(); err != nil {
				return err
			}
			return private
		}),
		adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
			return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint}, nil
		}),
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/_aliases" {
				return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
			}
			return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}),
	)

	report, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
	var failure *adapter.Failure
	if !errors.Is(err, adapter.ErrLifecycleCutoverGuardRejected) || !errors.As(err, &failure) ||
		failure.Operation != adapter.OperationSwapAlias || failure.Category != adapter.FailureRejected || !failure.OutcomeKnown ||
		errors.Is(err, private) || strings.Contains(err.Error(), "credential") || !report.Verified {
		t.Fatalf("CutoverAlias() report/error = %#v/%v", report, err)
	}
}

func TestCutoverAliasClassifiesGuardReturnDuringAcknowledgedAliasRequest(t *testing.T) {
	t.Parallel()

	swapStarted := make(chan struct{})
	continueSwap := make(chan struct{})
	callbackDone := make(chan error, 1)
	callbackFinished := make(chan struct{})
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			go func() {
				callbackDone <- operation()
				close(callbackFinished)
			}()
			select {
			case <-swapStarted:
			case <-callbackFinished:
			}
			return nil
		}),
		adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
			return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint}, nil
		}),
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/_aliases" {
				close(swapStarted)
				<-continueSwap
				return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
			}
			return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}),
	)

	type cutoverCall struct {
		report search.VerificationReport
		err    error
	}
	cutoverDone := make(chan cutoverCall, 1)
	go func() {
		report, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
		cutoverDone <- cutoverCall{report: report, err: err}
	}()
	select {
	case result := <-cutoverDone:
		close(continueSwap)
		<-callbackDone
		t.Fatalf("CutoverAlias returned with its alias request in flight: %#v/%v", result.report, result.err)
	case <-time.After(100 * time.Millisecond):
	}
	close(continueSwap)
	if callbackErr := <-callbackDone; callbackErr != nil {
		t.Fatalf("in-flight callback error = %v", callbackErr)
	}
	result := <-cutoverDone
	var failure *adapter.Failure
	if !errors.Is(result.err, adapter.ErrLifecycleCutoverGuardRejected) || !errors.As(result.err, &failure) ||
		failure.Operation != adapter.OperationSwapAlias || failure.Category != adapter.FailureRejected || !failure.OutcomeKnown || !result.report.Verified {
		t.Fatalf("CutoverAlias() report/error = %#v/%v", result.report, result.err)
	}
}

func TestCutoverAliasPreservesAmbiguousSwapCancellationWhenGuardReturns(t *testing.T) {
	t.Parallel()

	swapStarted := make(chan struct{})
	callbackDone := make(chan error, 1)
	callbackFinished := make(chan struct{})
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			go func() {
				callbackDone <- operation()
				close(callbackFinished)
			}()
			select {
			case <-swapStarted:
			case <-callbackFinished:
			}
			return nil
		}),
		adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
			return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint}, nil
		}),
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/_aliases" {
				close(swapStarted)
				<-request.Context().Done()
				return nil, request.Context().Err()
			}
			return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}),
	)

	report, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
	var failure *adapter.Failure
	if !errors.Is(err, adapter.ErrLifecycleCutoverGuardRejected) || !errors.Is(err, context.Canceled) ||
		!errors.As(err, &failure) || failure.Operation != adapter.OperationSwapAlias ||
		failure.Category != adapter.FailureCancelled || failure.OutcomeKnown || !report.Verified {
		t.Fatalf("CutoverAlias() report/error = %#v/%v", report, err)
	}
	if callbackErr := <-callbackDone; !errors.Is(callbackErr, context.Canceled) {
		t.Fatalf("callback error = %v", callbackErr)
	}
}

func TestCutoverAliasPreservesUnverifiedFailureWhenGuardAlsoViolatesContract(t *testing.T) {
	t.Parallel()

	var repeatedErr error
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			firstErr := operation()
			repeatedErr = operation()
			return firstErr
		}),
		adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
			return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint, Drift: 1}, nil
		}),
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			return cursorResponse(http.StatusOK, `{"count":2,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}),
	)

	report, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2")
	var failure *adapter.Failure
	if !errors.Is(err, adapter.ErrLifecycleCutoverGuardRejected) || !errors.Is(err, adapter.ErrLifecycleCutoverUnverified) ||
		!errors.As(err, &failure) || failure.Operation != adapter.OperationSwapAlias ||
		failure.Category != adapter.FailureRejected || !failure.OutcomeKnown || report.Verified || report.Drift != 1 {
		t.Fatalf("CutoverAlias() report/error = %#v/%v", report, err)
	}
	if !errors.Is(repeatedErr, adapter.ErrLifecycleCutoverGuardRejected) {
		t.Fatalf("repeated callback error = %v", repeatedErr)
	}
}

func TestCutoverAliasDeniesBeforeGuardOrDispatch(t *testing.T) {
	t.Parallel()

	guardCalls, requests := 0, 0
	client := newCutoverClientWithAuthorizer(t,
		adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return errors.New("denied") }),
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			guardCalls++
			return operation()
		}),
		nil,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return cursorResponse(http.StatusOK, `{}`), nil
		}),
	)

	if _, err := client.CutoverAlias(t.Context(), "tenant", "events-read", "events-v1", "events-v2", "definition-v2"); !errors.Is(err, adapter.ErrLifecycleDenied) {
		t.Fatalf("CutoverAlias() error = %v", err)
	}
	if guardCalls != 0 || requests != 0 {
		t.Fatalf("denied cutover reached guard/transport: %d/%d", guardCalls, requests)
	}
}

func newCutoverClient(
	t *testing.T,
	guard adapter.LifecycleCutoverGuard,
	verifier adapter.LifecycleVerifier,
	transport http.RoundTripper,
) *adapter.Client {
	return newCutoverClientWithAuthorizer(t,
		adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }),
		guard,
		verifier,
		transport,
	)
}

func allowLifecycleMutationGuard() adapter.LifecycleMutationGuard {
	return adapter.LifecycleMutationGuardFunc(func(_ context.Context, _ adapter.LifecycleMutationRequest, operation func() error) error {
		return operation()
	})
}

func newCutoverClientWithAuthorizer(
	t *testing.T,
	authorizer adapter.LifecycleAuthorizer,
	guard adapter.LifecycleCutoverGuard,
	verifier adapter.LifecycleVerifier,
	transport http.RoundTripper,
) *adapter.Client {
	t.Helper()
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, Transport: transport,
		TransportOwnership: adapter.TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: authorizer,
			Verifier:   verifier, CutoverGuard: guard,
			MutationGuard:      allowLifecycleMutationGuard(),
			ReindexCursorCodec: mustReindexCursorCodec(t),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
