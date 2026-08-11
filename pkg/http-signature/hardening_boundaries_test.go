package httpsignature

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBufferedBodyRejectsInvalidReaderCount(t *testing.T) {
	t.Parallel()

	for _, count := range []int{-1, 32*1024 + 1} {
		_, err := readBoundedAndClose(context.Background(), invalidCountReadCloser{count: count}, 1<<20)
		if !errors.Is(err, ErrBodyRead) {
			t.Fatalf("readBoundedAndClose() count %d error = %v, want ErrBodyRead", count, err)
		}
	}
}

func TestTrailerBodyRejectsInvalidReaderCount(t *testing.T) {
	t.Parallel()

	digest, err := newDigestWriter(SHA256)
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{-1, 9} {
		body := &trailerSigningBody{
			body:     invalidCountReadCloser{count: count},
			ctx:      context.Background(),
			maxBytes: 1 << 20,
			writers:  []digestWriter{{algorithm: SHA256, hash: digest}},
		}
		_, err = body.Read(make([]byte, 8))
		if !errors.Is(err, ErrBodyRead) {
			t.Fatalf("Read() count %d error = %v, want ErrBodyRead", count, err)
		}
	}
}

func TestSignatureBaseUsesNetHTTPContentLength(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodPost, "https://example.com/upload", strings.NewReader("abc"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Length", "999")
	input := SignatureInput{
		Components: []ComponentIdentifier{{Name: "content-length"}},
		Parameters: []Parameter{{Name: "created", Value: int64(1)}},
	}
	base, err := CreateSignatureBase(MessageContext{Request: request}, input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(base, "\"content-length\": 3\n") {
		t.Fatalf("request signature base = %q", base)
	}

	response := &http.Response{
		StatusCode: http.StatusOK, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Length": []string{"999"}}, Body: io.NopCloser(strings.NewReader("abc")), ContentLength: 3,
	}
	base, err = CreateSignatureBase(MessageContext{Response: response, ResponseTransport: ResponseTransportWrite}, input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(base, "\"content-length\": 3\n") {
		t.Fatalf("response signature base = %q", base)
	}
}

func TestSignatureBaseAllowsEmptyRFCQueryParameterValue(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "https://example.com/path?qux=", nil)
	if err != nil {
		t.Fatal(err)
	}
	input := SignatureInput{
		Components: []ComponentIdentifier{{
			Name:       "@query-param",
			Parameters: []Parameter{{Name: "name", Value: "qux"}},
		}},
		Parameters: []Parameter{{Name: "created", Value: int64(1)}},
	}
	base, err := CreateSignatureBase(MessageContext{Request: request}, input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(base, "\"@query-param\";name=\"qux\": \n") {
		t.Fatalf("signature base = %q", base)
	}
}

func TestSignatureBaseUsesNetHTTPAuthorityOverride(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "https://backend.example/path", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "public.example"
	input := SignatureInput{
		Components: []ComponentIdentifier{{Name: "@authority"}, {Name: "host"}},
		Parameters: []Parameter{{Name: "created", Value: int64(1)}},
	}
	base, err := CreateSignatureBase(MessageContext{Request: request}, input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(base, "\"@authority\": public.example\n\"host\": public.example\n") {
		t.Fatalf("signature base = %q", base)
	}
	request.Header["Host"] = []string{"attacker.example"}
	base, err = CreateSignatureBase(MessageContext{Request: request}, input)
	if err != nil {
		t.Fatalf("CreateSignatureBase(Header Host conflict) error = %v", err)
	}
	if !strings.HasPrefix(base, "\"@authority\": public.example\n\"host\": public.example\n") {
		t.Fatalf("Header Host conflict signature base = %q", base)
	}

	request.Host = ""
	input.Components = []ComponentIdentifier{{Name: "host"}}
	base, err = CreateSignatureBase(MessageContext{Request: request}, input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(base, "\"host\": backend.example\n") {
		t.Fatalf("URL-derived host signature base = %q", base)
	}
}

func TestSignatureBaseUsesSanitizedNetHTTPContentLength(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodPost, "https://example.test/", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header["Content-Length"] = []string{"999"}
	input := SignatureInput{Components: []ComponentIdentifier{{Name: "content-length"}}}
	base, err := CreateSignatureBase(MessageContext{Request: request}, input)
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	if !strings.HasPrefix(base, "\"content-length\": 0\n") {
		t.Fatalf("signature base = %q, want transport-sanitized Content-Length 0", base)
	}

	request.TransferEncoding = []string{"chunked"}
	if _, err := CreateSignatureBase(MessageContext{Request: request}, input); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("CreateSignatureBase(chunked) error = %v, want ErrSignatureBase", err)
	}
}

func TestSignatureBaseStripsIPv6ZoneLikeNetHTTPWireHost(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "http://[fe80::1%25eth0]/", nil)
	if err != nil {
		t.Fatal(err)
	}
	input := SignatureInput{Components: []ComponentIdentifier{{Name: "@authority"}, {Name: "host"}}}
	base, err := CreateSignatureBase(MessageContext{Request: request}, input)
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	if !strings.HasPrefix(base, "\"@authority\": [fe80::1]\n\"host\": [fe80::1]\n") {
		t.Fatalf("signature base = %q, want zoneless wire authority and Host", base)
	}
}

func TestSignatureBaseRejectsHostThatNetHTTPSanitizesOffWire(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "attacker.example/ smuggled"
	if _, err := CreateSignatureBase(MessageContext{Request: request}, SignatureInput{
		Components: []ComponentIdentifier{{Name: "host"}},
	}); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("CreateSignatureBase() error = %v, want ErrSignatureBase", err)
	}

	var wire bytes.Buffer
	if err := request.Write(&wire); err != nil {
		t.Fatalf("Request.Write() error = %v", err)
	}
	if !strings.Contains(wire.String(), "Host: \r\n") {
		t.Fatalf("wire request = %q, want sanitized empty Host", wire.String())
	}
}

func TestSignatureBaseUsesNetHTTPTransportManagedRequestFields(t *testing.T) {
	t.Parallel()

	t.Run("user agent", func(t *testing.T) {
		request, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header["User-Agent"] = []string{"first", "ignored"}
		assertTransportManagedFieldMatchesWire(t, request, "user-agent", "first", "User-Agent: first\r\n")
	})

	t.Run("transfer encoding", func(t *testing.T) {
		request, err := http.NewRequest(http.MethodPost, "https://example.test/", strings.NewReader("body"))
		if err != nil {
			t.Fatal(err)
		}
		request.TransferEncoding = []string{"chunked"}
		request.Header["Transfer-Encoding"] = []string{"identity"}
		assertTransportManagedFieldMatchesWire(t, request, "transfer-encoding", "chunked", "Transfer-Encoding: chunked\r\n")
	})

	t.Run("trailer declaration", func(t *testing.T) {
		request, err := http.NewRequest(http.MethodPost, "https://example.test/", strings.NewReader("body"))
		if err != nil {
			t.Fatal(err)
		}
		request.TransferEncoding = []string{"chunked"}
		request.Trailer = http.Header{"X-Zeta": nil, "X-Alpha": nil}
		request.Header["Trailer"] = []string{"X-Wrong"}
		assertTransportManagedFieldMatchesWire(t, request, "trailer", "X-Alpha,X-Zeta", "Trailer: X-Alpha,X-Zeta\r\n")
	})

	t.Run("connection close", func(t *testing.T) {
		request, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Close = true
		assertTransportManagedFieldMatchesWire(t, request, "connection", "close", "Connection: close\r\n")
	})
}

func TestSignatureBaseUsesNetHTTPTransportManagedResponseFields(t *testing.T) {
	t.Parallel()

	serverRequest := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	t.Run("transfer encoding", func(t *testing.T) {
		response := &http.Response{
			StatusCode: http.StatusOK, ProtoMajor: 1, ProtoMinor: 1,
			Header: http.Header{"Transfer-Encoding": []string{"identity"}},
			Body:   io.NopCloser(strings.NewReader("body")), ContentLength: -1,
			TransferEncoding: []string{"chunked"}, Request: serverRequest,
		}
		assertTransportManagedResponseFieldMatchesWire(t, response, "transfer-encoding", "chunked", "Transfer-Encoding: chunked\r\n")
	})

	t.Run("trailer declaration", func(t *testing.T) {
		response := &http.Response{
			StatusCode: http.StatusOK, ProtoMajor: 1, ProtoMinor: 1,
			Header: http.Header{"Trailer": []string{"X-Wrong"}},
			Body:   io.NopCloser(strings.NewReader("body")), ContentLength: -1,
			TransferEncoding: []string{"chunked"}, Trailer: http.Header{"X-Zeta": nil, "X-Alpha": nil},
			Request: serverRequest,
		}
		assertTransportManagedResponseFieldMatchesWire(t, response, "trailer", "X-Alpha,X-Zeta", "Trailer: X-Alpha,X-Zeta\r\n")
	})

	t.Run("connection close", func(t *testing.T) {
		response := &http.Response{
			StatusCode: http.StatusOK, ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header),
			Body: http.NoBody, ContentLength: 0, Close: true, Request: serverRequest,
		}
		assertTransportManagedResponseFieldMatchesWire(t, response, "connection", "close", "Connection: close\r\n")
	})

	t.Run("inbound connection identity", func(t *testing.T) {
		clientRequest, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
		if err != nil {
			t.Fatal(err)
		}
		directResponse := &http.Response{
			StatusCode: http.StatusOK, ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("body")), ContentLength: -1, Close: true, Request: clientRequest,
		}
		if _, err := CreateSignatureBase(MessageContext{Response: directResponse}, SignatureInput{
			Components: []ComponentIdentifier{{Name: "connection"}},
		}); !errors.Is(err, ErrSignatureBase) {
			t.Fatalf("CreateSignatureBase(direct response) error = %v, want ErrSignatureBase", err)
		}

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Connection", "close")
			writer.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		response, err := server.Client().Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		if !response.Close || response.Header.Get("Connection") != "" || response.Request.RequestURI != "" {
			t.Fatalf("received response close=%t header=%q request-target=%q", response.Close, response.Header.Get("Connection"), response.Request.RequestURI)
		}
		base, err := CreateSignatureBase(MessageContext{Response: response, ResponseTransport: ResponseTransportReceived}, SignatureInput{
			Components: []ComponentIdentifier{{Name: "connection"}},
		})
		if err != nil {
			t.Fatalf("CreateSignatureBase(network response) error = %v", err)
		}
		if want := "\"connection\": close\n"; !strings.HasPrefix(base, want) {
			t.Fatalf("network signature base = %q, want prefix %q", base, want)
		}
	})

	t.Run("implicit close delimited response", func(t *testing.T) {
		clientRequest, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
		if err != nil {
			t.Fatal(err)
		}
		response := &http.Response{
			StatusCode: http.StatusOK, ProtoMajor: 1, ProtoMinor: 1,
			Header: make(http.Header), Body: http.NoBody, ContentLength: -1, Close: true,
			Request: clientRequest,
		}
		if _, err := CreateSignatureBase(MessageContext{Response: response, ResponseTransport: ResponseTransportReceived}, SignatureInput{
			Components: []ComponentIdentifier{{Name: "connection"}},
		}); !errors.Is(err, ErrSignatureBase) {
			t.Fatalf("CreateSignatureBase() error = %v, want ErrSignatureBase", err)
		}
	})

	t.Run("inbound trailer order unavailable", func(t *testing.T) {
		clientRequest, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
		if err != nil {
			t.Fatal(err)
		}
		response := &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Trailer": []string{"X-Wrong"}},
			TransferEncoding: []string{"chunked"}, Trailer: http.Header{"X-A": nil, "X-B": nil},
			Request: clientRequest,
		}
		if _, err := CreateSignatureBase(MessageContext{Response: response, ResponseTransport: ResponseTransportReceived}, SignatureInput{
			Components: []ComponentIdentifier{{Name: "trailer"}},
		}); !errors.Is(err, ErrSignatureBase) {
			t.Fatalf("CreateSignatureBase() error = %v, want ErrSignatureBase", err)
		}
	})
}

