package httpsignature

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPAdapterConstructorsRejectEachIndependentField(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil })
	signer := NewSigner(testSigningProfile(t, now, key))
	verifier := NewVerifier(testHTTPVerificationProfile(t, now, key))

	signingBase := SigningRoundTripperConfig{
		Transport: transport, Signer: signer, Label: "sig", Existing: ExistingSignaturesReject,
		Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
	}
	if _, err := NewSigningRoundTripper(signingBase); err != nil {
		t.Fatalf("valid signing transport error = %v", err)
	}
	for _, mutate := range []func(*SigningRoundTripperConfig){
		func(config *SigningRoundTripperConfig) { config.Transport = nil },
		func(config *SigningRoundTripperConfig) { config.Signer = nil },
		func(config *SigningRoundTripperConfig) { config.Label = "Bad" },
		func(config *SigningRoundTripperConfig) { config.Options = nil },
		func(config *SigningRoundTripperConfig) { config.Existing = 0 },
		func(config *SigningRoundTripperConfig) { config.Existing = ExistingSignaturesAppend + 1 },
	} {
		config := signingBase
		mutate(&config)
		if _, err := NewSigningRoundTripper(config); !errors.Is(err, ErrInvalidHTTPIntegration) {
			t.Fatalf("invalid signing transport error = %v", err)
		}
	}

	verificationBase := RequestVerificationMiddlewareConfig{
		Verifier:    verifier,
		SelectLabel: func(*http.Request, SignatureInputs, Signatures) (string, error) { return "sig", nil },
		MapError:    func(http.ResponseWriter, *http.Request, error) {},
	}
	if _, err := NewRequestVerificationMiddleware(verificationBase); err != nil {
		t.Fatalf("valid request middleware error = %v", err)
	}
	for _, mutate := range []func(*RequestVerificationMiddlewareConfig){
		func(config *RequestVerificationMiddlewareConfig) { config.Verifier = nil },
		func(config *RequestVerificationMiddlewareConfig) { config.SelectLabel = nil },
		func(config *RequestVerificationMiddlewareConfig) { config.MapError = nil },
	} {
		config := verificationBase
		mutate(&config)
		if _, err := NewRequestVerificationMiddleware(config); !errors.Is(err, ErrInvalidHTTPIntegration) {
			t.Fatalf("invalid request middleware error = %v", err)
		}
	}

	verifyingBase := VerifyingRoundTripperConfig{
		Transport: transport, Verifier: verifier,
		SelectLabel: func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "sig", nil },
	}
	if _, err := NewVerifyingRoundTripper(verifyingBase); err != nil {
		t.Fatalf("valid verifying transport error = %v", err)
	}
	for _, mutate := range []func(*VerifyingRoundTripperConfig){
		func(config *VerifyingRoundTripperConfig) { config.Transport = nil },
		func(config *VerifyingRoundTripperConfig) { config.Verifier = nil },
		func(config *VerifyingRoundTripperConfig) { config.SelectLabel = nil },
	} {
		config := verifyingBase
		mutate(&config)
		if _, err := NewVerifyingRoundTripper(config); !errors.Is(err, ErrInvalidHTTPIntegration) {
			t.Fatalf("invalid verifying transport error = %v", err)
		}
	}

	responseBase := ResponseSigningMiddlewareConfig{
		Signer: NewSigner(testResponseSigningProfile(t, now, key)), Label: "response",
		Existing: ExistingSignaturesReject, MaxBufferedBytes: 1,
		Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
			return SigningOptions{}, nil
		},
		MapError: func(http.ResponseWriter, *http.Request, error) {},
	}
	if _, err := NewResponseSigningMiddleware(responseBase); err != nil {
		t.Fatalf("valid response middleware error = %v", err)
	}
	for _, mutate := range []func(*ResponseSigningMiddlewareConfig){
		func(config *ResponseSigningMiddlewareConfig) { config.Signer = nil },
		func(config *ResponseSigningMiddlewareConfig) { config.Label = "Bad" },
		func(config *ResponseSigningMiddlewareConfig) { config.MaxBufferedBytes = 0 },
		func(config *ResponseSigningMiddlewareConfig) { config.Options = nil },
		func(config *ResponseSigningMiddlewareConfig) { config.MapError = nil },
		func(config *ResponseSigningMiddlewareConfig) { config.Existing = 0 },
		func(config *ResponseSigningMiddlewareConfig) { config.Existing = ExistingSignaturesAppend + 1 },
	} {
		config := responseBase
		mutate(&config)
		if _, err := NewResponseSigningMiddleware(config); !errors.Is(err, ErrInvalidHTTPIntegration) {
			t.Fatalf("invalid response middleware error = %v", err)
		}
	}
}

