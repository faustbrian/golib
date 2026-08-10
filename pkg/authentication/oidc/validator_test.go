package oidc_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	upstreamoidc "github.com/coreos/go-oidc/v3/oidc"
	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authoidc "github.com/faustbrian/golib/pkg/authentication/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

var oidcNow = time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)

var externalRSAFixture = struct {
	mutex sync.Mutex
	keys  [3]*rsa.PrivateKey
	calls map[*testing.T]int
}{keys: generateExternalRSAFixtureKeys(), calls: make(map[*testing.T]int)}

func generateExternalRSAFixtureKeys() [3]*rsa.PrivateKey {
	var keys [3]*rsa.PrivateKey
	for index := range keys {
		private, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		private.Precompute()
		keys[index] = private
	}
	return keys
}

func TestValidatorAuthenticatesStrictOIDCIDToken(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		TrustedAudiences: []string{"other"}, Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	token := signIDToken(t, private, map[string]any{
		"sub": "user-1", "iss": "https://issuer.example.test",
		"aud": []string{"client-1", "other"}, "azp": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
		"auth_time": oidcNow.Add(-time.Minute).Unix(),
		"nonce":     "nonce-1", "scope": "openid profile", "tenant": "north",
		"profile": map[string]any{"locale": "fi"},
	})

	result, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok || principal.Subject() != "user-1" || principal.Method() != "oidc" {
		t.Fatalf("Authenticate() principal = (%v, %v)", principal, ok)
	}
	if principal.Issuer() != "https://issuer.example.test" ||
		!principal.AuthenticatedAt().Equal(oidcNow.Add(-time.Minute)) {
		t.Fatalf("principal issuer/time = %q/%v", principal.Issuer(), principal.AuthenticatedAt())
	}
	if strings.Join(principal.Audiences(), ",") != "client-1,other" ||
		strings.Join(principal.Scopes(), ",") != "openid,profile" ||
		strings.Join(principal.TenantHints(), ",") != "north" {
		t.Fatalf("principal protocol data = %#v/%#v/%#v", principal.Audiences(), principal.Scopes(), principal.TenantHints())
	}
	if principal.Claims()["profile"].(map[string]any)["locale"] != "fi" {
		t.Fatalf("principal claims = %#v", principal.Claims())
	}
}

func TestValidatorEnforcesOIDCClaimsAndAuthorizedParty(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow), ClockSkew: time.Minute,
	})
	valid := map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	}
	tests := []struct {
		name  string
		alter func(map[string]any)
	}{
		{name: "wrong issuer", alter: func(claims map[string]any) { claims["iss"] = "https://attacker.test" }},
		{name: "wrong audience", alter: func(claims map[string]any) { claims["aud"] = "other" }},
		{name: "expired", alter: func(claims map[string]any) { claims["exp"] = oidcNow.Add(-2 * time.Minute).Unix() }},
		{name: "not before", alter: func(claims map[string]any) { claims["nbf"] = oidcNow.Add(2 * time.Minute).Unix() }},
		{name: "missing subject", alter: func(claims map[string]any) { delete(claims, "sub") }},
		{name: "missing issued at", alter: func(claims map[string]any) { delete(claims, "iat") }},
		{name: "future issued at", alter: func(claims map[string]any) { claims["iat"] = oidcNow.Add(2 * time.Minute).Unix() }},
		{name: "multiple audience missing azp", alter: func(claims map[string]any) { claims["aud"] = []string{"client-1", "other"} }},
		{name: "multiple audience wrong azp", alter: func(claims map[string]any) { claims["aud"] = []string{"client-1", "other"}; claims["azp"] = "other" }},
		{name: "duplicate audience", alter: func(claims map[string]any) {
			claims["aud"] = []string{"client-1", "client-1"}
			claims["azp"] = "client-1"
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			claims := cloneClaims(valid)
			tt.alter(claims)
			token := signIDToken(t, private, claims)
			if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token)); !errors.Is(err, authentication.ErrCredentialsRejected) {
				t.Fatalf("Authenticate() error = %v, want rejected", err)
			}
		})
	}
}

func TestValidatorRequiresExactIssuerDespiteUpstreamCompatibilityAliases(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://accounts.google.com", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	token := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "accounts.google.com", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	})

	if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateBearer() error = %v, want rejected", err)
	}
}

