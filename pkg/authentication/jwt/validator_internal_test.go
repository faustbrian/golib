package jwt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	upstreamjwt "github.com/lestrrat-go/jwx/v3/jwt"
)

func TestStringClaimAcceptsSupportedShapesAndRejectsHostileValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		claim       string
		value       any
		splitSpaces bool
		want        []string
		wantError   bool
	}{
		{name: "missing", claim: "missing"},
		{name: "space string", claim: "scope", value: "read write", splitSpaces: true, want: []string{"read", "write"}},
		{name: "single string", claim: "tenant", value: "north", want: []string{"north"}},
		{name: "empty string", claim: "tenant", value: "", wantError: true},
		{name: "string slice", claim: "tenant", value: []string{"north", "south"}, want: []string{"north", "south"}},
		{name: "any slice", claim: "tenant", value: []any{"north", "south"}, want: []string{"north", "south"}},
		{name: "non-string item", claim: "tenant", value: []any{"north", 1}, wantError: true},
		{name: "empty item", claim: "tenant", value: []any{""}, wantError: true},
		{name: "unsupported", claim: "tenant", value: 1, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := upstreamjwt.New()
			if tt.value != nil {
				if err := token.Set(tt.claim, tt.value); err != nil {
					t.Fatalf("Set() error = %v", err)
				}
			}
			got, err := stringClaim(token, tt.claim, tt.splitSpaces)
			if (err != nil) != tt.wantError || strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("stringClaim() = %#v, %v", got, err)
			}
			if len(got) > 0 {
				got[0] = "mutated"
			}
		})
	}
}

