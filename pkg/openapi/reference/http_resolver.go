package reference

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/idna"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
)

// HTTPResolverOptions defines the complete authority, transport policy, and
// resource budget of an HTTP resolver. Host names are exact matches; wildcard
// authorization is intentionally unsupported. AllowedCIDRs grants explicit
// access to otherwise denied private or special-purpose address ranges.
type HTTPResolverOptions struct {
	AllowedSchemes         []string
	AllowedHosts           []string
	AllowedPorts           []int
	AllowedCIDRs           []string
	MaxBytes               int64
	MaxDocuments           int
	MaxRedirects           int
	MaxConcurrency         int
	MaxAddresses           int
	MaxResponseHeaderBytes int64
	Timeout                time.Duration
	ParseLimits            parse.Limits
}

// DefaultHTTPResolverOptions returns conservative resource and TLS defaults
// but no allowed hosts. Callers must explicitly grant every remote host.
func DefaultHTTPResolverOptions() HTTPResolverOptions {
	return HTTPResolverOptions{
		AllowedSchemes:         []string{"https"},
		AllowedPorts:           []int{443},
		MaxBytes:               16_777_216,
		MaxDocuments:           128,
		MaxRedirects:           4,
		MaxConcurrency:         8,
		MaxAddresses:           16,
		MaxResponseHeaderBytes: 1_048_576,
		Timeout:                10 * time.Second,
		ParseLimits:            parse.DefaultLimits(),
	}
}

// NewHTTPResolver constructs a concurrency-safe remote resolver. It disables
// environment proxies and transparent decompression, validates every redirect,
// resolves and approves every dial address before connecting, and performs no
// caching. The document budget is cumulative, so callers should own one
// resolver per bounded operation.
func NewHTTPResolver(options HTTPResolverOptions) (*HTTPResolver, error) {
	policy, err := newHTTPPolicy(options)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            policy.dialContext,
		DisableCompression:     true,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           options.MaxConcurrency,
		MaxIdleConnsPerHost:    options.MaxConcurrency,
		MaxConnsPerHost:        options.MaxConcurrency,
		IdleConnTimeout:        options.Timeout,
		ResponseHeaderTimeout:  options.Timeout,
		TLSHandshakeTimeout:    options.Timeout,
		ExpectContinueTimeout:  options.Timeout,
		MaxResponseHeaderBytes: options.MaxResponseHeaderBytes,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	resolver := &HTTPResolver{
		policy:       policy,
		transport:    transport,
		semaphore:    make(chan struct{}, options.MaxConcurrency),
		maxBytes:     options.MaxBytes,
		maxDocuments: options.MaxDocuments,
		maxRedirects: options.MaxRedirects,
		timeout:      options.Timeout,
		parseLimits:  policy.parseLimits,
		createRequest: func(ctx context.Context, identifier string) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, identifier, nil)
		},
	}
	resolver.client = &http.Client{
		Transport:     transport,
		CheckRedirect: resolver.checkRedirect,
	}
	return resolver, nil
}

// HTTPResolver resolves explicitly authorized HTTP resources. It is safe for
// concurrent use. Call CloseIdleConnections when an operation no longer needs
// the resolver so retained transport connections are released promptly.
type HTTPResolver struct {
	policy        *httpPolicy
	transport     *http.Transport
	client        *http.Client
	semaphore     chan struct{}
	maxBytes      int64
	maxDocuments  int
	maxRedirects  int
	timeout       time.Duration
	parseLimits   parse.Limits
	createRequest func(context.Context, string) (*http.Request, error)

	mu        sync.Mutex
	documents int
}