func TestSignatureBaseRejectsAmbiguousCookieFieldInstances(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header["Cookie"] = []string{"a=1", "b=2"}
	if _, err := CreateSignatureBase(MessageContext{Request: request}, SignatureInput{
		Components: []ComponentIdentifier{{Name: "cookie"}},
	}); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("multi-line Cookie error = %v, want ErrSignatureBase", err)
	}

	for _, value := range []string{"a=1;b=2", "", "a=1; ", "; a=1"} {
		request.Header["Cookie"] = []string{value}
		if _, err := CreateSignatureBase(MessageContext{Request: request}, SignatureInput{
			Components: []ComponentIdentifier{{Name: "cookie"}},
		}); !errors.Is(err, ErrSignatureBase) {
			t.Fatalf("unsafe Cookie %q error = %v, want ErrSignatureBase", value, err)
		}
	}

	request.Header["Cookie"] = []string{"a=1; b=2"}
	if _, err := CreateSignatureBase(MessageContext{Request: request}, SignatureInput{
		Components: []ComponentIdentifier{{Name: "cookie"}},
	}); err != nil {
		t.Fatalf("canonical Cookie error = %v", err)
	}

	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{
		"Set-Cookie": []string{"a=1", "b=2"},
	}}
	if _, err := CreateSignatureBase(MessageContext{Response: response}, SignatureInput{
		Components: []ComponentIdentifier{{Name: "set-cookie"}},
	}); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("ordinary multi-line Set-Cookie error = %v, want ErrSignatureBase", err)
	}
	if _, err := CreateSignatureBase(MessageContext{Response: response}, SignatureInput{
		Components: []ComponentIdentifier{{Name: "set-cookie", Parameters: []Parameter{{Name: "bs", Value: true}}}},
	}); err != nil {
		t.Fatalf("binary-wrapped multi-line Set-Cookie error = %v", err)
	}
}

