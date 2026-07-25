package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	router "github.com/faustbrian/golib/pkg/router"
)

func TestConcurrentDispatchIntrospectionAndGeneration(t *testing.T) {
	t.Parallel()

	builder := router.New()
	mustRegister(t, builder, router.Route{
		Name: "item", Methods: []string{"GET"}, Path: "/items/{id}",
		Metadata: map[string]string{"bounded": "yes"},
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("X-ID", request.PathValue("id"))
			if request.Context().Err() != nil {
				writer.Header().Set("X-Canceled", "yes")
			}
		}),
	})
	compiled := mustCompile(t, builder)

	var wait sync.WaitGroup
	for worker := range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := range 100 {
				value := strings.Repeat("x", 1+(worker+iteration)%16)
				path, err := compiled.Path("item", router.Param("id", value))
				if err != nil {
					t.Errorf("generate: %v", err)
					return
				}
				request := httptest.NewRequest(http.MethodGet, path, nil)
				expectedCanceled := ""
				if iteration%2 == 0 {
					ctx, cancel := context.WithCancel(request.Context())
					cancel()
					request = request.WithContext(ctx)
					expectedCanceled = "yes"
				}
				response := httptest.NewRecorder()
				compiled.ServeHTTP(response, request)
				if response.Code != http.StatusOK || response.Header().Get("X-ID") != value ||
					response.Header().Get("X-Canceled") != expectedCanceled {
					t.Errorf("dispatch: status=%d id=%q", response.Code, response.Header().Get("X-ID"))
					return
				}
				table := compiled.Routes()
				table[0].Metadata["bounded"] = "changed"
			}
		}()
	}
	wait.Wait()
	if compiled.Routes()[0].Metadata["bounded"] != "yes" {
		t.Fatal("concurrent introspection mutated compiled metadata")
	}
}

func FuzzRoutePatternCompilation(f *testing.F) {
	for _, pattern := range []string{"/", "/users/{id}", "/assets/{path...}", "/exact/{$}", "/a%2Fb", "/../x", "relative", "/{"} {
		f.Add(pattern)
	}
	f.Fuzz(func(t *testing.T, pattern string) {
		if len(pattern) > 4_096 {
			t.Skip()
		}
		builder := router.New()
		err := builder.Register(router.Route{
			Methods: []string{"GET"}, Path: pattern,
			Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		})
		if err != nil {
			return
		}
		if _, err := builder.Compile(); err != nil {
			t.Fatalf("validated pattern failed compilation: %v", err)
		}
	})
}

func FuzzNamedRouteRoundTrip(f *testing.F) {
	for _, value := range []string{"simple", "a/b", "hello world", "åäö", "..", "", "a%2Fb", "line\nbreak"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 2_048 {
			t.Skip()
		}
		builder := router.New()
		mustRegister(t, builder, router.Route{
			Name: "value", Methods: []string{"GET"}, Path: "/value/{value}",
			Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("X-Value", request.PathValue("value"))
			}),
		})
		compiled := mustCompile(t, builder)
		path, err := compiled.Path("value", router.Param("value", value))
		if err != nil {
			return
		}
		response := httptest.NewRecorder()
		compiled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Header().Get("X-Value") != value {
			t.Fatalf("round trip: status=%d got=%q", response.Code, response.Header().Get("X-Value"))
		}
	})
}

