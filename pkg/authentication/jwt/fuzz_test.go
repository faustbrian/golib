package jwt

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

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
