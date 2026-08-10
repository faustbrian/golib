package httpsignature

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (transport roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type observedBody struct {
	reader *strings.Reader
	reads  int
	closed bool
}

func (body *observedBody) Read(buffer []byte) (int, error) {
	body.reads++
	return body.reader.Read(buffer)
}

func (body *observedBody) Close() error {
	body.closed = true
	return nil
}

func TestSigningRoundTripperSignsCloneWithoutPreReadingOrReplacingBody(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	profile := testSigningProfile(t, now, key)
	body := &observedBody{reader: strings.NewReader("payload")}
	request, err := http.NewRequest(http.MethodPost, "https://example.com/pay", body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header = nil

	transport, err := NewSigningRoundTripper(SigningRoundTripperConfig{
		Transport: roundTripperFunc(func(signedRequest *http.Request) (*http.Response, error) {
			if signedRequest == request {
				t.Fatal("RoundTrip() forwarded the caller request instead of a clone")
			}
			if signedRequest.Header.Get("Signature-Input") == "" || signedRequest.Header.Get("Signature") == "" {
				t.Fatal("RoundTrip() did not attach signature fields")
			}
			if body.reads != 0 || signedRequest.Body != body {
				t.Fatalf("body before transport = %#v, reads = %d", signedRequest.Body, body.reads)
			}
			content, readErr := io.ReadAll(signedRequest.Body)
			if readErr != nil || string(content) != "payload" {
				t.Fatalf("transport body = %q, error = %v", content, readErr)
			}
			_ = signedRequest.Body.Close()
			return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil)), Request: signedRequest}, nil
		}),
		Signer:   NewSigner(profile),
		Label:    "sig",
		Existing: ExistingSignaturesReject,
		Options: func(context.Context, *http.Request) (SigningOptions, error) {
			return SigningOptions{}, nil
		},
		ExternalContext: func(context.Context, *http.Request) (*ExternalRequestContext, error) {
			return &ExternalRequestContext{Scheme: "https", Authority: "example.com", RequestTarget: "/pay"}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSigningRoundTripper() error = %v", err)
	}

	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	_ = response.Body.Close()
	if request.Header.Get("Signature-Input") != "" || request.Header.Get("Signature") != "" {
		t.Fatal("RoundTrip() mutated caller headers")
	}
	if !body.closed {
		t.Fatal("underlying transport did not retain body ownership")
	}
}

func TestRequestVerificationMiddlewareVerifiesWithoutReadingBodyAndMapsFailures(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	signingProfile := testSigningProfile(t, now, key)
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/pay", nil)
	signed, err := NewSigner(signingProfile).Sign(context.Background(), MessageContext{Request: request}, "sig", SigningOptions{})
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	verificationProfile := testHTTPVerificationProfile(t, now, key)
	middleware, err := NewRequestVerificationMiddleware(RequestVerificationMiddlewareConfig{
		Verifier: NewVerifier(verificationProfile),
		SelectLabel: func(_ *http.Request, _ SignatureInputs, _ Signatures) (string, error) {
			return "sig", nil
		},
		MapError: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "signature rejected", http.StatusUnauthorized)
		},
		ExternalContext: func(context.Context, *http.Request) (*ExternalRequestContext, error) {
			return &ExternalRequestContext{Scheme: "https", Authority: "example.com", RequestTarget: "/pay"}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRequestVerificationMiddleware() error = %v", err)
	}

	nextCalls := 0
	handler := middleware(http.HandlerFunc(func(writer http.ResponseWriter, verifiedRequest *http.Request) {
		nextCalls++
		verified, ok := VerifiedSignatureFromContext(verifiedRequest.Context())
		if !ok || verified.Label != "sig" || verified.KeyID != "key" {
			t.Fatalf("VerifiedSignatureFromContext() = %#v, %t", verified, ok)
		}
		body := verifiedRequest.Body.(*observedBody)
		if body.reads != 0 {
			t.Fatalf("middleware pre-read body %d times", body.reads)
		}
		content, _ := io.ReadAll(body)
		_, _ = writer.Write(content)
	}))

	validBody := &observedBody{reader: strings.NewReader("payload")}
	valid := httptest.NewRequest(http.MethodPost, "https://example.com/pay", validBody)
	valid.Header.Set("Signature-Input", signed.SignatureInputField())
	valid.Header.Set("Signature", signed.SignatureField())
	validResponse := httptest.NewRecorder()
	handler.ServeHTTP(validResponse, valid)
	if validResponse.Code != http.StatusOK || validResponse.Body.String() != "payload" || nextCalls != 1 {
		t.Fatalf("valid response = %d %q, next calls = %d", validResponse.Code, validResponse.Body.String(), nextCalls)
	}

	invalid := httptest.NewRequest(http.MethodPost, "https://example.com/pay", strings.NewReader("payload"))
	invalid.Header.Set("Signature-Input", signed.SignatureInputField())
	invalid.Header.Set("Signature", `sig=:YmFk:`)
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusUnauthorized || nextCalls != 1 {
		t.Fatalf("invalid response = %d, next calls = %d", invalidResponse.Code, nextCalls)
	}
}

func TestHTTPAdaptersRequireExplicitPolicyAndRejectExistingFields(t *testing.T) {
	t.Parallel()

	if _, err := NewSigningRoundTripper(SigningRoundTripperConfig{}); !errors.Is(err, ErrInvalidHTTPIntegration) {
		t.Fatalf("NewSigningRoundTripper() error = %v", err)
	}
	if _, err := NewRequestVerificationMiddleware(RequestVerificationMiddlewareConfig{}); !errors.Is(err, ErrInvalidHTTPIntegration) {
		t.Fatalf("NewRequestVerificationMiddleware() error = %v", err)
	}

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	body := &observedBody{reader: strings.NewReader("payload")}
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/pay", body)
	request.Header.Set("Signature-Input", `old=("@method")`)
	request.Header.Set("Signature", `old=:b2xk:`)
	optionsCalls := 0
	transportCalls := 0
	transport, err := NewSigningRoundTripper(SigningRoundTripperConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			transportCalls++
			return nil, errors.New("must not run")
		}),
		Signer:   NewSigner(testSigningProfile(t, now, key)),
		Label:    "sig",
		Existing: ExistingSignaturesReject,
		Options: func(context.Context, *http.Request) (SigningOptions, error) {
			optionsCalls++
			return SigningOptions{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSigningRoundTripper() error = %v", err)
	}
	if _, err := transport.RoundTrip(request); !errors.Is(err, ErrExistingSignatures) {
		t.Fatalf("RoundTrip() error = %v, want ErrExistingSignatures", err)
	}
	if optionsCalls != 0 || transportCalls != 0 || !body.closed || body.reads != 0 {
		t.Fatalf("rejected request: options=%d transport=%d closed=%t reads=%d", optionsCalls, transportCalls, body.closed, body.reads)
	}
}

func TestSigningRoundTripperAppendsExistingLabelsInWireOrder(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	request.Header.Set("Signature-Input", `old=("@method")`)
	request.Header.Set("Signature", `old=:b2xk:`)
	transport, err := NewSigningRoundTripper(SigningRoundTripperConfig{
		Transport: roundTripperFunc(func(signedRequest *http.Request) (*http.Response, error) {
			inputs, parseErr := ParseSignatureInputs(signedRequest.Header.Values("Signature-Input"))
			if parseErr != nil {
				t.Fatalf("ParseSignatureInputs() error = %v", parseErr)
			}
			signatures, parseErr := ParseSignatures(signedRequest.Header.Values("Signature"))
			if parseErr != nil {
				t.Fatalf("ParseSignatures() error = %v", parseErr)
			}
			inputEntries, signatureEntries := inputs.Entries(), signatures.Entries()
			if len(inputEntries) != 2 || inputEntries[0].Label != "old" || inputEntries[1].Label != "new" ||
				len(signatureEntries) != 2 || signatureEntries[0].Label != "old" || signatureEntries[1].Label != "new" {
				t.Fatalf("appended fields = %#v, %#v", inputEntries, signatureEntries)
			}
			return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
		}),
		Signer: NewSigner(testSigningProfile(t, now, key)), Label: "new", Existing: ExistingSignaturesAppend,
		Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
	})
	if err != nil {
		t.Fatalf("NewSigningRoundTripper() error = %v", err)
	}
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if request.Header.Get("Signature") != `old=:b2xk:` {
		t.Fatal("RoundTrip() mutated caller signature fields")
	}
}

func TestResponseSigningMiddlewareBuffersAndSignsRelatedRequestComponents(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "@method", Parameters: []Parameter{{Name: "req", Value: true}}},
		},
		Expires:            ParameterRequired,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterForbidden,
		Tag:                ParameterForbidden,
		Lifetime:           time.Minute,
		ResolveTimeout:     time.Second,
		Now:                func() time.Time { return now },
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewSigningProfile() error = %v", err)
	}
	middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
		Signer:           NewSigner(profile),
		Label:            "res",
		Existing:         ExistingSignaturesReject,
		MaxBufferedBytes: 16,
		Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
			return SigningOptions{}, nil
		},
		ExternalContext: func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error) {
			return &ExternalRequestContext{Scheme: "https", Authority: "example.com", RequestTarget: "/pay"}, nil
		},
		MapError: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "response signing failed", http.StatusInternalServerError)
		},
	})
	if err != nil {
		t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "https://example.com/pay", nil)
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte("created"))
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || recorder.Body.String() != "created" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	inputs, err := ParseSignatureInputs(recorder.Header().Values("Signature-Input"))
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	signatures, err := ParseSignatures(recorder.Header().Values("Signature"))
	if err != nil {
		t.Fatalf("ParseSignatures() error = %v", err)
	}
	verificationProfile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "@method", Parameters: []Parameter{{Name: "req", Value: true}}},
		},
		Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden, MaxAge: time.Minute, ClockSkew: time.Second,
		ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	response := recorder.Result()
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("response body Close() error = %v", closeErr)
		}
	}()
	if _, err := NewVerifier(verificationProfile).Verify(context.Background(), MessageContext{Response: response, RelatedRequest: request}, "res", inputs, signatures); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestResponseSigningMiddlewareFailsClosedOnBufferLimit(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
		Signer: NewSigner(testSigningProfile(t, now, key)), Label: "sig", Existing: ExistingSignaturesReject,
		MaxBufferedBytes: 3,
		Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
			return SigningOptions{}, nil
		},
		MapError: func(writer http.ResponseWriter, _ *http.Request, err error) {
			if !errors.Is(err, ErrResponseBodyTooLarge) {
				t.Errorf("MapError() error = %v", err)
			}
			http.Error(writer, "too large", http.StatusInternalServerError)
		},
	})
	if err != nil {
		t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("four"))
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://example.com/", nil))
	if recorder.Code != http.StatusInternalServerError || recorder.Body.String() != "too large\n" || recorder.Header().Get("Signature") != "" {
		t.Fatalf("response = %d %q, signature = %q", recorder.Code, recorder.Body.String(), recorder.Header().Get("Signature"))
	}
}

