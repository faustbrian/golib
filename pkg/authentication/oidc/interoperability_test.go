package oidc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authoidc "github.com/faustbrian/golib/pkg/authentication/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

func TestGoogleProviderMetadataSnapshot(t *testing.T) {
	t.Parallel()

	metadata, err := os.ReadFile("testdata/google-openid-configuration-2026-08-09.json")
	if err != nil {
		t.Fatalf("ReadFile(Google metadata) error = %v", err)
	}
	private := rsaKey(t)
	keys, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &private.PublicKey, KeyID: "google-snapshot", Algorithm: "RS256", Use: "sig",
	}}})
	if err != nil {
		t.Fatalf("Marshal(JWK set) error = %v", err)
	}
	discoveryRequests := 0
	keyRequests := 0
	client := &http.Client{Transport: externalRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body := metadata
		contentType := "application/json"
		switch request.URL.Host {
		case "accounts.google.com":
			discoveryRequests++
		case "www.googleapis.com":
			keyRequests++
			body = keys
			contentType = "application/jwk-set+json"
		default:
			t.Fatalf("unexpected provider host %q", request.URL.Host)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}},
			Body: io.NopCloser(bytes.NewReader(body)), Request: request,
		}, nil
	})}
	validator, err := authoidc.New(context.Background(), authoidc.Config{
		Issuer: "https://accounts.google.com", ClientID: "client", Algorithms: []string{"RS256"},
		Clock: authtest.NewClock(oidcNow), HTTPClient: client, DiscoveryTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("New(Google snapshot) error = %v", err)
	}
	token := signIDTokenWithKeyID(t, private, "google-snapshot", map[string]any{
		"iss": "https://accounts.google.com", "sub": "user", "aud": "client",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	})
	if _, err := validator.ValidateBearer(context.Background(), token); err != nil {
		t.Fatalf("ValidateBearer(Google snapshot) error = %v", err)
	}
	if discoveryRequests != 1 || keyRequests != 1 {
		t.Fatalf("provider requests = discovery %d, keys %d", discoveryRequests, keyRequests)
	}
}

func TestOpenIDConnectCoreIDTokenClaimVector(t *testing.T) {
	t.Parallel()

	// OpenID Connect Core 1.0, Section 2: non-normative ID-token claims.
	private := rsaKey(t)
	now := time.Unix(1_311_281_000, 0).UTC()
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://server.example.com", ClientID: "s6BhdRkqt3",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(now),
	})
	encoded, err := os.ReadFile("testdata/oidc-core-section-2-claims.json")
	if err != nil {
		t.Fatalf("ReadFile(conformance vector) error = %v", err)
	}
	claims := map[string]any{}
	if err := json.Unmarshal(encoded, &claims); err != nil {
		t.Fatalf("Unmarshal(conformance vector) error = %v", err)
	}
	token := signIDToken(t, private, claims)
	result, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok || principal.Subject() != "24400320" ||
		!principal.AuthenticatedAt().Equal(time.Unix(1_311_280_969, 0).UTC()) ||
		principal.Claims()["acr"] != "urn:mace:incommon:iap:silver" {
		t.Fatalf("Authenticate() principal = (%v, %v)", principal, ok)
	}
}

func TestRepresentativeProviderMetadataProfiles(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		name          string
		scopes        []string
		responseTypes []string
		subjectTypes  []string
		algorithms    []string
	}{
		{name: "Google-style", scopes: []string{"openid", "profile", "email"}, responseTypes: []string{"code", "id_token"}, subjectTypes: []string{"public"}, algorithms: []string{"RS256"}},
		{name: "Keycloak-style", scopes: []string{"openid"}, responseTypes: []string{"code", "id_token", "code id_token"}, subjectTypes: []string{"public", "pairwise"}, algorithms: []string{"RS256", "PS256"}},
		{name: "Dex-style", responseTypes: []string{"code"}, subjectTypes: []string{"public"}, algorithms: []string{"RS256"}},
	}
	for _, profile := range profiles {
		t.Run(profile.name, func(t *testing.T) {
			private := rsaKey(t)
			server := httptest.NewServer(nil)
			server.Config.Handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/.well-known/openid-configuration" {
					metadata := map[string]any{
						"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize",
						"token_endpoint": server.URL + "/token", "jwks_uri": server.URL + "/keys",
						"response_types_supported":              profile.responseTypes,
						"subject_types_supported":               profile.subjectTypes,
						"id_token_signing_alg_values_supported": profile.algorithms,
					}
					if profile.scopes != nil {
						metadata["scopes_supported"] = profile.scopes
					}
					writeExternalJSONResponse(writer, metadata)
					return
				}
				writeExternalJSONResponse(writer, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
					Key: &private.PublicKey, KeyID: "current", Algorithm: "RS256", Use: "sig",
				}}})
			})
			t.Cleanup(server.Close)

			validator, err := authoidc.New(context.Background(), authoidc.Config{
				Issuer: server.URL, ClientID: "client", Algorithms: []string{"RS256"},
				Clock: authtest.NewClock(oidcNow), InsecureHTTP: true,
				HTTPClient: server.Client(), DiscoveryTimeout: 5 * time.Second,
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			token := signIDTokenWithKeyID(t, private, "current", map[string]any{
				"iss": server.URL, "sub": "user", "aud": "client",
				"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
			})
			if _, err := validator.ValidateBearer(context.Background(), token); err != nil {
				t.Fatalf("ValidateBearer() error = %v", err)
			}
		})
	}
}

type externalRoundTripperFunc func(*http.Request) (*http.Response, error)

func (function externalRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
