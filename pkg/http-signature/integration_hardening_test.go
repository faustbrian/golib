package httpsignature

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestVerifyingRoundTripperDoesNotTrustSignedDigestWithoutVerifyingBody(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	response := signedDigestHeaderResponse(t, now, key, request, []byte("authentic"), &observedBody{reader: strings.NewReader("substituted")})
	body := response.Body.(*observedBody)
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:               roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:                NewVerifier(hardeningResponseDigestVerificationProfile(t, now, key)),
		SelectLabel:             func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "digest", nil },
		ContentDigestAlgorithms: []DigestAlgorithm{SHA256},
		MaxBufferedBytes:        32,
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}

	if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrDigestMismatch) {
		t.Fatalf("RoundTrip() = %#v, %v, want ErrDigestMismatch", got, verifyErr)
	}
	if body.reads == 0 || !body.closed {
		t.Fatalf("substituted body reads = %d, closed = %t", body.reads, body.closed)
	}
}

func TestVerifyingRoundTripperRejectsTransparentDecompressionForContentDigest(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	response := signedDigestHeaderResponse(t, now, key, request, []byte("coded bytes"), &observedBody{reader: strings.NewReader("decoded bytes")})
	response.Uncompressed = true
	body := response.Body.(*observedBody)
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:               roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:                NewVerifier(hardeningResponseDigestVerificationProfile(t, now, key)),
		SelectLabel:             func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "digest", nil },
		ContentDigestAlgorithms: []DigestAlgorithm{SHA256},
		MaxBufferedBytes:        32,
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}

	if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrHTTPIntegrationVerification) {
		t.Fatalf("RoundTrip() = %#v, %v, want ErrHTTPIntegrationVerification", got, verifyErr)
	}
	if body.reads != 0 || !body.closed {
		t.Fatalf("transparent body reads = %d, closed = %t, want 0, true", body.reads, body.closed)
	}
}

func TestVerifyingRoundTripperRejectsDigestCoveredProtocolTransitionsBeforeBodyRead(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name   string
		method string
		status int
		header http.Header
	}{
		{
			name: "101 protocol switch", method: http.MethodGet, status: http.StatusSwitchingProtocols,
			header: http.Header{"Connection": []string{"Upgrade"}, "Upgrade": []string{"example"}},
		},
		{name: "successful CONNECT", method: http.MethodConnect, status: http.StatusOK, header: make(http.Header)},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			request, _ := http.NewRequest(test.method, "https://example.com:443", nil)
			body := &observedBody{reader: strings.NewReader("protocol bytes")}
			digests, _ := ComputeDigests([]DigestAlgorithm{SHA256}, []byte("protocol bytes"))
			response := &http.Response{
				StatusCode: test.status, Header: test.header.Clone(), Body: body, Request: request,
			}
			response.Header.Set("Content-Digest", digests.String())
			signed, err := NewSigner(hardeningResponseDigestSigningProfile(t, now, key)).Sign(
				context.Background(), MessageContext{Response: response}, "digest", SigningOptions{},
			)
			if err != nil {
				t.Fatalf("Sign() error = %v", err)
			}
			response.Header.Set("Signature-Input", signed.SignatureInputField())
			response.Header.Set("Signature", signed.SignatureField())
			transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
				Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
				Verifier:  NewVerifier(hardeningResponseDigestVerificationProfile(t, now, key)),
				SelectLabel: func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) {
					return "digest", nil
				},
				ContentDigestAlgorithms: []DigestAlgorithm{SHA256}, MaxBufferedBytes: 32,
			})
			if err != nil {
				t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
			}
			if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrInvalidHTTPIntegration) {
				t.Fatalf("RoundTrip() = %#v, %v, want protocol transition rejection", got, verifyErr)
			}
			if body.reads != 0 || !body.closed {
				t.Fatalf("protocol body reads=%d closed=%t, want 0, true", body.reads, body.closed)
			}
		})
	}
}

func TestBufferedTrailerVerifyingRoundTripperRejectsProtocolTransitionsBeforeBodyRead(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name   string
		method string
		status int
		header http.Header
	}{
		{
			name: "101 protocol switch", method: http.MethodGet, status: http.StatusSwitchingProtocols,
			header: http.Header{"Connection": []string{"Upgrade"}, "Upgrade": []string{"example"}},
		},
		{name: "successful CONNECT", method: http.MethodConnect, status: http.StatusOK, header: make(http.Header)},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			request, _ := http.NewRequest(test.method, "https://example.com:443", nil)
			body := &observedBody{reader: strings.NewReader("protocol bytes")}
			response := &http.Response{
				StatusCode: test.status, Header: test.header.Clone(), Trailer: make(http.Header), Body: body, Request: request,
			}
			transport, err := NewBufferedTrailerVerifyingRoundTripper(BufferedTrailerVerifyingRoundTripperConfig{
				Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
				Verifier:  NewVerifier(testResponseTrailerVerificationProfile(t, now, key)),
				SelectLabel: func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) {
					return "trail", nil
				},
				RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
			})
			if err != nil {
				t.Fatalf("NewBufferedTrailerVerifyingRoundTripper() error = %v", err)
			}
			if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrInvalidHTTPIntegration) {
				t.Fatalf("RoundTrip() = %#v, %v, want protocol transition rejection", got, verifyErr)
			}
			if body.reads != 0 || !body.closed {
				t.Fatalf("protocol body reads=%d closed=%t, want 0, true", body.reads, body.closed)
			}
		})
	}
}

func TestVerifyingRoundTripperVerifiesContentDigestAndReturnsReplayableCodedBody(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	coded := []byte("application-managed coded bytes")
	receivedBody := &observedBody{reader: strings.NewReader(string(coded))}
	response := signedDigestHeaderResponse(t, now, key, request, coded, receivedBody)
	response.Header.Set("Content-Encoding", "example-coding")
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:               roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:                NewVerifier(hardeningResponseDigestVerificationProfile(t, now, key)),
		SelectLabel:             func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "digest", nil },
		ContentDigestAlgorithms: []DigestAlgorithm{SHA256},
		MaxBufferedBytes:        64,
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}

	verifiedResponse, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	content, err := io.ReadAll(verifiedResponse.Body)
	if err != nil || string(content) != string(coded) {
		t.Fatalf("verified body = %q, %v", content, err)
	}
	if !receivedBody.closed || verifiedResponse.ContentLength != int64(len(coded)) || verifiedResponse.Header.Get("Content-Encoding") != "example-coding" {
		t.Fatalf("received closed=%t length=%d encoding=%q", receivedBody.closed, verifiedResponse.ContentLength, verifiedResponse.Header.Get("Content-Encoding"))
	}
	if _, ok := VerifiedSignatureFromResponse(verifiedResponse); !ok {
		t.Fatal("verified response metadata missing")
	}
}

func TestVerifyingRoundTripperPreservesTrailersPopulatedDuringDigestRead(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	response := signedDigestHeaderResponse(t, now, key, request, []byte("payload"), http.NoBody)
	response.Trailer = http.Header{"X-Final": nil}
	response.Body = &trailerPopulatingBody{
		reader: strings.NewReader("payload"), trailer: response.Trailer,
		values: http.Header{"X-Final": []string{"ready"}},
	}
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:               roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:                NewVerifier(hardeningResponseDigestVerificationProfile(t, now, key)),
		SelectLabel:             func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "digest", nil },
		ContentDigestAlgorithms: []DigestAlgorithm{SHA256}, MaxBufferedBytes: 32,
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}
	verifiedResponse, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if verifiedResponse.Trailer.Get("X-Final") != "ready" {
		t.Fatalf("response trailer = %#v", verifiedResponse.Trailer)
	}
}

func TestVerifyingRoundTripperRejectsProtectedTrailerCollisionPopulatedAtEOF(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	response := signedDigestHeaderResponse(t, now, key, request, []byte("payload"), http.NoBody)
	response.Trailer = http.Header{"Signature": nil}
	response.Body = &trailerPopulatingBody{
		reader: strings.NewReader("payload"), trailer: response.Trailer,
		values: http.Header{"signature": []string{"digest=:YmFk:"}},
	}
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:               roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:                NewVerifier(hardeningResponseDigestVerificationProfile(t, now, key)),
		SelectLabel:             func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "digest", nil },
		ContentDigestAlgorithms: []DigestAlgorithm{SHA256}, MaxBufferedBytes: 32,
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}
	if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrAmbiguousProtectedField) {
		t.Fatalf("RoundTrip() = %#v, %v, want ErrAmbiguousProtectedField", got, verifyErr)
	}
}

func TestVerifyingRoundTripperRejectsCoveredDigestWithoutBoundedPolicy(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	response := signedDigestHeaderResponse(t, now, key, request, []byte("payload"), &observedBody{reader: strings.NewReader("payload")})
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:   roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:    NewVerifier(hardeningResponseDigestVerificationProfile(t, now, key)),
		SelectLabel: func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "digest", nil },
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}
	if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrInvalidHTTPIntegration) {
		t.Fatalf("RoundTrip() = %#v, %v, want ErrInvalidHTTPIntegration", got, verifyErr)
	}
}

func TestVerifyingRoundTripperBoundsContentDigestBody(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	body := &observedBody{reader: strings.NewReader("payload")}
	response := signedDigestHeaderResponse(t, now, key, request, []byte("payload"), body)
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:               roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:                NewVerifier(hardeningResponseDigestVerificationProfile(t, now, key)),
		SelectLabel:             func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "digest", nil },
		ContentDigestAlgorithms: []DigestAlgorithm{SHA256},
		MaxBufferedBytes:        3,
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}
	if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrBodyTooLarge) {
		t.Fatalf("RoundTrip() = %#v, %v, want ErrBodyTooLarge", got, verifyErr)
	}
	if !body.closed {
		t.Fatal("oversized body was not closed")
	}
}

func TestVerifyingRoundTripperRejectsMalformedCoveredContentDigestBeforeBodyRead(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	body := &observedBody{reader: strings.NewReader("payload")}
	response := signedDigestHeaderResponse(t, now, key, request, []byte("payload"), body)
	response.Header.Set("Content-Digest", "malformed")
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:               roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:                NewVerifier(hardeningResponseDigestVerificationProfile(t, now, key)),
		SelectLabel:             func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "digest", nil },
		ContentDigestAlgorithms: []DigestAlgorithm{SHA256},
		MaxBufferedBytes:        32,
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}
	if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrInvalidDigestField) {
		t.Fatalf("RoundTrip() = %#v, %v, want ErrInvalidDigestField", got, verifyErr)
	}
	if body.reads != 0 || !body.closed {
		t.Fatalf("malformed digest body reads=%d closed=%t", body.reads, body.closed)
	}
}

func TestVerifyingRoundTripperRejectsDigestAlgorithmNotAuthenticatedByKeyedCoverage(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	authenticSHA256, _ := ComputeDigests([]DigestAlgorithm{SHA256}, []byte("authentic"))
	substitutedSHA512, _ := ComputeDigests([]DigestAlgorithm{SHA512}, []byte("substituted"))
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Digest": []string{authenticSHA256.String() + ", " + substitutedSHA512.String()},
		},
		Body:    &observedBody{reader: strings.NewReader("substituted")},
		Request: request,
	}
	signed, err := NewSigner(hardeningKeyedDigestSigningProfile(t, now, key)).Sign(
		context.Background(), MessageContext{Response: response}, "digest", SigningOptions{},
	)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	response.Header.Set("Signature-Input", signed.SignatureInputField())
	response.Header.Set("Signature", signed.SignatureField())
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:               roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:                NewVerifier(hardeningKeyedDigestVerificationProfile(t, now, key)),
		SelectLabel:             func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "digest", nil },
		ContentDigestAlgorithms: []DigestAlgorithm{SHA512},
		MaxBufferedBytes:        32,
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}
	if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrInvalidHTTPIntegration) {
		t.Fatalf("RoundTrip() = %#v, %v, want unauthenticated-algorithm rejection", got, verifyErr)
	}
}

func TestVerifyingRoundTripperAcceptsDigestAlgorithmAuthenticatedByKeyedCoverage(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	digests, _ := ComputeDigests([]DigestAlgorithm{SHA256}, []byte("payload"))
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Digest": []string{digests.String()}},
		Body:       &observedBody{reader: strings.NewReader("payload")},
		Request:    request,
	}
	signed, err := NewSigner(hardeningKeyedDigestSigningProfile(t, now, key)).Sign(
		context.Background(), MessageContext{Response: response}, "digest", SigningOptions{},
	)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	response.Header.Set("Signature-Input", signed.SignatureInputField())
	response.Header.Set("Signature", signed.SignatureField())
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:               roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:                NewVerifier(hardeningKeyedDigestVerificationProfile(t, now, key)),
		SelectLabel:             func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "digest", nil },
		ContentDigestAlgorithms: []DigestAlgorithm{SHA256}, MaxBufferedBytes: 32,
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
}

func TestVerifyingRoundTripperRejectsMissingActualSentRequestSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	response := signedDigestHeaderResponse(t, now, key, request, []byte("payload"), &observedBody{reader: strings.NewReader("payload")})
	body := response.Body.(*observedBody)
	response.Request = nil
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:   roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:    NewVerifier(hardeningResponseDigestVerificationProfile(t, now, key)),
		SelectLabel: func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "digest", nil },
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}
	if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrInvalidHTTPIntegration) {
		t.Fatalf("RoundTrip() = %#v, %v, want ErrInvalidHTTPIntegration", got, verifyErr)
	}
	if !body.closed {
		t.Fatal("response without an actual-sent snapshot did not close its body")
	}
}

func TestVerifyingRoundTripperRejectsUnmatchedSelectedLabel(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	response := signedDigestHeaderResponse(t, now, key, request, []byte("payload"), &observedBody{reader: strings.NewReader("payload")})
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:  NewVerifier(hardeningResponseDigestVerificationProfile(t, now, key)),
		SelectLabel: func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) {
			return "missing", nil
		},
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}
	if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrInvalidSignedFields) {
		t.Fatalf("RoundTrip() = %#v, %v, want ErrInvalidSignedFields", got, verifyErr)
	}
}

