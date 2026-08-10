package compatibility_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/http-signature/compatibility"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestExplicitLegacySigningAdaptersRemainProtocolSeparated(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		new  func(compatibility.SigningRoundTripperConfig) (*compatibility.SigningRoundTripper, error)
		want compatibility.Protocol
	}{
		{"cavage", compatibility.NewCavageSigningRoundTripper, compatibility.CavageDraft},
		{"aws", compatibility.NewAWSSigV4SigningRoundTripper, compatibility.AWSSigV4},
		{"oauth", compatibility.NewOAuth1SigningRoundTripper, compatibility.OAuth1},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			original := httptest.NewRequest(http.MethodPost, "https://example.com/resource", strings.NewReader("body"))
			var delegated *http.Request
			adapter, err := test.new(compatibility.SigningRoundTripperConfig{
				Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
					delegated = request
					return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Request: request}, nil
				}),
				Sign: func(_ context.Context, request *http.Request) error {
					request.Header.Set("Authorization", "external-format")
					return nil
				},
				ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
			})
			if err != nil {
				t.Fatal(err)
			}
			if adapter.Protocol() != test.want {
				t.Fatalf("protocol = %q, want %q", adapter.Protocol(), test.want)
			}
			response, err := adapter.RoundTrip(original)
			if err != nil || response.StatusCode != http.StatusNoContent {
				t.Fatalf("RoundTrip() = %#v, %v", response, err)
			}
			if delegated == original || original.Header.Get("Authorization") != "" || delegated.Header.Get("Authorization") == "" {
				t.Fatal("adapter did not isolate protocol-specific request mutation")
			}
			if delegated.Body != original.Body {
				t.Fatal("adapter replaced caller body")
			}
		})
	}
}

func TestVendorAdapterRequiresAnExplicitSafeName(t *testing.T) {
	t.Parallel()

	config := compatibility.SigningRoundTripperConfig{
		Transport:   roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil }),
		Sign:        func(context.Context, *http.Request) error { return nil },
		ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
	}
	for _, name := range []string{"", strings.Repeat("a", 65), "AWS", "contains space", "contains/slash", "`", "{", "/", ":"} {
		if _, err := compatibility.NewVendorSigningRoundTripper(name, config); !errors.Is(err, compatibility.ErrInvalidAdapter) {
			t.Fatalf("NewVendorSigningRoundTripper(%q) error = %v", name, err)
		}
	}
	for _, name := range []string{"a", "z", "0", "9", "-", "_", ".", "acme-v2", strings.Repeat("a", 64)} {
		adapter, err := compatibility.NewVendorSigningRoundTripper(name, config)
		if err != nil || adapter.Protocol() != compatibility.Protocol("vendor:"+name) {
			t.Fatalf("vendor adapter %q = %#v, %v", name, adapter, err)
		}
	}
}

func TestCompatibilityAdaptersSanitizeCallbackFailures(t *testing.T) {
	t.Parallel()

	secretFailure := errors.New("credential=secret")
	var reported error
	adapter, err := compatibility.NewCavageSigningRoundTripper(compatibility.SigningRoundTripperConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil }),
		Sign:      func(context.Context, *http.Request) error { return secretFailure },
		ReportError: func(_ context.Context, protocol compatibility.Protocol, operation compatibility.Operation, err error) {
			if protocol != compatibility.CavageDraft || operation != compatibility.OperationSign {
				t.Fatalf("reported boundary = %q, %q", protocol, operation)
			}
			reported = err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := adapter.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", nil))
	if response != nil || !errors.Is(err, compatibility.ErrSigning) || strings.Contains(err.Error(), "secret") || !errors.Is(reported, secretFailure) {
		t.Fatalf("sanitized result = %#v, %v; reported = %v", response, err, reported)
	}
}

