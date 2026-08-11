package httpsignature

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

type gatedReadBody struct {
	started chan struct{}
	release chan struct{}
}

func (body *gatedReadBody) Read(buffer []byte) (int, error) {
	close(body.started)
	<-body.release
	buffer[0] = 'x'
	return 1, nil
}

func (*gatedReadBody) Close() error { return nil }

func assertBodyIntegrationConstructorsRejectEachIndependentField(t *testing.T) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil })
	mapError := func(http.ResponseWriter, *http.Request, error) {}

	bufferedTransport := BufferedContentDigestRoundTripperConfig{Transport: transport, Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1}
	if _, err := NewBufferedContentDigestRoundTripper(bufferedTransport); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BufferedContentDigestRoundTripperConfig){
		func(config *BufferedContentDigestRoundTripperConfig) { config.Transport = nil },
		func(config *BufferedContentDigestRoundTripperConfig) { config.MaxBytes = 0 },
	} {
		config := bufferedTransport
		mutate(&config)
		if _, err := NewBufferedContentDigestRoundTripper(config); !errors.Is(err, ErrInvalidBodyIntegration) {
			t.Fatalf("buffered transport error = %v", err)
		}
	}

	bufferedMiddleware := BufferedContentDigestVerificationMiddlewareConfig{RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1, MapError: mapError}
	if _, err := NewBufferedContentDigestVerificationMiddleware(bufferedMiddleware); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BufferedContentDigestVerificationMiddlewareConfig){
		func(config *BufferedContentDigestVerificationMiddlewareConfig) { config.MaxBytes = 0 },
		func(config *BufferedContentDigestVerificationMiddlewareConfig) { config.MapError = nil },
	} {
		config := bufferedMiddleware
		mutate(&config)
		if _, err := NewBufferedContentDigestVerificationMiddleware(config); !errors.Is(err, ErrInvalidBodyIntegration) {
			t.Fatalf("buffered middleware error = %v", err)
		}
	}

	trailerSigner := NewSigner(testTrailerSigningProfile(t, now, key))
	trailerTransport := TrailerSigningRoundTripperConfig{
		Transport: transport, Signer: trailerSigner, Label: "trail", Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1,
		Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
	}
	if _, err := NewTrailerSigningRoundTripper(trailerTransport); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*TrailerSigningRoundTripperConfig){
		func(config *TrailerSigningRoundTripperConfig) { config.Transport = nil },
		func(config *TrailerSigningRoundTripperConfig) { config.Signer = nil },
		func(config *TrailerSigningRoundTripperConfig) { config.Signer = NewSigner(nil) },
		func(config *TrailerSigningRoundTripperConfig) { config.Label = "Bad" },
		func(config *TrailerSigningRoundTripperConfig) { config.MaxBytes = 0 },
		func(config *TrailerSigningRoundTripperConfig) { config.Options = nil },
	} {
		config := trailerTransport
		mutate(&config)
		if _, err := NewTrailerSigningRoundTripper(config); !errors.Is(err, ErrInvalidBodyIntegration) {
			t.Fatalf("trailer transport error = %v", err)
		}
	}

	responseTrailer := TrailerResponseSigningMiddlewareConfig{
		Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail", Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1,
		Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil }, ReportError: func(*http.Request, error) {},
	}
	if _, err := NewTrailerResponseSigningMiddleware(responseTrailer); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*TrailerResponseSigningMiddlewareConfig){
		func(config *TrailerResponseSigningMiddlewareConfig) { config.Signer = nil },
		func(config *TrailerResponseSigningMiddlewareConfig) { config.Signer = NewSigner(nil) },
		func(config *TrailerResponseSigningMiddlewareConfig) { config.Label = "Bad" },
		func(config *TrailerResponseSigningMiddlewareConfig) { config.MaxBytes = 0 },
		func(config *TrailerResponseSigningMiddlewareConfig) { config.Options = nil },
		func(config *TrailerResponseSigningMiddlewareConfig) { config.ReportError = nil },
	} {
		config := responseTrailer
		mutate(&config)
		if _, err := NewTrailerResponseSigningMiddleware(config); !errors.Is(err, ErrInvalidBodyIntegration) {
			t.Fatalf("response trailer error = %v", err)
		}
	}

	verifier := NewVerifier(testTrailerVerificationProfile(t, now, key))
	verificationMiddleware := BufferedTrailerVerificationMiddlewareConfig{
		Verifier: verifier, SelectLabel: func(*http.Request, SignatureInputs, Signatures) (string, error) { return "trail", nil },
		RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1, MapError: mapError,
	}
	if _, err := NewBufferedTrailerVerificationMiddleware(verificationMiddleware); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BufferedTrailerVerificationMiddlewareConfig){
		func(config *BufferedTrailerVerificationMiddlewareConfig) { config.Verifier = nil },
		func(config *BufferedTrailerVerificationMiddlewareConfig) { config.Verifier = NewVerifier(nil) },
		func(config *BufferedTrailerVerificationMiddlewareConfig) { config.SelectLabel = nil },
		func(config *BufferedTrailerVerificationMiddlewareConfig) { config.MaxBytes = 0 },
		func(config *BufferedTrailerVerificationMiddlewareConfig) { config.MapError = nil },
	} {
		config := verificationMiddleware
		mutate(&config)
		if _, err := NewBufferedTrailerVerificationMiddleware(config); !errors.Is(err, ErrInvalidBodyIntegration) {
			t.Fatalf("verification middleware error = %v", err)
		}
	}

	verificationTransport := BufferedTrailerVerifyingRoundTripperConfig{
		Transport: transport, Verifier: verifier,
		SelectLabel:        func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "trail", nil },
		RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1,
	}
	if _, err := NewBufferedTrailerVerifyingRoundTripper(verificationTransport); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BufferedTrailerVerifyingRoundTripperConfig){
		func(config *BufferedTrailerVerifyingRoundTripperConfig) { config.Transport = nil },
		func(config *BufferedTrailerVerifyingRoundTripperConfig) { config.Verifier = nil },
		func(config *BufferedTrailerVerifyingRoundTripperConfig) { config.Verifier = NewVerifier(nil) },
		func(config *BufferedTrailerVerifyingRoundTripperConfig) { config.SelectLabel = nil },
		func(config *BufferedTrailerVerifyingRoundTripperConfig) { config.MaxBytes = 0 },
	} {
		config := verificationTransport
		mutate(&config)
		if _, err := NewBufferedTrailerVerifyingRoundTripper(config); !errors.Is(err, ErrInvalidBodyIntegration) {
			t.Fatalf("verification transport error = %v", err)
		}
	}
}