func TestResponseContentDigestCoverageExcludesRelatedAndTrailerFields(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		component ComponentIdentifier
		want      bool
	}{
		{name: "response header", component: ComponentIdentifier{Name: "content-digest"}, want: true},
		{name: "related request", component: ComponentIdentifier{Name: "content-digest", Parameters: []Parameter{{Name: "req", Value: true}}}},
		{name: "response trailer", component: ComponentIdentifier{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}}},
		{name: "other field", component: ComponentIdentifier{Name: "content-type"}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := SignatureInput{Components: []ComponentIdentifier{test.component}}
			if got := signatureInputCoversResponseContentDigest(input); got != test.want {
				t.Fatalf("signatureInputCoversResponseContentDigest() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestVerifyingRoundTripperRejectsIncompleteDigestPolicies(t *testing.T) {
	t.Parallel()

	base := VerifyingRoundTripperConfig{
		Transport:   roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil }),
		Verifier:    NewVerifier(&VerificationProfile{}),
		SelectLabel: func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "digest", nil },
	}
	for _, mutate := range []func(*VerifyingRoundTripperConfig){
		func(config *VerifyingRoundTripperConfig) { config.ContentDigestAlgorithms = []DigestAlgorithm{SHA256} },
		func(config *VerifyingRoundTripperConfig) { config.MaxBufferedBytes = 1 },
		func(config *VerifyingRoundTripperConfig) {
			config.ContentDigestAlgorithms = []DigestAlgorithm{"unsupported"}
			config.MaxBufferedBytes = 1
		},
	} {
		config := base
		mutate(&config)
		if _, err := NewVerifyingRoundTripper(config); !errors.Is(err, ErrInvalidHTTPIntegration) {
			t.Fatalf("NewVerifyingRoundTripper(%#v) error = %v", config.ContentDigestAlgorithms, err)
		}
	}
}

func TestVerifyingRoundTripperBindsReqComponentsToImmutableActualSentRequest(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	callerRequest, _ := http.NewRequest(http.MethodGet, "https://example.com/caller", nil)
	actualSent, _ := http.NewRequest(http.MethodGet, "https://example.com/actual", nil)
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: actualSent}
	signer := NewSigner(hardeningResponseRequestBindingSigningProfile(t, now, key))
	signed, err := signer.Sign(context.Background(), MessageContext{Response: response, RelatedRequest: actualSent}, "binding", SigningOptions{})
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	response.Header.Set("Signature-Input", signed.SignatureInputField())
	response.Header.Set("Signature", signed.SignatureField())

	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:  NewVerifier(hardeningResponseRequestBindingVerificationProfile(t, now, key)),
		SelectLabel: func(selectedRequest *http.Request, _ *http.Response, _ SignatureInputs, _ Signatures) (string, error) {
			if selectedRequest.URL.Path != "/actual" {
				t.Errorf("SelectLabel request path = %q, want /actual", selectedRequest.URL.Path)
			}
			actualSent.URL.Path = "/mutated-after-send"
			return "binding", nil
		},
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}

	verifiedResponse, err := transport.RoundTrip(callerRequest)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if verifiedResponse.Request == actualSent || verifiedResponse.Request.URL.Path != "/actual" {
		t.Fatalf("verified request = %#v, want immutable /actual snapshot", verifiedResponse.Request)
	}
	if _, ok := VerifiedSignatureFromResponse(verifiedResponse); !ok {
		t.Fatal("verified response metadata missing")
	}
}

func TestVerifyingRoundTripperIsolatesResponseCallbacksFromVerifiedMessage(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	request.TLS = &tls.ConnectionState{OCSPResponse: []byte("request-state")}
	response := signedDigestHeaderResponse(t, now, key, request, []byte("payload"), &observedBody{reader: strings.NewReader("payload")})
	response.TLS = &tls.ConnectionState{OCSPResponse: []byte("response-state")}
	mutateSnapshot := func(requestSnapshot *http.Request, responseSnapshot *http.Response) {
		if responseSnapshot.Body != nil {
			t.Errorf("callback response Body = %#v, want nil isolated snapshot", responseSnapshot.Body)
		}
		if requestSnapshot.TLS != nil {
			requestSnapshot.TLS.OCSPResponse[0] = 'X'
		}
		if responseSnapshot.TLS != nil {
			responseSnapshot.TLS.OCSPResponse[0] = 'X'
		}
		responseSnapshot.Header.Set("Content-Digest", "sha-256=:YmFk:")
		responseSnapshot.Header.Set("Signature", "digest=:YmFk:")
		responseSnapshot.Body = io.NopCloser(strings.NewReader("substituted"))
	}
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:  NewVerifier(hardeningResponseDigestVerificationProfile(t, now, key)),
		SelectLabel: func(requestSnapshot *http.Request, responseSnapshot *http.Response, _ SignatureInputs, _ Signatures) (string, error) {
			mutateSnapshot(requestSnapshot, responseSnapshot)
			return "digest", nil
		},
		ContentDigestAlgorithms: []DigestAlgorithm{SHA256},
		MaxBufferedBytes:        32,
		ExternalContext: func(_ context.Context, requestSnapshot *http.Request, responseSnapshot *http.Response) (*ExternalRequestContext, error) {
			mutateSnapshot(requestSnapshot, responseSnapshot)
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
	}
	verifiedResponse, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	content, err := io.ReadAll(verifiedResponse.Body)
	if err != nil || string(content) != "payload" {
		t.Fatalf("verified body = %q, %v", content, err)
	}
	digests, err := ParseDigestFields(verifiedResponse.Header.Values("Content-Digest"))
	if err != nil || digests.Verify(content, []DigestAlgorithm{SHA256}) != nil {
		t.Fatalf("returned Content-Digest = %q, %v", verifiedResponse.Header.Get("Content-Digest"), err)
	}
	if string(request.TLS.OCSPResponse) != "request-state" || string(response.TLS.OCSPResponse) != "response-state" {
		t.Fatalf("callback mutated shared TLS state: request=%q response=%q", request.TLS.OCSPResponse, response.TLS.OCSPResponse)
	}
}

func TestCallbackSnapshotsRemoveMutableTLSState(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	request.TLS = &tls.ConnectionState{OCSPResponse: []byte("request-state")}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Trailer:    make(http.Header),
		Body:       http.NoBody,
		Request:    request,
		TLS:        &tls.ConnectionState{OCSPResponse: []byte("response-state")},
	}

	if snapshot := cloneRequestSnapshot(request); snapshot.TLS != nil {
		t.Fatalf("request callback snapshot exposed TLS state: %#v", snapshot.TLS)
	}
	responseSnapshot := responseCallbackSnapshot(response, request)
	if responseSnapshot.TLS != nil || responseSnapshot.Request.TLS != nil {
		t.Fatalf("response callback snapshot exposed TLS state: response=%#v request=%#v", responseSnapshot.TLS, responseSnapshot.Request.TLS)
	}
}

func TestTrailerSigningRoundTripperUsesHTTP1ChunkedFraming(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name    string
		method  string
		content string
		reject  bool
	}{
		{name: "POST body", method: http.MethodPost, content: "payload"},
		{name: "POST empty", method: http.MethodPost},
		{name: "GET body", method: http.MethodGet, content: "payload"},
		{name: "GET empty", method: http.MethodGet},
		{name: "DELETE body", method: http.MethodDelete, content: "payload"},
		{name: "DELETE empty", method: http.MethodDelete},
		{name: "OPTIONS body", method: http.MethodOptions, content: "payload"},
		{name: "OPTIONS empty", method: http.MethodOptions},
		{name: "HEAD body", method: http.MethodHead, content: "payload"},
		{name: "HEAD empty", method: http.MethodHead},
		{name: "CONNECT body", method: http.MethodConnect, content: "payload", reject: true},
		{name: "CONNECT empty", method: http.MethodConnect, reject: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			type receivedRequest struct {
				body          string
				contentLength int64
				encoding      []string
				trailer       http.Header
			}
			received := make(chan receivedRequest, 1)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				content, readErr := io.ReadAll(request.Body)
				if readErr != nil {
					http.Error(writer, "read failed", http.StatusBadRequest)
					return
				}
				received <- receivedRequest{
					body: string(content), contentLength: request.ContentLength,
					encoding: append([]string(nil), request.TransferEncoding...), trailer: request.Trailer.Clone(),
				}
				writer.WriteHeader(http.StatusNoContent)
			}))
			server.EnableHTTP2 = false
			server.Start()
			defer server.Close()

			network := &http.Transport{ForceAttemptHTTP2: false}
			defer network.CloseIdleConnections()
			transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
				Transport: network,
				Signer:    NewSigner(testTrailerSigningProfile(t, now, key)), Label: "trail",
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
			})
			if err != nil {
				t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
			}
			body := &observedBody{reader: strings.NewReader(test.content)}
			request, err := http.NewRequest(test.method, server.URL+"/upload", body)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			response, requestErr := (&http.Client{Transport: transport}).Do(request)
			if test.reject {
				if response != nil || !errors.Is(requestErr, ErrInvalidBodyIntegration) || !body.closed {
					t.Fatalf("Do() = %#v, %v, body closed=%t", response, requestErr, body.closed)
				}
				select {
				case got := <-received:
					t.Fatalf("rejected request reached wire: %#v", got)
				default:
				}
				return
			}
			if requestErr != nil {
				t.Fatalf("Do() error = %v", requestErr)
			}
			_ = response.Body.Close()
			got := <-received
			if got.body != test.content || got.contentLength != -1 || !slices.Equal(got.encoding, []string{"chunked"}) {
				t.Fatalf("received body=%q length=%d encoding=%v", got.body, got.contentLength, got.encoding)
			}
			if got.trailer.Get("Content-Digest") == "" || got.trailer.Get("Signature-Input") == "" || got.trailer.Get("Signature") == "" {
				t.Fatalf("received trailers = %#v", got.trailer)
			}
		})
	}
}

func TestTrailerSigningRoundTripperRejectsResponseBeforeBodyFinalization(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	requestBody := &observedBody{reader: strings.NewReader("payload")}
	responseBody := &observedBody{reader: strings.NewReader("untrusted response")}
	var streamedBody io.ReadCloser
	transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			streamedBody = request.Body
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: responseBody}, nil
		}),
		Signer: NewSigner(testTrailerSigningProfile(t, now, key)), Label: "trail",
		Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
	})
	if err != nil {
		t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
	}
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/upload", requestBody)
	response, err := transport.RoundTrip(request)
	if response != nil || !errors.Is(err, ErrBodyRead) {
		t.Fatalf("RoundTrip() = %#v, %v, want nil ErrBodyRead", response, err)
	}
	if requestBody.reads != 0 || !requestBody.closed || !responseBody.closed {
		t.Fatalf("request reads=%d closed=%t, response closed=%t", requestBody.reads, requestBody.closed, responseBody.closed)
	}
	if streamedBody == nil {
		t.Fatal("wrapped transport did not receive streaming body")
	}
	if closeErr := streamedBody.Close(); closeErr != nil {
		t.Fatalf("asynchronous second Close() error = %v", closeErr)
	}
}

func TestTrailerSigningRoundTripperPreservesAndAuthenticatesEOFPopulatedApplicationTrailersOnWire(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	verifier := NewVerifier(hardeningStreamingApplicationTrailerVerificationProfile(t, now, key))
	for _, protocol := range []struct {
		name string
		h2   bool
	}{
		{name: "HTTP/1.1"},
		{name: "HTTP/2", h2: true},
	} {
		protocol := protocol
		t.Run(protocol.name, func(t *testing.T) {
			type observation struct {
				protoMajor int
				trailer    string
				err        error
			}
			observed := make(chan observation, 1)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, verifyErr := io.ReadAll(request.Body)
				if verifyErr == nil {
					var inputs SignatureInputs
					inputs, verifyErr = ParseSignatureInputs(request.Trailer.Values("Signature-Input"))
					if verifyErr == nil {
						var signatures Signatures
						signatures, verifyErr = ParseSignatures(request.Trailer.Values("Signature"))
						if verifyErr == nil {
							_, verifyErr = verifier.Verify(request.Context(), MessageContext{Request: request}, "trail", inputs, signatures)
						}
					}
				}
				observed <- observation{protoMajor: request.ProtoMajor, trailer: request.Trailer.Get("X-Final"), err: verifyErr}
				writer.WriteHeader(http.StatusNoContent)
			}))
			server.EnableHTTP2 = protocol.h2
			if protocol.h2 {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()

			client := server.Client()
			if !protocol.h2 {
				network := &http.Transport{ForceAttemptHTTP2: false}
				defer network.CloseIdleConnections()
				client.Transport = network
			}
			transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
				Transport: client.Transport,
				Signer:    NewSigner(hardeningStreamingApplicationTrailerSigningProfile(t, now, key)), Label: "trail",
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
			})
			if err != nil {
				t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
			}
			client.Transport = transport
			request, _ := http.NewRequest(http.MethodPost, server.URL+"/upload", nil)
			request.Trailer = http.Header{"X-Final": nil}
			request.Body = &trailerPopulatingBody{
				reader: strings.NewReader("payload"), trailer: request.Trailer,
				values: http.Header{"X-Final": []string{"complete"}},
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			_ = response.Body.Close()
			got := <-observed
			wantProto := 1
			if protocol.h2 {
				wantProto = 2
			}
			if got.err != nil || got.protoMajor != wantProto || got.trailer != "complete" {
				t.Fatalf("observation = %#v, want protocol=%d authenticated trailer", got, wantProto)
			}
		})
	}
}

func TestTrailerSigningRoundTripperRejectsApplicationTrailerNamesAddedAtEOF(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			_, readErr := io.ReadAll(request.Body)
			_ = request.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
		}),
		Signer: NewSigner(testTrailerSigningProfile(t, now, key)), Label: "trail",
		Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
	})
	if err != nil {
		t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
	}
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/upload", nil)
	request.Trailer = make(http.Header)
	request.Body = &trailerPopulatingBody{
		reader: strings.NewReader("payload"), trailer: request.Trailer,
		values: http.Header{"X-Undeclared": []string{"late"}},
	}
	response, err := transport.RoundTrip(request)
	if response != nil || !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("RoundTrip() = %#v, %v, want nil ErrInvalidBodyIntegration", response, err)
	}
}

func TestTrailerSigningRoundTripperRejectsInvalidApplicationTrailerMutations(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name    string
		initial http.Header
		values  http.Header
		want    error
	}{
		{
			name: "case alias at EOF", initial: http.Header{"X-Final": nil},
			values: http.Header{"x-final": []string{"late"}}, want: ErrAmbiguousProtectedField,
		},
		{
			name: "protected value at EOF", initial: http.Header{"Signature": nil},
			values: http.Header{"Signature": []string{"trail=:YmFk:"}}, want: ErrExistingSignatures,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
				Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
					_, readErr := io.ReadAll(request.Body)
					_ = request.Body.Close()
					return nil, readErr
				}),
				Signer: NewSigner(testTrailerSigningProfile(t, now, key)), Label: "trail",
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
			})
			if err != nil {
				t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
			}
			request, _ := http.NewRequest(http.MethodPost, "https://example.com/upload", nil)
			request.Trailer = test.initial.Clone()
			request.Body = &trailerPopulatingBody{
				reader: strings.NewReader("payload"), trailer: request.Trailer, values: test.values,
			}
			response, err := transport.RoundTrip(request)
			if response != nil || !errors.Is(err, test.want) {
				t.Fatalf("RoundTrip() = %#v, %v, want nil %v", response, err, test.want)
			}
		})
	}
}