func TestVerifyingRoundTripperDoesNotInventCoveredContentLengthAfterBuffering(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, err := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodGet, "https://example.test/data", nil)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("payload")
	digests, err := ComputeDigests([]DigestAlgorithm{SHA256}, content)
	if err != nil {
		t.Fatal(err)
	}
	components := []ComponentIdentifier{{Name: "@status"}, {Name: "content-digest"}, {Name: "content-length"}}
	signingProfile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256}, CoveredComponents: components,
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		Lifetime: time.Minute, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	forged := &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{
			"Content-Digest": []string{digests.String()}, "Content-Length": []string{fmt.Sprint(len(content))},
		},
		ContentLength: int64(len(content)), Request: request,
	}
	signed, err := NewSigner(signingProfile).Sign(context.Background(), MessageContext{
		Response: forged, ResponseTransport: ResponseTransportReceived,
	}, "sig", SigningOptions{})
	if err != nil {
		t.Fatal(err)
	}
	received := &http.Response{
		StatusCode: http.StatusOK, Header: forged.Header.Clone(), Body: io.NopCloser(bytes.NewReader(content)),
		ContentLength: -1, Request: request,
	}
	received.Header.Del("Content-Length")
	received.Header.Set("Signature-Input", signed.SignatureInputField())
	received.Header.Set("Signature", signed.SignatureField())
	verificationProfile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256}, RequiredComponents: components,
		Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden,
		MaxAge: time.Minute, ClockSkew: time.Second, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewVerifyingRoundTripper(VerifyingRoundTripperConfig{
		Transport:               roundTripperFunc(func(*http.Request) (*http.Response, error) { return received, nil }),
		Verifier:                NewVerifier(verificationProfile),
		SelectLabel:             func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "sig", nil },
		ContentDigestAlgorithms: []DigestAlgorithm{SHA256}, MaxBufferedBytes: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response, verifyErr := transport.RoundTrip(request); response != nil || !errors.Is(verifyErr, ErrSignatureBase) {
		t.Fatalf("RoundTrip() = %#v, %v, want ErrSignatureBase", response, verifyErr)
	}
}

