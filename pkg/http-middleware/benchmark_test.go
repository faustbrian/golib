package middleware_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	middleware "github.com/faustbrian/golib/pkg/http-middleware"
	"github.com/faustbrian/golib/pkg/http-middleware/admission"
	compressmw "github.com/faustbrian/golib/pkg/http-middleware/compress"
	"github.com/faustbrian/golib/pkg/http-middleware/cors"
	"github.com/faustbrian/golib/pkg/http-middleware/proxy"
	"github.com/faustbrian/golib/pkg/http-middleware/requestid"
)

func BenchmarkBaseAndDeepChains(b *testing.B) {
	for _, depth := range []int{0, 16, 128} {
		b.Run(fmt.Sprintf("depth-%d", depth), func(b *testing.B) {
			items := make([]middleware.Middleware, depth)
			for index := range items {
				items[index] = passthrough
			}
			chain, _ := middleware.New(items...)
			handler, _ := chain.Handler(http.NotFoundHandler())
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			b.ReportAllocs()
			for b.Loop() {
				handler.ServeHTTP(httptest.NewRecorder(), request)
			}
		})
	}
}

func BenchmarkRequestID(b *testing.B) {
	item, _ := requestid.New(requestid.Policy{Generator: func() (string, error) { return "generated", nil }})
	handler := item(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	b.ReportAllocs()
	for b.Loop() {
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
}

func BenchmarkProxyParsing(b *testing.B) {
	item, _ := proxy.New(proxy.Policy{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Mode: proxy.Forwarded})
	handler := item(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "http://internal/", nil)
	request.RemoteAddr = "10.0.0.1:443"
	request.Header.Set("Forwarded", "for=198.51.100.7;proto=https;host=api.example")
	b.ReportAllocs()
	for b.Loop() {
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
}

func BenchmarkCORSPreflight(b *testing.B) {
	item, _ := cors.New(cors.Policy{AllowedOrigins: []string{"https://app.example"}, AllowedMethods: []string{"POST"}, AllowedHeaders: []string{"Content-Type"}})
	handler := item(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodOptions, "/", nil)
	request.Header.Set("Origin", "https://app.example")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "Content-Type")
	b.ReportAllocs()
	for b.Loop() {
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
}

func BenchmarkCompression(b *testing.B) {
	item, _ := compressmw.New(compressmw.Policy{MinimumBytes: 1})
	handler := item(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "a bounded compressible response payload")
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Encoding", "gzip, identity;q=0")
	b.ReportAllocs()
	for b.Loop() {
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
}

func BenchmarkAdmissionContention(b *testing.B) {
	item, _ := admission.New(admission.Policy{MaxInFlight: 8})
	handler := item(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	b.ReportAllocs()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			handler.ServeHTTP(httptest.NewRecorder(), request)
		}
	})
}
