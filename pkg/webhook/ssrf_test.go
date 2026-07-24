package webhook

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSSRFPolicyRejectsUnsafeURLsAndAddresses(t *testing.T) {
	t.Parallel()

	resolver := &staticResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	policy, err := NewSSRFPolicy(SSRFPolicyConfig{Resolver: resolver, MaxAddresses: 8})
	if err != nil {
		t.Fatalf("NewSSRFPolicy() error = %v", err)
	}

	tests := map[string]string{
		"HTTP":                "http://example.com/hook",
		"userinfo":            "https://user:pass@example.com/hook",
		"fragment":            "https://example.com/hook#fragment",
		"loopback IPv4":       "https://127.0.0.1/hook",
		"loopback IPv6":       "https://[::1]/hook",
		"mapped loopback":     "https://[::ffff:127.0.0.1]/hook",
		"private IPv4":        "https://10.0.0.1/hook",
		"link local metadata": "https://169.254.169.254/latest/meta-data",
		"documentation IPv4":  "https://192.0.2.1/hook",
		"documentation IPv6":  "https://[2001:db8::1]/hook",
		"non-ASCII host":      "https://bücher.example/hook",
		"noncanonical host":   "https://example.com./hook",
	}
	for name, rawURL := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			endpoint, parseErr := url.Parse(rawURL)
			if parseErr != nil {
				t.Fatalf("url.Parse() error = %v", parseErr)
			}
			if err := policy.Validate(context.Background(), endpoint); !errors.Is(err, ErrEndpointRejected) {
				t.Fatalf("Validate() error = %v, want ErrEndpointRejected", err)
			}
		})
	}
}

func TestSSRFPolicyRejectsMixedAndOversizedDNSAnswers(t *testing.T) {
	t.Parallel()

	for name, addresses := range map[string][]netip.Addr{
		"mixed": {
			netip.MustParseAddr("93.184.216.34"),
			netip.MustParseAddr("127.0.0.1"),
		},
		"too many": {
			netip.MustParseAddr("93.184.216.34"),
			netip.MustParseAddr("93.184.216.35"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			policy, err := NewSSRFPolicy(SSRFPolicyConfig{Resolver: &staticResolver{addresses: addresses}, MaxAddresses: 1})
			if err != nil {
				t.Fatalf("NewSSRFPolicy() error = %v", err)
			}
			if err := policy.Validate(context.Background(), mustURL(t, "https://example.com/hook")); !errors.Is(err, ErrEndpointRejected) {
				t.Fatalf("Validate() error = %v, want ErrEndpointRejected", err)
			}
		})
	}
}

func TestSecureHTTPClientRevalidatesDNSAtDialTime(t *testing.T) {
	t.Parallel()

	resolver := &sequenceResolver{answers: [][]netip.Addr{
		{netip.MustParseAddr("93.184.216.34")},
		{netip.MustParseAddr("127.0.0.1")},
	}}
	policy, err := NewSSRFPolicy(SSRFPolicyConfig{Resolver: resolver, MaxAddresses: 4})
	if err != nil {
		t.Fatalf("NewSSRFPolicy() error = %v", err)
	}
	client, err := NewSecureHTTPClient(policy, time.Second)
	if err != nil {
		t.Fatalf("NewSecureHTTPClient() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com/hook", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	_, err = client.Do(request)
	if !errors.Is(err, ErrEndpointRejected) {
		t.Fatalf("Do() error = %v, want ErrEndpointRejected", err)
	}
	if resolver.calls != 2 {
		t.Fatalf("resolver calls = %d, want URL validation and dial validation", resolver.calls)
	}
}

func TestSecureHTTPClientRejectsRedirectWithoutContactingTarget(t *testing.T) {
	t.Parallel()

	var targetContacted atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetContacted.Store(true)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	loopback := netip.MustParsePrefix("127.0.0.0/8")
	policy, err := NewSSRFPolicy(SSRFPolicyConfig{
		Resolver:        net.DefaultResolver,
		AllowHTTP:       true,
		AllowedPrefixes: []netip.Prefix{loopback},
		MaxAddresses:    8,
	})
	if err != nil {
		t.Fatalf("NewSSRFPolicy() error = %v", err)
	}
	client, err := NewSecureHTTPClient(policy, time.Second)
	if err != nil {
		t.Fatalf("NewSecureHTTPClient() error = %v", err)
	}
	response, err := client.Get(redirector.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("response.Body.Close() error = %v", err)
	}
	if response.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.StatusCode)
	}
	if targetContacted.Load() {
		t.Fatal("redirect target was contacted")
	}
}

