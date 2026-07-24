package router_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	router "github.com/faustbrian/golib/pkg/router"
)

func TestFineGrainedInputByteBudgets(t *testing.T) {
	t.Parallel()

	handler := http.NotFoundHandler()
	limits := router.DefaultLimits()
	limits.MaxMethodBytes = 3
	if err := router.New(router.WithLimits(limits)).Register(router.Route{
		Methods: []string{"LONG"}, Path: "/", Handler: handler,
	}); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("method bytes: %v", err)
	}

	limits = router.DefaultLimits()
	limits.MaxWildcardNameBytes = 2
	if err := router.New(router.WithLimits(limits)).Register(router.Route{
		Methods: []string{"GET"}, Path: "/{long}", Handler: handler,
	}); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("wildcard name bytes: %v", err)
	}
	if err := router.New(router.WithLimits(limits)).Register(router.Route{
		Methods: []string{"GET"}, Host: "{long}.example.com", Path: "/", Handler: handler,
	}); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("host wildcard name bytes: %v", err)
	}

	limits = router.DefaultLimits()
	limits.MaxWildcardsPerRoute = 1
	if err := router.New(router.WithLimits(limits)).Register(router.Route{
		Methods: []string{"GET"}, Host: "{tenant}.example.com", Path: "/{id}", Handler: handler,
	}); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("combined host and path wildcards: %v", err)
	}

	limits = router.DefaultLimits()
	limits.MaxURLParameterBytes = 3
	builder := router.New(router.WithLimits(limits))
	mustRegister(t, builder, router.Route{
		Name: "value", Methods: []string{"GET"}, Path: "/{id}", Handler: handler,
	})
	compiled := mustCompile(t, builder)
	if _, err := compiled.Path("value", router.Param("id", "xx")); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("parameter bytes: %v", err)
	}
	limits = router.DefaultLimits()
	limits.MaxWildcardNameBytes = 1
	builder = router.New(router.WithLimits(limits))
	mustRegister(t, builder, router.Route{
		Name: "short", Methods: []string{"GET"}, Path: "/{i}", Handler: handler,
	})
	compiled = mustCompile(t, builder)
	if _, err := compiled.Path("short", router.Param("ii", "x")); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("parameter name bytes: %v", err)
	}

	limits = router.DefaultLimits()
	limits.MaxQueryBytes = 3
	builder = router.New(router.WithLimits(limits))
	mustRegister(t, builder, router.Route{
		Name: "root", Methods: []string{"GET"}, Path: "/", Handler: handler,
	})
	compiled = mustCompile(t, builder)
	base, err := router.NewBaseURL("https", "example.com")
	if err != nil {
		t.Fatalf("base: %v", err)
	}
	if _, err := compiled.URL("root", base, url.Values{"abc": {"x"}}); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("query bytes: %v", err)
	}
	if _, err := compiled.URL("root", base, url.Values{"abcd": nil}); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("query key bytes: %v", err)
	}
	limits.MaxQueryBytes = 8
	limits.MaxQueryValues = 1
	builder = router.New(router.WithLimits(limits))
	mustRegister(t, builder, router.Route{
		Name: "count", Methods: []string{"GET"}, Path: "/", Handler: handler,
	})
	compiled = mustCompile(t, builder)
	if _, err := compiled.URL("count", base, url.Values{"a": nil, "b": nil}); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("query key count: %v", err)
	}
	limits.MaxQueryBytes = 4
	limits.MaxQueryValues = router.DefaultLimits().MaxQueryValues
	builder = router.New(router.WithLimits(limits))
	mustRegister(t, builder, router.Route{
		Name: "empty", Methods: []string{"GET"}, Path: "/", Handler: handler,
	})
	compiled = mustCompile(t, builder)
	if generated, err := compiled.URL("empty", base, url.Values{"key": nil}); err != nil || generated != "https://example.com/" {
		t.Fatalf("empty query value: generated=%q error=%v", generated, err)
	}
}

func TestRouteDocumentationIsBounded(t *testing.T) {
	t.Parallel()

	limits := router.DefaultLimits()
	limits.MaxDocumentationBytes = 8
	builder := router.New(router.WithLimits(limits))
	err := builder.Register(router.Route{
		Methods:       []string{http.MethodGet},
		Path:          "/",
		Handler:       http.NotFoundHandler(),
		Documentation: "documentation is caller-controlled metadata",
	})
	if !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("documentation limit: got %v", err)
	}
}

