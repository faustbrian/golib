package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	middleware "github.com/faustbrian/golib/pkg/http-middleware"
	"github.com/faustbrian/golib/pkg/http-middleware/requestid"
)

func ExampleChain() {
	outer := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Print("request ")
			next.ServeHTTP(w, r)
			fmt.Print("response")
		})
	}
	chain, err := middleware.New(outer)
	if err != nil {
		panic(err)
	}
	handler, err := chain.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { fmt.Print("handler ") }))
	if err != nil {
		panic(err)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	// Output: request handler response
}

func Example_requestIdentifier() {
	ids, err := requestid.New(requestid.Policy{Generator: func() (string, error) { return "example-id", nil }})
	if err != nil {
		panic(err)
	}
	recorder := httptest.NewRecorder()
	ids(http.NotFoundHandler()).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	fmt.Println(recorder.Header().Get("X-Request-ID"))
	// Output: example-id
}