func TestSSRFPolicyAllowsExplicitPrefixOnlyWhenConfigured(t *testing.T) {
	t.Parallel()

	policy, err := NewSSRFPolicy(SSRFPolicyConfig{
		Resolver:        &staticResolver{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		AllowHTTP:       true,
		AllowedPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		MaxAddresses:    1,
	})
	if err != nil {
		t.Fatalf("NewSSRFPolicy() error = %v", err)
	}
	if err := policy.Validate(context.Background(), mustURL(t, "http://localhost/hook")); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSSRFPolicyConfigurationAndAddressFailures(t *testing.T) {
	t.Parallel()

	if _, err := NewSSRFPolicy(SSRFPolicyConfig{}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewSSRFPolicy() error = %v", err)
	}
	invalid := netip.Prefix{}
	if _, err := NewSSRFPolicy(SSRFPolicyConfig{Resolver: net.DefaultResolver, MaxAddresses: 1, AllowedPrefixes: []netip.Prefix{invalid}}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewSSRFPolicy() allowed-prefix error = %v", err)
	}
	if _, err := NewSSRFPolicy(SSRFPolicyConfig{Resolver: net.DefaultResolver, MaxAddresses: 1, DeniedPrefixes: []netip.Prefix{invalid}}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewSSRFPolicy() denied-prefix error = %v", err)
	}
	policy, _ := NewSSRFPolicy(SSRFPolicyConfig{
		Resolver: net.DefaultResolver, MaxAddresses: 1,
		DeniedPrefixes: []netip.Prefix{netip.MustParsePrefix("93.184.216.0/24")},
	})
	for name, endpoint := range map[string]*url.URL{
		"nil":          nil,
		"relative":     {Path: "/hook"},
		"opaque":       {Scheme: "https", Opaque: "example.com/hook"},
		"invalid port": {Scheme: "https", Host: "example.com:99999", Path: "/hook"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := policy.Validate(context.Background(), endpoint); !errors.Is(err, ErrEndpointRejected) {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
	if err := policy.validateAddress(netip.Addr{}); !errors.Is(err, ErrEndpointRejected) {
		t.Fatalf("validateAddress(invalid) error = %v", err)
	}
	if err := policy.validateAddress(netip.MustParseAddr("93.184.216.34")); !errors.Is(err, ErrEndpointRejected) {
		t.Fatalf("validateAddress(denied) error = %v", err)
	}
}

func TestSecureHTTPClientConfigurationAndTransportFailures(t *testing.T) {
	t.Parallel()

	if _, err := NewSecureHTTPClient(nil, time.Second); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewSecureHTTPClient(nil) error = %v", err)
	}
	policy, _ := NewSSRFPolicy(SSRFPolicyConfig{
		Resolver:     &staticResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}},
		MaxAddresses: 1,
	})
	client, err := NewSecureHTTPClient(policy, time.Millisecond)
	if err != nil {
		t.Fatalf("NewSecureHTTPClient() error = %v", err)
	}
	transport := client.Transport.(*policyTransport)
	if transport.next.(*http.Transport).Proxy != nil {
		t.Fatal("secure transport inherited a proxy function")
	}
	if _, err := transport.RoundTrip(nil); !errors.Is(err, ErrEndpointRejected) {
		t.Fatalf("RoundTrip(nil) error = %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if _, err := transport.RoundTrip(request); !errors.Is(err, ErrEndpointRejected) {
		t.Fatalf("RoundTrip(policy rejection) error = %v", err)
	}
	dial := transport.next.(*http.Transport).DialContext
	if _, err := dial(context.Background(), "tcp", "missing-port"); !errors.Is(err, ErrEndpointRejected) {
		t.Fatalf("DialContext(invalid address) error = %v", err)
	}
	if _, err := dial(context.Background(), "tcp", "example.com:1"); err == nil {
		t.Fatal("DialContext() unexpectedly connected")
	}
}

type staticResolver struct {
	addresses []netip.Addr
	err       error
}

func (r *staticResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return append([]netip.Addr(nil), r.addresses...), r.err
}

type sequenceResolver struct {
	mu      sync.Mutex
	answers [][]netip.Addr
	calls   int
}

func (r *sequenceResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.calls
	r.calls++
	if index >= len(r.answers) {
		return nil, &net.DNSError{Err: "no answer"}
	}

	return append([]netip.Addr(nil), r.answers[index]...), nil
}
