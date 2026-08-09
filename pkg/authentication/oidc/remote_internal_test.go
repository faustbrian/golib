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
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestNewRejectsSigningAlgorithmsNotAdvertisedByProvider(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(nil)
	server.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize",
			"token_endpoint": server.URL + "/token", "jwks_uri": server.URL + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	t.Cleanup(server.Close)

	_, err := New(context.Background(), Config{
		Issuer: server.URL, ClientID: "client", Algorithms: []string{"ES256"},
		Clock: authtest.NewClock(time.Unix(1, 0)), InsecureHTTP: true,
		HTTPClient: server.Client(),
	})
	if !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("New(unadvertised algorithm) error = %v", err)
	}
}

func TestProviderMetadataValidationMatrix(t *testing.T) {
	t.Parallel()

	valid := providerMetadata{
		AuthorizationEndpoint: "https://issuer.example.test/authorize",
		TokenEndpoint:         "https://issuer.example.test/token",
		JWKSetURL:             "https://issuer.example.test/keys",
		ResponseTypes:         []string{"code id_token"},
		SubjectTypes:          []string{"public"},
		SigningAlgorithms:     []string{"RS256", "ES256"},
	}
	clone := func() providerMetadata {
		metadata := valid
		metadata.ResponseTypes = append([]string(nil), valid.ResponseTypes...)
		metadata.SubjectTypes = append([]string(nil), valid.SubjectTypes...)
		metadata.SigningAlgorithms = append([]string(nil), valid.SigningAlgorithms...)
		metadata.Scopes = append([]string(nil), valid.Scopes...)
		return metadata
	}
	tests := []struct {
		name  string
		alter func(*providerMetadata)
	}{
		{name: "authorization endpoint", alter: func(metadata *providerMetadata) {
			metadata.AuthorizationEndpoint = "http://issuer.example.test/authorize"
		}},
		{name: "JWK set URL", alter: func(metadata *providerMetadata) { metadata.JWKSetURL = "http://issuer.example.test/keys" }},
		{name: "token endpoint", alter: func(metadata *providerMetadata) { metadata.TokenEndpoint = "ftp://issuer.example.test/token" }},
		{name: "userinfo endpoint", alter: func(metadata *providerMetadata) { metadata.UserInfoEndpoint = "ftp://issuer.example.test/userinfo" }},
		{name: "registration endpoint", alter: func(metadata *providerMetadata) { metadata.RegistrationEndpoint = "ftp://issuer.example.test/register" }},
		{name: "missing token endpoint", alter: func(metadata *providerMetadata) { metadata.TokenEndpoint = "" }},
		{name: "missing response types", alter: func(metadata *providerMetadata) { metadata.ResponseTypes = nil }},
		{name: "empty response type", alter: func(metadata *providerMetadata) { metadata.ResponseTypes = []string{""} }},
		{name: "blank response type", alter: func(metadata *providerMetadata) { metadata.ResponseTypes = []string{" "} }},
		{name: "duplicate response type", alter: func(metadata *providerMetadata) { metadata.ResponseTypes = []string{"code", "code"} }},
		{name: "missing subject types", alter: func(metadata *providerMetadata) { metadata.SubjectTypes = nil }},
		{name: "unknown subject type", alter: func(metadata *providerMetadata) { metadata.SubjectTypes = []string{"transient"} }},
		{name: "missing signing algorithms", alter: func(metadata *providerMetadata) { metadata.SigningAlgorithms = nil }},
		{name: "missing RS256", alter: func(metadata *providerMetadata) { metadata.SigningAlgorithms = []string{"ES256"} }},
		{name: "duplicate signing algorithm", alter: func(metadata *providerMetadata) { metadata.SigningAlgorithms = []string{"RS256", "RS256"} }},
		{name: "missing configured algorithm", alter: func(metadata *providerMetadata) { metadata.SigningAlgorithms = []string{"RS256"} }},
		{name: "scope omits openid", alter: func(metadata *providerMetadata) { metadata.Scopes = []string{"profile"} }},
		{name: "duplicate scope", alter: func(metadata *providerMetadata) { metadata.Scopes = []string{"openid", "openid"} }},
	}
	allowed := map[string]struct{}{"RS256": {}, "ES256": {}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := clone()
			tt.alter(&metadata)
			if validProviderMetadata(metadata, false, allowed) {
				t.Fatal("validProviderMetadata() = true")
			}
		})
	}

	implicit := clone()
	implicit.TokenEndpoint = ""
	implicit.ResponseTypes = []string{"id_token", "id_token token"}
	implicit.UserInfoEndpoint = "https://issuer.example.test/userinfo"
	implicit.RegistrationEndpoint = "https://issuer.example.test/register"
	implicit.Scopes = []string{"openid", "profile"}
	if !validProviderMetadata(implicit, false, allowed) {
		t.Fatal("validProviderMetadata(implicit) = false")
	}
}

