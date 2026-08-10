package httpsignature

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBufferedContentDigestRoundTripperDigestsCloneAndMakesBodyReplayable(t *testing.T) {
	t.Parallel()

	originalBody := &observedBody{reader: strings.NewReader("payload")}
	request := httptest.NewRequest(http.MethodPost, "https://example.com/upload", originalBody)
	request.GetBody = nil
	transport, err := NewBufferedContentDigestRoundTripper(BufferedContentDigestRoundTripperConfig{
		Transport: roundTripperFunc(func(digested *http.Request) (*http.Response, error) {
			if digested == request || digested.Header.Get("Content-Digest") == "" {
				t.Fatal("RoundTrip() did not pass a separately digested request")
			}
			body, readErr := io.ReadAll(digested.Body)
			if readErr != nil || string(body) != "payload" {
				t.Fatalf("body = %q, error = %v", body, readErr)
			}
			replay, replayErr := digested.GetBody()
			if replayErr != nil {
				t.Fatalf("GetBody() error = %v", replayErr)
			}
			defer func() {
				if closeErr := replay.Close(); closeErr != nil {
					t.Errorf("replay Close() error = %v", closeErr)
				}
			}()
			replayed, replayReadErr := io.ReadAll(replay)
			if replayReadErr != nil {
				t.Fatalf("replay ReadAll() error = %v", replayReadErr)
			}
			if string(replayed) != "payload" || digested.ContentLength != 7 {
				t.Fatalf("replay = %q, length = %d", replayed, digested.ContentLength)
			}
			_ = digested.Body.Close()
			return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
		}),
		Algorithms: []DigestAlgorithm{SHA512, SHA256},
		MaxBytes:   8,
	})
	if err != nil {
		t.Fatalf("NewBufferedContentDigestRoundTripper() error = %v", err)
	}
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if !originalBody.closed || originalBody.reads == 0 {
		t.Fatalf("original body closed = %t, reads = %d", originalBody.closed, originalBody.reads)
	}
	if request.Header.Get("Content-Digest") != "" || request.Body != originalBody {
		t.Fatal("RoundTrip() mutated caller request fields")
	}
}

func TestTrailerSigningRoundTripperStreamsDigestAndSignatureAtEOF(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	signingProfile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@method"},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired, Nonce: ParameterForbidden, Tag: ParameterForbidden,
		Lifetime: time.Minute, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewSigningProfile() error = %v", err)
	}
	body := &observedBody{reader: strings.NewReader("payload")}
	request := httptest.NewRequest(http.MethodPost, "https://example.com/upload", body)
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("payload")), nil }
	transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
		Transport: roundTripperFunc(func(streamed *http.Request) (*http.Response, error) {
			if body.reads != 0 {
				t.Fatalf("adapter pre-read body %d times", body.reads)
			}
			if streamed.Trailer.Get("Content-Digest") != "" || streamed.Trailer.Get("Signature") != "" {
				t.Fatal("trailers were populated before EOF")
			}
			if streamed.GetBody != nil {
				t.Fatal("streaming request incorrectly advertised replayability")
			}
			content, readErr := io.ReadAll(streamed.Body)
			if readErr != nil || string(content) != "payload" {
				t.Fatalf("streamed body = %q, error = %v", content, readErr)
			}
			_ = streamed.Body.Close()
			if streamed.Trailer.Get("Content-Digest") == "" || streamed.Trailer.Get("Signature-Input") == "" || streamed.Trailer.Get("Signature") == "" {
				t.Fatalf("trailers after EOF = %#v", streamed.Trailer)
			}
			inputs, parseErr := ParseSignatureInputs(streamed.Trailer.Values("Signature-Input"))
			if parseErr != nil {
				t.Fatalf("ParseSignatureInputs() error = %v", parseErr)
			}
			signatures, parseErr := ParseSignatures(streamed.Trailer.Values("Signature"))
			if parseErr != nil {
				t.Fatalf("ParseSignatures() error = %v", parseErr)
			}
			verificationProfile := testTrailerVerificationProfile(t, now, key)
			if _, verifyErr := NewVerifier(verificationProfile).Verify(streamed.Context(), MessageContext{Request: streamed}, "trail", inputs, signatures); verifyErr != nil {
				t.Fatalf("Verify() error = %v", verifyErr)
			}
			return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
		}),
		Signer: NewSigner(signingProfile), Label: "trail", Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 8,
		Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		ExternalContext: func(context.Context, *http.Request) (*ExternalRequestContext, error) {
			return &ExternalRequestContext{Scheme: "https", Authority: "example.com", RequestTarget: "/upload"}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
	}
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if !body.closed {
		t.Fatal("transport did not close streaming body")
	}
}

func testTrailerVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@method"},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
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
	return profile
}

type trailerPopulatingBody struct {
	reader  *strings.Reader
	trailer http.Header
	values  http.Header
}

func (body *trailerPopulatingBody) Read(buffer []byte) (int, error) {
	count, err := body.reader.Read(buffer)
	if errors.Is(err, io.EOF) {
		for name, values := range body.values {
			body.trailer[name] = append([]string(nil), values...)
		}
	}
	return count, err
}

func (*trailerPopulatingBody) Close() error { return nil }

type closeFailingBody struct {
	reader *strings.Reader
}

func (body *closeFailingBody) Read(buffer []byte) (int, error) { return body.reader.Read(buffer) }
func (*closeFailingBody) Close() error                         { return errors.New("close failed with sensitive detail") }

type zeroProgressBody struct{}

func (zeroProgressBody) Read([]byte) (int, error) { return 0, nil }
func (zeroProgressBody) Close() error             { return nil }

func TestBufferedBodyReadFailsClosedWhenOwnershipCannotBeReleased(t *testing.T) {
	t.Parallel()

	content, err := readBoundedAndClose(context.Background(), &closeFailingBody{reader: strings.NewReader("payload")}, 8)
	if !errors.Is(err, ErrBodyRead) || content != nil {
		t.Fatalf("readBoundedAndClose() = %q, %v, want nil ErrBodyRead", content, err)
	}
	if strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("readBoundedAndClose() disclosed close error: %q", err)
	}
}

func TestTrailerSigningBodyRejectsZeroProgressReader(t *testing.T) {
	t.Parallel()

	body := &trailerSigningBody{
		body: zeroProgressBody{}, ctx: context.Background(), maxBytes: 1,
		finalize: func(DigestField) error { return nil },
	}
	if count, err := body.Read(make([]byte, 1)); !errors.Is(err, ErrBodyRead) || count != 0 {
		t.Fatalf("Read() = %d, %v, want 0, ErrBodyRead", count, err)
	}
}

func TestBufferedTrailerVerificationMiddlewareWaitsForEOFAndVerifiesDigestAndSignature(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request := httptest.NewRequest(http.MethodPost, "https://example.com/upload", nil)
	field, _ := ComputeDigests([]DigestAlgorithm{SHA256}, []byte("payload"))
	request.Trailer = http.Header{"Content-Digest": []string{field.String()}}
	signed, err := NewSigner(testTrailerSigningProfile(t, now, key)).Sign(context.Background(), MessageContext{Request: request}, "trail", SigningOptions{})
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	values := http.Header{
		"Content-Digest":  []string{field.String()},
		"Signature-Input": []string{signed.SignatureInputField()},
		"Signature":       []string{signed.SignatureField()},
	}
	request.Trailer = http.Header{"Content-Digest": nil, "Signature-Input": nil, "Signature": nil}
	request.Body = &trailerPopulatingBody{reader: strings.NewReader("payload"), trailer: request.Trailer, values: values}

	middleware, err := NewBufferedTrailerVerificationMiddleware(BufferedTrailerVerificationMiddlewareConfig{
		Verifier:           NewVerifier(testTrailerVerificationProfile(t, now, key)),
		SelectLabel:        func(*http.Request, SignatureInputs, Signatures) (string, error) { return "trail", nil },
		RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 8,
		ExternalContext: func(context.Context, *http.Request) (*ExternalRequestContext, error) {
			return &ExternalRequestContext{Scheme: "https", Authority: "example.com", RequestTarget: "/upload"}, nil
		},
		MapError: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "trailer rejected", http.StatusBadRequest)
		},
	})
	if err != nil {
		t.Fatalf("NewBufferedTrailerVerificationMiddleware() error = %v", err)
	}
	nextCalls := 0
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, verifiedRequest *http.Request) {
		nextCalls++
		if _, ok := VerifiedSignatureFromContext(verifiedRequest.Context()); !ok {
			t.Fatal("verified signature metadata missing")
		}
		content, _ := io.ReadAll(verifiedRequest.Body)
		if string(content) != "payload" {
			t.Fatalf("next body = %q", content)
		}
		replay, replayErr := verifiedRequest.GetBody()
		if replayErr != nil {
			t.Fatalf("GetBody() error = %v", replayErr)
		}
		_ = replay.Close()
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || nextCalls != 1 {
		t.Fatalf("response = %d, next = %d", recorder.Code, nextCalls)
	}
}

