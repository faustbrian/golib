package jsonrpc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPTransportRoundTrip(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s", request.Method)
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", request.Header.Get("Content-Type"))
		}
		if request.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", request.Header.Get("Accept"))
		}
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(request.Body)
		assertJSONEqual(t, body, []byte(`{"jsonrpc":"2.0","method":"ping","id":1}`))
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","result":"pong","id":1}`))
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(server.URL, WithHTTPHeader("Authorization", "Bearer token"))
	if err != nil {
		t.Fatal(err)
	}
	reply, err := transport.RoundTrip(context.Background(), []byte(`{"jsonrpc":"2.0","method":"ping","id":1}`))
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	assertJSONEqual(t, reply, []byte(`{"jsonrpc":"2.0","result":"pong","id":1}`))
}

func TestHTTPTransportNoContent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	transport, _ := NewHTTPTransport(server.URL)
	reply, err := transport.RoundTrip(context.Background(), []byte(`{}`))
	if err != nil || reply != nil {
		t.Errorf("RoundTrip() = (%q, %v), want nil, nil", reply, err)
	}
}

func TestHTTPTransportDoesNotFollowRedirectsByDefault(t *testing.T) {
	t.Parallel()

	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		targetCalled = true
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","result":null,"id":1}`))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	transport, _ := NewHTTPTransport(source.URL, WithHTTPHeader("X-API-Key", "secret"))
	_, err := transport.RoundTrip(context.Background(), []byte(`{}`))
	if !errors.Is(err, ErrHTTPStatus) {
		t.Fatalf("RoundTrip(redirect) error = %v, want HTTP status error", err)
	}
	if targetCalled {
		t.Fatal("default transport followed a redirect and forwarded the request")
	}

	optedIn, _ := NewHTTPTransport(source.URL, WithHTTPClient(&http.Client{}))
	if _, err := optedIn.RoundTrip(context.Background(), []byte(`{}`)); err != nil {
		t.Fatalf("RoundTrip(explicit redirect client) error = %v", err)
	}
	if !targetCalled {
		t.Fatal("explicit client redirect policy was not honored")
	}
}

func TestHTTPTransportValidation(t *testing.T) {
	t.Parallel()

	if _, err := NewHTTPTransport("://invalid"); err == nil {
		t.Error("NewHTTPTransport(invalid) unexpectedly succeeded")
	}
	if _, err := NewHTTPTransport("ftp://example.com/rpc"); err == nil {
		t.Error("NewHTTPTransport(ftp) unexpectedly succeeded")
	}

	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		limit       int64
		want        error
	}{
		{name: "status", status: http.StatusBadGateway, contentType: "text/plain", body: "upstream failed", want: ErrHTTPStatus},
		{name: "media type", status: http.StatusOK, contentType: "text/plain", body: `{}`, want: ErrHTTPContentType},
		{name: "response too large", status: http.StatusOK, contentType: "application/json", body: "12345", limit: 4, want: ErrResponseTooLarge},
		{name: "error response too large", status: http.StatusBadGateway, contentType: "text/plain", body: "12345", limit: 4, want: ErrResponseTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", tt.contentType)
				writer.WriteHeader(tt.status)
				_, _ = writer.Write([]byte(tt.body))
			}))
			defer server.Close()
			options := []HTTPTransportOption{}
			if tt.limit > 0 {
				options = append(options, WithMaxResponseBytes(tt.limit))
			}
			transport, _ := NewHTTPTransport(server.URL, options...)
			_, err := transport.RoundTrip(context.Background(), []byte(`{}`))
			if !errors.Is(err, tt.want) {
				t.Errorf("RoundTrip() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestHTTPTransportNetworkError(t *testing.T) {
	t.Parallel()

	transport, _ := NewHTTPTransport("http://example.invalid", WithHTTPClient(&http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		}),
	}))
	_, err := transport.RoundTrip(context.Background(), []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Errorf("RoundTrip() error = %v", err)
	}
}

func TestHTTPTransportRejectsNilContextWithoutNetworkIO(t *testing.T) {
	t.Parallel()

	called := false
	transport, _ := NewHTTPTransport("http://example.test", WithHTTPClient(&http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unexpected network call")
		}),
	}))
	//lint:ignore SA1012 Public boundary must reject nil context before network I/O.
	if _, err := transport.RoundTrip(nil, []byte(`{}`)); err == nil { //nolint:staticcheck // verifies defensive nil handling
		t.Fatal("RoundTrip(nil context) unexpectedly succeeded")
	}
	if called {
		t.Fatal("RoundTrip(nil context) performed network I/O")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
