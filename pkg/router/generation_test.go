package router_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	router "github.com/faustbrian/golib/pkg/router"
)

func TestNamedPathGenerationEscapesSegmentsAndRoundTrips(t *testing.T) {
	t.Parallel()

	builder := router.New()
	mustRegister(t, builder, router.Route{
		Name: "files.show", Methods: []string{"GET"}, Path: "/files/{name}",
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("X-Name", request.PathValue("name"))
		}),
	})
	compiled := mustCompile(t, builder)

	path, err := compiled.Path("files.show", router.Param("name", "a/b c"))
	if err != nil {
		t.Fatalf("generate path: %v", err)
	}
	if path != "/files/a%2Fb%20c" {
		t.Fatalf("path: got %q", path)
	}
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK || response.Header().Get("X-Name") != "a/b c" {
		t.Fatalf("round trip: status=%d value=%q", response.Code, response.Header().Get("X-Name"))
	}
}

func TestGenerationSupportsEveryServeMuxWildcardIdentifier(t *testing.T) {
	t.Parallel()

	builder := router.New()
	mustRegister(t, builder, router.Route{
		Name: "identifier", Methods: []string{http.MethodGet},
		Path: "/{_id}/{väärde}",
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("X-ID", request.PathValue("_id"))
			writer.Header().Set("X-Value", request.PathValue("väärde"))
		}),
	})
	compiled := mustCompile(t, builder)
	path, err := compiled.Path("identifier",
		router.Param("_id", "first"),
		router.Param("väärde", "second"),
	)
	if err != nil {
		t.Fatalf("generate path: %v", err)
	}
	response := httptest.NewRecorder()
	compiled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK || response.Header().Get("X-ID") != "first" ||
		response.Header().Get("X-Value") != "second" {
		t.Fatalf("round trip: status=%d id=%q value=%q", response.Code,
			response.Header().Get("X-ID"), response.Header().Get("X-Value"))
	}
}

func TestRemainderGenerationRequiresExplicitSafeSegments(t *testing.T) {
	t.Parallel()

	builder := router.New()
	mustRegister(t, builder, router.Route{
		Name: "assets", Methods: []string{"GET"}, Path: "/assets/{path...}",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	compiled := mustCompile(t, builder)

	path, err := compiled.Path("assets", router.Remainder("path", "images", "a/b.png"))
	if err != nil {
		t.Fatalf("generate remainder: %v", err)
	}
	if path != "/assets/images/a%2Fb.png" {
		t.Fatalf("path: got %q", path)
	}
	for _, parameter := range []router.URLParameter{
		router.Param("path", "images/a.png"),
		router.Remainder("path", "..", "secret"),
		router.Remainder("path"),
	} {
		if _, err := compiled.Path("assets", parameter); !errors.Is(err, router.ErrInvalidParameter) {
			t.Fatalf("unsafe remainder error: got %v", err)
		}
	}
}

func TestGenerationRejectsParameterSetErrors(t *testing.T) {
	t.Parallel()

	builder := router.New()
	mustRegister(t, builder, router.Route{
		Name: "users.show", Methods: []string{"GET"}, Path: "/users/{id}",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	compiled := mustCompile(t, builder)

	tests := []struct {
		name       string
		route      string
		parameters []router.URLParameter
		kind       error
	}{
		{name: "unknown route", route: "missing", kind: router.ErrGeneration},
		{name: "missing", route: "users.show", kind: router.ErrInvalidParameter},
		{name: "unknown parameter", route: "users.show", parameters: []router.URLParameter{router.Param("id", "1"), router.Param("extra", "2")}, kind: router.ErrInvalidParameter},
		{name: "duplicate", route: "users.show", parameters: []router.URLParameter{router.Param("id", "1"), router.Param("id", "2")}, kind: router.ErrInvalidParameter},
		{name: "empty parameter name", route: "users.show", parameters: []router.URLParameter{router.Param("", "1")}, kind: router.ErrInvalidParameter},
		{name: "invalid parameter name", route: "users.show", parameters: []router.URLParameter{router.Param("1id", "1")}, kind: router.ErrInvalidParameter},
		{name: "dot segment", route: "users.show", parameters: []router.URLParameter{router.Param("id", "..")}, kind: router.ErrInvalidParameter},
		{name: "slash-only segment", route: "users.show", parameters: []router.URLParameter{router.Param("id", "/")}, kind: router.ErrInvalidParameter},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := compiled.Path(test.route, test.parameters...)
			if !errors.Is(err, test.kind) {
				t.Fatalf("error: got %v", err)
			}
		})
	}
}

func TestAbsoluteURLGenerationValidatesBaseHostAndQuery(t *testing.T) {
	t.Parallel()

	builder := router.New()
	mustRegister(t, builder, router.Route{
		Name: "tenant.user", Methods: []string{"GET"},
		Host: "{tenant}.example.com", Path: "/users/{id}",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	compiled := mustCompile(t, builder)
	base, err := router.NewBaseURL("https", "ignored.example.com:8443")
	if err != nil {
		t.Fatalf("base URL: %v", err)
	}
	query := url.Values{"tag": {"z", "a"}, "q": {"hello world"}}
	generated, err := compiled.URL(
		"tenant.user", base, query,
		router.Param("tenant", "acme"), router.Param("id", "a/b"),
	)
	if err != nil {
		t.Fatalf("generate URL: %v", err)
	}
	if generated != "https://acme.example.com:8443/users/a%2Fb?q=hello+world&tag=z&tag=a" {
		t.Fatalf("URL: got %q", generated)
	}

	invalid := [][2]string{
		{"ftp", "example.com"},
		{"https", "user@example.com"},
		{"https", "example.com/path"},
		{"https", "example.com\r\nX-Evil: yes"},
	}
	for _, input := range invalid {
		if _, err := router.NewBaseURL(input[0], input[1]); !errors.Is(err, router.ErrGeneration) {
			t.Fatalf("base %q %q: got %v", input[0], input[1], err)
		}
	}
	if _, err := compiled.URL("tenant.user", base, nil, router.Param("tenant", "a.b"), router.Param("id", "1")); !errors.Is(err, router.ErrInvalidParameter) {
		t.Fatalf("host injection: got %v", err)
	}
}

func TestGenerationEnforcesOutputAndQueryLimits(t *testing.T) {
	t.Parallel()

	limits := router.DefaultLimits()
	limits.MaxGeneratedURLBytes = 24
	limits.MaxQueryValues = 1
	builder := router.New(router.WithLimits(limits))
	mustRegister(t, builder, router.Route{
		Name: "x", Methods: []string{"GET"}, Path: "/{value}",
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	compiled := mustCompile(t, builder)
	if _, err := compiled.Path("x", router.Param("value", "a very long route value")); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("output limit: got %v", err)
	}
	base, err := router.NewBaseURL("https", "example.com")
	if err != nil {
		t.Fatalf("base: %v", err)
	}
	if _, err := compiled.URL("x", base, url.Values{"a": {"1", "2"}}, router.Param("value", "v")); !errors.Is(err, router.ErrLimitExceeded) {
		t.Fatalf("query limit: got %v", err)
	}
}