func TestBufferedTrailerVerifyingRoundTripperWaitsForEOFAndVerifiesResponse(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request := httptest.NewRequest(http.MethodGet, "https://example.com/data", nil)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Trailer:    make(http.Header),
		Request:    request,
	}
	field, _ := ComputeDigests([]DigestAlgorithm{SHA256}, []byte("payload"))
	response.Trailer.Set("Content-Digest", field.String())
	signed, err := NewSigner(testResponseTrailerSigningProfile(t, now, key)).Sign(
		context.Background(), MessageContext{Response: response, RelatedRequest: request}, "trail", SigningOptions{},
	)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	values := http.Header{
		"Content-Digest":  []string{field.String()},
		"Signature-Input": []string{signed.SignatureInputField()},
		"Signature":       []string{signed.SignatureField()},
	}
	response.Trailer = http.Header{"Content-Digest": nil, "Signature-Input": nil, "Signature": nil}
	response.Body = &trailerPopulatingBody{reader: strings.NewReader("payload"), trailer: response.Trailer, values: values}

	transport, err := NewBufferedTrailerVerifyingRoundTripper(BufferedTrailerVerifyingRoundTripperConfig{
		Transport:          roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:           NewVerifier(testResponseTrailerVerificationProfile(t, now, key)),
		SelectLabel:        func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "trail", nil },
		RequiredAlgorithms: []DigestAlgorithm{SHA256},
		MaxBytes:           8,
		ExternalContext: func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error) {
			return &ExternalRequestContext{Scheme: "https", Authority: "example.com", RequestTarget: "/data"}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewBufferedTrailerVerifyingRoundTripper() error = %v", err)
	}
	verifiedResponse, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	content, err := io.ReadAll(verifiedResponse.Body)
	if err != nil || string(content) != "payload" {
		t.Fatalf("verified body = %q, error = %v", content, err)
	}
	verified, ok := VerifiedSignatureFromResponse(verifiedResponse)
	if !ok || verified.Label != "trail" {
		t.Fatalf("VerifiedSignatureFromResponse() = %#v, %t", verified, ok)
	}
}

func TestTrailerResponseSigningMiddlewareStreamsSignedDigestTrailers(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	reported := make(chan error, 1)
	middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
		Signer:     NewSigner(testResponseTrailerSigningProfile(t, now, key)),
		Label:      "trail",
		Algorithms: []DigestAlgorithm{SHA512, SHA256},
		MaxBytes:   8,
		Options:    func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		ExternalContext: func(context.Context, *http.Request) (*ExternalRequestContext, error) {
			return &ExternalRequestContext{Scheme: "http", Authority: "127.0.0.1", RequestTarget: "/data"}, nil
		},
		ReportError: func(_ *http.Request, err error) {
			reported <- err
		},
	})
	if err != nil {
		t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
	}
	server := httptest.NewServer(middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte("payload"))
	})))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/data", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("response body Close() error = %v", closeErr)
		}
	}()
	content, err := io.ReadAll(response.Body)
	if err != nil || string(content) != "payload" {
		t.Fatalf("response body = %q, error = %v", content, err)
	}
	if response.Trailer.Get("Content-Digest") == "" || response.Trailer.Get("Signature-Input") == "" || response.Trailer.Get("Signature") == "" {
		t.Fatalf("response trailers = %#v", response.Trailer)
	}
	digests, err := ParseDigestFields(response.Trailer.Values("Content-Digest"))
	if err != nil || digests.Verify(content, []DigestAlgorithm{SHA256}) != nil {
		t.Fatalf("Content-Digest = %q, parse error = %v", response.Trailer.Get("Content-Digest"), err)
	}
	inputs, err := ParseSignatureInputs(response.Trailer.Values("Signature-Input"))
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	signatures, err := ParseSignatures(response.Trailer.Values("Signature"))
	if err != nil {
		t.Fatalf("ParseSignatures() error = %v", err)
	}
	if _, err := NewVerifier(testResponseTrailerVerificationProfile(t, now, key)).Verify(
		context.Background(), MessageContext{Response: response, RelatedRequest: request}, "trail", inputs, signatures,
	); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	select {
	case err := <-reported:
		t.Fatalf("ReportError() = %v", err)
	default:
	}
}

func TestTrailerResponseWriterKeepsFirstFinalStatus(t *testing.T) {
	t.Parallel()

	stream := &trailerResponseWriter{
		ResponseWriter: httptest.NewRecorder(),
		request:        httptest.NewRequest(http.MethodGet, "https://example.com/", nil),
		maxBytes:       1,
	}
	stream.WriteHeader(http.StatusCreated)
	stream.WriteHeader(http.StatusAccepted)
	if stream.status != http.StatusCreated {
		t.Fatalf("final status = %d, want %d", stream.status, http.StatusCreated)
	}
}

