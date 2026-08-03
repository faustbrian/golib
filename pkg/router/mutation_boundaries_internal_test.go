package router

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestEveryLimitMustBePositive(t *testing.T) {
	t.Parallel()

	valid := DefaultLimits()
	if !valid.valid() {
		t.Fatal("default limits are invalid")
	}
	value := reflect.ValueOf(&valid).Elem()
	for index := range value.NumField() {
		candidate := valid
		field := reflect.ValueOf(&candidate).Elem().Field(index)
		field.SetInt(0)
		if candidate.valid() {
			t.Fatalf("zero %s was accepted", value.Type().Field(index).Name)
		}
		field.SetInt(-1)
		if candidate.valid() {
			t.Fatalf("negative %s was accepted", value.Type().Field(index).Name)
		}
	}
}

func TestDiagnosticCompositionAndUTF8Boundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		diagnostic *Error
		want       string
	}{
		{diagnostic: &Error{}, want: "router error"},
		{diagnostic: &Error{Kind: ErrConflict}, want: "route conflict"},
		{diagnostic: &Error{Field: "path"}, want: "router error: path"},
		{diagnostic: &Error{Detail: "bad"}, want: "router error: bad"},
		{diagnostic: &Error{Source: "routes.go"}, want: "router error (source routes.go)"},
		{
			diagnostic: &Error{Kind: ErrConflict, Field: "path", Detail: "bad", Source: "routes.go"},
			want:       "route conflict: path: bad (source routes.go)",
		},
	}
	for _, test := range tests {
		if got := test.diagnostic.Error(); got != test.want {
			t.Fatalf("Error() = %q, want %q", got, test.want)
		}
	}

	boundedTests := []struct {
		value string
		limit int
		want  string
	}{
		{value: "abc", limit: -1, want: ""},
		{value: "abc", limit: 0, want: ""},
		{value: "abc", limit: 1, want: "a"},
		{value: "abc", limit: 2, want: "ab"},
		{value: "abc", limit: 3, want: "abc"},
		{value: "abcd", limit: 4, want: "abcd"},
		{value: "abcd", limit: 3, want: "abc"},
		{value: "abcde", limit: 4, want: "a..."},
		{value: "aåb", limit: 2, want: "a"},
		{value: "aåbcdef", limit: 6, want: "aå..."},
		{value: string([]byte{'a', 0xff, 'b'}), limit: 8, want: "a�b"},
	}
	for _, test := range boundedTests {
		if got := bounded(test.value, test.limit); got != test.want {
			t.Fatalf("bounded(%q, %d) = %q, want %q", test.value, test.limit, got, test.want)
		}
	}
	if validUTF8Boundary("å", 2) != 2 || validUTF8Boundary("å", 1) != 0 ||
		validUTF8Boundary("aå", 2) != 1 {
		t.Fatal("UTF-8 boundary selection changed")
	}
}

