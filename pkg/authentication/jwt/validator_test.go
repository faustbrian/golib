package jwt_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
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
	var failure *authentication.Failure
	claims := cloneMap(valid)
	claims["exp"] = jwtNow.Add(-2 * time.Minute)
	if _, err := validator.ValidateBearer(context.Background(), signedToken(t, signer, jwa.RS256(), claims)); !errors.As(err, &failure) {
		t.Fatalf("ValidateBearer() error = %v, want *authentication.Failure", err)
	}
}

func TestValidatorRejectsMalformedNumericDates(t *testing.T) {
	t.Parallel()

	keys, signer := rsaKeys(t, "key-1", jwa.RS256())
	validator := newValidator(t, keys, authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, Clock: authtest.NewClock(jwtNow),
	})
	payloads := map[string]string{
		"expiration":              `{"sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":1700000000,"exp":"later"}`,
		"quoted expiration epoch": `{"sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":1700000000,"exp":"4102444800"}`,
		"RFC3339 expiration":      `{"sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":1700000000,"exp":"2100-01-01T00:00:00Z"}`,
		"fractional issued at":    `{"sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":1784116800.5,"exp":4102444800}`,
		"exponent expiration":     `{"sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":1784116800,"exp":4.1024448e9}`,
		"not before":              `{"sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":1700000000,"nbf":[],"exp":4102444800}`,
		"quoted not before epoch": `{"sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":1700000000,"nbf":"1700000000","exp":4102444800}`,
		"issued at":               `{"sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":"now","exp":4102444800}`,
		"quoted issued at epoch":  `{"sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":"1700000000","exp":4102444800}`,
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			token := signedPayload(t, signer, jwa.RS256(), []byte(payload))
			if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, authentication.ErrCredentialsRejected) || !errors.Is(err, upstreamjwt.ParseError()) {
				t.Fatalf("ValidateBearer() error = %v, want rejected parse error", err)
			}
		})
	}
}

