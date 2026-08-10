package jwt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

func TestConcurrentCloseStateHonorsCompletionAndCancellation(t *testing.T) {
	t.Parallel()

	completed := &Remote{closing: true, closeDone: make(chan struct{})}
	completed.closeErr = errors.New("shutdown failed")
	close(completed.closeDone)
	if err := completed.Close(context.Background()); err != completed.closeErr {
		t.Fatalf("Close(completed) error = %v", err)
	}

	waiting := &Remote{closing: true, closeDone: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waiting.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close(canceled waiter) error = %v", err)
	}

	closed := &Remote{closed: true}
	if err := closed.closeResult(); err != nil {
		t.Fatalf("closeResult(closed) error = %v", err)
	}
	failed := &Remote{closeErr: context.DeadlineExceeded}
	if err := failed.closeResult(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("closeResult(failed) error = %v", err)
	}

	busy := &Remote{operations: map[uint64]context.CancelFunc{1: func() {}}}
	deadline, deadlineCancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer deadlineCancel()
	<-deadline.Done()
	if err := busy.Close(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close(active operation deadline) error = %v", err)
	}
}

func TestKeySetClassifiesUnregisteredCacheLookup(t *testing.T) {
	t.Parallel()

	cache, err := jwk.NewCache(context.Background(), httprc.NewClient())
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	t.Cleanup(func() { _ = cache.Shutdown(context.Background()) })
	remote := &Remote{cache: cache, url: "https://issuer.example.test/unregistered"}
	if _, err := remote.KeySet(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("KeySet(unregistered) error = %v", err)
	}
}

func TestRemoteConfigurationRejectsEachUnsafeBoundary(t *testing.T) {
	t.Parallel()
	if _, err := NewRemote(context.Background(), "://"); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewRemote(invalid URL) error = %v", err)
	}

	parsed, err := url.Parse("https://issuer.example.test/keys")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	base := remoteConfig{
		client: http.DefaultClient, minRefresh: time.Nanosecond,
		maxRefresh: time.Nanosecond, maxBodyBytes: 1, maxHeaderBytes: 1,
		maxKeys: 1, initTimeout: time.Nanosecond,
	}
	if !validRemoteConfiguration(parsed, base) {
		t.Fatal("validRemoteConfiguration(exact bounds) = false")
	}
	tests := map[string]func(*url.URL, *remoteConfig){
		"host":          func(u *url.URL, _ *remoteConfig) { u.Host = "" },
		"user":          func(u *url.URL, _ *remoteConfig) { u.User = url.User("user") },
		"fragment":      func(u *url.URL, _ *remoteConfig) { u.Fragment = "fragment" },
		"scheme":        func(u *url.URL, _ *remoteConfig) { u.Scheme = "ftp" },
		"client":        func(_ *url.URL, c *remoteConfig) { c.client = nil },
		"minimum":       func(_ *url.URL, c *remoteConfig) { c.minRefresh = -1 },
		"refresh order": func(_ *url.URL, c *remoteConfig) { c.minRefresh = 2; c.maxRefresh = 1 },
		"body":          func(_ *url.URL, c *remoteConfig) { c.maxBodyBytes = -1 },
		"headers":       func(_ *url.URL, c *remoteConfig) { c.maxHeaderBytes = -1 },
		"keys":          func(_ *url.URL, c *remoteConfig) { c.maxKeys = -1 },
		"jitter low":    func(_ *url.URL, c *remoteConfig) { c.refreshJitter = -0.1 },
		"jitter high":   func(_ *url.URL, c *remoteConfig) { c.refreshJitter = 1 },
		"timeout":       func(_ *url.URL, c *remoteConfig) { c.initTimeout = -1 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidateURL := *parsed
			candidateConfig := base
			mutate(&candidateURL, &candidateConfig)
			if validRemoteConfiguration(&candidateURL, candidateConfig) {
				t.Fatal("validRemoteConfiguration() = true")
			}
		})
	}
	httpURL := *parsed
	httpURL.Scheme = "http"
	base.allowHTTP = true
	if !validRemoteConfiguration(&httpURL, base) {
		t.Fatal("validRemoteConfiguration(HTTP opt-in) = false")
	}
}

func TestRemoteOperationStateRejectsClosingAndIgnoresUnknownCompletion(t *testing.T) {
	t.Parallel()

	remote := &Remote{closing: true}
	if _, _, _, err := remote.beginOperation(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("beginOperation(closing) error = %v", err)
	}
	remote = &Remote{operations: make(map[uint64]context.CancelFunc)}
	remote.endOperation(99)

	idle := make(chan struct{})
	remote = &Remote{
		closing:    true,
		idle:       idle,
		operations: map[uint64]context.CancelFunc{1: func() {}},
	}
	remote.endOperation(1)
	select {
	case <-idle:
	default:
		t.Fatal("endOperation(last operation) did not signal idle")
	}
}