func TestTrailerSigningRoundTripperRejectsInvalidInitiallyDeclaredApplicationTrailer(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	delegated := false
	transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			delegated = true
			return nil, nil
		}),
		Signer: NewSigner(testTrailerSigningProfile(t, now, key)), Label: "trail",
		Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
	})
	if err != nil {
		t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
	}
	body := &observedBody{reader: strings.NewReader("payload")}
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/upload", body)
	request.Trailer = http.Header{"Content-Length": []string{"7"}}
	response, err := transport.RoundTrip(request)
	if response != nil || !errors.Is(err, ErrInvalidBodyIntegration) || delegated || !body.closed {
		t.Fatalf("RoundTrip() = %#v, %v, delegated=%t body closed=%t", response, err, delegated, body.closed)
	}
}

func TestTrailerSigningRoundTripperRejectsNilResponseAfterFinalization(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			if _, readErr := io.ReadAll(request.Body); readErr != nil {
				return nil, readErr
			}
			_ = request.Body.Close()
			return nil, nil
		}),
		Signer: NewSigner(testTrailerSigningProfile(t, now, key)), Label: "trail",
		Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
	})
	if err != nil {
		t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
	}
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/upload", &observedBody{reader: strings.NewReader("payload")})
	if response, err := transport.RoundTrip(request); response != nil || !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("RoundTrip() = %#v, %v, want nil ErrInvalidBodyIntegration", response, err)
	}
}

func TestTrailerSigningRoundTripperRejectsBodyClosedBeforeEOF(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	responseBody := &observedBody{reader: strings.NewReader("untrusted response")}
	transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			_ = request.Body.Close()
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: responseBody}, nil
		}),
		Signer: NewSigner(testTrailerSigningProfile(t, now, key)), Label: "trail",
		Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
	})
	if err != nil {
		t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
	}
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/upload", &observedBody{reader: strings.NewReader("payload")})
	if response, err := transport.RoundTrip(request); response != nil || !errors.Is(err, ErrBodyRead) || !responseBody.closed {
		t.Fatalf("RoundTrip() = %#v, %v, response closed=%t", response, err, responseBody.closed)
	}
}

func TestTrailerSigningRoundTripperRejectsProtocolDependentCoveredFields(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, field := range []string{"connection", "content-length", "keep-alive", "proxy-connection", "te", "trailer", "transfer-encoding", "upgrade"} {
		field := field
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			profile, err := NewSigningProfile(SigningProfileConfig{
				AllowedAlgorithms: []Algorithm{HMACSHA256},
				CoveredComponents: []ComponentIdentifier{
					{Name: "@method"},
					{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
					{Name: field},
				},
				Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
				Nonce: ParameterForbidden, Tag: ParameterForbidden,
				Lifetime: time.Minute, ResolveTimeout: time.Second, Now: func() time.Time { return now },
				Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
					return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
				}),
			})
			if err != nil {
				t.Fatalf("NewSigningProfile() error = %v", err)
			}
			if _, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
				Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil }),
				Signer:    NewSigner(profile), Label: "trail", Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
			}); !errors.Is(err, ErrInvalidBodyIntegration) {
				t.Fatalf("NewTrailerSigningRoundTripper() error = %v, want ErrInvalidBodyIntegration", err)
			}
		})
	}
}

func TestTrailerResponseSigningMiddlewareRejectsProtocolDependentCoveredFields(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, field := range []string{"connection", "content-length", "keep-alive", "proxy-connection", "te", "trailer", "transfer-encoding", "upgrade"} {
		field := field
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			profile, err := NewSigningProfile(SigningProfileConfig{
				AllowedAlgorithms: []Algorithm{HMACSHA256},
				CoveredComponents: []ComponentIdentifier{
					{Name: "@status"},
					{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
					{Name: field},
				},
				Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
				Nonce: ParameterForbidden, Tag: ParameterForbidden,
				Lifetime: time.Minute, ResolveTimeout: time.Second, Now: func() time.Time { return now },
				Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
					return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
				}),
			})
			if err != nil {
				t.Fatalf("NewSigningProfile() error = %v", err)
			}
			if _, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
				Signer: NewSigner(profile), Label: "trail", Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
				ReportError: func(*http.Request, error) {},
			}); !errors.Is(err, ErrInvalidBodyIntegration) {
				t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v, want ErrInvalidBodyIntegration", err)
			}
		})
	}
}

func TestTrailerSigningRoundTripperUsesProtocolIndependentHTTP2Trailers(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name    string
		method  string
		content string
	}{
		{name: "POST body", method: http.MethodPost, content: "payload"},
		{name: "GET empty", method: http.MethodGet},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			type observation struct {
				protoMajor int
				body       string
				encoding   []string
				trailer    http.Header
				err        error
			}
			observed := make(chan observation, 1)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				content, readErr := io.ReadAll(request.Body)
				observed <- observation{
					protoMajor: request.ProtoMajor, body: string(content),
					encoding: append([]string(nil), request.TransferEncoding...), trailer: request.Trailer.Clone(), err: readErr,
				}
				writer.WriteHeader(http.StatusNoContent)
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()

			client := server.Client()
			transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
				Transport: client.Transport,
				Signer:    NewSigner(testTrailerSigningProfile(t, now, key)), Label: "trail",
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
			})
			if err != nil {
				t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
			}
			client.Transport = transport
			request, err := http.NewRequest(test.method, server.URL+"/upload", &observedBody{reader: strings.NewReader(test.content)})
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			_ = response.Body.Close()
			got := <-observed
			if got.err != nil || got.protoMajor != 2 || got.body != test.content || len(got.encoding) != 0 {
				t.Fatalf("observation = %#v", got)
			}
			if got.trailer.Get("Content-Digest") == "" || got.trailer.Get("Signature-Input") == "" || got.trailer.Get("Signature") == "" {
				t.Fatalf("received trailers = %#v", got.trailer)
			}
		})
	}
}

func TestTrailerSigningRoundTripperSupportsStandard307And308Replay(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			now := time.Unix(1_700_000_000, 0)
			key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
			verifier := NewVerifier(hardeningTrailerPathVerificationProfile(t, now, key))
			type receivedAttempt struct {
				path string
				body string
				err  error
			}
			attempts := make(chan receivedAttempt, 2)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				content, readErr := io.ReadAll(request.Body)
				if readErr != nil {
					attempts <- receivedAttempt{path: request.URL.Path, err: readErr}
					http.Error(writer, "read failed", http.StatusBadRequest)
					return
				}
				inputs, inputErr := ParseSignatureInputs(request.Trailer.Values("Signature-Input"))
				signatures, signatureErr := ParseSignatures(request.Trailer.Values("Signature"))
				verifyErr := inputErr
				if verifyErr == nil {
					verifyErr = signatureErr
				}
				if verifyErr == nil {
					_, verifyErr = verifier.Verify(request.Context(), MessageContext{Request: request}, "trail", inputs, signatures)
				}
				attempts <- receivedAttempt{path: request.URL.Path, body: string(content), err: verifyErr}
				if request.URL.Path == "/start" {
					writer.Header().Set("Location", "/final")
					writer.WriteHeader(status)
					return
				}
				writer.WriteHeader(http.StatusNoContent)
			}))
			server.EnableHTTP2 = false
			server.Start()
			defer server.Close()

			network := &http.Transport{ForceAttemptHTTP2: false}
			defer network.CloseIdleConnections()
			transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
				Transport: network,
				Signer:    NewSigner(hardeningTrailerPathSigningProfile(t, now, key)), Label: "trail",
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
			})
			if err != nil {
				t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
			}
			request, _ := http.NewRequest(http.MethodPost, server.URL+"/start", strings.NewReader("payload"))
			if request.GetBody == nil {
				t.Fatal("test request is not replayable")
			}
			response, err := (&http.Client{Transport: transport}).Do(request)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusNoContent || response.Request.URL.Path != "/final" {
				t.Fatalf("response status=%d request=%s", response.StatusCode, response.Request.URL.Path)
			}
			for index, wantPath := range []string{"/start", "/final"} {
				got := <-attempts
				if got.path != wantPath || got.body != "payload" || got.err != nil {
					t.Fatalf("attempt %d = path %q body %q error %v", index, got.path, got.body, got.err)
				}
			}
		})
	}
}

func TestBodySigningAdaptersRemoveCaseAliasedFramingFields(t *testing.T) {
	t.Parallel()

	assertFramingRemoved := func(t *testing.T, request *http.Request) {
		t.Helper()
		for name := range request.Header {
			if strings.EqualFold(name, "Content-Length") || strings.EqualFold(name, "Transfer-Encoding") || strings.EqualFold(name, "Trailer") {
				t.Fatalf("framing header %q survived in %#v", name, request.Header)
			}
		}
	}

	t.Run("buffered", func(t *testing.T) {
		var sent *http.Request
		transport, err := NewBufferedContentDigestRoundTripper(BufferedContentDigestRoundTripperConfig{
			Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				sent = request
				return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
			}),
			Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		})
		if err != nil {
			t.Fatalf("NewBufferedContentDigestRoundTripper() error = %v", err)
		}
		request := httptest.NewRequest(http.MethodPost, "https://example.com/data", strings.NewReader("payload"))
		request.Header["content-length"] = []string{"999"}
		request.Header["transfer-encoding"] = []string{"identity"}
		request.Header["trailer"] = []string{"X-Final"}
		if _, err := transport.RoundTrip(request); err != nil {
			t.Fatalf("RoundTrip() error = %v", err)
		}
		if sent == nil || sent.ContentLength != 7 || len(sent.TransferEncoding) != 0 {
			t.Fatalf("sent framing = %#v", sent)
		}
		assertFramingRemoved(t, sent)
	})

	t.Run("trailer", func(t *testing.T) {
		now := time.Unix(1_700_000_000, 0)
		key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
		var sent *http.Request
		transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
			Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				sent = request
				_, readErr := io.ReadAll(request.Body)
				return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, readErr
			}),
			Signer: NewSigner(testTrailerSigningProfile(t, now, key)), Label: "trail",
			Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
			Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		})
		if err != nil {
			t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
		}
		request := httptest.NewRequest(http.MethodPost, "https://example.com/data", strings.NewReader("payload"))
		request.Header["content-length"] = []string{"999"}
		request.Header["transfer-encoding"] = []string{"identity"}
		request.Header["trailer"] = []string{"X-Final"}
		if _, err := transport.RoundTrip(request); err != nil {
			t.Fatalf("RoundTrip() error = %v", err)
		}
		if sent == nil || sent.ContentLength != -1 || !slices.Equal(sent.TransferEncoding, []string{"chunked"}) {
			t.Fatalf("sent framing = %#v", sent)
		}
		assertFramingRemoved(t, sent)
	})
}

func TestBufferedContentDigestVerificationPreservesEOFPopulatedTrailersOnWire(t *testing.T) {
	t.Parallel()

	digests, err := ComputeDigests([]DigestAlgorithm{SHA256}, []byte("payload"))
	if err != nil {
		t.Fatalf("ComputeDigests() error = %v", err)
	}
	type observation struct {
		body     string
		trailer  string
		encoding []string
		err      error
	}
	observed := make(chan observation, 1)
	middleware, err := NewBufferedContentDigestVerificationMiddleware(BufferedContentDigestVerificationMiddlewareConfig{
		RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		MapError: func(writer http.ResponseWriter, _ *http.Request, errorValue error) {
			observed <- observation{err: errorValue}
			writer.WriteHeader(http.StatusBadRequest)
		},
	})
	if err != nil {
		t.Fatalf("NewBufferedContentDigestVerificationMiddleware() error = %v", err)
	}
	server := httptest.NewServer(middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		content, readErr := io.ReadAll(request.Body)
		observed <- observation{
			body: string(content), trailer: request.Trailer.Get("X-Final"),
			encoding: append([]string(nil), request.TransferEncoding...), err: readErr,
		}
		writer.WriteHeader(http.StatusNoContent)
	})))
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/upload", strings.NewReader("payload"))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Digest", digests.String())
	request.ContentLength = -1
	request.Trailer = http.Header{"X-Final": []string{"complete"}}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = response.Body.Close()
	got := <-observed
	if response.StatusCode != http.StatusNoContent || got.err != nil || got.body != "payload" || got.trailer != "complete" ||
		!slices.Equal(got.encoding, []string{"chunked"}) {
		t.Fatalf("response=%d observation=%#v", response.StatusCode, got)
	}
}

