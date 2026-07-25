package authentication_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

func TestPrincipalCopiesIdentityData(t *testing.T) {
	t.Parallel()

	authenticatedAt := time.Date(2026, time.July, 15, 7, 0, 0, 0, time.UTC)
	audiences := []string{"orders", "billing"}
	tenants := []string{"north"}
	scopes := []string{"orders:read"}
	claims := map[string]any{
		"email":   "service@example.test",
		"groups":  []any{"operators"},
		"profile": map[string]any{"region": "eu"},
	}

	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{
		Subject:         "service-123",
		Method:          "bearer",
		Issuer:          "https://issuer.example.test",
		Audiences:       audiences,
		TenantHints:     tenants,
		Scopes:          scopes,
		Claims:          claims,
		AuthenticatedAt: authenticatedAt,
	})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}

	audiences[0] = "changed"
	tenants[0] = "changed"
	scopes[0] = "changed"
	claims["email"] = "changed"
	claims["groups"].([]any)[0] = "changed"
	claims["profile"].(map[string]any)["region"] = "changed"

	if principal.IsAnonymous() {
		t.Fatal("Principal.IsAnonymous() = true, want false")
	}
	if got := principal.Subject(); got != "service-123" {
		t.Errorf("Principal.Subject() = %q, want service-123", got)
	}
	if got := principal.Method(); got != "bearer" {
		t.Errorf("Principal.Method() = %q, want bearer", got)
	}
	if got := principal.Issuer(); got != "https://issuer.example.test" {
		t.Errorf("Principal.Issuer() = %q", got)
	}
	if got := principal.Audiences(); !reflect.DeepEqual(got, []string{"orders", "billing"}) {
		t.Errorf("Principal.Audiences() = %#v", got)
	}
	if got := principal.TenantHints(); !reflect.DeepEqual(got, []string{"north"}) {
		t.Errorf("Principal.TenantHints() = %#v", got)
	}
	if got := principal.Scopes(); !reflect.DeepEqual(got, []string{"orders:read"}) {
		t.Errorf("Principal.Scopes() = %#v", got)
	}
	if got := principal.AuthenticatedAt(); !got.Equal(authenticatedAt) {
		t.Errorf("Principal.AuthenticatedAt() = %v", got)
	}

	returnedAudiences := principal.Audiences()
	returnedAudiences[0] = "mutated"
	returnedClaims := principal.Claims()
	returnedClaims["email"] = "mutated"
	returnedClaims["groups"].([]any)[0] = "mutated"
	returnedClaims["profile"].(map[string]any)["region"] = "mutated"

	wantClaims := map[string]any{
		"email":   "service@example.test",
		"groups":  []any{"operators"},
		"profile": map[string]any{"region": "eu"},
	}
	if got := principal.Audiences(); !reflect.DeepEqual(got, []string{"orders", "billing"}) {
		t.Errorf("Principal.Audiences() after caller mutation = %#v", got)
	}
	if got := principal.Claims(); !reflect.DeepEqual(got, wantClaims) {
		t.Errorf("Principal.Claims() after caller mutation = %#v", got)
	}
}

func TestPrincipalRejectsInvalidIdentity(t *testing.T) {
	t.Parallel()

	tests := map[string]authentication.PrincipalSpec{
		"missing subject": {Method: "basic"},
		"missing method":  {Subject: "subject"},
		"empty audience":  {Subject: "subject", Method: "basic", Audiences: []string{""}},
		"empty scope":     {Subject: "subject", Method: "basic", Scopes: []string{""}},
		"empty tenant":    {Subject: "subject", Method: "basic", TenantHints: []string{""}},
	}

	for name, spec := range tests {
		spec := spec
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := authentication.NewPrincipal(spec)
			if !errors.Is(err, authentication.ErrInvalidPrincipal) {
				t.Fatalf("NewPrincipal() error = %v, want ErrInvalidPrincipal", err)
			}
		})
	}
}

func TestAnonymousPrincipalIsExplicit(t *testing.T) {
	t.Parallel()

	principal := authentication.AnonymousPrincipal()
	if !principal.IsAnonymous() {
		t.Fatal("AnonymousPrincipal().IsAnonymous() = false, want true")
	}
	if principal.Subject() != "" || principal.Method() != "" {
		t.Fatalf("anonymous principal contains identity: subject=%q method=%q", principal.Subject(), principal.Method())
	}
}

func TestPrincipalRejectsHostileClaims(t *testing.T) {
	t.Parallel()

	tooManyClaims := make(map[string]any, authentication.MaxClaims+1)
	for index := range authentication.MaxClaims + 1 {
		tooManyClaims[fmt.Sprintf("claim-%d", index)] = index
	}
	largeSlice := make([]string, authentication.MaxClaimCollection+1)
	largeMap := make(map[string]string, authentication.MaxClaimCollection+1)
	for index := range authentication.MaxClaimCollection + 1 {
		largeMap[fmt.Sprintf("key-%d", index)] = "value"
	}
	deep := any("value")
	for range authentication.MaxClaimDepth + 1 {
		deep = []any{deep}
	}
	tests := map[string]map[string]any{
		"too many claims":      tooManyClaims,
		"empty claim name":     {"": "value"},
		"unsupported pointer":  {"value": new(string)},
		"non-string map":       {"value": map[int]string{1: "value"}},
		"oversized map":        {"value": largeMap},
		"oversized collection": {"value": largeSlice},
		"excessive depth":      {"value": deep},
		"nested invalid map":   {"value": map[string]any{"bad": new(string)}},
		"nested invalid slice": {"value": []any{new(string)}},
	}
	for name, claims := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := authentication.NewPrincipal(authentication.PrincipalSpec{
				Subject: "service",
				Method:  "bearer",
				Claims:  claims,
			})
			if !errors.Is(err, authentication.ErrInvalidPrincipal) {
				t.Fatalf("NewPrincipal() error = %v, want ErrInvalidPrincipal", err)
			}
		})
	}
}

func TestPrincipalCopiesNilArrayAndScalarClaimShapes(t *testing.T) {
	t.Parallel()

	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{
		Subject: "service",
		Method:  "bearer",
		Claims: map[string]any{
			"nil":     nil,
			"array":   [2]int{1, 2},
			"boolean": true,
			"nested":  map[string]any{"nil": nil},
		},
	})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	claims := principal.Claims()
	if claims["nil"] != nil || !reflect.DeepEqual(claims["array"], []any{1, 2}) || claims["boolean"] != true {
		t.Fatalf("claims = %#v", claims)
	}
}
