package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestPolicyAndDirectInformationBoundaries(t *testing.T) {
	t.Parallel()
	for _, policy := range []Policy{
		{Mode: XForwarded + 1}, {MaxHops: -1}, {MaxHops: 129},
		{MaxHeaderBytes: -1}, {MaxHeaderBytes: 1<<20 + 1},
		{Trusted: make([]netip.Prefix, 257)}, {Trusted: []netip.Prefix{{}}},
	} {
		_, err := New(policy)
		var configuration *ConfigError
		if !errors.As(err, &configuration) || !errors.Is(err, ErrInvalidPolicy) || configuration.Error() == "" {
			t.Fatalf("New(%+v) error = %v", policy, err)
		}
	}
	if got := FromContext(context.Background()); got != (Info{}) {
		t.Fatalf("missing info = %+v", got)
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	request.RemoteAddr = "2001:db8::1"
	request.TLS = &tls.ConnectionState{}
	info := directInfo(request)
	if info.ClientIP.String() != "2001:db8::1" || info.Scheme != "https" {
		t.Fatalf("direct info = %+v", info)
	}
	request.RemoteAddr = "not-an-address"
	if directInfo(request).ClientIP.IsValid() {
		t.Fatal("invalid remote address became valid")
	}
}

func TestForwardedValueNodeHostAndPrefixGrammar(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw, want string
		ok        bool
	}{
		{"token", "token", true}, {" token ", "token", true}, {"", "", false}, {"bad\"token", "bad\"token", false},
		{`"quoted"`, "quoted", true}, {`"escaped\\value"`, `escaped\value`, true}, {`"`, "", false}, {`"unterminated`, "", false},
		{`"bad\"`, "", false}, {"\"bad\r\"", "", false}, {"\"bad\n\"", "", false}, {"\"bad\x00\"", "", false}, {`"bad"quote"`, "", false},
	} {
		got, ok := forwardedValue(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("forwardedValue(%q) = %q, %v", tc.raw, got, ok)
		}
	}
	for _, value := range []string{"198.51.100.1", "[2001:db8::1]", "198.51.100.1:443", "[2001:db8::1]:443"} {
		if _, ok := parseNode(value); !ok {
			t.Fatalf("parseNode(%q) failed", value)
		}
	}
	for _, value := range []string{"_hidden", "unknown", "bad"} {
		if _, ok := parseNode(value); ok {
			t.Fatalf("parseNode(%q) succeeded", value)
		}
	}
	for _, value := range []string{"example.com", "example.com:443", "[2001:db8::1]:443"} {
		if !validHost(value) {
			t.Fatalf("validHost(%q) = false", value)
		}
	}
	for _, value := range []string{"", strings.Repeat("a", 256), "bad/path", "user@example.com", "example.com:0", "example.com:99999", "example.com:not"} {
		if validHost(value) {
			t.Fatalf("validHost(%q) = true", value)
		}
	}
	for _, value := range []string{"/", "/api", "/api/v1"} {
		if !validPrefix(value) {
			t.Fatalf("validPrefix(%q) = false", value)
		}
	}
	for _, value := range []string{"", "api", "//evil", "/api/../admin", "/api?x", "/api#x", "/api\\x", strings.Repeat("/", 257), "/bad\n", "/%zz"} {
		if validPrefix(value) {
			t.Fatalf("validPrefix(%q) = true", value)
		}
	}
}

func TestForwardedFieldBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		values         []string
		value          string
		present, valid bool
	}{
		{nil, "", false, true}, {[]string{"a", "b"}, "", true, false}, {[]string{""}, "", true, false},
		{[]string{"a,b"}, "b", true, true}, {[]string{strings.Repeat("a", 4097)}, "", true, false}, {[]string{"bad\n"}, "", true, false},
	} {
		header := http.Header{}
		if tc.values != nil {
			header["X-Test"] = tc.values
		}
		value, present, valid := forwardedField(header, "X-Test", 2, 5000)
		if value != tc.value || present != tc.present || valid != tc.valid {
			t.Fatalf("forwardedField(%q) = %q, %v, %v", tc.values, value, present, valid)
		}
	}
}

