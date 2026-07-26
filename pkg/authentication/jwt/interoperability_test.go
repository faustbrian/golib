package jwt_test

import (
	"context"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authjwt "github.com/faustbrian/golib/pkg/authentication/jwt"
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
