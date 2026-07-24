package requestid_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/requestid"
)

func FuzzInboundIdentifier(f *testing.F) {
	f.Add("trusted", true)
	f.Add("bad\rvalue", true)
	f.Fuzz(func(t *testing.T, value string, trust bool) {
		middleware, err := requestid.New(requestid.Policy{TrustInbound: trust, MaxLength: 64, Generator: func() (string, error) { return "generated", nil }})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header["X-Request-Id"] = []string{value}
		recorder := httptest.NewRecorder()
		middleware(http.NotFoundHandler()).ServeHTTP(recorder, req)
		if got := recorder.Header().Get("X-Request-ID"); len(got) > 64 {
			t.Fatalf("identifier length = %d", len(got))
		}
	})
}