func TestExplicitLegacyVerificationMiddlewareRejectsBeforeApplication(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		new  func(compatibility.VerificationMiddlewareConfig) (compatibility.VerificationMiddleware, error)
		want compatibility.Protocol
	}{
		{"cavage", compatibility.NewCavageVerificationMiddleware, compatibility.CavageDraft},
		{"aws", compatibility.NewAWSSigV4VerificationMiddleware, compatibility.AWSSigV4},
		{"oauth", compatibility.NewOAuth1VerificationMiddleware, compatibility.OAuth1},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			called := false
			var reported error
			middleware, err := test.new(compatibility.VerificationMiddlewareConfig{
				Verify: func(context.Context, *http.Request) error { return errors.New("key=secret") },
				ReportError: func(_ context.Context, protocol compatibility.Protocol, operation compatibility.Operation, err error) {
					if protocol != test.want || operation != compatibility.OperationVerify {
						t.Fatalf("reported boundary = %q, %q", protocol, operation)
					}
					reported = err
				},
				Reject: func(writer http.ResponseWriter, _ *http.Request, err error) {
					if !errors.Is(err, compatibility.ErrVerification) || strings.Contains(err.Error(), "secret") {
						t.Fatalf("unsafe rejection error = %v", err)
					}
					http.Error(writer, "unauthorized", http.StatusUnauthorized)
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(
				recorder,
				httptest.NewRequest(http.MethodGet, "https://example.com", nil),
			)
			if called || recorder.Code != http.StatusUnauthorized || reported == nil {
				t.Fatalf("called = %v, status = %d, reported = %v", called, recorder.Code, reported)
			}
		})
	}
}

func TestCompatibilityAdapterConfigurationAndNilBoundaries(t *testing.T) {
	t.Parallel()
	var nilAdapter *compatibility.SigningRoundTripper
	if nilAdapter.Protocol() != "" {
		t.Fatal("nil adapter reported a protocol")
	}

	validSigning := compatibility.SigningRoundTripperConfig{
		Transport:   roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil }),
		Sign:        func(context.Context, *http.Request) error { return nil },
		ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
	}
	for _, config := range []compatibility.SigningRoundTripperConfig{
		{},
		{Sign: validSigning.Sign, ReportError: validSigning.ReportError},
		{Transport: validSigning.Transport, ReportError: validSigning.ReportError},
		{Transport: validSigning.Transport, Sign: validSigning.Sign},
	} {
		if _, err := compatibility.NewCavageSigningRoundTripper(config); !errors.Is(err, compatibility.ErrInvalidAdapter) {
			t.Fatalf("invalid signing config error = %v", err)
		}
	}
	adapter, err := compatibility.NewCavageSigningRoundTripper(validSigning)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := adapter.RoundTrip(nil); response != nil || !errors.Is(err, compatibility.ErrInvalidAdapter) {
		t.Fatalf("nil request result = %#v, %v", response, err)
	}
	var absent *compatibility.SigningRoundTripper
	if response, err := absent.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", nil)); response != nil || !errors.Is(err, compatibility.ErrInvalidAdapter) {
		t.Fatalf("nil adapter result = %#v, %v", response, err)
	}
	if response, err := new(compatibility.SigningRoundTripper).RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", nil)); response != nil || !errors.Is(err, compatibility.ErrInvalidAdapter) {
		t.Fatalf("zero adapter result = %#v, %v", response, err)
	}

	validVerification := compatibility.VerificationMiddlewareConfig{
		Verify:      func(context.Context, *http.Request) error { return nil },
		ReportError: func(context.Context, compatibility.Protocol, compatibility.Operation, error) {},
		Reject:      func(http.ResponseWriter, *http.Request, error) {},
	}
	if _, err := compatibility.NewVendorVerificationMiddleware("", validVerification); !errors.Is(err, compatibility.ErrInvalidAdapter) {
		t.Fatalf("invalid vendor verification error = %v", err)
	}
	for _, config := range []compatibility.VerificationMiddlewareConfig{
		{},
		{ReportError: validVerification.ReportError, Reject: validVerification.Reject},
		{Verify: validVerification.Verify, Reject: validVerification.Reject},
		{Verify: validVerification.Verify, ReportError: validVerification.ReportError},
	} {
		if _, err := compatibility.NewCavageVerificationMiddleware(config); !errors.Is(err, compatibility.ErrInvalidAdapter) {
			t.Fatalf("invalid verification config error = %v", err)
		}
	}
	middleware, err := compatibility.NewCavageVerificationMiddleware(validVerification)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	middleware(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://example.com", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("nil next status = %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("nil request delegated") })).ServeHTTP(recorder, nil)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("nil request status = %d", recorder.Code)
	}

	vendorMiddleware, err := compatibility.NewVendorVerificationMiddleware("acme-v2", validVerification)
	if err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	vendorMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })).ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "https://example.com", io.NopCloser(strings.NewReader("body"))),
	)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("vendor middleware status = %d", recorder.Code)
	}
}
