package authhttp_test

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authhttp"
)

func TestBasicAuthorizationExtractionIsStrict(t *testing.T) {
	t.Parallel()

	extractor := mustExtractor(t, authhttp.BasicAuthorization())
	valid := base64.StdEncoding.EncodeToString([]byte("alice:password"))
	tests := []struct {
		name    string
		headers []string
		want    error
		valid   bool
	}{
		{name: "absent", want: authentication.ErrCredentialsAbsent},
		{name: "valid", headers: []string{"Basic " + valid}, valid: true},
		{name: "case insensitive scheme", headers: []string{"bAsIc " + valid}, valid: true},
		{name: "empty payload", headers: []string{"Basic "}, want: authentication.ErrCredentialsInvalid},
		{name: "extra whitespace", headers: []string{"Basic  " + valid}, want: authentication.ErrCredentialsInvalid},
		{name: "tab separator", headers: []string{"Basic\t" + valid}, want: authentication.ErrCredentialsInvalid},
		{name: "invalid base64", headers: []string{"Basic !!!"}, want: authentication.ErrCredentialsInvalid},
		{name: "noncanonical base64", headers: []string{"Basic YTpi=="}, want: authentication.ErrCredentialsInvalid},
		{name: "missing colon", headers: []string{"Basic " + base64.StdEncoding.EncodeToString([]byte("alice"))}, want: authentication.ErrCredentialsInvalid},
		{name: "empty username", headers: []string{"Basic " + base64.StdEncoding.EncodeToString([]byte(":password"))}, want: authentication.ErrCredentialsInvalid},
		{name: "control in username", headers: []string{"Basic " + base64.StdEncoding.EncodeToString([]byte("ali\x00ce:password"))}, want: authentication.ErrCredentialsInvalid},
		{name: "control in password", headers: []string{"Basic " + base64.StdEncoding.EncodeToString([]byte("alice:pass\x7fword"))}, want: authentication.ErrCredentialsInvalid},
		{name: "duplicates", headers: []string{"Basic " + valid, "Basic " + valid}, want: authentication.ErrAmbiguousCredentials},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			request := requestWithHeaders(tt.headers)
			credential, err := extractor.Extract(request)
			if tt.valid {
				if err != nil {
					t.Fatalf("Extract() error = %v", err)
				}
				basicCredential, ok := credential.(authentication.BasicCredential)
				if !ok || basicCredential.Username() != "alice" || basicCredential.Password() != "password" {
					t.Fatalf("Extract() credential = %#v", credential)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Extract() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestBearerAuthorizationExtractionEnforcesGrammarAndBounds(t *testing.T) {
	t.Parallel()

	extractor := mustExtractor(t, authhttp.BearerAuthorization(authhttp.WithBearerMaxBytes(12)))
	tests := []struct {
		name   string
		header string
		valid  bool
		want   error
	}{
		{name: "valid", header: "Bearer abc-._~+/==", valid: true},
		{name: "wrong scheme is absent", header: "Basic abc", want: authentication.ErrCredentialsAbsent},
		{name: "empty", header: "Bearer ", want: authentication.ErrCredentialsInvalid},
		{name: "space", header: "Bearer abc def", want: authentication.ErrCredentialsInvalid},
		{name: "padding in middle", header: "Bearer abc=def", want: authentication.ErrCredentialsInvalid},
		{name: "unicode", header: "Bearer töken", want: authentication.ErrCredentialsInvalid},
		{name: "oversized", header: "Bearer abcdefghijklm", want: authentication.ErrCredentialsInvalid},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			request := requestWithHeaders([]string{tt.header})
			credential, err := extractor.Extract(request)
			if tt.valid {
				if err != nil {
					t.Fatalf("Extract() error = %v", err)
				}
				bearerCredential, ok := credential.(authentication.BearerCredential)
				if !ok || bearerCredential.Token() != "abc-._~+/==" {
					t.Fatalf("Extract() credential = %#v", credential)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Extract() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAPIKeySourcesMustBeExplicitAndRejectDuplicates(t *testing.T) {
	t.Parallel()

	headerSource := authhttp.APIKeyHeader("X-API-Key-ID", "X-API-Key", authhttp.WithAPIKeyMaxBytes(32))
	querySource := authhttp.APIKeyQuery("key_id", "api_key", authhttp.WithAPIKeyMaxBytes(32))
	cookieSource := authhttp.APIKeyCookie("key_id", "api_key", authhttp.WithAPIKeyMaxBytes(32))

	tests := []struct {
		name     string
		sources  []authhttp.Source
		headers  http.Header
		rawQuery string
		cookies  []*http.Cookie
		want     error
		wantID   string
		wantKey  string
	}{
		{name: "header", sources: []authhttp.Source{headerSource}, headers: http.Header{"X-Api-Key-Id": {"primary"}, "X-Api-Key": {"secret"}}, wantID: "primary", wantKey: "secret"},
		{name: "query", sources: []authhttp.Source{querySource}, rawQuery: "key_id=primary&api_key=secret", wantID: "primary", wantKey: "secret"},
		{name: "cookie", sources: []authhttp.Source{cookieSource}, cookies: []*http.Cookie{{Name: "key_id", Value: "primary"}, {Name: "api_key", Value: "secret"}}, wantID: "primary", wantKey: "secret"},
		{name: "query disabled", sources: []authhttp.Source{headerSource}, rawQuery: "key_id=primary&api_key=secret", want: authentication.ErrCredentialsAbsent},
		{name: "missing id", sources: []authhttp.Source{headerSource}, headers: http.Header{"X-Api-Key": {"secret"}}, want: authentication.ErrCredentialsInvalid},
		{name: "duplicate header", sources: []authhttp.Source{headerSource}, headers: http.Header{"X-Api-Key-Id": {"primary"}, "X-Api-Key": {"one", "two"}}, want: authentication.ErrAmbiguousCredentials},
		{name: "duplicate query", sources: []authhttp.Source{querySource}, rawQuery: "key_id=primary&api_key=one&api_key=two", want: authentication.ErrAmbiguousCredentials},
		{name: "duplicate cookie", sources: []authhttp.Source{cookieSource}, cookies: []*http.Cookie{{Name: "key_id", Value: "primary"}, {Name: "api_key", Value: "one"}, {Name: "api_key", Value: "two"}}, want: authentication.ErrAmbiguousCredentials},
		{name: "multiple sources", sources: []authhttp.Source{headerSource, querySource}, headers: http.Header{"X-Api-Key-Id": {"header"}, "X-Api-Key": {"header-secret"}}, rawQuery: "key_id=query&api_key=query-secret", want: authentication.ErrAmbiguousCredentials},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			extractor := mustExtractor(t, tt.sources...)
			headers := tt.headers
			if headers == nil {
				headers = make(http.Header)
			}
			request := &http.Request{Header: headers, URL: &url.URL{RawQuery: tt.rawQuery}}
			for _, cookie := range tt.cookies {
				request.AddCookie(cookie)
			}
			credential, err := extractor.Extract(request)
			if tt.want != nil {
				if !errors.Is(err, tt.want) {
					t.Fatalf("Extract() error = %v, want %v", err, tt.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}
			apiKey, ok := credential.(authentication.APIKeyCredential)
			if !ok || apiKey.KeyID() != tt.wantID || apiKey.Key() != tt.wantKey {
				t.Fatalf("Extract() credential = %#v", credential)
			}
		})
	}
}

func TestBearerQueryAndCookieAreExplicitSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  authhttp.Source
		request *http.Request
	}{
		{name: "query", source: authhttp.BearerQuery("access_token"), request: &http.Request{Header: make(http.Header), URL: &url.URL{RawQuery: "access_token=query-token"}}},
		{name: "cookie", source: authhttp.BearerCookie("access_token"), request: requestWithCookie(&http.Cookie{Name: "access_token", Value: "cookie-token"})},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			credential, err := mustExtractor(t, tt.source).Extract(tt.request)
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}
			if _, ok := credential.(authentication.BearerCredential); !ok {
				t.Fatalf("Extract() credential = %T", credential)
			}
		})
	}
}

func TestExtractorRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := [][]authhttp.Source{
		nil,
		{nil},
		{authhttp.APIKeyHeader("bad header", "X-Key")},
		{authhttp.APIKeyQuery("", "key")},
		{authhttp.BearerCookie("")},
		{authhttp.BearerAuthorization(authhttp.WithBearerMaxBytes(0))},
	}
	for _, sources := range tests {
		if _, err := authhttp.NewExtractor(sources...); !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Errorf("NewExtractor() error = %v", err)
		}
	}
}

func TestExtractorRejectsNilRequest(t *testing.T) {
	t.Parallel()

	if _, err := mustExtractor(t, authhttp.BearerAuthorization()).Extract(nil); !errors.Is(err, authentication.ErrCredentialsInvalid) {
		t.Fatalf("Extract(nil) error = %v", err)
	}
}

func TestExtractorSeparatesOriginAndProxyCredentials(t *testing.T) {
	t.Parallel()

	extractor := mustExtractor(t, authhttp.BearerAuthorization())
	request := &http.Request{Header: make(http.Header), URL: &url.URL{}}
	request.Header.Set("Proxy-Authorization", "Bearer proxy-token")
	if _, err := extractor.Extract(request); !errors.Is(err, authentication.ErrCredentialsAbsent) {
		t.Fatalf("Extract(proxy only) error = %v", err)
	}

	request.Header.Add("authorization", "Bearer origin-token")
	credential, err := extractor.Extract(request)
	if err != nil {
		t.Fatalf("Extract(origin and proxy) error = %v", err)
	}
	bearer, ok := credential.(authentication.BearerCredential)
	if !ok || bearer.Token() != "origin-token" {
		t.Fatalf("Extract(origin and proxy) credential = %T", credential)
	}
}

func TestAuthorizationSourcesHandleNakedSchemesAndBasicBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source authhttp.Source
		header string
		want   error
	}{
		{name: "naked wanted scheme", source: authhttp.BearerAuthorization(), header: "Bearer", want: authentication.ErrCredentialsInvalid},
		{name: "naked other scheme", source: authhttp.BearerAuthorization(), header: "Digest", want: authentication.ErrCredentialsAbsent},
		{name: "oversized basic", source: authhttp.BasicAuthorization(), header: "Basic " + base64.StdEncoding.EncodeToString(make([]byte, 8*1024+3)), want: authentication.ErrCredentialsInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mustExtractor(t, tt.source).Extract(requestWithHeaders([]string{tt.header}))
			if !errors.Is(err, tt.want) {
				t.Fatalf("Extract() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNamedBearerSourcesRejectAbsentDuplicateAndHostileValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  authhttp.Source
		request *http.Request
		want    error
	}{
		{name: "nil URL", source: authhttp.BearerQuery("token"), request: &http.Request{Header: make(http.Header)}, want: authentication.ErrCredentialsInvalid},
		{name: "malformed query", source: authhttp.BearerQuery("token"), request: &http.Request{Header: make(http.Header), URL: &url.URL{RawQuery: "%"}}, want: authentication.ErrCredentialsInvalid},
		{name: "absent", source: authhttp.BearerQuery("token"), request: &http.Request{Header: make(http.Header), URL: &url.URL{}}, want: authentication.ErrCredentialsAbsent},
		{name: "duplicate", source: authhttp.BearerQuery("token"), request: &http.Request{Header: make(http.Header), URL: &url.URL{RawQuery: "token=one&token=two"}}, want: authentication.ErrAmbiguousCredentials},
		{name: "empty", source: authhttp.BearerQuery("token"), request: &http.Request{Header: make(http.Header), URL: &url.URL{RawQuery: "token="}}, want: authentication.ErrCredentialsInvalid},
		{name: "invalid", source: authhttp.BearerCookie("token"), request: requestWithCookie(&http.Cookie{Name: "token", Value: "bad token"}), want: authentication.ErrCredentialsInvalid},
		{name: "oversized", source: authhttp.BearerQuery("token", authhttp.WithBearerMaxBytes(3)), request: &http.Request{Header: make(http.Header), URL: &url.URL{RawQuery: "token=long"}}, want: authentication.ErrCredentialsInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mustExtractor(t, tt.source).Extract(tt.request)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Extract() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAPIKeySourcesRejectMalformedQueriesAndBoundedValues(t *testing.T) {
	t.Parallel()

	source := authhttp.APIKeyQuery("id", "key", authhttp.WithAPIKeyMaxBytes(3))
	tests := []struct {
		name    string
		request *http.Request
		want    error
	}{
		{name: "nil URL", request: &http.Request{Header: make(http.Header)}, want: authentication.ErrCredentialsInvalid},
		{name: "malformed query", request: &http.Request{Header: make(http.Header), URL: &url.URL{RawQuery: "%"}}, want: authentication.ErrCredentialsInvalid},
		{name: "empty values", request: &http.Request{Header: make(http.Header), URL: &url.URL{RawQuery: "id=&key="}}, want: authentication.ErrCredentialsInvalid},
		{name: "oversized key", request: &http.Request{Header: make(http.Header), URL: &url.URL{RawQuery: "id=primary&key=long"}}, want: authentication.ErrCredentialsInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mustExtractor(t, source).Extract(tt.request)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Extract() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func mustExtractor(t *testing.T, sources ...authhttp.Source) *authhttp.Extractor {
	t.Helper()
	extractor, err := authhttp.NewExtractor(sources...)
	if err != nil {
		t.Fatalf("NewExtractor() error = %v", err)
	}
	return extractor
}

func requestWithHeaders(values []string) *http.Request {
	request := &http.Request{Header: make(http.Header), URL: &url.URL{}}
	for _, value := range values {
		request.Header.Add("Authorization", value)
	}
	return request
}

func requestWithCookie(cookie *http.Cookie) *http.Request {
	request := &http.Request{Header: make(http.Header), URL: &url.URL{}}
	request.AddCookie(cookie)
	return request
}