func FuzzRequestTargets(f *testing.F) {
	for _, target := range []string{"/", "/x", "/x%2Fy", "/../x", "/a//b", "*"} {
		f.Add(target)
	}
	builder := router.New()
	_ = builder.Register(router.Route{
		Methods: []string{"GET"}, Path: "/{path...}",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	compiled, err := builder.Compile()
	if err != nil {
		f.Fatalf("compile: %v", err)
	}
	f.Fuzz(func(t *testing.T, target string) {
		if len(target) > 4_096 {
			t.Skip()
		}
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		if err != nil {
			return
		}
		response := httptest.NewRecorder()
		compiled.ServeHTTP(response, request)
		if response.Code < 100 || response.Code > 599 {
			t.Fatalf("invalid status: %d", response.Code)
		}
	})
}

func FuzzHostMatching(f *testing.F) {
	for _, host := range []string{
		"acme.example.com", "ACME.EXAMPLE.COM:8443", "a.b.example.com",
		"täst.example.com", "example.com@evil.invalid", "[::1]", "",
	} {
		f.Add(host)
	}
	builder := router.New()
	_ = builder.Register(router.Route{
		Methods: []string{"GET"}, Host: "{tenant}.example.com", Path: "/",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	compiled, err := builder.Compile()
	if err != nil {
		f.Fatalf("compile: %v", err)
	}
	f.Fuzz(func(t *testing.T, host string) {
		if len(host) > 1_024 {
			t.Skip()
		}
		request := httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
		request.Host = host
		response := httptest.NewRecorder()
		compiled.ServeHTTP(response, request)
		if response.Code < 100 || response.Code > 599 {
			t.Fatalf("invalid status: %d", response.Code)
		}
	})
}

func FuzzGroupComposition(f *testing.F) {
	for _, seed := range [][7]string{
		{"/api", "/v1", "api.", "v1.", "{tenant}.example.com", "public", "one"},
		{"/", "", "", "", "", "root", "two"},
		{"/{bad}", "/v1", " bad", "v1.", "bad host", "value", "three"},
		{"/a/../b", "/x", "api.", "x.", "api.example.com", "value", "four"},
	} {
		f.Add(seed[0], seed[1], seed[2], seed[3], seed[4], seed[5], seed[6])
	}
	f.Fuzz(func(t *testing.T, outerPrefix, innerPrefix, outerName, innerName, host, outerMetadata, innerMetadata string) {
		if len(outerPrefix)+len(innerPrefix)+len(outerName)+len(innerName)+len(host)+len(outerMetadata)+len(innerMetadata) > 4_096 {
			t.Skip()
		}
		builder := router.New()
		err := builder.Group(router.GroupOptions{
			PathPrefix: outerPrefix, NamePrefix: outerName, Host: host,
			Middleware: []router.NamedMiddleware{{Name: "outer", Middleware: passthroughMiddleware}},
			Metadata:   map[string]string{"outer": outerMetadata},
		}, func(outer *router.Builder) error {
			return outer.Group(router.GroupOptions{
				PathPrefix: innerPrefix, NamePrefix: innerName, Host: host,
				Middleware: []router.NamedMiddleware{{Name: "inner", Middleware: passthroughMiddleware}},
				Metadata:   map[string]string{"inner": innerMetadata},
			}, func(inner *router.Builder) error {
				return inner.Register(router.Route{
					Name: "route", Methods: []string{"GET"}, Path: "/x",
					Middleware: []router.NamedMiddleware{{Name: "route", Middleware: passthroughMiddleware}},
					Metadata:   map[string]string{"route": "yes"},
					Handler:    http.NotFoundHandler(),
				})
			})
		})
		if err != nil {
			if len(builder.PendingRoutes()) != 0 {
				t.Fatal("failed group published routes")
			}
			return
		}
		compiled, err := builder.Compile()
		if err != nil {
			return
		}
		info := compiled.Routes()[0]
		expectedPrefix := testJoinPrefix(testJoinPrefix("", outerPrefix), innerPrefix)
		if info.Pattern != testJoinPrefix(expectedPrefix, "/x") || info.Name != outerName+innerName+"route" ||
			info.Host != host || !slices.Equal(info.Middleware, []string{"outer", "inner", "route"}) ||
			info.Metadata["outer"] != outerMetadata || info.Metadata["inner"] != innerMetadata || info.Metadata["route"] != "yes" {
			t.Fatalf("nested composition: %#v", info)
		}
	})
}

func passthroughMiddleware(next http.Handler) http.Handler { return next }

func testJoinPrefix(prefix, path string) string {
	if prefix == "" || prefix == "/" {
		return path
	}
	if path == "" {
		return strings.TrimSuffix(prefix, "/")
	}
	return strings.TrimSuffix(prefix, "/") + path
}

func FuzzURLGenerationInputs(f *testing.F) {
	seeds := [][7]string{
		{"asset", "path", "images|logo.svg", "https", "example.com", "q", "hello world"},
		{"missing", "path", "..", "ftp", "user@example.com", "", ""},
		{"asset", "other", "a/b", "https", "example.com:8443", "tag", "åäö"},
	}
	for _, seed := range seeds {
		f.Add(seed[0], seed[1], seed[2], seed[3], seed[4], seed[5], seed[6])
	}
	builder := router.New()
	_ = builder.Register(router.Route{
		Name: "asset", Methods: []string{"GET"}, Host: "{tenant}.example.com",
		Path: "/assets/{path...}", Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		}),
	})
	compiled, err := builder.Compile()
	if err != nil {
		f.Fatalf("compile: %v", err)
	}
	f.Fuzz(func(t *testing.T, routeName, parameterName, remainder, scheme, authority, queryKey, queryValue string) {
		if len(routeName)+len(parameterName)+len(remainder)+len(scheme)+len(authority)+len(queryKey)+len(queryValue) > 8_192 {
			t.Skip()
		}
		base, err := router.NewBaseURL(scheme, authority)
		if err != nil {
			return
		}
		query := make(url.Values)
		if queryKey != "" {
			query[queryKey] = []string{queryValue}
		}
		segments := strings.Split(remainder, "|")
		generated, err := compiled.URL(
			routeName,
			base,
			query,
			router.Param("tenant", "acme"),
			router.Remainder(parameterName, segments...),
		)
		if err != nil {
			return
		}
		parsed, err := url.Parse(generated)
		if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" {
			t.Fatalf("invalid generated URL %q: %v", generated, err)
		}
		request := httptest.NewRequest(http.MethodGet, generated, nil)
		response := httptest.NewRecorder()
		compiled.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("generated route status: %d", response.Code)
		}
	})
}

