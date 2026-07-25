package httpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

type egressRoundTripper struct {
	policy *EgressPolicy
	next   http.RoundTripper
}

func (transport egressRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || transport.policy == nil || transport.next == nil {
		return nil, &EgressError{Reason: EgressReasonMalformed}
	}
	if err := transport.policy.ValidateURL(request.URL); err != nil {
		return nil, err
	}
	return transport.next.RoundTrip(request)
}

type egressContextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type egressDialer struct {
	dialer egressContextDialer
	policy *EgressPolicy
}

func (dialer *egressDialer) DialContext(
	ctx context.Context,
	network string,
	address string,
) (net.Conn, error) {
	if dialer == nil || dialer.dialer == nil || dialer.policy == nil ||
		(network != "tcp" && network != "tcp4" && network != "tcp6") {
		return nil, &EgressError{Reason: EgressReasonMalformed}
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, &EgressError{Reason: EgressReasonMalformed}
	}
	portValue, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || portValue == 0 {
		return nil, &EgressError{Reason: EgressReasonPort}
	}
	port := uint16(portValue)
	if err := dialer.policy.validateAuthority(host, port); err != nil {
		return nil, err
	}

	addresses, err := dialer.resolve(ctx, network, host)
	if err != nil {
		return nil, err
	}
	for _, resolved := range addresses {
		if err := dialer.policy.ValidateIP(net.IP(resolved.AsSlice())); err != nil {
			return nil, err
		}
	}

	var result error
	for _, resolved := range addresses {
		connection, err := dialer.dialer.DialContext(
			ctx, network, net.JoinHostPort(resolved.String(), portText),
		)
		if err == nil {
			return connection, nil
		}
		result = errors.Join(result, err)
	}
	return nil, result
}

func (dialer *egressDialer) resolve(
	ctx context.Context,
	network string,
	host string,
) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return []netip.Addr{address}, nil
	}
	lookupNetwork := "ip"
	switch network {
	case "tcp4":
		lookupNetwork = "ip4"
	case "tcp6":
		lookupNetwork = "ip6"
	}
	addresses, err := dialer.policy.resolver.LookupNetIP(ctx, lookupNetwork, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, &net.DNSError{Name: host, Err: "no addresses"}
	}
	return addresses, nil
}

var _ http.RoundTripper = egressRoundTripper{}