func TestBuilderExactValidationBoundaries(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxRoutes = 1
	limits.MaxMethodsPerRoute = 2
	limits.MaxMethodBytes = 3
	limits.MaxMiddleware = 1
	limits.MaxMetadataEntries = 1
	limits.MaxNameBytes = 3
	limits.MaxSourceBytes = 3
	limits.MaxOperationBytes = 3
	limits.MaxDocumentationBytes = 3
	limits.MaxWildcardNameBytes = 2
	limits.MaxWildcardsPerRoute = 1
	limits.MaxHostBytes = 15
	limits.MaxPatternBytes = 15

	valid := Route{
		Name: "abc", Methods: []string{"GET", "PUT"}, Host: "api.example.com",
		Path: "/{id}", Handler: valueHandler{}, Middleware: []NamedMiddleware{named("abc")},
		ExcludeMiddleware: []string{"abc"}, Metadata: map[string]string{"a": "b"},
		Source: "src", Operation: "op1",
		Documentation: "doc",
	}
	if err := New(WithLimits(limits)).Register(valid); err != nil {
		t.Fatalf("exact route rejected: %v", err)
	}
	if err := New(WithLimits(limits)).validateRoute(Route{
		Methods: []string{"GET", "PUT", "POST"}, Path: "/", Handler: valueHandler{},
	}); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("direct overlong method count = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Route)
	}{
		{name: "methods", mutate: func(route *Route) { route.Methods = append(route.Methods, "POST") }},
		{name: "method bytes", mutate: func(route *Route) { route.Methods = []string{"POST"} }},
		{name: "middleware", mutate: func(route *Route) { route.Middleware = append(route.Middleware, named("b")) }},
		{name: "exclusions", mutate: func(route *Route) { route.ExcludeMiddleware = []string{"a", "b"} }},
		{name: "exclusion bytes", mutate: func(route *Route) { route.ExcludeMiddleware = []string{"abcd"} }},
		{name: "metadata", mutate: func(route *Route) { route.Metadata["c"] = "d" }},
		{name: "name", mutate: func(route *Route) { route.Name = "abcd" }},
		{name: "source", mutate: func(route *Route) { route.Source = "abcd" }},
		{name: "operation", mutate: func(route *Route) { route.Operation = "abcd" }},
		{name: "documentation", mutate: func(route *Route) { route.Documentation = "abcd" }},
		{name: "wildcards", mutate: func(route *Route) { route.Path = "/{id}/{x}" }},
		{name: "wildcard name", mutate: func(route *Route) { route.Path = "/{ids}" }},
		{name: "host", mutate: func(route *Route) { route.Host = "toolong.example.com" }},
		{name: "path", mutate: func(route *Route) { route.Path = "/123456789012345" }},
	}
	for _, test := range tests {
		route := cloneRoute(valid)
		test.mutate(&route)
		if err := New(WithLimits(limits)).Register(route); err == nil {
			t.Fatalf("%s boundary was accepted", test.name)
		}
	}

	optionsApplied := 0
	builder := New(
		func(*Builder) { optionsApplied++ },
		func(*Builder) { optionsApplied++ },
	)
	if optionsApplied != 2 || builder.optionErr != nil {
		t.Fatalf("options applied=%d error=%v", optionsApplied, builder.optionErr)
	}
	builder = New(func(current *Builder) {
		current.globalMiddleware = []NamedMiddleware{named("a"), named("b")}
	})
	if builder.optionErr != nil || len(builder.globalMiddleware) != 2 {
		t.Fatalf("middleware copy = %d, %v", len(builder.globalMiddleware), builder.optionErr)
	}
	continued := 0
	builder = New(nil, func(*Builder) { continued++ })
	if continued != 1 || builder.optionErr == nil {
		t.Fatalf("option continuation = %d, %v", continued, builder.optionErr)
	}
	nameLimits := DefaultLimits()
	nameLimits.MaxNameBytes = 3
	if err := New(WithLimits(nameLimits), WithMiddleware(named("abc"))).Register(Route{
		Methods: []string{"GET"}, Path: "/", Handler: valueHandler{},
	}); err != nil {
		t.Fatalf("exact global middleware name rejected: %v", err)
	}
	if err := New(WithLimits(nameLimits), WithMiddleware(named("abcd"))).Register(Route{
		Methods: []string{"GET"}, Path: "/", Handler: valueHandler{},
	}); err == nil {
		t.Fatal("overlong global middleware name accepted")
	}
	if validName("azAZ09._:-") != true || validName(".bad") || validName("a+") ||
		!isToken("AZaz09!#$%&'*+-.^_`|~") || isToken("") || isToken("bad method") {
		t.Fatal("name or token grammar changed")
	}
	for _, character := range []rune{'a' - 1, 'z' + 1, 'A' - 1, 'Z' + 1, '0' - 1, '9' + 1} {
		if asciiLetterOrDigit(character) {
			t.Fatalf("non-alphanumeric %q accepted", character)
		}
	}

	hostBuilder := New(WithLimits(limits))
	for _, host := range []string{"api.example.com", "{i}.example.com", "a.b.c"} {
		if err := hostBuilder.validateHost(host, "src"); err != nil {
			t.Fatalf("valid host %q rejected: %v", host, err)
		}
	}
	if err := hostBuilder.validateHost("{id}.example.co", "src"); err != nil {
		t.Fatalf("exact host wildcard name rejected: %v", err)
	}
	for _, host := range []string{
		"å.example", "bad/path", "{id.example", "id}.example", "a{b.example", "{i}.bad_label",
	} {
		if err := hostBuilder.validateHost(host, "src"); err == nil {
			t.Fatalf("invalid host %q accepted", host)
		}
	}
	for _, path := range []string{"/", "/a/b", "/{id}"} {
		if err := hostBuilder.validatePath(path, "src"); err != nil {
			t.Fatalf("valid path %q rejected: %v", path, err)
		}
	}
	if err := hostBuilder.validatePath("/12345678901234", "src"); err != nil {
		t.Fatalf("exact path byte maximum rejected: %v", err)
	}
	for _, path := range []string{"", "relative", "/a/../b"} {
		if err := hostBuilder.validatePath(path, "src"); err == nil {
			t.Fatalf("invalid path %q accepted", path)
		}
	}
	if hostBuilder.validatePath("/123456789012345", "src") == nil {
		t.Fatal("overlong path accepted")
	}
	if validName("a.+") {
		t.Fatal("name validation stopped before a later invalid character")
	}
}