// Resolve retrieves and parses one authorized JSON or YAML resource.
func (resolver *HTTPResolver) Resolve(
	ctx context.Context,
	identifier string,
) (Resource, error) {
	switch ctx {
	case nil:
		return Resource{}, errors.New("HTTP resolver: nil context")
	}
	err := ctx.Err()
	switch err {
	case nil:
	default:
		return Resource{}, err
	}
	resourceURL, err := resolver.policy.validateURL(identifier)
	if err != nil {
		return Resource{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, resolver.timeout)
	defer cancel()
	select {
	case resolver.semaphore <- struct{}{}:
		defer func() { <-resolver.semaphore }()
	case <-ctx.Done():
		return Resource{}, ctx.Err()
	}
	err = resolver.reserveDocument()
	switch err {
	case nil:
	default:
		return Resource{}, err
	}

	request, err := resolver.createRequest(ctx, resourceURL.String())
	if err != nil {
		return Resource{}, fmt.Errorf("HTTP resolver: create request: %w", ErrResourceAccess)
	}
	request.Header.Set("Accept", "application/json, application/yaml, text/yaml")
	response, err := resolver.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Resource{}, ctxErr
		}
		return Resource{}, fmt.Errorf("HTTP resolver: fetch resource: %w", httpAccessError(err))
	}
	defer func() { _ = response.Body.Close() }()
	if !successfulHTTPStatus(response.StatusCode) {
		return Resource{}, fmt.Errorf("HTTP resolver: %w: response status", ErrResourceDenied)
	}
	encoding := strings.TrimSpace(response.Header.Get("Content-Encoding"))
	if encoding != "" && !strings.EqualFold(encoding, "identity") {
		return Resource{}, fmt.Errorf("HTTP resolver: %w: encoded response", ErrResourceDenied)
	}
	if contentLengthExceeds(response.ContentLength, resolver.maxBytes) {
		return Resource{}, fmt.Errorf("HTTP resolver: %w", parse.ErrLimitExceeded)
	}
	format, err := responseFormat(response)
	if err != nil {
		return Resource{}, err
	}
	var root jsonvalue.Value
	if format == resourceJSON {
		root, err = parse.JSON(ctx, response.Body, resolver.parseLimits)
	} else {
		root, err = parse.YAML(ctx, response.Body, resolver.parseLimits)
	}
	if err != nil {
		return Resource{}, fmt.Errorf("HTTP resolver: parse resource: %w", err)
	}
	retrieval := response.Request.URL.String()
	return Resource{
		RetrievalURI: retrieval,
		CanonicalURI: retrieval,
		Root:         root,
	}, nil
}

func httpAccessError(err error) error {
	for _, classification := range []error{ErrResourceDenied, ErrResourceLimitExceeded} {
		if errors.Is(err, classification) {
			return classification
		}
	}
	return ErrResourceAccess
}