func TestBodyIntegrationConstructorsRejectEachIndependentField(t *testing.T) {
	t.Parallel()
	assertBodyIntegrationConstructorsRejectEachIndependentField(t)
}

func TestBodyIntegrationExactByteAndStatusBoundaries(t *testing.T) {
	t.Parallel()

	body := &countingBody{reader: iotest.OneByteReader(strings.NewReader("ab"))}
	content, err := readBoundedAndClose(context.Background(), body, 2)
	if err != nil || string(content) != "ab" || body.closed != 1 {
		t.Fatalf("exact bounded read = %q, %v, closes=%d", content, err, body.closed)
	}
	body = &countingBody{reader: iotest.OneByteReader(strings.NewReader("abc"))}
	if _, err := readBoundedAndClose(context.Background(), body, 2); !errors.Is(err, ErrBodyTooLarge) || body.closed != 1 {
		t.Fatalf("over-bound read = %v, closes=%d", err, body.closed)
	}

	finalized := 0
	stream := &trailerSigningBody{
		body: io.NopCloser(iotest.OneByteReader(strings.NewReader("ab"))), ctx: context.Background(), maxBytes: 2,
		finalize: func(DigestField) error { finalized++; return nil },
	}
	if content, err := io.ReadAll(stream); err != nil || string(content) != "ab" || stream.written != 2 || finalized != 1 {
		t.Fatalf("exact trailer stream = %q, %v, written=%d, finalized=%d", content, err, stream.written, finalized)
	}
	stream = &trailerSigningBody{
		body: io.NopCloser(iotest.OneByteReader(strings.NewReader("ab"))), ctx: context.Background(), maxBytes: 1,
		finalize: func(DigestField) error { return nil },
	}
	if _, err := io.ReadAll(stream); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("over-bound trailer stream error = %v", err)
	}

	for _, status := range []int{100, 199} {
		writer := &trailerResponseWriter{ResponseWriter: httptest.NewRecorder(), request: httptest.NewRequest(http.MethodGet, "https://example.com", nil), maxBytes: 2}
		writer.WriteHeader(status)
		if writer.status != 0 {
			t.Fatalf("informational status %d committed as %d", status, writer.status)
		}
	}
	for _, status := range []int{200, 999} {
		writer := &trailerResponseWriter{ResponseWriter: httptest.NewRecorder(), request: httptest.NewRequest(http.MethodGet, "https://example.com", nil), maxBytes: 2}
		writer.WriteHeader(status)
		if writer.status != status {
			t.Fatalf("status %d committed as %d", status, writer.status)
		}
	}
	for _, test := range []struct {
		status  int
		allowed bool
	}{{199, false}, {200, true}, {204, false}, {205, false}, {304, false}} {
		if got := responseBodyAllowed(test.status); got != test.allowed {
			t.Fatalf("responseBodyAllowed(%d) = %v", test.status, got)
		}
	}
	writer := &trailerResponseWriter{ResponseWriter: httptest.NewRecorder(), request: httptest.NewRequest(http.MethodGet, "https://example.com", nil), maxBytes: 2}
	if count, err := writer.Write([]byte("a")); err != nil || count != 1 || writer.written != 1 {
		t.Fatalf("first response stream = %d, %v, written=%d", count, err, writer.written)
	}
	if count, err := writer.Write([]byte("b")); err != nil || count != 1 || writer.written != 2 {
		t.Fatalf("exact response stream = %d, %v, written=%d", count, err, writer.written)
	}
	if count, err := writer.Write([]byte("c")); count != 0 || !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("over-bound response stream = %d, %v", count, err)
	}
}

type failingReadBody struct {
	closed int
}

func (*failingReadBody) Read([]byte) (int, error) { return 0, errors.New("private backend detail") }
func (body *failingReadBody) Close() error {
	body.closed++
	return nil
}

type countingBody struct {
	reader io.Reader
	closed int
}

func (body *countingBody) Read(buffer []byte) (int, error) { return body.reader.Read(buffer) }
func (body *countingBody) Close() error {
	body.closed++
	return nil
}

