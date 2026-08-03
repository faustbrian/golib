package cors

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExactCORSPolicyBoundsAreAccepted(t *testing.T) {
	t.Parallel()

	origins := make([]string, 256)
	methods := make([]string, 256)
	headers := make([]string, 256)
	exposed := make([]string, 256)
	for index := range origins {
		origins[index] = "https://example.com"
		methods[index] = http.MethodGet
		headers[index] = "X-Request"
		exposed[index] = "X-Response"
	}
	for name, policy := range map[string]Policy{
		"minimum values":       {MaxHeaderValues: 1},
		"maximum values":       {MaxHeaderValues: 256, AllowedOrigins: origins, AllowedMethods: methods, AllowedHeaders: headers, ExposedHeaders: exposed},
		"maximum age":          {MaxAgeSeconds: 86400},
		"minimum header bytes": {MaxHeaderBytes: 1},
		"maximum header bytes": {MaxHeaderBytes: 1 << 20},
		"exact list lengths": {
			MaxHeaderValues: 1,
			AllowedOrigins:  []string{"https://example.com"}, AllowedMethods: []string{http.MethodGet},
			AllowedHeaders: []string{"X-Request"}, ExposedHeaders: []string{"X-Response"},
		},
	} {
		if _, err := New(policy); err != nil {
			t.Fatalf("New(%s exact bound) error = %v", name, err)
		}
	}
}

func TestEachCORSListEnforcesItsOwnValueBudget(t *testing.T) {
	t.Parallel()

	for name, policy := range map[string]Policy{
		"origins": {MaxHeaderValues: 1, AllowedOrigins: []string{"https://a.example", "https://b.example"}},
		"methods": {MaxHeaderValues: 1, AllowedMethods: []string{http.MethodGet, http.MethodPost}},
		"headers": {MaxHeaderValues: 1, AllowedHeaders: []string{"X-One", "X-Two"}},
		"exposed": {MaxHeaderValues: 1, ExposedHeaders: []string{"X-One", "X-Two"}},
	} {
		if _, err := New(policy); err == nil {
			t.Fatalf("New(%s over limit) succeeded", name)
		}
	}
}

func TestWildcardDoesNotHideLaterInvalidOrigin(t *testing.T) {
	t.Parallel()

	if _, err := New(Policy{AllowedOrigins: []string{"*", "invalid"}}); err == nil {
		t.Fatal("invalid origin after wildcard was ignored")
	}
}

func TestCORSHeaderBudgetUsesExactAggregateBytes(t *testing.T) {
	t.Parallel()

	origin := "https://a.example"
	middleware, err := New(Policy{AllowedOrigins: []string{origin}, MaxHeaderBytes: len(origin)})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", origin)
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || recorder.Header().Get("Access-Control-Allow-Origin") != origin {
		t.Fatalf("response = %d %v", recorder.Code, recorder.Header())
	}

	header := http.Header{
		"Origin":                                 {"ab", "c"},
		"Access-Control-Request-Method":          {"POST"},
		"Access-Control-Request-Headers":         {"X-One"},
		"Access-Control-Request-Private-Network": {"true"},
	}
	if got, want := corsHeaderBytes(header), 2+1+4+5+4; got != want {
		t.Fatalf("corsHeaderBytes() = %d, want %d", got, want)
	}
}

func TestSingleOriginFitsExactHeaderValueBudget(t *testing.T) {
	t.Parallel()

	origin := "https://a.example"
	middleware, err := New(Policy{AllowedOrigins: []string{origin}, MaxHeaderValues: 1})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", origin)
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)
	if recorder.Header().Get("Access-Control-Allow-Origin") != origin {
		t.Fatalf("headers = %v", recorder.Header())
	}
}

func TestOversizedNonPreflightRequestsPassThrough(t *testing.T) {
	t.Parallel()

	for name, request := range map[string]*http.Request{
		"options without method": httptest.NewRequest(http.MethodOptions, "/", nil),
		"get with method":        httptest.NewRequest(http.MethodGet, "/", nil),
	} {
		if name == "options without method" {
			request.Header.Set("Origin", "xx")
		} else {
			request.Header.Set("Access-Control-Request-Method", http.MethodGet)
		}
		middleware, _ := New(Policy{MaxHeaderBytes: 1})
		recorder := httptest.NewRecorder()
		middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		})).ServeHTTP(recorder, request)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf("%s status = %d", name, recorder.Code)
		}
	}
}