func TestValidatorRejectsUntrustedAdditionalAudiences(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	token := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test",
		"aud": []string{"client-1", "other"}, "azp": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	})

	if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateBearer() error = %v, want rejected", err)
	}
}

func TestValidatorAppliesConfiguredClockSkewToAllNumericDates(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow), ClockSkew: time.Minute,
	})
	base := map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	}
	tests := []struct {
		name  string
		alter func(map[string]any)
	}{
		{name: "recently expired", alter: func(claims map[string]any) { claims["exp"] = oidcNow.Add(-30 * time.Second).Unix() }},
		{name: "not before within skew", alter: func(claims map[string]any) { claims["nbf"] = oidcNow.Add(30 * time.Second).Unix() }},
		{name: "issued at within skew", alter: func(claims map[string]any) { claims["iat"] = oidcNow.Add(30 * time.Second).Unix() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := cloneClaims(base)
			tt.alter(claims)
			if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(signIDToken(t, private, claims))); err != nil {
				t.Fatalf("Authenticate() error = %v", err)
			}
		})
	}
}

func TestValidatorAppliesExactFractionalClockEdges(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow), ClockSkew: time.Minute,
	})
	base := map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": float64(oidcNow.Unix()), "exp": float64(oidcNow.Add(time.Hour).Unix()),
	}
	tests := []struct {
		name    string
		claim   string
		value   float64
		accepts bool
	}{
		{name: "expiry at lower edge", claim: "exp", value: float64(oidcNow.Add(-time.Minute).Unix())},
		{name: "expiry after lower edge", claim: "exp", value: float64(oidcNow.Add(-time.Minute).Unix()) + 0.5, accepts: true},
		{name: "issued at upper edge", claim: "iat", value: float64(oidcNow.Add(time.Minute).Unix()), accepts: true},
		{name: "issued after upper edge", claim: "iat", value: float64(oidcNow.Add(time.Minute).Unix()) + 0.5},
		{name: "not before upper edge", claim: "nbf", value: float64(oidcNow.Add(time.Minute).Unix()), accepts: true},
		{name: "not before after upper edge", claim: "nbf", value: float64(oidcNow.Add(time.Minute).Unix()) + 0.5},
		{name: "auth time upper edge", claim: "auth_time", value: float64(oidcNow.Add(time.Minute).Unix()), accepts: true},
		{name: "auth time after upper edge", claim: "auth_time", value: float64(oidcNow.Add(time.Minute).Unix()) + 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := cloneClaims(base)
			claims[tt.claim] = tt.value
			_, err := validator.ValidateBearer(context.Background(), signIDToken(t, private, claims))
			if tt.accepts && err != nil {
				t.Fatalf("ValidateBearer() error = %v", err)
			}
			if !tt.accepts && !errors.Is(err, authentication.ErrCredentialsRejected) {
				t.Fatalf("ValidateBearer() error = %v, want rejected", err)
			}
		})
	}
}

func TestValidatorAcceptsFractionalAuthenticationTime(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	token := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
		"auth_time": float64(oidcNow.Add(-time.Minute).Unix()) + 0.5,
	})

	principal, err := validator.ValidateBearer(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateBearer() error = %v", err)
	}
	want := oidcNow.Add(-time.Minute).Add(500 * time.Millisecond)
	if !principal.AuthenticatedAt().Equal(want) {
		t.Fatalf("AuthenticatedAt() = %v, want %v", principal.AuthenticatedAt(), want)
	}
}

func TestValidatorAcceptsEpochAuthenticationTime(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	token := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
		"auth_time": 0,
	})

	principal, err := validator.ValidateBearer(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateBearer() error = %v", err)
	}
	if !principal.AuthenticatedAt().Equal(time.Unix(0, 0).UTC()) {
		t.Fatalf("AuthenticatedAt() = %v", principal.AuthenticatedAt())
	}
}

func TestValidatorUsesNonceCallback(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	seen := ""
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
		NonceValidator: authoidc.NonceValidatorFunc(func(_ context.Context, nonce string) error {
			seen = nonce
			if nonce != "expected" {
				return errors.New("nonce rejected")
			}
			return nil
		}),
	})
	claims := map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(), "nonce": "expected",
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(signIDToken(t, private, claims))); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if seen != "expected" {
		t.Fatalf("nonce callback saw %q", seen)
	}
	claims["nonce"] = "wrong"
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(signIDToken(t, private, claims))); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("Authenticate(wrong nonce) error = %v", err)
	}
}