func TestCompilerHelperBoundaries(t *testing.T) {
	t.Parallel()

	if !methodSetsOverlap([]string{"GET"}, []string{"HEAD"}) ||
		!methodSetsOverlap([]string{"HEAD"}, []string{"GET"}) ||
		!methodSetsOverlap([]string{"POST", "PUT"}, []string{"PATCH", "PUT"}) ||
		methodSetsOverlap([]string{"POST"}, []string{"GET"}) {
		t.Fatal("method overlap changed")
	}
	if hostSignature("{tenant}.EXAMPLE.com") != "{}.example.com" ||
		hostSpecificity("") != -1 || hostSpecificity("{x}.example.com") != 2 ||
		!hostPatternsOverlap("{x}.example.com", "api.example.com") ||
		hostPatternsOverlap("a.example.com", "b.example.com") ||
		hostPatternsOverlap("a.example.com", "example.com") {
		t.Fatal("host compilation helpers changed")
	}
	if !compiledHostLess("api.example.com", "{x}.example.com") ||
		!compiledHostLess("api.example.com", "www.example.com") ||
		compiledHostLess("www.example.com", "api.example.com") {
		t.Fatal("compiled host ordering changed")
	}
	if routeSortKey(Route{Name: "x", Host: "h", Path: "/p", Methods: []string{"PUT", "GET"}}) !=
		"x\x00h\x00/p\x00GET,PUT" {
		t.Fatal("route sort key changed")
	}

	builder := New(WithMiddleware(named("one"), named("two")))
	resolved, names, resolveErr := builder.resolveMiddleware(Route{
		Source: "src", ExcludeMiddleware: []string{"one"}, Middleware: []NamedMiddleware{{Middleware: passthrough}},
	})
	if resolveErr != nil || len(resolved) != 2 || !reflect.DeepEqual(names, []string{"two", ""}) {
		t.Fatalf("resolved middleware = %#v, %#v, %v", resolved, names, resolveErr)
	}
	exactMiddlewareLimits := DefaultLimits()
	exactMiddlewareLimits.MaxMiddleware = 2
	exactMiddlewareBuilder := New(WithLimits(exactMiddlewareLimits), WithMiddleware(named("one")))
	if _, _, err := exactMiddlewareBuilder.resolveMiddleware(Route{Middleware: []NamedMiddleware{named("two")}}); err != nil {
		t.Fatalf("exact resolved middleware depth rejected: %v", err)
	}
	if _, _, err := New().resolveMiddleware(Route{Middleware: []NamedMiddleware{
		{Middleware: passthrough}, named("same"), named("same"),
	}}); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("duplicate after unnamed middleware = %v", err)
	}
	if _, _, err := New().resolveMiddleware(Route{ExcludeMiddleware: []string{"bad+"}}); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("invalid middleware exclusion = %v", err)
	}
	order := ""
	wrapped, middlewareErr := applyMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		order += "h"
	}), []NamedMiddleware{
		{Name: "one", Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { order += "1"; next.ServeHTTP(writer, request) })
		}},
		{Name: "two", Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { order += "2"; next.ServeHTTP(writer, request) })
		}},
	}, "src")
	if middlewareErr != nil {
		t.Fatal(middlewareErr)
	}
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if order != "12h" {
		t.Fatalf("middleware order = %q", order)
	}

	errorFn := func(kind error, field, source, detail string) error {
		return &Error{Kind: kind, Field: field, Source: source, Detail: detail}
	}
	if err := validateNames([]Route{{}, {Name: "one"}, {Name: "two"}}, errorFn); err != nil {
		t.Fatalf("unique names rejected: %v", err)
	}
	if err := validateNames([]Route{{}, {Name: "same"}, {Name: "same"}}, errorFn); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate names = %v", err)
	}
	if err := validateHostPatternConflicts([]Route{
		{Host: "{x}.example.com", Methods: []string{"GET"}},
		{Host: "api.{x}.com", Methods: []string{"HEAD"}},
	}, errorFn); !errors.Is(err, ErrConflict) {
		t.Fatalf("overlapping hosts = %v", err)
	}
	if err := validateHostPatternConflicts([]Route{
		{Host: "one.example.com", Methods: []string{"POST"}},
		{Host: "{x}.example.com", Methods: []string{"GET"}},
		{Host: "api.{x}.com", Methods: []string{"HEAD"}},
	}, errorFn); !errors.Is(err, ErrConflict) {
		t.Fatalf("later overlapping hosts = %v", err)
	}
	if err := validateHostPatternConflicts([]Route{
		{Host: "{x}.example.com", Methods: []string{"GET"}},
		{Host: "{y}.example.com", Methods: []string{"POST"}},
		{Host: "api.{x}.com", Methods: []string{"HEAD"}},
	}, errorFn); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict after equal signature = %v", err)
	}
	if err := validateHostPatternConflicts([]Route{
		{Host: "{x}.example.com", Methods: []string{"GET"}},
		{Host: "static.example.com", Methods: []string{"POST"}},
		{Host: "api.{x}.com", Methods: []string{"HEAD"}},
	}, errorFn); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict after non-overlap = %v", err)
	}
	if err := validateHostPatternConflicts([]Route{
		{Host: "{x}.example.com", Methods: []string{"GET"}},
		{Host: "api.{x}.org", Methods: []string{"POST"}},
	}, errorFn); err != nil {
		t.Fatalf("independent hosts conflicted: %v", err)
	}

	compileBuilder := New()
	for _, route := range []Route{
		{Name: "z", Methods: []string{"GET"}, Host: "{x}.example.com", Path: "/z", Handler: valueHandler{}},
		{Name: "a", Methods: []string{"GET"}, Host: "api.example.com", Path: "/a", Handler: valueHandler{}},
		{Name: "m", Methods: []string{"GET"}, Host: "www.example.com", Path: "/m", Handler: valueHandler{}},
	} {
		if err := compileBuilder.Register(route); err != nil {
			t.Fatal(err)
		}
	}
	compiled, err := compileBuilder.Compile()
	if err != nil {
		t.Fatal(err)
	}
	patterns := []string{compiled.hosts[0].pattern, compiled.hosts[1].pattern, compiled.hosts[2].pattern}
	if !reflect.DeepEqual(patterns, []string{"api.example.com", "www.example.com", "{x}.example.com"}) {
		t.Fatalf("compiled host order = %#v", patterns)
	}
}