func TestEmptyGroupStillValidatesMetadataBounds(t *testing.T) {
	t.Parallel()

	limits := router.DefaultLimits()
	limits.MaxMetadataKeyBytes = 4
	builder := router.New(router.WithLimits(limits))
	err := builder.Group(router.GroupOptions{
		Metadata: map[string]string{"unbounded-key": "value"},
	}, func(*router.Builder) error { return nil })
	if !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("group metadata limit: got %v", err)
	}
}

func TestUnsupportedConnectRouteFailsAtStartup(t *testing.T) {
	t.Parallel()

	err := router.New().Register(router.Route{
		Methods: []string{http.MethodConnect},
		Path:    "/",
		Handler: http.NotFoundHandler(),
	})
	if !errors.Is(err, router.ErrUnsupported) {
		t.Fatalf("CONNECT registration: got %v", err)
	}
}

func TestTrustedBaseAuthorityIsBounded(t *testing.T) {
	t.Parallel()

	authority := strings.Repeat("a", 300) + ".example"
	if _, err := router.NewBaseURL("https", authority); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("authority limit: got %v", err)
	}
}

func TestMiddlewareExclusionListIsBoundedBeforeCompilation(t *testing.T) {
	t.Parallel()

	limits := router.DefaultLimits()
	limits.MaxMiddleware = 1
	err := router.New(router.WithLimits(limits)).Register(router.Route{
		Methods:           []string{http.MethodGet},
		Path:              "/",
		Handler:           http.NotFoundHandler(),
		ExcludeMiddleware: []string{"first", "second"},
	})
	if !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("middleware exclusions: got %v", err)
	}

	passthrough := router.NamedMiddleware{
		Name:       "group",
		Middleware: func(next http.Handler) http.Handler { return next },
	}
	err = router.New(router.WithLimits(limits)).Group(router.GroupOptions{
		Middleware: []router.NamedMiddleware{passthrough},
	}, func(child *router.Builder) error {
		return child.Register(router.Route{
			Methods: []string{http.MethodGet}, Path: "/", Handler: http.NotFoundHandler(),
			Middleware: []router.NamedMiddleware{{Middleware: passthrough.Middleware}},
		})
	})
	if !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("resolved group middleware: got %v", err)
	}

	limits = router.DefaultLimits()
	limits.MaxMethodsPerRoute = 1
	err = router.New(router.WithLimits(limits)).Register(router.Route{
		Methods: []string{http.MethodGet, http.MethodPost},
		Path:    "/", Handler: http.NotFoundHandler(),
	})
	if !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("route method collection: got %v", err)
	}
}

func TestRemainderSegmentCountIsBounded(t *testing.T) {
	t.Parallel()

	limits := router.DefaultLimits()
	limits.MaxURLParameters = 1
	builder := router.New(router.WithLimits(limits))
	mustRegister(t, builder, router.Route{
		Name: "files", Methods: []string{http.MethodGet},
		Path: "/files/{path...}", Handler: http.NotFoundHandler(),
	})
	compiled := mustCompile(t, builder)
	if _, err := compiled.Path("files", router.Remainder("path", "one", "two")); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("remainder segment count: got %v", err)
	}
}

func TestGlobalMiddlewareIsBoundedAfterAllOptions(t *testing.T) {
	t.Parallel()

	limits := router.DefaultLimits()
	limits.MaxMiddleware = 1
	middleware := []router.NamedMiddleware{
		{Name: "first", Middleware: func(next http.Handler) http.Handler { return next }},
		{Name: "second", Middleware: func(next http.Handler) http.Handler { return next }},
	}
	for _, builder := range []*router.Builder{
		router.New(router.WithLimits(limits), router.WithMiddleware(middleware...)),
		router.New(router.WithMiddleware(middleware...), router.WithLimits(limits)),
	} {
		err := builder.Register(router.Route{
			Methods: []string{http.MethodGet}, Path: "/", Handler: http.NotFoundHandler(),
		})
		if !errors.Is(err, router.ErrLimitExceeded) {
			t.Fatalf("global middleware options: got %v", err)
		}
	}

	owned := []router.NamedMiddleware{middleware[0]}
	builder := router.New(router.WithMiddleware(owned...))
	owned[0].Name = "mutated"
	mustRegister(t, builder, router.Route{
		Methods: []string{http.MethodGet}, Path: "/", Handler: http.NotFoundHandler(),
	})
	compiled := mustCompile(t, builder)
	if got := compiled.Routes()[0].Middleware[0]; got != "first" {
		t.Fatalf("global middleware alias: got %q", got)
	}
}