func assertTransportManagedFieldMatchesWire(t *testing.T, request *http.Request, field, wantBaseValue, wantWire string) {
	t.Helper()

	base, err := CreateSignatureBase(MessageContext{Request: request}, SignatureInput{
		Components: []ComponentIdentifier{{Name: field}},
	})
	if err != nil {
		t.Fatalf("CreateSignatureBase(%q) error = %v", field, err)
	}
	if want := fmt.Sprintf("\"%s\": %s\n", field, wantBaseValue); !strings.HasPrefix(base, want) {
		t.Fatalf("signature base = %q, want prefix %q", base, want)
	}

	var wire bytes.Buffer
	if err := request.Write(&wire); err != nil {
		t.Fatalf("Request.Write() error = %v", err)
	}
	if !strings.Contains(wire.String(), wantWire) {
		t.Fatalf("wire request = %q, want field %q", wire.String(), wantWire)
	}
}

func assertTransportManagedResponseFieldMatchesWire(t *testing.T, response *http.Response, field, wantBaseValue, wantWire string) {
	t.Helper()

	base, err := CreateSignatureBase(MessageContext{Response: response, ResponseTransport: ResponseTransportWrite}, SignatureInput{
		Components: []ComponentIdentifier{{Name: field}},
	})
	if err != nil {
		t.Fatalf("CreateSignatureBase(%q) error = %v", field, err)
	}
	if want := fmt.Sprintf("\"%s\": %s\n", field, wantBaseValue); !strings.HasPrefix(base, want) {
		t.Fatalf("signature base = %q, want prefix %q", base, want)
	}

	var wire bytes.Buffer
	if err := response.Write(&wire); err != nil {
		t.Fatalf("Response.Write() error = %v", err)
	}
	if !strings.Contains(wire.String(), wantWire) {
		t.Fatalf("wire response = %q, want field %q", wire.String(), wantWire)
	}
}

