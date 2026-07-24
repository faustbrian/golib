package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

var (
	// ErrInvalidEgressPolicy indicates malformed egress configuration.
	ErrInvalidEgressPolicy = errors.New("invalid HTTP egress policy")
	// ErrEgressDenied indicates that an outbound destination is not permitted.
	ErrEgressDenied = errors.New("HTTP egress destination denied")
)

// EgressReason is a stable low-cardinality denial category.
type EgressReason string

const (
	EgressReasonScheme    EgressReason = "scheme"
	EgressReasonHost      EgressReason = "host"
	EgressReasonPort      EgressReason = "port"
	EgressReasonOrigin    EgressReason = "origin"
	EgressReasonCIDR      EgressReason = "cidr"
	EgressReasonAddress   EgressReason = "address-class"
	EgressReasonMetadata  EgressReason = "metadata-service"
	EgressReasonMalformed EgressReason = "malformed"
)

// EgressError reports a destination denial without rendering host, path,
// query, credentials, or resolved addresses.
type EgressError struct {
	Reason EgressReason
}

// Error implements error.
func (*EgressError) Error() string { return "HTTP egress destination denied" }

// Unwrap returns the stable egress-denial sentinel.
func (*EgressError) Unwrap() error { return ErrEgressDenied }

// EgressOptions configures immutable outbound destination policy. Empty scheme
// and port lists default to HTTPS and port 443. Empty host, origin, and CIDR
// lists allow any value that passes the remaining checks.
type EgressOptions struct {
	AllowedSchemes       []string
	AllowedHosts         []string
	AllowedPorts         []uint16
	AllowedOrigins       []string
	AllowedCIDRs         []string
	DeniedCIDRs          []string
	AllowPrivate         bool
	AllowLoopback        bool
	AllowLinkLocal       bool
	AllowMulticast       bool
	AllowMetadataService bool
	Resolver             EgressResolver
}

// EgressResolver resolves every candidate address before a connection is
// attempted. Implementations must be safe for concurrent use.
type EgressResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

// EgressResolverFunc adapts a function to EgressResolver.
type EgressResolverFunc func(context.Context, string, string) ([]netip.Addr, error)

// LookupNetIP implements EgressResolver.
func (function EgressResolverFunc) LookupNetIP(
	ctx context.Context,
	network string,
	host string,
) ([]netip.Addr, error) {
	return function(ctx, network, host)
}

// EgressPolicy is an immutable URL and resolved-address policy.
type EgressPolicy struct {
	schemes        map[string]struct{}
	hosts          map[string]struct{}
	ports          map[uint16]struct{}
	origins        map[string]struct{}
	allowedCIDRs   []*net.IPNet
	deniedCIDRs    []*net.IPNet
	allowPrivate   bool
	allowLoopback  bool
	allowLinkLocal bool
	allowMulticast bool
	allowMetadata  bool
	resolver       EgressResolver
}

// NewEgressPolicy validates and snapshots outbound destination policy.
func NewEgressPolicy(options EgressOptions) (*EgressPolicy, error) {
	schemes := options.AllowedSchemes
	if len(schemes) == 0 {
		schemes = []string{"https"}
	}
	policy := &EgressPolicy{
		schemes:      make(map[string]struct{}, len(schemes)),
		hosts:        make(map[string]struct{}, len(options.AllowedHosts)),
		ports:        make(map[uint16]struct{}, max(len(options.AllowedPorts), 1)),
		origins:      make(map[string]struct{}, len(options.AllowedOrigins)),
		allowPrivate: options.AllowPrivate, allowLoopback: options.AllowLoopback,
		allowLinkLocal: options.AllowLinkLocal, allowMulticast: options.AllowMulticast,
		allowMetadata: options.AllowMetadataService,
	}
	if options.Resolver == nil {
		policy.resolver = net.DefaultResolver
	} else if nilLike(options.Resolver) {
		return nil, fmt.Errorf("%w: resolver is nil", ErrInvalidEgressPolicy)
	} else {
		policy.resolver = options.Resolver
	}
	for _, scheme := range schemes {
		normalized := strings.ToLower(strings.TrimSpace(scheme))
		if normalized != "http" && normalized != "https" {
			return nil, fmt.Errorf("%w: allowed scheme is unsupported", ErrInvalidEgressPolicy)
		}
		policy.schemes[normalized] = struct{}{}
	}
	ports := options.AllowedPorts
	if len(ports) == 0 {
		ports = []uint16{443}
	}
	for _, port := range ports {
		if port == 0 {
			return nil, fmt.Errorf("%w: allowed port is zero", ErrInvalidEgressPolicy)
		}
		policy.ports[port] = struct{}{}
	}
	for _, host := range options.AllowedHosts {
		normalized, err := normalizeEgressHost(host)
		if err != nil {
			return nil, err
		}
		policy.hosts[normalized] = struct{}{}
	}
	for _, rawOrigin := range options.AllowedOrigins {
		normalized, err := normalizeEgressOrigin(rawOrigin)
		if err != nil {
			return nil, err
		}
		policy.origins[normalized] = struct{}{}
	}
	var err error
	policy.allowedCIDRs, err = parseEgressCIDRs(options.AllowedCIDRs)
	if err != nil {
		return nil, err
	}
	policy.deniedCIDRs, err = parseEgressCIDRs(options.DeniedCIDRs)
	if err != nil {
		return nil, err
	}
	return policy, nil
}

func (policy *EgressPolicy) validateAuthority(host string, port uint16) error {
	normalized, err := normalizeEgressHost(host)
	if err != nil {
		return &EgressError{Reason: EgressReasonMalformed}
	}
	if len(policy.hosts) > 0 {
		if _, allowed := policy.hosts[normalized]; !allowed {
			return &EgressError{Reason: EgressReasonHost}
		}
	}
	if _, allowed := policy.ports[port]; !allowed {
		return &EgressError{Reason: EgressReasonPort}
	}
	return nil
}

