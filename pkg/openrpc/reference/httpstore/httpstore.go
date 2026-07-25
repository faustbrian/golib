// Package httpstore provides an optional, SSRF-resistant HTTP document store
// for the reference resolver. Core reference parsing never imports or invokes
// this package implicitly.
package httpstore

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrPolicy          = errors.New("reference httpstore: invalid policy")
	ErrURIDenied       = errors.New("reference httpstore: URI denied")
	ErrAddressDenied   = errors.New("reference httpstore: network address denied")
	ErrRedirect        = errors.New("reference httpstore: redirect denied")
	ErrRequest         = errors.New("reference httpstore: request failed")
	ErrHTTPStatus      = errors.New("reference httpstore: unexpected HTTP status")
	ErrContentEncoding = errors.New("reference httpstore: content encoding denied")
	ErrResponseLimit   = errors.New("reference httpstore: response limit exceeded")
)

// Policy explicitly authorizes HTTP targets and bounds transport behavior.
type Policy struct {
	AllowedHosts           []string
	AllowHTTP              bool
	AllowPrivateAddresses  bool
	MaxRedirects           int
	Timeout                time.Duration
	DialTimeout            time.Duration
	ResponseHeaderTimeout  time.Duration
	MaxResponseHeaderBytes int64
}

// DefaultPolicy permits only HTTPS, denies every host until allowlisted, and
// blocks private, loopback, link-local, multicast, and unspecified addresses.
func DefaultPolicy() Policy {
	return Policy{
		MaxRedirects:           3,
		Timeout:                15 * time.Second,
		DialTimeout:            5 * time.Second,
		ResponseHeaderTimeout:  5 * time.Second,
		MaxResponseHeaderBytes: 1 << 20,
	}
}

// Store is a bounded HTTP implementation of reference.Store.
type Store struct {
	client       *http.Client
	policy       Policy
	allowedHosts map[string]struct{}
}

// New constructs an isolated client with compression disabled, safe DNS/IP
// dialing, and bounded redirects. It starts no goroutines.
func New(policy Policy) (*Store, error) {
	if policy.MaxRedirects < 0 || policy.Timeout <= 0 || policy.DialTimeout <= 0 ||
		policy.ResponseHeaderTimeout <= 0 || policy.MaxResponseHeaderBytes <= 0 {
		return nil, ErrPolicy
	}
	policy.AllowedHosts = append([]string(nil), policy.AllowedHosts...)
	store := &Store{policy: policy, allowedHosts: make(map[string]struct{}, len(policy.AllowedHosts))}
	for _, host := range policy.AllowedHosts {
		host = strings.ToLower(strings.TrimSuffix(host, "."))
		if host == "" || strings.ContainsAny(host, "/:@") {
			return nil, ErrPolicy
		}
		store.allowedHosts[host] = struct{}{}
	}
	dialer := &net.Dialer{
		Timeout: policy.DialTimeout,
		// Use a literal duration so mutation testing does not manufacture an
		// equivalent multiplication of two compile-time constants.
		KeepAlive: time.Duration(30_000_000_000),
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = policy.ResponseHeaderTimeout
	transport.MaxResponseHeaderBytes = policy.MaxResponseHeaderBytes
	transport.DialContext = store.dialContext(dialer)
	store.client = &http.Client{
		Transport: transport,
		Timeout:   policy.Timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) > policy.MaxRedirects || store.authorize(request.URL) != nil {
				return ErrRedirect
			}
			return nil
		},
	}
	return store, nil
}

// Load fetches one identity-encoded JSON resource within maxBytes.
func (store *Store) Load(ctx context.Context, documentURI string, maxBytes int) ([]byte, error) {
	if store == nil || store.client == nil || ctx == nil || maxBytes <= 0 {
		return nil, ErrPolicy
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := url.Parse(documentURI)
	if err != nil || store.authorize(target) != nil {
		return nil, ErrURIDenied
	}
	// An authorized absolute HTTP(S) URL is always a valid client request URL.
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	response, err := store.client.Do(request)
	if err != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		switch {
		case errors.Is(err, ErrAddressDenied):
			return nil, ErrAddressDenied
		case errors.Is(err, ErrRedirect):
			return nil, ErrRedirect
		default:
			return nil, ErrRequest
		}
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return nil, ErrHTTPStatus
	}
	encoding := strings.TrimSpace(strings.ToLower(response.Header.Get("Content-Encoding")))
	if encoding != "" && encoding != "identity" {
		return nil, ErrContentEncoding
	}
	if response.ContentLength > int64(maxBytes) {
		return nil, ErrResponseLimit
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(maxBytes)+1))
	if err != nil {
		return nil, ErrRequest
	}
	if len(data) > maxBytes {
		return nil, ErrResponseLimit
	}
	return data, nil
}

func (store *Store) authorize(target *url.URL) error {
	schemeAllowed := target != nil && (target.Scheme == "https" ||
		(store.policy.AllowHTTP && target.Scheme == "http"))
	if target == nil || target.User != nil || target.Fragment != "" ||
		!schemeAllowed {
		return ErrURIDenied
	}
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	if host == "" {
		return ErrURIDenied
	}
	if _, allowed := store.allowedHosts[host]; !allowed {
		return ErrURIDenied
	}
	return nil
}

func (store *Store) dialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, ErrAddressDenied
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, ErrRequest
		}
		for _, address := range addresses {
			if !store.policy.AllowPrivateAddresses && deniedAddress(address.IP) {
				return nil, ErrAddressDenied
			}
		}
		for _, address := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
			if dialErr == nil {
				return connection, nil
			}
		}
		return nil, ErrRequest
	}
}

func deniedAddress(address net.IP) bool {
	return !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() ||
		address.IsMulticast() || address.IsUnspecified()
}
