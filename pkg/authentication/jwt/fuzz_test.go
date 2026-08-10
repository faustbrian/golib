package jwt

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authtest"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
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

func FuzzValidateSignedPayload(f *testing.F) {
	key, err := jwk.Import([]byte("01234567890123456789012345678901"))
	if err != nil {
		f.Fatalf("jwk.Import() error = %v", err)
	}
	_ = key.Set(jwk.KeyIDKey, "key")
	_ = key.Set(jwk.AlgorithmKey, jwa.HS256())
	set := jwk.NewSet()
	_ = set.AddKey(key)
	now := time.Unix(1_800_000_000, 0).UTC()
	validator, err := New(Config{
		Issuer: "https://issuer.example.test", Audience: "orders",
		Algorithms: []jwa.SignatureAlgorithm{jwa.HS256()}, KeySet: set,
		Clock: authtest.NewClock(now),
	})
	if err != nil {
		f.Fatalf("New() error = %v", err)
	}
	f.Add([]byte(`{"sub":"service","iss":"https://issuer.example.test","aud":"orders","iat":1800000000,"exp":1800003600}`))
	f.Add([]byte(`{"sub":"duplicate","sub":"service"}`))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > defaultMaxTokenBytes/2 {
			t.Skip()
		}
		protected := jws.NewHeaders()
		_ = protected.Set("alg", jwa.HS256())
		_ = protected.Set("kid", "key")
		token, err := jws.Sign(payload, jws.WithKey(jwa.HS256(), key, jws.WithProtectedHeaders(protected)))
		if err != nil {
			t.Fatalf("jws.Sign() error = %v", err)
		}
		_, _ = validator.ValidateBearer(context.Background(), string(token))
	})
}

func FuzzCacheLifetimeHeaders(f *testing.F) {
	f.Add("max-age=60", "0", "")
	f.Add("max-age=0, max-age=3600", "invalid", "")
	f.Fuzz(func(t *testing.T, cacheControl, age, expires string) {
		if len(cacheControl)+len(age)+len(expires) > 16*1024 {
			t.Skip()
		}
		header := http.Header{
			"Cache-Control": {cacheControl},
			"Age":           {age},
			"Expires":       {expires},
		}
		_ = cacheLifetime(header, time.Unix(1_800_000_000, 0), time.Second, time.Hour)
	})
}

func FuzzRemoteJWKResponseBoundary(f *testing.F) {
	f.Add([]byte(`{"keys":[{"kty":"oct","kid":"key","alg":"HS256","k":"MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE"}]}`), "seed", uint16(http.StatusOK))
	f.Add([]byte(`{"keys":[]}`), "", uint16(http.StatusOK))
	f.Add([]byte(`{"keys":[{"kid":"one","kid":"two"}]}`), "duplicate", uint16(http.StatusOK))
	f.Fuzz(func(t *testing.T, body []byte, headerValue string, status uint16) {
		if len(body) > 64*1024 || len(headerValue) > 64*1024 {
			t.Skip()
		}
		statuses := []int{http.StatusOK, http.StatusNotModified, http.StatusServiceUnavailable}
		responseStatus := statuses[int(status)%len(statuses)]
		source := &http.Client{Transport: internalRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: responseStatus,
				Header: http.Header{
					"Content-Type": {"application/json"},
					"X-Fuzz":       {headerValue},
				},
				Body: io.NopCloser(bytes.NewReader(body)), Request: request,
			}, nil
		})}
		client := hardenedRemoteHTTPClient(remoteConfig{
			client: source, minRefresh: time.Second, maxRefresh: time.Minute,
			maxBodyBytes: 16 * 1024, maxHeaderBytes: 16 * 1024, maxKeys: defaultMaxKeys,
		})
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://issuer.example.test/keys", nil)
		if err != nil {
			t.Fatalf("NewRequestWithContext() error = %v", err)
		}
		response, _ := client.Transport.RoundTrip(request)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
	})
}