// ValidateURL validates scheme, authority, origin, and literal IP addresses.
// DNS hostnames receive their address validation at connection time.
func (policy *EgressPolicy) ValidateURL(target *url.URL) error {
	if policy == nil || target == nil || target.User != nil || !target.IsAbs() || target.Host == "" {
		return &EgressError{Reason: EgressReasonMalformed}
	}
	scheme := strings.ToLower(target.Scheme)
	if _, allowed := policy.schemes[scheme]; !allowed {
		return &EgressError{Reason: EgressReasonScheme}
	}
	host, err := normalizeEgressHost(target.Hostname())
	if err != nil {
		return &EgressError{Reason: EgressReasonMalformed}
	}
	if len(policy.hosts) > 0 {
		if _, allowed := policy.hosts[host]; !allowed {
			return &EgressError{Reason: EgressReasonHost}
		}
	}
	port, err := egressPort(target)
	if err != nil {
		return &EgressError{Reason: EgressReasonPort}
	}
	if _, allowed := policy.ports[port]; !allowed {
		return &EgressError{Reason: EgressReasonPort}
	}
	if len(policy.origins) > 0 {
		origin := scheme + "://" + net.JoinHostPort(host, strconv.Itoa(int(port)))
		if _, allowed := policy.origins[origin]; !allowed {
			return &EgressError{Reason: EgressReasonOrigin}
		}
	}
	if address := net.ParseIP(host); address != nil {
		return policy.ValidateIP(address)
	}
	return nil
}

// ValidateIP validates one address resolved for an outbound connection.
func (policy *EgressPolicy) ValidateIP(address net.IP) error {
	if policy == nil || address == nil || address.IsUnspecified() {
		return &EgressError{Reason: EgressReasonAddress}
	}
	for _, network := range policy.deniedCIDRs {
		if network.Contains(address) {
			return &EgressError{Reason: EgressReasonCIDR}
		}
	}
	if len(policy.allowedCIDRs) > 0 {
		allowed := false
		for _, network := range policy.allowedCIDRs {
			if network.Contains(address) {
				allowed = true
				break
			}
		}
		if !allowed {
			return &EgressError{Reason: EgressReasonCIDR}
		}
	}
	if metadataServiceAddress(address) {
		if !policy.allowMetadata {
			return &EgressError{Reason: EgressReasonMetadata}
		}
		return nil
	}
	if address.IsLoopback() && !policy.allowLoopback ||
		address.IsPrivate() && !policy.allowPrivate ||
		(address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast()) && !policy.allowLinkLocal ||
		address.IsMulticast() && !policy.allowMulticast {
		return &EgressError{Reason: EgressReasonAddress}
	}
	return nil
}

func normalizeEgressHost(host string) (string, error) {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if host == "" || strings.ContainsAny(host, "/@") {
		return "", fmt.Errorf("%w: allowed host is malformed", ErrInvalidEgressPolicy)
	}
	if address := net.ParseIP(host); address != nil {
		return address.String(), nil
	}
	normalized := strings.ToLower(host)
	if !validEgressDNSName(normalized) {
		return "", fmt.Errorf("%w: allowed host is malformed", ErrInvalidEgressPolicy)
	}
	return normalized, nil
}

func validEgressDNSName(host string) bool {
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for index := 0; index < len(label); index++ {
			character := label[index]
			if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func normalizeEgressOrigin(raw string) (string, error) {
	origin, err := url.Parse(raw)
	if err != nil || origin.User != nil || !origin.IsAbs() || origin.Host == "" ||
		(origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.Fragment != "" {
		return "", fmt.Errorf("%w: allowed origin is malformed", ErrInvalidEgressPolicy)
	}
	if origin.Scheme != "http" && origin.Scheme != "https" {
		return "", fmt.Errorf("%w: allowed origin scheme is unsupported", ErrInvalidEgressPolicy)
	}
	host, err := normalizeEgressHost(origin.Hostname())
	if err != nil {
		return "", err
	}
	port, _ := egressPort(origin)
	return strings.ToLower(origin.Scheme) + "://" + net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

func egressPort(target *url.URL) (uint16, error) {
	authority := target.Host
	if strings.HasPrefix(authority, "[") {
		closing := strings.LastIndex(authority, "]")
		if closing < 0 || authority[closing+1:] != "" && !strings.HasPrefix(authority[closing+1:], ":") {
			return 0, errors.New("invalid port")
		}
	} else if strings.Count(authority, ":") > 1 {
		return 0, errors.New("invalid port")
	}
	value := target.Port()
	if value == "" {
		if !strings.HasPrefix(authority, "[") && strings.Contains(authority, ":") {
			return 0, errors.New("invalid port")
		}
		switch strings.ToLower(target.Scheme) {
		case "http":
			return 80, nil
		case "https":
			return 443, nil
		default:
			return 0, errors.New("unknown scheme")
		}
	}
	port, err := strconv.ParseUint(value, 10, 16)
	if err != nil || port == 0 {
		return 0, errors.New("invalid port")
	}
	return uint16(port), nil
}

func parseEgressCIDRs(values []string) ([]*net.IPNet, error) {
	result := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("%w: CIDR is malformed", ErrInvalidEgressPolicy)
		}
		result = append(result, network)
	}
	return result, nil
}

func metadataServiceAddress(address net.IP) bool {
	for _, value := range []string{"169.254.169.254", "169.254.170.2", "100.100.100.200", "fd00:ec2::254"} {
		if address.Equal(net.ParseIP(value)) {
			return true
		}
	}
	return false
}