func successfulHTTPStatus(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

func contentLengthExceeds(length int64, maximum int64) bool {
	return length > maximum
}

// CloseIdleConnections closes retained keep-alive connections. Active calls
// remain governed by their contexts and the resolver timeout.
func (resolver *HTTPResolver) CloseIdleConnections() {
	resolver.transport.CloseIdleConnections()
}

func (resolver *HTTPResolver) checkRedirect(
	request *http.Request,
	via []*http.Request,
) error {
	if len(via) > resolver.maxRedirects {
		return fmt.Errorf("HTTP resolver: %w: redirect count", ErrResourceLimitExceeded)
	}
	_, err := resolver.policy.validateURL(request.URL.String())
	switch err {
	case nil:
	default:
		return err
	}
	request.Header.Del("Authorization")
	request.Header.Del("Cookie")
	request.Header.Del("Proxy-Authorization")
	return nil
}

func (resolver *HTTPResolver) reserveDocument() error {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	switch cmp.Compare(resolver.documents, resolver.maxDocuments) {
	case 0, 1:
		return fmt.Errorf("HTTP resolver: %w: document count", ErrResourceLimitExceeded)
	}
	resolver.documents++
	return nil
}

type httpPolicy struct {
	schemes      map[string]struct{}
	hosts        map[string]struct{}
	ports        map[int]struct{}
	allowedCIDRs []netip.Prefix
	maxAddresses int
	parseLimits  parse.Limits
	dial         func(context.Context, string, string) (net.Conn, error)
}

func newHTTPPolicy(options HTTPResolverOptions) (*httpPolicy, error) {
	if len(options.AllowedSchemes) == 0 || len(options.AllowedHosts) == 0 ||
		len(options.AllowedPorts) == 0 {
		return nil, fmt.Errorf("HTTP resolver: %w: empty authority allowlist", ErrResourceDenied)
	}
	if options.MaxBytes < 1 || options.MaxBytes == math.MaxInt64 ||
		options.MaxDocuments < 1 || options.MaxRedirects < 0 ||
		options.MaxConcurrency < 1 || options.MaxAddresses < 1 ||
		options.MaxResponseHeaderBytes < 1 || options.Timeout <= 0 {
		return nil, fmt.Errorf("HTTP resolver: %w: invalid limits", ErrResourceLimitExceeded)
	}
	if err := validParseLimits(options.ParseLimits); err != nil {
		return nil, err
	}

	policy := &httpPolicy{
		schemes:      make(map[string]struct{}, len(options.AllowedSchemes)),
		hosts:        make(map[string]struct{}, len(options.AllowedHosts)),
		ports:        make(map[int]struct{}, len(options.AllowedPorts)),
		allowedCIDRs: make([]netip.Prefix, 0, len(options.AllowedCIDRs)),
		maxAddresses: options.MaxAddresses,
		parseLimits:  options.ParseLimits,
		dial:         (&net.Dialer{}).DialContext,
	}
	policy.parseLimits.MaxBytes = min(options.MaxBytes, policy.parseLimits.MaxBytes)
	for _, scheme := range options.AllowedSchemes {
		normalized := strings.ToLower(scheme)
		if normalized != "http" && normalized != "https" {
			return nil, fmt.Errorf("HTTP resolver: %w: invalid scheme", ErrResourceDenied)
		}
		policy.schemes[normalized] = struct{}{}
	}
	for _, host := range options.AllowedHosts {
		normalized, err := normalizeHost(host)
		switch err {
		case nil:
		default:
			return nil, fmt.Errorf("HTTP resolver: %w: invalid host", ErrResourceDenied)
		}
		policy.hosts[normalized] = struct{}{}
	}
	for _, port := range options.AllowedPorts {
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("HTTP resolver: %w: invalid port", ErrResourceDenied)
		}
		policy.ports[port] = struct{}{}
	}
	for _, raw := range options.AllowedCIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("HTTP resolver: %w: invalid network", ErrResourceDenied)
		}
		policy.allowedCIDRs = append(policy.allowedCIDRs, prefix.Masked())
	}
	return policy, nil
}

