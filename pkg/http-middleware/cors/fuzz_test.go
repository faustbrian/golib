package cors_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/cors"
)

func FuzzOriginAndPreflight(f *testing.F) {
	f.Add("https://app.example", "POST", "Content-Type")
	f.Add("null", "BAD METHOD", "X-Test\r\n")
	f.Fuzz(func(t *testing.T, origin, method, headers string) {
		middleware, err := cors.New(cors.Policy{AllowedOrigins: []string{"https://app.example", "null"}, AllowedMethods: []string{"POST"}, AllowedHeaders: []string{"Content-Type"}, MaxHeaderBytes: 512})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodOptions, "/", nil)
		req.Header["Origin"] = []string{origin}
		req.Header["Access-Control-Request-Method"] = []string{method}
		req.Header["Access-Control-Request-Headers"] = []string{headers}
		recorder := httptest.NewRecorder()
		middleware(http.NotFoundHandler()).ServeHTTP(recorder, req)
		if len(recorder.Header().Get("Access-Control-Allow-Origin")) > 2048 {
			t.Fatal("unbounded origin")
		}
	})
}
