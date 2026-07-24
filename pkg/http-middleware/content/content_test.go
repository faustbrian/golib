package content_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/content"
)

func TestUnsupportedRequestMediaTypeReturns415(t *testing.T) {
	t.Parallel()

	middleware, _ := content.New(content.Policy{RequestTypes: []string{"application/json"}})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("payload"))
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler ran") })).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestMissingContentTypeIsAllowedOnlyForEmptyBodies(t *testing.T) {
	t.Parallel()

	middleware, _ := content.New(content.Policy{RequestTypes: []string{"application/json"}})
	for _, tc := range []struct {
		name   string
		body   string
		status int
	}{
		{name: "empty", status: http.StatusNoContent},
		{name: "non-empty", body: "{}", status: http.StatusUnsupportedMediaType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(recorder, req)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.status)
			}
		})
	}
}

func TestUnacceptableResponseTypeReturns406(t *testing.T) {
	t.Parallel()

	middleware, _ := content.New(content.Policy{ResponseTypes: []string{"application/json"}})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/plain;q=1, application/json;q=0")
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler ran") })).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotAcceptable {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestAcceptWildcardsAndWeightedDuplicatesAreParsed(t *testing.T) {
	t.Parallel()

	middleware, _ := content.New(content.Policy{ResponseTypes: []string{"application/json"}})
	for _, value := range []string{"*/*", "text/plain;q=1, application/*;q=0.5", "application/json;q=0, application/json;q=1"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", value)
		recorder := httptest.NewRecorder()
		middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(recorder, req)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("Accept %q status = %d", value, recorder.Code)
		}
	}
}

func TestMalformedAcceptQualityFailsClosed(t *testing.T) {
	t.Parallel()

	middleware, _ := content.New(content.Policy{ResponseTypes: []string{"application/json"}})
	for _, value := range []string{"application/json;q=.5", "application/json;q=1.001", "application/json;q=1;q=0"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", value)
		recorder := httptest.NewRecorder()
		middleware(http.NotFoundHandler()).ServeHTTP(recorder, req)
		if recorder.Code != http.StatusNotAcceptable {
			t.Fatalf("Accept %q status = %d", value, recorder.Code)
		}
	}
}
