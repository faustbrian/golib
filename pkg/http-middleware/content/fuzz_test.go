package content_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/content"
)

func FuzzAcceptMediaTypes(f *testing.F) {
	f.Add("application/json")
	f.Add("*/*;q=0.5")
	f.Fuzz(func(t *testing.T, value string) {
		middleware, err := content.New(content.Policy{ResponseTypes: []string{"application/json"}, MaxHeaderBytes: 256})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header["Accept"] = []string{value}
		middleware(http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), req)
	})
}