func TestBufferedContentDigestRoundTripperPreservesAndAuthenticatesEOFPopulatedTrailersOnWire(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	type observation struct {
		body          string
		trailer       string
		contentLength int64
		encoding      []string
		err           error
	}
	observed := make(chan observation, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		content, verifyErr := io.ReadAll(request.Body)
		if verifyErr == nil {
			var inputs SignatureInputs
			inputs, verifyErr = ParseSignatureInputs(request.Header.Values("Signature-Input"))
			if verifyErr == nil {
				var signatures Signatures
				signatures, verifyErr = ParseSignatures(request.Header.Values("Signature"))
				if verifyErr == nil {
					_, verifyErr = NewVerifier(hardeningBufferedRequestTrailerVerificationProfile(t, now, key)).Verify(
						request.Context(), MessageContext{Request: request}, "buffered", inputs, signatures,
					)
				}
			}
		}
		observed <- observation{
			body: string(content), trailer: request.Trailer.Get("X-Final"), contentLength: request.ContentLength,
			encoding: append([]string(nil), request.TransferEncoding...), err: verifyErr,
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	signing, err := NewSigningRoundTripper(SigningRoundTripperConfig{
		Transport: http.DefaultTransport, Signer: NewSigner(hardeningBufferedRequestTrailerSigningProfile(t, now, key)),
		Label: "buffered", Existing: ExistingSignaturesReject,
		Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
	})
	if err != nil {
		t.Fatalf("NewSigningRoundTripper() error = %v", err)
	}
	transport, err := NewBufferedContentDigestRoundTripper(BufferedContentDigestRoundTripperConfig{
		Transport: signing, Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
	})
	if err != nil {
		t.Fatalf("NewBufferedContentDigestRoundTripper() error = %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/upload", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.ContentLength = -1
	request.Trailer = http.Header{"X-Final": nil}
	request.Body = &trailerPopulatingBody{
		reader: strings.NewReader("payload"), trailer: request.Trailer,
		values: http.Header{"X-Final": []string{"complete"}},
	}
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = response.Body.Close()
	got := <-observed
	if response.StatusCode != http.StatusNoContent || got.err != nil || got.body != "payload" || got.trailer != "complete" ||
		got.contentLength != -1 || !slices.Equal(got.encoding, []string{"chunked"}) {
		t.Fatalf("response=%d observation=%#v", response.StatusCode, got)
	}
}

func TestBufferedRequestBodyAdaptersRejectEOFPopulatedProtectedTrailerAliases(t *testing.T) {
	t.Parallel()

	digests, err := ComputeDigests([]DigestAlgorithm{SHA256}, []byte("payload"))
	if err != nil {
		t.Fatalf("ComputeDigests() error = %v", err)
	}
	newRequest := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, "https://example.com/upload", nil)
		request.Header.Set("Content-Digest", digests.String())
		request.Trailer = http.Header{"Signature": nil}
		request.Body = &trailerPopulatingBody{
			reader: strings.NewReader("payload"), trailer: request.Trailer,
			values: http.Header{"signature": []string{"sig=:YmFk:"}},
		}
		return request
	}

	t.Run("round tripper", func(t *testing.T) {
		delegated := false
		transport, err := NewBufferedContentDigestRoundTripper(BufferedContentDigestRoundTripperConfig{
			Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				delegated = true
				return nil, nil
			}),
			Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		})
		if err != nil {
			t.Fatalf("NewBufferedContentDigestRoundTripper() error = %v", err)
		}
		request := newRequest()
		request.Header.Del("Content-Digest")
		response, err := transport.RoundTrip(request)
		if response != nil || !errors.Is(err, ErrAmbiguousProtectedField) || delegated {
			t.Fatalf("RoundTrip() = %#v, %v, delegated=%t", response, err, delegated)
		}
	})

	t.Run("verification middleware", func(t *testing.T) {
		nextCalls := 0
		var mapped error
		middleware, err := NewBufferedContentDigestVerificationMiddleware(BufferedContentDigestVerificationMiddlewareConfig{
			RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
			MapError: func(_ http.ResponseWriter, _ *http.Request, errorValue error) { mapped = errorValue },
		})
		if err != nil {
			t.Fatalf("NewBufferedContentDigestVerificationMiddleware() error = %v", err)
		}
		middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalls++ })).ServeHTTP(
			httptest.NewRecorder(), newRequest(),
		)
		if !errors.Is(mapped, ErrAmbiguousProtectedField) || nextCalls != 0 {
			t.Fatalf("mapped error=%v next calls=%d", mapped, nextCalls)
		}
	})
}

func TestBufferedTrailerVerifyingRoundTripperRejectsTransparentDecompression(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	response := signedTrailerResponse(t, now, key, request, "payload")
	response.Uncompressed = true
	transport, err := NewBufferedTrailerVerifyingRoundTripper(BufferedTrailerVerifyingRoundTripperConfig{
		Transport:          roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:           NewVerifier(testResponseTrailerVerificationProfile(t, now, key)),
		SelectLabel:        func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "trail", nil },
		RequiredAlgorithms: []DigestAlgorithm{SHA256},
		MaxBytes:           32,
	})
	if err != nil {
		t.Fatalf("NewBufferedTrailerVerifyingRoundTripper() error = %v", err)
	}
	if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrInvalidBodyIntegration) {
		t.Fatalf("RoundTrip() = %#v, %v, want ErrInvalidBodyIntegration", got, verifyErr)
	}
}

func TestBufferedTrailerVerifyingRoundTripperRejectsMissingActualSentRequestSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	response := signedTrailerResponse(t, now, key, request, "payload")
	body := &observedBody{reader: strings.NewReader("payload")}
	response.Request = nil
	response.Body = body
	transport, err := NewBufferedTrailerVerifyingRoundTripper(BufferedTrailerVerifyingRoundTripperConfig{
		Transport:          roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:           NewVerifier(testResponseTrailerVerificationProfile(t, now, key)),
		SelectLabel:        func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "trail", nil },
		RequiredAlgorithms: []DigestAlgorithm{SHA256},
		MaxBytes:           32,
	})
	if err != nil {
		t.Fatalf("NewBufferedTrailerVerifyingRoundTripper() error = %v", err)
	}
	if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrInvalidHTTPIntegration) {
		t.Fatalf("RoundTrip() = %#v, %v, want ErrInvalidHTTPIntegration", got, verifyErr)
	}
	if !body.closed || body.reads != 0 {
		t.Fatalf("body closed=%t reads=%d", body.closed, body.reads)
	}
}

func TestBufferedTrailerVerifyingRoundTripperBindsReqToActualSentSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	callerRequest, _ := http.NewRequest(http.MethodGet, "https://example.com/caller", nil)
	actualSent, _ := http.NewRequest(http.MethodGet, "https://example.com/actual", nil)
	digests, _ := ComputeDigests([]DigestAlgorithm{SHA256}, []byte("payload"))
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Trailer:    http.Header{"Content-Digest": []string{digests.String()}},
		Request:    actualSent,
	}
	signed, err := NewSigner(hardeningResponseTrailerBindingSigningProfile(t, now, key)).Sign(
		context.Background(), MessageContext{Response: response, RelatedRequest: actualSent}, "trail", SigningOptions{},
	)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	values := http.Header{
		"Content-Digest":  []string{digests.String()},
		"Signature-Input": []string{signed.SignatureInputField()},
		"Signature":       []string{signed.SignatureField()},
	}
	response.Trailer = http.Header{"Content-Digest": nil, "Signature-Input": nil, "Signature": nil}
	response.Body = &trailerPopulatingBody{reader: strings.NewReader("payload"), trailer: response.Trailer, values: values}
	transport, err := NewBufferedTrailerVerifyingRoundTripper(BufferedTrailerVerifyingRoundTripperConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:  NewVerifier(hardeningResponseTrailerBindingVerificationProfile(t, now, key)),
		SelectLabel: func(selectedRequest *http.Request, _ *http.Response, _ SignatureInputs, _ Signatures) (string, error) {
			if selectedRequest.URL.Path != "/actual" {
				t.Errorf("SelectLabel request path = %q, want /actual", selectedRequest.URL.Path)
			}
			actualSent.URL.Path = "/mutated-after-send"
			return "trail", nil
		},
		RequiredAlgorithms: []DigestAlgorithm{SHA256},
		MaxBytes:           32,
	})
	if err != nil {
		t.Fatalf("NewBufferedTrailerVerifyingRoundTripper() error = %v", err)
	}
	verifiedResponse, err := transport.RoundTrip(callerRequest)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if verifiedResponse.Request == actualSent || verifiedResponse.Request.URL.Path != "/actual" {
		t.Fatalf("verified request = %#v, want immutable /actual snapshot", verifiedResponse.Request)
	}
}

func TestBufferedTrailerVerifyingRoundTripperIsolatesResponseCallbacks(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
	request.TLS = &tls.ConnectionState{OCSPResponse: []byte("request-state")}
	response := signedTrailerResponse(t, now, key, request, "payload")
	response.TLS = &tls.ConnectionState{OCSPResponse: []byte("response-state")}
	mutateSnapshot := func(requestSnapshot *http.Request, responseSnapshot *http.Response) {
		if responseSnapshot.Body != nil {
			t.Errorf("callback response Body = %#v, want nil isolated snapshot", responseSnapshot.Body)
		}
		if requestSnapshot.TLS != nil {
			requestSnapshot.TLS.OCSPResponse[0] = 'X'
		}
		if responseSnapshot.TLS != nil {
			responseSnapshot.TLS.OCSPResponse[0] = 'X'
		}
		responseSnapshot.Trailer.Set("Content-Digest", "sha-256=:YmFk:")
		responseSnapshot.Trailer.Set("Signature", "trail=:YmFk:")
	}
	transport, err := NewBufferedTrailerVerifyingRoundTripper(BufferedTrailerVerifyingRoundTripperConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
		Verifier:  NewVerifier(testResponseTrailerVerificationProfile(t, now, key)),
		SelectLabel: func(requestSnapshot *http.Request, responseSnapshot *http.Response, _ SignatureInputs, _ Signatures) (string, error) {
			mutateSnapshot(requestSnapshot, responseSnapshot)
			return "trail", nil
		},
		RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		ExternalContext: func(_ context.Context, requestSnapshot *http.Request, responseSnapshot *http.Response) (*ExternalRequestContext, error) {
			mutateSnapshot(requestSnapshot, responseSnapshot)
			return nil, nil
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
		t.Fatalf("verified body = %q, %v", content, err)
	}
	if string(request.TLS.OCSPResponse) != "request-state" || string(response.TLS.OCSPResponse) != "response-state" {
		t.Fatalf("callback mutated shared TLS state: request=%q response=%q", request.TLS.OCSPResponse, response.TLS.OCSPResponse)
	}
}

func TestTrailerResponseWriterRejectsHijackAndFullDuplex(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		call func(*http.ResponseController) error
	}{
		{name: "hijack", call: func(controller *http.ResponseController) error {
			_, _, err := controller.Hijack()
			return err
		}},
		{name: "full duplex", call: func(controller *http.ResponseController) error {
			return controller.EnableFullDuplex()
		}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			underlying := &optionalProtocolWriter{header: make(http.Header)}
			stream := &trailerResponseWriter{
				ResponseWriter: underlying,
				request:        httptest.NewRequest(http.MethodGet, "https://example.com/", nil),
				maxBytes:       1,
			}
			if err := test.call(http.NewResponseController(stream)); !errors.Is(err, ErrInvalidBodyIntegration) {
				t.Fatalf("optional operation error = %v, want ErrInvalidBodyIntegration", err)
			}
			if !errors.Is(stream.failure, ErrInvalidBodyIntegration) || underlying.optionalCalls != 0 {
				t.Fatalf("failure = %v, delegated calls = %d", stream.failure, underlying.optionalCalls)
			}
		})
	}
}

func TestTrailerResponseSigningRestoresWireTrailerFraming(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	reported := make(chan error, 1)
	middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
		Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
		Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		ReportError: func(_ *http.Request, errorValue error) { reported <- errorValue },
	})
	if err != nil {
		t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
	}
	server := httptest.NewServer(middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Del("Trailer")
		writer.Header().Set("Content-Length", "7")
		_, _ = writer.Write([]byte("payload"))
	})))
	defer server.Close()

	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	content, err := io.ReadAll(response.Body)
	if err != nil || string(content) != "payload" {
		t.Fatalf("response body = %q, %v", content, err)
	}
	if response.ProtoMajor != 1 || !slices.Contains(response.TransferEncoding, "chunked") || response.ContentLength != -1 {
		t.Fatalf("wire framing = proto %d, transfer %v, length %d", response.ProtoMajor, response.TransferEncoding, response.ContentLength)
	}
	for _, name := range []string{"Content-Digest", "Signature-Input", "Signature"} {
		if response.Trailer.Get(name) == "" {
			t.Fatalf("response trailer %q missing from %#v", name, response.Trailer)
		}
	}
	select {
	case reportedErr := <-reported:
		t.Fatalf("ReportError() = %v", reportedErr)
	default:
	}
}

func TestTrailerResponseSigningPreservesCommittedHeadersAndApplicationTrailers(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	reported := make(chan error, 1)
	middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
		Signer: NewSigner(hardeningCommittedTrailerSigningProfile(t, now, key)), Label: "trail",
		Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		ReportError: func(_ *http.Request, errorValue error) { reported <- errorValue },
	})
	if err != nil {
		t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
	}
	server := httptest.NewServer(middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-Signed", "committed")
		writer.Header().Set("Trailer", "X-Final")
		writer.WriteHeader(http.StatusOK)
		writer.Header().Set("X-Signed", "mutable")
		writer.Header().Set("X-Final", "complete")
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
	defer func() { _ = response.Body.Close() }()
	if content, readErr := io.ReadAll(response.Body); readErr != nil || string(content) != "payload" {
		t.Fatalf("response body = %q, %v", content, readErr)
	}
	if response.Header.Get("X-Signed") != "committed" || response.Trailer.Get("X-Final") != "complete" {
		t.Fatalf("response header = %#v, trailer = %#v", response.Header, response.Trailer)
	}
	inputs, err := ParseSignatureInputs(response.Trailer.Values("Signature-Input"))
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	signatures, err := ParseSignatures(response.Trailer.Values("Signature"))
	if err != nil {
		t.Fatalf("ParseSignatures() error = %v", err)
	}
	verifier := NewVerifier(hardeningCommittedTrailerVerificationProfile(t, now, key))
	if _, err := verifier.Verify(
		context.Background(), MessageContext{
			Response: response, RelatedRequest: request, ResponseTransport: ResponseTransportReceived,
		}, "trail", inputs, signatures,
	); err != nil {
		t.Fatalf("Verify(received response) error = %v", err)
	}
	select {
	case reportedErr := <-reported:
		t.Fatalf("ReportError() = %v", reportedErr)
	default:
	}
}

func TestTrailerResponseSigningAuthenticatesLateTrailerPrefixValues(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	reported := make(chan error, 1)
	middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
		Signer: NewSigner(hardeningLateTrailerSigningProfile(t, now, key)), Label: "trail",
		Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		ReportError: func(_ *http.Request, errorValue error) { reported <- errorValue },
	})
	if err != nil {
		t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
	}
	server := httptest.NewServer(middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		writer.Header()[http.TrailerPrefix+"X-Late"] = []string{"complete"}
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
	defer func() { _ = response.Body.Close() }()
	content, err := io.ReadAll(response.Body)
	if err != nil || string(content) != "payload" || response.Trailer.Get("X-Late") != "complete" {
		t.Fatalf("response body = %q, trailer = %#v, error = %v", content, response.Trailer, err)
	}
	digests, err := ParseDigestFields(response.Trailer.Values("Content-Digest"))
	if err != nil || digests.Verify(content, []DigestAlgorithm{SHA256}) != nil {
		t.Fatalf("ParseDigestFields() error = %v, trailer = %#v", err, response.Trailer)
	}
	inputs, err := ParseSignatureInputs(response.Trailer.Values("Signature-Input"))
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	signatures, err := ParseSignatures(response.Trailer.Values("Signature"))
	if err != nil {
		t.Fatalf("ParseSignatures() error = %v", err)
	}
	if _, err := NewVerifier(hardeningLateTrailerVerificationProfile(t, now, key)).Verify(
		context.Background(), MessageContext{Response: response, RelatedRequest: request}, "trail", inputs, signatures,
	); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	select {
	case reportedErr := <-reported:
		t.Fatalf("ReportError() = %v", reportedErr)
	default:
	}
}

