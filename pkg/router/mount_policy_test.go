package router_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	router "github.com/faustbrian/golib/pkg/router"
)

func TestMountStripsPathOnCloneAndPreservesRequestTarget(t *testing.T) {
	t.Parallel()

	builder := router.New()
	err := builder.Mount("/rpc", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Path", request.URL.Path)
		writer.Header().Set("X-Raw-Path", request.URL.RawPath)
		writer.Header().Set("X-Request-URI", request.RequestURI)
		writer.Header().Set("X-Mount-Value", request.PathValue("mount"))
		writer.WriteHeader(http.StatusNoContent)
	}), router.MountOptions{Name: "rpc", StripPrefix: true})
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	compiled := mustCompile(t, builder)
	request := httptest.NewRequest(http.MethodPost, "/rpc/methods/a%2Fb", nil)
	original := request.URL.String()
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status: got %d", response.Code)
	}
	if response.Header().Get("X-Path") != "/methods/a/b" || response.Header().Get("X-Raw-Path") != "/methods/a%2Fb" {
		t.Fatalf("stripped paths: path=%q raw=%q", response.Header().Get("X-Path"), response.Header().Get("X-Raw-Path"))
	}
	if response.Header().Get("X-Request-URI") != "/rpc/methods/a%2Fb" {
		t.Fatalf("request target: got %q", response.Header().Get("X-Request-URI"))
	}
	if response.Header().Get("X-Mount-Value") != "methods/a/b" {
		t.Fatalf("mount path value: got %q", response.Header().Get("X-Mount-Value"))
	}
	if request.URL.String() != original {
		t.Fatalf("original URL mutated: got %q", request.URL.String())
	}
	info := compiled.Routes()[0]
	if info.Name != "rpc" || info.Pattern != "/rpc/{mount...}" {
		t.Fatalf("mount introspection: %#v", info)
	}
}

func TestMountStripsEscapedLiteralPrefixWithoutLosingRawPath(t *testing.T) {
	t.Parallel()

	builder := router.New()
	err := builder.Mount("/caf%C3%A9", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Path", request.URL.Path)
		writer.Header().Set("X-Raw-Path", request.URL.RawPath)
		writer.Header().Set("X-Request-URI", request.RequestURI)
		writer.WriteHeader(http.StatusNoContent)
	}), router.MountOptions{StripPrefix: true})
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	compiled := mustCompile(t, builder)
	request := httptest.NewRequest(http.MethodGet, "/caf%C3%A9/files/a%2Fb", nil)
	original := request.URL.String()
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status: got %d body=%q", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Path") != "/files/a/b" ||
		response.Header().Get("X-Raw-Path") != "/files/a%2Fb" {
		t.Fatalf("stripped paths: path=%q raw=%q",
			response.Header().Get("X-Path"), response.Header().Get("X-Raw-Path"))
	}
	if response.Header().Get("X-Request-URI") != "/caf%C3%A9/files/a%2Fb" {
		t.Fatalf("request target: got %q", response.Header().Get("X-Request-URI"))
	}
	if request.URL.String() != original {
		t.Fatalf("original URL mutated: got %q", request.URL.String())
	}
}

