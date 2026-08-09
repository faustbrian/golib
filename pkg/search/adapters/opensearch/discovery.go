package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

var (
	// ErrDiscoveryDisabled identifies an explicit topology refresh without an
	// allowlisted discovery policy.
	ErrDiscoveryDisabled = errors.New("search/opensearch: discovery is disabled")
	// ErrDiscoveryRejected identifies malformed, empty, oversized, or untrusted
	// topology. The existing endpoint set remains unchanged.
	ErrDiscoveryRejected = errors.New("search/opensearch: discovered topology was rejected")
)

// DiscoveryResult reports bounded topology counts without exposing endpoint
// labels to metrics or logs.
type DiscoveryResult struct {
	Discovered int
	Excluded   int
}

// Discover replaces the active endpoint set atomically after every eligible
// node passes the configured trust policy. It performs one request and owns no
// timer or background goroutine.
func (c *Client) Discover(ctx context.Context) (discoveryResult DiscoveryResult, err error) {
	if ctx == nil {
		return DiscoveryResult{}, ErrContextRequired
	}
	if err := c.begin(); err != nil {
		return DiscoveryResult{}, err
	}
	if c.discovery.MaximumNodes == 0 {
		return DiscoveryResult{}, ErrDiscoveryDisabled
	}
	requestCtx, cancel := context.WithTimeout(withOperation(ctx, OperationLifecycle), c.timeout)
	defer cancel()
	request := (&http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: "/_nodes/http"},
		Header: make(http.Header),
	}).WithContext(requestCtx)
	response, transportErr := c.client.Stream(request)
	if response == nil || transportErr != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}

		return DiscoveryResult{}, ErrTransport
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return DiscoveryResult{}, &statusError{status: response.StatusCode}
	}
	body, err := readBounded(response.Body, c.maximumResponseBytes)
	if err != nil {
		return DiscoveryResult{}, ErrDiscoveryRejected
	}

	var payload struct {
		Nodes map[string]struct {
			Roles []string `json:"roles"`
			HTTP  *struct {
				PublishAddress string `json:"publish_address"`
			} `json:"http"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return DiscoveryResult{}, ErrDiscoveryRejected
	}
	if len(payload.Nodes) == 0 {
		return DiscoveryResult{}, ErrDiscoveryRejected
	}
	identifiers := make([]string, 0, len(payload.Nodes))
	for identifier := range payload.Nodes {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)

	result := DiscoveryResult{}
	endpoints := make([]*url.URL, 0, len(payload.Nodes))
	for _, identifier := range identifiers {
		node := payload.Nodes[identifier]
		if node.HTTP == nil || dedicatedClusterManager(node.Roles) {
			result.Excluded++
			continue
		}
		endpoint, err := c.discoveredEndpoint(node.HTTP.PublishAddress)
		if err != nil {
			return DiscoveryResult{}, ErrDiscoveryRejected
		}
		endpoints = append(endpoints, endpoint)
		if len(endpoints) > c.discovery.MaximumNodes {
			return DiscoveryResult{}, ErrDiscoveryRejected
		}
	}
	if len(endpoints) == 0 {
		return DiscoveryResult{}, ErrDiscoveryRejected
	}
	result.Discovered = len(endpoints)
	c.transport.replaceEndpoints(endpoints)

	return result, nil
}

func dedicatedClusterManager(roles []string) bool {
	if len(roles) != 1 {
		return false
	}

	return roles[0] == "cluster_manager" || roles[0] == "master"
}

func (c *Client) discoveredEndpoint(publishAddress string) (*url.URL, error) {
	if prefix, address, found := strings.Cut(publishAddress, "/"); found {
		if prefix == "" {
			return nil, ErrDiscoveryRejected
		}
		publishAddress = address
	}
	if publishAddress == "" || strings.ContainsAny(publishAddress, "@/?#\x00\r\n") {
		return nil, ErrDiscoveryRejected
	}
	host, port, err := net.SplitHostPort(publishAddress)
	if err != nil {
		return nil, ErrDiscoveryRejected
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, ErrDiscoveryRejected
	}
	host = strings.Trim(host, "[]")
	if !c.discoveryAllows(host) {
		return nil, ErrDiscoveryRejected
	}

	return &url.URL{
		Scheme: c.transport.endpointScheme(),
		Host:   net.JoinHostPort(host, port),
	}, nil
}

func (c *Client) discoveryAllows(host string) bool {
	if address, err := netip.ParseAddr(host); err == nil {
		for _, prefix := range c.discovery.AllowedCIDRs {
			if prefix.Contains(address) {
				return true
			}
		}

		return false
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, suffix := range c.discovery.AllowedDNSSuffixes {
		if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return true
		}
	}

	return false
}
