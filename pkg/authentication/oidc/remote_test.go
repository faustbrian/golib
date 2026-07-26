package oidc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authoidc "github.com/faustbrian/golib/pkg/authentication/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

func TestDiscoveryValidatorRotatesKeysAndKeepsStaleKeysOnOutage(t *testing.T) {
	t.Parallel()

	first := rsaKey(t)
	second := rsaKey(t)
	state := &oidcServerState{keyID: "first", publicKey: &first.PublicKey}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	t.Cleanup(server.Close)
	state.issuer = server.URL
	clock := authtest.NewClock(oidcNow)

	validator, err := authoidc.New(context.Background(), authoidc.Config{
		Issuer: server.URL, ClientID: "client-1", Algorithms: []string{"RS256"},
		Clock: clock, InsecureHTTP: true,
		HTTPClient: server.Client(), DiscoveryTimeout: time.Second,
		MaxHTTPBodyBytes: 32 * 1024, MaxKeys: 8,
		MinRefreshInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	claims := map[string]any{
		"sub": "user", "iss": server.URL, "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	}
	firstToken := signIDTokenWithKeyID(t, first, "first", claims)
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(firstToken)); err != nil {
		t.Fatalf("Authenticate(first) error = %v", err)
	}

	state.set("second", &second.PublicKey, http.StatusOK)
	clock.Advance(time.Second)
	secondToken := signIDTokenWithKeyID(t, second, "second", claims)
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(secondToken)); err != nil {
		t.Fatalf("Authenticate(rotated) error = %v", err)
	}

	state.set("second", &second.PublicKey, http.StatusServiceUnavailable)
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(secondToken)); err != nil {
		t.Fatalf("Authenticate(cached during outage) error = %v", err)
	}
	third := rsaKey(t)
	thirdToken := signIDTokenWithKeyID(t, third, "third", claims)
	clock.Advance(time.Second)
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(thirdToken)); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Authenticate(unknown during outage) error = %v", err)
	}
}

func TestConcurrentOIDCAuthenticationAndRotationAreRaceSafe(t *testing.T) {
	t.Parallel()

	first := rsaKey(t)
	second := rsaKey(t)
	state := &oidcServerState{keyID: "first", publicKey: &first.PublicKey}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	t.Cleanup(server.Close)
	state.issuer = server.URL
	validator, err := authoidc.New(context.Background(), authoidc.Config{
		Issuer: server.URL, ClientID: "client-1", Algorithms: []string{"RS256"},
		Clock: authtest.NewClock(oidcNow), InsecureHTTP: true,
		HTTPClient: server.Client(), DiscoveryTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	claims := map[string]any{
		"sub": "user", "iss": server.URL, "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	}
	tokens := []string{
		signIDTokenWithKeyID(t, first, "first", claims),
		signIDTokenWithKeyID(t, second, "second", claims),
	}
	var group sync.WaitGroup
	for index := range 4 {
		group.Add(1)
		go func(offset int) {
			defer group.Done()
			for attempt := range 50 {
				_, err := validator.ValidateBearer(context.Background(), tokens[(offset+attempt)%len(tokens)])
				if err != nil && !errors.Is(err, authentication.ErrCredentialsRejected) {
					t.Errorf("ValidateBearer() error = %v", err)
					return
				}
			}
		}(index)
	}
	for attempt := range 20 {
		if attempt%2 == 0 {
			state.set("first", &first.PublicKey, http.StatusOK)
		} else {
			state.set("second", &second.PublicKey, http.StatusOK)
		}
	}
	group.Wait()
}

func TestDiscoveryAndJWKRequestsAreBoundedAndCancelable(t *testing.T) {
	t.Parallel()

	oversized := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(make([]byte, 1024))
	}))
	t.Cleanup(oversized.Close)
	if _, err := authoidc.New(context.Background(), authoidc.Config{
		Issuer: oversized.URL, ClientID: "client", Algorithms: []string{"RS256"},
		Clock: authtest.NewClock(oidcNow), InsecureHTTP: true,
		HTTPClient: oversized.Client(), MaxHTTPBodyBytes: 64,
	}); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("New(oversized discovery) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := authoidc.New(ctx, authoidc.Config{
		Issuer: oversized.URL, ClientID: "client", Algorithms: []string{"RS256"},
		Clock: authtest.NewClock(oidcNow), InsecureHTTP: true,
		HTTPClient: oversized.Client(),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("New(canceled) error = %v", err)
	}
}

type oidcServerState struct {
	mutex     sync.RWMutex
	issuer    string
	keyID     string
	publicKey any
	status    int
}

func (state *oidcServerState) set(keyID string, publicKey any, status int) {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	state.keyID = keyID
	state.publicKey = publicKey
	state.status = status
}

func (state *oidcServerState) handler(writer http.ResponseWriter, request *http.Request) {
	state.mutex.RLock()
	defer state.mutex.RUnlock()
	if request.URL.Path == "/.well-known/openid-configuration" {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"issuer": state.issuer, "authorization_endpoint": state.issuer + "/authorize",
			"token_endpoint": state.issuer + "/token", "jwks_uri": state.issuer + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
		return
	}
	if state.status != 0 && state.status != http.StatusOK {
		writer.WriteHeader(state.status)
		return
	}
	key := jose.JSONWebKey{Key: state.publicKey, KeyID: state.keyID, Algorithm: "RS256", Use: "sig"}
	_ = json.NewEncoder(writer).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key}})
}

func signIDTokenWithKeyID(t testing.TB, private any, keyID string, claims map[string]any) string {
	t.Helper()
	options := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: private}, options)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
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