type failingResponseWriter struct {
	header http.Header
}

func (writer *failingResponseWriter) Header() http.Header { return writer.header }
func (*failingResponseWriter) WriteHeader(int)            {}
func (*failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("private writer detail")
}

func TestBufferedDigestAdapterBoundaryFailures(t *testing.T) {
	t.Parallel()

	baseTransport := roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil })
	if _, err := NewBufferedContentDigestRoundTripper(BufferedContentDigestRoundTripperConfig{
		Transport: baseTransport, Algorithms: []DigestAlgorithm{"unsupported"}, MaxBytes: 1,
	}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("unsupported constructor error = %v", err)
	}
	if _, err := NewBufferedContentDigestVerificationMiddleware(BufferedContentDigestVerificationMiddlewareConfig{
		RequiredAlgorithms: []DigestAlgorithm{"unsupported"}, MaxBytes: 1,
		MapError: func(http.ResponseWriter, *http.Request, error) {},
	}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("unsupported middleware constructor error = %v", err)
	}

	var nilTransport *BufferedContentDigestRoundTripper
	if _, err := nilTransport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", nil)); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("nil receiver error = %v", err)
	}
	transport, err := NewBufferedContentDigestRoundTripper(BufferedContentDigestRoundTripperConfig{
		Transport: baseTransport, Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(nil); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("nil request error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://example.com", &failingReadBody{})
	if _, err := transport.RoundTrip(request); !errors.Is(err, ErrBodyRead) || strings.Contains(err.Error(), "private") {
		t.Fatalf("read error = %v", err)
	}
	invalidTransport := *transport
	invalidTransport.algorithms = []DigestAlgorithm{"unsupported"}
	if _, err := invalidTransport.RoundTrip(httptest.NewRequest(http.MethodPost, "https://example.com", strings.NewReader("x"))); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("invalid internal digest algorithm error = %v", err)
	}
	nilHeaderRequest := httptest.NewRequest(http.MethodPost, "https://example.com", strings.NewReader("x"))
	nilHeaderRequest.Header = nil
	delegating, _ := NewBufferedContentDigestRoundTripper(BufferedContentDigestRoundTripperConfig{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			replay, replayErr := request.GetBody()
			if replayErr != nil {
				t.Fatal(replayErr)
			}
			defer func() {
				if closeErr := replay.Close(); closeErr != nil {
					t.Errorf("replay Close() error = %v", closeErr)
				}
			}()
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
		}), Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1,
	})
	if _, err := delegating.RoundTrip(nilHeaderRequest); err != nil {
		t.Fatalf("nil header request error = %v", err)
	}

	for _, test := range []struct {
		name   string
		field  string
		body   io.Reader
		limit  int64
		cancel bool
		want   error
	}{
		{name: "missing", body: strings.NewReader("x"), limit: 1, want: ErrInvalidDigestField},
		{name: "malformed", field: "sha-256=:bad", body: strings.NewReader("x"), limit: 1, want: ErrInvalidDigestField},
		{name: "too large", field: "sha-256=:LXEWQrcmsEQBYnyp+6wy9chTD7GQPMTbAiWHF5IaSIE=:", body: strings.NewReader("xx"), limit: 1, want: ErrBodyTooLarge},
		{name: "cancelled", field: "sha-256=:LXEWQrcmsEQBYnyp+6wy9chTD7GQPMTbAiWHF5IaSIE=:", body: strings.NewReader("x"), limit: 1, cancel: true, want: context.Canceled},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var got error
			middleware, constructorErr := NewBufferedContentDigestVerificationMiddleware(BufferedContentDigestVerificationMiddlewareConfig{
				RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: test.limit,
				MapError: func(_ http.ResponseWriter, _ *http.Request, err error) { got = err },
			})
			if constructorErr != nil {
				t.Fatal(constructorErr)
			}
			ctx := context.Background()
			if test.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			request := httptest.NewRequest(http.MethodPost, "https://example.com", test.body).WithContext(ctx)
			if test.field != "" {
				request.Header.Set("Content-Digest", test.field)
			}
			middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next called") })).ServeHTTP(httptest.NewRecorder(), request)
			if !errors.Is(got, test.want) {
				t.Fatalf("mapped error = %v, want %v", got, test.want)
			}
		})
	}

	var got error
	middleware, _ := NewBufferedContentDigestVerificationMiddleware(BufferedContentDigestVerificationMiddlewareConfig{
		RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1,
		MapError: func(_ http.ResponseWriter, _ *http.Request, err error) { got = err },
	})
	middleware(nil).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "https://example.com", nil))
	if !errors.Is(got, ErrInvalidBodyIntegration) {
		t.Fatalf("nil next error = %v", got)
	}
	got = nil
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), nil)
	if !errors.Is(got, ErrInvalidBodyIntegration) {
		t.Fatalf("nil request error = %v", got)
	}
}

func TestReadBoundedAndCloseBoundaryFailures(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		ctx  context.Context
		body io.ReadCloser
		max  int64
		want error
	}{
		{name: "nil context", body: io.NopCloser(strings.NewReader("x")), max: 1, want: ErrInvalidBodyIntegration},
		{name: "invalid limit", ctx: context.Background(), body: io.NopCloser(strings.NewReader("x")), want: ErrInvalidBodyIntegration},
		{name: "read failure", ctx: context.Background(), body: &failingReadBody{}, max: 1, want: ErrBodyRead},
		{name: "zero progress", ctx: context.Background(), body: zeroProgressBody{}, max: 1, want: ErrBodyRead},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, err := readBoundedAndClose(test.ctx, test.body, test.max)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readBoundedAndClose(ctx, io.NopCloser(strings.NewReader("x")), 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error = %v", err)
	}
}