func TestRemoteBoundsConcurrentOperations(t *testing.T) {
	t.Parallel()

	remote := &Remote{}
	operations := make([]uint64, 128)
	for index := range operations {
		_, _, operation, err := remote.beginOperation(context.Background())
		if err != nil {
			t.Fatalf("beginOperation(%d) error = %v", index, err)
		}
		operations[index] = operation
	}
	if _, _, _, err := remote.beginOperation(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("beginOperation(excess) error = %v", err)
	}
	for _, operation := range operations {
		remote.endOperation(operation)
	}
}

func TestRemoteRefreshSchedulingHasFleetJitter(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(symmetricKeySet(t, "key", jwa.HS256(), "sig"))
	if err != nil {
		t.Fatalf("json.Marshal(JWK set) error = %v", err)
	}
	intervals := make(map[string]struct{})
	for range 64 {
		source := &http.Client{Transport: internalRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": {"max-age=60"},
					"Content-Type":  {"application/json"},
				},
				Body: io.NopCloser(bytes.NewReader(encoded)), Request: request,
			}, nil
		})}
		client := hardenedRemoteHTTPClient(remoteConfig{
			client: source, minRefresh: 30 * time.Second, maxRefresh: 2 * time.Minute,
			maxBodyBytes: defaultMaxJWKBodyBytes, maxHeaderBytes: defaultMaxJWKHeaderBytes,
			maxKeys: defaultMaxKeys, refreshJitter: defaultRefreshJitter,
		})
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://issuer.example.test/keys", nil)
		if err != nil {
			t.Fatalf("NewRequestWithContext() error = %v", err)
		}
		response, err := client.Transport.RoundTrip(request)
		if err != nil {
			t.Fatalf("RoundTrip() error = %v", err)
		}
		intervals[response.Header.Get("Cache-Control")] = struct{}{}
		_ = response.Body.Close()
	}
	if len(intervals) < 4 {
		t.Fatalf("fleet refresh intervals = %v, want at least 4 distinct values", intervals)
	}
}

func TestRemoteOptionsApplyExactSecurityBounds(t *testing.T) {
	t.Parallel()

	configuration := remoteConfig{}
	WithRefreshJitter(0.25)(&configuration)
	WithMaxJWKHeaderBytes(123)(&configuration)
	WithMaxJWKKeys(7)(&configuration)
	if configuration.refreshJitter != 0.25 || configuration.maxHeaderBytes != 123 || configuration.maxKeys != 7 {
		t.Fatalf("remote options produced %+v", configuration)
	}
}

func TestRemoteTransportGateHonorsCancellation(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://issuer.example.test/keys", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	transport := &jwkResponseTransport{refreshGate: gate}
	if _, err := transport.RoundTrip(request); !errors.Is(err, context.Canceled) {
		t.Fatalf("RoundTrip() error = %v, want canceled", err)
	}
}

func TestRemoteTransportRetainsTheNarrowestHeaderLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		current int64
		want    int64
	}{
		{current: -1, want: 100},
		{current: 0, want: 100},
		{current: 50, want: 50},
		{current: 100, want: 100},
		{current: 101, want: 100},
	}
	for _, tt := range tests {
		client := hardenedRemoteHTTPClient(remoteConfig{
			client:         &http.Client{Transport: &http.Transport{MaxResponseHeaderBytes: tt.current}},
			maxHeaderBytes: 100,
		})
		policy, ok := client.Transport.(*jwkResponseTransport)
		if !ok {
			t.Fatalf("client transport type = %T", client.Transport)
		}
		standard, ok := policy.base.(*http.Transport)
		if !ok {
			t.Fatalf("base transport type = %T", policy.base)
		}
		if standard.MaxResponseHeaderBytes != tt.want {
			t.Fatalf("header limit from %d = %d, want %d", tt.current, standard.MaxResponseHeaderBytes, tt.want)
		}
		if !standard.DisableCompression {
			t.Fatal("standard transport compression remains enabled")
		}
	}
}

