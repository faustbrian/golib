package oidc

import (
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

func FuzzInspectCompactToken(f *testing.F) {
	f.Add("eyJhbGciOiJSUzI1NiIsImtpZCI6ImtleSJ9.e30.signature")
	f.Add("not-a-token")
	f.Add("e30.e30.")
	allowed := map[string]struct{}{"RS256": {}}
	f.Fuzz(func(t *testing.T, token string) {
		if len(token) > 64*1024 {
			t.Skip()
		}
		_ = inspectCompactToken(token, allowed, authentication.MaxClaims, authentication.MaxClaimDepth)
	})
}

func FuzzRemoteURL(f *testing.F) {
	f.Add("https://issuer.example.test/keys", false)
	f.Add("http://127.0.0.1/keys", true)
	f.Add("https://user:password@example.test/keys", false)
	f.Fuzz(func(t *testing.T, rawURL string, allowHTTP bool) {
		if len(rawURL) > 16*1024 {
			t.Skip()
		}
		_ = validRemoteURL(rawURL, allowHTTP)
	})
}