func TestTrailerResponseSigningRejectsUnsafeLateTrailerPrefixValues(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name    string
		prepare func(http.ResponseWriter)
		want    error
	}{
		{name: "malformed", prepare: func(writer http.ResponseWriter) {
			writer.Header()[http.TrailerPrefix+"bad name"] = []string{"value"}
		}, want: ErrInvalidBodyIntegration},
		{name: "forbidden", prepare: func(writer http.ResponseWriter) {
			writer.Header()[http.TrailerPrefix+"Content-Length"] = []string{"7"}
		}, want: ErrInvalidBodyIntegration},
		{name: "declared collision", prepare: func(writer http.ResponseWriter) {
			writer.Header().Set("Trailer", "X-Late")
			writer.WriteHeader(http.StatusOK)
			writer.Header()[http.TrailerPrefix+"x-late"] = []string{"late"}
		}, want: ErrAmbiguousProtectedField},
		{name: "case collision", prepare: func(writer http.ResponseWriter) {
			writer.WriteHeader(http.StatusOK)
			writer.Header()[http.TrailerPrefix+"X-Late"] = []string{"first"}
			writer.Header()[http.TrailerPrefix+"x-late"] = []string{"second"}
		}, want: ErrAmbiguousProtectedField},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var reported error
			middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
				Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
				ReportError: func(_ *http.Request, errorValue error) { reported = errorValue },
			})
			if err != nil {
				t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
			}
			middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				test.prepare(writer)
			})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "https://example.com/data", nil))
			if !errors.Is(reported, test.want) {
				t.Fatalf("ReportError() = %v, want %v", reported, test.want)
			}
		})
	}
}

func TestTrailerResponseSigningRejectsBodylessFinalResponsesBeforeCommit(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name   string
		method string
		status int
	}{
		{name: "HEAD", method: http.MethodHead, status: http.StatusOK},
		{name: "204", method: http.MethodGet, status: http.StatusNoContent},
		{name: "205", method: http.MethodGet, status: http.StatusResetContent},
		{name: "304", method: http.MethodGet, status: http.StatusNotModified},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			reported := make(chan error, 1)
			handlerCalls := 0
			middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
				Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
				ReportError: func(_ *http.Request, errorValue error) { reported <- errorValue },
			})
			if err != nil {
				t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
			}
			signedHandler := middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				handlerCalls++
				writer.Header().Set("X-Handler", "must-not-escape")
				writer.WriteHeader(test.status)
			}))
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("X-Outer", "must-not-escape")
				signedHandler.ServeHTTP(writer, request)
			}))
			defer server.Close()
			request, err := http.NewRequest(test.method, server.URL+"/data", nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			content, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil || response.StatusCode != http.StatusNotImplemented || len(content) != 0 ||
				response.Header.Get("X-Outer") != "" || response.Header.Get("X-Handler") != "" || response.Header.Get("Signature") != "" {
				t.Fatalf("status=%d body=%q header=%#v error=%v", response.StatusCode, content, response.Header, readErr)
			}
			if test.method == http.MethodHead && handlerCalls != 0 {
				t.Fatalf("HEAD handler calls = %d, want 0", handlerCalls)
			}
			select {
			case reportedErr := <-reported:
				if !errors.Is(reportedErr, ErrInvalidBodyIntegration) {
					t.Fatalf("ReportError() = %v", reportedErr)
				}
			default:
				t.Fatal("rejection was not reported")
			}
		})
	}
}

func TestTrailerResponseSigningRejectsHTTP10BeforeHandler(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	reported := make(chan error, 1)
	optionsCalls := 0
	handlerCalls := 0
	middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
		Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
		Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		Options: func(context.Context, *http.Request) (SigningOptions, error) {
			optionsCalls++
			return SigningOptions{}, nil
		},
		ReportError: func(_ *http.Request, errorValue error) { reported <- errorValue },
	})
	if err != nil {
		t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
	}
	signedHandler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { handlerCalls++ }))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Outer", "must-not-escape")
		signedHandler.ServeHTTP(writer, request)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	connection, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer func() { _ = connection.Close() }()
	if _, err := io.WriteString(connection, "GET /data HTTP/1.0\r\nHost: "+host+"\r\n\r\n"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/data", nil)
	response, err := http.ReadResponse(bufio.NewReader(connection), request)
	if err != nil {
		t.Fatalf("ReadResponse() error = %v", err)
	}
	content, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusNotImplemented || len(content) != 0 || response.Header.Get("X-Outer") != "" ||
		response.Header.Get("Signature") != "" || optionsCalls != 0 || handlerCalls != 0 {
		t.Fatalf("status=%d body=%q header=%#v options=%d handler=%d error=%v", response.StatusCode, content, response.Header, optionsCalls, handlerCalls, readErr)
	}
	select {
	case reportedErr := <-reported:
		if !errors.Is(reportedErr, ErrInvalidBodyIntegration) {
			t.Fatalf("ReportError() = %v", reportedErr)
		}
	default:
		t.Fatal("HTTP/1.0 rejection was not reported")
	}
}

func TestTrailerResponseSigningRejectsPrecommitCallbackFailuresOnWire(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name         string
		optionsFail  bool
		externalFail bool
	}{
		{name: "options", optionsFail: true},
		{name: "external context", externalFail: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reported := make(chan error, 1)
			handlerCalls := 0
			middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
				Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options: func(context.Context, *http.Request) (SigningOptions, error) {
					if test.optionsFail {
						return SigningOptions{}, errors.New("private options failure")
					}
					return SigningOptions{}, nil
				},
				ExternalContext: func(context.Context, *http.Request) (*ExternalRequestContext, error) {
					if test.externalFail {
						return nil, errors.New("private external-context failure")
					}
					return nil, nil
				},
				ReportError: func(_ *http.Request, errorValue error) { reported <- errorValue },
			})
			if err != nil {
				t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
			}
			signedHandler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { handlerCalls++ }))
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("X-Outer", "must-not-escape")
				signedHandler.ServeHTTP(writer, request)
			}))
			defer server.Close()
			response, err := server.Client().Get(server.URL + "/data")
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			content, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil || response.StatusCode != http.StatusInternalServerError || len(content) != 0 ||
				response.Header.Get("X-Outer") != "" || response.Header.Get("Signature") != "" || handlerCalls != 0 {
				t.Fatalf("status=%d body=%q header=%#v handler=%d error=%v", response.StatusCode, content, response.Header, handlerCalls, readErr)
			}
			select {
			case reportedErr := <-reported:
				if !errors.Is(reportedErr, ErrHTTPIntegrationSigning) {
					t.Fatalf("ReportError() = %v", reportedErr)
				}
			default:
				t.Fatal("callback rejection was not reported")
			}
		})
	}
}

func TestTrailerResponseSigningRejectsSuccessfulConnectOnWire(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "implicit", handler: func(http.ResponseWriter, *http.Request) {}},
		{name: "explicit", handler: func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("X-Stale", "must-not-escape")
			writer.Header().Set("Trailer", "X-Final")
			writer.WriteHeader(http.StatusOK)
			writer.Header()[http.TrailerPrefix+"X-Late"] = []string{"must-not-escape"}
		}},
		{name: "upper bound", handler: func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(299)
		}},
		{name: "write", handler: func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte("tunnel"))
		}},
		{name: "flush", handler: func(writer http.ResponseWriter, _ *http.Request) {
			_ = http.NewResponseController(writer).Flush()
		}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reported := make(chan error, 1)
			middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
				Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
				ReportError: func(_ *http.Request, errorValue error) { reported <- errorValue },
			})
			if err != nil {
				t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
			}
			server := httptest.NewServer(middleware(test.handler))
			defer server.Close()
			request, err := http.NewRequest(http.MethodConnect, server.URL, nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			content, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil || response.StatusCode != http.StatusNotImplemented || len(content) != 0 {
				t.Fatalf("response status = %d, body = %q, error = %v", response.StatusCode, content, readErr)
			}
			for _, name := range []string{"Content-Digest", "Signature-Input", "Signature"} {
				if response.Header.Get(name) != "" || response.Trailer.Get(name) != "" {
					t.Fatalf("rejected CONNECT emitted %q in header %#v or trailer %#v", name, response.Header, response.Trailer)
				}
			}
			if response.Header.Get("X-Stale") != "" || response.Trailer.Get("X-Late") != "" {
				t.Fatalf("rejected CONNECT retained handler fields in header %#v or trailer %#v", response.Header, response.Trailer)
			}
			select {
			case reportedErr := <-reported:
				if !errors.Is(reportedErr, ErrInvalidBodyIntegration) {
					t.Fatalf("ReportError() = %v", reportedErr)
				}
			default:
				t.Fatal("successful CONNECT rejection was not reported")
			}
		})
	}
}

func TestTrailerResponseSigningAllowsNonSuccessfulConnect(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, status := range []int{http.StatusMultipleChoices, http.StatusProxyAuthRequired} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			reported := make(chan error, 1)
			middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
				Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
				ReportError: func(_ *http.Request, errorValue error) { reported <- errorValue },
			})
			if err != nil {
				t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
			}
			server := httptest.NewServer(middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(status)
				_, _ = writer.Write([]byte("denied"))
			})))
			defer server.Close()
			request, err := http.NewRequest(http.MethodConnect, server.URL, nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			content, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil || response.StatusCode != status || string(content) != "denied" {
				t.Fatalf("response status = %d, body = %q, error = %v", response.StatusCode, content, readErr)
			}
			for _, name := range []string{"Content-Digest", "Signature-Input", "Signature"} {
				if response.Trailer.Get(name) == "" {
					t.Fatalf("ordinary CONNECT response trailer %q missing from %#v", name, response.Trailer)
				}
			}
			select {
			case reportedErr := <-reported:
				t.Fatalf("ReportError() = %v", reportedErr)
			default:
			}
		})
	}
}

func TestTrailerResponseWriterFlushCommitsThroughValidation(t *testing.T) {
	t.Parallel()

	t.Run("status", func(t *testing.T) {
		underlying := &flushProtocolWriter{header: make(http.Header)}
		stream := &trailerResponseWriter{
			ResponseWriter: underlying,
			request:        httptest.NewRequest(http.MethodGet, "https://example.com/", nil),
			maxBytes:       1,
		}
		if err := http.NewResponseController(stream).Flush(); err != nil {
			t.Fatalf("Flush() error = %v", err)
		}
		stream.WriteHeader(http.StatusCreated)
		if stream.status != http.StatusOK || underlying.status != http.StatusOK || underlying.flushes != 1 {
			t.Fatalf("status = %d, underlying status = %d, flushes = %d", stream.status, underlying.status, underlying.flushes)
		}
	})

	t.Run("protected collision", func(t *testing.T) {
		underlying := &flushProtocolWriter{header: http.Header{
			"Signature": []string{"sig=:AA==:"},
			"signature": []string{"sig=:AQ==:"},
		}}
		stream := &trailerResponseWriter{
			ResponseWriter: underlying,
			request:        httptest.NewRequest(http.MethodGet, "https://example.com/", nil),
			maxBytes:       1,
		}
		if err := http.NewResponseController(stream).Flush(); !errors.Is(err, ErrAmbiguousProtectedField) {
			t.Fatalf("Flush() error = %v, want ErrAmbiguousProtectedField", err)
		}
		if underlying.flushes != 0 || !errors.Is(stream.failure, ErrAmbiguousProtectedField) {
			t.Fatalf("delegated flushes = %d, failure = %v", underlying.flushes, stream.failure)
		}
	})

	t.Run("existing failure", func(t *testing.T) {
		underlying := &flushProtocolWriter{header: make(http.Header)}
		stream := &trailerResponseWriter{
			ResponseWriter: underlying,
			request:        httptest.NewRequest(http.MethodGet, "https://example.com/", nil),
			maxBytes:       1,
			failure:        ErrBodyRead,
		}
		if err := http.NewResponseController(stream).Flush(); !errors.Is(err, ErrBodyRead) {
			t.Fatalf("Flush() error = %v, want ErrBodyRead", err)
		}
		if underlying.flushes != 0 || underlying.status != 0 {
			t.Fatalf("delegated flushes = %d, status = %d", underlying.flushes, underlying.status)
		}
	})

	t.Run("underlying failure", func(t *testing.T) {
		underlying := &flushProtocolWriter{header: make(http.Header), flushErr: errors.New("private flush detail")}
		stream := &trailerResponseWriter{
			ResponseWriter: underlying,
			request:        httptest.NewRequest(http.MethodGet, "https://example.com/", nil),
			maxBytes:       1,
		}
		if err := http.NewResponseController(stream).Flush(); !errors.Is(err, ErrBodyRead) || strings.Contains(err.Error(), "private") {
			t.Fatalf("Flush() error = %v, want redacted ErrBodyRead", err)
		}
		if !errors.Is(stream.failure, ErrBodyRead) || underlying.flushes != 1 {
			t.Fatalf("failure = %v, delegated flushes = %d", stream.failure, underlying.flushes)
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		underlying := &unsupportedFlushWriter{header: make(http.Header)}
		stream := &trailerResponseWriter{
			ResponseWriter: underlying,
			request:        httptest.NewRequest(http.MethodGet, "https://example.com/", nil),
			maxBytes:       1,
		}
		if err := http.NewResponseController(stream).Flush(); !errors.Is(err, http.ErrNotSupported) {
			t.Fatalf("Flush() error = %v, want ErrNotSupported", err)
		}
		if stream.failure != nil {
			t.Fatalf("unsupported flush latched failure = %v", stream.failure)
		}
	})
}

func TestTrailerResponseWriterRejectsInvalidUnderlyingWriteCounts(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		count int
	}{
		{name: "negative", count: -1},
		{name: "oversized", count: 2},
		{name: "private error", count: 0},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			underlying := &invalidCountResponseWriter{header: make(http.Header), count: test.count}
			digestHash, err := newDigestWriter(SHA256)
			if err != nil {
				t.Fatalf("newDigestWriter() error = %v", err)
			}
			stream := &trailerResponseWriter{
				ResponseWriter: underlying,
				request:        httptest.NewRequest(http.MethodGet, "https://example.com/", nil),
				maxBytes:       8,
				writers:        []digestWriter{{algorithm: SHA256, hash: digestHash}},
			}
			count, panicValue, err := invokeTrailerResponseWrite(stream, []byte("x"))
			if panicValue != nil || count != 0 || !errors.Is(err, ErrBodyRead) || strings.Contains(err.Error(), "private") ||
				!errors.Is(stream.failure, ErrBodyRead) {
				t.Fatalf("Write() = %d, %v, panic = %v, failure = %v", count, err, panicValue, stream.failure)
			}
		})
	}
}

