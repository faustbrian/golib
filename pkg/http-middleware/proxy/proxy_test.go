package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/proxy"
)

func TestUntrustedPeerCannotInfluenceEffectiveInformation(t *testing.T) {
	t.Parallel()

	middleware, err := proxy.New(proxy.Policy{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://service.example/path", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	req.Header.Set("Forwarded", "for=192.0.2.1;proto=https;host=evil.example")
	middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		info := proxy.FromContext(r.Context())
		if info.ClientIP.String() != "203.0.113.5" || info.Provenance != proxy.Direct {
			t.Fatalf("info = %#v", info)
		}
		if info.Host != "service.example" || info.Scheme != "http" {
			t.Fatalf("effective target = %#v", info)
		}
	})).ServeHTTP(httptest.NewRecorder(), req)
}

func TestTrustedProxySelectsFirstUntrustedClient(t *testing.T) {
	t.Parallel()

	middleware, err := proxy.New(proxy.Policy{
		Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		Mode:    proxy.XForwarded,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://internal/path", nil)
	req.RemoteAddr = "10.0.0.3:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.2")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "api.example.com")
	middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		info := proxy.FromContext(r.Context())
		if info.ClientIP.String() != "198.51.100.7" || info.Provenance != proxy.TrustedForwarded {
			t.Fatalf("info = %#v", info)
		}
		if info.Scheme != "https" || info.Host != "api.example.com" {
			t.Fatalf("effective target = %#v", info)
		}
	})).ServeHTTP(httptest.NewRecorder(), req)
}

func TestTrustedProxyUsesNearestTrustedForwardingMetadata(t *testing.T) {
	t.Parallel()

	middleware, _ := proxy.New(proxy.Policy{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Mode: proxy.Forwarded})
	req := httptest.NewRequest(http.MethodGet, "http://internal/path", nil)
	req.RemoteAddr = "10.0.0.3:443"
	req.Header.Set("Forwarded", `for=198.51.100.7;proto=https;host=evil.example, for=10.0.0.2;proto=http;host=api.internal`)
	middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		info := proxy.FromContext(r.Context())
		if info.ClientIP.String() != "198.51.100.7" || info.Scheme != "http" || info.Host != "api.internal" {
			t.Fatalf("info = %#v", info)
		}
	})).ServeHTTP(httptest.NewRecorder(), req)
}

func TestMalformedForwardingMetadataFailsEntireDecisionClosed(t *testing.T) {
	t.Parallel()

	middleware, _ := proxy.New(proxy.Policy{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Mode: proxy.XForwarded})
	req := httptest.NewRequest(http.MethodGet, "http://internal/path", nil)
	req.RemoteAddr = "10.0.0.3:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header["X-Forwarded-Proto"] = []string{"https", "http"}
	middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		info := proxy.FromContext(r.Context())
		if info.Provenance != proxy.Direct || info.ClientIP.String() != "10.0.0.3" {
			t.Fatalf("info = %#v", info)
		}
	})).ServeHTTP(httptest.NewRecorder(), req)
}

func TestForwardedParsesQuotedAddressAndRejectsQuotedDelimiters(t *testing.T) {
	t.Parallel()

	middleware, _ := proxy.New(proxy.Policy{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Mode: proxy.Forwarded})
	for _, tc := range []struct {
		value, client string
		provenance    proxy.Provenance
	}{
		{value: `for="198.51.100.7:1234";proto=https`, client: "198.51.100.7", provenance: proxy.TrustedForwarded},
		{value: `for="198.51.100.7, 10.0.0.2";proto=https`, client: "10.0.0.3", provenance: proxy.Direct},
		{value: `for=198.51.100.7;host="evil example"`, client: "10.0.0.3", provenance: proxy.Direct},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://internal/path", nil)
		req.RemoteAddr = "10.0.0.3:443"
		req.Header.Set("Forwarded", tc.value)
		middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			info := proxy.FromContext(r.Context())
			if info.ClientIP.String() != tc.client || info.Provenance != tc.provenance {
				t.Fatalf("value %q info = %#v", tc.value, info)
			}
		})).ServeHTTP(httptest.NewRecorder(), req)
	}
}
