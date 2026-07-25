package router_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	router "github.com/faustbrian/golib/pkg/router"
)

func TestCompiledRouterDispatchesWithPathValuesAndMatchedRoute(t *testing.T) {
	t.Parallel()

	builder := router.New()
	err := builder.Register(router.Route{
		Name: "files.show", Methods: []string{http.MethodGet},
		Path: "/files/{name}", Metadata: map[string]string{"kind": "asset"},
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			matched, ok := router.MatchedRoute(request)
			if !ok || matched.Name != "files.show" {
				t.Errorf("matched route: got %#v, %v", matched, ok)
			}
			writer.Header().Set("X-Path-Value", request.PathValue("name"))
			writer.WriteHeader(http.StatusNoContent)
		}),
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	compiled, err := builder.Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/files/a%2Fb", nil)
	originalURL := request.URL.String()
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status: got %d", response.Code)
	}
	if got := response.Header().Get("X-Path-Value"); got != "a/b" {
		t.Fatalf("path value: got %q", got)
	}
	if request.URL.String() != originalURL {
		t.Fatalf("request URL mutated: got %q", request.URL.String())
	}
}

func TestCompiledRouterPreservesHTTPMethodSemantics(t *testing.T) {
	t.Parallel()

	builder := router.New()
	mustRegister(t, builder, router.Route{
		Methods: []string{http.MethodGet}, Path: "/items/{id}",
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("X-Handler", "get")
			_, _ = writer.Write([]byte("body"))
		}),
	})
	compiled := mustCompile(t, builder)

	tests := []struct {
		method    string
		target    string
		status    int
		allow     string
		handler   string
		bodyEmpty bool
	}{
		{method: http.MethodGet, target: "/items/1", status: http.StatusOK, handler: "get"},
		{method: http.MethodHead, target: "/items/1", status: http.StatusOK, handler: "get"},
		{method: http.MethodPost, target: "/items/1", status: http.StatusMethodNotAllowed, allow: "GET, HEAD, OPTIONS"},
		{method: http.MethodOptions, target: "/items/1", status: http.StatusNoContent, allow: "GET, HEAD, OPTIONS", bodyEmpty: true},
		{method: http.MethodGet, target: "/missing", status: http.StatusNotFound},
		{method: "BREW", target: "/missing", status: http.StatusNotImplemented},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.target, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			compiled.ServeHTTP(response, httptest.NewRequest(test.method, test.target, nil))
			if response.Code != test.status {
				t.Fatalf("status: got %d, want %d; body %q", response.Code, test.status, response.Body.String())
			}
			if got := response.Header().Get("Allow"); got != test.allow {
				t.Fatalf("allow: got %q, want %q", got, test.allow)
			}
			if got := response.Header().Get("X-Handler"); got != test.handler {
				t.Fatalf("handler: got %q, want %q", got, test.handler)
			}
			if test.bodyEmpty && response.Body.Len() != 0 {
				t.Fatalf("body must be empty: %q", response.Body.String())
			}
		})
	}
}

func TestExplicitOptionsAndHeadRoutesWin(t *testing.T) {
	t.Parallel()

	builder := router.New()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		mustRegister(t, builder, router.Route{
			Methods: []string{method}, Path: "/resource",
			Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("X-Method", method)
				writer.WriteHeader(http.StatusAccepted)
			}),
		})
	}
	compiled := mustCompile(t, builder)
	for _, method := range []string{http.MethodHead, http.MethodOptions} {
		response := httptest.NewRecorder()
		compiled.ServeHTTP(response, httptest.NewRequest(method, "/resource", nil))
		if response.Code != http.StatusAccepted || response.Header().Get("X-Method") != method {
			t.Fatalf("%s route did not win: status=%d header=%q", method, response.Code, response.Header().Get("X-Method"))
		}
	}
}

