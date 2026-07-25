package jwt

import (
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
)

func FuzzInspectCompactJWT(f *testing.F) {
	f.Add("eyJhbGciOiJSUzI1NiIsImtpZCI6ImtleSJ9.e30.signature")
	f.Add("not-a-token")
	f.Add("e30.e30.")
	allowed := map[string]struct{}{"RS256": {}}
	f.Fuzz(func(t *testing.T, token string) {
		if len(token) > 64*1024 {
			t.Skip()
		}
		_ = inspectCompactJWT(token, allowed, authentication.MaxClaims, authentication.MaxClaimDepth)
	})
}
