package router_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	router "github.com/faustbrian/golib/pkg/router"
)

func TestSupportedMatchingIsDifferentialWithServeMux(t *testing.T) {
	t.Parallel()

	type fixture struct {
		pattern string
		path    string
	}
	fixtures := []fixture{
		{pattern: "/", path: "/"},
		{pattern: "/users/{id}", path: "/users/{id}"},
		{pattern: "/assets/{rest...}", path: "/assets/{rest...}"},
		{pattern: "/exact/{$}", path: "/exact/{$}"},
		{pattern: "/tree/", path: "/tree/"},
	}
	standard := http.NewServeMux()
	builder := router.New()
	for index, fixture := range fixtures {
		name := fmt.Sprintf("route-%d", index)
		handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("X-Route", name)
			writer.Header().Set("X-ID", request.PathValue("id"))
			writer.Header().Set("X-Rest", request.PathValue("rest"))
			writer.WriteHeader(http.StatusNoContent)
		})
		standard.Handle("GET "+fixture.pattern, handler)
		mustRegister(t, builder, router.Route{Methods: []string{"GET"}, Path: fixture.path, Handler: handler})
	}
	compiled := mustCompile(t, builder)

	targets := []string{
		"/", "/users/42", "/users/a%2Fb", "/assets/css/app.css",
		"/assets/a%2Fb", "/exact/", "/exact/more", "/tree", "/tree/leaf",
		"/a//b", "/a/../b", "/a/../users/42?source=redirect",
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, target := range targets {
			t.Run(method+" "+target, func(t *testing.T) {
				standardResponse := httptest.NewRecorder()
				standard.ServeHTTP(
					standardResponse,
					httptest.NewRequestWithContext(context.Background(), method, target, nil),
				)
				routerResponse := httptest.NewRecorder()
				compiled.ServeHTTP(routerResponse, httptest.NewRequest(method, target, nil))

				for _, header := range []string{"Location", "X-Route", "X-ID", "X-Rest"} {
					if routerResponse.Header().Get(header) != standardResponse.Header().Get(header) {
						t.Fatalf("%s: router=%q standard=%q", header, routerResponse.Header().Get(header), standardResponse.Header().Get(header))
					}
				}
				if routerResponse.Code != standardResponse.Code {
					t.Fatalf("status: router=%d standard=%d", routerResponse.Code, standardResponse.Code)
				}
			})
		}
	}
}

func TestSupportedMethodsAndLiteralHostsAreDifferentialWithServeMux(t *testing.T) {
	t.Parallel()

	fixtures := []struct {
		method string
		host   string
		path   string
	}{
		{method: http.MethodDelete, path: "/delete/{id}"},
		{method: http.MethodGet, path: "/get/{id}"},
		{method: http.MethodHead, path: "/head/{id}"},
		{method: http.MethodPost, host: "api.example.com", path: "/items/{id}"},
		{method: http.MethodPut, path: "/put/{id}"},
		{method: http.MethodPatch, path: "/records/{id}"},
		{method: http.MethodOptions, host: "api.example.com", path: "/explicit"},
		{method: http.MethodTrace, path: "/trace/{id}"},
		{method: "BREW", path: "/extension/{id}"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.method+" "+fixture.host+fixture.path, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("X-Matched", request.PathValue("id"))
				writer.WriteHeader(http.StatusNoContent)
			})
			standard := http.NewServeMux()
			standard.Handle(fixture.method+" "+fixture.host+fixture.path, handler)
			builder := router.New()
			mustRegister(t, builder, router.Route{
				Methods: []string{fixture.method}, Host: fixture.host,
				Path: fixture.path, Handler: handler,
			})
			compiled := mustCompile(t, builder)

			host := fixture.host
			if host == "" {
				host = "fallback.example.com"
			}
			for _, requestCase := range []struct {
				host string
				path string
			}{
				{host: host + ":8443", path: strings.Replace(fixture.path, "{id}", "a%2Fb", 1)},
				{host: "other.example.com", path: "/missing"},
			} {
				standardRequest := httptest.NewRequest(fixture.method, requestCase.path, nil)
				standardRequest.Host = requestCase.host
				standardResponse := httptest.NewRecorder()
				standard.ServeHTTP(standardResponse, standardRequest)

				routerRequest := httptest.NewRequest(fixture.method, requestCase.path, nil)
				routerRequest.Host = requestCase.host
				routerResponse := httptest.NewRecorder()
				compiled.ServeHTTP(routerResponse, routerRequest)

				if routerResponse.Code != standardResponse.Code ||
					routerResponse.Header().Get("Location") != standardResponse.Header().Get("Location") ||
					routerResponse.Header().Get("X-Matched") != standardResponse.Header().Get("X-Matched") {
					t.Fatalf("%s%s: router=%d/%q/%q standard=%d/%q/%q",
						requestCase.host, requestCase.path,
						routerResponse.Code, routerResponse.Header().Get("Location"), routerResponse.Header().Get("X-Matched"),
						standardResponse.Code, standardResponse.Header().Get("Location"), standardResponse.Header().Get("X-Matched"))
				}
			}
		})
	}
}

