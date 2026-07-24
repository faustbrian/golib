package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestNewAppliesFiniteDefaultsAndRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	server, err := New(listener, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if server.http.ReadHeaderTimeout != defaultReadHeaderTimeout ||
		server.http.ReadTimeout != defaultReadTimeout ||
		server.http.WriteTimeout != defaultWriteTimeout ||
		server.http.IdleTimeout != defaultIdleTimeout ||
		server.http.MaxHeaderBytes != defaultMaxHeaderBytes ||
		server.shutdownTimeout != defaultShutdownTimeout {
		t.Fatalf("server defaults = %#v", server.http)
	}

	var typedListener *net.TCPListener
	var typedHandler *handlerStub
	invalid := []struct {
		listener net.Listener
		handler  http.Handler
		config   Config
	}{
		{handler: http.NotFoundHandler()},
		{listener: typedListener, handler: http.NotFoundHandler()},
		{listener: listener},
		{listener: listener, handler: typedHandler},
		{listener: listener, handler: http.NotFoundHandler(), config: Config{ReadHeaderTimeout: -1}},
		{listener: listener, handler: http.NotFoundHandler(), config: Config{ReadTimeout: -1}},
		{listener: listener, handler: http.NotFoundHandler(), config: Config{WriteTimeout: -1}},
		{listener: listener, handler: http.NotFoundHandler(), config: Config{IdleTimeout: -1}},
		{listener: listener, handler: http.NotFoundHandler(), config: Config{ShutdownTimeout: -1}},
		{listener: listener, handler: http.NotFoundHandler(), config: Config{MaxHeaderBytes: -1}},
	}
	for _, input := range invalid {
		server, err := New(input.listener, input.handler, input.config)
		if server != nil || !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("New(invalid) = (%v, %v)", server, err)
		}
	}
}

func TestServerServesAndShutsDownWithContext(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	server, err := New(listener, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}), Config{
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      2 * time.Second,
		IdleTimeout:       3 * time.Second,
		ShutdownTimeout:   time.Second,
		MaxHeaderBytes:    8 * 1024,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- server.Run(ctx) }()

	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + listener.Addr().String() + "/health/live")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.StatusCode)
	}

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not shut down")
	}
}

func TestServerRejectsAmbiguousRequestFramingBeforeHandler(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	var handlerCalls atomic.Int32
	server, err := New(listener, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		handlerCalls.Add(1)
	}), Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- server.Run(ctx) }()

	connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		cancel()
		t.Fatalf("DialTimeout() error = %v", err)
	}
	t.Cleanup(func() {
		_ = connection.Close()
	})
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		cancel()
		t.Fatalf("SetDeadline() error = %v", err)
	}
	_, err = io.WriteString(connection,
		"POST /v1/tenants/tenant-1/commands HTTP/1.1\r\n"+
			"Host: "+listener.Addr().String()+"\r\n"+
			"Content-Length: 4\r\n"+
			"Content-Length: 5\r\n"+
			"Connection: close\r\n\r\n"+
			"test",
	)
	if err != nil {
		cancel()
		t.Fatalf("WriteString() error = %v", err)
	}
	statusLine, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		cancel()
		t.Fatalf("ReadString() error = %v", err)
	}
	if !strings.Contains(statusLine, " 400 ") {
		cancel()
		t.Fatalf("status line = %q, want HTTP 400", statusLine)
	}
	if calls := handlerCalls.Load(); calls != 0 {
		cancel()
		t.Fatalf("handler calls = %d, want 0", calls)
	}

	cancel()
	if err := <-result; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestServerReturnsListenerFailure(t *testing.T) {
	t.Parallel()

	listenerErr := errors.New("listener failed")
	server, err := New(&listenerStub{err: listenerErr}, http.NotFoundHandler(), Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := server.Run(context.Background()); !errors.Is(err, listenerErr) {
		t.Fatalf("Run() error = %v, want listener failure", err)
	}
}

func TestServerBoundsSlowShutdown(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	server, err := New(listener, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	}), Config{ShutdownTimeout: time.Millisecond})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- server.Run(ctx) }()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, requestErr := (&http.Client{Timeout: time.Second}).Get("http://" + listener.Addr().String())
		if requestErr == nil {
			_ = response.Body.Close()
		}
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want shutdown deadline", err)
	}
	close(release)
	select {
	case <-requestDone:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not finish after forced shutdown")
	}
}

type handlerStub struct{}

func (*handlerStub) ServeHTTP(http.ResponseWriter, *http.Request) {}

type listenerStub struct {
	err error
}

func (listener *listenerStub) Accept() (net.Conn, error) { return nil, listener.err }
func (*listenerStub) Close() error                       { return nil }
func (*listenerStub) Addr() net.Addr                     { return stubAddr("listener") }

type stubAddr string

func (address stubAddr) Network() string { return string(address) }
func (address stubAddr) String() string  { return string(address) }
