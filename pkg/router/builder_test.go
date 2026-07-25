package router_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	router "github.com/faustbrian/golib/pkg/router"
)

func TestRegisterValidatesAndCopiesRoute(t *testing.T) {
	t.Parallel()

	methods := []string{http.MethodGet}
	metadata := map[string]string{"audience": "public"}
	middleware := []router.NamedMiddleware{{
		Name: "audit",
		Middleware: func(next http.Handler) http.Handler {
			return next
		},
	}}
	excluded := []string{"global"}
	builder := router.New()

	err := builder.Register(router.Route{
		Name:              "users.show",
		Methods:           methods,
		Path:              "/users/{id}",
		Handler:           http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Middleware:        middleware,
		ExcludeMiddleware: excluded,
		Metadata:          metadata,
		Operation:         "showUser",
		Source:            "routes/public.go:12",
	})
	if err != nil {
		t.Fatalf("register valid route: %v", err)
	}

	methods[0] = http.MethodPost
	metadata["audience"] = "internal"
	middleware[0] = router.NamedMiddleware{}
	excluded[0] = "changed"

	table := builder.PendingRoutes()
	if got := table[0].Methods[0]; got != http.MethodGet {
		t.Fatalf("method alias leaked: got %q", got)
	}
	if got := table[0].Metadata["audience"]; got != "public" {
		t.Fatalf("metadata alias leaked: got %q", got)
	}
	if got := table[0].Middleware[0].Name; got != "audit" {
		t.Fatalf("middleware alias leaked: got %q", got)
	}
	if got := table[0].ExcludeMiddleware[0]; got != "global" {
		t.Fatalf("exclusion alias leaked: got %q", got)
	}

	table[0].Methods[0] = http.MethodDelete
	table[0].Metadata["audience"] = "changed"
	table[0].Middleware[0] = router.NamedMiddleware{}
	table[0].ExcludeMiddleware[0] = "changed"
	again := builder.PendingRoutes()
	if got := again[0].Methods[0]; got != http.MethodGet {
		t.Fatalf("route table alias leaked: got %q", got)
	}
	if got := again[0].Metadata["audience"]; got != "public" {
		t.Fatalf("route metadata alias leaked: got %q", got)
	}
	if got := again[0].Middleware[0].Name; got != "audit" {
		t.Fatalf("route middleware alias leaked: got %q", got)
	}
	if got := again[0].ExcludeMiddleware[0]; got != "global" {
		t.Fatalf("route exclusion alias leaked: got %q", got)
	}
}

func TestRegisterReturnsTypedErrors(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	tests := []struct {
		name  string
		route router.Route
		kind  error
		field string
	}{
		{name: "nil handler", route: router.Route{Methods: []string{"GET"}, Path: "/x"}, kind: router.ErrInvalidRoute, field: "handler"},
		{name: "no method", route: router.Route{Path: "/x", Handler: handler}, kind: router.ErrInvalidRoute, field: "methods"},
		{name: "lowercase method", route: router.Route{Methods: []string{"get"}, Path: "/x", Handler: handler}, kind: router.ErrInvalidRoute, field: "methods"},
		{name: "duplicate method", route: router.Route{Methods: []string{"GET", "GET"}, Path: "/x", Handler: handler}, kind: router.ErrInvalidRoute, field: "methods"},
		{name: "relative path", route: router.Route{Methods: []string{"GET"}, Path: "x", Handler: handler}, kind: router.ErrInvalidRoute, field: "path"},
		{name: "dot segment", route: router.Route{Methods: []string{"GET"}, Path: "/a/../x", Handler: handler}, kind: router.ErrInvalidRoute, field: "path"},
		{name: "bad name", route: router.Route{Name: " users", Methods: []string{"GET"}, Path: "/x", Handler: handler}, kind: router.ErrInvalidRoute, field: "name"},
		{name: "bad host", route: router.Route{Methods: []string{"GET"}, Host: "https://example.com", Path: "/x", Handler: handler}, kind: router.ErrInvalidRoute, field: "host"},
		{name: "nil middleware", route: router.Route{Methods: []string{"GET"}, Path: "/x", Handler: handler, Middleware: []router.NamedMiddleware{{Name: "nil"}}}, kind: router.ErrInvalidRoute, field: "middleware"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := router.New().Register(test.route)
			if !errors.Is(err, test.kind) {
				t.Fatalf("error kind: got %v", err)
			}
			var routeErr *router.Error
			if !errors.As(err, &routeErr) {
				t.Fatalf("error type: got %T", err)
			}
			if routeErr.Field != test.field {
				t.Fatalf("error field: got %q, want %q", routeErr.Field, test.field)
			}
		})
	}
}

func TestRegisterEnforcesLimitsAndBoundsDiagnostics(t *testing.T) {
	t.Parallel()

	limits := router.DefaultLimits()
	limits.MaxRoutes = 1
	limits.MaxSourceBytes = 8
	builder := router.New(router.WithLimits(limits))
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	if err := builder.Register(router.Route{Methods: []string{"GET"}, Path: "/one", Handler: handler}); err != nil {
		t.Fatalf("register first route: %v", err)
	}
	err := builder.Register(router.Route{
		Methods: []string{"GET"}, Path: "/two", Handler: handler,
		Source: strings.Repeat("sensitive", 20),
	})
	if !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("limit error: got %v", err)
	}
	if len(err.Error()) > 160 {
		t.Fatalf("diagnostic is not bounded: %d bytes", len(err.Error()))
	}
}
