package oidc_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	upstreamoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authoidc "github.com/faustbrian/golib/pkg/authentication/oidc"
)

func BenchmarkValidateBearer(b *testing.B) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		b.Fatalf("GenerateKey() error = %v", err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	keySet := &upstreamoidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&private.PublicKey}}
	validator, err := authoidc.NewWithKeySet(authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(now),
	}, keySet)
	if err != nil {
		b.Fatalf("NewWithKeySet() error = %v", err)
	}
	token := signIDTokenWithKeyID(b, private, "key-1", map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := validator.ValidateBearer(context.Background(), token); err != nil {
			b.Fatal(err)
		}
	}
}