func TestBufferedResponseWriterRejectsInvalidHTTPStatus(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("WriteHeader(99) did not panic")
		}
	}()
	newBufferedResponseWriter(1).WriteHeader(99)
}

func TestBufferedResponseWriterUsesFinalStatusAfterInformationalResponse(t *testing.T) {
	t.Parallel()

	writer := newBufferedResponseWriter(1)
	writer.Header().Set("Link", "</style.css>; rel=preload")
	writer.WriteHeader(http.StatusEarlyHints)
	writer.Header().Set("Content-Type", "text/plain")
	writer.WriteHeader(http.StatusCreated)
	response := writer.response(httptest.NewRequest(http.MethodGet, "https://example.com/", nil))
	if response.StatusCode != http.StatusCreated || response.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("response status = %d, headers = %#v", response.StatusCode, response.Header)
	}
}

func TestResponseSigningMiddlewareComputesCoveredContentDigest(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
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
		t.Fatalf("NewSigningProfile() error = %v", err)
	}
	middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
		Signer: NewSigner(profile), Label: "res", Existing: ExistingSignaturesReject, MaxBufferedBytes: 16,
		ContentDigestAlgorithms: []DigestAlgorithm{SHA256},
		Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
			return SigningOptions{}, nil
		},
		MapError: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "failed", http.StatusInternalServerError)
		},
	})
	if err != nil {
		t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte("payload")) })).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "https://example.com/data", nil),
	)
	digest, err := ParseDigestFields(recorder.Header().Values("Content-Digest"))
	if err != nil || digest.Verify([]byte("payload"), []DigestAlgorithm{SHA256}) != nil {
		t.Fatalf("Content-Digest = %q, parse error = %v", recorder.Header().Get("Content-Digest"), err)
	}
	inputs, _ := ParseSignatureInputs(recorder.Header().Values("Signature-Input"))
	entries := inputs.Entries()
	if len(entries) != 1 || len(entries[0].Components) != 2 || entries[0].Components[1].Name != "content-digest" {
		t.Fatalf("Signature-Input = %#v", entries)
	}
}

