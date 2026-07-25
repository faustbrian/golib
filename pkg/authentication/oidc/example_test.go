package oidc_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"time"

	upstreamoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authoidc "github.com/faustbrian/golib/pkg/authentication/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

func ExampleNewWithKeySet() {
	private, _ := rsa.GenerateKey(rand.Reader, 2048)
	keys := &upstreamoidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&private.PublicKey}}
	now := time.Unix(1_800_000_000, 0).UTC()
	validator, _ := authoidc.NewWithKeySet(authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(now),
	}, keys)
	signer, _ := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: private},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "key"),
	)
	payload, _ := json.Marshal(map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	signed, _ := signer.Sign(payload)
	compact, _ := signed.CompactSerialize()
	principal, err := validator.ValidateBearer(context.Background(), compact)
	fmt.Println(err, principal.Subject())
	// Output: <nil> user
}
