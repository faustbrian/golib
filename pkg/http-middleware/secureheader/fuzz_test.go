package secureheader_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/secureheader"
)

func FuzzConfiguredHeaderValues(f *testing.F) {
	f.Add("camera=()", "default-src 'none'")
	f.Add("x\r\nInjected: yes", "")
	f.Fuzz(func(t *testing.T, permissions, contentSecurity string) {
		middleware, err := secureheader.New(secureheader.Policy{
			PermissionsPolicy:     permissions,
			ContentSecurityPolicy: contentSecurity,
		})
		if err != nil {
			return
		}
		recorder := httptest.NewRecorder()
		middleware(http.NotFoundHandler()).ServeHTTP(
			recorder,
			httptest.NewRequest(http.MethodGet, "/", nil),
		)
		for name, values := range recorder.Header() {
			for _, value := range values {
				if strings.ContainsAny(name+value, "\r\n\x00") {
					t.Fatalf("unsafe response field %q: %q", name, value)
				}
			}
		}
	})
}