func TestResponseSigningMiddlewareDigestsAndEmitsActualHEADContent(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
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
		t.Fatalf("NewSigningProfile() error = %v", err)
	}
	middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
		Signer: NewSigner(profile), Label: "res", Existing: ExistingSignaturesReject, MaxBufferedBytes: 16,
		ContentDigestAlgorithms: []DigestAlgorithm{SHA256},
		Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
			return SigningOptions{}, nil
		},
		MapError: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "failed", http.StatusInternalServerError)
		},
	})
	if err != nil {
		t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("representation"))
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "https://example.com/data", nil))
	if recorder.Body.Len() != 0 {
		t.Fatalf("HEAD response body = %q, want empty", recorder.Body.String())
	}
	digests, err := ParseDigestFields(recorder.Header().Values("Content-Digest"))
	if err != nil || digests.Verify(nil, []DigestAlgorithm{SHA256}) != nil {
		t.Fatalf("Content-Digest = %q, parse error = %v", recorder.Header().Get("Content-Digest"), err)
	}
}

func TestBufferedResponseWriterRejectsBodyForBodylessStatus(t *testing.T) {
	t.Parallel()

	writer := newBufferedResponseWriter(8)
	writer.WriteHeader(http.StatusNoContent)
	count, err := writer.Write([]byte("payload"))
	if !errors.Is(err, http.ErrBodyNotAllowed) || count != 0 || writer.body.Len() != 0 {
		t.Fatalf("Write() = %d, %v, body = %q", count, err, writer.body.String())
	}
}

