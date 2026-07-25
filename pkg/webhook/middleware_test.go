package webhook

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMiddlewareProvidesAuthenticatedContextAndRestoredBody(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	request := signedRequestFixture(t, now)
	verifier := verifierFixture(t, now)
	nextCalled := false
	handler, err := verifier.Middleware(MiddlewareConfig{
		Request: RequestOptions{MaxBodyBytes: 64, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 256}},
	}, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		nextCalled = true
		verification, ok := VerificationFromContext(request.Context())
		if !ok || verification.KeyID != "key" {
			t.Errorf("verification context = %#v, %v", verification, ok)
		}
		body, ok := VerifiedBodyFromContext(request.Context())
		if !ok || string(body) != "body" {
			t.Errorf("body context = %q, %v", body, ok)
		}
		restored, readErr := io.ReadAll(request.Body)
		if readErr != nil || string(restored) != "body" {
			t.Errorf("restored body = %q, error = %v", restored, readErr)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	if err != nil {
		t.Fatalf("Middleware() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if !nextCalled || recorder.Code != http.StatusNoContent {
		t.Fatalf("next called = %v, status = %d", nextCalled, recorder.Code)
	}
}

func TestMiddlewareReturnsOnlySafeFailureAndSkipsHandler(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	request := signedRequestFixture(t, now)
	request.Header.Set(SignatureHeader, "secret-signature-material")
	verifier := verifierFixture(t, now)
	var observed error
	handler, err := verifier.Middleware(MiddlewareConfig{
		Request:       RequestOptions{MaxBodyBytes: 64, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 256}},
		FailureStatus: http.StatusUnauthorized,
		OnError:       func(_ context.Context, err error) { observed = err },
	}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler called after verification failure")
	}))
	if err != nil {
		t.Fatalf("Middleware() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || recorder.Body.String() != "webhook verification failed\n" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	if observed == nil || bytes.Contains(recorder.Body.Bytes(), []byte(SignatureHeader)) || bytes.Contains(recorder.Body.Bytes(), []byte("secret")) {
		t.Fatalf("observed = %v, unsafe response = %q", observed, recorder.Body.String())
	}
}

func TestMiddlewareContainsDiagnosticHookPanic(t *testing.T) {
	t.Parallel()

	verifier := verifierFixture(t, time.Unix(1_700_000_000, 0))
	handler, err := verifier.Middleware(MiddlewareConfig{
		Request: RequestOptions{MaxBodyBytes: 64, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 256}},
		OnError: func(context.Context, error) {
			panic("diagnostic sink panic")
		},
	}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if err != nil {
		t.Fatalf("Middleware() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "https://example.com/hook", nil)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestMiddlewareValidatesConfiguration(t *testing.T) {
	t.Parallel()

	verifier := verifierFixture(t, time.Unix(1_700_000_000, 0))
	if _, err := verifier.Middleware(MiddlewareConfig{}, nil); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("Middleware() error = %v, want ErrInvalidConfiguration", err)
	}
	if _, err := verifier.Middleware(MiddlewareConfig{FailureStatus: 99}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("Middleware() status error = %v, want ErrInvalidConfiguration", err)
	}
}

func TestContextAccessorsRejectAbsentAndCopyBody(t *testing.T) {
	t.Parallel()

	if _, ok := VerificationFromContext(context.Background()); ok {
		t.Fatal("VerificationFromContext() found absent value")
	}
	if _, ok := VerifiedBodyFromContext(context.Background()); ok {
		t.Fatal("VerifiedBodyFromContext() found absent value")
	}
	body := []byte("body")
	ctx := context.WithValue(context.Background(), verifiedBodyContextKey{}, body)
	got, ok := VerifiedBodyFromContext(ctx)
	if !ok {
		t.Fatal("VerifiedBodyFromContext() did not find value")
	}
	got[0] = 'X'
	if string(body) != "body" {
		t.Fatalf("VerifiedBodyFromContext() exposed mutable context body: %q", body)
	}
}

func verifierFixture(t *testing.T, now time.Time) *Verifier {
	t.Helper()

	verifier, err := NewVerifier(VerifierConfig{
		Algorithm: SHA256,
		Keys:      []VerificationKey{{ID: "key", Secret: []byte("secret")}},
		Clock:     func() time.Time { return now },
		Tolerance: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}

	return verifier
}