func TestPrincipalRejectsInvalidTokenShapes(t *testing.T) {
	t.Parallel()

	validator := &Validator{scopeClaim: "scope", tenantClaim: "tenant"}
	base := func() upstreamjwt.Token {
		token := upstreamjwt.New()
		_ = token.Set("sub", "service")
		_ = token.Set("iss", "issuer")
		_ = token.Set("aud", []string{"orders"})
		_ = token.Set("iat", time.Unix(1, 0))
		return token
	}
	tests := []struct {
		name  string
		alter func(upstreamjwt.Token)
	}{
		{name: "missing subject", alter: func(token upstreamjwt.Token) { _ = token.Remove("sub") }},
		{name: "invalid scope", alter: func(token upstreamjwt.Token) { _ = token.Set("scope", 1) }},
		{name: "invalid tenant", alter: func(token upstreamjwt.Token) { _ = token.Set("tenant", "") }},
		{name: "invalid custom claim", alter: func(token upstreamjwt.Token) { _ = token.Set("custom", make(chan int)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := base()
			tt.alter(token)
			if _, err := validator.principal(token); !errors.Is(err, authentication.ErrInvalidPrincipal) {
				t.Fatalf("principal() error = %v", err)
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
		{},
		[]byte(`1`),
		[]byte(`[]`),
		[]byte(`{} {}`),
		[]byte(`{"a":1,"a":2}`),
		[]byte(`{"a":1,"b":2}`),
		[]byte(`{"a":{"b":1}}`),
		[]byte(`{"a":`),
		[]byte(`{invalid}`),
		[]byte(`}`),
		append([]byte(`{"a":`), append(largeArray, '}')...),
	}
	for index, encoded := range tests {
		maxMembers := 1
		maxDepth := 1
		if index == 0 {
			maxMembers = authentication.MaxClaims
		}
		if err := inspectJSONObject(encoded, maxMembers, maxDepth); err == nil {
			t.Fatalf("inspectJSONObject(%q) error = nil", encoded)
		}
	}
	if err := inspectJSONObject(largeArray, authentication.MaxClaimCollection+1, 2); err == nil {
		t.Fatal("inspectJSONObject(oversized array) error = nil")
	}
	if err := inspectJSONObject([]byte(`[[[]]]`), authentication.MaxClaims, 2); err == nil {
		t.Fatal("inspectJSONObject(nested array) error = nil")
	}
	if err := inspectJSONObject([]byte(`{"a":1`), authentication.MaxClaims, authentication.MaxClaimDepth); err == nil {
		t.Fatal("inspectJSONObject(missing delimiter) error = nil")
	}
	decoder := json.NewDecoder(strings.NewReader(`[]`))
	if _, err := decoder.Token(); err != nil {
		t.Fatalf("decoder.Token() error = %v", err)
	}
	if err := inspectJSONValue(decoder, 0, authentication.MaxClaims, authentication.MaxClaimDepth, false); err == nil {
		t.Fatal("inspectJSONValue(unexpected closing delimiter) error = nil")
	}
}

func TestInspectCompactJWTRejectsEachBoundary(t *testing.T) {
	t.Parallel()

	encode := func(value string) string {
		return base64RawURL([]byte(value))
	}
	allowed := map[string]struct{}{"HS256": {}}
	tests := []string{
		"a.b.c.d",
		"%.e30.signature",
		encode(`{"alg":"HS256","kid":"key"}`) + ".%.signature",
		encode(`[]`) + "." + encode(`{}`) + ".signature",
		encode(`{"alg":"HS256","kid":"key"}`) + "." + encode(`[]`) + ".signature",
		encode(`{"alg":1,"kid":"key"}`) + "." + encode(`{}`) + ".signature",
		encode(`{"alg":"RS256","kid":"key"}`) + "." + encode(`{}`) + ".signature",
		encode(`{"alg":"HS256","kid":1}`) + "." + encode(`{}`) + ".signature",
		encode(`{"alg":"HS256","kid":"key","crit":[]}`) + "." + encode(`{}`) + ".signature",
	}
	for _, token := range tests {
		if err := inspectCompactJWT(token, allowed, authentication.MaxClaims, authentication.MaxClaimDepth); err == nil {
			t.Fatalf("inspectCompactJWT(%q) error = nil", token)
		}
	}
}

func TestConfigurationAndKeySetValidationBoundaries(t *testing.T) {
	t.Parallel()

	valid := symmetricKeySet(t, "key", jwa.HS256(), "sig")
	base := Config{
		Issuer: "issuer", Audience: "audience", Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()},
		KeySet: valid, Clock: authtest.NewClock(time.Unix(1, 0)),
	}
	invalid := []Config{
		withAlgorithms(base, nil),
		withAlgorithms(base, []jwa.SignatureAlgorithm{jwa.HS256(), jwa.HS256()}),
		withAlgorithms(base, []jwa.SignatureAlgorithm{jwa.NewSignatureAlgorithm("UNKNOWN")}),
	}
	for _, configuration := range invalid {
		if _, err := New(configuration); !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Fatalf("New(%+v) error = %v", configuration, err)
		}
	}

	keySets := []jwk.Set{
		jwk.NewSet(),
		symmetricKeySet(t, "", jwa.HS256(), "sig"),
		symmetricKeySet(t, "key", jwa.EmptySignatureAlgorithm(), "sig"),
		symmetricKeySet(t, "key", jwa.HS384(), "sig"),
		symmetricKeySet(t, "key", jwa.HS256(), "enc"),
	}
	for _, set := range keySets {
		configuration := base
		configuration.KeySet = set
		if _, err := New(configuration); !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Fatalf("New(invalid key set) error = %v", err)
		}
	}
	typeMismatch := base
	typeMismatch.Algorithms = []jwa.SignatureAlgorithm{jwa.RS256()}
	typeMismatch.KeySet = symmetricKeySet(t, "key", jwa.RS256(), "sig")
	if _, err := New(typeMismatch); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("New(key type mismatch) error = %v", err)
	}
	if _, err := copyAndValidateKeySet(valid, map[string]struct{}{"HS256": {}}, 0); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("copyAndValidateKeySet(max=0) error = %v", err)
	}
	if _, err := copyAndValidateKeySet(marshalErrorSet{embeddedSet: valid}, map[string]struct{}{"HS256": {}}, 1); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("copyAndValidateKeySet(marshal error) error = %v", err)
	}
	providerConfiguration := base
	providerConfiguration.KeySet = nil
	providerConfiguration.Provider = valueProvider{set: valid}
	if _, err := New(providerConfiguration); err != nil {
		t.Fatalf("New(value provider) error = %v", err)
	}
}

