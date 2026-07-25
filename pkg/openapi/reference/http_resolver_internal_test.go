package reference

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/specification"
)

func TestHTTPPolicyClassifiesNetworkAddresses(t *testing.T) {
	t.Parallel()

	policy := testHTTPPolicy(t)
	for raw, allowed := range map[string]bool{
		"8.8.8.8":         true,
		"0.0.0.1":         false,
		"127.0.0.1":       false,
		"10.0.0.1":        false,
		"100.64.0.1":      false,
		"169.254.1.1":     false,
		"192.0.2.1":       false,
		"198.18.0.1":      false,
		"198.51.100.1":    false,
		"203.0.113.1":     false,
		"240.0.0.1":       false,
		"100.63.255.255":  true,
		"100.127.255.255": false,
		"100.128.0.0":     true,
		"192.0.0.1":       false,
		"192.0.1.1":       true,
		"198.17.255.255":  true,
		"198.19.255.255":  false,
		"198.20.0.0":      true,
		"2001:4860::1":    true,
		"64:ff9b:1::1":    false,
		"100::1":          false,
		"100:0:0:1::1":    false,
		"2001:2::1":       false,
		"2001:db8::1":     false,
		"3fff::1":         false,
		"5f00::1":         false,
		"fc00::1":         false,
		"fe80::1":         false,
		"::1":             false,
	} {
		address := netip.MustParseAddr(raw)
		if actual := policy.addressAllowed(address); actual != allowed {
			t.Fatalf("addressAllowed(%s) = %t", raw, actual)
		}
	}
	policy.allowedCIDRs = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	if !policy.addressAllowed(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("explicit network exception was ignored")
	}
}

func TestHTTPPolicyDeniesPinnedSpecialPurposeRegistries(t *testing.T) {
	t.Parallel()

	policy := testHTTPPolicy(t)
	for _, name := range []string{
		"registries/iana/iana-ipv4-special-registry-1.csv",
		"registries/iana/iana-ipv6-special-registry-1.csv",
	} {
		body, err := specification.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		records, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range records[1:] {
			if len(record) == 0 || strings.TrimSpace(record[0]) == "" {
				continue
			}
			for _, raw := range strings.Split(record[0], ",") {
				block := strings.TrimSpace(strings.SplitN(raw, " ", 2)[0])
				if block == "" {
					continue
				}
				prefix, err := netip.ParsePrefix(block)
				if err != nil {
					t.Fatalf("parse pinned address block %q: %v", block, err)
				}
				if policy.addressAllowed(prefix.Addr()) {
					t.Errorf("pinned special-purpose block %s is allowed", prefix)
				}
			}
		}
	}
}

func TestHTTPBoundaryHelpersAcceptExactEndpoints(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusOK, http.StatusMultipleChoices - 1} {
		if !successfulHTTPStatus(status) {
			t.Fatalf("successful status %d rejected", status)
		}
	}
	for _, status := range []int{http.StatusOK - 1, http.StatusMultipleChoices} {
		if successfulHTTPStatus(status) {
			t.Fatalf("unsuccessful status %d accepted", status)
		}
	}
	if contentLengthExceeds(2, 2) || !contentLengthExceeds(3, 2) {
		t.Fatal("content length endpoint contract failed")
	}
	if !validAddressCount(1, 1) || validAddressCount(0, 1) ||
		validAddressCount(2, 1) {
		t.Fatal("address count endpoint contract failed")
	}
}

func TestHTTPPolicyAcceptsMinimumLimitsAndPortEndpoints(t *testing.T) {
	t.Parallel()

	options := DefaultHTTPResolverOptions()
	options.AllowedHosts = []string{"example.test"}
	options.AllowedPorts = []int{1, 65535}
	options.MaxBytes = 1
	options.MaxDocuments = 1
	options.MaxRedirects = 0
	options.MaxConcurrency = 1
	options.MaxAddresses = 1
	options.MaxResponseHeaderBytes = 1
	options.Timeout = 1
	if _, err := newHTTPPolicy(options); err != nil {
		t.Fatalf("minimum HTTP policy error = %v", err)
	}
}

