package oidc

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	upstreamoidc "github.com/coreos/go-oidc/v3/oidc"
	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
)

func TestClaimStringsAcceptsSupportedShapesAndRejectsHostileValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		value       any
		splitSpaces bool
		want        []string
		wantError   bool
	}{
		{name: "missing"},
		{name: "space string", value: "read write", splitSpaces: true, want: []string{"read", "write"}},
		{name: "single string", value: "north", want: []string{"north"}},
		{name: "empty string", value: "", wantError: true},
		{name: "string slice", value: []string{"north", "south"}, want: []string{"north", "south"}},
		{name: "any slice", value: []any{"north", "south"}, want: []string{"north", "south"}},
		{name: "non-string item", value: []any{"north", 1}, wantError: true},
		{name: "empty item", value: []any{""}, wantError: true},
		{name: "unsupported", value: 1, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := claimStrings(tt.value, tt.splitSpaces)
			if (err != nil) != tt.wantError || strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("claimStrings() = %#v, %v", got, err)
			}
			if len(got) > 0 {
				got[0] = "mutated"
			}
		})
	}
}

func TestInspectJSONObjectRejectsHostileJSONShapes(t *testing.T) {
	t.Parallel()

	largeArray, err := json.Marshal(make([]int, authentication.MaxClaimCollection+1))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	tests := [][]byte{
		{}, []byte(`1`), []byte(`[]`), []byte(`{} {}`),
		[]byte(`{"a":1,"a":2}`), []byte(`{"a":1,"b":2}`),
		[]byte(`{"a":{"b":1}}`), []byte(`{"a":`),
		[]byte(`{invalid}`), []byte(`}`),
	}
	for _, encoded := range tests {
		if err := inspectJSONObject(encoded, 1, 1); err == nil {
			t.Fatalf("inspectJSONObject(%q) error = nil", encoded)
		}
	}
	if err := inspectJSONObject(largeArray, authentication.MaxClaimCollection+1, 2); err == nil {
		t.Fatal("inspectJSONObject(oversized array) error = nil")
	}
	oversizedMember, err := json.Marshal(map[string]any{
		"values": make([]int, authentication.MaxClaimCollection+1),
	})
	if err != nil {
		t.Fatalf("Marshal(oversized member) error = %v", err)
	}
	if err := inspectJSONObject(oversizedMember, 1, 2); err == nil {
		t.Fatal("inspectJSONObject(oversized member array) error = nil")
	}
	if err := inspectJSONObject([]byte(`[[[]]]`), authentication.MaxClaims, 2); err == nil {
		t.Fatal("inspectJSONObject(nested array) error = nil")
	}
	if err := inspectJSONObject([]byte(`{"a":1`), authentication.MaxClaims, authentication.MaxClaimDepth); err == nil {
		t.Fatal("inspectJSONObject(missing delimiter) error = nil")
	}
	decoder := json.NewDecoder(strings.NewReader(`[]`))
	_, _ = decoder.Token()
	if err := inspectJSONValue(decoder, 0, authentication.MaxClaims, authentication.MaxClaimDepth, false); err == nil {
		t.Fatal("inspectJSONValue(unexpected closing delimiter) error = nil")
	}
}

func TestInspectJSONObjectAcceptsExactDepthAndMemberBounds(t *testing.T) {
	t.Parallel()

	if err := inspectJSONObject([]byte(`{"a":{"b":1}}`), 1, 2); err != nil {
		t.Fatalf("inspectJSONObject(exact bounds) error = %v", err)
	}
	array := make([]int, authentication.MaxClaimCollection)
	encoded, err := json.Marshal(map[string]any{"values": array})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := inspectJSONObject(encoded, 1, 2); err != nil {
		t.Fatalf("inspectJSONObject(exact collection bound) error = %v", err)
	}
}

func TestInspectCompactTokenRejectsEachBoundary(t *testing.T) {
	t.Parallel()

	encode := func(value string) string { return base64.RawURLEncoding.EncodeToString([]byte(value)) }
	allowed := map[string]struct{}{"RS256": {}}
	tests := []string{
		"a.b.c.d",
		"%.e30.signature",
		encode(`{"alg":"RS256"}`) + ".%.signature",
		encode(`[]`) + "." + encode(`{}`) + ".signature",
		encode(`{"alg":"RS256"}`) + "." + encode(`[]`) + ".signature",
		encode(`{"alg":1}`) + "." + encode(`{}`) + ".signature",
		encode(`{"alg":"RS384"}`) + "." + encode(`{}`) + ".signature",
		encode(`{"alg":"RS256","crit":[]}`) + "." + encode(`{}`) + ".signature",
	}
	for _, token := range tests {
		if err := inspectCompactToken(token, allowed, authentication.MaxClaims, authentication.MaxClaimDepth); err == nil {
			t.Fatalf("inspectCompactToken(%q) error = nil", token)
		}
	}
}