func TestTrailerSigningBodyBoundaryFailures(t *testing.T) {
	t.Parallel()

	sha512Hash, err := newDigestWriter(SHA512)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newDigestWriter("unsupported"); !errors.Is(err, ErrUnsupportedDigestAlgorithm) {
		t.Fatalf("unsupported digest writer error = %v", err)
	}
	body := &trailerSigningBody{
		body: io.NopCloser(strings.NewReader("x")), ctx: context.Background(), maxBytes: 1,
		writers:  []digestWriter{{algorithm: SHA512, hash: sha512Hash}},
		finalize: func(DigestField) error { return errors.New("finalize") },
	}
	if count, err := body.Read(make([]byte, 1)); count != 1 || err != nil {
		t.Fatalf("content read = %d, %v", count, err)
	}
	if count, err := body.Read(make([]byte, 1)); count != 0 || err == nil || err.Error() != "finalize" {
		t.Fatalf("finalizing read = %d, %v", count, err)
	}
	if count, err := body.Read(make([]byte, 1)); count != 0 || err == nil || err.Error() != "finalize" {
		t.Fatalf("finished read = %d, %v", count, err)
	}
	successful := &trailerSigningBody{
		body: io.NopCloser(strings.NewReader("")), ctx: context.Background(), maxBytes: 1,
		finalize: func(DigestField) error { return nil },
	}
	if count, err := successful.Read(make([]byte, 1)); count != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("successful finalization = %d, %v", count, err)
	}
	if count, err := successful.Read(make([]byte, 1)); count != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("completed successful read = %d, %v", count, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := &trailerSigningBody{body: io.NopCloser(strings.NewReader("x")), ctx: ctx, maxBytes: 1}
	if _, err := cancelled.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error = %v", err)
	}
	large := &trailerSigningBody{body: io.NopCloser(strings.NewReader("xx")), ctx: context.Background(), maxBytes: 1}
	if _, err := large.Read(make([]byte, 2)); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("large read error = %v", err)
	}
	closer := &countingBody{reader: strings.NewReader("")}
	if err := (&trailerSigningBody{body: closer}).Close(); err != nil || closer.closed != 1 {
		t.Fatalf("Close() = %v, count = %d", err, closer.closed)
	}
}

func TestTrailerSigningBodySerializesConcurrentTerminalState(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		complete func(*trailerSigningBody)
		want     error
	}{
		{name: "close failure state", complete: func(body *trailerSigningBody) { _ = body.Close() }, want: ErrBodyRead},
		{name: "successful state", complete: func(body *trailerSigningBody) { body.complete(nil) }, want: io.EOF},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			underlying := &gatedReadBody{started: make(chan struct{}), release: make(chan struct{})}
			body := &trailerSigningBody{body: underlying, ctx: context.Background(), maxBytes: 1}
			result := make(chan error, 1)
			go func() {
				_, err := body.Read(make([]byte, 1))
				result <- err
			}()
			<-underlying.started
			test.complete(body)
			close(underlying.release)
			if err := <-result; !errors.Is(err, test.want) {
				t.Fatalf("Read() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestRequestTrailerNormalizationAndNameIdentity(t *testing.T) {
	t.Parallel()

	for _, header := range []http.Header{
		{"Content-Length": []string{"1"}},
		{"X-Final": []string{"one"}, "x-final": []string{"two"}},
	} {
		if _, err := normalizeTrailerFields(header); err == nil {
			t.Fatalf("normalizeTrailerFields(%#v) succeeded", header)
		}
	}
	names := applicationTrailerNames(http.Header{
		"Content-Digest": nil, "Signature-Input": nil, "Signature": nil, "X-Final": nil,
	})
	if len(names) != 1 {
		t.Fatalf("application trailer names = %#v, want only X-Final", names)
	}
	if sameTrailerNames(map[string]struct{}{"X-Final": {}}, map[string]struct{}{"X-Other": {}}) {
		t.Fatal("different equal-sized trailer name sets matched")
	}
}

func TestTrailerProfilesRequireTrailerDigestCoverage(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	signing, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256}, CoveredComponents: []ComponentIdentifier{{Name: "@method"}},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden, Lifetime: time.Minute, ResolveTimeout: time.Second,
		Now: func() time.Time { return now }, Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if signingProfileCoversTrailerDigest(signing) {
		t.Fatal("profile without trailer digest reported coverage")
	}
	if _, err := NewTrailerSigningRoundTripper(TrailerSigningRoundTripperConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil }), Signer: NewSigner(signing),
		Label: "sig", Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1,
		Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
	}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("request trailer constructor error = %v", err)
	}
	if _, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{
		Signer: NewSigner(signing), Label: "sig", Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1,
		Options:     func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		ReportError: func(*http.Request, error) {},
	}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("response trailer constructor error = %v", err)
	}
}