func TestHTTPPolicyRejectsNonCanonicalAllowedHosts(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"*.example.test", "example.test/path"} {
		if _, err := normalizeHost(host); err == nil {
			t.Errorf("normalizeHost(%q) unexpectedly succeeded", host)
		}
		options := DefaultHTTPResolverOptions()
		options.AllowedHosts = []string{host}
		if _, err := newHTTPPolicy(options); !errors.Is(err, ErrResourceDenied) {
			t.Errorf("allowed host %q error = %v", host, err)
		}
	}
}

func TestHTTPResolverInjectsRequestConstructionFailure(t *testing.T) {
	t.Parallel()

	options := DefaultHTTPResolverOptions()
	options.AllowedHosts = []string{"example.test"}
	resolver, err := NewHTTPResolver(options)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("sensitive request detail")
	resolver.createRequest = func(context.Context, string) (*http.Request, error) {
		return nil, want
	}
	if _, err := resolver.Resolve(
		context.Background(), "https://example.test/schema.json",
	); !errors.Is(err, ErrResourceAccess) || strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("request construction error = %v", err)
	}
}

func TestHTTPResolverRedactsRequestPathsAndTransportErrors(t *testing.T) {
	t.Parallel()

	options := DefaultHTTPResolverOptions()
	options.AllowedHosts = []string{"example.test"}
	resolver, err := NewHTTPResolver(options)
	if err != nil {
		t.Fatal(err)
	}
	resolver.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("sensitive transport detail")
	})
	_, err = resolver.Resolve(
		context.Background(), "https://example.test/private-token/schema.json",
	)
	if err == nil {
		t.Fatal("transport failure was accepted")
	}
	if !errors.Is(err, ErrResourceAccess) {
		t.Errorf("transport failure classification = %v", err)
	}
	for _, sensitive := range []string{"private-token", "sensitive transport detail"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Errorf("HTTP resolver error exposed %q: %v", sensitive, err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestHTTPPolicyValidatesCanonicalAuthorities(t *testing.T) {
	t.Parallel()

	policy := testHTTPPolicy(t)
	for _, identifier := range []string{
		"https://example.test/schema.json",
		"https://EXAMPLE.TEST./schema.json",
	} {
		if _, err := policy.validateURL(identifier); err != nil {
			t.Fatalf("identifier %q rejected: %v", identifier, err)
		}
	}
	for _, identifier := range []string{
		"%",
		"https:opaque",
		"https:///schema.json",
		"https://example.test:invalid/schema.json",
		"https://example.test:999999999999999999999/schema.json",
		"https://other.test/schema.json",
		"http://example.test/schema.json",
		"https://example.test:8443/schema.json",
		"https://example.test/schema.json#fragment",
	} {
		if _, err := policy.validateURL(identifier); !errors.Is(err, ErrResourceDenied) {
			t.Fatalf("identifier %q error = %v", identifier, err)
		}
	}
}

func TestHTTPPolicyDialRejectsInvalidAndUnreachableTargets(t *testing.T) {
	t.Parallel()

	policy := testHTTPPolicy(t)
	if _, err := policy.dialContext(
		context.Background(), "tcp", "invalid",
	); !errors.Is(err, ErrResourceDenied) {
		t.Fatalf("invalid authority error = %v", err)
	}
	if _, err := policy.dialContext(
		context.Background(), "tcp", "example.test:invalid",
	); !errors.Is(err, ErrResourceDenied) {
		t.Fatalf("invalid port error = %v", err)
	}
	if _, err := policy.dialContext(
		context.Background(), "tcp", "example.test:80",
	); !errors.Is(err, ErrResourceDenied) {
		t.Fatalf("denied port error = %v", err)
	}
	if _, err := policy.dialContext(
		context.Background(), "tcp", "127.0.0.1:443",
	); !errors.Is(err, ErrResourceDenied) {
		t.Fatalf("private address error = %v", err)
	}

	policy.allowedCIDRs = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	want := errors.New("dial failed")
	policy.dial = func(context.Context, string, string) (net.Conn, error) {
		return nil, want
	}
	if _, err := policy.dialContext(
		context.Background(), "tcp", "127.0.0.1:443",
	); !errors.Is(err, want) {
		t.Fatalf("allowed connection error = %v", err)
	}
	policy.maxAddresses = 0
	if _, err := policy.dialContext(
		context.Background(), "tcp", "127.0.0.1:443",
	); !errors.Is(err, ErrResourceLimitExceeded) {
		t.Fatalf("address limit error = %v", err)
	}
	policy = testHTTPPolicy(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := policy.dialContext(
		ctx, "tcp", "does-not-exist.invalid:443",
	); err == nil {
		t.Fatal("canceled DNS resolution returned no error")
	}
}

func TestHTTPResponseFormatUsesAuthoritativeMediaTypeOrExtension(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		contentType string
		path        string
		want        resourceFormat
		wantError   bool
	}{
		{contentType: "application/problem+json", path: "/schema", want: resourceJSON},
		{contentType: "application/x-yaml", path: "/schema", want: resourceYAML},
		{contentType: "text/yaml; charset=utf-8", path: "/schema", want: resourceYAML},
		{contentType: "application/octet-stream", path: "/schema.yml", wantError: true},
		{contentType: "application/octet-stream", path: "/schema.json", wantError: true},
		{contentType: "malformed type", path: "/schema.json", wantError: true},
		{path: "/schema.json", want: resourceJSON},
		{path: "/schema.yml", want: resourceYAML},
		{path: "/schema", wantError: true},
	} {
		response := &http.Response{
			Header:  make(http.Header),
			Request: &http.Request{URL: &url.URL{Path: test.path}},
			Body:    io.NopCloser(strings.NewReader("")),
		}
		response.Header.Set("Content-Type", test.contentType)
		actual, err := responseFormat(response)
		if test.wantError {
			if !errors.Is(err, ErrUnsupportedResourceFormat) {
				t.Fatalf("responseFormat(%q, %q) error = %v", test.contentType, test.path, err)
			}
			continue
		}
		if err != nil || actual != test.want {
			t.Fatalf("responseFormat(%q, %q) = %v, %v", test.contentType, test.path, actual, err)
		}
	}
}

func TestHTTPHelpersNormalizeHostsAndPorts(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"EXAMPLE.TEST.": "example.test",
		"127.0.0.1":     "127.0.0.1",
	} {
		actual, err := normalizeHost(input)
		if err != nil || actual != want {
			t.Fatalf("normalizeHost(%q) = %q, %v", input, actual, err)
		}
	}
	if _, err := normalizeHost(""); err == nil {
		t.Fatal("empty host was accepted")
	}
	if _, err := normalizeHost("."); err == nil {
		t.Fatal("empty normalized host was accepted")
	}
	for raw, want := range map[string]int{
		"http://example.test":       80,
		"https://example.test":      443,
		"https://example.test:8443": 8443,
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := effectivePort(parsed)
		if err != nil || actual != want {
			t.Fatalf("effectivePort(%q) = %d, %v", raw, actual, err)
		}
	}
	if _, err := effectivePort(&url.URL{Scheme: "ftp"}); err == nil {
		t.Fatal("unknown default port was accepted")
	}
}

func testHTTPPolicy(t *testing.T) *httpPolicy {
	t.Helper()
	options := DefaultHTTPResolverOptions()
	options.AllowedHosts = []string{"example.test"}
	policy, err := newHTTPPolicy(options)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}