func TestConfigurationRejectsDuplicateAlgorithmsAndNilDependencies(t *testing.T) {
	t.Parallel()

	base := Config{
		Issuer: "https://issuer.example.test", ClientID: "client",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(time.Unix(1, 0)),
	}
	keySet := valueKeySet{}
	duplicate := base
	duplicate.Algorithms = []string{"RS256", "RS256"}
	if _, err := NewWithKeySet(duplicate, keySet); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewWithKeySet(duplicate algorithm) error = %v", err)
	}
	unsupported := base
	unsupported.Algorithms = []string{"HS256"}
	if _, err := NewWithKeySet(unsupported, keySet); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewWithKeySet(unsupported algorithm) error = %v", err)
	}
	if _, err := NewWithKeySet(base, nil); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewWithKeySet(nil) error = %v", err)
	}
	var typedNil *valueKeySet
	if _, err := NewWithKeySet(base, typedNil); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewWithKeySet(typed nil) error = %v", err)
	}
	if _, err := NewWithKeySet(base, keySet); err != nil {
		t.Fatalf("NewWithKeySet(value key set) error = %v", err)
	}
	var nonce *valueNonceValidator
	base.NonceValidator = nonce
	if _, err := NewWithKeySet(base, keySet); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("NewWithKeySet(typed nil nonce) error = %v", err)
	}
}

func TestConfigurationAcceptsExactUpperBounds(t *testing.T) {
	t.Parallel()

	configuration := Config{
		Issuer: "https://issuer.example.test", ClientID: "client",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(time.Unix(1, 0)),
		ClockSkew: 24 * time.Hour, MaxTokenBytes: maximumTokenBytes,
		MaxClaims: authentication.MaxClaims, MaxClaimDepth: authentication.MaxClaimDepth,
		MaxHTTPBodyBytes: maximumHTTPBodyBytes, DiscoveryTimeout: maximumDiscoveryWait, MaxKeys: maximumJWKCount,
		MinRefreshInterval: maximumRefreshInterval, MaxRefreshInterval: maximumRefreshInterval,
		MaxRefreshWaiters: maximumRefreshWaiters,
	}
	if _, err := NewWithKeySet(configuration, valueKeySet{}); err != nil {
		t.Fatalf("NewWithKeySet(exact bounds) error = %v", err)
	}
	configuration.Issuer = "http://issuer.example.test"
	configuration.InsecureHTTP = true
	if _, err := NewWithKeySet(configuration, valueKeySet{}); err != nil {
		t.Fatalf("NewWithKeySet(insecure HTTP opt-in) error = %v", err)
	}
	defaults := Config{
		Issuer: "https://issuer.example.test", ClientID: "client",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(time.Unix(1, 0)),
	}
	applyDefaults(&defaults)
	if defaults.ClockSkew != 5*time.Minute {
		t.Errorf("default ClockSkew = %v, want 5m", defaults.ClockSkew)
	}
}

func TestConfigurationRejectsResourceLimitExpansion(t *testing.T) {
	t.Parallel()

	base := Config{
		Issuer: "https://issuer.example.test", ClientID: "client",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(time.Unix(1, 0)),
	}
	tests := map[string]func(*Config){
		"token bytes":       func(configuration *Config) { configuration.MaxTokenBytes = 1<<20 + 1 },
		"HTTP body":         func(configuration *Config) { configuration.MaxHTTPBodyBytes = 16<<20 + 1 },
		"discovery timeout": func(configuration *Config) { configuration.DiscoveryTimeout = 5*time.Minute + time.Nanosecond },
		"keys":              func(configuration *Config) { configuration.MaxKeys = 4097 },
		"minimum refresh": func(configuration *Config) {
			configuration.MinRefreshInterval = 24*time.Hour + time.Nanosecond
			configuration.MaxRefreshInterval = configuration.MinRefreshInterval
		},
		"refresh interval": func(configuration *Config) { configuration.MaxRefreshInterval = 24*time.Hour + time.Nanosecond },
		"refresh waiters":  func(configuration *Config) { configuration.MaxRefreshWaiters = 4097 },
	}
	for name, expand := range tests {
		t.Run(name, func(t *testing.T) {
			configuration := base
			expand(&configuration)
			if _, err := NewWithKeySet(configuration, valueKeySet{}); !errors.Is(err, authentication.ErrInvalidConfiguration) {
				t.Fatalf("NewWithKeySet() error = %v", err)
			}
		})
	}
}