func TestSigningRoundTripperRejectsEachConfigurationAndSigningBoundary(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	base := func() SigningRoundTripperConfig {
		return SigningRoundTripperConfig{
			Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil }),
			Signer:    NewSigner(testSigningProfile(t, now, key)), Label: "sig", Existing: ExistingSignaturesAppend,
			Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		}
	}
	for _, test := range []struct {
		name      string
		configure func(*SigningRoundTripperConfig)
		prepare   func(*http.Request)
		want      error
	}{
		{name: "malformed input", prepare: func(request *http.Request) {
			request.Header.Set("Signature-Input", "bad")
			request.Header.Set("Signature", "old=:AA==:")
		}, want: ErrExistingSignatures},
		{name: "malformed signature", prepare: func(request *http.Request) {
			request.Header.Set("Signature-Input", `old=("@method")`)
			request.Header.Set("Signature", "bad")
		}, want: ErrExistingSignatures},
		{name: "label mismatch", prepare: func(request *http.Request) {
			request.Header.Set("Signature-Input", `old=("@method")`)
			request.Header.Set("Signature", "other=:AA==:")
		}, want: ErrExistingSignatures},
		{name: "options", configure: func(config *SigningRoundTripperConfig) {
			config.Options = func(context.Context, *http.Request) (SigningOptions, error) {
				return SigningOptions{}, errors.New("private")
			}
		}, want: ErrHTTPIntegrationSigning},
		{name: "external", configure: func(config *SigningRoundTripperConfig) {
			config.ExternalContext = func(context.Context, *http.Request) (*ExternalRequestContext, error) {
				return nil, errors.New("private")
			}
		}, want: ErrHTTPIntegrationSigning},
		{name: "sign", configure: func(config *SigningRoundTripperConfig) {
			config.Options = func(context.Context, *http.Request) (SigningOptions, error) {
				return SigningOptions{Nonce: "forbidden"}, nil
			}
		}, want: ErrHTTPIntegrationSigning},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			config := base()
			if test.configure != nil {
				test.configure(&config)
			}
			transport, err := NewSigningRoundTripper(config)
			if err != nil {
				t.Fatal(err)
			}
			body := &countingBody{reader: strings.NewReader("payload")}
			request := httptest.NewRequest(http.MethodPost, "https://example.com/pay", body)
			if test.prepare != nil {
				test.prepare(request)
			}
			if _, err := transport.RoundTrip(request); !errors.Is(err, test.want) {
				t.Fatalf("RoundTrip() error = %v, want %v", err, test.want)
			}
			if body.closed != 1 {
				t.Fatalf("body close count = %d", body.closed)
			}
		})
	}
	transport, err := NewSigningRoundTripper(base())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(nil); !errors.Is(err, ErrInvalidHTTPIntegration) {
		t.Fatalf("nil request error = %v", err)
	}
	var nilTransport *SigningRoundTripper
	if _, err := nilTransport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", nil)); !errors.Is(err, ErrInvalidHTTPIntegration) {
		t.Fatalf("nil receiver error = %v", err)
	}
	duplicateConfig := base()
	duplicateConfig.Label = "old"
	duplicate, err := NewSigningRoundTripper(duplicateConfig)
	if err != nil {
		t.Fatal(err)
	}
	duplicateRequest := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	duplicateRequest.Header.Set("Signature-Input", `old=("@method")`)
	duplicateRequest.Header.Set("Signature", "old=:AA==:")
	if _, err := duplicate.RoundTrip(duplicateRequest); !errors.Is(err, ErrExistingSignatures) {
		t.Fatalf("duplicate appended label error = %v", err)
	}
}

