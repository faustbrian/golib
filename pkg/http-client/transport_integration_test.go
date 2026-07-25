package httpclient

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestTransportIntegrationNegotiatesHTTPVersions(t *testing.T) {
	tests := []struct {
		name      string
		start     func(http.Handler) (*httptest.Server, http.RoundTripper)
		wantMajor int
	}{
		{
			name: "HTTP/1.1",
			start: func(handler http.Handler) (*httptest.Server, http.RoundTripper) {
				server := httptest.NewServer(handler)
				return server, nil
			},
			wantMajor: 1,
		},
		{
			name: "HTTP/2",
			start: func(handler http.Handler) (*httptest.Server, http.RoundTripper) {
				server := httptest.NewUnstartedServer(handler)
				server.EnableHTTP2 = true
				server.StartTLS()
				return server, server.Client().Transport
			},
			wantMajor: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			protocol := make(chan int, 1)
			server, transport := test.start(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				protocol <- request.ProtoMajor
				writer.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			client, err := New(Config{Transport: transport})
			if err != nil {
				t.Fatalf("construct client: %v", err)
			}
			defer func() { _ = client.Close() }()
			request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
			response, err := client.Do(request)
			if err != nil {
				t.Fatalf("perform request: %v", err)
			}
			_ = response.Body.Close()
			if got := <-protocol; got != test.wantMajor {
				t.Fatalf("protocol major = %d, want %d", got, test.wantMajor)
			}
		})
	}
}

func TestTransportIntegrationReusesHTTP1Connection(t *testing.T) {
	var connections atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "reusable")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	client, err := New(Config{})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() { _ = client.Close() }()
	for range 2 {
		request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		response, requestErr := client.Do(request)
		if requestErr != nil {
			t.Fatalf("perform request: %v", requestErr)
		}
		if _, readErr := io.Copy(io.Discard, response.Body); readErr != nil {
			t.Fatalf("read response: %v", readErr)
		}
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Fatalf("close response: %v", closeErr)
		}
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("connection count = %d, want 1", got)
	}
}

func TestTransportIntegrationUsesConfiguredProxy(t *testing.T) {
	targets := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		targets <- request.URL.String()
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	client, err := New(Config{
		Transport: transport, TransportOwnership: TransportOwned,
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() { _ = client.Close() }()
	request, _ := http.NewRequest(http.MethodGet, "http://vendor.example.test/widgets?q=1", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("perform proxied request: %v", err)
	}
	_ = response.Body.Close()
	if got := <-targets; got != "http://vendor.example.test/widgets?q=1" {
		t.Fatalf("proxy target = %q", got)
	}
}

func TestTransportIntegrationTotalTimeoutCancelsRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(250 * time.Millisecond)
	}))
	defer server.Close()
	client, err := New(Config{Timeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	defer func() { _ = client.Close() }()
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	started := time.Now()
	response, err := client.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("timeout elapsed = %v", elapsed)
	}
}