func TestRemainderConstructorRejectsAboveHardCeilingWithoutCopying(t *testing.T) {
	segments := make([]string, 65_537)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	parameter := router.Remainder("path", segments...)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(parameter)
	if bytes := after.TotalAlloc - before.TotalAlloc; bytes > 65_536 {
		t.Fatalf("oversized remainder allocation: got %d bytes", bytes)
	}
	builder := router.New()
	mustRegister(t, builder, router.Route{
		Name: "files", Methods: []string{http.MethodGet},
		Path: "/files/{path...}", Handler: http.NotFoundHandler(),
	})
	if _, err := mustCompile(t, builder).Path("files", parameter); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("oversized remainder error: got %v", err)
	}
}

func TestNestedGroupPrefixesUseComposedBudgets(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		limit func(*router.Limits)
		outer router.GroupOptions
		inner router.GroupOptions
	}{
		{
			name:  "path",
			limit: func(limits *router.Limits) { limits.MaxPatternBytes = 6 },
			outer: router.GroupOptions{PathPrefix: "/abc"},
			inner: router.GroupOptions{PathPrefix: "/def"},
		},
		{
			name:  "name",
			limit: func(limits *router.Limits) { limits.MaxNameBytes = 5 },
			outer: router.GroupOptions{NamePrefix: "api."},
			inner: router.GroupOptions{NamePrefix: "v1."},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			limits := router.DefaultLimits()
			testCase.limit(&limits)
			builder := router.New(router.WithLimits(limits))
			err := builder.Group(testCase.outer, func(outer *router.Builder) error {
				return outer.Group(testCase.inner, func(*router.Builder) error { return nil })
			})
			if !errors.Is(err, router.ErrLimitExceeded) {
				t.Fatalf("composed group prefix: got %v", err)
			}
			if len(builder.PendingRoutes()) != 0 {
				t.Fatal("failed composed group published routes")
			}
		})
	}
}

func TestExhaustedGroupBudgetDoesNotInvokeAnotherCallback(t *testing.T) {
	t.Parallel()

	limits := router.DefaultLimits()
	limits.MaxGroups = 1
	builder := router.New(router.WithLimits(limits))
	if err := builder.Group(router.GroupOptions{}, func(*router.Builder) error { return nil }); err != nil {
		t.Fatalf("first group: %v", err)
	}
	called := false
	err := builder.Group(router.GroupOptions{}, func(*router.Builder) error {
		called = true
		return nil
	})
	if !errors.Is(err, router.ErrLimitExceeded) || called {
		t.Fatalf("exhausted group budget: error=%v called=%v", err, called)
	}
}

func TestSyntacticallyValidByteOverflowsUseLimitErrors(t *testing.T) {
	t.Parallel()

	handler := http.NotFoundHandler()
	for _, testCase := range []struct {
		name    string
		limit   func(*router.Limits)
		route   router.Route
		group   router.GroupOptions
		isGroup bool
	}{
		{
			name: "route name", limit: func(limits *router.Limits) { limits.MaxNameBytes = 3 },
			route: router.Route{Name: "valid", Methods: []string{http.MethodGet}, Path: "/", Handler: handler},
		},
		{
			name: "route host", limit: func(limits *router.Limits) { limits.MaxHostBytes = 3 },
			route: router.Route{Methods: []string{http.MethodGet}, Host: "api.example.com", Path: "/", Handler: handler},
		},
		{
			name: "route path", limit: func(limits *router.Limits) { limits.MaxPatternBytes = 4 },
			route: router.Route{Methods: []string{http.MethodGet}, Path: "/valid", Handler: handler},
		},
		{
			name: "group path", limit: func(limits *router.Limits) { limits.MaxPatternBytes = 4 },
			group: router.GroupOptions{PathPrefix: "/valid"}, isGroup: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			limits := router.DefaultLimits()
			testCase.limit(&limits)
			builder := router.New(router.WithLimits(limits))
			var err error
			if testCase.isGroup {
				err = builder.Group(testCase.group, func(*router.Builder) error { return nil })
			} else {
				err = builder.Register(testCase.route)
			}
			if !errors.Is(err, router.ErrLimitExceeded) {
				t.Fatalf("byte budget: got %v", err)
			}
		})
	}
}

