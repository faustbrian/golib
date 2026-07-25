package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewClientProvidesFiniteSafeDefaults(t *testing.T) {
	t.Parallel()

	client, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	httpClient := client.HTTPClient()
	if httpClient.Timeout != 30*time.Second {
		t.Fatalf("Timeout = %v, want 30s", httpClient.Timeout)
	}

	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", httpClient.Transport)
	}

	if transport.DialContext == nil {
		t.Fatal("DialContext is nil")
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig is nil")
	}
	if transport.TLSClientConfig.MinVersion == 0 {
		t.Fatal("TLS minimum version is not explicit")
	}
	if transport.TLSHandshakeTimeout <= 0 {
		t.Fatalf("TLSHandshakeTimeout = %v, want a finite timeout", transport.TLSHandshakeTimeout)
	}
	if transport.ResponseHeaderTimeout <= 0 {
		t.Fatalf("ResponseHeaderTimeout = %v, want a finite timeout", transport.ResponseHeaderTimeout)
	}
	if transport.IdleConnTimeout <= 0 {
		t.Fatalf("IdleConnTimeout = %v, want a finite timeout", transport.IdleConnTimeout)
	}
	if transport.MaxResponseHeaderBytes <= 0 {
		t.Fatalf("MaxResponseHeaderBytes = %d, want a finite limit", transport.MaxResponseHeaderBytes)
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatal("ForceAttemptHTTP2 = false, want true")
	}
}

func TestNewClientRejectsNegativeTimeout(t *testing.T) {
	t.Parallel()

	_, err := New(Config{Timeout: -time.Second})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
	}
}

func TestNewClientRejectsUnknownTransportOwnership(t *testing.T) {
	t.Parallel()

	_, err := New(Config{TransportOwnership: TransportOwnership(255)})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
	}
}

func TestClientRejectsNilRequest(t *testing.T) {
	t.Parallel()

	client, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.Do(nil)
	if !errors.Is(err, ErrNilRequest) {
		t.Fatalf("Do() error = %v, want ErrNilRequest", err)
	}
}

func TestClientRejectsRequestAfterClose(t *testing.T) {
	t.Parallel()

	client, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	_, err = client.Do(request)
	if !errors.Is(err, ErrClientClosed) {
		t.Fatalf("Do() error = %v, want ErrClientClosed", err)
	}
}

func TestClientCloseCancelsPendingRequest(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	client, err := New(Config{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()

		return nil, request.Context().Err()
	})})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, doErr := client.Do(request)
		result <- doErr
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case doErr := <-result:
		if !errors.Is(doErr, context.Canceled) {
			t.Fatalf("Do() error = %v, want context.Canceled", doErr)
		}
	case <-time.After(time.Second):
		t.Fatal("pending request was not canceled")
	}
}

func TestClientCloseClosesActiveResponseBodies(t *testing.T) {
	t.Parallel()

	body := &trackingBody{closed: make(chan struct{})}
	client, err := New(Config{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       body,
		}, nil
	})})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if response.Body == body {
		t.Fatal("response body is not lifecycle-managed")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case <-body.closed:
	case <-time.After(time.Second):
		t.Fatal("active response body was not closed")
	}
}

func TestClientRejectsResponseReturnedDuringClose(t *testing.T) {
	t.Parallel()

	body := &trackingBody{closed: make(chan struct{})}
	var client *Client
	client, err := New(Config{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if closeErr := client.Close(); closeErr != nil {
			return nil, closeErr
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       body,
		}, nil
	})})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := client.Do(request)
	if !errors.Is(err, ErrClientClosed) {
		t.Fatalf("Do() error = %v, want ErrClientClosed", err)
	}
	if response != nil {
		t.Fatalf("Do() response = %#v, want nil", response)
	}

	select {
	case <-body.closed:
	case <-time.After(time.Second):
		t.Fatal("response returned during close was not closed")
	}
}

func TestClientClosePreservesBodyCloseError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("body close failure")
	client, err := New(Config{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       closeErrorBody{err: wantErr},
		}, nil
	})})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	if _, err := client.Do(request); err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if err := client.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("Close() error = %v, want %v", err, wantErr)
	}
}

func TestClientCloseDoesNotCloseBorrowedTransport(t *testing.T) {
	t.Parallel()

	transport := &borrowedTransport{}
	client, err := New(Config{Transport: transport})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if transport.closeCalls != 0 {
		t.Fatalf("borrowed transport CloseIdleConnections calls = %d, want 0", transport.closeCalls)
	}
}

func TestClientCloseClosesOwnedTransport(t *testing.T) {
	t.Parallel()

	transport := &borrowedTransport{}
	client, err := New(Config{
		Transport:          transport,
		TransportOwnership: TransportOwned,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if transport.closeCalls != 1 {
		t.Fatalf("owned transport CloseIdleConnections calls = %d, want 1", transport.closeCalls)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if transport.closeCalls != 1 {
		t.Fatalf("owned transport CloseIdleConnections calls after second Close = %d, want 1", transport.closeCalls)
	}
}

func TestTransportErrorRedactsURLCredentialsAndQuery(t *testing.T) {
	t.Parallel()

	client, err := New(Config{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("wire failure")
	})})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"https://alice:password@example.com/path?token=secret",
		nil,
	)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	_, err = client.Do(request)
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("Do() error = %T %v, want *TransportError", err, err)
	}
	if !errors.Is(err, transportErr.Unwrap()) {
		t.Fatal("TransportError does not preserve its cause")
	}
	for _, secret := range []string{"alice", "password", "token", "secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("Do() error %q contains secret %q", err, secret)
		}
	}
	if !strings.Contains(err.Error(), "https://example.com/path") {
		t.Fatalf("Do() error = %q, want sanitized URL", err)
	}
}

func TestTransportErrorHandlesRequestWithoutURL(t *testing.T) {
	t.Parallel()

	client, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.Do(&http.Request{Method: http.MethodGet})
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("Do() error = %T %v, want *TransportError", err, err)
	}
	if transportErr.URL != "" {
		t.Fatalf("TransportError.URL = %q, want empty", transportErr.URL)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type trackingBody struct {
	once   sync.Once
	closed chan struct{}
}

type closeErrorBody struct {
	err error
}

func (closeErrorBody) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (body closeErrorBody) Close() error {
	return body.err
}

func (*trackingBody) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (body *trackingBody) Close() error {
	body.once.Do(func() { close(body.closed) })

	return nil
}

type borrowedTransport struct {
	closeCalls int
}

func (*borrowedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

func (transport *borrowedTransport) CloseIdleConnections() {
	transport.closeCalls++
}
