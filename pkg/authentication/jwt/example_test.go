package jwt_test

import (
	"context"
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authjwt "github.com/faustbrian/golib/pkg/authentication/jwt"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	upstreamjwt "github.com/lestrrat-go/jwx/v3/jwt"
)

func ExampleNew() {
	key, _ := jwk.Import([]byte("01234567890123456789012345678901"))
	_ = key.Set(jwk.KeyIDKey, "key")
	_ = key.Set(jwk.AlgorithmKey, jwa.HS256())
	keys := jwk.NewSet()
	_ = keys.AddKey(key)
	now := time.Unix(1_800_000_000, 0).UTC()
	validator, _ := authjwt.New(authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "service",
		Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()}, KeySet: keys,
		Clock: authtest.NewClock(now),
	})
	token := upstreamjwt.New()
	_ = token.Set("sub", "service")
	_ = token.Set("iss", "https://issuer.example.test")
	_ = token.Set("aud", "service")
	_ = token.Set("iat", now)
	_ = token.Set("exp", now.Add(time.Hour))
	signed, _ := upstreamjwt.Sign(token, upstreamjwt.WithKey(jwa.HS256(), key))
	principal, err := validator.ValidateBearer(context.Background(), string(signed))
	fmt.Println(err, principal.Subject())
	// Output: <nil> service
}