func TestConfigurationRejectsEachInvalidBoundary(t *testing.T) {
	t.Parallel()

	base := Config{
		Issuer: "https://issuer.example.test", ClientID: "client",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(time.Unix(1, 0)),
		ClockSkew: time.Second, MaxTokenBytes: 1,
		MaxClaims: 1, MaxClaimDepth: 1, MaxHTTPBodyBytes: 1,
		DiscoveryTimeout: time.Nanosecond, MaxKeys: 1,
		MinRefreshInterval: time.Nanosecond, MaxRefreshInterval: time.Nanosecond,
		MaxRefreshWaiters: 1, ScopeClaim: "scope", TenantClaim: "tenant",
	}
	tests := map[string]func(*Config){
		"issuer parse":         func(c *Config) { c.Issuer = "://" },
		"issuer user":          func(c *Config) { c.Issuer = "https://user@issuer.example.test" },
		"issuer fragment":      func(c *Config) { c.Issuer += "#fragment" },
		"issuer missing host":  func(c *Config) { c.Issuer = "https:///issuer" },
		"issuer scheme":        func(c *Config) { c.Issuer = "ftp://issuer.example.test" },
		"client":               func(c *Config) { c.ClientID = "" },
		"clock":                func(c *Config) { c.Clock = nil },
		"negative skew":        func(c *Config) { c.ClockSkew = -time.Nanosecond },
		"excess skew":          func(c *Config) { c.ClockSkew = 24*time.Hour + time.Nanosecond },
		"token bytes":          func(c *Config) { c.MaxTokenBytes = -1 },
		"claims zero":          func(c *Config) { c.MaxClaims = -1 },
		"claims excess":        func(c *Config) { c.MaxClaims = authentication.MaxClaims + 1 },
		"depth zero":           func(c *Config) { c.MaxClaimDepth = -1 },
		"depth excess":         func(c *Config) { c.MaxClaimDepth = authentication.MaxClaimDepth + 1 },
		"HTTP body":            func(c *Config) { c.MaxHTTPBodyBytes = -1 },
		"discovery timeout":    func(c *Config) { c.DiscoveryTimeout = -1 },
		"keys":                 func(c *Config) { c.MaxKeys = -1 },
		"minimum refresh":      func(c *Config) { c.MinRefreshInterval = -1 },
		"refresh order":        func(c *Config) { c.MinRefreshInterval = 2; c.MaxRefreshInterval = 1 },
		"refresh waiters":      func(c *Config) { c.MaxRefreshWaiters = -1 },
		"duplicate claim name": func(c *Config) { c.TenantClaim = c.ScopeClaim },
		"algorithms":           func(c *Config) { c.Algorithms = nil },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			configuration := base
			mutate(&configuration)
			if _, err := NewWithKeySet(configuration, valueKeySet{}); !errors.Is(err, authentication.ErrInvalidConfiguration) {
				t.Errorf("NewWithKeySet() error = %v, want invalid configuration", err)
			}
		})
	}
}

func TestPrincipalRejectsMissingExpiryBeforeClaims(t *testing.T) {
	t.Parallel()

	validator := &Validator{clock: authtest.NewClock(time.Unix(1, 0))}
	token := &upstreamoidc.IDToken{IssuedAt: time.Unix(1, 0)}
	if _, err := validator.principal(context.Background(), token); !errors.Is(err, authentication.ErrInvalidPrincipal) {
		t.Fatalf("principal(missing expiry) error = %v", err)
	}
}

func TestNilNonceValidatorRecognizesNilInterface(t *testing.T) {
	t.Parallel()

	if !isNilNonceValidator(nil) {
		t.Fatal("isNilNonceValidator(nil) = false")
	}
}