func TestTrailerResponseWriterRejectsInvalidApplicationTrailerDeclarations(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		header http.Header
		want   error
	}{
		{name: "case collision", header: http.Header{
			"Trailer": []string{"X-Final"},
			"trailer": []string{"X-Other"},
		}, want: ErrAmbiguousProtectedField},
		{name: "conditional field", header: http.Header{
			"Trailer": []string{"If-Match"},
		}, want: ErrInvalidBodyIntegration},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			underlying := &flushProtocolWriter{header: test.header.Clone()}
			stream := &trailerResponseWriter{
				ResponseWriter: underlying,
				request:        httptest.NewRequest(http.MethodGet, "https://example.com/", nil),
				maxBytes:       8,
			}
			stream.WriteHeader(http.StatusOK)
			if !errors.Is(stream.failure, test.want) || underlying.status != 0 {
				t.Fatalf("failure = %v, status = %d, want %v and no commit", stream.failure, underlying.status, test.want)
			}
		})
	}
}

func TestTrailerResponseSigningRejectsPostCommitFieldAmbiguity(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{name: "protected value", handler: func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusOK)
			writer.Header().Set("Signature", "trail=:AA==:")
		}, want: ErrExistingSignatures},
		{name: "declared trailer collision", handler: func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Trailer", "X-Final")
			writer.WriteHeader(http.StatusOK)
			writer.Header()["X-Final"] = []string{"first"}
			writer.Header()["x-final"] = []string{"second"}
		}, want: ErrAmbiguousProtectedField},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var reported error
			middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
				Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
				ReportError: func(_ *http.Request, errorValue error) { reported = errorValue },
			})
			if err != nil {
				t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
			}
			middleware(test.handler).ServeHTTP(
				httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "https://example.com/data", nil),
			)
			if !errors.Is(reported, test.want) {
				t.Fatalf("ReportError() = %v, want %v", reported, test.want)
			}
		})
	}
}

func TestTrailerResponseSigningClearsInjectedProtectedTrailersAfterCommitOnWire(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, protocol := range []struct {
		name string
		h2   bool
	}{
		{name: "HTTP/1.1"},
		{name: "HTTP/2", h2: true},
	} {
		protocol := protocol
		for _, attack := range []struct {
			name   string
			inject func(http.Header)
			want   error
		}{
			{
				name: "declared fields",
				inject: func(header http.Header) {
					header.Set("Content-Digest", "sha-256=:YmFk:")
					header.Set("Signature-Input", `trail=("@status")`)
					header.Set("Signature", "trail=:YmFk:")
				},
				want: ErrExistingSignatures,
			},
			{
				name: "late prefix collision",
				inject: func(header http.Header) {
					header.Set(http.TrailerPrefix+"Signature", "trail=:YmFk:")
				},
				want: ErrAmbiguousProtectedField,
			},
		} {
			attack := attack
			t.Run(protocol.name+"/"+attack.name, func(t *testing.T) {
				reported := make(chan error, 1)
				middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
					Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
					Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
					Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
					ReportError: func(_ *http.Request, errorValue error) { reported <- errorValue },
				})
				if err != nil {
					t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
				}
				server := httptest.NewUnstartedServer(middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					writer.Header().Set("Trailer", "X-App")
					writer.WriteHeader(http.StatusOK)
					writer.Header().Set("X-App", "preserved")
					attack.inject(writer.Header())
				})))
				server.EnableHTTP2 = protocol.h2
				if protocol.h2 {
					server.StartTLS()
				} else {
					server.Start()
				}
				defer server.Close()

				client := server.Client()
				if !protocol.h2 {
					network := &http.Transport{ForceAttemptHTTP2: false}
					defer network.CloseIdleConnections()
					client.Transport = network
				}
				response, err := client.Get(server.URL)
				if err != nil {
					t.Fatalf("Get() error = %v", err)
				}
				_, readErr := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if readErr != nil {
					t.Fatalf("ReadAll() error = %v", readErr)
				}
				for _, name := range []string{"Content-Digest", "Signature-Input", "Signature"} {
					if response.Trailer.Get(name) != "" {
						t.Fatalf("injected protected trailer %q escaped: %#v", name, response.Trailer)
					}
				}
				if response.Trailer.Get("X-App") != "preserved" {
					t.Fatalf("application trailer = %#v", response.Trailer)
				}
				select {
				case reportedErr := <-reported:
					if !errors.Is(reportedErr, attack.want) {
						t.Fatalf("ReportError() = %v, want %v", reportedErr, attack.want)
					}
				default:
					t.Fatal("post-commit injection was not reported")
				}
			})
		}
	}
}

func TestResponseSigningRejectsProtocolTransitionsOnWire(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name    string
		method  string
		handler http.Handler
	}{
		{
			name: "101 protocol switch", method: http.MethodGet,
			handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Connection", "Upgrade")
				writer.Header().Set("Upgrade", "example")
				writer.WriteHeader(http.StatusSwitchingProtocols)
			}),
		},
		{
			name: "successful CONNECT", method: http.MethodConnect,
			handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte("tunnel bytes"))
			}),
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			mapped := make(chan error, 1)
			reported := make(chan error, 1)
			middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
				Signer: NewSigner(testResponseSigningProfile(t, now, key)), Label: "response",
				Existing: ExistingSignaturesReject, MaxBufferedBytes: 32,
				Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
					return SigningOptions{}, nil
				},
				MapError: func(writer http.ResponseWriter, _ *http.Request, errorValue error) {
					clearHTTPHeader(writer.Header())
					http.Error(writer, "rejected", http.StatusBadGateway)
					mapped <- errorValue
				},
				ReportError: func(_ *http.Request, errorValue error) { reported <- errorValue },
			})
			if err != nil {
				t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
			}
			server := httptest.NewServer(middleware(test.handler))
			defer server.Close()

			request, err := http.NewRequest(test.method, server.URL, nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusBadGateway || response.Header.Get("Signature") != "" {
				t.Fatalf("wire response status=%d header=%#v", response.StatusCode, response.Header)
			}
			select {
			case mappedErr := <-mapped:
				if !errors.Is(mappedErr, ErrInvalidHTTPIntegration) {
					t.Fatalf("MapError() = %v", mappedErr)
				}
			default:
				t.Fatal("protocol transition was not mapped before commit")
			}
			select {
			case reportedErr := <-reported:
				t.Fatalf("ReportError() called for pre-commit rejection: %v", reportedErr)
			default:
			}
		})
	}
}

func TestResponseSigningRejectsPreexistingOuterProtectedFields(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name    string
		header  http.Header
		wantErr error
	}{
		{name: "existing digest", header: http.Header{"Content-Digest": []string{"sha-256=:AA==:"}}, wantErr: ErrExistingDigest},
		{name: "existing signature", header: http.Header{"Signature": []string{"sig=:AA==:"}}, wantErr: ErrExistingSignatures},
		{name: "existing content length", header: http.Header{"Content-Length": []string{"999"}}, wantErr: ErrInvalidHTTPIntegration},
		{name: "case collision", header: http.Header{"Signature": []string{"sig=:AA==:"}, "signature": []string{"sig=:AQ==:"}}, wantErr: ErrAmbiguousProtectedField},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			for name, values := range test.header {
				recorder.Header()[name] = append([]string(nil), values...)
			}
			var mapped error
			middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
				Signer: NewSigner(testResponseSigningProfile(t, now, key)), Label: "response",
				Existing: ExistingSignaturesReject, MaxBufferedBytes: 32,
				Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
					return SigningOptions{}, nil
				},
				MapError:    func(_ http.ResponseWriter, _ *http.Request, errorValue error) { mapped = errorValue },
				ReportError: func(*http.Request, error) {},
			})
			if err != nil {
				t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
			}
			handlerCalls := 0
			middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { handlerCalls++ })).ServeHTTP(
				recorder, httptest.NewRequest(http.MethodGet, "https://example.com/data", nil),
			)
			if !errors.Is(mapped, test.wantErr) || handlerCalls != 0 {
				t.Fatalf("mapped error = %v, handler calls = %d", mapped, handlerCalls)
			}
		})
	}
}

func TestResponseSigningRejectsConflictingContentLength(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{{Name: "@status"}, {Name: "content-length"}},
		Expires:           ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		Lifetime: time.Minute, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewSigningProfile() error = %v", err)
	}
	var mapped error
	middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
		Signer: NewSigner(profile), Label: "response", Existing: ExistingSignaturesReject, MaxBufferedBytes: 32,
		Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
			return SigningOptions{}, nil
		},
		MapError:    func(_ http.ResponseWriter, _ *http.Request, errorValue error) { mapped = errorValue },
		ReportError: func(*http.Request, error) {},
	})
	if err != nil {
		t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", "999")
		_, _ = writer.Write([]byte("abc"))
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://example.com/data", nil))
	if !errors.Is(mapped, ErrInvalidHTTPIntegration) || recorder.Body.Len() != 0 {
		t.Fatalf("mapped error = %v, copied body = %q", mapped, recorder.Body.Bytes())
	}

	mapped = nil
	recorder = httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header()["Content-Length"] = []string{"3"}
		writer.Header()["content-length"] = []string{"3"}
		_, _ = writer.Write([]byte("abc"))
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://example.com/data", nil))
	if !errors.Is(mapped, ErrAmbiguousProtectedField) || recorder.Body.Len() != 0 {
		t.Fatalf("case-colliding mapped error = %v, copied body = %q", mapped, recorder.Body.Bytes())
	}
}

func TestResponseSigningReportsRedactedCommittedWriteFailures(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name  string
		count int
		err   error
	}{
		{name: "write error", err: errors.New("private backend detail")},
		{name: "short write", count: 1},
		{name: "negative count", count: -1},
		{name: "oversized count", count: len("payload") + 1},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var reported error
			reportCalls := 0
			middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
				Signer: NewSigner(testResponseSigningProfile(t, now, key)), Label: "response",
				Existing: ExistingSignaturesReject, MaxBufferedBytes: 32,
				Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
					return SigningOptions{}, nil
				},
				MapError: func(http.ResponseWriter, *http.Request, error) {
					t.Error("MapError called after response commit")
				},
				ReportError: func(_ *http.Request, errorValue error) {
					reportCalls++
					reported = errorValue
				},
			})
			if err != nil {
				t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
			}
			writer := &bufferedEmissionFailureWriter{header: make(http.Header), count: test.count, err: test.err}
			middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte("payload"))
			})).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "https://example.com/data", nil))
			if writer.status != http.StatusOK || reportCalls != 1 || !errors.Is(reported, ErrBodyRead) {
				t.Fatalf("status=%d report calls=%d error=%v", writer.status, reportCalls, reported)
			}
			if strings.Contains(reported.Error(), "private") {
				t.Fatalf("ReportError disclosed backend detail: %q", reported)
			}
		})
	}
}

func TestResponseSigningPreservesValidRepresentationContentLength(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{{Name: "@status"}, {Name: "content-length"}},
		Expires:           ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		Lifetime: time.Minute, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewSigningProfile() error = %v", err)
	}
	for _, test := range []struct {
		name   string
		method string
		status int
	}{
		{name: "HEAD", method: http.MethodHead, status: http.StatusOK},
		{name: "304", method: http.MethodGet, status: http.StatusNotModified},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var mapped error
			var signedLength int64
			middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
				Signer: NewSigner(profile), Label: "response", Existing: ExistingSignaturesReject, MaxBufferedBytes: 32,
				Options: func(_ context.Context, _ *http.Request, response *http.Response) (SigningOptions, error) {
					signedLength = response.ContentLength
					return SigningOptions{}, nil
				},
				MapError:    func(_ http.ResponseWriter, _ *http.Request, errorValue error) { mapped = errorValue },
				ReportError: func(*http.Request, error) {},
			})
			if err != nil {
				t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
			}
			recorder := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Length", "123")
				writer.WriteHeader(test.status)
			})).ServeHTTP(recorder, httptest.NewRequest(test.method, "https://example.com/data", nil))
			if mapped != nil || signedLength != 123 || recorder.Header().Get("Content-Length") != "123" {
				t.Fatalf("mapped error = %v, signed length = %d, wire length = %q", mapped, signedLength, recorder.Header().Get("Content-Length"))
			}
		})
	}
}

func TestResponseSigningOwnsOuterHeadersThroughHandlerAndWire(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	reported := make(chan error, 1)
	middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
		Signer: NewSigner(hardeningOuterHeaderSigningProfile(t, now, key)), Label: "response",
		Existing: ExistingSignaturesReject, MaxBufferedBytes: 32,
		Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
			return SigningOptions{}, nil
		},
		MapError:    func(_ http.ResponseWriter, _ *http.Request, errorValue error) { reported <- errorValue },
		ReportError: func(_ *http.Request, errorValue error) { reported <- errorValue },
	})
	if err != nil {
		t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
	}
	signedHandler := middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if got := writer.Header().Values("X-Signed"); !slices.Equal(got, []string{"outer"}) {
			t.Errorf("handler X-Signed = %#v, want inherited outer value", got)
		}
		if got := writer.Header().Values("Set-Cookie"); !slices.Equal(got, []string{"outer=1"}) {
			t.Errorf("handler Set-Cookie = %#v, want inherited outer value", got)
		}
		writer.Header().Set("X-Signed", "inner")
		writer.Header().Add("Set-Cookie", "inner=2")
		writer.Header().Del("X-Delete")
		_, _ = writer.Write([]byte("payload"))
	}))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Signed", "outer")
		writer.Header().Add("Set-Cookie", "outer=1")
		writer.Header().Set("X-Delete", "outer")
		signedHandler.ServeHTTP(writer, request)
	}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/data", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if body, readErr := io.ReadAll(response.Body); readErr != nil || string(body) != "payload" {
		t.Fatalf("response body = %q, error = %v", body, readErr)
	}
	if got := response.Header.Values("X-Signed"); !slices.Equal(got, []string{"inner"}) {
		t.Fatalf("wire X-Signed = %#v, want exact signed value", got)
	}
	if got := response.Header.Values("Set-Cookie"); !slices.Equal(got, []string{"outer=1", "inner=2"}) {
		t.Fatalf("wire Set-Cookie = %#v, want inherited and appended values", got)
	}
	if response.Header.Get("X-Delete") != "" {
		t.Fatalf("deleted outer header escaped to wire: %#v", response.Header)
	}
	inputs, err := ParseSignatureInputs(response.Header.Values("Signature-Input"))
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	signatures, err := ParseSignatures(response.Header.Values("Signature"))
	if err != nil {
		t.Fatalf("ParseSignatures() error = %v", err)
	}
	if _, err := NewVerifier(hardeningOuterHeaderVerificationProfile(t, now, key)).Verify(
		context.Background(), MessageContext{Response: response, RelatedRequest: request}, "response", inputs, signatures,
	); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	select {
	case reportedErr := <-reported:
		t.Fatalf("MapError() = %v", reportedErr)
	default:
	}
}