func TestNewFailsWhenInitialJWKSetIsUnavailable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(nil)
	server.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/.well-known/openid-configuration" {
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize",
				"token_endpoint": server.URL + "/token", "jwks_uri": server.URL + "/keys",
				"response_types_supported":              []string{"code"},
				"subject_types_supported":               []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
			return
		}
		writer.WriteHeader(http.StatusServiceUnavailable)
	})
	t.Cleanup(server.Close)

	_, err := New(context.Background(), Config{
		Issuer: server.URL, ClientID: "client", Algorithms: []string{"RS256"},
		Clock: authtest.NewClock(time.Unix(1, 0)), InsecureHTTP: true,
		HTTPClient: server.Client(), DiscoveryTimeout: time.Second,
	})
	if !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("New(unavailable JWK set) error = %v", err)
	}
}

func TestNewDoesNotExposeProviderResponseText(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte("provider-secret-response"))
	}))
	t.Cleanup(server.Close)

	_, err := New(context.Background(), Config{
		Issuer: server.URL, ClientID: "client", Algorithms: []string{"RS256"},
		Clock: authtest.NewClock(time.Unix(1, 0)), InsecureHTTP: true,
		HTTPClient: server.Client(), DiscoveryTimeout: time.Second,
	})
	if !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("New() error = %v", err)
	}
	for current := err; current != nil; current = errors.Unwrap(current) {
		if strings.Contains(current.Error(), "provider-secret-response") {
			t.Fatal("New() exposed provider response text")
		}
	}

	discovery := httptest.NewServer(nil)
	baseTransport := discovery.Client().Transport
	discovery.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"issuer": discovery.URL, "authorization_endpoint": discovery.URL + "/authorize",
			"token_endpoint": discovery.URL + "/token", "jwks_uri": discovery.URL + "/keys?credential=provider-secret-transport",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	t.Cleanup(discovery.Close)
	client := discovery.Client()
	client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/keys" {
			return nil, errors.New("provider-secret-transport")
		}
		return baseTransport.RoundTrip(request)
	})
	_, err = New(context.Background(), Config{
		Issuer: discovery.URL, ClientID: "client", Algorithms: []string{"RS256"},
		Clock: authtest.NewClock(time.Unix(1, 0)), InsecureHTTP: true,
		HTTPClient: client, DiscoveryTimeout: time.Second,
	})
	for current := err; current != nil; current = errors.Unwrap(current) {
		if strings.Contains(current.Error(), "provider-secret-transport") {
			t.Fatal("New() exposed JWK transport details")
		}
	}
}

