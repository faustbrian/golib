//go:build integration

package oidc_test

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	authoidc "github.com/faustbrian/golib/pkg/authentication/oidc"
	clockpkg "github.com/faustbrian/golib/pkg/clock"
)

func TestKeycloakProviderIssuedIDToken(t *testing.T) {
	issuer := os.Getenv("OIDC_INTEROP_ISSUER")
	tokenPath := os.Getenv("OIDC_INTEROP_TOKEN_FILE")
	clientID := os.Getenv("OIDC_INTEROP_CLIENT_ID")
	if issuer == "" || tokenPath == "" || clientID == "" {
		t.Skip("Keycloak interoperability environment is not configured")
	}
	encoded, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("ReadFile(provider token) error = %v", err)
	}
	token := strings.TrimSpace(string(encoded))
	validator, err := authoidc.New(context.Background(), authoidc.Config{
		Issuer: issuer, ClientID: clientID, Algorithms: []string{"RS256"},
		Clock: clockpkg.System{}, InsecureHTTP: true, DiscoveryTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("New(Keycloak) error = %v", err)
	}
	principal, err := validator.ValidateBearer(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateBearer(Keycloak ID token) error = %v", err)
	}
	if principal.Subject() == "" || principal.Issuer() != issuer ||
		!slices.Contains(principal.Audiences(), clientID) ||
		principal.Claims()["preferred_username"] != "alice" {
		t.Fatalf("Keycloak principal identity contract was not preserved")
	}
}