func TestHTTPIntegrationExistingFieldsFailBeforeCallbacks(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, prepare := range []func(http.Header){
		func(header http.Header) { header.Set("Signature-Input", `old=("@method")`) },
		func(header http.Header) { header.Set("Signature", "old=:AA==:") },
		func(header http.Header) {
			header.Set("Signature-Input", `old=("@method")`)
			header.Set("Signature", "other=:AA==:")
		},
	} {
		calls := 0
		transport, err := NewSigningRoundTripper(SigningRoundTripperConfig{
			Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil }),
			Signer:    NewSigner(testSigningProfile(t, now, key)), Label: "sig", Existing: ExistingSignaturesAppend,
			Options: func(context.Context, *http.Request) (SigningOptions, error) { calls++; return SigningOptions{}, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
		prepare(request.Header)
		if _, err := transport.RoundTrip(request); !errors.Is(err, ErrExistingSignatures) || calls != 0 {
			t.Fatalf("existing request fields error = %v, callback calls = %d", err, calls)
		}
	}
	request := &http.Request{}
	if response, err := signingRoundTripError(request, ErrHTTPIntegrationSigning); response != nil || !errors.Is(err, ErrHTTPIntegrationSigning) {
		t.Fatalf("nil-body signing error = %#v, %v", response, err)
	}
	if _, _, err := appendSignedFields(
		SignatureInputs{entries: []SignatureInput{{Label: "missing"}}},
		Signatures{},
		SignedFields{input: SignatureInput{Label: "new"}, signature: SignatureValue{Label: "new", Value: []byte("x")}},
	); !errors.Is(err, ErrInvalidSignedFields) {
		t.Fatalf("missing existing signature error = %v", err)
	}
}

func TestBufferedResponseWriterExactStatusAndCapacityBoundaries(t *testing.T) {
	t.Parallel()

	for _, status := range []int{100, 199} {
		writer := newBufferedResponseWriter(1)
		writer.WriteHeader(status)
		if writer.status != 0 {
			t.Fatalf("informational status %d committed as %d", status, writer.status)
		}
	}
	for _, status := range []int{101, 999} {
		writer := newBufferedResponseWriter(1)
		writer.WriteHeader(status)
		if writer.status != status {
			t.Fatalf("status %d committed as %d", status, writer.status)
		}
	}
	writer := newBufferedResponseWriter(2)
	if count, err := writer.Write([]byte("ab")); err != nil || count != 2 {
		t.Fatalf("exact-capacity write = %d, %v", count, err)
	}
	if count, err := writer.Write([]byte("c")); count != 0 || !errors.Is(err, ErrResponseBodyTooLarge) {
		t.Fatalf("over-capacity write = %d, %v", count, err)
	}
}

func TestResponseSigningExistingFieldsFailBeforeOptions(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, prepare := range []func(http.Header){
		func(header http.Header) { header.Set("Signature-Input", `old=("@status")`) },
		func(header http.Header) { header.Set("Signature", "old=:AA==:") },
		func(header http.Header) {
			header.Set("Signature-Input", `old=("@status")`)
			header.Set("Signature", "other=:AA==:")
		},
	} {
		calls := 0
		var mapped error
		middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
			Signer: NewSigner(testResponseSigningProfile(t, now, key)), Label: "response", Existing: ExistingSignaturesAppend, MaxBufferedBytes: 1,
			Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
				calls++
				return SigningOptions{}, nil
			},
			MapError: func(_ http.ResponseWriter, _ *http.Request, errorValue error) { mapped = errorValue },
		})
		if err != nil {
			t.Fatal(err)
		}
		middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { prepare(writer.Header()) })).ServeHTTP(
			httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "https://example.com", nil),
		)
		if !errors.Is(mapped, ErrExistingSignatures) || calls != 0 {
			t.Fatalf("existing response fields error = %v, callback calls = %d", mapped, calls)
		}
	}
}