func TestGenerationHelperExactBoundaries(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxURLParameters = 2
	limits.MaxURLParameterBytes = 6
	limits.MaxWildcardNameBytes = 3
	provided, parameterErr := collectParameters([]URLParameter{Param("id", "12"), Param("x", "y")}, limits)
	if parameterErr != nil || len(provided) != 2 {
		t.Fatalf("exact parameters = %#v, %v", provided, parameterErr)
	}
	invalid := [][]URLParameter{
		{Param("id", "1"), Param("x", "2"), Param("z", "3")},
		{Param("name", "1")},
		{Param("id", "12345")},
		{Param("id", "1"), Param("id", "2")},
		{{name: "1bad", values: []string{"x"}}},
		{{name: "id", kind: remainderParameter, oversized: true}},
	}
	for index, parameters := range invalid {
		if _, err := collectParameters(parameters, limits); err == nil {
			t.Fatalf("invalid parameter set %d accepted", index)
		}
	}

	path, used, pathErr := renderPath("/fixed/{id}/{rest...}/{$}", map[string]URLParameter{
		"id": Param("id", "a b"), "rest": Remainder("rest", "x", "y"),
	})
	if pathErr != nil || path != "/fixed/a%20b/x/y/" || len(used) != 2 {
		t.Fatalf("rendered path = %q, %#v, %v", path, used, pathErr)
	}
	for _, test := range []struct {
		pattern   string
		name      string
		parameter URLParameter
	}{
		{pattern: "/{id}", name: "id", parameter: Remainder("id", "x")},
		{pattern: "/{rest...}", name: "rest", parameter: Param("rest", "x")},
		{pattern: "/{id}", name: "id", parameter: URLParameter{name: "id", kind: segmentParameter}},
	} {
		if _, _, err := renderPath(test.pattern, map[string]URLParameter{test.name: test.parameter}); err == nil {
			t.Fatalf("wrong path parameter accepted: %#v", test.parameter)
		}
	}

	host, hostUsed, hostErr := renderHost("{tenant}.example.com", "base.example:8443", map[string]URLParameter{
		"tenant": Param("tenant", "acme"),
	})
	if hostErr != nil || host != "acme.example.com:8443" || len(hostUsed) != 1 {
		t.Fatalf("rendered host = %q, %#v, %v", host, hostUsed, hostErr)
	}
	if name, kind, wildcard := pathWildcard("{rest...}"); name != "rest" || kind != remainderParameter || !wildcard {
		t.Fatalf("remainder wildcard = %q, %d, %t", name, kind, wildcard)
	}
	if _, _, wildcard := pathWildcard("ab"); wildcard {
		t.Fatal("short literal became wildcard")
	}
	if err := rejectUnused(map[string]URLParameter{"x": Param("x", "1")}, map[string]struct{}{}); err == nil {
		t.Fatal("unused parameter accepted")
	}
	if !safePathSegment("value") || safePathSegment("") || safePathSegment(".") ||
		safePathSegment("..") || safePathSegment("//") || safePathSegment("a\x00b") {
		t.Fatal("safe path segment grammar changed")
	}
	validHosts := []string{"a", "a-b", "A9"}
	for _, value := range validHosts {
		if !safeHostLabel(value) {
			t.Fatalf("valid host label %q rejected", value)
		}
	}
	for _, value := range []string{"", "-a", "a-", strings.Repeat("a", 64), "a_b"} {
		if safeHostLabel(value) {
			t.Fatalf("invalid host label %q accepted", value)
		}
	}

	queryLimits := DefaultLimits()
	queryLimits.MaxQueryValues = 2
	queryLimits.MaxQueryBytes = 4
	if err := validateQuery(url.Values{"a": {"1"}, "b": {}}, queryLimits); err != nil {
		t.Fatalf("exact query rejected: %v", err)
	}
	for _, query := range []url.Values{
		{"a": {"1", "2", "3"}}, {"abcde": {""}}, {"a": {"1234"}},
	} {
		if err := validateQuery(query, queryLimits); err == nil {
			t.Fatalf("invalid query accepted: %#v", query)
		}
	}

	exactRemainder := make([]string, maxRemainderSegments)
	if parameter := Remainder("rest", exactRemainder...); parameter.oversized || len(parameter.values) != maxRemainderSegments {
		t.Fatal("exact remainder segment maximum rejected")
	}
	if parameter := Remainder("rest", make([]string, maxRemainderSegments+1)...); !parameter.oversized || parameter.values != nil {
		t.Fatal("oversized remainder was copied or accepted")
	}
	if base, err := NewBaseURL("HTTPS", strings.Repeat("a", maxTrustedAuthorityBytes)); err != nil ||
		base.scheme != "https" {
		t.Fatalf("exact base URL rejected: %#v, %v", base, err)
	}
	if _, err := NewBaseURL("httpss", "example.com"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("overlong scheme = %v", err)
	}
	if _, err := NewBaseURL("https", ""); !errors.Is(err, ErrGeneration) {
		t.Fatalf("empty base authority = %v", err)
	}
	for _, name := range []string{"a", "Z", "_", "a0", "å1"} {
		if !validWildcardName(name) {
			t.Fatalf("valid wildcard name %q rejected", name)
		}
	}
	for _, name := range []string{"", "0a", "-a", "a-"} {
		if validWildcardName(name) {
			t.Fatalf("invalid wildcard name %q accepted", name)
		}
	}
	if ascii("å") || !ascii("azAZ09") {
		t.Fatal("ASCII classification changed")
	}
	if !ascii(string(rune(127))) || ascii(string(rune(128))) {
		t.Fatal("ASCII numeric boundary changed")
	}

	generatedLimits := DefaultLimits()
	generatedLimits.MaxNameBytes = 1
	generatedLimits.MaxGeneratedURLBytes = 2
	generatedRouter := &Router{
		limits: generatedLimits,
		named:  map[string]generationRoute{"x": {path: "/x"}},
	}
	if path, err := generatedRouter.Path("x"); err != nil || path != "/x" {
		t.Fatalf("exact generated path = %q, %v", path, err)
	}
	if _, err := generatedRouter.Path("xx"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("overlong generated name = %v", err)
	}
	generatedLimits.MaxGeneratedURLBytes = len("https://e/x")
	generatedRouter.limits = generatedLimits
	if absolute, err := generatedRouter.URL("x", BaseURL{scheme: "https", host: "e"}, nil); err != nil || absolute != "https://e/x" {
		t.Fatalf("exact generated URL = %q, %v", absolute, err)
	}

	if path, _, err := renderPath("/{$}/after/{id}", map[string]URLParameter{"id": Param("id", "x")}); err != nil || path != "//after/x" {
		t.Fatalf("middle exact marker = %q, %v", path, err)
	}
	if _, _, err := renderPath("/{id}", map[string]URLParameter{
		"id": {name: "id", kind: segmentParameter, values: []string{"x", "y"}},
	}); err == nil {
		t.Fatal("multi-value segment parameter accepted")
	}
	if host, _, err := renderHost("api.{tenant}.example", "base", map[string]URLParameter{
		"tenant": Param("tenant", "acme"),
	}); err != nil || host != "api.acme.example" {
		t.Fatalf("literal-first host = %q, %v", host, err)
	}
	if _, _, err := renderHost("{tenant}.example", "base", map[string]URLParameter{
		"tenant": Remainder("tenant", "x"),
	}); err == nil {
		t.Fatal("remainder host parameter accepted")
	}
	if _, _, err := renderHost("{tenant}.example", "base", map[string]URLParameter{
		"tenant": {name: "tenant", kind: segmentParameter, values: []string{"x", "y"}},
	}); err == nil {
		t.Fatal("multi-value host parameter accepted")
	}
	if _, _, wildcard := pathWildcard("abc"); wildcard {
		t.Fatal("three-byte literal became wildcard")
	}
	if name, kind, wildcard := pathWildcard("{x}"); !wildcard || name != "x" || kind != segmentParameter {
		t.Fatalf("three-byte wildcard = %q, %d, %t", name, kind, wildcard)
	}
	if _, _, wildcard := pathWildcard("{xy"); wildcard {
		t.Fatal("unterminated wildcard accepted")
	}
	if !safeHostLabel(strings.Repeat("a", 63)) || safeHostLabel(strings.Repeat("a", 64)) {
		t.Fatal("host label byte boundary changed")
	}

	byteLimits := DefaultLimits()
	byteLimits.MaxURLParameters = 2
	byteLimits.MaxWildcardNameBytes = 2
	byteLimits.MaxURLParameterBytes = 4
	if _, err := collectParameters([]URLParameter{{name: "id", values: []string{"12"}}}, byteLimits); err != nil {
		t.Fatalf("exact cumulative parameter bytes rejected: %v", err)
	}
	if _, err := collectParameters([]URLParameter{{name: "id", values: []string{"123"}}}, byteLimits); err == nil {
		t.Fatal("cumulative parameter bytes overflow accepted")
	}
	if _, err := collectParameters([]URLParameter{{name: "id", kind: remainderParameter, values: []string{"", ""}}}, byteLimits); err != nil {
		t.Fatalf("exact parameter value count rejected: %v", err)
	}
	multiByteLimits := DefaultLimits()
	multiByteLimits.MaxURLParameters = 2
	multiByteLimits.MaxWildcardNameBytes = 2
	multiByteLimits.MaxURLParameterBytes = 4
	if _, err := collectParameters([]URLParameter{
		{name: "i", values: []string{"1"}}, {name: "id", values: []string{""}},
	}, multiByteLimits); err != nil {
		t.Fatalf("exact multi-parameter bytes rejected: %v", err)
	}
	if _, err := collectParameters([]URLParameter{
		{name: "i", values: []string{"1"}}, {name: "id", values: []string{"2"}},
	}, multiByteLimits); err == nil {
		t.Fatal("multi-parameter byte overflow accepted")
	}
	valueLimits := DefaultLimits()
	valueLimits.MaxURLParameters = 2
	if _, err := collectParameters([]URLParameter{
		Param("a", "1"), Remainder("b", "2", "3"),
	}, valueLimits); err == nil {
		t.Fatal("cumulative parameter value overflow accepted")
	}
	nameByteLimits := DefaultLimits()
	nameByteLimits.MaxURLParameters = 2
	nameByteLimits.MaxWildcardNameBytes = 2
	nameByteLimits.MaxURLParameterBytes = 2
	if _, err := collectParameters([]URLParameter{
		{name: "a", values: []string{""}}, {name: "bb", values: []string{""}},
	}, nameByteLimits); err == nil {
		t.Fatal("cumulative parameter-name byte overflow accepted")
	}

	singleQueryLimits := DefaultLimits()
	singleQueryLimits.MaxQueryValues = 2
	singleQueryLimits.MaxQueryBytes = 4
	if err := validateQuery(url.Values{"a": {"1", "2"}}, singleQueryLimits); err != nil {
		t.Fatalf("exact query values rejected: %v", err)
	}
	if err := validateQuery(url.Values{"a": {"1", "234"}}, singleQueryLimits); err == nil {
		t.Fatal("cumulative query bytes overflow accepted")
	}
	multiQueryLimits := DefaultLimits()
	multiQueryLimits.MaxQueryValues = 3
	multiQueryLimits.MaxQueryBytes = 5
	if err := validateQuery(url.Values{"abcde": {}}, multiQueryLimits); err != nil {
		t.Fatalf("exact query-key byte budget rejected: %v", err)
	}
	if err := validateQuery(url.Values{"a": {"1234"}}, multiQueryLimits); err != nil {
		t.Fatalf("exact single-query byte budget rejected: %v", err)
	}
	if err := validateQuery(url.Values{"a": {"1"}, "b": {"2"}, "c": {}}, multiQueryLimits); err != nil {
		t.Fatalf("exact multi-query budget rejected: %v", err)
	}
	if err := validateQuery(url.Values{"a": {"1"}, "bb": {"22"}}, multiQueryLimits); err == nil {
		t.Fatal("multi-query byte overflow accepted")
	}
	queryCountLimits := DefaultLimits()
	queryCountLimits.MaxQueryValues = 2
	if err := validateQuery(url.Values{"empty": {}, "values": {"1", "2"}}, queryCountLimits); err == nil {
		t.Fatal("empty and populated query count overflow accepted")
	}
	queryCountLimits.MaxQueryValues = 3
	if err := validateQuery(url.Values{"one": {"1", "2"}, "two": {"3", "4"}}, queryCountLimits); err == nil {
		t.Fatal("multi-key query count overflow accepted")
	}
}

