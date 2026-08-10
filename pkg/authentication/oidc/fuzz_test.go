package oidc

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	upstreamoidc "github.com/coreos/go-oidc/v3/oidc"
	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	jose "github.com/go-jose/go-jose/v4"
)

func FuzzInspectCompactToken(f *testing.F) {
	f.Add("eyJhbGciOiJSUzI1NiIsImtpZCI6ImtleSJ9.e30.signature")
	f.Add("not-a-token")
	f.Add("e30.e30.")
	f.Add("eyJhbGciOiJSUzI1NiIsImtpZCI6ImtleSJ9.eyJzdWIiOiJcdWQ4MDAifQ.signature")
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
		var fields map[string]json.RawMessage
		if inspectJSONObjectLimits(encoded, 128, 6, maximumJWKCount) == nil &&
			json.Unmarshal(encoded, &metadata) == nil &&
			json.Unmarshal(encoded, &fields) == nil && validProviderMetadataMemberTypes(fields) {
			_ = validProviderMetadata(metadata, false, allowed)
		}
	})
}

func FuzzValidateBearer(f *testing.F) {
	private := internalRSAFixture.keys[0]
	now := time.Unix(1_800_000_000, 0).UTC()
	validator, err := NewWithKeySet(Config{
		Issuer: "https://issuer.example.test", ClientID: "client",
		Algorithms: []string{"RS256"}, Clock: authtest.NewClock(now),
	}, &upstreamoidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&private.PublicKey}})
	if err != nil {
		f.Fatalf("NewWithKeySet() error = %v", err)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: private},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "key"),
	)
	if err != nil {
		f.Fatalf("NewSigner() error = %v", err)
	}
	payload, err := json.Marshal(map[string]any{
		"sub": "user", "iss": "https://issuer.example.test", "aud": "client",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	if err != nil {
		f.Fatalf("Marshal() error = %v", err)
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		f.Fatalf("Sign() error = %v", err)
	}
	valid, err := signed.CompactSerialize()
	if err != nil {
		f.Fatalf("CompactSerialize() error = %v", err)
	}
	f.Add(valid)
	f.Add("fuzz-secret-marker")
	f.Fuzz(func(t *testing.T, token string) {
		if len(token) > maximumTokenBytes {
			t.Skip()
		}
		_, validateErr := validator.ValidateBearer(context.Background(), token)
		if validateErr == nil {
			return
		}
		if !errors.Is(validateErr, authentication.ErrCredentialsInvalid) &&
			!errors.Is(validateErr, authentication.ErrCredentialsRejected) {
			t.Fatalf("ValidateBearer() error classification = %v", validateErr)
		}
		if strings.Contains(token, "fuzz-secret-marker") &&
			strings.Contains(validateErr.Error(), "fuzz-secret-marker") {
			t.Fatal("ValidateBearer() exposed token marker")
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
