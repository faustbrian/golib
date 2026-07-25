// Package routertest provides small consumer-facing helpers for compiled route
// tests without introducing a parallel runtime API.
package routertest

import (
	"context"
	"net/http"
	"net/http/httptest"

	router "github.com/faustbrian/golib/pkg/router"
)

// TestingT is the subset of testing.TB used by the helpers.
type TestingT interface {
	Helper()
	Fatalf(format string, arguments ...any)
}

// MustCompile compiles builder or fails test.
func MustCompile(testingT TestingT, builder *router.Builder) *router.Router {
	testingT.Helper()
	compiled, err := builder.Compile()
	if err != nil {
		testingT.Fatalf("compile router: %v", err)
		return nil
	}
	return compiled
}

// Serve sends a bodyless request to handler and returns its recorder.
func Serve(testingT TestingT, handler http.Handler, method, target string) *httptest.ResponseRecorder {
	testingT.Helper()
	request, err := http.NewRequestWithContext(context.Background(), method, target, nil)
	if err != nil {
		testingT.Fatalf("create request: %v", err)
		return nil
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// AssertStatus fails test when response does not contain expected.
func AssertStatus(testingT TestingT, response *httptest.ResponseRecorder, expected int) {
	testingT.Helper()
	if response == nil || response.Code != expected {
		actual := 0
		if response != nil {
			actual = response.Code
		}
		testingT.Fatalf("response status: got %d, want %d", actual, expected)
	}
}

// RouteTable returns a copied compiled route table or fails for a nil router.
func RouteTable(testingT TestingT, compiled *router.Router) []router.RouteInfo {
	testingT.Helper()
	if compiled == nil {
		testingT.Fatalf("route table: nil compiled router")
		return nil
	}
	return compiled.Routes()
}
