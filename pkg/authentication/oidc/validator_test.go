package oidc_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	upstreamoidc "github.com/coreos/go-oidc/v3/oidc"
	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authoidc "github.com/faustbrian/golib/pkg/authentication/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

var oidcNow = time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)

func TestValidatorAuthenticatesStrictOIDCIDToken(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	token := signIDToken(t, private, map[string]any{
		"sub": "user-1", "iss": "https://issuer.example.test",
		"aud": []string{"client-1", "other"}, "azp": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
		"auth_time": oidcNow.Add(-time.Minute).Unix(),
		"nonce":     "nonce-1", "scope": "openid profile", "tenant": "north",
		"profile": map[string]any{"locale": "fi"},
	})

	result, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok || principal.Subject() != "user-1" || principal.Method() != "oidc" {
		t.Fatalf("Authenticate() principal = (%v, %v)", principal, ok)
	}
	if principal.Issuer() != "https://issuer.example.test" ||
		!principal.AuthenticatedAt().Equal(oidcNow.Add(-time.Minute)) {
		t.Fatalf("principal issuer/time = %q/%v", principal.Issuer(), principal.AuthenticatedAt())
	}
	if strings.Join(principal.Audiences(), ",") != "client-1,other" ||
		strings.Join(principal.Scopes(), ",") != "openid,profile" ||
		strings.Join(principal.TenantHints(), ",") != "north" {
		t.Fatalf("principal protocol data = %#v/%#v/%#v", principal.Audiences(), principal.Scopes(), principal.TenantHints())
	}
	if principal.Claims()["profile"].(map[string]any)["locale"] != "fi" {
		t.Fatalf("principal claims = %#v", principal.Claims())
	}
}

func TestValidatorEnforcesOIDCClaimsAndAuthorizedParty(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow), ClockSkew: time.Minute,
	})
	valid := map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	}
	tests := []struct {
		name  string
		alter func(map[string]any)
	}{
		{name: "wrong issuer", alter: func(claims map[string]any) { claims["iss"] = "https://attacker.test" }},
		{name: "wrong audience", alter: func(claims map[string]any) { claims["aud"] = "other" }},
		{name: "expired", alter: func(claims map[string]any) { claims["exp"] = oidcNow.Add(-2 * time.Minute).Unix() }},
		{name: "not before", alter: func(claims map[string]any) { claims["nbf"] = oidcNow.Add(2 * time.Minute).Unix() }},
		{name: "missing subject", alter: func(claims map[string]any) { delete(claims, "sub") }},
		{name: "missing issued at", alter: func(claims map[string]any) { delete(claims, "iat") }},
		{name: "future issued at", alter: func(claims map[string]any) { claims["iat"] = oidcNow.Add(2 * time.Minute).Unix() }},
		{name: "multiple audience missing azp", alter: func(claims map[string]any) { claims["aud"] = []string{"client-1", "other"} }},
		{name: "multiple audience wrong azp", alter: func(claims map[string]any) { claims["aud"] = []string{"client-1", "other"}; claims["azp"] = "other" }},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			claims := cloneClaims(valid)
			tt.alter(claims)
			token := signIDToken(t, private, claims)
			if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token)); !errors.Is(err, authentication.ErrCredentialsRejected) {
				t.Fatalf("Authenticate() error = %v, want rejected", err)
			}
		})
	}
}

func TestValidatorAppliesConfiguredClockSkewToAllNumericDates(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow), ClockSkew: time.Minute,
	})
	base := map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	}
	tests := []struct {
		name  string
		alter func(map[string]any)
	}{
		{name: "recently expired", alter: func(claims map[string]any) { claims["exp"] = oidcNow.Add(-30 * time.Second).Unix() }},
		{name: "not before within skew", alter: func(claims map[string]any) { claims["nbf"] = oidcNow.Add(30 * time.Second).Unix() }},
		{name: "issued at within skew", alter: func(claims map[string]any) { claims["iat"] = oidcNow.Add(30 * time.Second).Unix() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := cloneClaims(base)
			tt.alter(claims)
			if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(signIDToken(t, private, claims))); err != nil {
				t.Fatalf("Authenticate() error = %v", err)
			}
		})
	}
}