func TestMiddlewareOrderAndIntrospectionAreStableAndImmutable(t *testing.T) {
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
	mustRegister(t, builder, router.Route{
		Name: "z", Methods: []string{"POST", "GET"}, Path: "/z/{id}",
		Middleware: []router.NamedMiddleware{layer("route")},
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			calls = append(calls, "handler")
		}),
	})
	mustRegister(t, builder, router.Route{
		Name: "a", Methods: []string{"GET"}, Path: "/a",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	compiled := mustCompile(t, builder)
	compiled.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/z/1", nil))

	wantCalls := []string{"router:in", "route:in", "handler", "route:out", "router:out"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("middleware order: got %v, want %v", calls, wantCalls)
	}
	table := compiled.Routes()
	if got := []string{table[0].Name, table[1].Name}; !slices.Equal(got, []string{"a", "z"}) {
		t.Fatalf("route ordering: got %v", got)
	}
	if got := table[1].Middleware; !slices.Equal(got, []string{"router", "route"}) {
		t.Fatalf("middleware table: got %v", got)
	}
	table[1].Methods[0] = "DELETE"
	table[1].Metadata["changed"] = "yes"
	if compiled.Routes()[1].Methods[0] == "DELETE" || compiled.Routes()[1].Metadata["changed"] != "" {
		t.Fatal("route table aliases internal state")
	}
}

func TestCompileReturnsTypedConflictsAndFreezesOnlyOnSuccess(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	t.Run("duplicate name", func(t *testing.T) {
		builder := router.New()
		mustRegister(t, builder, router.Route{Name: "same", Methods: []string{"GET"}, Path: "/a", Handler: handler})
		mustRegister(t, builder, router.Route{Name: "same", Methods: []string{"POST"}, Path: "/b", Handler: handler})
		_, err := builder.Compile()
		if !errors.Is(err, router.ErrDuplicateName) {
			t.Fatalf("error: got %v", err)
		}
	})
	t.Run("pattern conflict", func(t *testing.T) {
		builder := router.New()
		mustRegister(t, builder, router.Route{Methods: []string{"GET"}, Path: "/{x}", Handler: handler})
		mustRegister(t, builder, router.Route{Methods: []string{"GET"}, Path: "/{y}", Handler: handler})
		_, err := builder.Compile()
		if !errors.Is(err, router.ErrConflict) {
			t.Fatalf("error: got %v", err)
		}
		if _, retryErr := builder.Compile(); !errors.Is(retryErr, router.ErrConflict) {
			t.Fatalf("failed compile retry: got %v", retryErr)
		}
	})
	t.Run("successful compile", func(t *testing.T) {
		builder := router.New()
		mustRegister(t, builder, router.Route{Methods: []string{"GET"}, Path: "/", Handler: handler})
		compiled := mustCompile(t, builder)
		if compiled == nil {
			t.Fatal("compiled router is nil")
		}
		if _, err := builder.Compile(); !errors.Is(err, router.ErrCompileState) {
			t.Fatalf("second compile: got %v", err)
		}
		if err := builder.Register(router.Route{Methods: []string{"GET"}, Path: "/later", Handler: handler}); !errors.Is(err, router.ErrCompileState) {
			t.Fatalf("late registration: got %v", err)
		}
	})
}

func TestConflictFailureDoesNotConstructAnyMiddleware(t *testing.T) {
	t.Parallel()

	constructed := 0
	layer := router.NamedMiddleware{
		Name: "side-effect",
		Middleware: func(next http.Handler) http.Handler {
			constructed++
			return next
		},
	}
	builder := router.New()
	for _, route := range []router.Route{
		{Name: "first", Methods: []string{http.MethodGet}, Path: "/{first}", Handler: http.NotFoundHandler(), Middleware: []router.NamedMiddleware{layer}},
		{Name: "second", Methods: []string{http.MethodGet}, Path: "/{second}", Handler: http.NotFoundHandler(), Middleware: []router.NamedMiddleware{layer}},
	} {
		mustRegister(t, builder, route)
	}
	compiled, err := builder.Compile()
	if !errors.Is(err, router.ErrConflict) || compiled != nil {
		t.Fatalf("compile conflict: router=%v error=%v", compiled, err)
	}
	if constructed != 0 {
		t.Fatalf("middleware constructed before validation completed: %d", constructed)
	}
}

func TestRegistrationOrderDoesNotChangeDispatchOrIntrospection(t *testing.T) {
	t.Parallel()

	routes := []router.Route{
		{Name: "literal", Methods: []string{"GET"}, Path: "/items/current", Handler: marker("literal")},
		{Name: "wildcard", Methods: []string{"GET"}, Path: "/items/{id}", Handler: marker("wildcard")},
		{Name: "post", Methods: []string{"POST"}, Path: "/items/{id}", Handler: marker("post")},
	}
	orders := [][]int{{0, 1, 2}, {2, 0, 1}, {1, 2, 0}}
	for _, order := range orders {
		builder := router.New()
		for _, index := range order {
			mustRegister(t, builder, routes[index])
		}
		compiled := mustCompile(t, builder)
		table := compiled.Routes()
		if got := []string{table[0].Name, table[1].Name, table[2].Name}; !slices.Equal(got, []string{"literal", "post", "wildcard"}) {
			t.Fatalf("order %v table: %v", order, got)
		}
		for _, request := range []struct {
			method string
			path   string
			want   string
		}{
			{method: "GET", path: "/items/current", want: "literal"},
			{method: "GET", path: "/items/42", want: "wildcard"},
			{method: "POST", path: "/items/42", want: "post"},
		} {
			response := httptest.NewRecorder()
			compiled.ServeHTTP(response, httptest.NewRequest(request.method, request.path, nil))
			if got := response.Header().Get("X-Route"); got != request.want {
				t.Fatalf("order %v %s %s: got %q", order, request.method, request.path, got)
			}
		}
	}
}

func TestHostPatternsMatchPortsAndSingleLabels(t *testing.T) {
	t.Parallel()

	builder := router.New()
	mustRegister(t, builder, router.Route{
		Name: "tenant", Methods: []string{"GET"}, Host: "{tenant}.example.com", Path: "/",
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("X-Tenant", request.PathValue("tenant"))
			writer.WriteHeader(http.StatusNoContent)
		}),
	})
	compiled := mustCompile(t, builder)

	request := httptest.NewRequest(http.MethodGet, "http://acme.example.com/", nil)
	request.Host = "acme.example.com:8080"
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("X-Tenant") != "acme" {
		t.Fatalf("host wildcard: status=%d tenant=%q", response.Code, response.Header().Get("X-Tenant"))
	}

	response = httptest.NewRecorder()
	compiled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://deep.acme.example.com/", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("multi-label wildcard status: got %d", response.Code)
	}
}

func TestInvalidTargetsAreRejected(t *testing.T) {
	t.Parallel()

	compiled := mustCompile(t, router.New())
	tests := []*http.Request{
		{Method: "bad method", URL: &url.URL{Path: "/"}},
		{Method: http.MethodConnect, URL: &url.URL{}, RequestURI: "example.com:443"},
		{Method: http.MethodGet},
	}
	for _, request := range tests {
		response := httptest.NewRecorder()
		compiled.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "bad request") {
			t.Fatalf("invalid target: status=%d body=%q", response.Code, response.Body.String())
		}
	}
}

func mustRegister(t *testing.T, builder *router.Builder, route router.Route) {
	t.Helper()
	if err := builder.Register(route); err != nil {
		t.Fatalf("register route: %v", err)
	}
}

func mustCompile(t *testing.T, builder *router.Builder) *router.Router {
	t.Helper()
	compiled, err := builder.Compile()
	if err != nil {
		t.Fatalf("compile router: %v", err)
	}
	return compiled
}

func marker(value string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-Route", value)
		writer.WriteHeader(http.StatusNoContent)
	})
}