func TestTrailerResponseWriterRejectsProtocolSwitch(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	stream := &trailerResponseWriter{
		ResponseWriter: recorder,
		request:        httptest.NewRequest(http.MethodGet, "https://example.com/", nil),
		maxBytes:       1,
	}
	stream.WriteHeader(http.StatusSwitchingProtocols)
	if !errors.Is(stream.failure, ErrInvalidBodyIntegration) || stream.status != 0 || recorder.Code != http.StatusOK {
		t.Fatalf("protocol switch: failure=%v status=%d emitted=%d", stream.failure, stream.status, recorder.Code)
	}
}

func testResponseTrailerSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired, Nonce: ParameterForbidden, Tag: ParameterForbidden,
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

func testResponseTrailerVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
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
	return profile
}

func testTrailerSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@method"},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired, Nonce: ParameterForbidden, Tag: ParameterForbidden,
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

func TestBufferedContentDigestRoundTripperFailsClosedOnLimitAndExistingField(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		prepare func(*http.Request)
		want    error
	}{
		{name: "limit", want: ErrBodyTooLarge},
		{name: "existing", prepare: func(request *http.Request) { request.Header.Set("Content-Digest", "sha-256=:YWJj:") }, want: ErrExistingDigest},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			body := &observedBody{reader: strings.NewReader("payload")}
			request := httptest.NewRequest(http.MethodPost, "https://example.com/upload", body)
			if test.prepare != nil {
				test.prepare(request)
			}
			calls := 0
			transport, err := NewBufferedContentDigestRoundTripper(BufferedContentDigestRoundTripperConfig{
				Transport:  roundTripperFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("must not run") }),
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 3,
			})
			if err != nil {
				t.Fatalf("NewBufferedContentDigestRoundTripper() error = %v", err)
			}
			if _, err := transport.RoundTrip(request); !errors.Is(err, test.want) {
				t.Fatalf("RoundTrip() error = %v, want %v", err, test.want)
			}
			if calls != 0 || !body.closed {
				t.Fatalf("transport calls = %d, body closed = %t", calls, body.closed)
			}
		})
	}
}

func TestBufferedContentDigestVerificationMiddlewareVerifiesBeforeNext(t *testing.T) {
	t.Parallel()

	field, _ := ComputeDigests([]DigestAlgorithm{SHA256}, []byte("payload"))
	middleware, err := NewBufferedContentDigestVerificationMiddleware(BufferedContentDigestVerificationMiddlewareConfig{
		RequiredAlgorithms: []DigestAlgorithm{SHA256},
		MaxBytes:           8,
		MapError: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "digest rejected", http.StatusBadRequest)
		},
	})
	if err != nil {
		t.Fatalf("NewBufferedContentDigestVerificationMiddleware() error = %v", err)
	}
	nextCalls := 0
	handler := middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		nextCalls++
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil || string(body) != "payload" {
			t.Fatalf("next body = %q, error = %v", body, readErr)
		}
		if request.GetBody == nil {
			t.Fatal("verified request body is not replayable")
		}
		replay, replayErr := request.GetBody()
		if replayErr != nil {
			t.Fatalf("GetBody() error = %v", replayErr)
		}
		_ = replay.Close()
		writer.WriteHeader(http.StatusNoContent)
	}))

	validBody := &observedBody{reader: strings.NewReader("payload")}
	valid := httptest.NewRequest(http.MethodPost, "https://example.com/upload", validBody)
	valid.Header.Set("Content-Digest", field.String())
	validResponse := httptest.NewRecorder()
	handler.ServeHTTP(validResponse, valid)
	if validResponse.Code != http.StatusNoContent || nextCalls != 1 || !validBody.closed {
		t.Fatalf("valid response = %d, next = %d, closed = %t", validResponse.Code, nextCalls, validBody.closed)
	}

	invalid := httptest.NewRequest(http.MethodPost, "https://example.com/upload", strings.NewReader("tampered"))
	invalid.Header.Set("Content-Digest", field.String())
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest || nextCalls != 1 {
		t.Fatalf("invalid response = %d, next = %d", invalidResponse.Code, nextCalls)
	}
}

func TestBufferedContentDigestAdaptersRequireExplicitLimitsAndPolicy(t *testing.T) {
	t.Parallel()

	if _, err := NewBufferedContentDigestRoundTripper(BufferedContentDigestRoundTripperConfig{}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("NewBufferedContentDigestRoundTripper() error = %v", err)
	}
	if _, err := NewBufferedContentDigestVerificationMiddleware(BufferedContentDigestVerificationMiddlewareConfig{}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("NewBufferedContentDigestVerificationMiddleware() error = %v", err)
	}
	if _, err := readBoundedAndClose(context.Background(), nil, 1); err != nil {
		t.Fatalf("readBoundedAndClose(nil) error = %v", err)
	}
}
