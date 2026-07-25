package httpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEgressDialerValidatesEveryDNSAnswerBeforeConnecting(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int64
	resolver := EgressResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("127.0.0.1")}, nil
	})
	policy, err := NewEgressPolicy(EgressOptions{
		AllowedHosts: []string{"api.example.test"}, Resolver: resolver,
	})
	if err != nil {
		t.Fatalf("construct rebinding policy: %v", err)
	}
	dialer := &egressDialer{
		policy: policy,
		dialer: egressDialerFunc(func(context.Context, string, string) (net.Conn, error) {
			attempts.Add(1)
			return nil, errors.New("unexpected dial")
		}),
	}
	if _, err := dialer.DialContext(context.Background(), "tcp", "api.example.test:443"); !errors.Is(err, ErrEgressDenied) || attempts.Load() != 0 {
		t.Fatalf("rebinding error = %v, attempts = %d", err, attempts.Load())
	}
}

func TestEgressDialerConnectsOnlyToValidatedResolvedAddress(t *testing.T) {
	t.Parallel()

	resolver := EgressResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.0.2.20")}, nil
	})
	policy, _ := NewEgressPolicy(EgressOptions{
		AllowedHosts: []string{"api.example.test"}, AllowedCIDRs: []string{"192.0.2.0/24"},
		Resolver: resolver,
	})
	var dialed string
	peer, accepted := net.Pipe()
	t.Cleanup(func() {
		_ = peer.Close()
		_ = accepted.Close()
	})
	dialer := &egressDialer{
		policy: policy,
		dialer: egressDialerFunc(func(_ context.Context, network string, address string) (net.Conn, error) {
			if network != "tcp" {
				t.Fatalf("dial network = %q", network)
			}
			dialed = address
			return accepted, nil
		}),
	}
	connection, err := dialer.DialContext(context.Background(), "tcp", "api.example.test:443")
	if err != nil {
		t.Fatalf("validated dial: %v", err)
	}
	_ = connection.Close()
	if dialed != "192.0.2.20:443" {
		t.Fatalf("dialed address = %q", dialed)
	}
}

func TestEgressPolicyRejectsMalformedConfigurationAndDeniedCIDR(t *testing.T) {
	t.Parallel()

	var nilResolver EgressResolverFunc
	for _, options := range []EgressOptions{
		{AllowedPorts: []uint16{0}},
		{AllowedHosts: []string{""}},
		{AllowedHosts: []string{"*.example.test"}},
		{AllowedHosts: []string{"-bad.example.test"}},
		{AllowedHosts: []string{strings.Repeat("a", 64) + ".test"}},
		{AllowedHosts: []string{strings.Repeat("a", 254)}},
		{AllowedHosts: []string{"snowman-☃.test"}},
		{AllowedOrigins: []string{"https://example.test/path"}},
		{AllowedOrigins: []string{"ftp://example.test"}},
		{AllowedOrigins: []string{"ftp://example.test:21"}},
		{AllowedOrigins: []string{"https://example.test:bad"}},
		{AllowedOrigins: []string{"https://bad_host.test"}},
		{AllowedCIDRs: []string{"not-cidr"}},
		{DeniedCIDRs: []string{"not-cidr"}},
		{Resolver: nilResolver},
	} {
		if _, err := NewEgressPolicy(options); !errors.Is(err, ErrInvalidEgressPolicy) {
			t.Fatalf("invalid options %#v error = %v", options, err)
		}
	}
	policy, _ := NewEgressPolicy(EgressOptions{DeniedCIDRs: []string{"192.0.2.0/24"}})
	if err := policy.ValidateIP(net.ParseIP("192.0.2.10")); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("denied CIDR error = %v", err)
	}
}