func TestJWKAlgorithmFamiliesAndVerificationOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		keyType   jwa.KeyType
		algorithm string
		want      bool
	}{
		{name: "HMAC", keyType: jwa.OctetSeq(), algorithm: "HS256", want: true},
		{name: "RSA", keyType: jwa.RSA(), algorithm: "RS256", want: true},
		{name: "RSA PSS", keyType: jwa.RSA(), algorithm: "PS256", want: true},
		{name: "ECDSA", keyType: jwa.EC(), algorithm: "ES256", want: true},
		{name: "EdDSA", keyType: jwa.OKP(), algorithm: "EdDSA", want: true},
		{name: "Ed25519", keyType: jwa.OKP(), algorithm: "Ed25519", want: true},
		{name: "confused", keyType: jwa.RSA(), algorithm: "HS256"},
		{name: "unknown", keyType: jwa.RSA(), algorithm: "future"},
	}
	for _, tt := range tests {
		if got := keyTypeMatchesAlgorithm(tt.keyType, tt.algorithm); got != tt.want {
			t.Errorf("%s keyTypeMatchesAlgorithm() = %v", tt.name, got)
		}
	}
	if !containsVerifyOperation(jwk.KeyOperationList{jwk.KeyOpSign, jwk.KeyOpVerify}) {
		t.Fatal("containsVerifyOperation(verify) = false")
	}
	if containsVerifyOperation(jwk.KeyOperationList{jwk.KeyOpEncrypt}) {
		t.Fatal("containsVerifyOperation(encrypt) = true")
	}

	base := Config{
		Issuer: "issuer", Audience: "audience", Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()},
		Clock: authtest.NewClock(time.Unix(1, 0)),
	}
	valid := symmetricKeySet(t, "key", jwa.HS256(), "sig")
	validKey, _ := valid.Key(0)
	_ = validKey.Set(jwk.KeyOpsKey, jwk.KeyOperationList{jwk.KeyOpVerify})
	base.KeySet = valid
	if _, err := New(base); err != nil {
		t.Fatalf("New(verify operation) error = %v", err)
	}

	invalid := symmetricKeySet(t, "key", jwa.HS256(), "sig")
	invalidKey, _ := invalid.Key(0)
	_ = invalidKey.Set(jwk.KeyOpsKey, jwk.KeyOperationList{jwk.KeyOpEncrypt})
	base.KeySet = invalid
	if _, err := New(base); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("New(encrypt operation) error = %v", err)
	}
}

func TestValidateBearerAndProviderFailureBoundaries(t *testing.T) {
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

	want := authentication.NewFailure(authentication.FailureUnavailable)
	validator = &Validator{provider: KeyProviderFunc(func(context.Context) (jwk.Set, error) {
		return nil, want
	})}
	if _, err := validator.keySet(context.Background()); err != want {
		t.Fatalf("keySet(classified failure) error = %v", err)
	}
	validator = &Validator{
		provider:   KeyProviderFunc(func(context.Context) (jwk.Set, error) { return jwk.NewSet(), nil }),
		algorithms: map[string]struct{}{"HS256": {}}, maxKeys: 1,
	}
	if _, err := validator.keySet(context.Background()); !errors.Is(err, authentication.ErrAuthenticationUnavailable) {
		t.Fatalf("keySet(invalid set) error = %v", err)
	}
}

func symmetricKeySet(t *testing.T, keyID string, algorithm jwa.SignatureAlgorithm, usage string) jwk.Set {
	t.Helper()
	key, err := jwk.Import([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if keyID != "" {
		_ = key.Set(jwk.KeyIDKey, keyID)
	}
	if algorithm != jwa.EmptySignatureAlgorithm() {
		_ = key.Set(jwk.AlgorithmKey, algorithm)
	}
	if usage != "" {
		_ = key.Set(jwk.KeyUsageKey, usage)
	}
	set := jwk.NewSet()
	if err := set.AddKey(key); err != nil {
		t.Fatalf("AddKey() error = %v", err)
	}
	return set
}

func withAlgorithms(configuration Config, algorithms []jwa.SignatureAlgorithm) Config {
	configuration.Algorithms = algorithms
	return configuration
}

type embeddedSet interface{ jwk.Set }

type marshalErrorSet struct{ embeddedSet }

func (marshalErrorSet) MarshalJSON() ([]byte, error) { return nil, errors.New("marshal failed") }

type valueProvider struct{ set jwk.Set }

func (p valueProvider) KeySet(context.Context) (jwk.Set, error) { return p.set, nil }

func base64RawURL(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}