func TestValidatorAllowsExactlyOneConcurrentNonceConsumption(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	var consumed atomic.Bool
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
		NonceValidator: authoidc.NonceValidatorFunc(func(_ context.Context, nonce string) error {
			if nonce != "single-use" || !consumed.CompareAndSwap(false, true) {
				return errors.New("nonce already consumed")
			}
			return nil
		}),
	})
	token := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(), "nonce": "single-use",
	})
	const callers = 32
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			_, err := validator.ValidateBearer(context.Background(), token)
			results <- err
		}()
	}
	close(start)
	accepted := 0
	for range callers {
		err := <-results
		if err == nil {
			accepted++
		} else if !errors.Is(err, authentication.ErrCredentialsRejected) {
			t.Fatalf("ValidateBearer() error = %v", err)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted validations = %d, want 1", accepted)
	}
}

func TestValidatorContainsNonceCallbackPanic(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
		NonceValidator: authoidc.NonceValidatorFunc(func(context.Context, string) error {
			panic("nonce-secret")
		}),
	})
	token := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(), "nonce": "nonce-secret",
	})

	if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateBearer() error = %v, want rejected", err)
	}
}

func TestValidatorRejectsDistributedClaimsWithoutRetainingTokens(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	token := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
		"_claim_names": map[string]any{"email": "source-1"},
		"_claim_sources": map[string]any{"source-1": map[string]any{
			"endpoint": "https://claims.example.test", "access_token": "distributed-secret",
		}},
	})
	principal, err := validator.ValidateBearer(context.Background(), token)
	if !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateBearer() = %#v, %v", principal, err)
	}
	if strings.Contains(err.Error(), "distributed-secret") {
		t.Fatal("ValidateBearer() exposed distributed claim token")
	}
	onlySources := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
		"_claim_sources": map[string]any{"source-1": map[string]any{"access_token": "distributed-secret"}},
	})
	if _, err := validator.ValidateBearer(context.Background(), onlySources); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateBearer(_claim_sources) error = %v", err)
	}
}

func TestValidatorRejectsClaimsThatCannotBeDecodedLosslessly(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	payloads := []struct {
		encoded []byte
		target  error
	}{
		{encoded: []byte(`{"sub":"user","iss":"https://issuer.example.test","aud":"client-1","iat":1800000000,"exp":1800003600,"scope":1e1000}`), target: authentication.ErrCredentialsRejected},
		{encoded: []byte("{\"sub\":\"user\xff\",\"iss\":\"https://issuer.example.test\",\"aud\":\"client-1\",\"iat\":1800000000,\"exp\":1800003600}"), target: authentication.ErrCredentialsInvalid},
		{encoded: []byte(`{"sub":"\ud800","iss":"https://issuer.example.test","aud":"client-1","iat":1800000000,"exp":1800003600}`), target: authentication.ErrCredentialsInvalid},
	}
	for _, payload := range payloads {
		token := signRawIDToken(t, private, payload.encoded)
		if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, payload.target) {
			t.Fatalf("ValidateBearer(%q) error = %v", payload.encoded, err)
		}
	}
}

func TestValidatorValidatesAllClaimsBeforeConsumingNonce(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	var calls atomic.Int64
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
		NonceValidator: authoidc.NonceValidatorFunc(func(context.Context, string) error {
			calls.Add(1)
			return nil
		}),
	})
	token := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
		"nonce": "single-use", "scope": 42,
	})
	if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateBearer() error = %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("nonce callback calls = %d, want 0", got)
	}
}

func TestValidatorPreservesNonceCancellationAsUnavailable(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
		NonceValidator: authoidc.NonceValidatorFunc(func(context.Context, string) error {
			return context.Canceled
		}),
	})
	token := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(), "nonce": "nonce",
	})
	if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, context.Canceled) ||
		!errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("ValidateBearer() error = %v", err)
	}
}

