package oidc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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

func FuzzProviderMetadata(f *testing.F) {
	f.Add([]byte(`{"authorization_endpoint":"https://issuer.example/authorize","token_endpoint":"https://issuer.example/token","jwks_uri":"https://issuer.example/keys","response_types_supported":["code"],"subject_types_supported":["public"],"id_token_signing_alg_values_supported":["RS256"]}`))
	f.Add([]byte(`{}`))
	allowed := map[string]struct{}{"RS256": {}}
	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 64*1024 {
			t.Skip()
		}
		var metadata providerMetadata
		if json.Unmarshal(encoded, &metadata) == nil {
			_ = validProviderMetadata(metadata, false, allowed)
		}
	})
}

func FuzzJWKSetResponse(f *testing.F) {
	f.Add([]byte(`{"keys":[]}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 64*1024 {
			t.Skip()
		}
		set := &remoteKeySet{
			url: "https://issuer.example/keys",
			client: &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK, Header: make(http.Header),
					Body: io.NopCloser(bytes.NewReader(encoded)), Request: request,
				}, nil
			})},
			maxBodyBytes: 64 * 1024, maxKeys: 64,
			allowed: map[string]struct{}{"RS256": {}},
		}
		_, _ = set.fetch(context.Background())
	})
}

func FuzzNumericDate(f *testing.F) {
	f.Add("0")
	f.Add("1.5")
	f.Add("253402300800")
	f.Fuzz(func(t *testing.T, encoded string) {
		if len(encoded) > 1024 {
			t.Skip()
		}
		_, _ = numericDate(json.RawMessage(encoded))
	})
}