func TestNumericDateBoundariesAndFraction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		encoded string
		want    time.Time
	}{
		{encoded: `-62135596800`, want: time.Unix(-62135596800, 0).UTC()},
		{encoded: `253402300799`, want: time.Unix(253402300799, 0).UTC()},
		{encoded: `1.5`, want: time.Unix(1, int64(500*time.Millisecond)).UTC()},
	}
	for _, tt := range tests {
		got, err := numericDate(json.RawMessage(tt.encoded))
		if err != nil || !got.Equal(tt.want) {
			t.Errorf("numericDate(%s) = %v, %v, want %v", tt.encoded, got, err, tt.want)
		}
	}
	for _, encoded := range []string{`-62135596801`, `253402300800`, `"invalid"`} {
		if _, err := numericDate(json.RawMessage(encoded)); err == nil {
			t.Errorf("numericDate(%s) error = nil", encoded)
		}
	}
}

func TestTokenHashAlgorithmsAndMalformedHeaders(t *testing.T) {
	t.Parallel()

	value := "bound-value"
	sha256Digest := sha256.Sum256([]byte(value))
	sha384Digest := sha512.Sum384([]byte(value))
	sha512Digest := sha512.Sum512([]byte(value))
	tests := []struct {
		algorithm string
		digest    []byte
	}{
		{algorithm: "RS256", digest: sha256Digest[:]},
		{algorithm: "PS384", digest: sha384Digest[:]},
		{algorithm: "ES512", digest: sha512Digest[:]},
	}
	for _, tt := range tests {
		expected := base64.RawURLEncoding.EncodeToString(tt.digest[:len(tt.digest)/2])
		if !validTokenHash(tt.algorithm, value, expected) {
			t.Errorf("validTokenHash(%s) = false", tt.algorithm)
		}
	}
	if validTokenHash("EdDSA", value, "ignored") {
		t.Fatal("validTokenHash(EdDSA) = true")
	}

	validHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`)) + ".claims.signature"
	if algorithm, err := compactTokenAlgorithm(validHeader); err != nil || algorithm != "RS256" {
		t.Fatalf("compactTokenAlgorithm(valid) = %q, %v", algorithm, err)
	}
	for _, raw := range []string{
		"missing-separator",
		"!.claims.signature",
		base64.RawURLEncoding.EncodeToString([]byte(`{`)) + ".claims.signature",
		base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + ".claims.signature",
	} {
		if _, err := compactTokenAlgorithm(raw); err == nil {
			t.Errorf("compactTokenAlgorithm(%q) error = nil", raw)
		}
	}

	if err := verifyTokenBinding(validHeader, &upstreamoidc.IDToken{}, TokenBinding{AccessToken: value}); err == nil {
		t.Fatal("verifyTokenBinding(missing claims) error = nil")
	}
	if err := verifyTokenBinding("invalid", &upstreamoidc.IDToken{}, TokenBinding{AccessToken: value}); err == nil {
		t.Fatal("verifyTokenBinding(invalid header) error = nil")
	}
}

func TestValidateBearerRejectsCanceledAndEmptyInput(t *testing.T) {
	t.Parallel()

	validator := &Validator{maxTokenBytes: 1}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := validator.ValidateBearer(ctx, "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateBearer(canceled) error = %v", err)
	}
	if _, err := validator.ValidateBearer(context.Background(), ""); !errors.Is(err, authentication.ErrCredentialsInvalid) {
		t.Fatalf("ValidateBearer(empty) error = %v", err)
	}
	validator.maxTokenBytes = 3
	if _, err := validator.ValidateBearer(context.Background(), "a.b"); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("ValidateBearer(exact token bound) error = %v", err)
	}
	if _, err := validator.ValidateBearer(context.Background(), "a.bc"); !errors.Is(err, authentication.ErrCredentialsInvalid) {
		t.Fatalf("ValidateBearer(over token bound) error = %v", err)
	}
}

func TestAuthenticateDistinguishesExactAndOversizedTokens(t *testing.T) {
	t.Parallel()

	validator := &Validator{maxTokenBytes: 3}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential("a.b")); !errors.Is(err, authentication.ErrCredentialsRejected) {
		t.Fatalf("Authenticate(exact token bound) error = %v", err)
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential("a.bc")); !errors.Is(err, authentication.ErrCredentialsInvalid) {
		t.Fatalf("Authenticate(over token bound) error = %v", err)
	}
	if _, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential("")); !errors.Is(err, authentication.ErrCredentialsInvalid) {
		t.Fatalf("Authenticate(empty token) error = %v", err)
	}
}

type valueKeySet struct{ err error }

func (s valueKeySet) VerifySignature(context.Context, string) ([]byte, error) {
	return nil, s.err
}

type valueNonceValidator struct{}

func (*valueNonceValidator) ValidateNonce(context.Context, string) error { return nil }

var _ upstreamoidc.KeySet = valueKeySet{}