func signedHeaderRequest(t *testing.T, now time.Time, key HMACKey) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "https://example.com/pay", nil)
	signed, err := NewSigner(testSigningProfile(t, now, key)).Sign(context.Background(), MessageContext{Request: request}, "sig", SigningOptions{})
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Signature-Input", signed.SignatureInputField())
	request.Header.Set("Signature", signed.SignatureField())
	return request
}

func TestRequestVerificationMiddlewareRejectsEachUntrustedBoundary(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name      string
		configure func(*RequestVerificationMiddlewareConfig)
		mutate    func(*http.Request)
		want      error
	}{
		{name: "input", mutate: func(request *http.Request) { request.Header.Set("Signature-Input", "bad") }, want: ErrInvalidSignatureInput},
		{name: "signature", mutate: func(request *http.Request) { request.Header.Set("Signature", "bad") }, want: ErrInvalidSignature},
		{name: "selector", configure: func(config *RequestVerificationMiddlewareConfig) {
			config.SelectLabel = func(*http.Request, SignatureInputs, Signatures) (string, error) { return "", errors.New("private") }
		}, want: ErrInvalidHTTPIntegration},
		{name: "invalid label", configure: func(config *RequestVerificationMiddlewareConfig) {
			config.SelectLabel = func(*http.Request, SignatureInputs, Signatures) (string, error) { return "Not Valid", nil }
		}, want: ErrInvalidHTTPIntegration},
		{name: "external", configure: func(config *RequestVerificationMiddlewareConfig) {
			config.ExternalContext = func(context.Context, *http.Request) (*ExternalRequestContext, error) {
				return nil, errors.New("private")
			}
		}, want: ErrInvalidHTTPIntegration},
		{name: "cryptographic", mutate: func(request *http.Request) {
			request.Header.Set("Signature", "sig=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:")
		}, want: ErrInvalidSignatureValue},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			request := signedHeaderRequest(t, now, key)
			config := RequestVerificationMiddlewareConfig{
				Verifier:    NewVerifier(testHTTPVerificationProfile(t, now, key)),
				SelectLabel: func(*http.Request, SignatureInputs, Signatures) (string, error) { return "sig", nil },
			}
			var got error
			config.MapError = func(_ http.ResponseWriter, _ *http.Request, err error) { got = err }
			if test.configure != nil {
				test.configure(&config)
			}
			if test.mutate != nil {
				test.mutate(request)
			}
			middleware, err := NewRequestVerificationMiddleware(config)
			if err != nil {
				t.Fatal(err)
			}
			middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next called") })).ServeHTTP(httptest.NewRecorder(), request)
			if !errors.Is(got, test.want) {
				t.Fatalf("mapped error = %v, want %v", got, test.want)
			}
		})
	}
	var got error
	middleware, _ := NewRequestVerificationMiddleware(RequestVerificationMiddlewareConfig{
		Verifier:    NewVerifier(testHTTPVerificationProfile(t, now, key)),
		SelectLabel: func(*http.Request, SignatureInputs, Signatures) (string, error) { return "sig", nil },
		MapError:    func(_ http.ResponseWriter, _ *http.Request, err error) { got = err },
	})
	middleware(nil).ServeHTTP(httptest.NewRecorder(), signedHeaderRequest(t, now, key))
	if !errors.Is(got, ErrInvalidHTTPIntegration) {
		t.Fatalf("nil next error = %v", got)
	}
	got = nil
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), nil)
	if !errors.Is(got, ErrInvalidHTTPIntegration) {
		t.Fatalf("nil request error = %v", got)
	}
	//lint:ignore SA1012 This verifies the public nil-context failure contract.
	if _, ok := VerifiedSignatureFromContext(nil); ok { //nolint:staticcheck // Verifies nil-context rejection.
		t.Fatal("nil context reported verification")
	}
}