func TestDynamicOriginRequiresAcceptanceWithoutError(t *testing.T) {
	t.Parallel()

	for name, callback := range map[string]func(context.Context, string) (bool, error){
		"accepted with error":  func(context.Context, string) (bool, error) { return true, errors.New("failed") },
		"denied without error": func(context.Context, string) (bool, error) { return false, nil },
	} {
		middleware, _ := New(Policy{AllowOrigin: callback})
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Origin", "https://example.com")
		recorder := httptest.NewRecorder()
		middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent || recorder.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%s response = %d %v", name, recorder.Code, recorder.Header())
		}
	}
}

func TestAbsentOptionalCORSHeadersRemainAbsent(t *testing.T) {
	t.Parallel()

	middleware, _ := New(Policy{AllowedOrigins: []string{"https://example.com"}})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)
	if _, exists := recorder.Header()["Access-Control-Expose-Headers"]; exists {
		t.Fatalf("empty exposed header exists: %v", recorder.Header())
	}

	preflight, _ := New(Policy{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{http.MethodPost},
	})
	request = httptest.NewRequest(http.MethodOptions, "/", nil)
	request.Header.Set("Origin", "https://example.com")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	recorder = httptest.NewRecorder()
	preflight(http.NotFoundHandler()).ServeHTTP(recorder, request)
	for _, name := range []string{"Access-Control-Allow-Headers", "Access-Control-Max-Age"} {
		if _, exists := recorder.Header()[name]; exists {
			t.Fatalf("optional header %s exists: %v", name, recorder.Header())
		}
	}
}

func TestPreflightRejectsMissingMethodAtItsOwnBoundary(t *testing.T) {
	t.Parallel()

	configuration, err := compile(Policy{AllowedMethods: []string{http.MethodPost}})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if configuration.preflight(recorder, httptest.NewRequest(http.MethodOptions, "/", nil)) {
		t.Fatal("preflight without requested method succeeded")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestHeaderParsingExactBounds(t *testing.T) {
	t.Parallel()

	exactToken := strings.Repeat("a", 128)
	header := http.Header{"X-Test": {exactToken}}
	value, present, valid := singular(header, "X-Test", 128)
	if value != exactToken || !present || !valid {
		t.Fatalf("singular exact bound = %q, %v, %v", value, present, valid)
	}
	if !validToken(exactToken) {
		t.Fatal("128-byte token was rejected")
	}
	if got := splitHeaderList([]string{"X-One", "X-Two"}, 2, 10); len(got) != 2 {
		t.Fatalf("exact header list = %v", got)
	}
	if got := splitHeaderList([]string{"X-One", "X-Two"}, 2, 9); got != nil {
		t.Fatalf("over-budget header list = %v", got)
	}
}

func TestCanonicalOriginLengthAndDefaultPortBounds(t *testing.T) {
	t.Parallel()

	host := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." +
		strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	exact := "https://" + host + ":65535"
	if len(exact) != 267 {
		t.Fatalf("origin fixture length = %d, want 267", len(exact))
	}
	if got, ok := canonicalOrigin(exact); !ok || got != exact {
		t.Fatalf("canonicalOrigin(exact) = %q, %v", got, ok)
	}
	if _, ok := canonicalOrigin(exact + "x"); ok {
		t.Fatal("overlong origin was accepted")
	}
	unicodeHost := strings.TrimSuffix(strings.Repeat(strings.Repeat("ü", 30)+".", 5), ".")
	unicodeOrigin := "https://" + unicodeHost
	if len(unicodeOrigin) <= 267 {
		t.Fatalf("Unicode origin fixture length = %d", len(unicodeOrigin))
	}
	if got, ok := canonicalOrigin(unicodeOrigin); !ok || !strings.HasPrefix(got, "https://xn--") {
		t.Fatalf("canonicalOrigin(long Unicode origin) = %q, %v", got, ok)
	}
	if _, ok := canonicalOrigin(strings.Repeat("a", maximumRawOriginBytes+1)); ok {
		t.Fatal("origin above the raw input bound was accepted")
	}
	for raw := range map[string]struct{}{
		"http://example.com:443": {},
		"https://example.com:80": {},
	} {
		if got, ok := canonicalOrigin(raw); !ok || got != raw {
			t.Fatalf("canonicalOrigin(%q) = %q, %v", raw, got, ok)
		}
	}
}
