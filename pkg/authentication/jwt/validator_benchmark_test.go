package jwt_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authjwt "github.com/faustbrian/golib/pkg/authentication/jwt"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	upstreamjwt "github.com/lestrrat-go/jwx/v3/jwt"
)

func BenchmarkValidateBearer(b *testing.B) {
	validator, signed := benchmarkStaticValidator(b, "key")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := validator.ValidateBearer(context.Background(), signed); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateBearerCachedRemote(b *testing.B) {
	validator, _, cleanup := benchmarkRemoteValidator(b)
	b.Cleanup(cleanup)
	_, signed := benchmarkSigningMaterial(b, "key")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := validator.ValidateBearer(context.Background(), signed); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateBearerRotationMiss(b *testing.B) {
	validator, _, cleanup := benchmarkRemoteValidator(b)
	b.Cleanup(cleanup)
	_, unknown := benchmarkSigningMaterial(b, "rotated-key")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := validator.ValidateBearer(context.Background(), unknown); err == nil {
			b.Fatal("ValidateBearer(rotation miss) error = nil")
		}
	}
}

func BenchmarkValidateBearerHostileInput(b *testing.B) {
	validator, _ := benchmarkStaticValidator(b, "key")
	hostile := strings.Repeat("x", 16*1024)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := validator.ValidateBearer(context.Background(), hostile); err == nil {
			b.Fatal("ValidateBearer(hostile) error = nil")
		}
	}
}

func BenchmarkValidateBearerContention(b *testing.B) {
	validator, signed := benchmarkStaticValidator(b, "key")
	b.ReportAllocs()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			if _, err := validator.ValidateBearer(context.Background(), signed); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func BenchmarkRemoteKeySetCopy(b *testing.B) {
	_, remote, cleanup := benchmarkRemoteValidator(b)
	b.Cleanup(cleanup)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		set, err := remote.KeySet(context.Background())
		if err != nil || set.Len() != 1 {
			b.Fatalf("KeySet() = %v, %v", set, err)
		}
	}
}

func benchmarkStaticValidator(b *testing.B, keyID string) (*authjwt.Validator, string) {
	b.Helper()
	set, signed := benchmarkSigningMaterial(b, keyID)
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "service",
		Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()}, KeySet: set,
		Clock: authtest.NewClock(time.Unix(1_800_000_000, 0).UTC()),
	})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	return validator, signed
}

func benchmarkRemoteValidator(b *testing.B) (*authjwt.Validator, *authjwt.Remote, func()) {
	b.Helper()
	set, _ := benchmarkSigningMaterial(b, "key")
	body, err := json.Marshal(set)
	if err != nil {
		b.Fatalf("json.Marshal(JWK set) error = %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Cache-Control", "max-age=60")
		_, _ = writer.Write(body)
	}))
	remote, err := authjwt.NewRemote(context.Background(), server.URL,
		authjwt.WithInsecureHTTP(), authjwt.WithHTTPClient(server.Client()),
	)
	if err != nil {
		server.Close()
		b.Fatalf("NewRemote() error = %v", err)
	}
	validator, err := authjwt.New(authjwt.Config{
		Issuer: "https://issuer.example.test", Audience: "service",
		Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()}, Provider: remote,
		Clock: authtest.NewClock(time.Unix(1_800_000_000, 0).UTC()),
	})
	if err != nil {
		server.Close()
		b.Fatalf("New() error = %v", err)
	}
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = remote.Close(ctx)
		server.Close()
	}
	return validator, remote, cleanup
}

func benchmarkSigningMaterial(b *testing.B, keyID string) (jwk.Set, string) {
	b.Helper()
	key, err := jwk.Import([]byte("01234567890123456789012345678901"))
	if err != nil {
		b.Fatalf("Import() error = %v", err)
	}
	_ = key.Set(jwk.KeyIDKey, keyID)
	_ = key.Set(jwk.AlgorithmKey, jwa.HS256())
	set := jwk.NewSet()
	_ = set.AddKey(key)
	now := time.Unix(1_800_000_000, 0).UTC()
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
	return set, string(signed)
}
