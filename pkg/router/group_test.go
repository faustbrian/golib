package router_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	router "github.com/faustbrian/golib/pkg/router"
)

func TestNestedGroupsFlattenComposition(t *testing.T) {
	t.Parallel()

	var calls []string
	layer := func(name string) router.NamedMiddleware {
		return router.NamedMiddleware{Name: name, Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls = append(calls, name+":in")
				next.ServeHTTP(writer, request)
				calls = append(calls, name+":out")
			})
		}}
	}

	builder := router.New(router.WithMiddleware(layer("router")))
	err := builder.Group(router.GroupOptions{
		Host: "{tenant}.example.com", PathPrefix: "/api", NamePrefix: "api.",
		Middleware: []router.NamedMiddleware{layer("outer")},
		Metadata:   map[string]string{"audience": "public"},
	}, func(outer *router.Builder) error {
		return outer.Group(router.GroupOptions{
			PathPrefix: "/v1", NamePrefix: "v1.",
			Middleware: []router.NamedMiddleware{layer("inner")},
			Metadata:   map[string]string{"version": "1"},
		}, func(inner *router.Builder) error {
			return inner.Register(router.Route{
				Name: "users.show", Methods: []string{"GET"}, Path: "/users/{id}",
				Middleware: []router.NamedMiddleware{layer("route")},
				Metadata:   map[string]string{"resource": "user"},
				Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					calls = append(calls, "handler")
				}),
			})
		})
	})
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	compiled := mustCompile(t, builder)

	request := httptest.NewRequest(http.MethodGet, "http://acme.example.com/api/v1/users/7", nil)
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status: got %d", response.Code)
	}
	wantCalls := []string{
		"router:in", "outer:in", "inner:in", "route:in", "handler",
		"route:out", "inner:out", "outer:out", "router:out",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("middleware order: got %v, want %v", calls, wantCalls)
	}

	info := compiled.Routes()[0]
	if info.Name != "api.v1.users.show" || info.Host != "{tenant}.example.com" || info.Pattern != "/api/v1/users/{id}" {
		t.Fatalf("flattened route: %#v", info)
	}
	wantMetadata := map[string]string{"audience": "public", "version": "1", "resource": "user"}
	if !reflect.DeepEqual(info.Metadata, wantMetadata) {
		t.Fatalf("metadata: got %v, want %v", info.Metadata, wantMetadata)
	}
}

func TestRouteMayExcludeNamedGroupMiddleware(t *testing.T) {
	t.Parallel()

	called := false
	builder := router.New()
	err := builder.Group(router.GroupOptions{Middleware: []router.NamedMiddleware{{
		Name: "group",
		Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				called = true
				next.ServeHTTP(writer, request)
			})
		},
	}}}, func(group *router.Builder) error {
		return group.Register(router.Route{
			Methods: []string{http.MethodGet}, Path: "/",
			ExcludeMiddleware: []string{"group"},
			Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusNoContent)
			}),
		})
	})
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	compiled := mustCompile(t, builder)
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusNoContent || called {
		t.Fatalf("excluded group middleware: status=%d called=%v", response.Code, called)
	}
	if middleware := compiled.Routes()[0].Middleware; len(middleware) != 0 {
		t.Fatalf("resolved middleware: %v", middleware)
	}
}

func TestGroupCompositionRejectsInvalidAndPartialState(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	tests := []struct {
		name    string
		options router.GroupOptions
		route   router.Route
	}{
		{name: "relative prefix", options: router.GroupOptions{PathPrefix: "api"}, route: router.Route{Methods: []string{"GET"}, Path: "/x", Handler: handler}},
		{name: "wildcard prefix", options: router.GroupOptions{PathPrefix: "/{api}"}, route: router.Route{Methods: []string{"GET"}, Path: "/x", Handler: handler}},
		{name: "host conflict", options: router.GroupOptions{Host: "api.example.com"}, route: router.Route{Methods: []string{"GET"}, Host: "other.example.com", Path: "/x", Handler: handler}},
		{name: "metadata conflict", options: router.GroupOptions{Metadata: map[string]string{"scope": "one"}}, route: router.Route{Methods: []string{"GET"}, Path: "/x", Handler: handler, Metadata: map[string]string{"scope": "two"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			builder := router.New()
			err := builder.Group(test.options, func(group *router.Builder) error {
				return group.Register(test.route)
			})
			if !errors.Is(err, router.ErrInvalidRoute) {
				t.Fatalf("error: got %v", err)
			}
			if len(builder.PendingRoutes()) != 0 {
				t.Fatal("failed group published partial routes")
			}
		})
	}

	builder := router.New()
	sentinel := errors.New("stop")
	err := builder.Group(router.GroupOptions{}, func(group *router.Builder) error {
		if err := group.Register(router.Route{Methods: []string{"GET"}, Path: "/x", Handler: handler}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) || len(builder.PendingRoutes()) != 0 {
		t.Fatalf("callback rollback: error=%v routes=%d", err, len(builder.PendingRoutes()))
	}
}

func TestGroupLimitsAndCapturedBuilderLifecycle(t *testing.T) {
	t.Parallel()

	limits := router.DefaultLimits()
	limits.MaxGroups = 1
	limits.MaxGroupDepth = 1
	builder := router.New(router.WithLimits(limits))
	var captured *router.Builder
	err := builder.Group(router.GroupOptions{}, func(group *router.Builder) error {
		captured = group
		return group.Group(router.GroupOptions{}, func(*router.Builder) error { return nil })
	})
	if !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("depth error: got %v", err)
	}
	if captured == nil {
		t.Fatal("group callback was not invoked")
	}
}
