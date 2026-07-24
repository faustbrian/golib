package oidc_test

import (
	"context"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	authoidc "github.com/faustbrian/golib/pkg/authentication/oidc"
)

func TestOpenIDConnectCoreIDTokenClaimVector(t *testing.T) {
	t.Parallel()

	// OpenID Connect Core 1.0, Section 2: non-normative ID-token claims.
	private := rsaKey(t)
	now := time.Unix(1_311_281_000, 0).UTC()
	validator := staticValidator(t, private, authoidc.Config{
		Issuer: "https://server.example.com", ClientID: "s6BhdRkqt3",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(now),
	})
	token := signIDToken(t, private, map[string]any{
		"iss": "https://server.example.com", "sub": "24400320",
		"aud": "s6BhdRkqt3", "nonce": "n-0S6_WzA2Mj",
		"exp": int64(1_311_281_970), "iat": int64(1_311_280_970),
		"auth_time": int64(1_311_280_969), "acr": "urn:mace:incommon:iap:silver",
	})
	result, err := validator.Authenticate(context.Background(), authentication.NewBearerCredential(token))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	principal, ok := result.Principal()
	if !ok || principal.Subject() != "24400320" ||
		!principal.AuthenticatedAt().Equal(time.Unix(1_311_280_969, 0).UTC()) ||
		principal.Claims()["acr"] != "urn:mace:incommon:iap:silver" {
		t.Fatalf("Authenticate() principal = (%v, %v)", principal, ok)
	}
}
