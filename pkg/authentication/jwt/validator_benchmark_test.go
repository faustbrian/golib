package jwt_test

import (
	"context"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authjwt "github.com/faustbrian/golib/pkg/authentication/jwt"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	upstreamjwt "github.com/lestrrat-go/jwx/v3/jwt"
)

func BenchmarkValidateBearer(b *testing.B) {
	key, err := jwk.Import([]byte("01234567890123456789012345678901"))
	if err != nil {
		b.Fatalf("Import() error = %v", err)
	}
	_ = key.Set(jwk.KeyIDKey, "key")
	_ = key.Set(jwk.AlgorithmKey, jwa.HS256())
	set := jwk.NewSet()
	_ = set.AddKey(key)
	now := time.Unix(1_800_000_000, 0).UTC()
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "service",
		Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()}, KeySet: set,
		Clock: authtest.NewClock(now),
	})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	token := upstreamjwt.New()
	_ = token.Set("sub", "service")
	_ = token.Set("iss", "https://issuer.example.test")
	_ = token.Set("aud", "service")
	_ = token.Set("iat", now)
	_ = token.Set("exp", now.Add(time.Hour))
	signed, err := upstreamjwt.Sign(token, upstreamjwt.WithKey(jwa.HS256(), key))
	if err != nil {
		b.Fatalf("Sign() error = %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := validator.ValidateBearer(context.Background(), string(signed)); err != nil {
			b.Fatal(err)
		}
	}
}