func TestLiteralHostMethodFallbackIsDifferentialWithServeMux(t *testing.T) {
	t.Parallel()

	standard := http.NewServeMux()
	builder := router.New(router.WithAutomaticOPTIONS(false))
	for _, fixture := range []struct {
		method string
		host   string
	}{
		{method: http.MethodGet, host: "api.example.com"},
		{method: http.MethodPost},
	} {
		handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("X-Method", fixture.method)
			writer.WriteHeader(http.StatusNoContent)
		})
		standard.Handle(fixture.method+" "+fixture.host+"/shared", handler)
		mustRegister(t, builder, router.Route{
			Methods: []string{fixture.method}, Host: fixture.host,
			Path: "/shared", Handler: handler,
		})
	}
	compiled := mustCompile(t, builder)

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut} {
		standardRequest := httptest.NewRequest(method, "/shared", nil)
		standardRequest.Host = "api.example.com:8443"
		standardResponse := httptest.NewRecorder()
		standard.ServeHTTP(standardResponse, standardRequest)
		routerRequest := httptest.NewRequest(method, "/shared", nil)
		routerRequest.Host = "api.example.com:8443"
		routerResponse := httptest.NewRecorder()
		compiled.ServeHTTP(routerResponse, routerRequest)

		if routerResponse.Code != standardResponse.Code ||
			routerResponse.Header().Get("Allow") != standardResponse.Header().Get("Allow") ||
			routerResponse.Header().Get("X-Method") != standardResponse.Header().Get("X-Method") {
			t.Fatalf("%s: router=%d/%q/%q standard=%d/%q/%q", method,
				routerResponse.Code, routerResponse.Header().Get("Allow"), routerResponse.Header().Get("X-Method"),
				standardResponse.Code, standardResponse.Header().Get("Allow"), standardResponse.Header().Get("X-Method"))
		}
	}
}

func TestDefaultNotFoundAndMethodNotAllowedMatchServeMux(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	standard := http.NewServeMux()
	standard.Handle("GET /known", handler)
	builder := router.New(router.WithAutomaticOPTIONS(false))
	mustRegister(t, builder, router.Route{
		Methods: []string{http.MethodGet}, Path: "/known", Handler: handler,
	})
	compiled := mustCompile(t, builder)

	for _, requestCase := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/missing"},
		{method: http.MethodPost, path: "/known"},
	} {
		standardResponse := httptest.NewRecorder()
		standard.ServeHTTP(standardResponse, httptest.NewRequest(requestCase.method, requestCase.path, nil))
		routerResponse := httptest.NewRecorder()
		compiled.ServeHTTP(routerResponse, httptest.NewRequest(requestCase.method, requestCase.path, nil))

		if routerResponse.Code != standardResponse.Code ||
			routerResponse.Header().Get("Allow") != standardResponse.Header().Get("Allow") ||
			routerResponse.Body.String() != standardResponse.Body.String() {
			t.Fatalf("%s %s: router=%d/%q/%q standard=%d/%q/%q",
				requestCase.method, requestCase.path,
				routerResponse.Code, routerResponse.Header().Get("Allow"), routerResponse.Body.String(),
				standardResponse.Code, standardResponse.Header().Get("Allow"), standardResponse.Body.String())
		}
	}
}