func (policy *httpPolicy) validateURL(identifier string) (*url.URL, error) {
	parsed, err := url.Parse(identifier)
	switch err {
	case nil:
	default:
		return nil, fmt.Errorf("HTTP resolver: %w", ErrResourceDenied)
	}
	switch parsed.Opaque {
	case "":
	default:
		return nil, fmt.Errorf("HTTP resolver: %w", ErrResourceDenied)
	}
	switch parsed.User {
	case nil:
	default:
		return nil, fmt.Errorf("HTTP resolver: %w", ErrResourceDenied)
	}
	switch parsed.RawQuery {
	case "":
	default:
		return nil, fmt.Errorf("HTTP resolver: %w", ErrResourceDenied)
	}
	switch parsed.Fragment {
	case "":
	default:
		return nil, fmt.Errorf("HTTP resolver: %w", ErrResourceDenied)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if _, allowed := policy.schemes[scheme]; !allowed {
		return nil, fmt.Errorf("HTTP resolver: %w: scheme", ErrResourceDenied)
	}
	host, err := normalizeHost(parsed.Hostname())
	switch err {
	case nil:
	default:
		return nil, fmt.Errorf("HTTP resolver: %w: host", ErrResourceDenied)
	}
	if _, allowed := policy.hosts[host]; !allowed {
		return nil, fmt.Errorf("HTTP resolver: %w: host", ErrResourceDenied)
	}
	port, err := effectivePort(parsed)
	switch err {
	case nil:
	default:
		return nil, fmt.Errorf("HTTP resolver: %w: port", ErrResourceDenied)
	}
	if _, allowed := policy.ports[port]; !allowed {
		return nil, fmt.Errorf("HTTP resolver: %w: port", ErrResourceDenied)
	}
	return parsed, nil
}

func (policy *httpPolicy) dialContext(
	ctx context.Context,
	network string,
	address string,
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	switch err {
	case nil:
	default:
		return nil, fmt.Errorf("HTTP resolver: %w: dial authority", ErrResourceDenied)
	}
	parsedPort, err := strconv.Atoi(port)
	switch err {
	case nil:
	default:
		return nil, fmt.Errorf("HTTP resolver: %w: dial port", ErrResourceDenied)
	}
	if _, allowed := policy.ports[parsedPort]; !allowed {
		return nil, fmt.Errorf("HTTP resolver: %w: dial port", ErrResourceDenied)
	}
	addresses, err := resolveAddresses(ctx, host)
	switch err {
	case nil:
	default:
		return nil, fmt.Errorf("HTTP resolver: resolve host: %w", err)
	}
	if !validAddressCount(len(addresses), policy.maxAddresses) {
		return nil, fmt.Errorf("HTTP resolver: %w: address count", ErrResourceLimitExceeded)
	}
	for _, candidate := range addresses {
		if !policy.addressAllowed(candidate) {
			return nil, fmt.Errorf("HTTP resolver: %w: resolved address", ErrResourceDenied)
		}
	}

	var failures []error
	for _, candidate := range addresses {
		connection, dialErr := policy.dial(
			ctx,
			network,
			net.JoinHostPort(candidate.String(), port),
		)
		switch dialErr {
		case nil:
			return connection, nil
		}
		failures = append(failures, dialErr)
	}
	return nil, fmt.Errorf("HTTP resolver: connect: %w", errors.Join(failures...))
}

func validAddressCount(count int, maximum int) bool {
	switch count {
	case 0:
		return false
	}
	switch cmp.Compare(count, maximum) {
	case 1:
		return false
	default:
		return true
	}
}

func (policy *httpPolicy) addressAllowed(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range policy.allowedCIDRs {
		if prefix.Contains(address) {
			return true
		}
	}
	return address.IsGlobalUnicast() && !address.IsPrivate() &&
		!address.IsLoopback() && !address.IsLinkLocalUnicast() &&
		!address.IsLinkLocalMulticast() && !address.IsUnspecified() &&
		!specialPurposeAddress(address)
}

func specialPurposeAddress(address netip.Addr) bool {
	for _, prefix := range specialPurposePrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

var specialPurposePrefixes = [...]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
}

func resolveAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	address, err := netip.ParseAddr(host)
	switch err {
	case nil:
		return []netip.Addr{address}, nil
	}
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func normalizeHost(host string) (string, error) {
	switch host {
	case "":
		return "", errors.New("empty host")
	}
	address, err := netip.ParseAddr(host)
	switch err {
	case nil:
		return address.Unmap().String(), nil
	}
	ascii, err := idna.Lookup.ToASCII(strings.TrimSuffix(host, "."))
	switch err {
	case nil:
	default:
		return "", errors.New("invalid host")
	}
	switch ascii {
	case "":
		return "", errors.New("invalid host")
	}
	return strings.ToLower(ascii), nil
}

func effectivePort(resourceURL *url.URL) (int, error) {
	switch raw := resourceURL.Port(); raw {
	case "":
	default:
		return strconv.Atoi(raw)
	}
	switch strings.ToLower(resourceURL.Scheme) {
	case "http":
		return 80, nil
	case "https":
		return 443, nil
	default:
		return 0, errors.New("unknown scheme")
	}
}

type resourceFormat uint8

const (
	resourceUnknown resourceFormat = iota
	resourceJSON
	resourceYAML
)

func responseFormat(response *http.Response) (resourceFormat, error) {
	contentType := strings.TrimSpace(response.Header.Get("Content-Type"))
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			return resourceUnknown, fmt.Errorf("HTTP resolver: %w", ErrUnsupportedResourceFormat)
		}
		mediaType = strings.ToLower(mediaType)
		switch mediaType {
		case "application/json":
			return resourceJSON, nil
		case "application/yaml", "application/x-yaml", "text/yaml":
			return resourceYAML, nil
		}
		if strings.HasSuffix(mediaType, "+json") {
			return resourceJSON, nil
		}
		return resourceUnknown, fmt.Errorf("HTTP resolver: %w", ErrUnsupportedResourceFormat)
	}
	switch strings.ToLower(path.Ext(response.Request.URL.Path)) {
	case ".json":
		return resourceJSON, nil
	case ".yaml", ".yml":
		return resourceYAML, nil
	default:
		return resourceUnknown, fmt.Errorf("HTTP resolver: %w", ErrUnsupportedResourceFormat)
	}
}
