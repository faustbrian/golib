package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

type valueHandler struct{}

func (valueHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func TestDefensiveOptionAndNilReceiverContracts(t *testing.T) {
	t.Parallel()

	handler := valueHandler{}
	builders := []*Builder{
		New(WithLimits(Limits{})),
		New(nil),
		New(WithNotFound(nil)),
		New(WithMethodNotAllowed(nil)),
		New(WithRedirectPolicy(RedirectPolicy(99))),
	}
	for _, builder := range builders {
		if err := builder.Register(Route{Methods: []string{"GET"}, Path: "/", Handler: handler}); err == nil {
			t.Fatal("invalid option did not surface")
		}
	}
	if _, err := New(nil).Compile(); err == nil {
		t.Fatal("compile ignored invalid option")
	}

	var builder *Builder
	if err := builder.Register(Route{}); !errors.Is(err, ErrCompileState) {
		t.Fatalf("nil register: %v", err)
	}
	if builder.PendingRoutes() != nil {
		t.Fatal("nil builder returned routes")
	}
	if _, err := builder.Compile(); !errors.Is(err, ErrCompileState) {
		t.Fatalf("nil compile: %v", err)
	}
	if err := builder.Group(GroupOptions{}, func(*Builder) error { return nil }); !errors.Is(err, ErrCompileState) {
		t.Fatalf("nil group: %v", err)
	}
	if err := builder.Mount("/", handler, MountOptions{}); !errors.Is(err, ErrCompileState) {
		t.Fatalf("nil mount: %v", err)
	}

	var compiled *Router
	if compiled.Routes() != nil {
		t.Fatal("nil router returned routes")
	}
	if _, err := compiled.Path("x"); !errors.Is(err, ErrCompileState) {
		t.Fatalf("nil path: %v", err)
	}
	if _, err := compiled.URL("x", BaseURL{}, nil); !errors.Is(err, ErrCompileState) {
		t.Fatalf("nil URL: %v", err)
	}
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("nil router status: %d", response.Code)
	}
	if _, ok := MatchedRoute(nil); ok {
		t.Fatal("nil request had a route")
	}
	if _, ok := MatchedRoute(httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)); ok {
		t.Fatal("unmatched request had a route")
	}

	var routeError *Error
	if routeError.Error() != "router error" || routeError.Unwrap() != nil {
		t.Fatal("nil typed error contract changed")
	}
	if bounded("abcdef", 2) != "ab" || bounded("aå", 2) != "a" || bounded("x", 0) != "" ||
		!isNilHandler(http.HandlerFunc(nil)) || isNilHandler(handler) || validName("") {
		t.Fatal("defensive helper contract changed")
	}
}

