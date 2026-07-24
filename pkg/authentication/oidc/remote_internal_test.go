package oidc

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	jose "github.com/go-jose/go-jose/v4"
)

func TestNewRejectsConfigurationAndInvalidDiscoveryMetadata(t *testing.T) {
	t.Parallel()

	if _, err := New(context.Background(), Config{}); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("New(invalid config) error = %v", err)
	}
	server := httptest.NewServer(nil)
	server.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize",
			"token_endpoint": server.URL + "/token", "jwks_uri": "ftp://keys.example.test/keys",
		})
	})
	t.Cleanup(server.Close)
	_, err := New(context.Background(), Config{
		Issuer: server.URL, ClientID: "client", Algorithms: []string{"RS256"},
		Clock: authtest.NewClock(time.Unix(1, 0)), InsecureHTTP: true,
		HTTPClient: server.Client(),
	})
	if !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("New(invalid metadata) error = %v", err)
	}
}

func TestRemoteKeySetRejectsSignatureStructuresAndCachedMismatch(t *testing.T) {
	t.Parallel()

	private := mustRSAKey(t)
	other := mustRSAKey(t)
	raw := signCompact(t, private, "key", []byte(`{"sub":"user"}`))
	set := &remoteKeySet{algorithms: []jose.SignatureAlgorithm{jose.RS256}}
	if _, err := set.VerifySignature(context.Background(), "invalid"); err == nil {
		t.Fatal("VerifySignature(invalid) error = nil")
	}
	missingKeyID := signCompact(t, private, "", []byte(`{}`))
	if _, err := set.VerifySignature(context.Background(), missingKeyID); err == nil {
		t.Fatal("VerifySignature(missing kid) error = nil")
	}
	set.keys = []jose.JSONWebKey{{Key: &other.PublicKey, KeyID: "key", Algorithm: "RS256", Use: "sig"}}
	if _, err := set.VerifySignature(context.Background(), raw); err == nil {
		t.Fatal("VerifySignature(cached mismatch) error = nil")
	}
}

func TestRemoteFetchRejectsTransportAndJWKFailures(t *testing.T) {
	t.Parallel()

	private := mustRSAKey(t)
	ecPrivate, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(EC) error = %v", err)
	}
	valid := jose.JSONWebKey{Key: &private.PublicKey, KeyID: "key", Algorithm: "RS256", Use: "sig"}
	encode := func(keys ...jose.JSONWebKey) []byte {
		body, err := json.Marshal(jose.JSONWebKeySet{Keys: keys})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		return body
	}
	tests := []struct {
		name    string
		url     string
		client  *http.Client
		body    []byte
		status  int
		maxKeys int
	}{
		{name: "invalid URL", url: "://", client: http.DefaultClient, maxKeys: 1},
		{name: "transport", client: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("network") })}, maxKeys: 1},
		{name: "status", status: http.StatusServiceUnavailable, maxKeys: 1},
		{name: "invalid JSON", body: []byte(`{`), maxKeys: 1},
		{name: "empty", body: encode(), maxKeys: 1},
		{name: "too many", body: encode(valid, jose.JSONWebKey{Key: &private.PublicKey, KeyID: "other", Algorithm: "RS256", Use: "sig"}), maxKeys: 1},
		{name: "missing metadata", body: encode(jose.JSONWebKey{Key: &private.PublicKey}), maxKeys: 1},
		{name: "private key", body: encode(jose.JSONWebKey{Key: private, KeyID: "key", Algorithm: "RS256", Use: "sig"}), maxKeys: 1},
		{name: "duplicate", body: encode(valid, valid), maxKeys: 2},
		{name: "disallowed", body: encode(jose.JSONWebKey{Key: &private.PublicKey, KeyID: "key", Algorithm: "RS384", Use: "sig"}), maxKeys: 1},
		{name: "key type mismatch", body: encode(jose.JSONWebKey{Key: &ecPrivate.PublicKey, KeyID: "key", Algorithm: "RS256", Use: "sig"}), maxKeys: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.client
			url := tt.url
			if client == nil {
				status := tt.status
				if status == 0 {
					status = http.StatusOK
				}
				client = &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(tt.body)), Header: make(http.Header), Request: request}, nil
				})}
			}
			if url == "" {
				url = "https://issuer.example.test/keys"
			}
			set := &remoteKeySet{url: url, client: client, maxBodyBytes: 1 << 20, maxKeys: tt.maxKeys, allowed: map[string]struct{}{"RS256": {}}}
			if _, err := set.fetch(context.Background()); err == nil {
				t.Fatal("fetch() error = nil")
			}
		})
	}
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(encode(valid))),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}
	set := &remoteKeySet{
		url: "https://issuer.example.test/keys", client: client,
		maxBodyBytes: 1 << 20, maxKeys: 1, allowed: map[string]struct{}{"RS256": {}},
	}
	if keys, err := set.fetch(context.Background()); err != nil || len(keys) != 1 {
		t.Fatalf("fetch(valid) = %d keys, %v", len(keys), err)
	}
}

