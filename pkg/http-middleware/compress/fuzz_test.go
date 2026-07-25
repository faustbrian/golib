package compress_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/compress"
)

func FuzzAcceptEncoding(f *testing.F) {
	f.Add("gzip")
	f.Add("gzip;q=0, *;q=1")
	f.Fuzz(func(t *testing.T, value string) {
		middleware, err := compress.New(compress.Policy{MinimumBytes: 1, MaxHeaderBytes: 256})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header["Accept-Encoding"] = []string{value}
		recorder := httptest.NewRecorder()
		middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "payload") })).ServeHTTP(recorder, req)
		if recorder.Body.Len() > 1024 {
			t.Fatalf("response too large: %d", recorder.Body.Len())
		}
	})
}