func TestNewPreservesDiscoveryDeadlineAndRejectsIssuerMismatch(t *testing.T) {
	t.Parallel()

	t.Run("deadline", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			<-request.Context().Done()
		}))
		t.Cleanup(server.Close)

		_, err := New(context.Background(), Config{
			Issuer: server.URL, ClientID: "client", Algorithms: []string{"RS256"},
			Clock: authtest.NewClock(time.Unix(1, 0)), InsecureHTTP: true,
			HTTPClient: server.Client(), DiscoveryTimeout: time.Millisecond,
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("New(deadline) error = %v", err)
		}
	})

	t.Run("issuer mismatch", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(nil)
		server.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"issuer": "https://different.example.test", "authorization_endpoint": server.URL + "/authorize",
				"token_endpoint": server.URL + "/token", "jwks_uri": server.URL + "/keys",
				"response_types_supported":              []string{"code"},
				"subject_types_supported":               []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		})
		t.Cleanup(server.Close)

		_, err := New(context.Background(), Config{
			Issuer: server.URL, ClientID: "client", Algorithms: []string{"RS256"},
			Clock: authtest.NewClock(time.Unix(1, 0)), InsecureHTTP: true,
			HTTPClient: server.Client(), DiscoveryTimeout: time.Second,
		})
		if !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Fatalf("New(issuer mismatch) error = %v", err)
		}
	})
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
	multiSigner, err := jose.NewMultiSigner([]jose.SigningKey{
		{Algorithm: jose.RS256, Key: private},
		{Algorithm: jose.RS256, Key: other},
	}, (&jose.SignerOptions{}).WithHeader("kid", "key"))
	if err != nil {
		t.Fatalf("NewMultiSigner() error = %v", err)
	}
	multiSigned, err := multiSigner.Sign([]byte(`{}`))
	if err != nil {
		t.Fatalf("Sign(multiple) error = %v", err)
	}
	if _, err := set.VerifySignature(context.Background(), multiSigned.FullSerialize()); err == nil {
		t.Fatal("VerifySignature(multiple signatures) error = nil")
	}
	missingKeyID := signCompact(t, private, "", []byte(`{}`))
	set.keys = []jose.JSONWebKey{{Key: &private.PublicKey, Algorithm: "RS256", Use: "sig"}}
	if payload, err := set.VerifySignature(context.Background(), missingKeyID); err != nil || string(payload) != `{}` {
		t.Fatalf("VerifySignature(missing kid) = %q, %v", payload, err)
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
		{name: "wrong use", body: encode(jose.JSONWebKey{Key: &private.PublicKey, KeyID: "key", Algorithm: "RS256", Use: "enc"}), maxKeys: 1},
		{name: "private key", body: encode(jose.JSONWebKey{Key: private, KeyID: "key", Algorithm: "RS256", Use: "sig"}), maxKeys: 1},
		{name: "duplicate", body: encode(valid, valid), maxKeys: 2},
		{name: "disallowed", body: encode(jose.JSONWebKey{Key: &private.PublicKey, KeyID: "key", Algorithm: "RS384", Use: "sig"}), maxKeys: 1},
		{name: "key type mismatch", body: encode(jose.JSONWebKey{Key: &ecPrivate.PublicKey, KeyID: "key", Algorithm: "RS256", Use: "sig"}), maxKeys: 1},
		{name: "omitted algorithm incompatible key", body: encode(jose.JSONWebKey{Key: &ecPrivate.PublicKey, KeyID: "key", Use: "sig"}), maxKeys: 1},
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

func TestRemoteFetchAcceptsEachConditionalValidatorIndependently(t *testing.T) {
	t.Parallel()

	set := &remoteKeySet{
		url: "https://issuer.example.test/keys",
		client: &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotModified, Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("")), Request: request,
			}, nil
		})},
	}
	for name, validators := range map[string][2]string{
		"etag":          {`"version"`, ""},
		"last modified": {"", "Wed, 15 Jul 2026 12:00:00 GMT"},
	} {
		result, err := set.fetchConditional(context.Background(), validators[0], validators[1])
		if err != nil || !result.notModified {
			t.Errorf("fetchConditional(%s) = %+v, %v", name, result, err)
		}
	}
	if _, err := set.fetchConditional(context.Background(), "", ""); err == nil {
		t.Fatal("fetchConditional(304 without validator) error = nil")
	}
}