func TestRemoteFetchReportsReadAndSizeFailures(t *testing.T) {
	t.Parallel()

	tests := []io.ReadCloser{
		&errorReadCloser{err: errors.New("read failed")},
		io.NopCloser(strings.NewReader("oversized")),
	}
	for _, body := range tests {
		client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header), Request: request}, nil
		})}
		set := &remoteKeySet{url: "https://issuer.example.test/keys", client: client, maxBodyBytes: 1, maxKeys: 1}
		if _, err := set.fetch(context.Background()); err == nil {
			t.Fatal("fetch() error = nil")
		}
	}
}

func TestHTTPHardeningAndBoundedReaders(t *testing.T) {
	t.Parallel()

	client := hardenedClient(nil, 1)
	if client.Timeout != 30*time.Second || client.Transport == nil {
		t.Fatalf("hardenedClient() = %#v", client)
	}
	if err := client.CheckRedirect(&http.Request{}, nil); err == nil {
		t.Fatal("CheckRedirect() error = nil")
	}
	transport := boundedTransport{base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport failed")
	}), maximum: 1}
	if _, err := transport.RoundTrip(&http.Request{}); err == nil {
		t.Fatal("RoundTrip() error = nil")
	}
	body := &boundedBody{body: io.NopCloser(strings.NewReader("abc")), remaining: 1}
	buffer := make([]byte, 8)
	if _, err := body.Read(buffer); !errors.Is(err, errHTTPBodyTooLarge) {
		t.Fatalf("Read(oversized) error = %v", err)
	}
	if _, err := body.Read(buffer); !errors.Is(err, errHTTPBodyTooLarge) {
		t.Fatalf("Read(after oversized) error = %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := readBounded(errorReader{err: errors.New("read failed")}, 1); err == nil {
		t.Fatal("readBounded(read error) error = nil")
	}
	if _, err := readBounded(strings.NewReader("ab"), 1); !errors.Is(err, errHTTPBodyTooLarge) {
		t.Fatalf("readBounded(oversized) error = %v", err)
	}
}