func TestValidatorRejectsNonASCIIOrOversizedSubject(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	for _, subject := range []string{"käyttäjä", strings.Repeat("a", 256)} {
		token := signIDToken(t, private, map[string]any{
			"sub": subject, "iss": "https://issuer.example.test", "aud": "client-1",
			"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
		})
		if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, authentication.ErrCredentialsRejected) {
			t.Fatalf("ValidateBearer(subject length %d) error = %v", len(subject), err)
		}
	}
}

func TestValidatorAcceptsExactTokenAndSubjectBounds(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	claims := map[string]any{
		"sub": "user\x7f", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	}
	token := signIDToken(t, private, claims)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow), MaxTokenBytes: len(token),
	})
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token)); err != nil {
		t.Fatalf("Authenticate(exact token and subject bounds) error = %v", err)
	}
	exactSubjectToken := signIDToken(t, private, map[string]any{
		"sub": strings.Repeat("s", 255), "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	})
	exactSubjectValidator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	if _, err := exactSubjectValidator.ValidateBearer(context.Background(), exactSubjectToken); err != nil {
		t.Fatalf("ValidateBearer(exact subject bound) error = %v", err)
	}
}

func TestValidatorRequiresAuthorizedPartyForTrustedMultipleAudiences(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1", TrustedAudiences: []string{"other"},
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	token := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": []string{"client-1", "other"},
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	})
	if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateBearer(missing azp) error = %v", err)
	}
}

func TestValidateIDTokenBindsAccessTokenAndAuthorizationCode(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	accessToken := "access-token"
	authorizationCode := "authorization-code"
	token := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
		"at_hash": oidcHash(accessToken), "c_hash": oidcHash(authorizationCode),
	})

	principal, err := validator.ValidateIDToken(context.Background(), token, authoidc.TokenBinding{
		AccessToken: accessToken, AuthorizationCode: authorizationCode,
	})
	if err != nil {
		t.Fatalf("ValidateIDToken() error = %v", err)
	}
	if _, exposed := principal.Claims()["at_hash"]; exposed {
		t.Fatal("principal exposed at_hash")
	}
	if _, exposed := principal.Claims()["c_hash"]; exposed {
		t.Fatal("principal exposed c_hash")
	}
	if _, err := validator.ValidateIDToken(context.Background(), token, authoidc.TokenBinding{
		AccessToken: "different", AuthorizationCode: authorizationCode,
	}); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateIDToken(mismatched access token) error = %v", err)
	}
	if _, err := validator.ValidateIDToken(context.Background(), token, authoidc.TokenBinding{
		AccessToken: accessToken, AuthorizationCode: "different",
	}); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateIDToken(mismatched code) error = %v", err)
	}
	if _, err := validator.ValidateIDToken(context.Background(), token, authoidc.TokenBinding{
		AccessToken: "different",
	}); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateIDToken(access-token-only mismatch) error = %v", err)
	}
	if _, err := validator.ValidateIDToken(context.Background(), token, authoidc.TokenBinding{
		AuthorizationCode: "different",
	}); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateIDToken(code-only mismatch) error = %v", err)
	}
}

func TestValidatorRejectsMalformedBoundedAndDuplicateTokens(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
		MaxTokenBytes: 512, MaxClaims: 8, MaxClaimDepth: 3,
	})
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"key-1"}`))
	duplicate := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"one","sub":"two"}`))
	nullKeyID := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":null}`)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte("signature"))
	tooMany := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(), "a": 1, "b": 2, "c": 3, "d": 4,
	})
	validToken := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	})
	validParts := strings.Split(validToken, ".")
	tests := []string{
		"not-a-token", header + "." + duplicate + ".signature", nullKeyID, tooMany,
		validParts[0] + "." + validParts[1] + ".%",
		strings.Repeat("x", 513),
	}
	for _, token := range tests {
		if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token)); !errors.Is(err, authentication.ErrCredentialsInvalid) {
			t.Errorf("Authenticate() error = %v", err)
		}
	}
	wrongSignature := signIDToken(t, rsaKey(t), map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	})
	signatureValidator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	if _, err := signatureValidator.ValidateBearer(context.Background(), wrongSignature); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateBearer(wrong signature) error = %v", err)
	}
	disallowedHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS384","kid":"key-1"}`))
	if _, err := signatureValidator.ValidateBearer(context.Background(),
		disallowedHeader+"."+validParts[1]+"."+validParts[2]); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateBearer(disallowed algorithm) error = %v", err)
	}
}