func signedHeaderResponse(t *testing.T, now time.Time, key HMACKey, request *http.Request) *http.Response {
	t.Helper()
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &countingBody{reader: strings.NewReader("payload")}, Request: request}
	signed, err := NewSigner(testResponseSigningProfile(t, now, key)).Sign(context.Background(), MessageContext{Response: response, RelatedRequest: request}, "response", SigningOptions{})
	if err != nil {
		t.Fatal(err)
	}
	response.Header.Set("Signature-Input", signed.SignatureInputField())
	response.Header.Set("Signature", signed.SignatureField())
	return response
}

func TestVerifyingRoundTripperRejectsEachResponseBoundary(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request := httptest.NewRequest(http.MethodGet, "https://example.com/data", nil)
	backendErr := errors.New("backend")
	direct := &VerifyingRoundTripper{transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, backendErr })}
	if _, err := direct.RoundTrip(request); !errors.Is(err, backendErr) {
		t.Fatalf("backend error = %v", err)
	}
	direct.transport = roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil })
	if _, err := direct.RoundTrip(request); !errors.Is(err, ErrHTTPIntegrationVerification) {
		t.Fatalf("nil response error = %v", err)
	}
	var nilTransport *VerifyingRoundTripper
	if _, err := nilTransport.RoundTrip(request); !errors.Is(err, ErrInvalidHTTPIntegration) {
		t.Fatalf("nil receiver error = %v", err)
	}
	if _, err := direct.RoundTrip(nil); !errors.Is(err, ErrInvalidHTTPIntegration) {
		t.Fatalf("nil request error = %v", err)
	}

	for _, test := range []struct {
		name      string
		configure func(*VerifyingRoundTripperConfig)
		mutate    func(*http.Response)
		want      error
	}{
		{name: "input", mutate: func(response *http.Response) { response.Header.Set("Signature-Input", "bad") }, want: ErrInvalidSignatureInput},
		{name: "signature", mutate: func(response *http.Response) { response.Header.Set("Signature", "bad") }, want: ErrInvalidSignature},
		{name: "selector", configure: func(config *VerifyingRoundTripperConfig) {
			config.SelectLabel = func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) {
				return "", errors.New("private")
			}
		}, want: ErrInvalidHTTPIntegration},
		{name: "selector error with valid label", configure: func(config *VerifyingRoundTripperConfig) {
			config.SelectLabel = func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) {
				return "response", errors.New("private")
			}
		}, want: ErrInvalidHTTPIntegration},
		{name: "invalid selector label", configure: func(config *VerifyingRoundTripperConfig) {
			config.SelectLabel = func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) {
				return "Bad", nil
			}
		}, want: ErrInvalidHTTPIntegration},
		{name: "external", configure: func(config *VerifyingRoundTripperConfig) {
			config.ExternalContext = func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error) {
				return nil, errors.New("private")
			}
		}, want: ErrInvalidHTTPIntegration},
		{name: "cryptographic", mutate: func(response *http.Response) {
			response.Header.Set("Signature", "response=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:")
		}, want: ErrInvalidSignatureValue},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			response := signedHeaderResponse(t, now, key, request)
			body := response.Body.(*countingBody)
			config := VerifyingRoundTripperConfig{
				Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
				Verifier:  NewVerifier(testResponseVerificationProfile(t, now, key)),
				SelectLabel: func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) {
					return "response", nil
				},
			}
			if test.configure != nil {
				test.configure(&config)
			}
			if test.mutate != nil {
				test.mutate(response)
			}
			transport, err := NewVerifyingRoundTripper(config)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := transport.RoundTrip(request); got != nil || !errors.Is(err, test.want) {
				t.Fatalf("result = %#v, %v, want %v", got, err, test.want)
			}
			if body.closed != 1 {
				t.Fatalf("body close count = %d", body.closed)
			}
		})
	}
	if _, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{}); !errors.Is(err, ErrInvalidHTTPIntegration) {
		t.Fatalf("empty constructor error = %v", err)
	}
	if _, ok := VerifiedSignatureFromResponse(nil); ok {
		t.Fatal("nil response reported verification")
	}
	if _, ok := VerifiedSignatureFromResponse(&http.Response{}); ok {
		t.Fatal("response without request reported verification")
	}
}