func TestDiagnosticsAreSingleLineValidUTF8(t *testing.T) {
	t.Parallel()

	diagnostic := (&router.Error{
		Kind:   router.ErrInvalidRoute,
		Field:  "path\nforged",
		Detail: "bad\r\tdetail",
		Source: "routes.go\x00" + string([]byte{0xff}),
	}).Error()
	if strings.ContainsAny(diagnostic, "\x00\t\r\n") {
		t.Fatalf("diagnostic contains control characters: %q", diagnostic)
	}
	if !utf8.ValidString(diagnostic) {
		t.Fatalf("diagnostic is not valid UTF-8: %q", diagnostic)
	}
	if len(diagnostic) > 160 {
		t.Fatalf("diagnostic is not bounded: %d", len(diagnostic))
	}
}

func TestDiagnosticsDoNotProcessBeyondOutputBudget(t *testing.T) {
	diagnostic := &router.Error{
		Kind:   errors.New(strings.Repeat("kind", 16_384)),
		Field:  strings.Repeat("field", 16_384),
		Detail: strings.Repeat("detail", 16_384),
		Source: strings.Repeat("source", 16_384),
	}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	rendered := diagnostic.Error()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(rendered)
	if bytes := after.TotalAlloc - before.TotalAlloc; bytes > 16_384 {
		t.Fatalf("diagnostic allocation: %d bytes", bytes)
	}
}

func TestHostileRuntimeAndIdentifierInputsAreBounded(t *testing.T) {
	t.Parallel()

	limits := router.DefaultLimits()
	limits.MaxNameBytes = 4
	limits.MaxMethodBytes = 4
	limits.MaxRequestTargetBytes = 16
	passthrough := func(next http.Handler) http.Handler { return next }
	handler := http.NotFoundHandler()

	global := router.New(
		router.WithLimits(limits),
		router.WithMiddleware(router.NamedMiddleware{Name: "longer", Middleware: passthrough}),
	)
	if err := global.Register(router.Route{Methods: []string{http.MethodGet}, Path: "/", Handler: handler}); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("global middleware name: %v", err)
	}
	for name, route := range map[string]router.Route{
		"route middleware name": {
			Methods: []string{http.MethodGet}, Path: "/", Handler: handler,
			Middleware: []router.NamedMiddleware{{Name: "longer", Middleware: passthrough}},
		},
		"middleware exclusion name": {
			Methods: []string{http.MethodGet}, Path: "/", Handler: handler,
			ExcludeMiddleware: []string{"longer"},
		},
	} {
		if err := router.New(router.WithLimits(limits)).Register(route); !errors.Is(err, router.ErrLimitExceeded) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := router.New(router.WithLimits(limits)).Group(
		router.GroupOptions{NamePrefix: "longer"},
		func(*router.Builder) error { return nil },
	); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("group name prefix: %v", err)
	}
	if err := router.New(router.WithLimits(limits)).Group(
		router.GroupOptions{Middleware: []router.NamedMiddleware{{Name: "longer", Middleware: passthrough}}},
		func(*router.Builder) error { return nil },
	); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("group middleware name: %v", err)
	}
	mountLimits := limits
	mountLimits.MaxPatternBytes = 4
	if err := router.New(router.WithLimits(mountLimits)).Mount("/long", handler, router.MountOptions{}); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("mount prefix: %v", err)
	}

	builder := router.New(router.WithLimits(limits))
	mustRegister(t, builder, router.Route{
		Name: "root", Methods: []string{http.MethodGet}, Path: "/", Handler: handler,
	})
	compiled := mustCompile(t, builder)
	if _, err := compiled.Path("longer"); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("generation route name: %v", err)
	}
	base, err := router.NewBaseURL("https", "example.com")
	if err != nil {
		t.Fatalf("base URL: %v", err)
	}
	if _, err := compiled.URL("longer", base, nil); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("absolute generation route name: %v", err)
	}
	if _, err := router.NewBaseURL(strings.Repeat("h", 6), "example.com"); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("scheme bytes: %v", err)
	}

	request := httptest.NewRequest("LONGER", "/missing", nil)
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized method status: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/"+strings.Repeat("x", 32), nil)
	response = httptest.NewRecorder()
	compiled.ServeHTTP(response, request)
	if response.Code != http.StatusRequestURITooLong {
		t.Fatalf("oversized request target status: %d", response.Code)
	}
}