func TestCanonicalRedirectsPrecedeRouteAndMethodSelection(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	standard := http.NewServeMux()
	standard.Handle("GET /known", handler)
	builder := router.New()
	mustRegister(t, builder, router.Route{
		Methods: []string{http.MethodGet}, Path: "/known", Handler: handler,
	})
	compiled := mustCompile(t, builder)

	for _, requestCase := range []struct {
		method string
		target string
	}{
		{method: http.MethodGet, target: "/a/../missing?source=test"},
		{method: http.MethodPost, target: "/a/../known?source=test"},
		{method: "BREW", target: "/a//../missing?source=test"},
	} {
		standardResponse := httptest.NewRecorder()
		standard.ServeHTTP(standardResponse, httptest.NewRequest(requestCase.method, requestCase.target, nil))
		routerResponse := httptest.NewRecorder()
		compiled.ServeHTTP(routerResponse, httptest.NewRequest(requestCase.method, requestCase.target, nil))

		if routerResponse.Code != standardResponse.Code ||
			routerResponse.Header().Get("Location") != standardResponse.Header().Get("Location") ||
			routerResponse.Body.String() != standardResponse.Body.String() {
			t.Fatalf("%s %s: router=%d/%q/%q standard=%d/%q/%q",
				requestCase.method, requestCase.target,
				routerResponse.Code, routerResponse.Header().Get("Location"), routerResponse.Body.String(),
				standardResponse.Code, standardResponse.Header().Get("Location"), standardResponse.Body.String())
		}
	}
}

func TestDocumentedServeMuxDispatchDivergences(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	standard := http.NewServeMux()
	standard.Handle("GET /known", handler)
	builder := router.New()
	mustRegister(t, builder, router.Route{
		Methods: []string{http.MethodGet}, Path: "/known", Handler: handler,
	})
	compiled := mustCompile(t, builder)

	for _, requestCase := range []struct {
		method        string
		path          string
		standardCode  int
		standardAllow string
		routerCode    int
		routerAllow   string
	}{
		{http.MethodPost, "/known", 405, "GET, HEAD", 405, "GET, HEAD, OPTIONS"},
		{http.MethodOptions, "/known", 405, "GET, HEAD", 204, "GET, HEAD, OPTIONS"},
		{"BREW", "/missing", 404, "", 501, ""},
		{http.MethodOptions, "*", 400, "", 204, "GET, HEAD, OPTIONS"},
	} {
		standardResponse := httptest.NewRecorder()
		standard.ServeHTTP(standardResponse, httptest.NewRequest(requestCase.method, requestCase.path, nil))
		routerResponse := httptest.NewRecorder()
		compiled.ServeHTTP(routerResponse, httptest.NewRequest(requestCase.method, requestCase.path, nil))

		if standardResponse.Code != requestCase.standardCode ||
			standardResponse.Header().Get("Allow") != requestCase.standardAllow ||
			routerResponse.Code != requestCase.routerCode ||
			routerResponse.Header().Get("Allow") != requestCase.routerAllow {
			t.Fatalf("%s %s: router=%d/%q standard=%d/%q",
				requestCase.method, requestCase.path,
				routerResponse.Code, routerResponse.Header().Get("Allow"),
				standardResponse.Code, standardResponse.Header().Get("Allow"))
		}
	}
}

func TestHostSpecificityFallbackAndEquivalentPatterns(t *testing.T) {
	t.Parallel()

	builder := router.New()
	register := func(host, path, marker string) {
		mustRegister(t, builder, router.Route{
			Methods: []string{"GET"}, Host: host, Path: path,
			Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("X-Marker", marker)
				writer.Header().Set("X-Host-Value", request.PathValue("account"))
			}),
		})
	}
	register("", "/fallback", "fallback")
	register("{tenant}.example.com", "/tenant", "wildcard")
	register("api.example.com", "/tenant", "exact")
	register("{account}.example.com", "/account", "equivalent")
	compiled := mustCompile(t, builder)

	tests := []struct {
		host, path, marker, value string
	}{
		{host: "api.example.com:8443", path: "/tenant", marker: "exact"},
		{host: "ACME.EXAMPLE.COM", path: "/tenant", marker: "wildcard"},
		{host: "acme.example.com", path: "/account", marker: "equivalent", value: "acme"},
		{host: "api.example.com", path: "/fallback", marker: "fallback"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, "http://example.invalid"+test.path, nil)
		request.Host = test.host
		response := httptest.NewRecorder()
		compiled.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("X-Marker") != test.marker || response.Header().Get("X-Host-Value") != test.value {
			t.Fatalf("%s%s: status=%d marker=%q value=%q", test.host, test.path, response.Code, response.Header().Get("X-Marker"), response.Header().Get("X-Host-Value"))
		}
	}
}