func TestRemoteFetchAcceptsOptionalJWKMetadata(t *testing.T) {
	t.Parallel()

	private := mustRSAKey(t)
	for name, key := range map[string]jose.JSONWebKey{
		"algorithm": {Key: &private.PublicKey, KeyID: "key", Use: "sig"},
		"use":       {Key: &private.PublicKey, KeyID: "key", Algorithm: "RS256"},
		"key ID":    {Key: &private.PublicKey, Algorithm: "RS256", Use: "sig"},
		"all":       {Key: &private.PublicKey},
	} {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key}})
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			set := &remoteKeySet{
				url: "https://issuer.example.test/keys",
				client: &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
				})},
				maxBodyBytes: 1 << 20, maxKeys: 1, allowed: map[string]struct{}{"RS256": {}},
			}
			if keys, err := set.fetch(context.Background()); err != nil || len(keys) != 1 {
				t.Fatalf("fetch() = %d keys, %v", len(keys), err)
			}
		})
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
	if bounded := hardenedClient(&http.Client{Timeout: time.Hour}, 1); bounded.Timeout != 30*time.Second {
		t.Fatalf("hardenedClient(expansive timeout) = %v", bounded.Timeout)
	}
	if bounded := hardenedClient(&http.Client{Timeout: -time.Second}, 1); bounded.Timeout != 30*time.Second {
		t.Fatalf("hardenedClient(negative timeout) = %v", bounded.Timeout)
	}
	if bounded := hardenedClient(&http.Client{Timeout: 10 * time.Second}, 1); bounded.Timeout != 10*time.Second {
		t.Fatalf("hardenedClient(short timeout) = %v", bounded.Timeout)
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
	if got, err := readBounded(strings.NewReader("a"), 1); err != nil || string(got) != "a" {
		t.Fatalf("readBounded(exact) = %q, %v", got, err)
	}
	exactBody := &boundedBody{body: io.NopCloser(strings.NewReader("a")), remaining: 1}
	exactBuffer := make([]byte, 2)
	if read, err := exactBody.Read(exactBuffer); err != nil || read != 1 || exactBody.remaining != 0 {
		t.Fatalf("Read(exact) = %d, %v, remaining %d", read, err, exactBody.remaining)
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
	set.minRefreshInterval = time.Hour
	set.maxRefreshInterval = time.Hour
	minimum, maximum = set.refreshBounds()
	if minimum != time.Hour || maximum != time.Hour {
		t.Fatalf("refreshBounds(equal) = %v, %v", minimum, maximum)
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
		{name: "exact maximum", header: http.Header{"Cache-Control": {"max-age=60"}}, want: time.Minute},
		{name: "minimum", header: http.Header{"Cache-Control": {"max-age=0"}}, want: 10 * time.Second},
		{name: "exact minimum", header: http.Header{"Cache-Control": {"max-age=10"}}, want: 10 * time.Second},
		{name: "expires", header: http.Header{"Date": {date.Format(http.TimeFormat)}, "Expires": {date.Add(45 * time.Second).Format(http.TimeFormat)}}, want: 45 * time.Second},
		{name: "expires equal", header: http.Header{"Date": {date.Format(http.TimeFormat)}, "Expires": {date.Format(http.TimeFormat)}}, want: 10 * time.Second},
		{name: "expires above maximum", header: http.Header{"Date": {date.Format(http.TimeFormat)}, "Expires": {date.Add(2 * time.Minute).Format(http.TimeFormat)}}, want: time.Minute},
		{name: "zero age", header: http.Header{"Cache-Control": {"max-age=30"}, "Age": {"0"}}, want: 30 * time.Second},
		{name: "age remaining", header: http.Header{"Cache-Control": {"max-age=30"}, "Age": {"5"}}, want: 25 * time.Second},
		{name: "age exact", header: http.Header{"Cache-Control": {"max-age=30"}, "Age": {"30"}}, want: 10 * time.Second},
		{name: "age exhausted", header: http.Header{"Cache-Control": {"max-age=30"}, "Age": {"40"}}, want: 10 * time.Second},
	}
	for _, tt := range tests {
		if got := cacheLifetime(tt.header, 10*time.Second, time.Minute); got != tt.want {
			t.Errorf("cacheLifetime(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestRemoteRefreshLifetimeUsesBoundedPerInstanceJitter(t *testing.T) {
	t.Parallel()

	first := (&remoteKeySet{jitter: 1}).refreshLifetime(time.Hour, time.Minute)
	second := (&remoteKeySet{jitter: 2}).refreshLifetime(time.Hour, time.Minute)
	if first == second {
		t.Fatalf("refreshLifetime() = %v for both instances", first)
	}
	minimum := time.Hour - (time.Hour-time.Minute)/10
	if first < minimum || first > time.Hour || second < minimum || second > time.Hour {
		t.Fatalf("refreshLifetime() = %v and %v, want [%v, %v]", first, second, minimum, time.Hour)
	}
}

func TestRemoteURLValidationRejectsEachUnsafeComponent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw       string
		allowHTTP bool
		want      bool
	}{
		{raw: "https://issuer.example.test/keys", want: true},
		{raw: "http://issuer.example.test/keys", allowHTTP: true, want: true},
		{raw: "http://issuer.example.test/keys"},
		{raw: "ftp://issuer.example.test/keys", allowHTTP: true},
		{raw: "https:///keys"},
		{raw: "https://user@issuer.example.test/keys"},
		{raw: "https://issuer.example.test/keys#fragment"},
		{raw: "://"},
	}
	for _, tt := range tests {
		if got := validRemoteURL(tt.raw, tt.allowHTTP); got != tt.want {
			t.Errorf("validRemoteURL(%q, %v) = %v, want %v", tt.raw, tt.allowHTTP, got, tt.want)
		}
	}
}

func TestRemoteRefreshUpdatesConditionalValidators(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	set := &remoteKeySet{
		refreshing: true, refreshDone: done,
		etag: `"old"`, lastModified: "old date",
		minRefreshInterval: time.Second, maxRefreshInterval: time.Minute,
	}
	set.finishRefresh(time.Unix(1, 0), fetchResult{
		notModified: true,
		etag:        `"new"`, lastModified: "new date",
		header: http.Header{"Cache-Control": {"max-age=10"}},
	}, nil)

	if set.etag != `"new"` || set.lastModified != "new date" {
		t.Fatalf("conditional validators = %q, %q", set.etag, set.lastModified)
	}
	select {
	case <-done:
	default:
		t.Fatal("refresh completion was not signaled")
	}
}

func TestRemoteVerificationWaitsForRefreshAndFiltersCandidateKeys(t *testing.T) {
	t.Parallel()

	private := mustRSAKey(t)
	ecPrivate, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(EC) error = %v", err)
	}
	raw := signCompact(t, private, "key", []byte(`{"sub":"user"}`))
	signed, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatalf("ParseSigned() error = %v", err)
	}
	keys := []jose.JSONWebKey{
		{Key: &private.PublicKey, KeyID: "key", Algorithm: "RS384"},
		{Key: &ecPrivate.PublicKey, KeyID: "key"},
		{Key: &private.PublicKey, KeyID: "key", Algorithm: "RS256"},
	}
	if payload, found, verifyErr := verifyWithKeys(signed, "key", keys); verifyErr != nil || !found || string(payload) != `{"sub":"user"}` {
		t.Fatalf("verifyWithKeys() = %q, %v, %v", payload, found, verifyErr)
	}

	done := make(chan struct{})
	close(done)
	set := &remoteKeySet{
		algorithms: []jose.SignatureAlgorithm{jose.RS256},
		waiters:    make(chan struct{}, 1),
		refreshing: true, refreshDone: done,
	}
	set.clock = &refreshCompletionClock{set: set, key: &private.PublicKey}
	if payload, verifyErr := set.VerifySignature(context.Background(), raw); verifyErr != nil || string(payload) != `{"sub":"user"}` {
		t.Fatalf("VerifySignature(waited refresh) = %q, %v", payload, verifyErr)
	}
}