func TestRemoteRefreshConfigurationAndCacheSemantics(t *testing.T) {
	t.Parallel()

	set := &remoteKeySet{}
	if err := set.acquireWaiter(context.Background()); err != nil {
		t.Fatalf("acquireWaiter(unbounded) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := set.acquireWaiter(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquireWaiter(canceled unbounded) error = %v", err)
	}
	set.waiters = make(chan struct{}, 1)
	set.waiters <- struct{}{}
	if err := set.acquireWaiter(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquireWaiter(canceled bounded) error = %v", err)
	}
	<-set.waiters

	if now := set.now(); now.IsZero() {
		t.Fatal("now(default) is zero")
	}
	minimum, maximum := set.refreshBounds()
	if minimum != time.Minute || maximum != time.Hour {
		t.Fatalf("refreshBounds(default) = %v, %v", minimum, maximum)
	}
	set.minRefreshInterval = 2 * time.Hour
	set.maxRefreshInterval = time.Minute
	minimum, maximum = set.refreshBounds()
	if minimum != 2*time.Hour || maximum != 2*time.Hour {
		t.Fatalf("refreshBounds(clamped) = %v, %v", minimum, maximum)
	}

	date := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		header http.Header
		want   time.Duration
	}{
		{name: "default", header: make(http.Header), want: 10 * time.Second},
		{name: "no cache", header: http.Header{"Cache-Control": {"no-cache"}}, want: 10 * time.Second},
		{name: "unrelated", header: http.Header{"Cache-Control": {"public"}}, want: 10 * time.Second},
		{name: "max age", header: http.Header{"Cache-Control": {"public, max-age=30"}}, want: 30 * time.Second},
		{name: "quoted max age", header: http.Header{"Cache-Control": {`max-age="20"`}}, want: 20 * time.Second},
		{name: "invalid max age", header: http.Header{"Cache-Control": {"max-age=invalid"}}, want: 10 * time.Second},
		{name: "negative max age", header: http.Header{"Cache-Control": {"max-age=-1"}}, want: 10 * time.Second},
		{name: "maximum", header: http.Header{"Cache-Control": {"max-age=999999"}}, want: time.Minute},
		{name: "minimum", header: http.Header{"Cache-Control": {"max-age=0"}}, want: 10 * time.Second},
		{name: "expires", header: http.Header{"Date": {date.Format(http.TimeFormat)}, "Expires": {date.Add(45 * time.Second).Format(http.TimeFormat)}}, want: 45 * time.Second},
		{name: "expires above maximum", header: http.Header{"Date": {date.Format(http.TimeFormat)}, "Expires": {date.Add(2 * time.Minute).Format(http.TimeFormat)}}, want: time.Minute},
		{name: "age remaining", header: http.Header{"Cache-Control": {"max-age=30"}, "Age": {"5"}}, want: 25 * time.Second},
		{name: "age exhausted", header: http.Header{"Cache-Control": {"max-age=30"}, "Age": {"40"}}, want: 10 * time.Second},
	}
	for _, tt := range tests {
		if got := cacheLifetime(tt.header, 10*time.Second, time.Minute); got != tt.want {
			t.Errorf("cacheLifetime(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestJOSEKeyAlgorithmFamilies(t *testing.T) {
	t.Parallel()

	rsaPrivate := mustRSAKey(t)
	p256, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(P-256) error = %v", err)
	}
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(P-384) error = %v", err)
	}
	edPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(Ed25519) error = %v", err)
	}
	tests := []struct {
		name      string
		key       any
		algorithm string
		want      bool
	}{
		{name: "RSA", key: &rsaPrivate.PublicKey, algorithm: "RS256", want: true},
		{name: "RSA PSS", key: &rsaPrivate.PublicKey, algorithm: "PS256", want: true},
		{name: "RSA mismatch", key: &p256.PublicKey, algorithm: "RS256"},
		{name: "ECDSA", key: &p256.PublicKey, algorithm: "ES256", want: true},
		{name: "ECDSA wrong key", key: &rsaPrivate.PublicKey, algorithm: "ES256"},
		{name: "ECDSA wrong curve", key: &p384.PublicKey, algorithm: "ES256"},
		{name: "ECDSA unknown", key: &p256.PublicKey, algorithm: "ES999"},
		{name: "EdDSA", key: edPublic, algorithm: "EdDSA", want: true},
		{name: "EdDSA mismatch", key: &rsaPrivate.PublicKey, algorithm: "EdDSA"},
		{name: "unknown", key: &rsaPrivate.PublicKey, algorithm: "future"},
	}
	for _, tt := range tests {
		if got := joseKeyMatchesAlgorithm(tt.key, tt.algorithm); got != tt.want {
			t.Errorf("joseKeyMatchesAlgorithm(%s) = %v", tt.name, got)
		}
	}
}

func mustRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	return private
}

func signCompact(t *testing.T, private *rsa.PrivateKey, keyID string, payload []byte) string {
	t.Helper()
	options := (&jose.SignerOptions{}).WithType("JWT")
	if keyID != "" {
		options.WithHeader("kid", keyID)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: private}, options)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	compact, err := signed.CompactSerialize()
	if err != nil {
		t.Fatalf("CompactSerialize() error = %v", err)
	}
	return compact
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type errorReadCloser struct{ err error }

func (r *errorReadCloser) Read([]byte) (int, error) { return 0, r.err }
func (*errorReadCloser) Close() error               { return nil }
