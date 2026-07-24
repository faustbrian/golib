package siblings_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware "github.com/faustbrian/golib/pkg/http-middleware"
	"github.com/faustbrian/golib/pkg/http-middleware/adapter"
	"github.com/faustbrian/golib/pkg/http-middleware/observe"
	router "github.com/faustbrian/golib/pkg/router"
	"github.com/faustbrian/golib/pkg/service/serverhttp"
)

func TestGoRouterProvidesBoundedObservationMetadata(t *testing.T) {
	t.Parallel()
	builder := router.New()
	if err := builder.Register(router.Route{
		Name:    "users.show",
		Methods: []string{http.MethodGet},
		Path:    "/users/{id}",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		Middleware: []router.NamedMiddleware{{
			Name: "observe-route",
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					matched, ok := router.MatchedRoute(request)
					if ok {
						observe.RecordRoute(request, matched.Name)
					}
					next.ServeHTTP(w, request)
				})
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	compiled, err := builder.Compile()
	if err != nil {
		t.Fatal(err)
	}
	var event observe.Event
	observer, _ := observe.New(observe.Policy{
		Observer: func(_ context.Context, value observe.Event) { event = value },
	})
	chain, _ := middleware.New(observer)
	handler, _ := chain.Handler(compiled)
	handler.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/users/private-id", nil),
	)
	if event.Route != "users.show" {
		t.Fatalf("route = %q", event.Route)
	}
}

func TestGoServiceCoreOwnershipCannotBeInstalledTwice(t *testing.T) {
	t.Parallel()
	requestIDs, err := serverhttp.RequestIDs(serverhttp.RequestIDConfig{})
	if err != nil {
		t.Fatal(err)
	}
	bodyLimit, err := serverhttp.LimitBody(1024)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		concern adapter.Concern
		item    func(http.Handler) http.Handler
	}{
		{adapter.Recovery, serverhttp.Recover()},
		{adapter.RequestID, requestIDs},
		{adapter.BodyLimit, bodyLimit},
	} {
		descriptor, describeErr := adapter.Named(tc.concern, tc.item)
		if describeErr != nil {
			t.Fatal(describeErr)
		}
		chain, chainErr := middleware.Described(descriptor)
		if chainErr != nil {
			t.Fatal(chainErr)
		}
		if validationErr := adapter.ValidateGoService(chain, adapter.GoServiceDefaults()); !errors.Is(validationErr, adapter.ErrDuplicateOwnership) {
			t.Fatalf("%s validation error = %v", tc.concern, validationErr)
		}
	}
}