func TestValidatorHonorsExactNumericDateBoundaries(t *testing.T) {
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
	tests := []struct {
		name      string
		alter     func(map[string]any)
		wantError bool
	}{
		{name: "expiration one second ahead", alter: func(claims map[string]any) { claims["exp"] = jwtNow.Add(time.Second) }},
		{name: "expiration at now", alter: func(claims map[string]any) { claims["exp"] = jwtNow }, wantError: true},
		{name: "not before at now", alter: func(claims map[string]any) { claims["nbf"] = jwtNow }},
		{name: "not before one second ahead", alter: func(claims map[string]any) { claims["nbf"] = jwtNow.Add(time.Second) }, wantError: true},
		{name: "issued at now", alter: func(claims map[string]any) { claims["iat"] = jwtNow }},
		{name: "issued at one second ahead", alter: func(claims map[string]any) { claims["iat"] = jwtNow.Add(time.Second) }, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := cloneMap(base)
			tt.alter(claims)
			_, err := validator.ValidateBearer(context.Background(), signedToken(t, signer, jwa.RS256(), claims))
			if (err != nil) != tt.wantError {
				t.Fatalf("ValidateBearer() error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}

func TestValidatorReadsValidationClockOnce(t *testing.T) {
	t.Parallel()

	keys, signer := rsaKeys(t, "key-1", jwa.RS256())
	clock := &countingClock{now: jwtNow}
	validator := newValidator(t, keys, authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, Clock: clock,
	})
	token := signedToken(t, signer, jwa.RS256(), map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": jwtNow, "nbf": jwtNow, "exp": jwtNow.Add(time.Hour),
	})
	if _, err := validator.ValidateBearer(context.Background(), token); err != nil {
		t.Fatalf("ValidateBearer() error = %v", err)
	}
	if got := clock.calls.Load(); got != 1 {
		t.Fatalf("Clock.Now() calls = %d, want 1", got)
	}
}

func TestValidatorPreservesPrivateJSONNumbersLosslessly(t *testing.T) {
	t.Parallel()

	keys, signer := rsaKeys(t, "key-1", jwa.RS256())
	validator := newValidator(t, keys, authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, Clock: authtest.NewClock(jwtNow),
	})
	payload := []byte(`{"sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":1784116800,"exp":4102444800,"private_number":9007199254740993}`)
	principal, err := validator.ValidateBearer(context.Background(), signedPayload(t, signer, jwa.RS256(), payload))
	if err != nil {
		t.Fatalf("ValidateBearer() error = %v", err)
	}
	number, ok := principal.Claims()["private_number"].(json.Number)
	if !ok || number.String() != "9007199254740993" {
		t.Fatalf("private_number = %T(%v), want exact json.Number", principal.Claims()["private_number"], principal.Claims()["private_number"])
	}
}

func TestValidatorEnforcesSubjectAndRequiredClaimPolicy(t *testing.T) {
	t.Parallel()

	keys, signer := rsaKeys(t, "key-1", jwa.RS256())
	validator := newValidator(t, keys, authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, Clock: authtest.NewClock(jwtNow),
		Subjects: []string{"service-a", "service-b"}, RequiredClaims: []string{"tenant", "jti"},
	})
	base := map[string]any{
		"sub": "service-a", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": jwtNow, "exp": jwtNow.Add(time.Hour), "tenant": "north", "jti": "request-1",
	}
	if _, err := validator.ValidateBearer(context.Background(), signedToken(t, signer, jwa.RS256(), base)); err != nil {
		t.Fatalf("ValidateBearer(allowed subject) error = %v", err)
	}

	tests := []struct {
		name  string
		alter func(map[string]any)
	}{
		{name: "subject outside allowlist", alter: func(claims map[string]any) { claims["sub"] = "service-c" }},
		{name: "missing custom required claim", alter: func(claims map[string]any) { delete(claims, "jti") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := cloneMap(base)
			tt.alter(claims)
			if _, err := validator.ValidateBearer(context.Background(), signedToken(t, signer, jwa.RS256(), claims)); !errors.Is(err, authentication.ErrCredentialsRejected) {
				t.Fatalf("ValidateBearer() error = %v, want rejected", err)
			}
		})
	}
}

func TestValidatorPreservesSafeStandardsErrorCategories(t *testing.T) {
	t.Parallel()

	keys, signer := rsaKeys(t, "key-1", jwa.RS256())
	validator := newValidator(t, keys, authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, Clock: authtest.NewClock(jwtNow),
		RequiredClaims: []string{"tenant"},
	})
	base := map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": jwtNow, "exp": jwtNow.Add(time.Hour), "tenant": "north",
	}
	tests := []struct {
		name  string
		alter func(map[string]any)
		want  error
	}{
		{name: "expired", alter: func(claims map[string]any) { claims["exp"] = jwtNow }, want: upstreamjwt.TokenExpiredError()},
		{name: "not yet valid", alter: func(claims map[string]any) { claims["nbf"] = jwtNow.Add(time.Second) }, want: upstreamjwt.TokenNotYetValidError()},
		{name: "issued in future", alter: func(claims map[string]any) { claims["iat"] = jwtNow.Add(time.Second) }, want: upstreamjwt.InvalidIssuedAtError()},
		{name: "issuer mismatch", alter: func(claims map[string]any) { claims["iss"] = "https://sensitive.example.test/private" }, want: upstreamjwt.InvalidIssuerError()},
		{name: "audience mismatch", alter: func(claims map[string]any) { claims["aud"] = "sensitive-audience" }, want: upstreamjwt.InvalidAudienceError()},
		{name: "required claim missing", alter: func(claims map[string]any) { delete(claims, "tenant") }, want: upstreamjwt.MissingRequiredClaimError()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := cloneMap(base)
			tt.alter(claims)
			token := signedToken(t, signer, jwa.RS256(), claims)
			_, err := validator.ValidateBearer(context.Background(), token)
			if !errors.Is(err, authentication.ErrCredentialsRejected) || !errors.Is(err, tt.want) {
				t.Fatalf("ValidateBearer() error = %v, want rejected and %v", err, tt.want)
			}
			if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("ValidateBearer() disclosed sensitive data: %v", err)
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
	remoteKeyReference := signedTokenWithHeaders(t, signer, jwa.RS256(), claims, map[string]any{
		"alg": "RS256", "kid": "key-1", "jku": "https://attacker.example.test/keys",
	})
	embeddedKey, err := signer.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey() error = %v", err)
	}
	embeddedKeyReference := signedTokenWithHeaders(t, signer, jwa.RS256(), claims, map[string]any{
		"alg": "RS256", "kid": "key-1", "jwk": embeddedKey,
	})
	tests := map[string]string{
		"none":                   noneHeader + "." + parts[1] + ".",
		"unknown key":            unknownKeyHeader + "." + parts[1] + "." + parts[2],
		"unknown critical":       critical,
		"remote key reference":   remoteKeyReference,
		"embedded key reference": embeddedKeyReference,
		"malformed":              "not-a-jwt",
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

func TestValidatorRejectsNonCanonicalBase64TruncationAndNestedPayloads(t *testing.T) {
	t.Parallel()

	keys, signer := rsaKeys(t, "key-1", jwa.RS256())
	validator := newValidator(t, keys, authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, Clock: authtest.NewClock(jwtNow),
	})
	valid := signedToken(t, signer, jwa.RS256(), map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": jwtNow, "exp": jwtNow.Add(time.Hour),
	})
	parts := strings.Split(valid, ".")
	tests := map[string]string{
		"noncanonical header base64url":    nonCanonicalBase64URL(parts[0]) + "." + parts[1] + "." + parts[2],
		"padded signature base64url":       parts[0] + "." + parts[1] + "." + parts[2] + "=",
		"noncanonical signature base64url": parts[0] + "." + parts[1] + "." + nonCanonicalBase64URL(parts[2]),
		"truncated signature":              parts[0] + "." + parts[1] + "." + parts[2][:len(parts[2])-1],
		"nested compact payload":           signedPayload(t, signer, jwa.RS256(), []byte(`"`+valid+`"`)),
		"JWE compact serialization":        "a.b.c.d.e",
	}
	for name, token := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, authentication.ErrCredentialsRejected) {
				t.Fatalf("ValidateBearer() error = %v, want rejected", err)
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
		"duplicate":     header + "." + duplicatePayload + ".signature",
		"too many":      tooMany,
		"too deep":      tooDeep,
		"oversized":     strings.Repeat("x", 513),
		"invalid UTF-8": signedPayload(t, signer, jwa.RS256(), []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}),
		"huge number": signedPayload(t, signer, jwa.RS256(), []byte(
			`{"sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":1784116800,"exp":4102444800,"private_number":`+strings.Repeat("9", 129)+`}`,
		)),
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
		{Issuer: "issuer", Audience: "audience", Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, KeySet: keys, Clock: authtest.NewClock(jwtNow), Subjects: []string{""}},
		{Issuer: "issuer", Audience: "audience", Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, KeySet: keys, Clock: authtest.NewClock(jwtNow), Subjects: []string{"service", "service"}},
		{Issuer: "issuer", Audience: "audience", Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, KeySet: keys, Clock: authtest.NewClock(jwtNow), RequiredClaims: []string{""}},
		{Issuer: "issuer", Audience: "audience", Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, KeySet: keys, Clock: authtest.NewClock(jwtNow), RequiredClaims: []string{"tenant", "tenant"}},
		{Issuer: "issuer", Audience: "audience", Algorithms: []jwa.SignatureAlgorithm{jwa.RS256()}, KeySet: keys, Clock: authtest.NewClock(jwtNow), RequiredClaims: []string{"sub"}},
	}
	for index, configuration := range invalid {
		if _, err := authjwt.New(configuration); !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Errorf("New(invalid %d) error = %v", index, err)
		}
	}
	if _, err := authjwt.New(authjwt.Config{
		Issuer: "issuer", Audience: "audience", Algorithms: []jwa.SignatureAlgorithm{jwa.ES256K()},
		Provider: authjwt.KeyProviderFunc(func(context.Context) (jwk.Set, error) { return keys, nil }),
		Clock:    authtest.NewClock(jwtNow),
	}); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Errorf("New(ES256K) error = %v", err)
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
	private := freshRSAKey(t)
	var err error
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