func TestAmbiguousHostPatternsAndUnsafeAuthoritiesAreRejected(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	builder := router.New()
	mustRegister(t, builder, router.Route{Methods: []string{"GET"}, Host: "{left}.example.com", Path: "/x", Handler: handler})
	mustRegister(t, builder, router.Route{Methods: []string{"GET"}, Host: "api.{right}.com", Path: "/x", Handler: handler})
	if _, err := builder.Compile(); !errors.Is(err, router.ErrConflict) {
		t.Fatalf("ambiguous host error: got %v", err)
	}

	for _, host := range []string{"täst.example", "example.com:443", "bad_label.example", "-bad.example", "bad-.example", "[::1]"} {
		err := router.New().Register(router.Route{Methods: []string{"GET"}, Host: host, Path: "/", Handler: handler})
		if !errors.Is(err, router.ErrInvalidRoute) {
			t.Fatalf("unsafe host %q: got %v", host, err)
		}
	}
}

func TestAsteriskOptionsAndMalformedAuthority(t *testing.T) {
	t.Parallel()

	builder := router.New()
	mustRegister(t, builder, router.Route{Methods: []string{"GET", "POST"}, Path: "/", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})})
	compiled := mustCompile(t, builder)

	options := httptest.NewRequest(http.MethodOptions, "*", nil)
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, options)
	if response.Code != http.StatusNoContent || response.Header().Get("Allow") != "GET, HEAD, OPTIONS, POST" {
		t.Fatalf("OPTIONS *: status=%d allow=%q", response.Code, response.Header().Get("Allow"))
	}

	disabledBuilder := router.New(
		router.WithAutomaticOPTIONS(false),
		router.WithNotFound(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(498)
		})),
	)
	mustRegister(t, disabledBuilder, router.Route{
		Methods: []string{"GET"}, Path: "/", Handler: http.NotFoundHandler(),
	})
	response = httptest.NewRecorder()
	mustCompile(t, disabledBuilder).ServeHTTP(response, httptest.NewRequest(http.MethodOptions, "*", nil))
	if response.Code != 498 || response.Header().Get("Allow") != "" {
		t.Fatalf("disabled OPTIONS *: status=%d allow=%q", response.Code, response.Header().Get("Allow"))
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, authority := range []string{
		"example.com@evil.invalid",
		"example.com:bad",
		"[::1",
		"täst.example",
		strings.Repeat("a", 262),
	} {
		request.Host = authority
		response = httptest.NewRecorder()
		compiled.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("malformed authority %q status: got %d", authority, response.Code)
		}
	}
}

func TestRejectRedirectPolicyTreatsEncodedSeparatorsAsWildcardData(t *testing.T) {
	t.Parallel()

	builder := router.New(router.WithRedirectPolicy(router.RejectRedirects))
	mustRegister(t, builder, router.Route{
		Methods: []string{http.MethodGet}, Path: "/files/{value}",
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("X-Value", request.PathValue("value"))
			writer.WriteHeader(http.StatusNoContent)
		}),
	})
	compiled := mustCompile(t, builder)
	for _, testCase := range []struct {
		target string
		value  string
	}{
		{target: "/files/a%2F%2Fb", value: "a//b"},
		{target: "/files/a%2F..%2Fb", value: "a/../b"},
	} {
		response := httptest.NewRecorder()
		compiled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, testCase.target, nil))
		if response.Code != http.StatusNoContent || response.Header().Get("X-Value") != testCase.value {
			t.Fatalf("%s: status=%d value=%q", testCase.target, response.Code, response.Header().Get("X-Value"))
		}
		response = httptest.NewRecorder()
		compiled.ServeHTTP(response, httptest.NewRequest(http.MethodPost, testCase.target, nil))
		if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD, OPTIONS" {
			t.Fatalf("%s method miss: status=%d allow=%q",
				testCase.target, response.Code, response.Header().Get("Allow"))
		}
	}
}

func TestRejectRedirectPolicyRejectsSemanticSubtreeRoots(t *testing.T) {
	t.Parallel()

	builder := router.New(router.WithRedirectPolicy(router.RejectRedirects))
	for _, pattern := range []string{"/{value}/", "/exact/", "/exact/{$}", "/café/", "/encoded%2f/"} {
		mustRegister(t, builder, router.Route{
			Methods: []string{http.MethodGet}, Path: pattern, Handler: http.NotFoundHandler(),
		})
	}
	compiled := mustCompile(t, builder)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, target := range []string{"/one", "/exact", "/caf%C3%A9", "/encoded%2F"} {
			response := httptest.NewRecorder()
			compiled.ServeHTTP(response, httptest.NewRequest(method, target, nil))
			if response.Code != http.StatusNotFound || response.Header().Get("Location") != "" {
				t.Fatalf("%s %s: status=%d location=%q",
					method, target, response.Code, response.Header().Get("Location"))
			}
		}
	}
}
