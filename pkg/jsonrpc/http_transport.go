package jsonrpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// HTTP transport errors distinguish status, content-type, and body-limit
// failures and support errors.Is through direct return or wrapping.
var (
	ErrHTTPStatus       = errors.New("jsonrpc: unexpected HTTP status")
	ErrHTTPContentType  = errors.New("jsonrpc: invalid HTTP response content type")
	ErrResponseTooLarge = errors.New("jsonrpc: HTTP response too large")
)

const defaultMaxResponseBytes int64 = 4 << 20

var defaultHTTPClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// HTTPStatusError reports a non-200 HTTP response and its bounded body.
type HTTPStatusError struct {
	// StatusCode is the peer's HTTP response status.
	StatusCode int
	// Body is the trimmed, bounded response body.
	Body string
}

// Error returns the status code and, when present, response body.
func (err *HTTPStatusError) Error() string {
	if err.Body == "" {
		return fmt.Sprintf("%s: %d", ErrHTTPStatus, err.StatusCode)
	}
	return fmt.Sprintf("%s: %d: %s", ErrHTTPStatus, err.StatusCode, err.Body)
}

// Unwrap returns ErrHTTPStatus.
func (err *HTTPStatusError) Unwrap() error { return ErrHTTPStatus }

// HTTPTransportOption configures an HTTPTransport during construction.
type HTTPTransportOption func(*HTTPTransport)

// WithHTTPClient installs a non-nil HTTP client. Its timeout and redirect
// policy remain caller-owned.
func WithHTTPClient(client *http.Client) HTTPTransportOption {
	return func(transport *HTTPTransport) {
		if client != nil {
			transport.client = client
		}
	}
}

// WithHTTPHeader adds a header to each request. Content-Type and Accept are
// always overwritten with JSON values during RoundTrip.
func WithHTTPHeader(name, value string) HTTPTransportOption {
	return func(transport *HTTPTransport) { transport.headers.Set(name, value) }
}

// WithMaxResponseBytes changes the default four-MiB HTTP response-body limit.
func WithMaxResponseBytes(limit int64) HTTPTransportOption {
	return func(transport *HTTPTransport) {
		if limit > 0 {
			transport.maxResponseBytes = limit
		}
	}
}

// HTTPTransport exchanges JSON-RPC payloads over HTTP POST.
type HTTPTransport struct {
	endpoint         string
	client           *http.Client
	headers          http.Header
	maxResponseBytes int64
}

// NewHTTPTransport validates an HTTP(S) endpoint and constructs a transport.
// The default client does not follow redirects. Nil options are ignored.
func NewHTTPTransport(endpoint string, options ...HTTPTransportOption) (*HTTPTransport, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("jsonrpc: invalid HTTP endpoint %q", endpoint)
	}
	transport := &HTTPTransport{
		endpoint:         parsed.String(),
		client:           defaultHTTPClient,
		headers:          make(http.Header),
		maxResponseBytes: defaultMaxResponseBytes,
	}
	for _, option := range options {
		if option != nil {
			option(transport)
		}
	}
	return transport, nil
}

// RoundTrip posts payload and returns a bounded JSON response. A 204 response
// returns a nil payload, and non-200 statuses return *HTTPStatusError.
func (transport *HTTPTransport) RoundTrip(ctx context.Context, payload []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, transport.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: create HTTP request: %w", err)
	}
	request.Header = transport.headers.Clone()
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := transport.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, transport.maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > transport.maxResponseBytes {
		return nil, ErrResponseTooLarge
	}
	if response.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{
			StatusCode: response.StatusCode,
			Body:       strings.TrimSpace(string(body)),
		}
	}
	if !IsJSONContentType(response.Header.Get("Content-Type")) {
		return nil, ErrHTTPContentType
	}
	return body, nil
}
