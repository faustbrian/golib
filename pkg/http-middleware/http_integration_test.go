package middleware_test

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"testing"
	"time"

	middleware "github.com/faustbrian/golib/pkg/http-middleware"
	compressmw "github.com/faustbrian/golib/pkg/http-middleware/compress"
	"github.com/faustbrian/golib/pkg/http-middleware/cors"
	"github.com/faustbrian/golib/pkg/http-middleware/deadline"
	"github.com/faustbrian/golib/pkg/http-middleware/observe"
	"github.com/faustbrian/golib/pkg/http-middleware/recovery"
	"github.com/faustbrian/golib/pkg/http-middleware/responsepolicy"
	"github.com/faustbrian/golib/pkg/http-middleware/secureheader"
)

func TestRealListenerHTTP1AndHTTP2PreserveFlushAndTrailers(t *testing.T) {
	for _, protocol := range []string{"http1", "http2"} {
		t.Run(protocol, func(t *testing.T) {
			var event observe.Event
			observer, _ := observe.New(observe.Policy{Observer: func(_ context.Context, got observe.Event) { event = got }})
			recoverer, _ := recovery.New(recovery.Policy{})
			crossOrigin, _ := cors.New(cors.Policy{AllowedOrigins: []string{"https://app.example"}})
			headers, _ := secureheader.New(secureheader.APIDefaults())
			chain, _ := middleware.New(recoverer, observer, crossOrigin, headers, responsepolicy.NoStore())
			handler, _ := chain.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Trailer", "X-Checksum")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, "chunk")
				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Errorf("Flush() error = %v", err)
				}
				w.Header().Set("X-Checksum", "done")
			}))

			server := httptest.NewUnstartedServer(handler)
			server.EnableHTTP2 = protocol == "http2"
			if protocol == "http2" {
				server.StartTLS()
			} else {
				server.Start()
			}
			t.Cleanup(server.Close)
			req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
			req.Header.Set("Origin", "https://app.example")
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			payload, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if string(payload) != "chunk" || response.Trailer.Get("X-Checksum") != "done" {
				t.Fatalf("payload = %q, trailer = %v", payload, response.Trailer)
			}
			if protocol == "http2" && response.ProtoMajor != 2 {
				t.Fatalf("protocol = %s", response.Proto)
			}
			if protocol == "http1" && response.ProtoMajor != 1 {
				t.Fatalf("protocol = %s", response.Proto)
			}
			if response.Header.Get("Access-Control-Allow-Origin") != "https://app.example" || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("headers = %v", response.Header)
			}
			if event.Status != http.StatusOK || event.Bytes != 5 {
				t.Fatalf("event = %#v", event)
			}
		})
	}
}

func TestRealCompressionPreservesTrailers(t *testing.T) {
	t.Parallel()
	compressor, err := compressmw.New(compressmw.Policy{MinimumBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(compressor(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Trailer", "X-Checksum")
		_, _ = io.WriteString(w, "payload")
		w.Header().Set("X-Checksum", "done")
	})))
	t.Cleanup(server.Close)
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept-Encoding", "gzip")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response == nil {
		t.Fatal("nil response")
		return
	}
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	_ = response.Body.Close()
	if string(payload) != "payload" || response.Trailer.Get("X-Checksum") != "done" {
		t.Fatalf("payload = %q, headers = %v, trailers = %v", payload, response.Header, response.Trailer)
	}
	if response.Header.Get("X-Checksum") != "" {
		t.Fatalf("trailer leaked into initial headers: %v", response.Header)
	}
}

func TestRealTimeoutPreservesInformationalResponse(t *testing.T) {
	t.Parallel()
	timeout, err := deadline.NewTimeout(deadline.TimeoutPolicy{
		Timeout: time.Second, MaxResponseBytes: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(timeout(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", "</style.css>; rel=preload")
		w.WriteHeader(http.StatusEarlyHints)
		w.Header().Del("Link")
		w.WriteHeader(http.StatusNoContent)
	})))
	t.Cleanup(server.Close)
	var informational []int
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	trace := &httptrace.ClientTrace{Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
		informational = append(informational, code)
		if header.Get("Link") == "" {
			t.Error("early hints omitted Link")
		}
		return nil
	}}
	response, err := server.Client().Do(request.WithContext(httptrace.WithClientTrace(request.Context(), trace)))
	if err != nil {
		t.Fatal(err)
	}
	if response == nil {
		t.Fatal("nil response")
		return
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent || len(informational) != 1 || informational[0] != http.StatusEarlyHints {
		t.Fatalf("final = %d, informational = %v", response.StatusCode, informational)
	}
}

func TestRealTimeoutPreservesInformationalResponseBeforeTimeout(t *testing.T) {
	t.Parallel()
	timeout, err := deadline.NewTimeout(deadline.TimeoutPolicy{
		Timeout: time.Millisecond, MaxResponseBytes: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(timeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		<-r.Context().Done()
	})))
	t.Cleanup(server.Close)
	gotEarlyHints := false
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	trace := &httptrace.ClientTrace{Got1xxResponse: func(code int, _ textproto.MIMEHeader) error {
		gotEarlyHints = code == http.StatusEarlyHints
		return nil
	}}
	response, err := server.Client().Do(request.WithContext(httptrace.WithClientTrace(request.Context(), trace)))
	if err != nil {
		t.Fatal(err)
	}
	if response == nil {
		t.Fatal("nil response")
		return
	}
	_ = response.Body.Close()
	if !gotEarlyHints || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("early hints = %v, final = %d", gotEarlyHints, response.StatusCode)
	}
}

func TestRealHTTP1HijackSurvivesTrackingWrappers(t *testing.T) {
	recoverer, _ := recovery.New(recovery.Policy{})
	observer, _ := observe.New(observe.Policy{Observer: func(context.Context, observe.Event) {}})
	chain, _ := middleware.New(recoverer, observer)
	handler, _ := chain.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connection, buffered, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("Hijack() error = %v", err)
			return
		}
		defer func() { _ = connection.Close() }()
		_, _ = buffered.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		_ = buffered.Flush()
	}))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	_, _ = fmt.Fprintf(connection, "GET / HTTP/1.1\r\nHost: example\r\nConnection: close\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatalf("ReadResponse() error = %v", err)
	}
	payload, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if strings.TrimSpace(string(payload)) != "ok" {
		t.Fatalf("payload = %q", payload)
	}
}

func TestRealClientDisconnectCancelsWrappedRequest(t *testing.T) {
	t.Parallel()
	canceled := make(chan struct{})
	recoverer, _ := recovery.New(recovery.Policy{})
	observer, _ := observe.New(observe.Policy{Observer: func(context.Context, observe.Event) {}})
	chain, _ := middleware.New(recoverer, observer)
	handler, _ := chain.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(canceled)
	}))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	address := strings.TrimPrefix(server.URL, "http://")
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(connection, "GET / HTTP/1.1\r\nHost: example\r\n\r\n")
	_ = connection.Close()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("wrapped request was not canceled after disconnect")
	}
}
