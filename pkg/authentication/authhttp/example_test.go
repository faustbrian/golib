package authhttp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authhttp"
	"github.com/faustbrian/golib/pkg/authentication/bearer"
)

func ExampleNewMiddleware() {
	extractor, _ := authhttp.NewExtractor(authhttp.BearerAuthorization())
	authenticator, _ := bearer.New(bearer.ValidatorFunc(
		func(_ context.Context, _ string) (authentication.Principal, error) {
			return authentication.NewPrincipal(authentication.PrincipalSpec{
				Subject: "service", Method: "bearer",
			})
		},
	))
	middleware, _ := authhttp.NewMiddleware(extractor, authenticator)
	handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		principal, _ := authentication.PrincipalFromContext(request.Context())
		fmt.Println(principal.Subject())
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer token")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	// Output: service
}

func ExampleWithOptionalAnonymous() {
	extractor, _ := authhttp.NewExtractor(authhttp.BearerAuthorization())
	authenticator, _ := bearer.New(bearer.ValidatorFunc(
		func(context.Context, string) (authentication.Principal, error) {
			return authentication.Principal{}, authentication.NewFailure(authentication.FailureRejected)
		},
	))
	middleware, _ := authhttp.NewMiddleware(extractor, authenticator, authhttp.WithOptionalAnonymous())
	handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		principal, found := authentication.PrincipalFromContext(request.Context())
		fmt.Println(found, principal.IsAnonymous())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	// Output: true true
}