func TestResponseSigningRejectsHandlerManagedFramingOnHTTP1Wire(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name        string
		method      string
		status      int
		header      http.Header
		coverLength bool
		want        error
	}{
		{name: "chunked", method: http.MethodGet, status: http.StatusOK, header: http.Header{"Transfer-Encoding": []string{"chunked"}}, coverLength: true, want: ErrInvalidHTTPIntegration},
		{name: "identity", method: http.MethodGet, status: http.StatusOK, header: http.Header{"Transfer-Encoding": []string{"identity"}}, coverLength: true, want: ErrInvalidHTTPIntegration},
		{name: "multiple encodings", method: http.MethodGet, status: http.StatusOK, header: http.Header{"Transfer-Encoding": []string{"gzip", "chunked"}}, coverLength: true, want: ErrInvalidHTTPIntegration},
		{name: "lowercase alias", method: http.MethodGet, status: http.StatusOK, header: http.Header{"transfer-encoding": []string{"chunked"}}, coverLength: true, want: ErrInvalidHTTPIntegration},
		{name: "case collision", method: http.MethodGet, status: http.StatusOK, header: http.Header{
			"Transfer-Encoding": []string{"chunked"},
			"transfer-encoding": []string{"identity"},
		}, coverLength: true, want: ErrAmbiguousProtectedField},
		{name: "trailer declaration", method: http.MethodGet, status: http.StatusOK, header: http.Header{"Trailer": []string{"X-Final"}}, coverLength: true, want: ErrInvalidHTTPIntegration},
		{name: "late trailer key", method: http.MethodGet, status: http.StatusOK, header: http.Header{http.TrailerPrefix + "X-Final": []string{"complete"}}, coverLength: true, want: ErrInvalidHTTPIntegration},
		{name: "HEAD", method: http.MethodHead, status: http.StatusOK, header: http.Header{"Transfer-Encoding": []string{"chunked"}}, coverLength: true, want: ErrInvalidHTTPIntegration},
		{name: "204", method: http.MethodGet, status: http.StatusNoContent, header: http.Header{"Transfer-Encoding": []string{"chunked"}}, want: ErrInvalidHTTPIntegration},
		{name: "304", method: http.MethodGet, status: http.StatusNotModified, header: http.Header{"Transfer-Encoding": []string{"chunked"}}, want: ErrInvalidHTTPIntegration},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			profile := testResponseSigningProfile(t, now, key)
			if test.coverLength {
				profile = hardeningContentLengthSigningProfile(t, now, key)
			}
			mapped := make(chan error, 1)
			middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
				Signer: NewSigner(profile), Label: "response", Existing: ExistingSignaturesReject, MaxBufferedBytes: 32,
				Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
					return SigningOptions{}, nil
				},
				MapError: func(writer http.ResponseWriter, _ *http.Request, errorValue error) {
					mapped <- errorValue
					http.Error(writer, "rejected", http.StatusBadGateway)
				},
				ReportError: func(_ *http.Request, errorValue error) { mapped <- errorValue },
			})
			if err != nil {
				t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
			}
			server := httptest.NewServer(middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				for name, values := range test.header {
					writer.Header()[name] = append([]string(nil), values...)
				}
				writer.WriteHeader(test.status)
				if responseBodyAllowed(test.status) {
					_, _ = writer.Write([]byte("payload"))
				}
			})))
			defer server.Close()
			request, err := http.NewRequest(test.method, server.URL+"/data", nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusBadGateway || response.Header.Get("Signature") != "" {
				t.Fatalf("wire response status = %d, header = %#v", response.StatusCode, response.Header)
			}
			select {
			case mappedErr := <-mapped:
				if !errors.Is(mappedErr, test.want) {
					t.Fatalf("MapError() = %v, want %v", mappedErr, test.want)
				}
			default:
				t.Fatal("handler-managed framing was not rejected")
			}
		})
	}
}

func TestTrailerResponseSigningUsesImmutableNormalizedRequestSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request := httptest.NewRequest(http.MethodGet, "https://example.com/original", nil)
	original := request.Clone(request.Context())
	var reported error
	middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
		Signer: NewSigner(hardeningResponseTrailerBindingSigningProfile(t, now, key)), Label: "trail",
		Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		Options: func(_ context.Context, callbackRequest *http.Request) (SigningOptions, error) {
			callbackRequest.URL.Path = "/callback"
			return SigningOptions{}, nil
		},
		ReportError: func(_ *http.Request, errorValue error) { reported = errorValue },
	})
	if err != nil {
		t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(writer http.ResponseWriter, handlerRequest *http.Request) {
		if handlerRequest.URL.Path != "/original" {
			t.Errorf("handler path = %q, want /original", handlerRequest.URL.Path)
		}
		handlerRequest.URL.Path = "/handler"
		_, _ = writer.Write([]byte("payload"))
	})).ServeHTTP(recorder, request)
	if reported != nil {
		t.Fatalf("ReportError() = %v", reported)
	}
	response := recorder.Result()
	defer func() { _ = response.Body.Close() }()
	inputs, err := ParseSignatureInputs(response.Trailer.Values("Signature-Input"))
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	signatures, err := ParseSignatures(response.Trailer.Values("Signature"))
	if err != nil {
		t.Fatalf("ParseSignatures() error = %v", err)
	}
	if _, err := NewVerifier(hardeningResponseTrailerBindingVerificationProfile(t, now, key)).Verify(
		context.Background(), MessageContext{Response: response, RelatedRequest: original}, "trail", inputs, signatures,
	); err != nil {
		t.Fatalf("Verify(original request snapshot) error = %v", err)
	}
}

func TestTrailerResponseSigningRejectsRelatedRequestProtectedCollision(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request := httptest.NewRequest(http.MethodGet, "https://example.com/data", nil)
	request.Header = http.Header{"Accept-Signature": []string{"sig=()"}, "accept-signature": []string{"other=()"}}
	var reported error
	middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
		Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
		Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		ReportError: func(_ *http.Request, errorValue error) { reported = errorValue },
	})
	if err != nil {
		t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
	}
	handlerCalls := 0
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { handlerCalls++ })).ServeHTTP(httptest.NewRecorder(), request)
	if !errors.Is(reported, ErrAmbiguousProtectedField) || handlerCalls != 0 {
		t.Fatalf("reported error = %v, handler calls = %d", reported, handlerCalls)
	}
}

func TestTrailerResponseSigningRejectsDestinationProtectedCollision(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	var reported error
	middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
		Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
		Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		ReportError: func(_ *http.Request, errorValue error) { reported = errorValue },
	})
	if err != nil {
		t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	recorder.Header()["Accept-Signature"] = []string{"sig=()"}
	recorder.Header()["accept-signature"] = []string{"other=()"}
	handlerCalls := 0
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { handlerCalls++ })).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "https://example.com/data", nil),
	)
	if !errors.Is(reported, ErrAmbiguousProtectedField) || handlerCalls != 0 {
		t.Fatalf("reported error = %v, handler calls = %d", reported, handlerCalls)
	}
}

func TestTrailerSigningBodyRedactsReadAndCloseErrors(t *testing.T) {
	t.Parallel()

	readFailure := &trailerSigningBody{
		body: &failingReadBody{}, ctx: context.Background(), maxBytes: 1,
		finalize: func(DigestField) error { return nil },
	}
	if count, err := readFailure.Read(make([]byte, 1)); count != 0 || !errors.Is(err, ErrBodyRead) || strings.Contains(err.Error(), "private") {
		t.Fatalf("Read() = %d, %v, want redacted ErrBodyRead", count, err)
	}
	closeFailure := &trailerSigningBody{
		body: &closeFailingBody{reader: strings.NewReader("payload")}, ctx: context.Background(), maxBytes: 8,
		finalize: func(DigestField) error { return nil },
	}
	if err := closeFailure.Close(); !errors.Is(err, ErrBodyRead) || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("Close() = %v, want redacted ErrBodyRead", err)
	}
}

func TestTrailerResponseSigningRejectsProtectedFieldCollisionBeforeAndAfterCommit(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name    string
		handler http.Handler
	}{
		{name: "before commit", handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Accept-Signature", "sig=()")
			writer.Header()["accept-signature"] = []string{"other=()"}
			_, _ = writer.Write([]byte("payload"))
		})},
		{name: "after commit", handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusOK)
			writer.Header().Set("Accept-Signature", "sig=()")
			writer.Header()["accept-signature"] = []string{"other=()"}
		})},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reported := make(chan error, 1)
			middleware, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
				Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
				Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
				Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
				ReportError: func(_ *http.Request, errorValue error) { reported <- errorValue },
			})
			if err != nil {
				t.Fatalf("NewTrailerResponseSigningMiddleware() error = %v", err)
			}
			middleware(test.handler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "https://example.com/data", nil))
			select {
			case reportedErr := <-reported:
				if !errors.Is(reportedErr, ErrAmbiguousProtectedField) {
					t.Fatalf("ReportError() = %v", reportedErr)
				}
			default:
				t.Fatal("collision was not reported")
			}
		})
	}
}

func TestHTTPAdaptersRejectCaseCollidingProtectedFields(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))

	t.Run("request signing", func(t *testing.T) {
		request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
		request.Header = http.Header{
			"Accept-Signature": []string{"sig=()"},
			"accept-signature": []string{"other=()"},
		}
		calls := 0
		transport, err := NewSigningRoundTripper(SigningRoundTripperConfig{
			Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
			}),
			Signer: NewSigner(testSigningProfile(t, now, key)), Label: "sig", Existing: ExistingSignaturesReject,
			Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		})
		if err != nil {
			t.Fatalf("NewSigningRoundTripper() error = %v", err)
		}
		if got, signErr := transport.RoundTrip(request); got != nil || signErr == nil || calls != 0 {
			t.Fatalf("RoundTrip() = %#v, %v, calls=%d, want collision rejection", got, signErr, calls)
		}
	})

	t.Run("request verification", func(t *testing.T) {
		request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
		signed, err := NewSigner(testSigningProfile(t, now, key)).Sign(context.Background(), MessageContext{Request: request}, "sig", SigningOptions{})
		if err != nil {
			t.Fatalf("Sign() error = %v", err)
		}
		request.Header.Set("Signature-Input", signed.SignatureInputField())
		request.Header.Set("Signature", signed.SignatureField())
		request.Header["signature"] = []string{"sig=:YmFk:"}
		mapped := make(chan error, 1)
		middleware, err := NewRequestVerificationMiddleware(RequestVerificationMiddlewareConfig{
			Verifier:    NewVerifier(testHTTPVerificationProfile(t, now, key)),
			SelectLabel: func(*http.Request, SignatureInputs, Signatures) (string, error) { return "sig", nil },
			MapError:    func(_ http.ResponseWriter, _ *http.Request, errorValue error) { mapped <- errorValue },
		})
		if err != nil {
			t.Fatalf("NewRequestVerificationMiddleware() error = %v", err)
		}
		nextCalls := 0
		middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalls++ })).ServeHTTP(httptest.NewRecorder(), request)
		if nextCalls != 0 {
			t.Fatal("case-colliding request reached the next handler")
		}
		select {
		case <-mapped:
		default:
			t.Fatal("case-colliding request did not map an error")
		}
	})

	t.Run("response signing", func(t *testing.T) {
		mapped := make(chan error, 1)
		middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
			Signer: NewSigner(testResponseSigningProfile(t, now, key)), Label: "response",
			Existing: ExistingSignaturesReject, MaxBufferedBytes: 32,
			Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
				return SigningOptions{}, nil
			},
			MapError:    func(_ http.ResponseWriter, _ *http.Request, errorValue error) { mapped <- errorValue },
			ReportError: func(_ *http.Request, errorValue error) { mapped <- errorValue },
		})
		if err != nil {
			t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
		}
		request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
		middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Signature", "old=:YmFk:")
			writer.Header()["signature"] = []string{"old=:YmFk:"}
		})).ServeHTTP(httptest.NewRecorder(), request)
		select {
		case <-mapped:
		default:
			t.Fatal("case-colliding signed response did not map an error")
		}
	})

	t.Run("response signing request", func(t *testing.T) {
		mapped := make(chan error, 1)
		middleware, err := NewResponseSigningMiddleware(ResponseSigningMiddlewareConfig{
			Signer: NewSigner(testResponseSigningProfile(t, now, key)), Label: "response",
			Existing: ExistingSignaturesReject, MaxBufferedBytes: 32,
			Options: func(context.Context, *http.Request, *http.Response) (SigningOptions, error) {
				return SigningOptions{}, nil
			},
			MapError:    func(_ http.ResponseWriter, _ *http.Request, errorValue error) { mapped <- errorValue },
			ReportError: func(_ *http.Request, errorValue error) { mapped <- errorValue },
		})
		if err != nil {
			t.Fatalf("NewResponseSigningMiddleware() error = %v", err)
		}
		request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
		request.Header = http.Header{"Accept-Signature": []string{"sig=()"}, "accept-signature": []string{"other=()"}}
		middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler called") })).ServeHTTP(httptest.NewRecorder(), request)
		select {
		case <-mapped:
		default:
			t.Fatal("case-colliding related request did not map an error")
		}
	})

	t.Run("response verification", func(t *testing.T) {
		request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
		response := signedHeaderResponse(t, now, key, request)
		response.Header["signature"] = []string{"response=:YmFk:"}
		transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
			Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
			Verifier:  NewVerifier(testResponseVerificationProfile(t, now, key)),
			SelectLabel: func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) {
				return "response", nil
			},
		})
		if err != nil {
			t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
		}
		if got, verifyErr := transport.RoundTrip(request); got != nil || verifyErr == nil {
			t.Fatalf("RoundTrip() = %#v, %v, want collision rejection", got, verifyErr)
		}
	})

	t.Run("response trailer verification", func(t *testing.T) {
		request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
		response := signedHeaderResponse(t, now, key, request)
		response.Trailer = http.Header{"Accept-Signature": []string{"sig=()"}, "accept-signature": []string{"other=()"}}
		transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
			Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
			Verifier:  NewVerifier(testResponseVerificationProfile(t, now, key)),
			SelectLabel: func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) {
				return "response", nil
			},
		})
		if err != nil {
			t.Fatalf("NewVerifyingRoundTripper() error = %v", err)
		}
		if got, verifyErr := transport.RoundTrip(request); got != nil || verifyErr == nil {
			t.Fatalf("RoundTrip() = %#v, %v, want trailer collision rejection", got, verifyErr)
		}
	})
}

