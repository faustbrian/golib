package bodylimit_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/bodylimit"
)

func FuzzBodyLimit(f *testing.F) {
	f.Add([]byte("body"), uint16(4), true)
	f.Add([]byte("overflow"), uint16(1), false)
	f.Fuzz(func(t *testing.T, payload []byte, rawLimit uint16, knownLength bool) {
		limit := int64(rawLimit) + 1
		middleware, err := bodylimit.New(bodylimit.Policy{MaxBytes: limit})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))
		if !knownLength {
			request.ContentLength = -1
		}
		recorder := httptest.NewRecorder()
		middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, readErr := io.ReadAll(r.Body); readErr == nil {
				w.WriteHeader(http.StatusNoContent)
			}
		})).ServeHTTP(recorder, request)
		want := http.StatusNoContent
		if int64(len(payload)) > limit {
			want = http.StatusRequestEntityTooLarge
		}
		if recorder.Code != want {
			t.Fatalf("status = %d, want %d", recorder.Code, want)
		}
	})
}