func TestVerifyingRoundTripperVerifiesResponseWithoutReadingBody(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request := httptest.NewRequest(http.MethodGet, "https://example.com/data", nil)
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &observedBody{reader: strings.NewReader("payload")}, Request: request}
	signed, err := NewSigner(testResponseSigningProfile(t, now, key)).Sign(context.Background(), MessageContext{Response: response, RelatedRequest: request}, "res", SigningOptions{})
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	response.Header.Set("Signature-Input", signed.SignatureInputField())
	response.Header.Set("Signature", signed.SignatureField())
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:   roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:    NewVerifier(testResponseVerificationProfile(t, now, key)),
		SelectLabel: func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "res", nil },
		ExternalContext: func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error) {
			return &ExternalRequestContext{Scheme: "https", Authority: "example.com", RequestTarget: "/data"}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}
	verifiedResponse, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	body := verifiedResponse.Body.(*observedBody)
	if body.reads != 0 || body.closed {
		t.Fatalf("verified body reads = %d, closed = %t", body.reads, body.closed)
	}
	verified, ok := VerifiedSignatureFromResponse(verifiedResponse)
	if !ok || verified.Label != "res" {
		t.Fatalf("VerifiedSignatureFromResponse() = %#v, %t", verified, ok)
	}

	response.Header.Set("Signature", "res=:YmFk:")
	if _, err := transport.RoundTrip(request); err == nil || !body.closed {
		t.Fatalf("invalid RoundTrip() error = %v, body closed = %t", err, body.closed)
	} else {
		var verificationError *VerificationError
		if !errors.Is(err, ErrHTTPIntegrationVerification) || !errors.As(err, &verificationError) || verificationError.Failure != VerificationCryptographic {
			t.Fatalf("invalid RoundTrip() error = %#v", err)
		}
	}
}

func testResponseSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{{Name: "@status"}, {Name: "@method", Parameters: []Parameter{{Name: "req", Value: true}}}},
		Expires:           ParameterRequired, AlgorithmParameter: ParameterRequired, Nonce: ParameterForbidden, Tag: ParameterForbidden,
		Lifetime: time.Minute, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewSigningProfile() error = %v", err)
	}
	return profile
}

func testResponseVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{{Name: "@status"}, {Name: "@method", Parameters: []Parameter{{Name: "req", Value: true}}}},
		Created:            ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden, MaxAge: time.Minute, ClockSkew: time.Second,
		ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	return profile
}

func testSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()

	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		CoveredComponents:  []ComponentIdentifier{{Name: "@method"}, {Name: "@authority"}},
		Expires:            ParameterRequired,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterForbidden,
		Tag:                ParameterForbidden,
		Lifetime:           time.Minute,
		ResolveTimeout:     time.Second,
		Now:                func() time.Time { return now },
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewSigningProfile() error = %v", err)
	}
	return profile
}

func testHTTPVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()

	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{{Name: "@method"}, {Name: "@authority"}},
		Created:            ParameterRequired,
		Expires:            ParameterRequired,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterForbidden,
		Tag:                ParameterForbidden,
		MaxAge:             time.Minute,
		ClockSkew:          time.Second,
		ResolveTimeout:     time.Second,
		Now:                func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	return profile
}
