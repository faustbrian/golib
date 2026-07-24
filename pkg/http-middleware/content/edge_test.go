package content

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyAndMediaTypeValidation(t *testing.T) {
	t.Parallel()
	for _, policy := range []Policy{
		{MaxValues: -1}, {MaxValues: 257}, {MaxHeaderBytes: -1},
		{MaxHeaderBytes: 1<<20 + 1}, {RequestTypes: []string{"invalid"}},
		{ResponseTypes: []string{"invalid"}},
		{MaxValues: 1, RequestTypes: []string{"text/plain", "application/json"}},
		{MaxHeaderBytes: 8, ResponseTypes: []string{"application/json"}},
		{RequestTypes: []string{"*/json"}},
	} {
		_, err := New(policy)
		var configuration *ConfigError
		if !errors.As(err, &configuration) || !errors.Is(err, ErrInvalidPolicy) || configuration.Error() == "" {
			t.Fatalf("New(%+v) error = %v", policy, err)
		}
	}
	if types, err := normalizeTypes([]string{"Application/JSON; charset=utf-8"}); err != nil || types[0] != "application/json" {
		t.Fatalf("normalizeTypes = %v, %v", types, err)
	}
	if validMediaRange("invalid", true) || validMediaRange("application/json;charset=utf-8", true) {
		t.Fatal("invalid media range accepted")
	}
}

func TestNegotiationParserBoundaries(t *testing.T) {
	t.Parallel()
	supported := []string{"application/json"}
	if !acceptable(nil, supported, 2, 32) {
		t.Fatal("missing Accept must allow")
	}
	for _, lines := range [][]string{{strings.Repeat("x", 33)}, {"text/plain", "text/html", "image/png"}, {"application/json;broken"}, {"application/json;bad key=value"}, {"application/json;q=1;q=1"}, {"application/json;q=nope"}, {"application/json;"}} {
		if acceptable(lines, supported, 2, 32) {
			t.Fatalf("acceptable(%q) = true", lines)
		}
	}
	for _, lines := range [][]string{
		{"application/json, invalid;bad key=value"},
		{"application/json", strings.Repeat("x", 129)},
	} {
		if acceptable(lines, supported, 3, 128) {
			t.Fatalf("acceptable(%q) ignored malformed tail", lines)
		}
	}
	if !acceptable([]string{"text/plain;q=0, application/json; charset=utf-8"}, supported, 3, 128) {
		t.Fatal("supported parameterized type rejected")
	}
	if acceptable([]string{"*/json"}, supported, 1, 128) {
		t.Fatal("invalid wildcard media range accepted")
	}
	if !matchesAny("application/*", supported) || !matchesAny("*/*", supported) || matchesAny("text/plain", supported) {
		t.Fatal("wildcard matching failed")
	}
}

func TestContentTypePassAndMalformedBody(t *testing.T) {
	t.Parallel()
	middleware, err := New(Policy{RequestTypes: []string{"application/json"}, ResponseTypes: []string{"application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		contentType, accept string
		body                bool
		want                int
	}{
		{"application/json; charset=utf-8", "application/json", true, http.StatusNoContent},
		{"bad;=", "application/json", true, http.StatusUnsupportedMediaType},
		{"application/*", "application/json", true, http.StatusUnsupportedMediaType},
		{"", "application/json", false, http.StatusNoContent},
	} {
		var request *http.Request
		if tc.body {
			request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
		} else {
			request = httptest.NewRequest(http.MethodGet, "/", nil)
		}
		request.Header.Set("Content-Type", tc.contentType)
		request.Header.Set("Accept", tc.accept)
		recorder := httptest.NewRecorder()
		middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(recorder, request)
		if recorder.Code != tc.want {
			t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
		}
	}
}

func TestDuplicateContentTypeIsRejected(t *testing.T) {
	t.Parallel()
	middleware, err := New(Policy{RequestTypes: []string{"application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	request.Header["Content-Type"] = []string{"application/json", "application/json"}
	recorder := httptest.NewRecorder()
	middleware(http.NotFoundHandler()).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d", recorder.Code)
	}
}