func TestRouteValidationBoundaryMatrix(t *testing.T) {
	t.Parallel()

	handler := valueHandler{}
	base := DefaultLimits()
	tests := []struct {
		name   string
		limits Limits
		route  Route
	}{
		{name: "source", limits: func() Limits { value := base; value.MaxSourceBytes = 1; return value }(), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler, Source: "xx"}},
		{name: "operation", limits: func() Limits { value := base; value.MaxOperationBytes = 1; return value }(), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler, Operation: "xx"}},
		{name: "metadata count", limits: func() Limits { value := base; value.MaxMetadataEntries = 1; return value }(), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler, Metadata: map[string]string{"a": "1", "b": "2"}}},
		{name: "metadata key", limits: func() Limits { value := base; value.MaxMetadataKeyBytes = 1; return value }(), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler, Metadata: map[string]string{"": "1"}}},
		{name: "metadata value", limits: func() Limits { value := base; value.MaxMetadataValueBytes = 1; return value }(), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler, Metadata: map[string]string{"a": "22"}}},
		{name: "middleware count", limits: func() Limits { value := base; value.MaxMiddleware = 1; return value }(), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler, Middleware: []NamedMiddleware{named("a"), named("b")}}},
		{name: "middleware name", limits: base, route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler, Middleware: []NamedMiddleware{{Name: " bad", Middleware: passthrough}}}},
		{name: "middleware duplicate", limits: base, route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler, Middleware: []NamedMiddleware{named("same"), named("same")}}},
		{name: "empty host label", limits: base, route: Route{Methods: []string{"GET"}, Host: "a..example", Path: "/", Handler: handler}},
		{name: "bad host wildcard name", limits: base, route: Route{Methods: []string{"GET"}, Host: "{a+b}.example", Path: "/", Handler: handler}},
		{name: "bad host braces", limits: base, route: Route{Methods: []string{"GET"}, Host: "a{b.example", Path: "/", Handler: handler}},
		{name: "wildcard count", limits: func() Limits { value := base; value.MaxWildcardsPerRoute = 1; return value }(), route: Route{Methods: []string{"GET"}, Path: "/{a}/{b}", Handler: handler}},
		{name: "bad path escape", limits: base, route: Route{Methods: []string{"GET"}, Path: "/%zz", Handler: handler}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := New(WithLimits(test.limits)).Register(test.route); err == nil {
				t.Fatal("invalid route was accepted")
			}
		})
	}

	invalidPattern := Route{Methods: []string{"GET"}, Path: "/{bad-name}", Handler: handler}
	if err := New().Register(invalidPattern); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("ServeMux panic conversion: %v", err)
	}
}

func TestCompileMiddlewareAndRouteInformationBoundaries(t *testing.T) {
	t.Parallel()

	handler := valueHandler{}
	tests := []struct {
		name    string
		builder *Builder
		route   Route
	}{
		{name: "global nil", builder: New(WithMiddleware(NamedMiddleware{})), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler}},
		{name: "global bad name", builder: New(WithMiddleware(NamedMiddleware{Name: " bad", Middleware: passthrough})), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler}},
		{name: "global duplicate", builder: New(WithMiddleware(named("x"), named("x"))), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler}},
		{name: "invalid exclusion", builder: New(WithMiddleware(named("x"))), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler, ExcludeMiddleware: []string{""}}},
		{name: "duplicate exclusion", builder: New(WithMiddleware(named("x"))), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler, ExcludeMiddleware: []string{"x", "x"}}},
		{name: "resolved duplicate", builder: New(WithMiddleware(named("x"))), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler, Middleware: []NamedMiddleware{named("x")}}},
		{name: "nil result", builder: New(WithMiddleware(NamedMiddleware{Name: "nil-result", Middleware: func(http.Handler) http.Handler { return nil }})), route: Route{Methods: []string{"GET"}, Path: "/", Handler: handler}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.builder.Register(test.route); err != nil {
				t.Fatalf("register: %v", err)
			}
			if _, err := test.builder.Compile(); err == nil {
				t.Fatal("invalid middleware compiled")
			}
		})
	}

	limits := DefaultLimits()
	limits.MaxMiddleware = 1
	builder := New(WithLimits(limits), WithMiddleware(named("global")))
	if err := builder.Register(Route{Methods: []string{"GET"}, Path: "/", Handler: handler, Middleware: []NamedMiddleware{{Middleware: passthrough}}}); err != nil {
		t.Fatalf("register depth fixture: %v", err)
	}
	if _, err := builder.Compile(); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("resolved depth: %v", err)
	}
	builder = New(WithLimits(limits), WithMiddleware(named("one"), named("two")))
	if err := builder.Register(Route{Methods: []string{"GET"}, Path: "/", Handler: handler}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("global depth before registration: %v", err)
	}

	builder = New(WithMiddleware(NamedMiddleware{Middleware: passthrough}, named("excluded")))
	if err := builder.Register(Route{Methods: []string{"GET"}, Path: "/", Handler: handler, ExcludeMiddleware: []string{"excluded"}}); err != nil {
		t.Fatalf("register exclusion: %v", err)
	}
	if _, err := builder.Compile(); err != nil {
		t.Fatalf("compile exclusion: %v", err)
	}

	limits = DefaultLimits()
	limits.MaxURLParameters = 1
	builder = New(WithLimits(limits))
	if err := builder.Register(Route{Methods: []string{"GET"}, Path: "/{a}/{b}", Handler: handler}); err != nil {
		t.Fatalf("register parameter limit: %v", err)
	}
	if _, err := builder.Compile(); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("parameter limit: %v", err)
	}

	builder = New()
	if err := builder.Register(Route{Methods: []string{"GET"}, Host: "{id}.example.com", Path: "/{id}", Handler: handler}); err != nil {
		t.Fatalf("register duplicate wildcard: %v", err)
	}
	if _, err := builder.Compile(); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("duplicate wildcard: %v", err)
	}
	builder = New()
	if err := builder.Register(Route{Methods: []string{"GET"}, Host: "{id}.{id}.example.com", Path: "/", Handler: handler}); err != nil {
		t.Fatalf("register duplicate host wildcard: %v", err)
	}
	if _, err := builder.Compile(); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("duplicate host wildcard: %v", err)
	}
}