func TestTrailerResponseWriterBoundaryFailures(t *testing.T) {
	t.Parallel()

	underlying := httptest.NewRecorder()
	stream := &trailerResponseWriter{ResponseWriter: underlying, request: httptest.NewRequest(http.MethodGet, "https://example.com", nil), maxBytes: 1}
	if stream.Unwrap() != underlying {
		t.Fatal("Unwrap() did not return underlying writer")
	}
	stream.Header().Set("Signature", "sig=:AA==:")
	stream.WriteHeader(http.StatusOK)
	if !errors.Is(stream.failure, ErrExistingSignatures) {
		t.Fatalf("existing signature failure = %v", stream.failure)
	}
	if count, err := stream.Write([]byte("x")); count != 0 || !errors.Is(err, ErrExistingSignatures) {
		t.Fatalf("write after failure = %d, %v", count, err)
	}
	writeDetectsHeader := &trailerResponseWriter{ResponseWriter: httptest.NewRecorder(), request: httptest.NewRequest(http.MethodGet, "https://example.com", nil), maxBytes: 1}
	writeDetectsHeader.Header().Set("Signature", "sig=:AA==:")
	if count, err := writeDetectsHeader.Write([]byte("x")); count != 0 || !errors.Is(err, ErrExistingSignatures) {
		t.Fatalf("implicit header write = %d, %v", count, err)
	}

	large := &trailerResponseWriter{ResponseWriter: httptest.NewRecorder(), request: httptest.NewRequest(http.MethodGet, "https://example.com", nil), maxBytes: 1}
	if count, err := large.Write([]byte("xx")); count != 0 || !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("large write = %d, %v", count, err)
	}
	failing := &trailerResponseWriter{ResponseWriter: &failingResponseWriter{header: make(http.Header)}, request: httptest.NewRequest(http.MethodGet, "https://example.com", nil), maxBytes: 1}
	if _, err := failing.Write([]byte("x")); err == nil || !errors.Is(failing.failure, ErrBodyRead) {
		t.Fatalf("writer failure = %v, returned = %v", failing.failure, err)
	}

	for _, method := range []string{http.MethodHead, http.MethodGet} {
		method := method
		t.Run(method, func(t *testing.T) {
			request := httptest.NewRequest(method, "https://example.com", nil)
			writer := &trailerResponseWriter{ResponseWriter: httptest.NewRecorder(), request: request, maxBytes: 0}
			if method == http.MethodGet {
				writer.status = http.StatusNoContent
			}
			if count, err := writer.Write([]byte("x")); count != 1 || err != nil || writer.written != 0 {
				t.Fatalf("bodyless write = %d, %v, hashed=%d", count, err, writer.written)
			}
		})
	}
}

func signedTrailerRequest(t *testing.T, now time.Time, key HMACKey, content string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "https://example.com/upload", nil)
	field, err := ComputeDigests([]DigestAlgorithm{SHA256}, []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	request.Trailer = http.Header{"Content-Digest": []string{field.String()}}
	signed, err := NewSigner(testTrailerSigningProfile(t, now, key)).Sign(context.Background(), MessageContext{Request: request}, "trail", SigningOptions{})
	if err != nil {
		t.Fatal(err)
	}
	values := http.Header{
		"Content-Digest":  []string{field.String()},
		"Signature-Input": []string{signed.SignatureInputField()},
		"Signature":       []string{signed.SignatureField()},
	}
	request.Trailer = http.Header{"Content-Digest": nil, "Signature-Input": nil, "Signature": nil}
	request.Body = &trailerPopulatingBody{reader: strings.NewReader(content), trailer: request.Trailer, values: values}
	return request
}