func TestQueryParameterUsesHTMLFormParsing(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		rawQuery    string
		encodedName string
		wantValue   string
		wantError   bool
	}{
		{name: "malformed percent remains literal", rawQuery: "a=%", encodedName: "a", wantValue: "%25"},
		{name: "each invalid UTF-8 byte is replaced", rawQuery: "%FF%FF=x", encodedName: "%EF%BF%BD%EF%BF%BD", wantValue: "x"},
		{name: "truncated UTF-8 prefix is one replacement", rawQuery: "a=%E2%82", encodedName: "a", wantValue: "%EF%BF%BD"},
		{name: "empty sequences are skipped", rawQuery: "&x=1", encodedName: "", wantError: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			value, err := queryParameter(test.rawQuery, test.encodedName, DefaultMaxSignatureBaseBytes)
			if test.wantError {
				if err == nil {
					t.Fatalf("queryParameter() = %q, want error", value)
				}
				return
			}
			if err != nil || value != test.wantValue {
				t.Fatalf("queryParameter() = %q, %v, want %q", value, err, test.wantValue)
			}
		})
	}
}

func TestSignatureBaseRejectsObsFoldOutsideHTTP1(t *testing.T) {
	t.Parallel()

	input := SignatureInput{
		Components: []ComponentIdentifier{{Name: "x-folded"}},
		Parameters: []Parameter{{Name: "created", Value: int64(1)}},
	}
	for _, major := range []int{2, 3} {
		request, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.ProtoMajor = major
		request.ProtoMinor = 0
		request.Header.Set("X-Folded", "a\r\n b")
		if _, err := CreateSignatureBase(MessageContext{Request: request}, input); !errors.Is(err, ErrSignatureBase) {
			t.Fatalf("HTTP/%d CreateSignatureBase() error = %v, want ErrSignatureBase", major, err)
		}
	}
}

func TestSignatureBaseRejectsEmptyDerivedValuesOutsideQueryParameters(t *testing.T) {
	t.Parallel()

	input := SignatureInput{
		Components: []ComponentIdentifier{{Name: "@method"}},
		Parameters: []Parameter{{Name: "created", Value: int64(1)}},
	}
	for _, method := range []string{"", "GET X", "GET\r\nX"} {
		request, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Method = method
		if _, err := CreateSignatureBase(MessageContext{Request: request}, input); !errors.Is(err, ErrSignatureBase) {
			t.Fatalf("method %q CreateSignatureBase() error = %v, want ErrSignatureBase", method, err)
		}
	}
}