func TestRegisterPatternOnlyConvertsRegistrationErrors(t *testing.T) {
	t.Parallel()

	if err := registerPattern(http.NewServeMux(), "", valueHandler{}, "source"); !errors.Is(err, ErrConflict) {
		t.Fatalf("registration error was not converted: %v", err)
	}

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("runtime panic was converted")
		}
		if _, ok := recovered.(runtime.Error); !ok {
			t.Fatalf("unexpected panic type: %T", recovered)
		}
	}()
	_ = registerPattern(nil, "GET /", valueHandler{}, "source")
}

func TestControlledRegistrationErrorClassification(t *testing.T) {
	t.Parallel()

	if detail, controlled := controlledRegistrationError(errors.New("controlled")); !controlled || detail != "controlled" {
		t.Fatalf("controlled error: %q/%t", detail, controlled)
	}
	for _, value := range []any{"string panic", &runtime.TypeAssertionError{}} {
		if detail, controlled := controlledRegistrationError(value); controlled || detail != "" {
			t.Fatalf("uncontrolled panic %T: %q/%t", value, detail, controlled)
		}
	}
}

func TestPatternValidationPropagatesUncontrolledPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if recovered := recover(); recovered != "uncontrolled" {
			t.Fatalf("unexpected panic: %v", recovered)
		}
	}()
	_ = validateServeMuxRegistration(func() { panic("uncontrolled") }, func(error, string, string, string) error {
		t.Fatal("uncontrolled panic reached error conversion")
		return nil
	}, "source")
}