func TestRemoteVerificationReturnsCachedRefreshFailure(t *testing.T) {
	t.Parallel()

	private := mustRSAKey(t)
	raw := signCompact(t, private, "key", []byte(`{}`))
	set := &remoteKeySet{
		algorithms: []jose.SignatureAlgorithm{jose.RS256},
		clock:      &sequenceClock{times: []time.Time{time.Unix(2, 0), time.Unix(1, 0)}},
		waiters:    make(chan struct{}, 1), nextRefresh: time.Unix(1, 1),
		nextAttempt: time.Unix(1, 1),
		refreshErr:  errors.New("cached refresh failure"),
	}
	if _, err := set.VerifySignature(context.Background(), raw); err == nil {
		t.Fatal("VerifySignature(cached refresh failure) error = nil")
	}
}

func TestRemoteMetadataRefreshPreservesCancellationAndSanitizesFailure(t *testing.T) {
	t.Parallel()

	set := &remoteKeySet{
		issuer: "https://issuer.example.test",
		client: &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return nil, request.Context().Err()
		})},
		allowed: map[string]struct{}{"RS256": {}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := set.refresh(ctx, "", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("refresh(canceled) error = %v", err)
	}

	set.client = &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     make(http.Header), Body: io.NopCloser(strings.NewReader("provider-secret")), Request: request,
		}, nil
	})}
	_, err := set.refresh(context.Background(), "", "")
	if err == nil || strings.Contains(err.Error(), "provider-secret") {
		t.Fatalf("refresh(provider failure) error = %v", err)
	}
}