func TestSignatureBaseRejectsOutputBeyondExplicitResourceLimit(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("X-Large", "0123456789")
	input := SignatureInput{Components: []ComponentIdentifier{{Name: "x-large"}}}

	_, err = CreateSignatureBase(MessageContext{
		Request:               request,
		MaxSignatureBaseBytes: 16,
	}, input)
	if !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("CreateSignatureBase() error = %v, want ErrSignatureBaseLimit", err)
	}

	context := MessageContext{Request: request}
	base, err := CreateSignatureBase(context, input)
	if err != nil {
		t.Fatalf("CreateSignatureBase(default limit) error = %v", err)
	}
	context.MaxSignatureBaseBytes = len(base)
	exact, err := CreateSignatureBase(context, input)
	if err != nil || exact != base {
		t.Fatalf("CreateSignatureBase(exact limit) = %q, %v; want %q, nil", exact, err, base)
	}

	context.MaxSignatureBaseBytes = -1
	if _, err := CreateSignatureBase(context, input); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("CreateSignatureBase(negative limit) error = %v, want ErrSignatureBaseLimit", err)
	}

	request.Header.Set("X-Large", strings.Repeat("x", DefaultMaxSignatureBaseBytes))
	context.MaxSignatureBaseBytes = 0
	if _, err := CreateSignatureBase(context, input); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("CreateSignatureBase(default overflow) error = %v, want ErrSignatureBaseLimit", err)
	}

	request.Header.Set("X-Large", strings.Repeat("x", 64))
	context.MaxSignatureBaseBytes = 64
	binaryInput := SignatureInput{Components: []ComponentIdentifier{{
		Name: "x-large", Parameters: []Parameter{{Name: "bs", Value: true}},
	}}}
	if _, err := CreateSignatureBase(context, binaryInput); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("CreateSignatureBase(base64 amplification) error = %v, want ErrSignatureBaseLimit", err)
	}

	largeParameter := SignatureInput{
		Components: []ComponentIdentifier{{Name: "@method"}},
		Parameters: []Parameter{{Name: "tag", Value: strings.Repeat("x", 128)}},
	}
	if _, err := CreateSignatureBase(context, largeParameter); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("CreateSignatureBase(parameter amplification) error = %v, want ErrSignatureBaseLimit", err)
	}
	scalarParameters := SignatureInput{
		Components: []ComponentIdentifier{{Name: "@method"}},
		Parameters: []Parameter{{Name: "created", Value: int64(1)}, {Name: "x", Value: float64(1)}},
	}
	context.MaxSignatureBaseBytes = 0
	scalarBase, err := CreateSignatureBase(context, scalarParameters)
	if err != nil {
		t.Fatalf("CreateSignatureBase(scalar parameters) error = %v", err)
	}
	context.MaxSignatureBaseBytes = len(scalarBase)
	if exact, err := CreateSignatureBase(context, scalarParameters); err != nil || exact != scalarBase {
		t.Fatalf("CreateSignatureBase(exact scalar limit) = %q, %v; want %q, nil", exact, err, scalarBase)
	}

	context.MaxSignatureBaseBytes = 64
	request.Header.Set("X-Large", strings.Repeat("\n", 128))
	if _, err := CreateSignatureBase(context, input); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("CreateSignatureBase(malformed oversized field) error = %v, want ErrSignatureBaseLimit", err)
	}
	if _, err := CreateSignatureBase(context, binaryInput); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("CreateSignatureBase(malformed oversized bs field) error = %v, want ErrSignatureBaseLimit", err)
	}
	structuredInput := SignatureInput{Components: []ComponentIdentifier{{
		Name: "x-large", Parameters: []Parameter{{Name: "sf", Value: true}},
	}}}
	context.StructuredFields = map[string]StructuredFieldType{"x-large": StructuredFieldDictionary}
	if _, err := CreateSignatureBase(context, structuredInput); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("CreateSignatureBase(malformed oversized sf field) error = %v, want ErrSignatureBaseLimit", err)
	}

	request.RequestURI = "/" + strings.Repeat("x", 128)
	derivedInput := SignatureInput{Components: []ComponentIdentifier{{Name: "@scheme"}}}
	if _, err := CreateSignatureBase(context, derivedInput); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("CreateSignatureBase(unrelated target amplification) error = %v, want ErrSignatureBaseLimit", err)
	}

	request.RequestURI = ""
	request.URL.Opaque = strings.Repeat("x", 128)
	context.ExternalRequest = &ExternalRequestContext{
		Scheme: "https", Authority: "external.example", RequestTarget: "/",
	}
	if _, err := CreateSignatureBase(context, derivedInput); err != nil {
		t.Fatalf("CreateSignatureBase(bounded external target) error = %v", err)
	}
}

func TestStrictStructuredFieldsRejectRFC9651OnlyValues(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Example", "@1")
	input := SignatureInput{
		Components: []ComponentIdentifier{{Name: "example", Parameters: []Parameter{{Name: "sf", Value: true}}}},
		Parameters: []Parameter{{Name: "created", Value: int64(1)}},
	}
	_, err = CreateSignatureBase(MessageContext{
		Request:          request,
		StructuredFields: map[string]StructuredFieldType{"example": StructuredFieldItem},
	}, input)
	if !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("CreateSignatureBase() error = %v, want ErrSignatureBase", err)
	}
}

