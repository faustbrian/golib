package oidc_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strings"
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

func BenchmarkDiscoveryInitialization(b *testing.B) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		b.Fatalf("GenerateKey() error = %v", err)
	}
	state := &oidcServerState{keyID: "key", publicKey: &private.PublicKey}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	b.Cleanup(server.Close)
	state.issuer = server.URL
	configuration := authoidc.Config{
		Issuer: server.URL, ClientID: "client", Algorithms: []string{"RS256"},
		Clock: authtest.NewClock(time.Unix(1_800_000_000, 0).UTC()), InsecureHTTP: true,
		HTTPClient: server.Client(), DiscoveryTimeout: 5 * time.Second,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := authoidc.New(context.Background(), configuration); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRotationMiss(b *testing.B) {
	first, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		b.Fatalf("GenerateKey(first) error = %v", err)
	}
	second, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		b.Fatalf("GenerateKey(second) error = %v", err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	clock := authtest.NewClock(now)
	state := &oidcServerState{keyID: "first", publicKey: &first.PublicKey}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	b.Cleanup(server.Close)
	state.issuer = server.URL
	claims := map[string]any{
		"sub": "user", "iss": server.URL, "aud": "client",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
	secondToken := signIDTokenWithKeyID(b, second, "second", claims)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		state.set("first", &first.PublicKey, http.StatusOK)
		validator, err := authoidc.New(context.Background(), authoidc.Config{
			Issuer: server.URL, ClientID: "client", Algorithms: []string{"RS256"},
			Clock: clock, InsecureHTTP: true, HTTPClient: server.Client(), DiscoveryTimeout: 5 * time.Second,
			MinRefreshInterval: time.Nanosecond, MaxRefreshInterval: time.Hour,
		})
		if err != nil {
			b.Fatalf("New() error = %v", err)
		}
		state.set("second", &second.PublicKey, http.StatusOK)
		clock.Advance(time.Nanosecond)
		b.StartTimer()
		if _, err := validator.ValidateBearer(context.Background(), secondToken); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRefreshContention(b *testing.B) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		b.Fatalf("GenerateKey() error = %v", err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	state := &oidcServerState{keyID: "key", publicKey: &private.PublicKey}
	server := httptest.NewServer(http.HandlerFunc(state.handler))
	b.Cleanup(server.Close)
	state.issuer = server.URL
	validator, err := authoidc.New(context.Background(), authoidc.Config{
		Issuer: server.URL, ClientID: "client", Algorithms: []string{"RS256"},
		Clock: authtest.NewClock(now), InsecureHTTP: true,
		HTTPClient: server.Client(), DiscoveryTimeout: 5 * time.Second,
	})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	token := signIDTokenWithKeyID(b, private, "key", map[string]any{
		"sub": "user", "iss": server.URL, "aud": "client",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			if _, err := validator.ValidateBearer(context.Background(), token); err != nil {
				b.Error(err)
			}
		}
	})
}

func BenchmarkHostileTokenRejection(b *testing.B) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		b.Fatalf("GenerateKey() error = %v", err)
	}
	validator, err := authoidc.NewWithKeySet(authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(time.Unix(1_800_000_000, 0).UTC()),
	}, &upstreamoidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&private.PublicKey}})
	if err != nil {
		b.Fatalf("NewWithKeySet() error = %v", err)
	}
	hostile := strings.Repeat("x", 16*1024+1)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := validator.ValidateBearer(context.Background(), hostile); err == nil {
			b.Fatal("ValidateBearer() error = nil")
		}
	}
}
