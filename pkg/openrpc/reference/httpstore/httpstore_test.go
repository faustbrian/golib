package httpstore_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/reference"
	"github.com/faustbrian/golib/pkg/openrpc/reference/httpstore"
)

func TestStoreLoadsAllowedBoundedIdentityResponses(t *testing.T) {
	t.Parallel()

	server := testServer(t)
	defer server.Close()
	store := allowedStore(t, server.URL, 2)
	var _ reference.Store = store
	loaded, err := store.Load(context.Background(), server.URL+"/document", 1024)
	if err != nil || string(loaded) != `{"value":true}` {
		t.Fatalf("Load = %s, error = %v", loaded, err)
	}
	loaded, err = store.Load(context.Background(), server.URL+"/redirect", 1024)
	if err != nil || string(loaded) != `{"value":true}` {
		t.Fatalf("redirect Load = %s, error = %v", loaded, err)
	}
	exactRedirectStore := allowedStore(t, server.URL, 1)
	if _, err := exactRedirectStore.Load(context.Background(), server.URL+"/redirect", 1024); err != nil {
		t.Fatalf("exact redirect boundary error = %v", err)
	}
	if _, err := store.Load(context.Background(), server.URL+"/document", len(`{"value":true}`)); err != nil {
		t.Fatalf("exact content-length boundary error = %v", err)
	}
}

func TestStoreRejectsLimitsCompressionStatusAndRedirects(t *testing.T) {
	t.Parallel()

	server := testServer(t)
	defer server.Close()
	store := allowedStore(t, server.URL, 0)
	for _, test := range []struct {
		path string
		max  int
		want error
	}{
		{path: "/large", max: 2, want: httpstore.ErrResponseLimit},
		{path: "/compressed", max: 1024, want: httpstore.ErrContentEncoding},
		{path: "/missing", max: 1024, want: httpstore.ErrHTTPStatus},
		{path: "/redirect", max: 1024, want: httpstore.ErrRedirect},
	} {
		if _, err := store.Load(context.Background(), server.URL+test.path, test.max); !errors.Is(err, test.want) {
			t.Errorf("Load(%s) error = %v", test.path, err)
		}
	}
}

func TestStoreDeniesHostsSchemesCredentialsAndPrivateAddresses(t *testing.T) {
	t.Parallel()

	server := testServer(t)
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	policy := httpstore.DefaultPolicy()
	policy.AllowHTTP = true
	policy.AllowedHosts = []string{parsed.Hostname()}
	store, err := httpstore.New(policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), server.URL+"/document", 1024); !errors.Is(err, httpstore.ErrAddressDenied) {
		t.Fatalf("private address error = %v", err)
	}
	for _, target := range []string{
		"ftp://example.com/document",
		"http://other.example/document",
		"http://user:password@" + parsed.Host + "/document",
	} {
		if _, err := store.Load(context.Background(), target, 1024); !errors.Is(err, httpstore.ErrURIDenied) {
			t.Errorf("Load(%q) error = %v", target, err)
		}
	}
}