func TestValidatorClassifiesCallerKeySetCancellationAsUnavailable(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	ctx, cancel := context.WithCancel(context.Background())
	validator, err := authoidc.NewWithKeySet(authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	}, cancelingKeySet{cancel: cancel})
	if err != nil {
		t.Fatalf("NewWithKeySet() error = %v", err)
	}
	token := signIDToken(t, private, map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	})
	if _, err := validator.ValidateBearer(ctx, token); !errors.Is(err, authentication.ErrAuthenticationUnavailable) ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateBearer(canceled key set) error = %v", err)
	}
}

func TestValidatorRejectsInvalidPrincipalClaimShapes(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	base := map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client-1",
		"iat": oidcNow.Unix(), "exp": oidcNow.Add(time.Hour).Unix(),
	}
	tests := []struct {
		name  string
		alter func(map[string]any)
	}{
		{name: "numeric scope", alter: func(claims map[string]any) { claims["scope"] = 1 }},
		{name: "duplicate scope", alter: func(claims map[string]any) { claims["scope"] = "read read" }},
		{name: "non-canonical scope separator", alter: func(claims map[string]any) { claims["scope"] = "read\twrite" }},
		{name: "empty tenant collection", alter: func(claims map[string]any) { claims["tenant"] = []string{} }},
		{name: "empty private claim name", alter: func(claims map[string]any) { claims[""] = "value" }},
		{name: "empty tenant", alter: func(claims map[string]any) { claims["tenant"] = "" }},
		{name: "string auth time", alter: func(claims map[string]any) { claims["auth_time"] = "yesterday" }},
		{name: "numeric authorized party", alter: func(claims map[string]any) { claims["azp"] = 42 }},
		{name: "empty authorized party", alter: func(claims map[string]any) { claims["azp"] = "" }},
		{name: "null authorized party", alter: func(claims map[string]any) { claims["azp"] = nil }},
		{name: "null access-token hash", alter: func(claims map[string]any) { claims["at_hash"] = nil }},
		{name: "null authentication methods", alter: func(claims map[string]any) { claims["amr"] = nil }},
		{name: "non-string authentication method", alter: func(claims map[string]any) { claims["amr"] = []any{"pwd", 1} }},
		{name: "null authentication context", alter: func(claims map[string]any) { claims["acr"] = nil }},
		{name: "null authorization-code hash", alter: func(claims map[string]any) { claims["c_hash"] = nil }},
		{name: "null JWT ID", alter: func(claims map[string]any) { claims["jti"] = nil }},
		{name: "null nonce", alter: func(claims map[string]any) { claims["nonce"] = nil }},
		{name: "null scope", alter: func(claims map[string]any) { claims["scope"] = nil }},
		{name: "null session ID", alter: func(claims map[string]any) { claims["sid"] = nil }},
		{name: "null tenant", alter: func(claims map[string]any) { claims["tenant"] = nil }},
		{name: "string not before", alter: func(claims map[string]any) { claims["nbf"] = "tomorrow" }},
		{name: "oversized not before", alter: func(claims map[string]any) { claims["nbf"] = 1e300 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := cloneClaims(base)
			tt.alter(claims)
			token := signIDToken(t, private, claims)
			if _, err := validator.ValidateBearer(context.Background(), token); !errors.Is(err, authentication.ErrCredentialsRejected) {
				t.Fatalf("ValidateBearer() error = %v", err)
			}
		})
	}
}

func TestValidatorPreservesPrivateNumbersAndRejectsInvalidUnicode(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	payload := []byte(fmt.Sprintf(
		`{"iss":"https://issuer.example.test","sub":"user","aud":"client-1","iat":%d,"exp":%d,"private_number":9007199254740993}`,
		oidcNow.Unix(), oidcNow.Add(time.Hour).Unix(),
	))
	principal, err := validator.ValidateBearer(context.Background(), signRawIDToken(t, private, payload))
	if err != nil {
		t.Fatalf("ValidateBearer(lossless private number) error = %v", err)
	}
	number, ok := principal.Claims()["private_number"].(json.Number)
	if !ok || number.String() != "9007199254740993" {
		t.Fatalf("private_number = %#v", principal.Claims()["private_number"])
	}

	invalidUnicode := []byte(fmt.Sprintf(
		`{"iss":"https://issuer.example.test","sub":"user","aud":"client-1","iat":%d,"exp":%d,"private":"\uD800"}`,
		oidcNow.Unix(), oidcNow.Add(time.Hour).Unix(),
	))
	if _, err := validator.ValidateBearer(context.Background(), signRawIDToken(t, private, invalidUnicode)); !errors.Is(err, authentication.ErrCredentialsInvalid) {
		t.Fatalf("ValidateBearer(unpaired surrogate) error = %v", err)
	}
}

