package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/proxy"
)

func FuzzForwardedField(f *testing.F) {
	f.Add(`for=198.51.100.7;proto=https;host=api.example`)
	f.Add(`for="[2001:db8::1]:443"`)
	f.Fuzz(func(t *testing.T, value string) {
		middleware, err := proxy.New(proxy.Policy{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, MaxHeaderBytes: 256})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "http://internal/", nil)
		req.RemoteAddr = "10.0.0.1:80"
		req.Header["Forwarded"] = []string{value}
		middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			info := proxy.FromContext(r.Context())
			if len(info.Host) > 255 || len(info.Prefix) > 256 {
				t.Fatalf("unbounded info: %#v", info)
			}
		})).ServeHTTP(httptest.NewRecorder(), req)
	})
}