func TestBufferedTrailerVerificationMiddlewareRejectsEachUntrustedBoundary(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	for _, test := range []struct {
		name      string
		configure func(*BufferedTrailerVerificationMiddlewareConfig)
		mutate    func(*http.Request)
		want      error
	}{
		{name: "body read", mutate: func(request *http.Request) { request.Body = &failingReadBody{} }, want: ErrBodyRead},
		{name: "digest missing", mutate: func(request *http.Request) { request.Trailer["Content-Digest"] = nil }, want: ErrInvalidDigestField},
		{name: "digest mismatch", mutate: func(request *http.Request) {
			request.Trailer.Set("Content-Digest", "sha-256=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:")
		}, want: ErrDigestMismatch},
		{name: "input malformed", mutate: func(request *http.Request) { request.Trailer.Set("Signature-Input", "bad") }, want: ErrInvalidSignatureInput},
		{name: "signature malformed", mutate: func(request *http.Request) { request.Trailer.Set("Signature", "bad") }, want: ErrInvalidSignature},
		{name: "selector error", configure: func(config *BufferedTrailerVerificationMiddlewareConfig) {
			config.SelectLabel = func(*http.Request, SignatureInputs, Signatures) (string, error) { return "", errors.New("private") }
		}, want: ErrInvalidHTTPIntegration},
		{name: "selector error with valid label", configure: func(config *BufferedTrailerVerificationMiddlewareConfig) {
			config.SelectLabel = func(*http.Request, SignatureInputs, Signatures) (string, error) {
				return "trail", errors.New("private")
			}
		}, want: ErrInvalidHTTPIntegration},
		{name: "selector invalid", configure: func(config *BufferedTrailerVerificationMiddlewareConfig) {
			config.SelectLabel = func(*http.Request, SignatureInputs, Signatures) (string, error) { return "Not Valid", nil }
		}, want: ErrInvalidHTTPIntegration},
		{name: "external error", configure: func(config *BufferedTrailerVerificationMiddlewareConfig) {
			config.ExternalContext = func(context.Context, *http.Request) (*ExternalRequestContext, error) {
				return nil, errors.New("private")
			}
		}, want: ErrInvalidHTTPIntegration},
		{name: "signature mismatch", mutate: func(request *http.Request) {
			request.Trailer.Set("Signature", "trail=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:")
		}, want: ErrInvalidSignatureValue},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			request := signedTrailerRequest(t, now, key, "payload")
			config := BufferedTrailerVerificationMiddlewareConfig{
				Verifier:           NewVerifier(testTrailerVerificationProfile(t, now, key)),
				SelectLabel:        func(*http.Request, SignatureInputs, Signatures) (string, error) { return "trail", nil },
				RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 8,
			}
			var got error
			config.MapError = func(_ http.ResponseWriter, _ *http.Request, err error) { got = err }
			if test.configure != nil {
				test.configure(&config)
			}
			middleware, err := NewBufferedTrailerVerificationMiddleware(config)
			if err != nil {
				t.Fatal(err)
			}
			if test.mutate != nil {
				original := request.Body.(*trailerPopulatingBody)
				test.mutate(request)
				if populated, ok := request.Body.(*trailerPopulatingBody); ok && populated == original {
					test.mutate(&http.Request{Trailer: populated.values})
				}
			}
			middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next called") })).ServeHTTP(httptest.NewRecorder(), request)
			if !errors.Is(got, test.want) {
				t.Fatalf("mapped error = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTrailerVerificationConstructorsRejectIncompletePolicies(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	verifier := NewVerifier(testTrailerVerificationProfile(t, now, key))
	if _, err := NewBufferedTrailerVerificationMiddleware(BufferedTrailerVerificationMiddlewareConfig{
		Verifier: verifier, SelectLabel: func(*http.Request, SignatureInputs, Signatures) (string, error) { return "trail", nil },
		RequiredAlgorithms: []DigestAlgorithm{"unsupported"}, MaxBytes: 1, MapError: func(http.ResponseWriter, *http.Request, error) {},
	}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("middleware unsupported algorithm error = %v", err)
	}
	if _, err := NewBufferedTrailerVerifyingRoundTripper(BufferedTrailerVerifyingRoundTripperConfig{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil }), Verifier: verifier,
		SelectLabel:        func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "trail", nil },
		RequiredAlgorithms: []DigestAlgorithm{"unsupported"}, MaxBytes: 1,
	}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("transport unsupported algorithm error = %v", err)
	}
	if _, err := NewBufferedTrailerVerificationMiddleware(BufferedTrailerVerificationMiddlewareConfig{}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("empty middleware error = %v", err)
	}
	if _, err := NewBufferedTrailerVerifyingRoundTripper(BufferedTrailerVerifyingRoundTripperConfig{}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("empty transport error = %v", err)
	}
	if !verificationProfileCoversTrailerDigest(verifier.profile) {
		t.Fatal("trailer verification profile did not report required coverage")
	}
	var got error
	middleware, err := NewBufferedTrailerVerificationMiddleware(BufferedTrailerVerificationMiddlewareConfig{
		Verifier: verifier, SelectLabel: func(*http.Request, SignatureInputs, Signatures) (string, error) { return "trail", nil },
		RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1,
		MapError: func(_ http.ResponseWriter, _ *http.Request, err error) { got = err },
	})
	if err != nil {
		t.Fatal(err)
	}
	middleware(nil).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "https://example.com", nil))
	if !errors.Is(got, ErrInvalidBodyIntegration) {
		t.Fatalf("nil next error = %v", got)
	}
	got = nil
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), nil)
	if !errors.Is(got, ErrInvalidBodyIntegration) {
		t.Fatalf("nil request error = %v", got)
	}
	withoutTrailer := *verifier.profile
	withoutTrailer.requiredComponents = []ComponentIdentifier{{Name: "@method"}}
	if verificationProfileCoversTrailerDigest(&withoutTrailer) {
		t.Fatal("profile without trailer digest reported coverage")
	}
}

func signedTrailerResponse(t *testing.T, now time.Time, key HMACKey, request *http.Request, content string) *http.Response {
	t.Helper()
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Trailer: make(http.Header), Request: request}
	field, err := ComputeDigests([]DigestAlgorithm{SHA256}, []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	response.Trailer.Set("Content-Digest", field.String())
	signed, err := NewSigner(testResponseTrailerSigningProfile(t, now, key)).Sign(context.Background(), MessageContext{Response: response, RelatedRequest: request}, "trail", SigningOptions{})
	if err != nil {
		t.Fatal(err)
	}
	values := http.Header{"Content-Digest": []string{field.String()}, "Signature-Input": []string{signed.SignatureInputField()}, "Signature": []string{signed.SignatureField()}}
	response.Trailer = http.Header{"Content-Digest": nil, "Signature-Input": nil, "Signature": nil}
	response.Body = &trailerPopulatingBody{reader: strings.NewReader(content), trailer: response.Trailer, values: values}
	return response
}