func BenchmarkCompileRoutes(b *testing.B) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	b.ReportAllocs()
	for b.Loop() {
		builder := router.New()
		for index := range 100 {
			_ = builder.Register(router.Route{
				Name: "route." + strconv.Itoa(index), Methods: []string{"GET"},
				Path: "/routes/" + strconv.Itoa(index), Handler: handler,
			})
		}
		_, _ = builder.Compile()
	}
}

func BenchmarkDispatch(b *testing.B) {
	builder := router.New()
	_ = builder.Register(router.Route{
		Methods: []string{"GET"}, Path: "/items/{id}",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	compiled, _ := builder.Compile()
	request := httptest.NewRequest(http.MethodGet, "/items/42", nil)
	response := benchmarkWriter{header: make(http.Header)}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		compiled.ServeHTTP(response, request.Clone(request.Context()))
	}
}

func BenchmarkURLGeneration(b *testing.B) {
	builder := router.New()
	_ = builder.Register(router.Route{
		Name: "item", Methods: []string{"GET"}, Path: "/items/{id}",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	compiled, _ := builder.Compile()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = compiled.Path("item", router.Param("id", "42"))
	}
}

func BenchmarkMiddlewareDepth(b *testing.B) {
	layers := make([]router.NamedMiddleware, 16)
	for index := range layers {
		layers[index] = router.NamedMiddleware{Middleware: func(next http.Handler) http.Handler { return next }}
	}
	builder := router.New(router.WithMiddleware(layers...))
	_ = builder.Register(router.Route{
		Methods: []string{"GET"}, Path: "/",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	compiled, _ := builder.Compile()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := benchmarkWriter{header: make(http.Header)}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		compiled.ServeHTTP(response, request.Clone(request.Context()))
	}
}

func BenchmarkIntrospection(b *testing.B) {
	builder := router.New()
	for index := range 100 {
		_ = builder.Register(router.Route{
			Name: "route." + strconv.Itoa(index), Methods: []string{"GET"},
			Path: "/routes/" + strconv.Itoa(index), Metadata: map[string]string{"kind": "benchmark"},
			Handler: http.NotFoundHandler(),
		})
	}
	compiled, _ := builder.Compile()
	b.ReportAllocs()
	for b.Loop() {
		_ = compiled.Routes()
	}
}

type benchmarkWriter struct {
	header http.Header
}

func (writer benchmarkWriter) Header() http.Header      { return writer.header }
func (benchmarkWriter) Write(value []byte) (int, error) { return len(value), nil }
func (benchmarkWriter) WriteHeader(int)                 {}
