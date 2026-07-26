package webhook

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NetIPResolver is implemented by net.Resolver and controlled test resolvers.
type NetIPResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// SSRFPolicyConfig configures default-deny endpoint validation. AllowedPrefixes
// are explicit exceptions for private test or trusted network endpoints.
type SSRFPolicyConfig struct {
	Resolver        NetIPResolver
	AllowHTTP       bool
	AllowedPrefixes []netip.Prefix
	DeniedPrefixes  []netip.Prefix
	MaxAddresses    int
}

// SSRFPolicy validates URL syntax and every resolved address.
type SSRFPolicy struct {
	resolver     NetIPResolver
	allowHTTP    bool
	allowed      []netip.Prefix
	denied       []netip.Prefix
	maxAddresses int
}

var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// NewSSRFPolicy constructs a policy with explicit resolver and DNS bounds.
func NewSSRFPolicy(config SSRFPolicyConfig) (*SSRFPolicy, error) {
	if config.Resolver == nil || config.MaxAddresses <= 0 {
		return nil, fmt.Errorf("%w: resolver and positive address limit are required", ErrInvalidConfiguration)
	}
	allowed, err := copyPrefixes(config.AllowedPrefixes)
	if err != nil {
		return nil, err
	}
	denied, err := copyPrefixes(config.DeniedPrefixes)
	if err != nil {
		return nil, err
	}

	return &SSRFPolicy{
		resolver:     config.Resolver,
		allowHTTP:    config.AllowHTTP,
		allowed:      allowed,
		denied:       denied,
		maxAddresses: config.MaxAddresses,
	}, nil
}

func copyPrefixes(prefixes []netip.Prefix) ([]netip.Prefix, error) {
	result := make([]netip.Prefix, len(prefixes))
	for index, prefix := range prefixes {
		if !prefix.IsValid() {
			return nil, fmt.Errorf("%w: invalid address prefix", ErrInvalidConfiguration)
		}
		result[index] = prefix.Masked()
	}

	return result, nil
}

// Validate implements EndpointPolicy and resolves hostnames immediately.
func (p *SSRFPolicy) Validate(ctx context.Context, endpoint *url.URL) error {
	if endpoint == nil || !endpoint.IsAbs() || endpoint.Opaque != "" || endpoint.Host == "" {
		return fmt.Errorf("%w: absolute hierarchical URL required", ErrEndpointRejected)
	}
	schemeAllowed := endpoint.Scheme == "https" ||
		(p.allowHTTP && endpoint.Scheme == "http")
	if !schemeAllowed {
		return fmt.Errorf("%w: URL scheme denied", ErrEndpointRejected)
	}
	if endpoint.User != nil || endpoint.Fragment != "" {
		return fmt.Errorf("%w: userinfo and fragments are denied", ErrEndpointRejected)
	}
	host := endpoint.Hostname()
	if host == "" || strings.HasSuffix(host, ".") || !asciiHost(host) {
		return fmt.Errorf("%w: hostname is not canonical", ErrEndpointRejected)
	}
	if port := endpoint.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return fmt.Errorf("%w: invalid endpoint port", ErrEndpointRejected)
		}
	}

	_, err := p.resolveAndValidate(ctx, host)

	return err
}

func asciiHost(host string) bool {
	for _, value := range host {
		if value > 127 {
			return false
		}
	}

	return true
}

func (p *SSRFPolicy) resolveAndValidate(ctx context.Context, host string) ([]netip.Addr, error) {
	if literal, err := netip.ParseAddr(host); err == nil {
		literal = literal.Unmap()
		if err := p.validateAddress(literal); err != nil {
			return nil, err
		}

		return []netip.Addr{literal}, nil
	}
	addresses, err := p.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 || len(addresses) > p.maxAddresses {
		return nil, fmt.Errorf("%w: DNS answer unavailable or outside limits", ErrEndpointRejected)
	}
	validated := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if err := p.validateAddress(address); err != nil {
			return nil, err
		}
		validated = append(validated, address)
	}

	return validated, nil
}

func (p *SSRFPolicy) validateAddress(address netip.Addr) error {
	if !address.IsValid() {
		return fmt.Errorf("%w: invalid address", ErrEndpointRejected)
	}
	for _, prefix := range p.denied {
		if prefix.Contains(address) {
			return fmt.Errorf("%w: address explicitly denied", ErrEndpointRejected)
		}
	}
	for _, prefix := range p.allowed {
		if prefix.Contains(address) {
			return nil
		}
	}
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return fmt.Errorf("%w: non-public address denied", ErrEndpointRejected)
	}
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(address) {
			return fmt.Errorf("%w: reserved address denied", ErrEndpointRejected)
		}
	}

	return nil
}

// NewSecureHTTPClient creates a direct, no-proxy client whose transport
// re-resolves and revalidates addresses at dial time. Redirects are returned to
// the caller without being followed.
func NewSecureHTTPClient(policy *SSRFPolicy, timeout time.Duration) (*http.Client, error) {
	if policy == nil || timeout <= 0 {
		return nil, fmt.Errorf("%w: policy and positive timeout are required", ErrInvalidConfiguration)
	}
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid dial address", ErrEndpointRejected)
		}
		addresses, err := policy.resolveAndValidate(ctx, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, resolved := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}

		return nil, fmt.Errorf("secure dial failed: %w", lastErr)
	}

	return &http.Client{
		Transport: &policyTransport{policy: policy, next: transport},
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

type policyTransport struct {
	policy *SSRFPolicy
	next   http.RoundTripper
}

func (t *policyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, fmt.Errorf("%w: request is required", ErrEndpointRejected)
	}
	if err := t.policy.Validate(request.Context(), request.URL); err != nil {
		return nil, err
	}

	return t.next.RoundTrip(request)
}