func TestBufferedTrailerVerifyingRoundTripperRejectsEachUntrustedBoundary(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request := httptest.NewRequest(http.MethodGet, "https://example.com/data", nil)
	backendErr := errors.New("backend")
	backend := roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, backendErr })
	transport := &BufferedTrailerVerifyingRoundTripper{transport: backend}
	if response, err := transport.RoundTrip(request); response != nil || !errors.Is(err, backendErr) {
		t.Fatalf("backend result = %#v, %v", response, err)
	}
	transport.transport = roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil })
	if _, err := transport.RoundTrip(request); !errors.Is(err, ErrHTTPIntegrationVerification) {
		t.Fatalf("nil response error = %v", err)
	}
	var nilTransport *BufferedTrailerVerifyingRoundTripper
	if _, err := nilTransport.RoundTrip(request); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("nil receiver error = %v", err)
	}
	if _, err := transport.RoundTrip(nil); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("nil request error = %v", err)
	}

	for _, test := range []struct {
		name      string
		configure func(*BufferedTrailerVerifyingRoundTripperConfig)
		mutate    func(*http.Response)
		want      error
	}{
		{name: "body read", mutate: func(response *http.Response) { response.Body = &failingReadBody{} }, want: ErrBodyRead},
		{name: "body too large", configure: func(config *BufferedTrailerVerifyingRoundTripperConfig) { config.MaxBytes = 1 }, want: ErrBodyTooLarge},
		{name: "digest missing", mutate: func(response *http.Response) { response.Trailer["Content-Digest"] = nil }, want: ErrInvalidDigestField},
		{name: "digest mismatch", mutate: func(response *http.Response) {
			response.Trailer.Set("Content-Digest", "sha-256=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:")
		}, want: ErrDigestMismatch},
		{name: "input malformed", mutate: func(response *http.Response) { response.Trailer.Set("Signature-Input", "bad") }, want: ErrInvalidSignatureInput},
		{name: "signature malformed", mutate: func(response *http.Response) { response.Trailer.Set("Signature", "bad") }, want: ErrInvalidSignature},
		{name: "selector error", configure: func(config *BufferedTrailerVerifyingRoundTripperConfig) {
			config.SelectLabel = func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) {
				return "", errors.New("private")
			}
		}, want: ErrInvalidHTTPIntegration},
		{name: "selector error with valid label", configure: func(config *BufferedTrailerVerifyingRoundTripperConfig) {
			config.SelectLabel = func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) {
				return "trail", errors.New("private")
			}
		}, want: ErrInvalidHTTPIntegration},
		{name: "external error", configure: func(config *BufferedTrailerVerifyingRoundTripperConfig) {
			config.ExternalContext = func(context.Context, *http.Request, *http.Response) (*ExternalRequestContext, error) {
				return nil, errors.New("private")
			}
		}, want: ErrInvalidHTTPIntegration},
		{name: "signature mismatch", mutate: func(response *http.Response) {
			response.Trailer.Set("Signature", "trail=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:")
		}, want: ErrInvalidSignatureValue},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			response := signedTrailerResponse(t, now, key, request, "payload")
			config := BufferedTrailerVerifyingRoundTripperConfig{
				Transport:          roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }),
				Verifier:           NewVerifier(testResponseTrailerVerificationProfile(t, now, key)),
				SelectLabel:        func(*http.Request, *http.Response, SignatureInputs, Signatures) (string, error) { return "trail", nil },
				RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 8,
			}
			if test.configure != nil {
				test.configure(&config)
			}
			if test.mutate != nil {
				original := response.Body.(*trailerPopulatingBody)
				test.mutate(response)
				if populated, ok := response.Body.(*trailerPopulatingBody); ok && populated == original {
					test.mutate(&http.Response{Trailer: populated.values})
				}
			}
			verifying, err := NewBufferedTrailerVerifyingRoundTripper(config)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := verifying.RoundTrip(request); got != nil || !errors.Is(err, test.want) {
				t.Fatalf("result = %#v, %v, want %v", got, err, test.want)
			}
		})
	}
}