func TestRemoteRefreshReportRedactsTransportFailure(t *testing.T) {
	t.Parallel()

	private := mustRSAKey(t)
	raw := signCompact(t, private, "key", []byte(`{}`))
	set := testRemoteKeySet(t, authtest.NewClock(time.Unix(1, 0)), 1, roundTripperFunc(
		func(*http.Request) (*http.Response, error) {
			return nil, errors.New("provider-secret-transport")
		},
	))
	report := &verificationReport{}
	ctx := context.WithValue(context.Background(), verificationReportKey{}, report)
	_, _ = set.VerifySignature(ctx, raw)
	if report.err == nil || strings.Contains(report.err.Error(), "provider-secret-transport") {
		t.Fatalf("verification report error = %v", report.err)
	}
}

func TestProviderErrorRedactionPreservesOnlyStableCancellation(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := redactedProviderError(canceled, errors.New("provider detail")); !errors.Is(err, context.Canceled) {
		t.Fatalf("redactedProviderError(canceled context) = %v", err)
	}
	if err := redactedProviderError(context.Background(), fmt.Errorf("transport: %w", context.Canceled)); err != context.Canceled {
		t.Fatalf("redactedProviderError(canceled transport) = %v", err)
	}
	if err := redactedProviderError(context.Background(), fmt.Errorf("transport: %w", context.DeadlineExceeded)); err != context.DeadlineExceeded {
		t.Fatalf("redactedProviderError(deadline transport) = %v", err)
	}
}