func TestCompiledRouterMountIsAnOrdinaryHandler(t *testing.T) {
	t.Parallel()

	innerBuilder := router.New()
	mustRegister(t, innerBuilder, router.Route{
		Methods: []string{"GET"}, Path: "/health/{probe}",
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("X-Outer-Path-Value", request.PathValue("mount"))
			writer.Header().Set("X-Inner-Path-Value", request.PathValue("probe"))
			writer.WriteHeader(http.StatusNoContent)
		}),
	})
	mustRegister(t, innerBuilder, router.Route{
		Methods: []string{"GET"}, Path: "/collision/{mount}",
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("X-Colliding-Path-Value", request.PathValue("mount"))
			writer.WriteHeader(http.StatusNoContent)
		}),
	})
	outerBuilder := router.New()
	if err := outerBuilder.Mount("/service", mustCompile(t, innerBuilder), router.MountOptions{StripPrefix: true}); err != nil {
		t.Fatalf("mount router: %v", err)
	}
	outer := mustCompile(t, outerBuilder)
	response := httptest.NewRecorder()
	outer.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/service/health/ready", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status: got %d", response.Code)
	}
	if response.Header().Get("X-Outer-Path-Value") != "health/ready" ||
		response.Header().Get("X-Inner-Path-Value") != "ready" {
		t.Fatalf("nested path values: outer=%q inner=%q",
			response.Header().Get("X-Outer-Path-Value"), response.Header().Get("X-Inner-Path-Value"))
	}
	response = httptest.NewRecorder()
	outer.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/service/collision/inner", nil))
	if response.Code != http.StatusNoContent || response.Header().Get("X-Colliding-Path-Value") != "inner" {
		t.Fatalf("inner path value did not win: status=%d value=%q",
			response.Code, response.Header().Get("X-Colliding-Path-Value"))
	}
}

func TestCustomErrorHandlerOwnsPartialResponsesAndPanics(t *testing.T) {
	t.Parallel()

	builder := router.New(router.WithNotFound(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTeapot)
		_, _ = writer.Write([]byte("partial"))
		panic("custom error handler")
	})))
	mustRegister(t, builder, router.Route{
		Methods: []string{http.MethodGet}, Path: "/known", Handler: http.NotFoundHandler(),
	})
	compiled := mustCompile(t, builder)
	response := httptest.NewRecorder()
	defer func() {
		if recover() == nil {
			t.Error("router recovered a custom error-handler panic")
		}
		if response.Code != http.StatusTeapot || response.Body.String() != "partial" {
			t.Errorf("partial response changed: status=%d body=%q", response.Code, response.Body.String())
		}
	}()
	compiled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))
}

func TestMethodNotAllowedHandlerOwnsCancellationPartialWriteAndPanic(t *testing.T) {
	t.Parallel()

	builder := router.New(router.WithMethodNotAllowed(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !errors.Is(request.Context().Err(), context.Canceled) {
			t.Errorf("context error: %v", request.Context().Err())
		}
		writer.WriteHeader(http.StatusTeapot)
		_, _ = writer.Write([]byte("partial"))
		panic("custom method-not-allowed handler")
	})))
	mustRegister(t, builder, router.Route{
		Methods: []string{http.MethodGet}, Path: "/known", Handler: http.NotFoundHandler(),
	})
	request := httptest.NewRequest(http.MethodPost, "/known", nil)
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()
	defer func() {
		if recover() == nil {
			t.Error("router recovered a custom method-not-allowed panic")
		}
		if response.Code != http.StatusTeapot || response.Body.String() != "partial" ||
			response.Header().Get("Allow") != "GET, HEAD, OPTIONS" {
			t.Errorf("partial response changed: status=%d body=%q allow=%q",
				response.Code, response.Body.String(), response.Header().Get("Allow"))
		}
	}()
	mustCompile(t, builder).ServeHTTP(response, request.WithContext(ctx))
}

func TestMountValidationAndConflictReturnErrors(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if err := router.New().Mount("relative", handler, router.MountOptions{}); !errors.Is(err, router.ErrInvalidRoute) {
		t.Fatalf("prefix error: got %v", err)
	}
	if err := router.New().Mount("/bad%zz", handler, router.MountOptions{StripPrefix: true}); !errors.Is(err, router.ErrInvalidRoute) {
		t.Fatalf("prefix escape error: got %v", err)
	}
	if err := router.New().Mount("/x", nil, router.MountOptions{}); !errors.Is(err, router.ErrInvalidRoute) {
		t.Fatalf("handler error: got %v", err)
	}
	builder := router.New()
	if err := builder.Mount("/x", handler, router.MountOptions{}); err != nil {
		t.Fatalf("first mount: %v", err)
	}
	if err := builder.Mount("/x", handler, router.MountOptions{}); err != nil {
		t.Fatalf("second mount registration: %v", err)
	}
	if _, err := builder.Compile(); !errors.Is(err, router.ErrConflict) {
		t.Fatalf("mount conflict: got %v", err)
	}
}