func TestEgressValidationAndTransportBoundaryErrors(t *testing.T) {
	t.Parallel()

	policy, _ := NewEgressPolicy(EgressOptions{
		AllowedHosts:   []string{"api.example.test"},
		AllowedOrigins: []string{"https://api.example.test"},
	})
	if (&EgressError{}).Error() == "" {
		t.Fatal("egress error rendered empty text")
	}
	for _, target := range []*url.URL{
		nil,
		{Scheme: "https", Host: ""},
		{Scheme: "https", Host: "user@example.test", User: url.User("user")},
		{Scheme: "https", Host: "bad_host.test"},
		{Scheme: "https", Host: "api.example.test:bad"},
		{Scheme: "https", Host: "api.example.test:444"},
	} {
		if err := policy.ValidateURL(target); !errors.Is(err, ErrEgressDenied) {
			t.Fatalf("target %#v error = %v", target, err)
		}
	}
	portPolicy, _ := NewEgressPolicy(EgressOptions{})
	if err := portPolicy.ValidateURL(&url.URL{Scheme: "https", Host: "192.0.2.1:0"}); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("invalid URL port = %v", err)
	}
	for _, test := range []struct {
		target *url.URL
		port   uint16
		ok     bool
	}{
		{target: &url.URL{Scheme: "http", Host: "example.test"}, port: 80, ok: true},
		{target: &url.URL{Scheme: "https", Host: "[::1]"}, port: 443, ok: true},
		{target: &url.URL{Scheme: "https", Host: "[::1"}},
		{target: &url.URL{Scheme: "https", Host: "[::1]bad"}},
		{target: &url.URL{Scheme: "https", Host: "::1"}},
		{target: &url.URL{Scheme: "https", Host: "example.test:bad"}},
		{target: &url.URL{Scheme: "ftp", Host: "example.test"}},
		{target: &url.URL{Scheme: "https", Host: "example.test:0"}},
	} {
		port, err := egressPort(test.target)
		if test.ok && (err != nil || port != test.port) || !test.ok && err == nil {
			t.Fatalf("port for %#v = %d, %v", test.target, port, err)
		}
	}
	otherOrigin, _ := NewEgressPolicy(EgressOptions{
		AllowedHosts:   []string{"api.example.test"},
		AllowedOrigins: []string{"https://api.example.test:444"},
	})
	target, _ := url.Parse("https://api.example.test/")
	if err := otherOrigin.ValidateURL(target); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("origin mismatch = %v", err)
	}
	if err := (*EgressPolicy)(nil).ValidateURL(target); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("nil URL policy = %v", err)
	}
	for _, address := range []net.IP{nil, net.IPv4zero} {
		if err := policy.ValidateIP(address); !errors.Is(err, ErrEgressDenied) {
			t.Fatalf("address %v error = %v", address, err)
		}
	}
	if err := (*EgressPolicy)(nil).ValidateIP(net.ParseIP("192.0.2.1")); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("nil IP policy = %v", err)
	}
	if err := policy.validateAuthority("bad_host", 443); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("malformed authority = %v", err)
	}
	if err := policy.validateAuthority("other.example.test", 443); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("host authority = %v", err)
	}
	if err := policy.validateAuthority("api.example.test", 444); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("port authority = %v", err)
	}

	transport := egressRoundTripper{policy: policy, next: TransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
	})}
	if _, err := (egressRoundTripper{}).RoundTrip(nil); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("invalid wrapper = %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.test/", nil)
	if response, err := transport.RoundTrip(request); err != nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("valid wrapper = %#v, %v", response, err)
	}
}

func TestEgressDialerRejectsMalformedAndPropagatesResolutionAndDialFailures(t *testing.T) {
	t.Parallel()

	resolutionFailure := errors.New("resolve")
	resolver := EgressResolverFunc(func(_ context.Context, network string, host string) ([]netip.Addr, error) {
		switch host {
		case "failure.example.test":
			return nil, resolutionFailure
		case "empty.example.test":
			return nil, nil
		default:
			if network != "ip4" && network != "ip6" {
				t.Fatalf("lookup network = %q", network)
			}
			return []netip.Addr{netip.MustParseAddr("192.0.2.30")}, nil
		}
	})
	policy, _ := NewEgressPolicy(EgressOptions{
		AllowedHosts: []string{
			"failure.example.test", "empty.example.test", "dial.example.test", "192.0.2.40",
		},
		Resolver: resolver,
	})
	dialFailure := errors.New("dial")
	dialer := &egressDialer{policy: policy, dialer: egressDialerFunc(func(context.Context, string, string) (net.Conn, error) {
		return nil, dialFailure
	})}
	for _, input := range []struct{ network, address string }{
		{"udp", "dial.example.test:443"},
		{"tcp", "missing-port"},
		{"tcp", "dial.example.test:bad"},
		{"tcp", "other.example.test:443"},
		{"tcp", "dial.example.test:444"},
	} {
		if _, err := dialer.DialContext(context.Background(), input.network, input.address); !errors.Is(err, ErrEgressDenied) {
			t.Fatalf("dial %q %q error = %v", input.network, input.address, err)
		}
	}
	if _, err := (*egressDialer)(nil).DialContext(context.Background(), "tcp", "host:443"); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("nil dialer error = %v", err)
	}
	if _, err := dialer.DialContext(context.Background(), "tcp", "failure.example.test:443"); !errors.Is(err, resolutionFailure) {
		t.Fatalf("resolution error = %v", err)
	}
	if _, err := dialer.DialContext(context.Background(), "tcp", "empty.example.test:443"); err == nil {
		t.Fatal("empty resolution succeeded")
	}
	for _, network := range []string{"tcp4", "tcp6"} {
		if _, err := dialer.DialContext(context.Background(), network, "dial.example.test:443"); !errors.Is(err, dialFailure) {
			t.Fatalf("%s dial error = %v", network, err)
		}
	}
	if _, err := dialer.DialContext(context.Background(), "tcp", "192.0.2.40:443"); !errors.Is(err, dialFailure) {
		t.Fatalf("literal dial error = %v", err)
	}
}

type egressDialerFunc func(context.Context, string, string) (net.Conn, error)

func (function egressDialerFunc) DialContext(
	ctx context.Context,
	network string,
	address string,
) (net.Conn, error) {
	return function(ctx, network, address)
}
