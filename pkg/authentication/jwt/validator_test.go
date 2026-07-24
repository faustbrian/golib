package jwt_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authjwt "github.com/faustbrian/golib/pkg/authentication/jwt"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	upstreamjwt "github.com/lestrrat-go/jwx/v3/jwt"
)

var jwtNow = time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)

func TestValidatorAuthenticatesStrictJWT(t *testing.T) {
	t.Parallel()

	keys, signer := rsaKeys(t, "key-1", jwa.RS256())
	validator := newValidator(t, keys, authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()},
		Clock:      authtest.NewClock(jwtNow),
	})
	token := signedToken(t, signer, jwa.RS256(), map[string]any{
		"sub": "service-1", "iss": "https://issuer.example.test",
		"aud": []string{"orders", "billing"},
		"iat": jwtNow, "exp": jwtNow.Add(time.Hour),
		"scope": "orders:read orders:write", "tenant": []string{"north", "south"},
		"custom": map[string]any{"region": "eu"},
	})

	result, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token))
	if err != nil {
		_, parseErr := upstreamjwt.Parse([]byte(token),
			upstreamjwt.WithKeySet(keys),
			upstreamjwt.WithIssuer("https://issuer.example.test"),
			upstreamjwt.WithAudience("orders"),
			upstreamjwt.WithClock(authtest.NewClock(jwtNow)),
			upstreamjwt.WithRequiredClaim("sub"),
			upstreamjwt.WithRequiredClaim("iat"),
			upstreamjwt.WithRequiredClaim("exp"),
		)
		t.Fatalf("Authenticate() error = %v; upstream parse = %v", err, parseErr)
	}
	principal, ok := result.Principal()
	if !ok || principal.Subject() != "service-1" || principal.Method() != "jwt" {
		t.Fatalf("Authenticate() principal = (%v, %v)", principal, ok)
	}
	if principal.Issuer() != "https://issuer.example.test" || !principal.AuthenticatedAt().Equal(jwtNow) {
		t.Fatalf("principal issuer/time = %q/%v", principal.Issuer(), principal.AuthenticatedAt())
	}
	if strings.Join(principal.Audiences(), ",") != "orders,billing" || strings.Join(principal.Scopes(), ",") != "orders:read,orders:write" {
		t.Fatalf("principal audiences/scopes = %#v/%#v", principal.Audiences(), principal.Scopes())
	}
	if strings.Join(principal.TenantHints(), ",") != "north,south" {
		t.Fatalf("principal tenant hints = %#v", principal.TenantHints())
	}
	if got := principal.Claims()["custom"].(map[string]any)["region"]; got != "eu" {
		t.Fatalf("principal custom claim = %v", got)
	}
}

func TestValidatorRejectsInvalidJWTTrustDecisions(t *testing.T) {
	t.Parallel()

	keys, signer := rsaKeys(t, "key-1", jwa.RS256())
	validator := newValidator(t, keys, authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()},
		Clock:      authtest.NewClock(jwtNow), Skew: time.Minute,
	})
	valid := map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": jwtNow, "exp": jwtNow.Add(time.Hour),
	}

	tests := []struct {
		name  string
		alter func(map[string]any)
	}{
		{name: "wrong issuer", alter: func(claims map[string]any) { claims["iss"] = "https://attacker.test" }},
		{name: "wrong audience", alter: func(claims map[string]any) { claims["aud"] = "billing" }},
		{name: "expired", alter: func(claims map[string]any) { claims["exp"] = jwtNow.Add(-2 * time.Minute) }},
		{name: "not before", alter: func(claims map[string]any) { claims["nbf"] = jwtNow.Add(2 * time.Minute) }},
		{name: "future issued at", alter: func(claims map[string]any) { claims["iat"] = jwtNow.Add(2 * time.Minute) }},
		{name: "missing subject", alter: func(claims map[string]any) { delete(claims, "sub") }},
		{name: "missing expiration", alter: func(claims map[string]any) { delete(claims, "exp") }},
		{name: "empty issuer", alter: func(claims map[string]any) { claims["iss"] = "" }},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			claims := cloneMap(valid)
			tt.alter(claims)
			token := signedToken(t, signer, jwa.RS256(), claims)
			_, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token))
			if !errors.Is(err, authentication.ErrCredentialsRejected) {
				t.Fatalf("Authenticate() error = %v, want rejected", err)
			}
			if strings.Contains(err.Error(), token) {
				t.Fatal("authentication error disclosed token")
			}
		})
	}
}