func TestMountCopiesCallerOwnedOptions(t *testing.T) {
	t.Parallel()

	methods := []string{http.MethodGet}
	middleware := []router.NamedMiddleware{{
		Name: "audit", Middleware: func(next http.Handler) http.Handler { return next },
	}}
	metadata := map[string]string{"scope": "public"}
	builder := router.New()
	if err := builder.Mount("/assets", http.NotFoundHandler(), router.MountOptions{
		Methods: methods, Middleware: middleware, Metadata: metadata,
	}); err != nil {
		t.Fatalf("mount: %v", err)
	}
	methods[0] = http.MethodPost
	middleware[0].Name = "mutated"
	metadata["scope"] = "private"

	info := mustCompile(t, builder).Routes()[0]
	if info.Methods[0] != http.MethodGet || info.Middleware[0] != "audit" || info.Metadata["scope"] != "public" {
		t.Fatalf("mount option alias: %#v", info)
	}
}

func TestCustomErrorsOptionsAndRedirectPolicy(t *testing.T) {
	t.Parallel()

	notFound := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(498)
	})
	methodNotAllowed := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(497)
	})
	builder := router.New(
		router.WithNotFound(notFound),
		router.WithMethodNotAllowed(methodNotAllowed),
		router.WithAutomaticOPTIONS(false),
		router.WithRedirectPolicy(router.RejectRedirects),
	)
	mustRegister(t, builder, router.Route{
		Methods: []string{"GET"}, Path: "/tree/",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	compiled := mustCompile(t, builder)

	tests := []struct {
		method string
		path   string
		status int
		allow  string
	}{
		{method: "GET", path: "/missing", status: 498},
		{method: "POST", path: "/tree/item", status: 497, allow: "GET, HEAD"},
		{method: "OPTIONS", path: "/tree/item", status: 497, allow: "GET, HEAD"},
		{method: "GET", path: "/tree", status: 498},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		compiled.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status || response.Header().Get("Allow") != test.allow {
			t.Fatalf("%s %s: status=%d allow=%q", test.method, test.path, response.Code, response.Header().Get("Allow"))
		}
	}
}

func TestMalformedRequestsBypassCustomHandlersAndRouteMiddleware(t *testing.T) {
	t.Parallel()

	notFoundCalled := false
	methodNotAllowedCalled := false
	builder := router.New(
		router.WithNotFound(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			notFoundCalled = true
		})),
		router.WithMethodNotAllowed(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			methodNotAllowedCalled = true
		})),
		router.WithMiddleware(router.NamedMiddleware{
			Name: "panic", Middleware: func(_ http.Handler) http.Handler {
				return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					panic("route middleware reached for malformed request")
				})
			},
		}),
	)
	mustRegister(t, builder, router.Route{
		Methods: []string{http.MethodGet}, Path: "/known", Handler: http.NotFoundHandler(),
	})
	compiled := mustCompile(t, builder)

	requests := []*http.Request{
		{Method: "bad method", URL: &url.URL{Path: "/known"}},
		httptest.NewRequest(http.MethodGet, "/known", nil),
	}
	requests[1].Host = "example.com@evil.invalid"
	for _, request := range requests {
		ctx, cancel := context.WithCancel(request.Context())
		cancel()
		response := httptest.NewRecorder()
		compiled.ServeHTTP(response, request.WithContext(ctx))
		if response.Code != http.StatusBadRequest || notFoundCalled || methodNotAllowedCalled {
			t.Fatalf("malformed request: status=%d not-found=%t method-not-allowed=%t",
				response.Code, notFoundCalled, methodNotAllowedCalled)
		}
	}
}