func TestValidatorUsesNonceCallback(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	seen := ""
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
		NonceValidator: authoidc.NonceValidatorFunc(func(_ context.Context, nonce string) error {
			seen = nonce
			if nonce != "expected" {
				return errors.New("nonce rejected")
			}
			return nil
		}),
	})
	claims := map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(), "nonce": "expected",
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(signIDToken(t, private, claims))); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if seen != "expected" {
		t.Fatalf("nonce callback saw %q", seen)
	}
	claims["nonce"] = "wrong"
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(signIDToken(t, private, claims))); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("Authenticate(wrong nonce) error = %v", err)
	}
}

func TestValidatorRejectsMalformedBoundedAndDuplicateTokens(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
		MaxTokenBytes: 512, MaxClaims: 8, MaxClaimDepth: 3,
	})
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"key-1"}`))
	duplicate := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"one","sub":"two"}`))
	tooMany := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(), "a": 1, "b": 2, "c": 3, "d": 4,
	})
	tests := []string{
		"not-a-token", header + "." + duplicate + ".signature", tooMany,
		strings.Repeat("x", 513),
	}
	for _, token := range tests {
		if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token)); !errors.Is(err, authentication.ErrCredentialsRejected) && !errors.Is(err, authentication.ErrCredentialsInvalid) {
			t.Errorf("Authenticate() error = %v", err)
		}
	}
}

func TestValidatorRejectsInvalidPrincipalClaimShapes(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	base := map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	}
	tests := []struct {
		name  string
		alter func(map[string]any)
	}{
		{name: "numeric scope", alter: func(claims map[string]any) { claims["scope"] = 1 }},
		{name: "empty tenant", alter: func(claims map[string]any) { claims["tenant"] = "" }},
		{name: "zero auth time", alter: func(claims map[string]any) { claims["auth_time"] = 0 }},
		{name: "string auth time", alter: func(claims map[string]any) { claims["auth_time"] = "yesterday" }},
		{name: "numeric authorized party", alter: func(claims map[string]any) { claims["azp"] = 42 }},
		{name: "string not before", alter: func(claims map[string]any) { claims["nbf"] = "tomorrow" }},
		{name: "oversized not before", alter: func(claims map[string]any) { claims["nbf"] = 1e300 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := cloneClaims(base)
			tt.alter(claims)
			token := signIDToken(t, private, claims)
			if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, authentication.ErrCredentialsRejected) {
				t.Fatalf("ValidateBearer() error = %v", err)
			}
		})
	}
}

func TestValidatorHonorsCancellationAndConfiguration(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := validator.Authenticate(ctx, authentication.NewBearerCredential("token")); !errors.Is(err, context.Canceled) || !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Authenticate(canceled) error = %v", err)
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBasicCredential("user", "password")); !errors.Is(err, authentication.ErrCredentialsInvalid) {
		t.Fatalf("Authenticate(wrong kind) error = %v", err)
	}

	keySet := &upstreamoidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&private.PublicKey}}
	invalid := []authoidc.Config{
		{},
		{Issuer: "https://issuer.example.test?tenant=north", ClientID: "client", Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow)},
		{Issuer: "issuer", ClientID: "client", Algorithms: []string{"none"}, Clock: authtest.NewClock(oidcNow)},
		{Issuer: "issuer", ClientID: "client", Algorithms: []string{"HS256"}, Clock: authtest.NewClock(oidcNow)},
		{Issuer: "issuer", ClientID: "client", Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow), MaxClaims: authentication.MaxClaims + 1},
	}
	for index, configuration := range invalid {
		if _, err := authoidc.NewWithKeySet(configuration, keySet); !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Errorf("NewWithKeySet(invalid %d) error = %v", index, err)
		}
	}
}

func staticValidator(t *testing.T, private *rsa.PrivateKey, configuration authoidc.Config) *authoidc.Validator {
	t.Helper()
	keySet := &upstreamoidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&private.PublicKey}}
	validator, err := authoidc.NewWithKeySet(configuration, keySet)
	if err != nil {
		t.Fatalf("NewWithKeySet() error = %v", err)
	}
	return validator
}

func rsaKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	return private
}

func signIDToken(t *testing.T, private *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	options := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "key-1")
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
		t.Fatalf("Signer.Sign() error = %v", err)
	}
	compact, err := signed.CompactSerialize()
	if err != nil {
		t.Fatalf("CompactSerialize() error = %v", err)
	}
	return compact
}

func cloneClaims(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for name, value := range source {
		clone[name] = value
	}
	return clone
}