func TestValidatorRejectsAlgorithmKeyAndHeaderAttacks(t *testing.T) {
	t.Parallel()

	keys, signer := rsaKeys(t, "key-1", jwa.RS256())
	validator := newValidator(t, keys, authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, Clock: authtest.NewClock(jwtNow),
	})
	claims := map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": jwtNow, "exp": jwtNow.Add(time.Hour),
	}
	valid := signedToken(t, signer, jwa.RS256(), claims)
	parts := strings.Split(valid, ".")
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","kid":"key-1","typ":"JWT"}`))
	unknownKeyHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"unknown","typ":"JWT"}`))
	critical := signedTokenWithHeaders(t, signer, jwa.RS256(), claims, map[string]any{
		"alg": "RS256", "kid": "key-1", "typ": "JWT", "crit": []string{"custom"}, "custom": true,
	})
	tests := map[string]string{
		"none":             noneHeader + "." + parts[1] + ".",
		"unknown key":      unknownKeyHeader + "." + parts[1] + "." + parts[2],
		"unknown critical": critical,
		"malformed":        "not-a-jwt",
	}
	for name, token := range tests {
		token := token
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token)); !errors.Is(err, authentication.ErrCredentialsRejected) {
				t.Fatalf("Authenticate() error = %v, want rejected", err)
			}
		})
	}
}

func TestValidatorRejectsDuplicateAndOversizedClaims(t *testing.T) {
	t.Parallel()

	keys, signer := rsaKeys(t, "key-1", jwa.RS256())
	validator := newValidator(t, keys, authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, Clock: authtest.NewClock(jwtNow),
		MaxTokenBytes: 512, MaxClaims: 8, MaxClaimDepth: 3,
	})
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"key-1","typ":"JWT"}`))
	duplicatePayload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"one","sub":"two"}`))
	tooMany := signedToken(t, signer, jwa.RS256(), map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": jwtNow, "exp": jwtNow.Add(time.Hour),
		"a": 1, "b": 2, "c": 3, "d": 4,
	})
	tooDeep := signedToken(t, signer, jwa.RS256(), map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": jwtNow, "exp": jwtNow.Add(time.Hour), "nested": map[string]any{"a": map[string]any{"b": map[string]any{"c": true}}},
	})
	tests := map[string]string{
		"duplicate": header + "." + duplicatePayload + ".signature",
		"too many":  tooMany,
		"too deep":  tooDeep,
		"oversized": strings.Repeat("x", 513),
	}
	for name, token := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token)); !errors.Is(err, authentication.ErrCredentialsRejected) && !errors.Is(err, authentication.ErrCredentialsInvalid) {
				t.Fatalf("Authenticate() error = %v, want rejected or invalid", err)
			}
		})
	}
}

func TestValidatorRejectsInvalidPrincipalClaims(t *testing.T) {
	t.Parallel()

	keys, signer := rsaKeys(t, "key-1", jwa.RS256())
	validator := newValidator(t, keys, authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, Clock: authtest.NewClock(jwtNow),
	})
	base := map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": jwtNow, "exp": jwtNow.Add(time.Hour),
	}
	tests := []func(map[string]any){
		func(claims map[string]any) { claims["scope"] = 1 },
		func(claims map[string]any) { claims["tenant"] = "" },
	}
	for _, alter := range tests {
		claims := cloneMap(base)
		alter(claims)
		token := signedToken(t, signer, jwa.RS256(), claims)
		if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, authentication.ErrCredentialsRejected) {
			t.Fatalf("ValidateBearer() error = %v", err)
		}
	}
}

func TestValidatorHonorsCancellationAndConfigurationBounds(t *testing.T) {
	t.Parallel()

	keys, _ := rsaKeys(t, "key-1", jwa.RS256())
	validator := newValidator(t, keys, authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, Clock: authtest.NewClock(jwtNow),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := validator.Authenticate(ctx, authentication.NewBearerCredential("token")); !errors.Is(err, context.Canceled) || !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Authenticate(canceled) error = %v", err)
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBasicCredential("user", "password")); !errors.Is(err, authentication.ErrCredentialsInvalid) {
		t.Fatalf("Authenticate(wrong kind) error = %v", err)
	}

	invalid := []authjwt.Config{
		{},
		{Issuer: "issuer", Audience: "audience", Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, KeySet: keys},
		{Issuer: "issuer", Audience: "audience", Algorithms: []jwa.SignatureAlgorithm{jwa.NoSignature()}, KeySet: keys, Clock: authtest.NewClock(jwtNow)},
		{Issuer: "issuer", Audience: "audience", Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, KeySet: keys, Clock: authtest.NewClock(jwtNow), MaxClaims: authentication.MaxClaims + 1},
	}
	for index, configuration := range invalid {
		if _, err := authjwt.New(configuration); !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Errorf("New(invalid %d) error = %v", index, err)
		}
	}
}

func newValidator(t *testing.T, keys jwk.Set, configuration authjwt.Config) *authjwt.Validator {
	t.Helper()
	configuration.KeySet = keys
	validator, err := authjwt.New(configuration)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return validator
}

func rsaKeys(t *testing.T, keyID string, algorithm jwa.SignatureAlgorithm) (jwk.Set, jwk.Key) {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	signer, err := jwk.Import(private)
	if err != nil {
		t.Fatalf("jwk.Import(private) error = %v", err)
	}
	if err := signer.Set(jwk.KeyIDKey, keyID); err != nil {
		t.Fatalf("signer.Set(kid) error = %v", err)
	}
	if err := signer.Set(jwk.AlgorithmKey, algorithm); err != nil {
		t.Fatalf("signer.Set(alg) error = %v", err)
	}
	public, err := signer.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey() error = %v", err)
	}
	set := jwk.NewSet()
	if err := set.AddKey(public); err != nil {
		t.Fatalf("AddKey() error = %v", err)
	}
	return set, signer
}

func signedToken(t *testing.T, signer jwk.Key, algorithm jwa.SignatureAlgorithm, claims map[string]any) string {
	t.Helper()
	token := upstreamjwt.New()
	for name, value := range claims {
		if err := token.Set(name, value); err != nil {
			t.Fatalf("Token.Set(%q) error = %v", name, err)
		}
	}
	signed, err := upstreamjwt.Sign(token, upstreamjwt.WithKey(algorithm, signer))
	if err != nil {
		t.Fatalf("jwt.Sign() error = %v", err)
	}
	return string(signed)
}

func signedTokenWithHeaders(t *testing.T, signer jwk.Key, algorithm jwa.SignatureAlgorithm, claims, headers map[string]any) string {
	t.Helper()
	token := upstreamjwt.New()
	for name, value := range claims {
		if err := token.Set(name, value); err != nil {
			t.Fatalf("Token.Set(%q) error = %v", name, err)
		}
	}
	protected := jws.NewHeaders()
	for name, value := range headers {
		if err := protected.Set(name, value); err != nil {
			t.Fatalf("Headers.Set(%q) error = %v", name, err)
		}
	}
	signed, err := upstreamjwt.Sign(token, upstreamjwt.WithKey(algorithm, signer, jws.WithProtectedHeaders(protected)))
	if err != nil {
		t.Fatalf("jwt.Sign() error = %v", err)
	}
	return string(signed)
}

func cloneMap(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for name, value := range source {
		clone[name] = value
	}
	return clone
}