func TestXForwardedMetadataTruthTable(t *testing.T) {
	t.Parallel()
	direct := Info{ClientIP: netip.MustParseAddr("10.0.0.3"), Host: "internal", Scheme: "http", Provenance: Direct}
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	for _, tc := range []struct {
		name   string
		header http.Header
		ok     bool
	}{
		{"valid all", http.Header{"X-Forwarded-For": {"198.51.100.1, 10.0.0.2"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"api.example.com:443"}, "X-Forwarded-Prefix": {"/api"}}, true},
		{"missing for", http.Header{}, false}, {"duplicate for", http.Header{"X-Forwarded-For": {"a", "b"}}, false},
		{"empty for", http.Header{"X-Forwarded-For": {""}}, false},
		{"bad for", http.Header{"X-Forwarded-For": {"bad"}}, false}, {"bad proto", http.Header{"X-Forwarded-For": {"198.51.100.1"}, "X-Forwarded-Proto": {"ftp"}}, false},
		{"duplicate proto", http.Header{"X-Forwarded-For": {"198.51.100.1"}, "X-Forwarded-Proto": {"http", "https"}}, false},
		{"bad host", http.Header{"X-Forwarded-For": {"198.51.100.1"}, "X-Forwarded-Host": {"evil/path"}}, false},
		{"duplicate host", http.Header{"X-Forwarded-For": {"198.51.100.1"}, "X-Forwarded-Host": {"a", "b"}}, false},
		{"bad prefix", http.Header{"X-Forwarded-For": {"198.51.100.1"}, "X-Forwarded-Prefix": {"//evil"}}, false},
		{"duplicate prefix", http.Header{"X-Forwarded-For": {"198.51.100.1"}, "X-Forwarded-Prefix": {"/a", "/b"}}, false},
	} {
		request := httptest.NewRequest(http.MethodGet, "http://internal/", nil)
		request.Header = tc.header
		info, ok := forwardedInfo(request, direct, XForwarded, 16, 8192, trusted)
		if ok != tc.ok {
			t.Fatalf("%s ok = %v, info=%+v", tc.name, ok, info)
		}
		if tc.ok && (info.ClientIP.String() != "198.51.100.1" || info.Scheme != "https" || info.Prefix != "/api") {
			t.Fatalf("valid info = %+v", info)
		}
	}
}

func TestRFCForwardedMalformedMatrix(t *testing.T) {
	t.Parallel()
	direct := Info{ClientIP: netip.MustParseAddr("10.0.0.3"), Host: "internal", Scheme: "http", Provenance: Direct}
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	for _, value := range []string{"", "for=198.51.100.1;", "for=198.51.100.1,for=10.0.0.2,for=10.0.0.1", "broken", "for=", "for=_hidden", "proto=https", "for=198.51.100.1;proto=ftp", "for=198.51.100.1;host=bad/path", "for=198.51.100.1;for=203.0.113.1", "for=198.51.100.1;proto=http;proto=https", "for=198.51.100.1;bad key=value", "for=198.51.100.1;=value"} {
		_, ok := parseForwarded([]string{value}, direct, 2, 8192, trusted)
		if ok {
			t.Fatalf("parseForwarded(%q) succeeded", value)
		}
	}
	if _, ok := parseForwarded(nil, direct, 2, 8192, trusted); ok {
		t.Fatal("missing Forwarded succeeded")
	}
	info, ok := parseForwarded([]string{`for=198.51.100.1;by=_proxy;proto=HTTPS;host=api.example.com`}, direct, 2, 8192, trusted)
	if !ok || info.Scheme != "https" || info.Host != "api.example.com" {
		t.Fatalf("forwarded info = %+v, %v", info, ok)
	}
}