func TestTrailerSigningRoundTripperRejectsEachStreamingFailure(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	signer := NewSigner(testTrailerSigningProfile(t, now, key))
	baseConfig := func() TrailerSigningRoundTripperConfig {
		return TrailerSigningRoundTripperConfig{
			Signer: signer, Label: "trail", Algorithms: []DigestAlgorithm{SHA512, SHA256}, MaxBytes: 8,
			Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		}
	}
	for _, test := range []struct {
		name      string
		configure func(*TrailerSigningRoundTripperConfig)
		prepare   func(*http.Request)
		want      error
	}{
		{name: "options", configure: func(config *TrailerSigningRoundTripperConfig) {
			config.Options = func(context.Context, *http.Request) (SigningOptions, error) {
				return SigningOptions{}, errors.New("private")
			}
		}, want: ErrHTTPIntegrationSigning},
		{name: "external", configure: func(config *TrailerSigningRoundTripperConfig) {
			config.ExternalContext = func(context.Context, *http.Request) (*ExternalRequestContext, error) {
				return nil, errors.New("private")
			}
		}, want: ErrHTTPIntegrationSigning},
		{name: "sign", configure: func(config *TrailerSigningRoundTripperConfig) {
			config.Options = func(context.Context, *http.Request) (SigningOptions, error) {
				return SigningOptions{Nonce: "forbidden"}, nil
			}
		}, want: ErrHTTPIntegrationSigning},
		{name: "too large", configure: func(config *TrailerSigningRoundTripperConfig) { config.MaxBytes = 1 }, want: ErrBodyTooLarge},
		{name: "existing header", prepare: func(request *http.Request) { request.Header.Set("Signature", "sig=:AA==:") }, want: ErrExistingSignatures},
		{name: "existing trailer", prepare: func(request *http.Request) {
			request.Trailer = http.Header{"Content-Digest": []string{"sha-256=:AA==:"}}
		}, want: ErrExistingSignatures},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			config := baseConfig()
			config.Transport = roundTripperFunc(func(streamed *http.Request) (*http.Response, error) {
				_, err := io.ReadAll(streamed.Body)
				_ = streamed.Body.Close()
				return nil, err
			})
			if test.configure != nil {
				test.configure(&config)
			}
			transport, err := NewTrailerSigningRoundTripper(config)
			if err != nil {
				t.Fatal(err)
			}
			body := &countingBody{reader: strings.NewReader("payload")}
			request := httptest.NewRequest(http.MethodPost, "https://example.com/upload", body)
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

	config := baseConfig()
	config.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil })
	transport, err := NewTrailerSigningRoundTripper(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(nil); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("nil request error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://example.com/upload", nil)
	request.Body = nil
	if _, err := transport.RoundTrip(request); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("nil body error = %v", err)
	}
	var nilTransport *TrailerSigningRoundTripper
	if _, err := nilTransport.RoundTrip(request); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("nil receiver error = %v", err)
	}

	invalid := *transport
	invalid.algorithms = []DigestAlgorithm{"unsupported"}
	body := &countingBody{reader: strings.NewReader("x")}
	request = httptest.NewRequest(http.MethodPost, "https://example.com/upload", body)
	if _, err := invalid.RoundTrip(request); !errors.Is(err, ErrInvalidBodyIntegration) || body.closed != 1 {
		t.Fatalf("invalid internal algorithm = %v, closes=%d", err, body.closed)
	}

	badConfig := baseConfig()
	badConfig.Transport = config.Transport
	badConfig.Algorithms = []DigestAlgorithm{"unsupported"}
	if _, err := NewTrailerSigningRoundTripper(badConfig); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("unsupported constructor error = %v", err)
	}
}

func TestTrailerResponseSigningMiddlewareReportsEachPostWriteFailure(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	baseConfig := func() TrailerResponseSigningMiddlewareConfig {
		return TrailerResponseSigningMiddlewareConfig{
			Signer: NewSigner(testResponseTrailerSigningProfile(t, now, key)), Label: "trail",
			Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 8,
			Options: func(context.Context, *http.Request) (SigningOptions, error) { return SigningOptions{}, nil },
		}
	}
	for _, test := range []struct {
		name      string
		configure func(*TrailerResponseSigningMiddlewareConfig)
		handler   http.Handler
		prepare   func(http.ResponseWriter)
		want      error
	}{
		{name: "nil next", want: ErrInvalidBodyIntegration},
		{name: "existing before handler", prepare: func(writer http.ResponseWriter) { writer.Header().Set("Content-Digest", "sha-256=:AA==:") }, handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), want: ErrInvalidBodyIntegration},
		{name: "options", configure: func(config *TrailerResponseSigningMiddlewareConfig) {
			config.Options = func(context.Context, *http.Request) (SigningOptions, error) {
				return SigningOptions{}, errors.New("private")
			}
		}, handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), want: ErrHTTPIntegrationSigning},
		{name: "external", configure: func(config *TrailerResponseSigningMiddlewareConfig) {
			config.ExternalContext = func(context.Context, *http.Request) (*ExternalRequestContext, error) {
				return nil, errors.New("private")
			}
		}, handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), want: ErrHTTPIntegrationSigning},
		{name: "too large", configure: func(config *TrailerResponseSigningMiddlewareConfig) { config.MaxBytes = 1 }, handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte("xx")) }), want: ErrBodyTooLarge},
		{name: "existing after handler", handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.Header().Set("Signature", "sig=:AA==:") }), want: ErrExistingSignatures},
		{name: "sign", configure: func(config *TrailerResponseSigningMiddlewareConfig) {
			config.Options = func(context.Context, *http.Request) (SigningOptions, error) {
				return SigningOptions{Nonce: "forbidden"}, nil
			}
		}, handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), want: ErrHTTPIntegrationSigning},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			config := baseConfig()
			var got error
			config.ReportError = func(_ *http.Request, err error) { got = err }
			if test.configure != nil {
				test.configure(&config)
			}
			middleware, err := NewTrailerResponseSigningMiddleware(config)
			if err != nil {
				t.Fatal(err)
			}
			writer := httptest.NewRecorder()
			if test.prepare != nil {
				test.prepare(writer)
			}
			middleware(test.handler).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "https://example.com/data", nil))
			if !errors.Is(got, test.want) {
				t.Fatalf("reported error = %v, want %v", got, test.want)
			}
		})
	}

	config := baseConfig()
	config.ReportError = func(*http.Request, error) {}
	config.Algorithms = []DigestAlgorithm{"unsupported"}
	if _, err := NewTrailerResponseSigningMiddleware(config); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("unsupported constructor error = %v", err)
	}
	if _, err := NewTrailerResponseSigningMiddleware(TrailerResponseSigningMiddlewareConfig{}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("empty constructor error = %v", err)
	}
}
