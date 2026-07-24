package oidc

import (
	"context"
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
}

type valueKeySet struct{ err error }

func (s valueKeySet) VerifySignature(context.Context, string) ([]byte, error) {
	return nil, s.err
}

type valueNonceValidator struct{}

func (*valueNonceValidator) ValidateNonce(context.Context, string) error { return nil }

var _ upstreamoidc.KeySet = valueKeySet{}