func TestJWKResponseTransportRejectsBrokenAndOversizedResponses(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://issuer.example.test/keys", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	tests := map[string]internalRoundTripFunc{
		"nil response": func(*http.Request) (*http.Response, error) { return nil, nil },
		"nil body": func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: request}, nil
		},
		"oversized headers": func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"X": {"12"}},
				Body: io.NopCloser(strings.NewReader("{}")), Request: request,
			}, nil
		},
		"oversized error body": func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("too large")), Request: request,
			}, nil
		},
	}
	for name, base := range tests {
		t.Run(name, func(t *testing.T) {
			transport := &jwkResponseTransport{base: base, maxHeaderBytes: 2, maxBodyBytes: 2, maxKeys: 1}
			if _, err := transport.RoundTrip(request); err == nil {
				t.Fatal("RoundTrip() error = nil")
			}
		})
	}
}

func TestJWKResponseTransportAcceptsExactResponseBounds(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(symmetricKeySet(t, "key", jwa.HS256(), "sig"))
	if err != nil {
		t.Fatalf("json.Marshal(JWK set) error = %v", err)
	}
	header := http.Header{"X": {"12"}}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://issuer.example.test/keys", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	transport := &jwkResponseTransport{
		base: internalRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("Accept-Encoding") != "identity" {
				t.Fatalf("Accept-Encoding = %q", request.Header.Get("Accept-Encoding"))
			}
			return &http.Response{
				StatusCode: http.StatusOK, Header: header.Clone(),
				Body: io.NopCloser(bytes.NewReader(encoded)), Request: request,
			}, nil
		}),
		maxHeaderBytes: responseHeaderBytes(header), maxBodyBytes: int64(len(encoded)), maxKeys: 1,
		minRefresh: time.Second, maxRefresh: time.Second,
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip(exact bounds) error = %v", err)
	}
	_ = response.Body.Close()
}

func TestJWKResponseTransportAcceptsNilHeadersFromCustomTransport(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(symmetricKeySet(t, "key", jwa.HS256(), "sig"))
	if err != nil {
		t.Fatalf("json.Marshal(JWK set) error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://issuer.example.test/keys", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	transport := &jwkResponseTransport{
		base: internalRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(encoded)), Request: request,
			}, nil
		}),
		maxHeaderBytes: 1, maxBodyBytes: int64(len(encoded)), maxKeys: 1,
		minRefresh: time.Second, maxRefresh: time.Minute, refreshJitter: 0.1,
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip(nil headers) error = %v", err)
	}
	if response.Header.Get("Cache-Control") == "" {
		t.Fatal("RoundTrip(nil headers) did not set refresh policy")
	}
	_ = response.Body.Close()
}