func TestStoreValidatesPolicyAndCancellation(t *testing.T) {
	t.Parallel()

	if _, err := httpstore.New(httpstore.Policy{}); !errors.Is(err, httpstore.ErrPolicy) {
		t.Fatalf("New error = %v", err)
	}
	store, err := httpstore.New(httpstore.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Load(ctx, "https://example.com/document", 1024); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestStoreRejectsEveryInvalidPolicyBoundary(t *testing.T) {
	t.Parallel()

	mutations := []func(*httpstore.Policy){
		func(policy *httpstore.Policy) { policy.MaxRedirects = -1 },
		func(policy *httpstore.Policy) { policy.Timeout = 0 },
		func(policy *httpstore.Policy) { policy.DialTimeout = 0 },
		func(policy *httpstore.Policy) { policy.ResponseHeaderTimeout = 0 },
		func(policy *httpstore.Policy) { policy.MaxResponseHeaderBytes = 0 },
		func(policy *httpstore.Policy) { policy.AllowedHosts = []string{""} },
		func(policy *httpstore.Policy) { policy.AllowedHosts = []string{"user@example.com"} },
	}
	for index, mutate := range mutations {
		policy := httpstore.DefaultPolicy()
		mutate(&policy)
		if _, err := httpstore.New(policy); !errors.Is(err, httpstore.ErrPolicy) {
			t.Fatalf("policy %d error = %v", index, err)
		}
	}
}

func TestStoreRejectsInvalidLoadInputsAndRequestFailures(t *testing.T) {
	t.Parallel()

	var nilStore *httpstore.Store
	if _, err := nilStore.Load(context.Background(), "https://example.com", 1); !errors.Is(err, httpstore.ErrPolicy) {
		t.Fatalf("nil store error = %v", err)
	}
	zeroStore := &httpstore.Store{}
	if _, err := zeroStore.Load(context.Background(), "https://example.com", 1); !errors.Is(err, httpstore.ErrPolicy) {
		t.Fatalf("zero store error = %v", err)
	}
	store, err := httpstore.New(httpstore.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	var invalidContext context.Context
	if _, err := store.Load(invalidContext, "https://example.com", 1); !errors.Is(err, httpstore.ErrPolicy) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := store.Load(context.Background(), "https://example.com", 0); !errors.Is(err, httpstore.ErrPolicy) {
		t.Fatalf("zero limit error = %v", err)
	}
	for _, target := range []string{"http:///document", "http://[::1"} {
		if _, err := store.Load(context.Background(), target, 1); !errors.Is(err, httpstore.ErrURIDenied) {
			t.Fatalf("Load(%q) error = %v", target, err)
		}
	}

	policy := httpstore.DefaultPolicy()
	policy.AllowedHosts = []string{"localhost"}
	if store, err = httpstore.New(policy); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), "https://localhost:1/document", 1); !errors.Is(err, httpstore.ErrAddressDenied) {
		t.Fatalf("denied address error = %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	policy = httpstore.DefaultPolicy()
	policy.AllowHTTP = true
	policy.AllowPrivateAddresses = true
	policy.AllowedHosts = []string{"localhost"}
	store, err = httpstore.New(policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), fmt.Sprintf("http://localhost:%d/document", port), 1); !errors.Is(err, httpstore.ErrRequest) {
		t.Fatalf("request failure error = %v", err)
	}
}

func TestStoreHandlesStreamingLimitsReadFailuresAndRequestCancellation(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/chunked":
			writer.(http.Flusher).Flush()
			_, _ = writer.Write([]byte("0123456789"))
		case "/truncated":
			writer.Header().Set("Content-Length", "10")
			writer.(http.Flusher).Flush()
			_, _ = writer.Write([]byte("12"))
		case "/cancel":
			close(entered)
			<-request.Context().Done()
		}
	}))
	defer server.Close()
	store := allowedStore(t, server.URL, 0)
	if _, err := store.Load(context.Background(), server.URL+"/chunked", 2); !errors.Is(err, httpstore.ErrResponseLimit) {
		t.Fatalf("chunked limit error = %v", err)
	}
	if loaded, err := store.Load(context.Background(), server.URL+"/chunked", 10); err != nil || string(loaded) != "0123456789" {
		t.Fatalf("exact streaming boundary = %q, %v", loaded, err)
	}
	if _, err := store.Load(context.Background(), server.URL+"/truncated", 20); !errors.Is(err, httpstore.ErrRequest) {
		t.Fatalf("truncated body error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, loadErr := store.Load(ctx, server.URL+"/cancel", 20)
		done <- loadErr
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error = %v", err)
	}
}

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/document":
			_, _ = writer.Write([]byte(`{"value":true}`))
		case "/redirect":
			http.Redirect(writer, request, "/document", http.StatusFound)
		case "/large":
			writer.Header().Set("Content-Length", "10")
			_, _ = writer.Write([]byte("0123456789"))
		case "/compressed":
			writer.Header().Set("Content-Encoding", "gzip")
			_, _ = writer.Write([]byte("not-gzip"))
		default:
			http.Error(writer, "missing", http.StatusNotFound)
		}
	}))
}

func allowedStore(t *testing.T, serverURL string, redirects int) *httpstore.Store {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	policy := httpstore.DefaultPolicy()
	policy.AllowHTTP = true
	policy.AllowPrivateAddresses = true
	policy.MaxRedirects = redirects
	policy.AllowedHosts = []string{parsed.Hostname()}
	store, err := httpstore.New(policy)
	if err != nil {
		t.Fatal(fmt.Errorf("New: %w", err))
	}
	return store
}
