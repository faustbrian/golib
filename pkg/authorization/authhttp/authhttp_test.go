package authhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

type allowAuthorizer struct{}

func (allowAuthorizer) Decide(context.Context, authorization.Request) (authorization.Decision, error) {
	return authorization.Decision{Outcome: authorization.Allow, Revision: 3}, nil
}

func TestHandlerCompatibilitySurface(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(
		allowAuthorizer{},
		func(*http.Request) (authorization.Request, error) { return authorization.Request{}, nil },
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			decision, ok := DecisionFromContext(request.Context())
			if !ok || decision.Revision != 3 {
				t.Errorf("DecisionFromContext() = (%+v, %v)", decision, ok)
			}
			if _, ok := ErrorFromContext(request.Context()); ok {
				t.Error("allowed context contains an error")
			}
			writer.WriteHeader(http.StatusNoContent)
		}),
		WithDeniedHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})),
		WithErrorHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})),
	)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNoContent {
		t.Errorf("status = %d", recorder.Code)
	}
}