func TestVerifierBoundsReplayOutageAndRejectsLateSuccess(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, err := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := testVerificationProfile(t, now, key)
	profile.replayTimeout = 10 * time.Millisecond
	profile.replay = replayStoreFunc(func(ctx context.Context, _ ReplayRecord) error {
		<-ctx.Done()
		return nil
	})
	request, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := ParseSignatureInputs([]string{
		`sig=("@method");created=1700000000;expires=1700000060;nonce="nonce";keyid="key";alg="hmac-sha256"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatal(err)
	}
	value, err := Sign(context.Background(), HMACSHA256, key, []byte(base), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewVerifier(profile).Verify(
		context.Background(),
		MessageContext{Request: request},
		"sig",
		inputs,
		Signatures{entries: []SignatureValue{{Label: "sig", Value: value}}},
	)
	if !verificationFailureIs(err, VerificationReplay) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Verify() error = %v, want replay deadline", err)
	}
}

func TestMemoryReplayStoreDoesNotCallClockUnderLock(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	store, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 2, MaxTTL: time.Minute, MaxKeyIDBytes: 64, MaxNonceBytes: 64,
		Now: func() time.Time {
			if calls.Add(1) == 1 {
				close(entered)
				<-release
			}
			return now
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- store.Consume(context.Background(), ReplayRecord{KeyID: "key", Nonce: "one", ExpiresAt: now.Add(time.Minute)})
	}()
	<-entered
	if !store.mu.TryLock() {
		close(release)
		<-firstDone
		t.Fatal("Consume() held replay state while invoking the caller clock")
	}
	store.mu.Unlock()
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Consume() error = %v", err)
	}
}

func TestVerifierRejectsSignatureCreatedOutsideKeyLifetime(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, err := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := testVerificationProfile(t, now, key)
	profile.maxAge = time.Hour
	request, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := ParseSignatureInputs([]string{
		`sig=("@method");created=1699998200;expires=1700000060;nonce="nonce";keyid="key";alg="hmac-sha256"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	base, err := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	if err != nil {
		t.Fatal(err)
	}
	value, err := Sign(context.Background(), HMACSHA256, key, []byte(base), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewVerifier(profile).Verify(
		context.Background(),
		MessageContext{Request: request},
		"sig",
		inputs,
		Signatures{entries: []SignatureValue{{Label: "sig", Value: value}}},
	)
	if !verificationFailureIs(err, VerificationKey) {
		t.Fatalf("Verify() error = %v, want key failure", err)
	}
}

func TestVerifierRejectsUnresolvableBaseBeforeKeyResolution(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, err := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := testVerificationProfile(t, now, key)
	var resolutions atomic.Int32
	profile.resolver = resolverFunc(func(context.Context, string) (ResolvedKey, error) {
		resolutions.Add(1)
		return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Minute), FreshUntil: now.Add(time.Minute)}, nil
	})
	request, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := ParseSignatureInputs([]string{
		`sig=("@method" "missing");created=1700000000;expires=1700000060;nonce="nonce";keyid="key";alg="hmac-sha256"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewVerifier(profile).Verify(
		context.Background(),
		MessageContext{Request: request},
		"sig",
		inputs,
		Signatures{entries: []SignatureValue{{Label: "sig", Value: []byte("value")}}},
	)
	if !verificationFailureIs(err, VerificationBase) {
		t.Fatalf("Verify() error = %v, want base failure", err)
	}
	if got := resolutions.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
}

type invalidCountReadCloser struct{ count int }

func (reader invalidCountReadCloser) Read([]byte) (int, error) { return reader.count, nil }
func (invalidCountReadCloser) Close() error                    { return nil }

func TestFieldResolutionHandlesNoncanonicalHeaderKeysWithoutConfusion(t *testing.T) {
	t.Parallel()

	input := SignatureInput{
		Components: []ComponentIdentifier{{Name: "x-test"}},
		Parameters: []Parameter{{Name: "created", Value: int64(1)}},
	}
	request, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header = http.Header{"x-test": []string{"lower"}}
	base, err := CreateSignatureBase(MessageContext{Request: request}, input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(base, "\"x-test\": lower\n") {
		t.Fatalf("signature base = %q", base)
	}

	request.Header["X-Test"] = []string{"canonical"}
	if _, err := CreateSignatureBase(MessageContext{Request: request}, input); !errors.Is(err, ErrSignatureBase) {
		t.Fatalf("case-colliding CreateSignatureBase() error = %v, want ErrSignatureBase", err)
	}
}

var _ io.ReadCloser = invalidCountReadCloser{}

type replayStoreFunc func(context.Context, ReplayRecord) error

func (consume replayStoreFunc) Consume(ctx context.Context, record ReplayRecord) error {
	return consume(ctx, record)
}