func TestJWKResponseTransportPreservesNonSuccessfulResponses(t *testing.T) {
	t.Parallel()

	encoded := []byte(`{}`)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://issuer.example.test/keys", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	for _, status := range []int{http.StatusOK - 1, http.StatusMultipleChoices} {
		transport := &jwkResponseTransport{
			base: internalRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status, Header: make(http.Header), ContentLength: -1,
					Body: io.NopCloser(bytes.NewReader(encoded)), Request: request,
				}, nil
			}),
			maxHeaderBytes: 1, maxBodyBytes: int64(len(encoded)), maxKeys: 1,
		}
		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatalf("RoundTrip(status %d) error = %v", status, err)
		}
		if response.ContentLength != int64(len(encoded)) {
			t.Fatalf("RoundTrip(status %d) content length = %d", status, response.ContentLength)
		}
		_ = response.Body.Close()
	}

	for _, status := range []int{http.StatusOK, http.StatusMultipleChoices - 1} {
		transport := &jwkResponseTransport{
			base: internalRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{}`)), Request: request,
				}, nil
			}),
			maxHeaderBytes: 1, maxBodyBytes: 2, maxKeys: 1,
		}
		if _, err := transport.RoundTrip(request); err == nil {
			t.Fatalf("RoundTrip(success status %d with invalid JWK) error = nil", status)
		}
	}
}

func TestRemoteRefreshTimingHonorsBoundsAndCacheHeaders(t *testing.T) {
	t.Parallel()

	now := time.Unix(10_000, 0).UTC()
	minimum := 10 * time.Second
	maximum := 100 * time.Second
	tests := []struct {
		name   string
		header http.Header
		want   time.Duration
	}{
		{name: "quoted max age", header: http.Header{"Cache-Control": {`public, MAX-AGE="40"`}}, want: 40 * time.Second},
		{name: "preceded max age", header: http.Header{"Cache-Control": {"other=50, max-age=40"}}, want: 40 * time.Second},
		{name: "below minimum", header: http.Header{"Cache-Control": {"max-age=1"}}, want: minimum},
		{name: "above maximum", header: http.Header{"Cache-Control": {"max-age=101"}}, want: maximum},
		{name: "expires below minimum", header: http.Header{"Expires": {now.Add(time.Second).Format(http.TimeFormat)}}, want: minimum},
		{name: "expires above maximum", header: http.Header{"Expires": {now.Add(time.Hour).Format(http.TimeFormat)}}, want: maximum},
		{name: "other directive", header: http.Header{"Cache-Control": {"other=50"}}, want: minimum},
		{name: "unusable", header: http.Header{"Cache-Control": {"private, max-age=invalid"}}, want: minimum},
		{name: "no cache overrides max age", header: http.Header{"Cache-Control": {"max-age=90, no-cache"}}, want: minimum},
		{name: "no store overrides max age", header: http.Header{"Cache-Control": {"no-store, max-age=90"}}, want: minimum},
		{name: "must revalidate overrides max age", header: http.Header{"Cache-Control": {"max-age=90, must-revalidate"}}, want: minimum},
		{name: "age reduces max age", header: http.Header{"Cache-Control": {"max-age=90"}, "Age": {"30"}}, want: 60 * time.Second},
		{name: "age exhausts max age", header: http.Header{"Cache-Control": {"max-age=30"}, "Age": {"60"}}, want: minimum},
		{name: "zero age is inert", header: http.Header{"Cache-Control": {"max-age=90"}, "Age": {"0"}}, want: 90 * time.Second},
		{name: "negative age is ignored", header: http.Header{"Cache-Control": {"max-age=90"}, "Age": {"-30"}}, want: 90 * time.Second},
		{name: "malformed age is ignored", header: http.Header{"Cache-Control": {"max-age=90"}, "Age": {"invalid"}}, want: 90 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cacheLifetime(tt.header, now, minimum, maximum); got != tt.want {
				t.Fatalf("cacheLifetime() = %s, want %s", got, tt.want)
			}
		})
	}

	if got := jitterDuration(50*time.Second, minimum, maximum, 0.2, 0); got != 40*time.Second {
		t.Fatalf("jitterDuration(lower) = %s", got)
	}
	if got := jitterDuration(50*time.Second, minimum, maximum, 0.2, 25*uint64(time.Second)); got != 45*time.Second {
		t.Fatalf("jitterDuration(interior) = %s", got)
	}
	if got := jitterDuration(minimum, minimum, maximum, 0.2, 0); got != minimum {
		t.Fatalf("jitterDuration(clamped lower) = %s", got)
	}
	if got := jitterDuration(maximum, minimum, maximum, 0.2, 0); got != 80*time.Second {
		t.Fatalf("jitterDuration(clamped upper) = %s", got)
	}
	if got := jitterDuration(minimum, minimum, minimum, 0.2, 0); got != minimum {
		t.Fatalf("jitterDuration(fixed) = %s", got)
	}

	response := &http.Response{Header: make(http.Header)}
	transport := &jwkResponseTransport{minRefresh: time.Nanosecond, maxRefresh: time.Nanosecond, refreshJitter: 0}
	transport.jitterRefreshHeader(response)
	if response.Header.Get("Cache-Control") != "" {
		t.Fatalf("zero jitter changed headers to %q", response.Header.Get("Cache-Control"))
	}
	transport.refreshJitter = 0.5
	transport.jitterRefreshHeader(response)
	if response.Header.Get("Cache-Control") != "max-age=1" {
		t.Fatalf("subsecond jitter header = %q", response.Header.Get("Cache-Control"))
	}
	transport.minRefresh = time.Second
	transport.maxRefresh = time.Second
	response.Header = make(http.Header)
	transport.jitterRefreshHeader(response)
	if response.Header.Get("Cache-Control") != "max-age=1" {
		t.Fatalf("exact-second jitter header = %q", response.Header.Get("Cache-Control"))
	}
	transport.minRefresh = 1500 * time.Millisecond
	transport.maxRefresh = 1500 * time.Millisecond
	response.Header = make(http.Header)
	transport.jitterRefreshHeader(response)
	if response.Header.Get("Cache-Control") != "max-age=2" {
		t.Fatalf("fractional-second jitter header = %q", response.Header.Get("Cache-Control"))
	}
	if got := responseHeaderBytes(http.Header{"First": {"value", "second"}, "X": {"y"}}); got != 18 {
		t.Fatalf("responseHeaderBytes() = %d, want 18", got)
	}
	if got := mixJitter(1); got != 0x5692161d100b05e5 {
		t.Fatalf("mixJitter(1) = %#x", got)
	}
	body, err := readBoundedBody(io.NopCloser(strings.NewReader("exact")), 5)
	if err != nil || string(body) != "exact" {
		t.Fatalf("readBoundedBody(exact) = %q, %v", body, err)
	}
	body, err = readBoundedBody(io.NopCloser(strings.NewReader("valid")), math.MaxInt64)
	if err != nil || string(body) != "valid" {
		t.Fatalf("readBoundedBody(maximum limit) = %q, %v", body, err)
	}
	_, err = readBoundedBody(io.NopCloser(io.MultiReader(strings.NewReader("x"), internalFailingReader{})), 1)
	if err == nil {
		t.Fatal("readBoundedBody(exact body followed by failure) error = nil")
	}
}

func TestRemoteJWKValidationRejectsEveryKeyPolicyViolation(t *testing.T) {
	t.Parallel()

	key := func() jwk.Key {
		set := symmetricKeySet(t, "key", jwa.HS256(), "sig")
		result, ok := set.Key(0)
		if !ok {
			t.Fatal("symmetricKeySet() is empty")
		}
		return result
	}
	encode := func(mutator func(jwk.Key)) []byte {
		candidate := key()
		mutator(candidate)
		set := jwk.NewSet()
		if err := set.AddKey(candidate); err != nil {
			t.Fatalf("AddKey() error = %v", err)
		}
		encoded, err := json.Marshal(set)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		return encoded
	}
	tests := map[string][]byte{
		"empty set":            []byte(`{"keys":[]}`),
		"too many":             []byte(`{"keys":[{"kty":"oct","k":"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY","kid":"a","alg":"HS256"},{"kty":"oct","k":"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY","kid":"b","alg":"HS256"}]}`),
		"missing kid":          encode(func(key jwk.Key) { _ = key.Remove(jwk.KeyIDKey) }),
		"empty kid":            encode(func(key jwk.Key) { _ = key.Set(jwk.KeyIDKey, "") }),
		"missing algorithm":    encode(func(key jwk.Key) { _ = key.Remove(jwk.AlgorithmKey) }),
		"wrong algorithm type": encode(func(key jwk.Key) { _ = key.Set(jwk.AlgorithmKey, jwa.RS256()) }),
		"weak material": func() []byte {
			encoded, err := json.Marshal(keySetFromRaw(t, []byte("short"), jwa.HS256()))
			if err != nil {
				t.Fatalf("json.Marshal(weak key) error = %v", err)
			}
			return encoded
		}(),
		"wrong use":        encode(func(key jwk.Key) { _ = key.Set(jwk.KeyUsageKey, "enc") }),
		"wrong operations": encode(func(key jwk.Key) { _ = key.Set(jwk.KeyOpsKey, jwk.KeyOperationList{jwk.KeyOpSign}) }),
	}
	valid, err := json.Marshal(symmetricKeySet(t, "key", jwa.HS256(), "sig"))
	if err != nil {
		t.Fatalf("json.Marshal(valid key) error = %v", err)
	}
	if err := validateRemoteJWKBody(valid, 1); err != nil {
		t.Fatalf("validateRemoteJWKBody(exact key bound) error = %v", err)
	}
	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			maximum := 1
			if name == "too many" {
				maximum = 1
			}
			if err := validateRemoteJWKBody(encoded, maximum); err == nil {
				t.Fatalf("validateRemoteJWKBody(%s) error = nil", name)
			}
		})
	}
}

func TestCloneKeySetReportsEncodingFailure(t *testing.T) {
	t.Parallel()

	if _, err := cloneKeySet(failingJSONSet{embeddedJWKSet: jwk.NewSet()}); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("cloneKeySet() error = %v", err)
	}
	if _, err := cloneKeySet(malformedJSONSet{embeddedJWKSet: jwk.NewSet()}); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("cloneKeySet(malformed) error = %v", err)
	}
}

func TestRemoteRefreshWaiterHonorsCancellation(t *testing.T) {
	t.Parallel()

	remote := &Remote{refreshing: true, refreshDone: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := remote.refresh(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("refresh(canceled waiter) error = %v", err)
	}
}

type embeddedJWKSet interface{ jwk.Set }

type failingJSONSet struct{ embeddedJWKSet }

func (failingJSONSet) MarshalJSON() ([]byte, error) { return nil, fmt.Errorf("encode failure") }

type malformedJSONSet struct{ embeddedJWKSet }

func (malformedJSONSet) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }

type internalRoundTripFunc func(*http.Request) (*http.Response, error)

func (function internalRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type internalFailingReader struct{}

func (internalFailingReader) Read([]byte) (int, error) {
	return 0, errors.New("injected trailing-body failure")
}
