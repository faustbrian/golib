package cors

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompileValidationAndCanonicalOrigins(t *testing.T) {
	t.Parallel()
	tooMany := make([]string, 2)
	for _, policy := range []Policy{
		{MaxHeaderValues: -1}, {MaxHeaderValues: 257}, {MaxAgeSeconds: -1}, {MaxAgeSeconds: 86401},
		{MaxHeaderBytes: -1}, {MaxHeaderBytes: 1<<20 + 1}, {MaxHeaderValues: 1, AllowedOrigins: tooMany},
		{AllowedOrigins: []string{"bad"}}, {AllowedOrigins: []string{"*"}, AllowCredentials: true},
		{AllowedMethods: []string{"bad method"}}, {AllowedHeaders: []string{"bad header"}}, {ExposedHeaders: []string{"bad header"}},
		{AllowedMethods: []string{"*"}, AllowCredentials: true}, {AllowedHeaders: []string{"*"}, AllowCredentials: true},
		{ExposedHeaders: []string{"*"}, AllowCredentials: true},
	} {
		_, err := New(policy)
		var configuration *ConfigError
		if !errors.As(err, &configuration) || !errors.Is(err, ErrInvalidPolicy) || configuration.Error() == "" {
			t.Fatalf("New(%+v) error = %v", policy, err)
		}
	}
	for _, tc := range []struct {
		raw, want string
		ok        bool
	}{
		{"null", "null", true}, {"HTTPS://BÜCHER.example:443", "https://xn--bcher-kva.example", true},
		{"http://[2001:db8::1]:80", "http://[2001:db8::1]", true}, {"https://example.com:8443", "https://example.com:8443", true},
		{"", "", false}, {" https://example.com", "", false}, {strings.Repeat("x", 2049), "", false},
		{"ftp://example.com", "", false}, {"https://user@example.com", "", false}, {"https://example.com/path", "", false},
		{"https://example.com?q=1", "", false}, {"https://example.com#x", "", false}, {"https://", "", false}, {"https://%zz", "", false}, {"https://\u200d.example", "", false},
		{"https://example.com:", "", false}, {"https://example.com:65536", "", false},
	} {
		got, ok := canonicalOrigin(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("canonicalOrigin(%q) = %q, %v", tc.raw, got, ok)
		}
	}
	for _, value := range []string{"", strings.Repeat("a", 129), "bad token"} {
		if validToken(value) {
			t.Fatalf("validToken(%q) = true", value)
		}
	}
	if !validToken("X-Good_1") || !contains([]string{"a", "b"}, "b") || contains([]string{"a"}, "b") {
		t.Fatal("token or contains mismatch")
	}
}

func TestRequestRoutingAndDynamicOriginPaths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		policy      Policy
		method      string
		headers     http.Header
		want        int
		allowOrigin string
	}{
		{"no origin", Policy{}, http.MethodGet, nil, http.StatusAccepted, ""},
		{"oversized simple passes", Policy{MaxHeaderBytes: 1}, http.MethodGet, http.Header{"Origin": {"https://example.com"}}, http.StatusAccepted, ""},
		{"oversized preflight rejects", Policy{MaxHeaderBytes: 1}, http.MethodOptions, http.Header{"Origin": {"x"}, "Access-Control-Request-Method": {"GET"}}, http.StatusBadRequest, ""},
		{"multiple origin passes", Policy{AllowedOrigins: []string{"https://example.com"}}, http.MethodGet, http.Header{"Origin": {"https://example.com", "https://evil.test"}}, http.StatusAccepted, ""},
		{"invalid origin passes", Policy{AllowedOrigins: []string{"https://example.com"}}, http.MethodGet, http.Header{"Origin": {"bad"}}, http.StatusAccepted, ""},
		{"denied preflight", Policy{AllowedOrigins: []string{"https://example.com"}}, http.MethodOptions, http.Header{"Origin": {"https://evil.test"}, "Access-Control-Request-Method": {"GET"}}, http.StatusForbidden, ""},
		{"wildcard", Policy{AllowedOrigins: []string{"*"}}, http.MethodGet, http.Header{"Origin": {"https://example.com"}}, http.StatusAccepted, "*"},
		{"dynamic allow", Policy{AllowOrigin: func(context.Context, string) (bool, error) { return true, nil }}, http.MethodGet, http.Header{"Origin": {"https://example.com"}}, http.StatusAccepted, "https://example.com"},
		{"dynamic error", Policy{AllowOrigin: func(context.Context, string) (bool, error) { return false, errors.New("denied") }}, http.MethodGet, http.Header{"Origin": {"https://example.com"}}, http.StatusAccepted, ""},
	} {
		middleware, err := New(tc.policy)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		request := httptest.NewRequest(tc.method, "/", nil)
		request.Header = tc.headers.Clone()
		recorder := httptest.NewRecorder()
		middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })).ServeHTTP(recorder, request)
		if recorder.Code != tc.want || recorder.Header().Get("Access-Control-Allow-Origin") != tc.allowOrigin {
			t.Fatalf("%s response = %d %v", tc.name, recorder.Code, recorder.Header())
		}
	}
}

