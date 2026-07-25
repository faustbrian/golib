package reference_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestHTTPResolverReadsAuthorizedJSONAndYAML(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		switch request.URL.Path {
		case "/schema":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"type":"string"}`))
		case "/schema.yaml":
			writer.Header().Set("Content-Type", "application/yaml")
			_, _ = writer.Write([]byte("type: integer\n"))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	resolver := newTestHTTPResolver(t, server.URL, 1024, 2)

	for _, identifier := range []string{server.URL + "/schema", server.URL + "/schema.yaml"} {
		resource, err := resolver.Resolve(context.Background(), identifier)
		if err != nil {
			t.Fatal(err)
		}
		if resource.RetrievalURI != identifier || resource.CanonicalURI != identifier {
			t.Fatalf("resource identity = %#v", resource)
		}
		if _, exists := resource.Root.Lookup("type"); !exists {
			t.Fatalf("resource root = %#v", resource.Root)
		}
	}
}

func TestHTTPResolverRejectsUnauthorizedDestinations(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	parsed := mustURL(t, server.URL)
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	options := reference.DefaultHTTPResolverOptions()
	options.AllowedSchemes = []string{"http"}
	options.AllowedHosts = []string{"example.test"}
	options.AllowedPorts = []int{port}
	options.MaxDocuments = 8
	resolver, err := reference.NewHTTPResolver(options)
	if err != nil {
		t.Fatal(err)
	}

	identifiers := []string{
		server.URL,
		"ftp://example.test/schema.json",
		fmt.Sprintf("http://user@example.test:%d/schema.json", port),
		fmt.Sprintf("http://example.test:%d/schema.json?token=secret", port),
		"/schema.json",
	}
	for _, identifier := range identifiers {
		if _, err := resolver.Resolve(
			context.Background(), identifier,
		); !errors.Is(err, reference.ErrResourceDenied) {
			t.Fatalf("identifier %q error = %v", identifier, err)
		}
	}
}

func TestHTTPResolverRejectsPrivateAddressesWithoutExplicitNetwork(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	parsed := mustURL(t, server.URL)
	port, _ := strconv.Atoi(parsed.Port())
	options := reference.DefaultHTTPResolverOptions()
	options.AllowedSchemes = []string{"http"}
	options.AllowedHosts = []string{parsed.Hostname()}
	options.AllowedPorts = []int{port}
	resolver, err := reference.NewHTTPResolver(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(
		context.Background(), server.URL,
	); !errors.Is(err, reference.ErrResourceDenied) {
		t.Fatalf("private address error = %v", err)
	}
}

func TestHTTPResolverConfinesRedirects(t *testing.T) {
	t.Parallel()

	target := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{}`))
	}))
	t.Cleanup(target.Close)
	source := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	t.Cleanup(source.Close)
	resolver := newTestHTTPResolver(t, source.URL, 2, 1)
	if _, err := resolver.Resolve(
		context.Background(), source.URL,
	); !errors.Is(err, reference.ErrResourceDenied) {
		t.Fatalf("redirect escape error = %v", err)
	}

	var redirects atomic.Int32
	loop := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		redirects.Add(1)
		http.Redirect(writer, request, request.URL.Path, http.StatusFound)
	}))
	t.Cleanup(loop.Close)
	resolver = newTestHTTPResolver(t, loop.URL, 1, 1)
	if _, err := resolver.Resolve(
		context.Background(), loop.URL+"/loop",
	); !errors.Is(err, reference.ErrResourceLimitExceeded) {
		t.Fatalf("redirect limit error = %v", err)
	}
	if redirects.Load() != 2 {
		t.Fatalf("redirect requests = %d", redirects.Load())
	}
}

func TestHTTPResolverEnforcesResponsePolicyAndLimits(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		switch request.URL.Path {
		case "/large.json":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"value":"too large"}`))
		case "/compressed.json":
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set("Content-Encoding", "gzip")
			_, _ = writer.Write([]byte(`{}`))
		case "/unknown":
			writer.Header().Set("Content-Type", "application/octet-stream")
			_, _ = writer.Write([]byte(`{}`))
		case "/missing.json":
			http.Error(writer, "missing", http.StatusNotFound)
		case "/broken.json":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{]`))
		default:
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)

	resolver := newTestHTTPResolver(t, server.URL, 4, 1)
	for path, want := range map[string]error{
		"/large.json":      parse.ErrLimitExceeded,
		"/compressed.json": reference.ErrResourceDenied,
		"/unknown":         reference.ErrUnsupportedResourceFormat,
		"/missing.json":    reference.ErrResourceDenied,
		"/broken.json":     parse.ErrInvalidJSON,
	} {
		if _, err := resolver.Resolve(
			context.Background(), server.URL+path,
		); !errors.Is(err, want) {
			t.Fatalf("path %s error = %v", path, err)
		}
	}
}

func TestHTTPResolverAcceptsExactStatusAndBodySizeLimits(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusMultipleChoices - 1)
		_, _ = writer.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	resolver := newTestHTTPResolver(t, server.URL, 2, 0)
	if _, err := resolver.Resolve(context.Background(), server.URL); err != nil {
		t.Fatalf("exact response limits error = %v", err)
	}
}

