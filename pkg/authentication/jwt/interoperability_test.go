package jwt_test

import (
	"context"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authjwt "github.com/faustbrian/golib/pkg/authentication/jwt"
	golangjwt "github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	upstreamjwt "github.com/lestrrat-go/jwx/v3/jwt"
)

func TestRFC7520HMACJWKInteroperability(t *testing.T) {
	t.Parallel()

	// RFC 7520, Figure 5: HMAC SHA-256 symmetric JWK.
	keys, err := jwk.Parse([]byte(`{"keys":[{"kty":"oct","kid":"018c0ae5-4d9b-471b-bfd6-eef314bc7037","use":"sig","alg":"HS256","k":"hJtXIZ2uSN5kbQfbtTNWbpdmhkV8FJG-Onbc6mxCcYg"}]}`))
	if err != nil {
		t.Fatalf("Parse(RFC 7520 JWK) error = %v", err)
	}
	signer, ok := keys.Key(0)
	if !ok {
		t.Fatal("RFC 7520 JWK is missing")
	}
	now := time.Unix(1_311_281_000, 0).UTC()
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "https://server.example.com", Audience: "s6BhdRkqt3",
		Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()}, KeySet: keys,
		Clock: authtest.NewClock(now),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	token := upstreamjwt.New()
	for name, value := range map[string]any{
		"sub": "24400320", "iss": "https://server.example.com", "aud": "s6BhdRkqt3",
		"iat": now, "exp": now.Add(time.Hour),
	} {
		if err := token.Set(name, value); err != nil {
			t.Fatalf("Set(%s) error = %v", name, err)
		}
	}
	signed, err := upstreamjwt.Sign(token, upstreamjwt.WithKey(jwa.HS256(), signer))
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	result, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(string(signed)))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok || principal.Subject() != "24400320" {
		t.Fatalf("Authenticate() principal = (%v, %v)", principal, ok)
	}
}

func TestGolangJWTInteroperability(t *testing.T) {
	t.Parallel()

	secret := []byte("01234567890123456789012345678901")
	key, err := jwk.Import(secret)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if err := key.Set(jwk.KeyIDKey, "shared-key"); err != nil {
		t.Fatalf("Set(kid) error = %v", err)
	}
	if err := key.Set(jwk.AlgorithmKey, jwa.HS256()); err != nil {
		t.Fatalf("Set(alg) error = %v", err)
	}
	keys := jwk.NewSet()
	if err := keys.AddKey(key); err != nil {
		t.Fatalf("AddKey() error = %v", err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()}, KeySet: keys,
		Clock: authtest.NewClock(now),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	golangToken := golangjwt.NewWithClaims(golangjwt.SigningMethodHS256, golangjwt.MapClaims{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	golangToken.Header["kid"] = "shared-key"
	compact, err := golangToken.SignedString(secret)
	if err != nil {
		t.Fatalf("golang-jwt SignedString() error = %v", err)
	}
	principal, err := validator.ValidateBearer(context.Background(), compact)
	if err != nil {
		t.Fatalf("ValidateBearer(golang-jwt) error = %v", err)
	}
	if principal.Subject() != "service" {
		t.Fatalf("ValidateBearer(golang-jwt) subject = %q", principal.Subject())
	}

	lestrratToken := upstreamjwt.New()
	for name, value := range map[string]any{
		"sub": "service", "iss": "https://issuer.example.test", "aud": "orders",
		"iat": now, "exp": now.Add(time.Hour),
	} {
		if err := lestrratToken.Set(name, value); err != nil {
			t.Fatalf("Set(%s) error = %v", name, err)
		}
	}
	lestrratCompact, err := upstreamjwt.Sign(
		lestrratToken,
		upstreamjwt.WithKey(jwa.HS256(), key),
	)
	if err != nil {
		t.Fatalf("lestrrat-go Sign() error = %v", err)
	}
	parsed, err := golangjwt.Parse(
		string(lestrratCompact),
		func(*golangjwt.Token) (any, error) { return secret, nil },
		golangjwt.WithValidMethods([]string{"HS256"}),
		golangjwt.WithIssuer("https://issuer.example.test"),
		golangjwt.WithAudience("orders"),
		golangjwt.WithSubject("service"),
		golangjwt.WithIssuedAt(),
		golangjwt.WithExpirationRequired(),
		golangjwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("golang-jwt Parse(lestrrat-go) error = %v", err)
	}
	if parsed == nil || !parsed.Valid {
		t.Fatal("golang-jwt Parse(lestrrat-go) token is invalid")
	}
}