func TestGroupAndMetadataExactBoundaries(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxMetadataEntries = 2
	limits.MaxMetadataKeyBytes = 2
	limits.MaxMetadataValueBytes = 2
	merged, metadataErr := mergeMetadata(map[string]string{"a": "1"}, map[string]string{"bb": "22"}, limits)
	if metadataErr != nil || !reflect.DeepEqual(merged, map[string]string{"a": "1", "bb": "22"}) {
		t.Fatalf("metadata merge = %#v, %v", merged, metadataErr)
	}
	for _, entry := range [][2]string{{"", "1"}, {"abc", "1"}, {"a", "123"}} {
		if validateMetadataEntry(entry[0], entry[1], limits) == nil {
			t.Fatalf("invalid metadata accepted: %#v", entry)
		}
	}
	if _, err := mergeMetadata(map[string]string{"a": "1"}, map[string]string{"a": "2"}, limits); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("metadata conflict = %v", err)
	}
	if joinPrefix("", "/x") != "/x" || joinPrefix("/", "/x") != "/x" ||
		joinPrefix("/api/", "") != "/api" || joinPrefix("/api/", "/x") != "/api/x" {
		t.Fatal("prefix joining changed")
	}
	middleware := []NamedMiddleware{named("one"), {Middleware: passthrough}, named("two")}
	filtered := excludeInheritedMiddleware(middleware, []string{"one"})
	if len(filtered) != 2 || filtered[0].Name != "" || filtered[1].Name != "two" {
		t.Fatalf("filtered middleware = %#v", filtered)
	}

	accountingLimits := DefaultLimits()
	accountingLimits.MaxGroups = 3
	accountingLimits.MaxGroupDepth = 2
	accountingLimits.MaxRoutes = 3
	builder := New(WithLimits(accountingLimits))
	if err := builder.Register(Route{Methods: []string{"GET"}, Path: "/parent", Handler: valueHandler{}}); err != nil {
		t.Fatal(err)
	}
	if err := builder.Group(GroupOptions{PathPrefix: "/api", NamePrefix: "api."}, func(child *Builder) error {
		if child.limits.MaxGroups != 2 || child.limits.MaxRoutes != 2 || child.groupDepth != 1 {
			t.Fatalf("child budgets = groups:%d routes:%d depth:%d", child.limits.MaxGroups, child.limits.MaxRoutes, child.groupDepth)
		}
		if err := child.Register(Route{Name: "one", Methods: []string{"GET"}, Path: "/one", Handler: valueHandler{}}); err != nil {
			return err
		}
		return child.Group(GroupOptions{}, func(grandchild *Builder) error {
			if grandchild.groupDepth != 2 {
				t.Fatalf("grandchild depth = %d", grandchild.groupDepth)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	if builder.groups != 2 || len(builder.routes) != 2 || builder.routes[1].Path != "/api/one" ||
		builder.routes[1].Name != "api.one" {
		t.Fatalf("group accounting = groups:%d routes:%#v", builder.groups, builder.routes)
	}
	builder.groupDepth = accountingLimits.MaxGroupDepth
	if err := builder.Group(GroupOptions{}, func(*Builder) error { return nil }); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("depth overflow = %v", err)
	}
	builder = New(WithLimits(accountingLimits))
	builder.groups = 1
	if err := builder.Group(GroupOptions{}, func(child *Builder) error {
		if child.limits.MaxGroups != 1 {
			t.Fatalf("remaining group budget = %d", child.limits.MaxGroups)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if builder.groups != 2 {
		t.Fatalf("existing group count update = %d", builder.groups)
	}

	exactPrefixLimits := DefaultLimits()
	exactPrefixLimits.MaxPatternBytes = 4
	if err := New(WithLimits(exactPrefixLimits)).validatePrefix("/abc"); err != nil {
		t.Fatalf("exact prefix rejected: %v", err)
	}
	if err := New(WithLimits(exactPrefixLimits)).validatePrefix("/abcd"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("overlong prefix = %v", err)
	}
	compositionLimits := DefaultLimits()
	compositionLimits.MaxPatternBytes = 4
	compositionLimits.MaxNameBytes = 3
	compositionLimits.MaxMiddleware = 1
	compositionBuilder := New(WithLimits(compositionLimits))
	state, err := compositionBuilder.composeGroup(GroupOptions{
		PathPrefix: "/abc", NamePrefix: "abc", Middleware: []NamedMiddleware{{Middleware: passthrough}},
	})
	if err != nil || state.pathPrefix != "/abc" || state.namePrefix != "abc" || len(state.middleware) != 1 {
		t.Fatalf("exact group composition = %#v, %v", state, err)
	}
	if _, err := compositionBuilder.composeGroup(GroupOptions{Middleware: []NamedMiddleware{named("abc")}}); err != nil {
		t.Fatalf("exact group middleware name rejected: %v", err)
	}
	for _, options := range []GroupOptions{
		{PathPrefix: "/abcd"}, {NamePrefix: "abcd"},
		{Middleware: []NamedMiddleware{named("a"), named("b")}},
		{Middleware: []NamedMiddleware{named("abcd")}},
		{Middleware: []NamedMiddleware{{Name: "a+", Middleware: passthrough}}},
	} {
		if _, err := compositionBuilder.composeGroup(options); err == nil {
			t.Fatalf("invalid group composition accepted: %#v", options)
		}
	}
}

func TestRouterDispatchAndHostHelperBoundaries(t *testing.T) {
	t.Parallel()

	if redirectRoot("/tree/") != "/tree" || redirectRoot("/tree/{$}") != "/tree" ||
		redirectRoot("/files/{rest...}") != "/files" || redirectRoot("/plain") != "" {
		t.Fatal("redirect root changed")
	}
	for _, value := range []string{"", "relative", "/a//b", "/a/../b"} {
		if !nonCanonicalPath(value) {
			t.Fatalf("non-canonical path %q accepted", value)
		}
	}
	for _, value := range []string{"/", "/a", "/a/"} {
		if nonCanonicalPath(value) {
			t.Fatalf("canonical path %q rejected", value)
		}
	}
	if values, matched := matchHost("{x}.example.com", "api.EXAMPLE.com"); !matched ||
		!reflect.DeepEqual(values, []string{"api"}) {
		t.Fatalf("host match = %#v, %t", values, matched)
	}
	if _, matched := matchHost("{x}.example.com", "example.com"); matched {
		t.Fatal("different label count matched")
	}
	if _, matched := matchHost("{x}.example.com", "api.wrong.com"); matched {
		t.Fatal("literal host suffix mismatch was ignored")
	}
	if !isWildcardLabel("{x}") || isWildcardLabel("{}") || isWildcardLabel("x") {
		t.Fatal("wildcard label grammar changed")
	}
	if authorityHost("[::1]:8443") != "::1" || authorityHost("example.com") != "example.com" {
		t.Fatal("authority host extraction changed")
	}
	for _, authority := range []string{
		"example.com", "example.com:1", "example.com:65535", "[::1]:443",
	} {
		if invalidAuthority(authority) {
			t.Fatalf("valid authority %q rejected", authority)
		}
	}
	if invalidAuthority(strings.Repeat("a", maxTrustedAuthorityBytes)) {
		t.Fatal("exact authority byte maximum rejected")
	}
	for _, authority := range []string{
		strings.Repeat("a", maxTrustedAuthorityBytes+1), "bad/path", "example.com:0",
		"example.com:65536", "example.com:", "user@example.com",
	} {
		if !invalidAuthority(authority) {
			t.Fatalf("invalid authority %q accepted", authority)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/a?b=c", nil)
	request.RequestURI = "/a?b=c"
	if requestTargetTooLong(request, len(request.RequestURI)) ||
		!requestTargetTooLong(request, len(request.RequestURI)-1) {
		t.Fatal("request target boundary changed")
	}
	combined := httptest.NewRequest(http.MethodGet, "/ab?cd", nil)
	combined.RequestURI = ""
	if requestTargetTooLong(combined, 5) || !requestTargetTooLong(combined, 4) {
		t.Fatal("combined escaped target boundary changed")
	}
	componentCases := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{name: "request URI", mutate: func(value *http.Request) { value.RequestURI = "1234" }},
		{name: "path", mutate: func(value *http.Request) { value.RequestURI = ""; value.URL.Path = "1234" }},
		{name: "raw path", mutate: func(value *http.Request) { value.RequestURI = ""; value.URL.Path = "/a"; value.URL.RawPath = "%2Fa" }},
		{name: "raw query", mutate: func(value *http.Request) { value.RequestURI = ""; value.URL.Path = ""; value.URL.RawQuery = "1234" }},
	}
	for _, test := range componentCases {
		candidate := httptest.NewRequest(http.MethodGet, "/", nil)
		test.mutate(candidate)
		if requestTargetTooLong(candidate, 4) {
			t.Fatalf("exact %s byte maximum rejected", test.name)
		}
	}
	escapedCandidate := httptest.NewRequest(http.MethodGet, "/", nil)
	escapedCandidate.RequestURI = ""
	escapedCandidate.URL.Path = "/å"
	if !requestTargetTooLong(escapedCandidate, 4) {
		t.Fatal("escaped request-target overflow accepted")
	}
	for _, mutate := range []func(*http.Request){
		func(value *http.Request) { value.RequestURI = strings.Repeat("x", 5) },
		func(value *http.Request) { value.URL.Path = strings.Repeat("x", 5) },
		func(value *http.Request) { value.URL.RawPath = strings.Repeat("x", 5) },
		func(value *http.Request) { value.URL.RawQuery = strings.Repeat("x", 5) },
	} {
		candidate := httptest.NewRequest(http.MethodGet, "/", nil)
		candidate.RequestURI = "/"
		mutate(candidate)
		if !requestTargetTooLong(candidate, 4) {
			t.Fatal("individual request-target component overflow accepted")
		}
	}

	builder := New()
	for _, method := range []string{"GET", "POST"} {
		if err := builder.Register(Route{Methods: []string{method}, Path: "/x", Handler: valueHandler{}}); err != nil {
			t.Fatal(err)
		}
	}
	compiled, compileErr := builder.Compile()
	if compileErr != nil {
		t.Fatal(compileErr)
	}
	if got := compiled.allMethods(); !reflect.DeepEqual(got, []string{"GET", "HEAD", "OPTIONS", "POST"}) {
		t.Fatalf("all methods = %#v", got)
	}
	if got := (&Router{supported: map[string]struct{}{}}).allMethods(); len(got) != 0 {
		t.Fatalf("empty method set = %#v", got)
	}
	emptyResponse := httptest.NewRecorder()
	(&Router{automaticOptions: true, supported: map[string]struct{}{}, limits: DefaultLimits()}).ServeHTTP(
		emptyResponse,
		httptest.NewRequest(http.MethodOptions, "*", nil),
	)
	_, emptyAllowPresent := emptyResponse.Header()["Allow"]
	if emptyResponse.Code != http.StatusNoContent || emptyAllowPresent {
		t.Fatalf("empty asterisk options = %d, %q", emptyResponse.Code, emptyResponse.Header().Get("Allow"))
	}
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, httptest.NewRequest(http.MethodOptions, "*", nil))
	if response.Code != http.StatusNoContent || response.Header().Get("Allow") != "GET, HEAD, OPTIONS, POST" {
		t.Fatalf("asterisk options = %d, %q", response.Code, response.Header().Get("Allow"))
	}
	methodLimits := DefaultLimits()
	methodLimits.MaxMethodBytes = 3
	methodBuilder := New(WithLimits(methodLimits))
	if err := methodBuilder.Register(Route{Methods: []string{"GET"}, Path: "/", Handler: valueHandler{}}); err != nil {
		t.Fatal(err)
	}
	methodRouter, methodCompileErr := methodBuilder.Compile()
	if methodCompileErr != nil {
		t.Fatal(methodCompileErr)
	}
	response = httptest.NewRecorder()
	methodRouter.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("exact method length status = %d", response.Code)
	}
	response = httptest.NewRecorder()
	methodRouter.ServeHTTP(response, httptest.NewRequest("POST", "/", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("overlong method status = %d", response.Code)
	}
	if _, err := methodRouter.URL("", BaseURL{scheme: "https"}, nil); !errors.Is(err, ErrGeneration) {
		t.Fatalf("base without host = %v", err)
	}

	redirecting := testCompiledHost(http.MethodGet, "/other")
	redirectMux := http.NewServeMux()
	redirectMux.Handle(http.MethodGet+" /target", http.NotFoundHandler())
	redirecting.redirects = []*http.ServeMux{redirectMux}
	matching := testCompiledHost(http.MethodGet, "/target")
	rejectingRouter := &Router{redirectPolicy: RejectRedirects}
	targetRequest := httptest.NewRequest(http.MethodGet, "/target", nil)
	if got := rejectingRouter.matchingHostForMethod([]*compiledHost{redirecting, matching}, targetRequest); got != matching {
		t.Fatalf("matching host after redirect = %p, want %p", got, matching)
	}
	if got := rejectingRouter.allowedMethods([]*compiledHost{redirecting, matching}, targetRequest); !reflect.DeepEqual(got, []string{"GET", "HEAD"}) {
		t.Fatalf("allowed methods after redirect = %#v", got)
	}
	nonCanonical := testCompiledHost(http.MethodGet, "/a/b")
	nonCanonicalRequest := httptest.NewRequest(http.MethodGet, "/a//b", nil)
	followingRouter := &Router{redirectPolicy: FollowRedirects}
	if !followingRouter.hostMatchesRequest(nonCanonical, nonCanonicalRequest) {
		t.Fatal("follow policy rejected a redirecting match")
	}
	if rejectingRouter.hostMatchesRequest(nonCanonical, nonCanonicalRequest) {
		t.Fatal("reject policy accepted a redirecting match")
	}
	if !rejectingRouter.hostMatchesRequest(matching, targetRequest) {
		t.Fatal("reject policy rejected a canonical match")
	}
	missing := testCompiledHost(http.MethodPost, "/missing")
	if rejectingRouter.hostMatchesRequest(missing, targetRequest) {
		t.Fatal("missing pattern accepted as a match")
	}
}

func testCompiledHost(method, path string) *compiledHost {
	mux := http.NewServeMux()
	mux.Handle(method+" "+path, valueHandler{})
	return &compiledHost{
		mux:       mux,
		methods:   map[string]struct{}{method: {}},
		redirects: []*http.ServeMux{},
	}
}

func TestMountExactPrefixAndRawPathBoundaries(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	limits.MaxPatternBytes = 4
	err := New(WithLimits(limits)).Mount("/abc", valueHandler{}, MountOptions{Methods: []string{"GET"}})
	var exactError *Error
	if !errors.As(err, &exactError) || exactError.Detail != "path pattern is too long" {
		t.Fatalf("exact mount prefix error = %v", err)
	}
	for _, prefix := range []string{"/abcd", "", "relative", "/{x}"} {
		if err := New(WithLimits(limits)).Mount(prefix, valueHandler{}, MountOptions{Methods: []string{"GET"}}); err == nil {
			t.Fatalf("invalid mount prefix %q accepted", prefix)
		}
	}

	var paths []string
	handler := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.Path+"|"+request.URL.RawPath)
	})
	wrapped := stripMountPrefix("/api/v1", "/api/v1", handler)
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/one", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/one/two", nil),
	}
	requests[0].URL.RawPath = "/api/v1"
	requests[1].URL.RawPath = "/api/v1/one"
	requests[2].URL.RawPath = "/api/v1/one/two"
	for _, request := range requests {
		wrapped.ServeHTTP(httptest.NewRecorder(), request)
	}
	if !reflect.DeepEqual(paths, []string{"|", "/one|/one", "/one/two|/one/two"}) {
		t.Fatalf("stripped paths = %#v", paths)
	}
	missingRaw := httptest.NewRequest(http.MethodGet, "/api/v1", nil)
	missingRaw.URL.RawPath = "/api%2Fv1"
	response := httptest.NewRecorder()
	wrapped.ServeHTTP(response, missingRaw)
	if response.Code != http.StatusNotFound {
		t.Fatalf("short raw path status = %d", response.Code)
	}
	rootPaths := []string{}
	root := stripMountPrefix("/", "/", http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		rootPaths = append(rootPaths, request.URL.Path+"|"+request.URL.RawPath)
	}))
	rootRequest := httptest.NewRequest(http.MethodGet, "/one", nil)
	rootRequest.URL.RawPath = "/one"
	root.ServeHTTP(httptest.NewRecorder(), rootRequest)
	if !reflect.DeepEqual(rootPaths, []string{"one|one"}) {
		t.Fatalf("root stripped path = %#v", rootPaths)
	}
	invalidBoundary := stripMountPrefix("/api", "/api", valueHandler{})
	response = httptest.NewRecorder()
	invalidBoundary.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/apix", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("non-segment boundary status = %d", response.Code)
	}
}