func TestValidatorHonorsCancellationAndConfiguration(t *testing.T) {
	t.Parallel()

	private := rsaKey(t)
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://issuer.example.test", ClientID: "client-1",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := validator.Authenticate(ctx, authentication.NewBearerCredential("token")); !errors.Is(err, context.Canceled) || !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("Authenticate(canceled) error = %v", err)
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBasicCredential("user", "password")); !errors.Is(err, authentication.ErrCredentialsInvalid) {
		t.Fatalf("Authenticate(wrong kind) error = %v", err)
	}

	keySet := &upstreamoidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&private.PublicKey}}
	invalid := []authoidc.Config{
		{},
		{Issuer: "https://issuer.example.test?tenant=north", ClientID: "client", Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow)},
		{Issuer: "issuer", ClientID: "client", Algorithms: []string{"none"}, Clock: authtest.NewClock(oidcNow)},
		{Issuer: "issuer", ClientID: "client", Algorithms: []string{"HS256"}, Clock: authtest.NewClock(oidcNow)},
		{Issuer: "issuer", ClientID: "client", Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow), MaxClaims: authentication.MaxClaims + 1},
		{Issuer: "https://issuer.example.test", ClientID: "client", TrustedAudiences: []string{""}, Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow)},
		{Issuer: "https://issuer.example.test", ClientID: "client", TrustedAudiences: []string{"client"}, Algorithms: []string{"RS256"}, Clock: authtest.NewClock(oidcNow)},
	}
	for index, configuration := range invalid {
		if _, err := authoidc.NewWithKeySet(configuration, keySet); !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Errorf("NewWithKeySet(invalid %d) error = %v", index, err)
		}
	}
}

func staticValidator(t *testing.T, private *rsa.PrivateKey, configuration authoidc.Config) *authoidc.Validator {
	t.Helper()
	keySet := &upstreamoidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&private.PublicKey}}
	validator, err := authoidc.NewWithKeySet(configuration, keySet)
	if err != nil {
		t.Fatalf("NewWithKeySet() error = %v", err)
	}
	return validator
}

func rsaKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	externalRSAFixture.mutex.Lock()
	defer externalRSAFixture.mutex.Unlock()
	index := externalRSAFixture.calls[t]
	if index == 0 {
		t.Cleanup(func() {
			externalRSAFixture.mutex.Lock()
			delete(externalRSAFixture.calls, t)
			externalRSAFixture.mutex.Unlock()
		})
	}
	if index >= len(externalRSAFixture.keys) {
		t.Fatalf("rsaKey() requested %d distinct test keys", index+1)
	}
	externalRSAFixture.calls[t] = index + 1
	return externalRSAFixture.keys[index]
}

func signIDToken(t *testing.T, private *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	options := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "key-1")
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: private}, options)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return signRawIDTokenWithSigner(t, signer, payload)
}

func signRawIDToken(t *testing.T, private *rsa.PrivateKey, payload []byte) string {
	t.Helper()
	options := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "key-1")
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: private}, options)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	return signRawIDTokenWithSigner(t, signer, payload)
}

func signRawIDTokenWithSigner(t *testing.T, signer jose.Signer, payload []byte) string {
	t.Helper()
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("Signer.Sign() error = %v", err)
	}
	compact, err := signed.CompactSerialize()
	if err != nil {
		t.Fatalf("CompactSerialize() error = %v", err)
	}
	return compact
}

func cloneClaims(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for name, value := range source {
		clone[name] = value
	}
	return clone
}

func oidcHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(digest[:len(digest)/2])
}

type cancelingKeySet struct {
	cancel context.CancelFunc
}

func (keySet cancelingKeySet) VerifySignature(ctx context.Context, _ string) ([]byte, error) {
	keySet.cancel()
	return nil, ctx.Err()
}