func TestBufferedResponseWriterBoundarySemantics(t *testing.T) {
	t.Parallel()

	writer := newBufferedResponseWriter(1)
	writer.WriteHeader(http.StatusCreated)
	writer.WriteHeader(http.StatusAccepted)
	if writer.status != http.StatusCreated {
		t.Fatalf("status = %d", writer.status)
	}
	if _, err := writer.Write([]byte("xx")); !errors.Is(err, ErrResponseBodyTooLarge) {
		t.Fatalf("large write error = %v", err)
	}
	if _, err := writer.Write([]byte("x")); !errors.Is(err, ErrResponseBodyTooLarge) {
		t.Fatalf("write after limit error = %v", err)
	}
	empty := newBufferedResponseWriter(1)
	response := empty.response(httptest.NewRequest(http.MethodGet, "https://example.com", nil))
	if response.StatusCode != http.StatusOK || response.Header == nil {
		t.Fatalf("default response = %#v", response)
	}
	if signingProfileCoversComponent(testSigningProfile(t, time.Unix(1_700_000_000, 0), func() HMACKey { key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef")); return key }()), ComponentIdentifier{Name: "@method", Parameters: []Parameter{{Name: "UPPER", Value: true}}}) {
		t.Fatal("invalid component reported signing coverage")
	}
}

func responseDigestSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{{Name: "@status"}, {Name: "content-digest"}},
		Expires:           ParameterRequired, AlgorithmParameter: ParameterRequired, Nonce: ParameterForbidden, Tag: ParameterForbidden,
		Lifetime: time.Minute, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestResponseSigningMiddlewareRejectsEachBufferedBoundary(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	base := func() ResponseSigningMiddlewareConfig {
		return ResponseSigningMiddlewareConfig{
			Signer: NewSigner(testResponseSigningProfile(t, now, key)), Label: "response", Existing: ExistingSignaturesReject,
			MaxBufferedBytes: 16,
			Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
				return SigningOptions{}, nil
			},
		}
	}
	for _, test := range []struct {
		name      string
		configure func(*ResponseSigningMiddlewareConfig)
		handler   http.Handler
		want      error
	}{
		{name: "nil next", want: ErrInvalidHTTPIntegration},
		{name: "existing digest", configure: func(config *ResponseSigningMiddlewareConfig) {
			config.Signer = NewSigner(responseDigestSigningProfile(t, now, key))
			config.ContentDigestAlgorithms = []DigestAlgorithm{SHA256}
		}, handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Digest", "sha-256=:AA==:")
		}), want: ErrExistingDigest},
		{name: "existing rejected", handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Signature-Input", `old=("@status")`)
			writer.Header().Set("Signature", "old=:AA==:")
		}), want: ErrExistingSignatures},
		{name: "malformed input", configure: func(config *ResponseSigningMiddlewareConfig) { config.Existing = ExistingSignaturesAppend }, handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Signature-Input", "bad")
			writer.Header().Set("Signature", "old=:AA==:")
		}), want: ErrExistingSignatures},
		{name: "malformed signature", configure: func(config *ResponseSigningMiddlewareConfig) { config.Existing = ExistingSignaturesAppend }, handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Signature-Input", `old=("@status")`)
			writer.Header().Set("Signature", "bad")
		}), want: ErrExistingSignatures},
		{name: "label mismatch", configure: func(config *ResponseSigningMiddlewareConfig) { config.Existing = ExistingSignaturesAppend }, handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Signature-Input", `old=("@status")`)
			writer.Header().Set("Signature", "other=:AA==:")
		}), want: ErrExistingSignatures},
		{name: "options", configure: func(config *ResponseSigningMiddlewareConfig) {
			config.Options = func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
				return SigningOptions{}, errors.New("private")
			}
		}, handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), want: ErrHTTPIntegrationSigning},
		{name: "external", configure: func(config *ResponseSigningMiddlewareConfig) {
			config.ExternalContext = func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error) {
				return nil, errors.New("private")
			}
		}, handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), want: ErrHTTPIntegrationSigning},
		{name: "sign", configure: func(config *ResponseSigningMiddlewareConfig) {
			config.Options = func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
				return SigningOptions{Nonce: "forbidden"}, nil
			}
		}, handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), want: ErrHTTPIntegrationSigning},
		{name: "duplicate append", configure: func(config *ResponseSigningMiddlewareConfig) { config.Existing = ExistingSignaturesAppend }, handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Signature-Input", `response=("@status")`)
			writer.Header().Set("Signature", "response=:AA==:")
		}), want: ErrExistingSignatures},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			config := base()
			var got error
			config.MapError = func(_ http.ResponseWriter, _ *http.Request, err error) { got = err }
			if test.configure != nil {
				test.configure(&config)
			}
			middleware, err := NewResponseSigningMiddleware(config)
			if err != nil {
				t.Fatal(err)
			}
			middleware(test.handler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "https://example.com/data", nil))
			if !errors.Is(got, test.want) {
				t.Fatalf("mapped error = %v, want %v", got, test.want)
			}
		})
	}

	for _, config := range []ResponseSigningMiddlewareConfig{
		{},
		{Signer: NewSigner(testResponseSigningProfile(t, now, key)), Label: "response", Existing: ExistingSignaturesReject, MaxBufferedBytes: 1, ContentDigestAlgorithms: []DigestAlgorithm{SHA256}, Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
			return SigningOptions{}, nil
		}, MapError: func(http.ResponseWriter, *http.Request, error) {}},
		{Signer: NewSigner(responseDigestSigningProfile(t, now, key)), Label: "response", Existing: ExistingSignaturesReject, MaxBufferedBytes: 1, ContentDigestAlgorithms: []DigestAlgorithm{"unsupported"}, Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
			return SigningOptions{}, nil
		}, MapError: func(http.ResponseWriter, *http.Request, error) {}},
	} {
		if _, err := NewResponseSigningMiddleware(config); !errors.Is(err, ErrInvalidHTTPIntegration) {
			t.Fatalf("invalid constructor error = %v for %#v", err, config.ContentDigestAlgorithms)
		}
	}
}

func TestResponseSigningMiddlewareAppendsExistingSignature(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
		Signer: NewSigner(testResponseSigningProfile(t, now, key)), Label: "response", Existing: ExistingSignaturesAppend, MaxBufferedBytes: 8,
		Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
			return SigningOptions{}, nil
		},
		MapError: func(_ http.ResponseWriter, _ *http.Request, err error) { t.Fatalf("MapError() = %v", err) },
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Signature-Input", `old=("@status")`)
		writer.Header().Set("Signature", "old=:AA==:")
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://example.com/data", nil))
	inputs, err := ParseSignatureInputs(recorder.Header().Values("Signature-Input"))
	if err != nil || len(inputs.Entries()) != 2 || inputs.Entries()[0].Label != "old" || inputs.Entries()[1].Label != "response" {
		t.Fatalf("appended inputs = %#v, %v", inputs.Entries(), err)
	}
}