func TestBodyAdaptersRejectCaseCollidingProtectedFields(t *testing.T) {
	t.Parallel()

	t.Run("buffered request signing", func(t *testing.T) {
		body := &observedBody{reader: strings.NewReader("payload")}
		request := httptest.NewRequest(http.MethodPost, "https://example.com/data", body)
		request.Header = http.Header{"Accept-Signature": []string{"sig=()"}, "accept-signature": []string{"other=()"}}
		transport, err := NewBufferedContentDigestRoundTripper(BufferedContentDigestRoundTripperConfig{
			Transport:  roundTripperFunc(func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil }),
			Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		})
		if err != nil {
			t.Fatalf("NewBufferedContentDigestRoundTripper() error = %v", err)
		}
		if got, signErr := transport.RoundTrip(request); got != nil || !errors.Is(signErr, ErrAmbiguousProtectedField) {
			t.Fatalf("RoundTrip() = %#v, %v, want ErrAmbiguousProtectedField", got, signErr)
		}
		if !body.closed {
			t.Fatal("rejected body was not closed")
		}
	})

	t.Run("buffered request verification", func(t *testing.T) {
		digests, _ := ComputeDigests([]DigestAlgorithm{SHA256}, []byte("payload"))
		request := httptest.NewRequest(http.MethodPost, "https://example.com/data", strings.NewReader("payload"))
		request.Header.Set("Content-Digest", digests.String())
		request.Header["content-digest"] = []string{"sha-256=:YmFk:"}
		mapped := make(chan error, 1)
		middleware, err := NewBufferedContentDigestVerificationMiddleware(BufferedContentDigestVerificationMiddlewareConfig{
			RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
			MapError: func(_ http.ResponseWriter, _ *http.Request, errorValue error) { mapped <- errorValue },
		})
		if err != nil {
			t.Fatalf("NewBufferedContentDigestVerificationMiddleware() error = %v", err)
		}
		nextCalls := 0
		middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalls++ })).ServeHTTP(httptest.NewRecorder(), request)
		if nextCalls != 0 {
			t.Fatal("case-colliding digest reached the next handler")
		}
		select {
		case <-mapped:
		default:
			t.Fatal("case-colliding digest did not map an error")
		}
	})

	t.Run("trailer request signing", func(t *testing.T) {
		now := time.Unix(1_700_000_000, 0)
		key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
		request := httptest.NewRequest(http.MethodPost, "https://example.com/data", strings.NewReader("payload"))
		request.Trailer = http.Header{
			"Content-Digest": []string(nil),
			"content-digest": []string{"sha-256=:YmFk:"},
		}
		calls := 0
		transport, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
			Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }),
			Signer:    NewSigner(testTrailerSigningProfile(t, now, key)), Label: "trail",
			Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
			Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		})
		if err != nil {
			t.Fatalf("NewTrailerSigningRoundTripper() error = %v", err)
		}
		if got, signErr := transport.RoundTrip(request); got != nil || signErr == nil || calls != 0 {
			t.Fatalf("RoundTrip() = %#v, %v, calls=%d, want collision rejection", got, signErr, calls)
		}
	})

	t.Run("trailer response verification", func(t *testing.T) {
		now := time.Unix(1_700_000_000, 0)
		key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
		request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
		response := signedTrailerResponse(t, now, key, request, "payload")
		response.Trailer["signature"] = []string{"trail=:YmFk:"}
		transport, err := NewBufferedTrailerVerifyingRoundTripper(BufferedTrailerVerifyingRoundTripperConfig{
			Transport:          roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
			Verifier:           NewVerifier(testResponseTrailerVerificationProfile(t, now, key)),
			SelectLabel:        func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "trail", nil },
			RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		})
		if err != nil {
			t.Fatalf("NewBufferedTrailerVerifyingRoundTripper() error = %v", err)
		}
		if got, verifyErr := transport.RoundTrip(request); got != nil || verifyErr == nil {
			t.Fatalf("RoundTrip() = %#v, %v, want collision rejection", got, verifyErr)
		}
	})

	t.Run("trailer request verification header", func(t *testing.T) {
		now := time.Unix(1_700_000_000, 0)
		key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
		body := &observedBody{reader: strings.NewReader("payload")}
		request := httptest.NewRequest(http.MethodPost, "https://example.com/data", body)
		request.Header = http.Header{"Accept-Signature": []string{"sig=()"}, "accept-signature": []string{"other=()"}}
		mapped := make(chan error, 1)
		middleware, err := NewBufferedTrailerVerificationMiddleware(BufferedTrailerVerificationMiddlewareConfig{
			Verifier:           NewVerifier(testTrailerVerificationProfile(t, now, key)),
			SelectLabel:        func(*http.Request, SignatureInputs, Signatures) (string, error) { return "trail", nil },
			RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
			MapError: func(_ http.ResponseWriter, _ *http.Request, errorValue error) { mapped <- errorValue },
		})
		if err != nil {
			t.Fatalf("NewBufferedTrailerVerificationMiddleware() error = %v", err)
		}
		middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler called") })).ServeHTTP(httptest.NewRecorder(), request)
		if !body.closed {
			t.Fatal("rejected body was not closed")
		}
		select {
		case <-mapped:
		default:
			t.Fatal("case-colliding header did not map an error")
		}
	})

	t.Run("trailer request verification trailer", func(t *testing.T) {
		now := time.Unix(1_700_000_000, 0)
		key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
		request := signedTrailerRequest(t, now, key, "payload")
		populating := request.Body.(*trailerPopulatingBody)
		populating.values["signature"] = []string{"trail=:YmFk:"}
		mapped := make(chan error, 1)
		middleware, err := NewBufferedTrailerVerificationMiddleware(BufferedTrailerVerificationMiddlewareConfig{
			Verifier:           NewVerifier(testTrailerVerificationProfile(t, now, key)),
			SelectLabel:        func(*http.Request, SignatureInputs, Signatures) (string, error) { return "trail", nil },
			RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
			MapError: func(_ http.ResponseWriter, _ *http.Request, errorValue error) { mapped <- errorValue },
		})
		if err != nil {
			t.Fatalf("NewBufferedTrailerVerificationMiddleware() error = %v", err)
		}
		middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler called") })).ServeHTTP(httptest.NewRecorder(), request)
		select {
		case <-mapped:
		default:
			t.Fatal("case-colliding trailer did not map an error")
		}
	})

	t.Run("trailer response verification header", func(t *testing.T) {
		now := time.Unix(1_700_000_000, 0)
		key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
		request, _ := http.NewRequest(http.MethodGet, "https://example.com/data", nil)
		response := signedTrailerResponse(t, now, key, request, "payload")
		responseBody := &mutationContractBody{Reader: response.Body}
		response.Body = responseBody
		response.Header = http.Header{"Accept-Signature": []string{"sig=()"}, "accept-signature": []string{"other=()"}}
		transport, err := NewBufferedTrailerVerifyingRoundTripper(BufferedTrailerVerifyingRoundTripperConfig{
			Transport:          roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
			Verifier:           NewVerifier(testResponseTrailerVerificationProfile(t, now, key)),
			SelectLabel:        func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "trail", nil },
			RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 32,
		})
		if err != nil {
			t.Fatalf("NewBufferedTrailerVerifyingRoundTripper() error = %v", err)
		}
		if got, verifyErr := transport.RoundTrip(request); got != nil || !errors.Is(verifyErr, ErrAmbiguousProtectedField) {
			t.Fatalf("RoundTrip() = %#v, %v, want ErrAmbiguousProtectedField", got, verifyErr)
		}
		if responseBody.closed != 1 {
			t.Fatalf("ambiguous response close count = %d, want 1", responseBody.closed)
		}
	})
}

type optionalProtocolWriter struct {
	header        http.Header
	optionalCalls int
}

type flushProtocolWriter struct {
	header   http.Header
	status   int
	flushes  int
	flushErr error
}

func (writer *flushProtocolWriter) Header() http.Header { return writer.header }

func (writer *flushProtocolWriter) Write(content []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return len(content), nil
}

func (writer *flushProtocolWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
}

func (writer *flushProtocolWriter) FlushError() error {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	writer.flushes++
	return writer.flushErr
}

type unsupportedFlushWriter struct {
	header http.Header
	status int
}

func (writer *unsupportedFlushWriter) Header() http.Header { return writer.header }

func (writer *unsupportedFlushWriter) Write(content []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return len(content), nil
}

func (writer *unsupportedFlushWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
}

type invalidCountResponseWriter struct {
	header http.Header
	status int
	count  int
}

type bufferedEmissionFailureWriter struct {
	header http.Header
	status int
	count  int
	err    error
}

func (writer *bufferedEmissionFailureWriter) Header() http.Header { return writer.header }

func (writer *bufferedEmissionFailureWriter) Write([]byte) (int, error) {
	return writer.count, writer.err
}

func (writer *bufferedEmissionFailureWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
}

func (writer *invalidCountResponseWriter) Header() http.Header { return writer.header }

func (writer *invalidCountResponseWriter) Write([]byte) (int, error) {
	return writer.count, errors.New("private write detail")
}

func (writer *invalidCountResponseWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
}

func invokeTrailerResponseWrite(writer *trailerResponseWriter, content []byte) (count int, panicValue any, err error) {
	defer func() { panicValue = recover() }()
	count, err = writer.Write(content)
	return count, nil, err
}

func (writer *optionalProtocolWriter) Header() http.Header        { return writer.header }
func (*optionalProtocolWriter) Write(content []byte) (int, error) { return len(content), nil }
func (*optionalProtocolWriter) WriteHeader(int)                   {}
func (writer *optionalProtocolWriter) EnableFullDuplex() error    { writer.optionalCalls++; return nil }
func (writer *optionalProtocolWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	writer.optionalCalls++
	return nil, nil, nil
}

func signedDigestHeaderResponse(
	t *testing.T,
	now time.Time,
	key HMACKey,
	request *http.Request,
	digested []byte,
	body io.ReadCloser,
) *http.Response {
	t.Helper()
	digests, err := ComputeDigests([]DigestAlgorithm{SHA256}, digested)
	if err != nil {
		t.Fatalf("ComputeDigests() error = %v", err)
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: request}
	response.Header.Set("Content-Digest", digests.String())
	signed, err := NewSigner(hardeningResponseDigestSigningProfile(t, now, key)).Sign(
		context.Background(), MessageContext{Response: response}, "digest", SigningOptions{},
	)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	response.Header.Set("Signature-Input", signed.SignatureInputField())
	response.Header.Set("Signature", signed.SignatureField())
	return response
}

func hardeningResponseDigestSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		CoveredComponents:  []ComponentIdentifier{{Name: "@status"}, {Name: "content-digest"}},
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

func hardeningOuterHeaderSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "x-signed"},
			{Name: "set-cookie", Parameters: []Parameter{{Name: "bs", Value: true}}},
		},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
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

func hardeningOuterHeaderVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "x-signed"},
			{Name: "set-cookie", Parameters: []Parameter{{Name: "bs", Value: true}}},
		},
		Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		MaxAge: time.Minute, ClockSkew: time.Second, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	return profile
}

func hardeningContentLengthSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		CoveredComponents:  []ComponentIdentifier{{Name: "@status"}, {Name: "content-length"}},
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

func hardeningResponseDigestVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{{Name: "@status"}, {Name: "content-digest"}},
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

func hardeningResponseRequestBindingSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "@path", Parameters: []Parameter{{Name: "req", Value: true}}},
		},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
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

func hardeningResponseRequestBindingVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "@path", Parameters: []Parameter{{Name: "req", Value: true}}},
		},
		Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		MaxAge: time.Minute, ClockSkew: time.Second, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	return profile
}

func hardeningBufferedRequestTrailerSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@method"},
			{Name: "content-digest"},
			{Name: "transfer-encoding"},
			{Name: "x-final", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
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

func hardeningBufferedRequestTrailerVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@method"},
			{Name: "content-digest"},
			{Name: "transfer-encoding"},
			{Name: "x-final", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		MaxAge: time.Minute, ClockSkew: time.Second, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	return profile
}

func hardeningStreamingApplicationTrailerSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@method"},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
			{Name: "x-final", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
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

func hardeningStreamingApplicationTrailerVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@method"},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
			{Name: "x-final", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		MaxAge: time.Minute, ClockSkew: time.Second, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	return profile
}

func hardeningTrailerPathSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@method"},
			{Name: "@path"},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
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

func hardeningTrailerPathVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@method"},
			{Name: "@path"},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		MaxAge: time.Minute, ClockSkew: time.Second, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	return profile
}

func hardeningResponseTrailerBindingSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "@path", Parameters: []Parameter{{Name: "req", Value: true}}},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
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

func hardeningResponseTrailerBindingVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "@path", Parameters: []Parameter{{Name: "req", Value: true}}},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		MaxAge: time.Minute, ClockSkew: time.Second, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	return profile
}

func hardeningCommittedTrailerSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "x-signed"},
			{Name: "x-final", Parameters: []Parameter{{Name: "tr", Value: true}}},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
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

func hardeningLateTrailerSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "x-late", Parameters: []Parameter{{Name: "tr", Value: true}}},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
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

func hardeningLateTrailerVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "x-late", Parameters: []Parameter{{Name: "tr", Value: true}}},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		MaxAge: time.Minute, ClockSkew: time.Second, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	return profile
}

func hardeningCommittedTrailerVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "x-signed"},
			{Name: "x-final", Parameters: []Parameter{{Name: "tr", Value: true}}},
			{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		},
		Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		MaxAge: time.Minute, ClockSkew: time.Second, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	return profile
}

func hardeningKeyedDigestSigningProfile(t *testing.T, now time.Time, key HMACKey) *SigningProfile {
	t.Helper()
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "content-digest", Parameters: []Parameter{{Name: "key", Value: "sha-256"}}},
		},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
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

func hardeningKeyedDigestVerificationProfile(t *testing.T, now time.Time, key HMACKey) *VerificationProfile {
	t.Helper()
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@status"},
			{Name: "content-digest", Parameters: []Parameter{{Name: "key", Value: "sha-256"}}},
		},
		Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		MaxAge: time.Minute, ClockSkew: time.Second, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	return profile
}