func TestHTTPResolverEnforcesDocumentTimeoutAndContext(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		<-release
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{}`))
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})
	resolver := newTestHTTPResolver(t, server.URL, 1024, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := resolver.Resolve(ctx, server.URL); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
	//lint:ignore SA1012 This assertion verifies the nil-context contract.
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := resolver.Resolve(nil, server.URL); err == nil {
		t.Fatal("nil context was accepted")
	}
	canceled, cancelImmediately := context.WithCancel(context.Background())
	cancelImmediately()
	if _, err := resolver.Resolve(canceled, server.URL); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancellation error = %v", err)
	}
}

func TestHTTPResolverEnforcesDocumentBudget(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	parsed := mustURL(t, server.URL)
	port, _ := strconv.Atoi(parsed.Port())
	options := reference.DefaultHTTPResolverOptions()
	options.AllowedSchemes = []string{"http"}
	options.AllowedHosts = []string{parsed.Hostname()}
	options.AllowedPorts = []int{port}
	options.AllowedCIDRs = []string{"127.0.0.0/8"}
	options.MaxDocuments = 1
	resolver, err := reference.NewHTTPResolver(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(resolver.CloseIdleConnections)
	if _, err := resolver.Resolve(context.Background(), server.URL+"/one.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(
		context.Background(), server.URL+"/two.json",
	); !errors.Is(err, reference.ErrResourceLimitExceeded) {
		t.Fatalf("document limit error = %v", err)
	}
}

func TestHTTPResolverRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	valid := reference.DefaultHTTPResolverOptions()
	valid.AllowedHosts = []string{"example.test"}
	for _, mutate := range []func(*reference.HTTPResolverOptions){
		func(options *reference.HTTPResolverOptions) { options.AllowedHosts = nil },
		func(options *reference.HTTPResolverOptions) { options.AllowedSchemes = []string{"ftp"} },
		func(options *reference.HTTPResolverOptions) { options.AllowedHosts = []string{"*.example.test"} },
		func(options *reference.HTTPResolverOptions) { options.AllowedPorts = []int{0} },
		func(options *reference.HTTPResolverOptions) { options.AllowedCIDRs = []string{"invalid"} },
		func(options *reference.HTTPResolverOptions) { options.MaxBytes = 0 },
		func(options *reference.HTTPResolverOptions) { options.MaxDocuments = 0 },
		func(options *reference.HTTPResolverOptions) { options.MaxRedirects = -1 },
		func(options *reference.HTTPResolverOptions) { options.MaxConcurrency = 0 },
		func(options *reference.HTTPResolverOptions) { options.MaxAddresses = 0 },
		func(options *reference.HTTPResolverOptions) { options.MaxResponseHeaderBytes = 0 },
		func(options *reference.HTTPResolverOptions) { options.Timeout = 0 },
		func(options *reference.HTTPResolverOptions) { options.ParseLimits = parse.Limits{} },
	} {
		options := valid
		mutate(&options)
		if _, err := reference.NewHTTPResolver(options); err == nil {
			t.Fatalf("invalid options accepted: %#v", options)
		}
	}
}

func newTestHTTPResolver(
	t *testing.T,
	serverURL string,
	maxBytes int64,
	maxRedirects int,
) *reference.HTTPResolver {
	t.Helper()
	parsed := mustURL(t, serverURL)
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	options := reference.DefaultHTTPResolverOptions()
	options.AllowedSchemes = []string{"http"}
	options.AllowedHosts = []string{parsed.Hostname()}
	options.AllowedPorts = []int{port}
	options.AllowedCIDRs = []string{"127.0.0.0/8", "::1/128"}
	options.MaxBytes = maxBytes
	options.MaxDocuments = 16
	options.MaxRedirects = maxRedirects
	options.Timeout = time.Second
	resolver, err := reference.NewHTTPResolver(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(resolver.CloseIdleConnections)
	return resolver
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestHTTPResolverUsesOnlyConfiguredConcurrentSlots(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		entered <- struct{}{}
		<-release
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(closeRelease)
	parsed := mustURL(t, server.URL)
	port, _ := strconv.Atoi(parsed.Port())
	options := reference.DefaultHTTPResolverOptions()
	options.AllowedSchemes = []string{"http"}
	options.AllowedHosts = []string{parsed.Hostname()}
	options.AllowedPorts = []int{port}
	options.AllowedCIDRs = []string{"127.0.0.0/8"}
	options.MaxConcurrency = 1
	options.Timeout = time.Second
	resolver, err := reference.NewHTTPResolver(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(resolver.CloseIdleConnections)

	results := make(chan error, 2)
	go func() {
		_, resolveErr := resolver.Resolve(context.Background(), server.URL+"/one.json")
		results <- resolveErr
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := resolver.Resolve(ctx, server.URL+"/two.json"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("concurrency wait error = %v", err)
	}
	closeRelease()
	if err := <-results; err != nil {
		t.Fatal(err)
	}
}

func TestHTTPResolverRejectsUnapprovedResolvedAddress(t *testing.T) {
	t.Parallel()

	options := reference.DefaultHTTPResolverOptions()
	options.AllowedHosts = []string{"localhost"}
	options.AllowedPorts = []int{443}
	resolver, err := reference.NewHTTPResolver(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(resolver.CloseIdleConnections)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = resolver.Resolve(ctx, "https://localhost/schema.json")
	if !errors.Is(err, reference.ErrResourceDenied) {
		t.Fatalf("localhost resolution error = %v", err)
	}
}