func TestPreflightTruthTable(t *testing.T) {
	t.Parallel()
	base := Policy{AllowedOrigins: []string{"https://example.com"}, AllowedMethods: []string{"POST"}, AllowedHeaders: []string{"X-One"}, ExposedHeaders: []string{"X-Result"}, AllowCredentials: true, MaxAgeSeconds: 30}
	for _, tc := range []struct {
		name   string
		mutate func(http.Header)
		policy func(*Policy)
		want   int
	}{
		{"valid", nil, nil, http.StatusNoContent},
		{"pass", nil, func(p *Policy) { p.PassPreflight = true }, http.StatusAccepted},
		{"missing method", func(h http.Header) { h.Del("Access-Control-Request-Method") }, nil, http.StatusAccepted},
		{"duplicate method", func(h http.Header) { h["Access-Control-Request-Method"] = []string{"POST", "POST"} }, nil, http.StatusBadRequest},
		{"empty method", func(h http.Header) { h.Set("Access-Control-Request-Method", " ") }, nil, http.StatusBadRequest},
		{"method denied", func(h http.Header) { h.Set("Access-Control-Request-Method", "DELETE") }, nil, http.StatusForbidden},
		{"malformed wildcard method", func(h http.Header) { h["Access-Control-Request-Method"] = []string{"GET\r\nX-Evil: yes"} }, func(p *Policy) { p.AllowedMethods = []string{"*"}; p.AllowCredentials = false }, http.StatusBadRequest},
		{"malformed headers", func(h http.Header) { h.Set("Access-Control-Request-Headers", "bad header") }, nil, http.StatusBadRequest},
		{"header denied", func(h http.Header) { h.Set("Access-Control-Request-Headers", "X-Two") }, nil, http.StatusForbidden},
		{"private denied", func(h http.Header) { h.Set("Access-Control-Request-Private-Network", "true") }, nil, http.StatusForbidden},
		{"private allowed", func(h http.Header) { h.Set("Access-Control-Request-Private-Network", "true") }, func(p *Policy) { p.AllowPrivateNetwork = true }, http.StatusNoContent},
		{"private malformed", func(h http.Header) { h.Set("Access-Control-Request-Private-Network", "false") }, nil, http.StatusBadRequest},
		{"private duplicate", func(h http.Header) { h["Access-Control-Request-Private-Network"] = []string{"true", "true"} }, nil, http.StatusBadRequest},
	} {
		policy := base
		if tc.policy != nil {
			tc.policy(&policy)
		}
		middleware, err := New(policy)
		if err != nil {
			t.Fatal(err)
		}
		header := http.Header{"Origin": {"https://example.com"}, "Access-Control-Request-Method": {"POST"}, "Access-Control-Request-Headers": {"X-One"}}
		if tc.mutate != nil {
			tc.mutate(header)
		}
		request := httptest.NewRequest(http.MethodOptions, "/", nil)
		request.Header = header
		recorder := httptest.NewRecorder()
		middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })).ServeHTTP(recorder, request)
		if recorder.Code != tc.want {
			t.Fatalf("%s status = %d, want %d; headers=%v", tc.name, recorder.Code, tc.want, recorder.Header())
		}
		if tc.name == "valid" && (recorder.Header().Get("Access-Control-Max-Age") != "30" || recorder.Header().Get("Access-Control-Allow-Credentials") != "true") {
			t.Fatalf("valid headers = %v", recorder.Header())
		}
	}
}

func TestSingularAndHeaderListBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		values         []string
		present, valid bool
	}{{nil, false, true}, {[]string{"a", "b"}, true, false}, {[]string{""}, true, false}, {[]string{"a,b"}, true, false}, {[]string{strings.Repeat("a", 129)}, true, false}, {[]string{" value "}, true, true}} {
		header := http.Header{}
		if tc.values != nil {
			header["X-Test"] = tc.values
		}
		_, present, valid := singular(header, "X-Test", 128)
		if present != tc.present || valid != tc.valid {
			t.Fatalf("singular(%q) = %v, %v", tc.values, present, valid)
		}
	}
	for _, values := range [][]string{{""}, {"bad header"}, {"X-One", "X-Two", "X-Three"}, {"X-One,X-Two,X-Three"}} {
		if got := splitHeaderList(values, 2, 32); got != nil {
			t.Fatalf("splitHeaderList(%q) = %v", values, got)
		}
	}
	if got := splitHeaderList([]string{"x-one, x-two"}, 2, 32); len(got) != 2 || got[0] != "X-One" {
		t.Fatalf("header list = %v", got)
	}
}

func TestServeVaryWildcardPassesDirectly(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	serveVary(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) }), recorder, httptest.NewRequest(http.MethodGet, "/", nil), true)
	if recorder.Code != http.StatusAccepted || recorder.Header().Get("Vary") != "" {
		t.Fatalf("response = %d %v", recorder.Code, recorder.Header())
	}
}