var (
	testRSAOnce sync.Once
	testRSADER  []byte
	testRSAErr  error
)

func freshRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	testRSAOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			testRSAErr = err
			return
		}
		testRSADER, testRSAErr = x509.MarshalPKCS8PrivateKey(key)
	})
	if testRSAErr != nil {
		t.Fatalf("prepare RSA fixture: %v", testRSAErr)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(testRSADER)
	if err != nil {
		t.Fatalf("ParsePKCS8PrivateKey() error = %v", err)
	}
	private, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		t.Fatalf("ParsePKCS8PrivateKey() type = %T", parsed)
	}
	return private
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

func signedPayload(t *testing.T, signer jwk.Key, algorithm jwa.SignatureAlgorithm, payload []byte) string {
	t.Helper()
	protected := jws.NewHeaders()
	if err := protected.Set("alg", algorithm); err != nil {
		t.Fatalf("Headers.Set(alg) error = %v", err)
	}
	keyID, ok := signer.KeyID()
	if !ok {
		t.Fatal("signer has no key ID")
	}
	if err := protected.Set("kid", keyID); err != nil {
		t.Fatalf("Headers.Set(kid) error = %v", err)
	}
	signed, err := jws.Sign(payload, jws.WithKey(algorithm, signer, jws.WithProtectedHeaders(protected)))
	if err != nil {
		t.Fatalf("jws.Sign() error = %v", err)
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

type countingClock struct {
	now   time.Time
	calls atomic.Int64
}

func (clock *countingClock) Now() time.Time {
	clock.calls.Add(1)
	return clock.now
}

func nonCanonicalBase64URL(encoded string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	unused := 0
	switch len(encoded) % 4 {
	case 2:
		unused = 4
	case 3:
		unused = 2
	default:
		return encoded + "="
	}
	last := strings.IndexByte(alphabet, encoded[len(encoded)-1])
	if last < 0 {
		return encoded + "="
	}
	mask := (1 << unused) - 1
	canonical := last &^ mask
	return encoded[:len(encoded)-1] + string(alphabet[canonical|1])
}