func TestGroupBoundaryMatrix(t *testing.T) {
	t.Parallel()

	handler := valueHandler{}
	var builder *Builder
	builders := []*Builder{New(nil), func() *Builder { value := New(); _, _ = value.Compile(); return value }()}
	for _, builder = range builders {
		if err := builder.Group(GroupOptions{}, func(*Builder) error { return nil }); err == nil {
			t.Fatal("invalid builder state accepted group")
		}
	}
	if err := New().Group(GroupOptions{}, nil); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("nil callback: %v", err)
	}

	invalidOptions := []GroupOptions{
		{Host: "bad:443"},
		{NamePrefix: " bad"},
		{Middleware: []NamedMiddleware{{}}},
	}
	for _, options := range invalidOptions {
		if err := New().Group(options, func(*Builder) error { return nil }); err == nil {
			t.Fatalf("invalid group accepted: %#v", options)
		}
	}

	builder = New()
	if err := builder.Group(GroupOptions{Host: "one.example"}, func(outer *Builder) error {
		return outer.Group(GroupOptions{Host: "two.example"}, func(*Builder) error { return nil })
	}); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("nested host conflict: %v", err)
	}

	limits := DefaultLimits()
	limits.MaxGroups = 1
	builder = New(WithLimits(limits))
	if err := builder.Group(GroupOptions{}, func(*Builder) error { return nil }); err != nil {
		t.Fatalf("first group: %v", err)
	}
	if err := builder.Group(GroupOptions{}, func(*Builder) error { return nil }); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("group count: %v", err)
	}

	limits = DefaultLimits()
	limits.MaxRoutes = 1
	builder = New(WithLimits(limits))
	if err := builder.Register(Route{Methods: []string{"GET"}, Path: "/one", Handler: handler}); err != nil {
		t.Fatalf("parent route: %v", err)
	}
	if err := builder.Group(GroupOptions{}, func(child *Builder) error {
		return child.Register(Route{Methods: []string{"GET"}, Path: "/two", Handler: handler})
	}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("group route count: %v", err)
	}

	limits = DefaultLimits()
	limits.MaxMiddleware = 1
	if err := New(WithLimits(limits)).Group(GroupOptions{Middleware: []NamedMiddleware{named("a"), named("b")}}, func(*Builder) error { return nil }); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("group middleware count: %v", err)
	}
	if _, err := mergeMetadata(map[string]string{"a": "1"}, map[string]string{"b": "2"}, Limits{MaxMetadataEntries: 1}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("metadata merge limit: %v", err)
	}
	if joinPrefix("/api", "") != "/api" {
		t.Fatal("empty group path join changed")
	}
	if err := New().validatePrefix("/a/../b"); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("dot prefix: %v", err)
	}
	limits = DefaultLimits()
	limits.MaxMetadataEntries = 1
	if err := New(WithLimits(limits)).Group(GroupOptions{Metadata: map[string]string{"a": "1", "b": "2"}}, func(*Builder) error { return nil }); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("group metadata limit: %v", err)
	}
}

func TestGenerationBoundaryMatrix(t *testing.T) {
	t.Parallel()

	invalidBases := [][2]string{
		{"https", "example.com%zz"}, {"https", "example.com:0"},
		{"https", "example.com:65536"}, {"https", "example.com:"},
	}
	for _, input := range invalidBases {
		if _, err := NewBaseURL(input[0], input[1]); !errors.Is(err, ErrGeneration) {
			t.Fatalf("base %q: %v", input[1], err)
		}
	}

	builder := New()
	if err := builder.Register(Route{Name: "exact", Methods: []string{"GET"}, Path: "/exact/{$}", Handler: valueHandler{}}); err != nil {
		t.Fatalf("register exact: %v", err)
	}
	if err := builder.Register(Route{Name: "host", Methods: []string{"GET"}, Host: "{tenant}.example.com", Path: "/{id}", Handler: valueHandler{}}); err != nil {
		t.Fatalf("register host: %v", err)
	}
	compiled, err := builder.Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if path, err := compiled.Path("exact"); err != nil || path != "/exact/" {
		t.Fatalf("exact path: %q %v", path, err)
	}
	base, _ := NewBaseURL("https", "example.com")
	if _, err := compiled.URL("missing", base, nil); !errors.Is(err, ErrGeneration) {
		t.Fatalf("unknown URL route: %v", err)
	}
	if _, err := compiled.URL("host", BaseURL{}, nil); !errors.Is(err, ErrGeneration) {
		t.Fatalf("empty base: %v", err)
	}
	if _, err := compiled.URL("host", base, nil, URLParameter{name: " bad", values: []string{"x"}}); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("bad parameter name: %v", err)
	}
	if _, err := compiled.URL("host", base, nil, Param("id", "1")); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("missing host parameter: %v", err)
	}
	if _, err := compiled.URL("host", base, nil, Param("tenant", "acme"), Param("id", "1"), Param("extra", "x")); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("unused URL parameter: %v", err)
	}
	if _, err := compiled.URL("host", base, nil, Param("tenant", "acme"), Param("id", "..")); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("unsafe URL path parameter: %v", err)
	}
	if _, err := compiled.Path("host", URLParameter{name: "id", kind: segmentParameter}); !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("empty segment: %v", err)
	}

	limits := DefaultLimits()
	limits.MaxURLParameters = 1
	builder = New(WithLimits(limits))
	_ = builder.Register(Route{Name: "x", Methods: []string{"GET"}, Path: "/{x}", Handler: valueHandler{}})
	compiled, _ = builder.Compile()
	if _, err := compiled.Path("x", Param("x", "1"), Param("y", "2")); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("input parameter limit: %v", err)
	}

	limits = DefaultLimits()
	limits.MaxGeneratedURLBytes = 20
	builder = New(WithLimits(limits))
	_ = builder.Register(Route{Name: "x", Methods: []string{"GET"}, Path: "/x", Handler: valueHandler{}})
	compiled, _ = builder.Compile()
	longBase, _ := NewBaseURL("https", "long.example.com")
	if _, err := compiled.URL("x", longBase, nil); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("absolute output limit: %v", err)
	}

}