func TestRefreshStateChangeDetection(t *testing.T) {
	t.Parallel()

	observedAttempt := time.Unix(1, 0)
	observedRefresh := time.Unix(2, 0)
	if refreshStateChanged(false, observedAttempt, observedRefresh, observedAttempt, observedRefresh) {
		t.Fatal("refreshStateChanged(unchanged) = true")
	}
	if !refreshStateChanged(true, observedAttempt, observedRefresh, observedAttempt, observedRefresh) {
		t.Fatal("refreshStateChanged(refreshing) = false")
	}
	if !refreshStateChanged(false, observedAttempt.Add(time.Second), observedRefresh, observedAttempt, observedRefresh) {
		t.Fatal("refreshStateChanged(attempt) = false")
	}
	if !refreshStateChanged(false, observedAttempt, observedRefresh.Add(time.Second), observedAttempt, observedRefresh) {
		t.Fatal("refreshStateChanged(freshness) = false")
	}
}

func TestVerifyWithKeysContinuesToMatchingKey(t *testing.T) {
	t.Parallel()

	private := mustRSAKey(t)
	compact := signCompact(t, private, "match", []byte(`{"sub":"user"}`))
	signed, err := jose.ParseSigned(compact, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatalf("ParseSigned() error = %v", err)
	}
	payload, found, err := verifyWithKeys(signed, "match", []jose.JSONWebKey{
		{Key: &private.PublicKey, KeyID: "other", Algorithm: "RS256", Use: "sig"},
		{Key: &private.PublicKey, KeyID: "match", Algorithm: "RS256", Use: "sig"},
	})
	if err != nil || !found || string(payload) != `{"sub":"user"}` {
		t.Fatalf("verifyWithKeys() = %q, %v, %v", payload, found, err)
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
		{name: "RSA missing modulus", key: &rsa.PublicKey{E: 65537}, algorithm: "RS256"},
		{name: "RSA too small", key: &rsa.PublicKey{N: new(big.Int).Lsh(big.NewInt(1), 1023), E: 65537}, algorithm: "RS256"},
		{name: "RSA maximum", key: &rsa.PublicKey{N: new(big.Int).Lsh(big.NewInt(1), 8191), E: 65537}, algorithm: "RS256", want: true},
		{name: "RSA too large", key: &rsa.PublicKey{N: new(big.Int).Lsh(big.NewInt(1), 8192), E: 65537}, algorithm: "RS256"},
		{name: "RSA mismatch", key: &p256.PublicKey, algorithm: "RS256"},
		{name: "ECDSA", key: &p256.PublicKey, algorithm: "ES256", want: true},
		{name: "ECDSA wrong key", key: &rsaPrivate.PublicKey, algorithm: "ES256"},
		{name: "ECDSA missing curve", key: &ecdsa.PublicKey{}, algorithm: "ES256"},
		{name: "ECDSA missing point", key: &ecdsa.PublicKey{Curve: elliptic.P256()}, algorithm: "ES256"},
		{name: "ECDSA wrong curve", key: &p384.PublicKey, algorithm: "ES256"},
		{name: "ECDSA unknown", key: &p256.PublicKey, algorithm: "ES999"},
		{name: "EdDSA", key: edPublic, algorithm: "EdDSA", want: true},
		{name: "EdDSA wrong length", key: ed25519.PublicKey{1}, algorithm: "EdDSA"},
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

type sequenceClock struct {
	mutex sync.Mutex
	times []time.Time
}

type refreshCompletionClock struct {
	set   *remoteKeySet
	key   *rsa.PublicKey
	calls int
}

func (clock *refreshCompletionClock) Now() time.Time {
	clock.calls++
	now := time.Unix(1, 0)
	if clock.calls == 3 {
		clock.set.keys = []jose.JSONWebKey{{Key: clock.key, KeyID: "key", Algorithm: "RS256"}}
		clock.set.nextRefresh = now.Add(time.Minute)
		clock.set.refreshing = false
	}
	return now
}

func (clock *sequenceClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	now := clock.times[0]
	clock.times = clock.times[1:]
	return now
}

type errorReadCloser struct{ err error }

func (r *errorReadCloser) Read([]byte) (int, error) { return 0, r.err }
func (*errorReadCloser) Close() error               { return nil }