func TestMountAndDispatchBoundaryMatrix(t *testing.T) {
	t.Parallel()

	if err := New(nil).Mount("/x", valueHandler{}, MountOptions{}); err == nil {
		t.Fatal("mount ignored builder option error")
	}
	if err := New().Mount("/a//b", valueHandler{}, MountOptions{}); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("invalid mount prefix: %v", err)
	}
	builder := New()
	if err := builder.Mount("/", valueHandler{}, MountOptions{Methods: []string{"GET"}}); err != nil {
		t.Fatalf("root mount: %v", err)
	}
	compiled, err := builder.Compile()
	if err != nil {
		t.Fatalf("compile root mount: %v", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "*", nil)
	compiled.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("non-OPTIONS asterisk: %d", response.Code)
	}

	builder = New(WithRedirectPolicy(RejectRedirects))
	if err := builder.Register(Route{Methods: []string{"GET"}, Path: "/tree/", Handler: valueHandler{}}); err != nil {
		t.Fatalf("register tree: %v", err)
	}
	compiled, _ = builder.Compile()
	response = httptest.NewRecorder()
	compiled.ServeHTTP(
		response,
		httptest.NewRequestWithContext(context.Background(), http.MethodHead, "/tree", nil),
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("HEAD redirect rejection: %d", response.Code)
	}
	response = httptest.NewRecorder()
	compiled.ServeHTTP(
		response,
		httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/tree//leaf/", nil),
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("canonical trailing path rejection: %d", response.Code)
	}

	if _, matched := matchHost("{x}.example.com", ".example.com"); matched {
		t.Fatal("empty wildcard host label matched")
	}
}

func TestMountStripBoundaryRejectsMismatchedPaths(t *testing.T) {
	t.Parallel()

	called := false
	wrapped := stripMountPrefix("/api/v1", "/api/v1", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/other", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil),
	}
	requests[1].URL.RawPath = "/api%2Fv1%2Fresource"
	for _, request := range requests {
		response := httptest.NewRecorder()
		wrapped.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound || called {
			t.Fatalf("mismatched path: status=%d called=%t", response.Code, called)
		}
	}
}

func TestHostRelationHelpers(t *testing.T) {
	t.Parallel()

	if !nonCanonicalPath("") || !nonCanonicalPath("relative") {
		t.Fatal("empty or relative path was treated as canonical")
	}
	if !hostPatternsOverlap("", "a.example") || hostPatternsOverlap("a.example", "a.b.example") || hostPatternsOverlap("a.example", "b.example") {
		t.Fatal("host overlap relation changed")
	}
	if methodSetsOverlap([]string{"POST"}, []string{"GET"}) {
		t.Fatal("disjoint methods overlap")
	}
}

func named(name string) NamedMiddleware {
	return NamedMiddleware{Name: name, Middleware: passthrough}
}

func passthrough(next http.Handler) http.Handler { return next }
