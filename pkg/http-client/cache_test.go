package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheMiddlewareStoresFreshVariantsAndReturnsIndependentBodies(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		writer.Header().Set("Cache-Control", "max-age=60")
		writer.Header().Set("Vary", "Accept-Language")
		_, _ = fmt.Fprintf(writer, "%s:%d", request.Header.Get("Accept-Language"), call)
	}))
	t.Cleanup(server.Close)

	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name:  "vendor-cache",
		Layer: MiddlewareClient,
		Store: store,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request := func(language string) (*http.Response, string) {
		req, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/widgets", nil)
		if requestErr != nil {
			t.Fatalf("construct request: %v", requestErr)
		}
		req.Header.Set("Accept-Language", language)
		response, doErr := client.Do(req)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("read response: %v", readErr)
		}
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Fatalf("close response: %v", closeErr)
		}

		return response, string(body)
	}

	first, firstBody := request("en")
	second, secondBody := request("en")
	_, frenchBody := request("fr")
	third, thirdBody := request("en")
	if firstBody != "en:1" || secondBody != "en:1" || frenchBody != "fr:2" || thirdBody != "en:1" {
		t.Fatalf("bodies = %q, %q, %q, %q", firstBody, secondBody, frenchBody, thirdBody)
	}
	if calls.Load() != 2 {
		t.Fatalf("origin calls = %d, want 2", calls.Load())
	}
	if metadata, ok := CacheMetadataFromResponse(first); !ok || metadata.Provenance != CacheMiss {
		t.Fatalf("first metadata = %#v, %t", metadata, ok)
	}
	if metadata, ok := CacheMetadataFromResponse(second); !ok || metadata.Provenance != CacheHit || metadata.Age < 0 {
		t.Fatalf("second metadata = %#v, %t", metadata, ok)
	}
	if metadata, ok := CacheMetadataFromResponse(third); !ok || metadata.Provenance != CacheHit {
		t.Fatalf("third metadata = %#v, %t", metadata, ok)
	}
}

func TestSharedCacheDoesNotReuseAuthorizationWithoutExplicitPermission(t *testing.T) {
	t.Parallel()

	var privateCalls atomic.Int64
	var publicCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/public" {
			call := publicCalls.Add(1)
			writer.Header().Set("Cache-Control", "public, max-age=60")
			_, _ = fmt.Fprintf(writer, "public:%d", call)
			return
		}
		call := privateCalls.Add(1)
		writer.Header().Set("Cache-Control", "max-age=60")
		_, _ = fmt.Fprintf(writer, "private:%d", call)
	}))
	t.Cleanup(server.Close)

	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name:   "shared-cache",
		Layer:  MiddlewareClient,
		Store:  store,
		Shared: true,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request := func(path string) string {
		req, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+path, nil)
		if requestErr != nil {
			t.Fatalf("construct request: %v", requestErr)
		}
		req.Header.Set("Authorization", "Bearer test-secret")
		response, doErr := client.Do(req)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		defer func() { _ = response.Body.Close() }()
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("read response: %v", readErr)
		}

		return string(body)
	}

	if first, second := request("/private"), request("/private"); first == second || privateCalls.Load() != 2 {
		t.Fatalf("private bodies = %q, %q; calls = %d", first, second, privateCalls.Load())
	}
	if first, second := request("/public"), request("/public"); first != second || publicCalls.Load() != 1 {
		t.Fatalf("public bodies = %q, %q; calls = %d", first, second, publicCalls.Load())
	}
}

func TestCacheMiddlewareRevalidatesStaleETagAndFreshensStoredResponse(t *testing.T) {
	t.Parallel()

	clock := &cacheTestClock{now: time.Unix(1_700_000_000, 0)}
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Header.Get("If-None-Match") == `"widget-v1"` {
			writer.Header().Set("Cache-Control", "max-age=60")
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.Header().Set("Cache-Control", "max-age=1")
		writer.Header().Set("ETag", `"widget-v1"`)
		_, _ = io.WriteString(writer, "widget-v1")
	}))
	t.Cleanup(server.Close)

	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name:  "revalidation-cache",
		Layer: MiddlewareClient,
		Store: store,
		Clock: clock,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request := func() (*http.Response, string) {
		req, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/widgets/1", nil)
		if requestErr != nil {
			t.Fatalf("construct request: %v", requestErr)
		}
		response, doErr := client.Do(req)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("read response: %v", readErr)
		}
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Fatalf("close response: %v", closeErr)
		}

		return response, string(body)
	}

	_, firstBody := request()
	clock.Advance(2 * time.Second)
	second, secondBody := request()
	third, thirdBody := request()
	if firstBody != "widget-v1" || secondBody != "widget-v1" || thirdBody != "widget-v1" || calls.Load() != 2 {
		t.Fatalf("bodies = %q, %q, %q; calls = %d", firstBody, secondBody, thirdBody, calls.Load())
	}
	if metadata, ok := CacheMetadataFromResponse(second); !ok || metadata.Provenance != CacheRevalidated {
		t.Fatalf("revalidated metadata = %#v, %t", metadata, ok)
	}
	if metadata, ok := CacheMetadataFromResponse(third); !ok || metadata.Provenance != CacheHit {
		t.Fatalf("freshened metadata = %#v, %t", metadata, ok)
	}
}

func TestCacheMiddlewareHonorsNoStoreAndCredentialIsolation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		writer.Header().Set("Cache-Control", "max-age=60")
		_, _ = fmt.Fprintf(writer, "%s:%d", request.Header.Get("Authorization"), call)
	}))
	t.Cleanup(server.Close)

	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "isolated-cache", Layer: MiddlewareClient, Store: store,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request := func(authorization string, noStore bool) string {
		req, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
		if requestErr != nil {
			t.Fatalf("construct request: %v", requestErr)
		}
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		if noStore {
			req.Header.Set("Cache-Control", "no-store")
		}
		response, doErr := client.Do(req)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		defer func() { _ = response.Body.Close() }()
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("read response: %v", readErr)
		}

		return string(body)
	}

	first := request("Bearer account-a", false)
	second := request("Bearer account-b", false)
	third := request("", true)
	fourth := request("", true)
	if first == second || third == fourth || calls.Load() != 4 {
		t.Fatalf("isolated bodies = %q, %q, %q, %q; calls = %d", first, second, third, fourth, calls.Load())
	}
}

func TestCacheMiddlewareBoundsStorageWithoutTruncatingCallerResponse(t *testing.T) {
	t.Parallel()

	want := strings.Repeat("bounded-body-", 32)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.Header().Set("Cache-Control", "max-age=60")
		_, _ = io.WriteString(writer, want)
	}))
	t.Cleanup(server.Close)

	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "bounded-cache", Layer: MiddlewareClient, Store: store,
		MaximumBodyBytes: 32,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	for range 2 {
		request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
		if requestErr != nil {
			t.Fatalf("construct request: %v", requestErr)
		}
		response, doErr := client.Do(request)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil || string(body) != want {
			t.Fatalf("bounded response = %d bytes, %v", len(body), readErr)
		}
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Fatalf("close response: %v", closeErr)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("origin calls = %d, want 2", calls.Load())
	}
}

func TestCacheMiddlewareCoalescesConcurrentMisses(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})
	duplicate := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		} else {
			select {
			case duplicate <- struct{}{}:
			default:
			}
		}
		<-release
		writer.Header().Set("Cache-Control", "max-age=60")
		_, _ = io.WriteString(writer, "coalesced")
	}))
	t.Cleanup(server.Close)

	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "coalescing-cache", Layer: MiddlewareClient, Store: store,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	const requests = 12
	ready := make(chan struct{})
	errors := make(chan error, requests)
	var workers sync.WaitGroup
	workers.Add(requests)
	for range requests {
		go func() {
			defer workers.Done()
			<-ready
			request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
			if requestErr != nil {
				errors <- requestErr
				return
			}
			response, doErr := client.Do(request)
			if doErr != nil {
				errors <- doErr
				return
			}
			body, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil || string(body) != "coalesced" {
				errors <- fmt.Errorf("body %q: read %w, close %v", body, readErr, closeErr)
			}
		}()
	}
	close(ready)
	<-started
	select {
	case <-duplicate:
		close(release)
		workers.Wait()
		t.Fatal("concurrent miss reached origin more than once")
	case <-time.After(50 * time.Millisecond):
		close(release)
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("coalesced request: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("origin calls = %d, want 1", calls.Load())
	}
}

func TestCacheMiddlewareSupportsBypassRefreshTTLAndUnsafeInvalidation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		if request.Method == http.MethodPost {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = fmt.Fprintf(writer, "version:%d", call)
	}))
	t.Cleanup(server.Close)

	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "controlled-cache", Layer: MiddlewareClient, Store: store,
		TTLOverride: time.Minute,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	get := func(mode CacheMode) string {
		ctx, contextErr := WithCacheMode(context.Background(), mode)
		if contextErr != nil {
			t.Fatalf("set cache mode: %v", contextErr)
		}
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/widgets", nil)
		if requestErr != nil {
			t.Fatalf("construct request: %v", requestErr)
		}
		response, doErr := client.Do(request)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		defer func() { _ = response.Body.Close() }()
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("read response: %v", readErr)
		}

		return string(body)
	}

	if first, second := get(CacheModeDefault), get(CacheModeDefault); first != "version:1" || second != first {
		t.Fatalf("initial bodies = %q, %q", first, second)
	}
	if bypassed := get(CacheModeBypass); bypassed != "version:2" {
		t.Fatalf("bypass body = %q", bypassed)
	}
	if cached := get(CacheModeDefault); cached != "version:1" {
		t.Fatalf("bypass replaced cache: %q", cached)
	}
	if refreshed := get(CacheModeRefresh); refreshed != "version:3" {
		t.Fatalf("refresh body = %q", refreshed)
	}
	if cached := get(CacheModeDefault); cached != "version:3" {
		t.Fatalf("refresh did not replace cache: %q", cached)
	}

	post, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/widgets", nil)
	if err != nil {
		t.Fatalf("construct invalidation request: %v", err)
	}
	response, err := client.Do(post)
	if err != nil {
		t.Fatalf("execute invalidation request: %v", err)
	}
	_ = response.Body.Close()
	if afterInvalidation := get(CacheModeDefault); afterInvalidation != "version:5" {
		t.Fatalf("invalidation body = %q", afterInvalidation)
	}
	if calls.Load() != 5 {
		t.Fatalf("origin calls = %d, want 5", calls.Load())
	}
}

func TestCacheStoreNeverReceivesRawVaryCredentialValues(t *testing.T) {
	t.Parallel()

	const secret = "Bearer raw-vary-secret"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Cache-Control", "public, max-age=60")
		writer.Header().Set("Vary", "Authorization")
		_, _ = io.WriteString(writer, "credential-scoped")
	}))
	t.Cleanup(server.Close)

	memory, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	store := &inspectingCacheStore{CacheStore: memory, inspect: func(entry CacheEntry) error {
		if strings.Contains(fmt.Sprintf("%#v", entry), secret) {
			return fmt.Errorf("cache entry contains raw credential")
		}
		return nil
	}}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "credential-vary-cache", Layer: MiddlewareClient, Store: store,
		Shared: true,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	request.Header.Set("Authorization", secret)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	_ = response.Body.Close()
	if store.failure != nil {
		t.Fatal(store.failure)
	}
}

func TestCacheMiddlewareDoesNotCrossCookieIdentityBoundaries(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		writer.Header().Set("Cache-Control", "max-age=60")
		if request.URL.Path == "/set-cookie" {
			writer.Header().Set("Set-Cookie", fmt.Sprintf("session=%d; Path=/", call))
		}
		_, _ = fmt.Fprintf(writer, "%s:%d", request.Header.Get("Cookie"), call)
	}))
	t.Cleanup(server.Close)

	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "cookie-safe-cache", Layer: MiddlewareClient, Store: store,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request := func(path string, cookie string) string {
		req, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+path, nil)
		if requestErr != nil {
			t.Fatalf("construct request: %v", requestErr)
		}
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		response, doErr := client.Do(req)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		defer func() { _ = response.Body.Close() }()
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("read response: %v", readErr)
		}

		return string(body)
	}

	first := request("/cookie", "session=account-a")
	second := request("/cookie", "session=account-b")
	third := request("/set-cookie", "")
	fourth := request("/set-cookie", "")
	if first == second || third == fourth || calls.Load() != 4 {
		t.Fatalf("cookie bodies = %q, %q, %q, %q; calls = %d", first, second, third, fourth, calls.Load())
	}
}

func TestCacheMiddlewareRequiresExplicitMethodAndStatusOptIn(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		writer.Header().Set("Cache-Control", "max-age=60")
		writer.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(writer, "%s:%d", request.Method, call)
	}))
	t.Cleanup(server.Close)

	requestTwice := func(options CacheOptions, method string) (string, string) {
		store, storeErr := NewMemoryCache(MemoryCacheOptions{})
		if storeErr != nil {
			t.Fatalf("construct memory cache: %v", storeErr)
		}
		options.Name = "method-status-cache"
		options.Layer = MiddlewareClient
		options.Store = store
		middleware, middlewareErr := NewCacheMiddleware(options)
		if middlewareErr != nil {
			t.Fatalf("construct cache middleware: %v", middlewareErr)
		}
		client, clientErr := New(Config{Middleware: []Middleware{middleware}})
		if clientErr != nil {
			t.Fatalf("construct client: %v", clientErr)
		}
		defer func() { _ = client.Close() }()
		request := func() string {
			req, requestErr := http.NewRequestWithContext(context.Background(), method, server.URL, nil)
			if requestErr != nil {
				t.Fatalf("construct request: %v", requestErr)
			}
			response, doErr := client.Do(req)
			if doErr != nil {
				t.Fatalf("execute request: %v", doErr)
			}
			defer func() { _ = response.Body.Close() }()
			body, readErr := io.ReadAll(response.Body)
			if readErr != nil {
				t.Fatalf("read response: %v", readErr)
			}

			return string(body)
		}

		return request(), request()
	}

	first, second := requestTwice(CacheOptions{}, http.MethodGet)
	if first == second {
		t.Fatalf("default cache reused status 201: %q", first)
	}
	third, fourth := requestTwice(CacheOptions{
		Methods:  []string{http.MethodPost},
		Statuses: []int{http.StatusCreated},
	}, http.MethodPost)
	if third != fourth || calls.Load() != 3 {
		t.Fatalf("opt-in bodies = %q, %q; calls = %d", third, fourth, calls.Load())
	}
}

func TestMemoryCacheIsFiniteCopyingAndContextAware(t *testing.T) {
	t.Parallel()

	fixedKey := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1_700_000_000, 0)
	entry := func(value string, varyValue string) CacheEntry {
		header := http.Header{"Cache-Control": {"max-age=60"}, "X-Value": {value}}
		vary := []string{"Accept"}
		requestHeader := http.Header{"Accept": {varyValue}}
		return CacheEntry{
			StatusCode: http.StatusOK, Header: header, Body: []byte(value),
			StoredAt: now, RequestTime: now, ResponseTime: now,
			Vary: vary, VariantID: cacheVariantIdentity(fixedKey, vary, requestHeader),
		}
	}

	cache, err := NewMemoryCache(MemoryCacheOptions{MaximumEntries: 2, MaximumBytes: 4})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	first := entry("aa", "a")
	if err := cache.Save(context.Background(), "first", first); err != nil {
		t.Fatalf("save first: %v", err)
	}
	first.Body[0] = 'x'
	first.Header.Set("X-Value", "mutated")
	loaded, err := cache.Load(context.Background(), "first")
	if err != nil || len(loaded) != 1 || string(loaded[0].Body) != "aa" || loaded[0].Header.Get("X-Value") != "aa" {
		t.Fatalf("loaded first = %#v, %v", loaded, err)
	}
	loaded[0].Body[0] = 'y'
	loaded[0].Header.Set("X-Value", "changed")
	loadedAgain, _ := cache.Load(context.Background(), "first")
	if string(loadedAgain[0].Body) != "aa" || loadedAgain[0].Header.Get("X-Value") != "aa" {
		t.Fatalf("load aliases cache entry: %#v", loadedAgain)
	}

	if err := cache.Save(context.Background(), "second", entry("bb", "b")); err != nil {
		t.Fatalf("save second: %v", err)
	}
	if err := cache.Save(context.Background(), "third", entry("c", "c")); err != nil {
		t.Fatalf("save third: %v", err)
	}
	if evicted, _ := cache.Load(context.Background(), "first"); len(evicted) != 0 {
		t.Fatalf("oldest entry was not evicted: %#v", evicted)
	}

	variantA := entry("a", "a")
	variantB := entry("b", "b")
	if err := cache.Save(context.Background(), "variants", variantA); err != nil {
		t.Fatalf("save variant A: %v", err)
	}
	if err := cache.Save(context.Background(), "variants", variantB); err != nil {
		t.Fatalf("save variant B: %v", err)
	}
	variantA.Body = []byte("z")
	if err := cache.Save(context.Background(), "variants", variantA); err != nil {
		t.Fatalf("replace variant A: %v", err)
	}
	variants, err := cache.Load(context.Background(), "variants")
	if err != nil || len(variants) != 2 {
		t.Fatalf("variants = %#v, %v", variants, err)
	}
	if err := cache.Delete(context.Background(), "variants"); err != nil {
		t.Fatalf("delete variants: %v", err)
	}
	if variants, _ := cache.Load(context.Background(), "variants"); len(variants) != 0 {
		t.Fatalf("deleted variants = %#v", variants)
	}
	filterCache, _ := NewMemoryCache(MemoryCacheOptions{MaximumEntries: 3, MaximumBytes: 16})
	_ = filterCache.Save(context.Background(), "keep", entry("k", "k"))
	_ = filterCache.Save(context.Background(), "delete", entry("d", "d"))
	_ = filterCache.Delete(context.Background(), "delete")
	if kept, _ := filterCache.Load(context.Background(), "keep"); len(kept) != 1 {
		t.Fatalf("delete removed unrelated entry: %#v", kept)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, operation := range map[string]func(context.Context) error{
		"load":   func(ctx context.Context) error { _, loadErr := cache.Load(ctx, "key"); return loadErr },
		"save":   func(ctx context.Context) error { return cache.Save(ctx, "key", entry("a", "a")) },
		"delete": func(ctx context.Context) error { return cache.Delete(ctx, "key") },
	} {
		if err := operation(canceled); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s canceled error = %v", name, err)
		}
	}
}

func TestMemoryCacheRejectsInvalidBoundsMetadataAndContexts(t *testing.T) {
	t.Parallel()

	for _, options := range []MemoryCacheOptions{
		{MaximumEntries: -1},
		{MaximumEntries: maximumMemoryCacheEntries + 1},
		{MaximumBytes: -1},
	} {
		if _, err := NewMemoryCache(options); !errors.Is(err, ErrInvalidCache) {
			t.Fatalf("invalid memory options error = %v", err)
		}
	}
	cache, err := NewMemoryCache(MemoryCacheOptions{MaximumBytes: 1})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	var nilContext context.Context
	if _, err := cache.Load(nilContext, "key"); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("nil load context error = %v", err)
	}
	if err := cache.Save(nilContext, "key", CacheEntry{}); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("nil save context error = %v", err)
	}
	if err := cache.Delete(nilContext, "key"); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("nil delete context error = %v", err)
	}
	if err := cache.Save(context.Background(), "", CacheEntry{}); !errors.Is(err, ErrCacheLimit) {
		t.Fatalf("empty key error = %v", err)
	}
	if err := cache.Save(context.Background(), "key", CacheEntry{Body: []byte("xx")}); !errors.Is(err, ErrCacheLimit) {
		t.Fatalf("oversized entry error = %v", err)
	}
	if err := cache.Save(context.Background(), "key", CacheEntry{}); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("invalid variant error = %v", err)
	}
}

func TestCachePolicyRejectsInvalidConfigurationAndMetadataAccess(t *testing.T) {
	t.Parallel()

	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	var nilStore *MemoryCache
	var nilClock *cacheTestClock
	for _, options := range []CacheOptions{
		{Name: "cache", Store: nilStore},
		{Name: "cache", Store: store, Namespace: "invalid namespace"},
		{Name: "cache", Store: store, Namespace: strings.Repeat("x", maximumCacheNamespaceLength+1)},
		{Name: "cache", Store: store, MaximumBodyBytes: -1},
		{Name: "cache", Store: store, TTLOverride: -1},
		{Name: "cache", Store: store, Clock: nilClock},
		{Name: "cache", Store: store, VariantKey: []byte("short")},
		{Name: "cache", Store: store, Methods: []string{"bad method"}},
		{Name: "cache", Store: store, Methods: []string{http.MethodGet, http.MethodGet}},
		{Name: "cache", Store: store, Statuses: []int{199}},
		{Name: "cache", Store: store, Statuses: []int{http.StatusOK, http.StatusOK}},
		{Name: "cache", Store: store, FailureMode: CacheFailureMode(99)},
	} {
		if _, err := NewCacheMiddleware(options); !errors.Is(err, ErrInvalidCache) {
			t.Fatalf("invalid cache options error = %v", err)
		}
	}
	if _, err := NewCacheMiddleware(CacheOptions{Name: "INVALID", Store: store}); !errors.Is(err, ErrInvalidMiddleware) {
		t.Fatalf("invalid middleware metadata error = %v", err)
	}
	var nilContext context.Context
	if _, err := WithCacheMode(nilContext, CacheModeDefault); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("nil cache mode context error = %v", err)
	}
	if _, err := WithCacheMode(context.Background(), CacheMode(99)); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("invalid cache mode error = %v", err)
	}
	if metadata, ok := CacheMetadataFromResponse(nil); ok || metadata != (CacheMetadata{}) {
		t.Fatalf("nil response metadata = %#v, %t", metadata, ok)
	}
	if metadata, ok := CacheMetadataFromResponse(&http.Response{}); ok || metadata != (CacheMetadata{}) {
		t.Fatalf("requestless metadata = %#v, %t", metadata, ok)
	}
}

func TestCacheVariantKeyGenerationFailureIsTyped(t *testing.T) {
	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	original := cacheRandomReader
	cacheRandomReader = &cacheErrorReader{err: errors.New("entropy failure")}
	t.Cleanup(func() { cacheRandomReader = original })
	if _, err := NewCacheMiddleware(CacheOptions{Name: "cache", Store: store}); !errors.Is(err, ErrInvalidCache) {
		t.Fatalf("variant key generation error = %v", err)
	}
}

func TestCacheMiddlewareHonorsOnlyIfCachedMaxStaleAndStaleIfError(t *testing.T) {
	t.Parallel()

	clock := &cacheTestClock{now: time.Unix(1_700_000_000, 0)}
	var calls atomic.Int64
	var errorCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		call := calls.Add(1)
		if request.URL.Path == "/error" && errorCalls.Add(1) > 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Cache-Control", "max-age=1, stale-if-error=30")
		_, _ = fmt.Fprintf(writer, "%s:%d", request.URL.Path, call)
	}))
	t.Cleanup(server.Close)

	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "stale-cache", Layer: MiddlewareClient, Store: store, Clock: clock,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request := func(path string, cacheControl string) (*http.Response, string) {
		req, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+path, nil)
		if requestErr != nil {
			t.Fatalf("construct request: %v", requestErr)
		}
		if cacheControl != "" {
			req.Header.Set("Cache-Control", cacheControl)
		}
		response, doErr := client.Do(req)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("read response: %v", readErr)
		}
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Fatalf("close response: %v", closeErr)
		}

		return response, string(body)
	}

	miss, missBody := request("/missing", "only-if-cached")
	if miss.StatusCode != http.StatusGatewayTimeout || missBody != "" || calls.Load() != 0 {
		t.Fatalf("only-if-cached miss = %d %q; calls = %d", miss.StatusCode, missBody, calls.Load())
	}
	_, staleBody := request("/stale", "")
	clock.Advance(2 * time.Second)
	stale, reusedBody := request("/stale", "max-stale=10")
	if reusedBody != staleBody || calls.Load() != 1 {
		t.Fatalf("max-stale body = %q, want %q; calls = %d", reusedBody, staleBody, calls.Load())
	}
	if metadata, ok := CacheMetadataFromResponse(stale); !ok || metadata.Provenance != CacheStale {
		t.Fatalf("max-stale metadata = %#v, %t", metadata, ok)
	}

	_, errorBody := request("/error", "")
	clock.Advance(2 * time.Second)
	fallback, fallbackBody := request("/error", "")
	if fallback.StatusCode != http.StatusOK || fallbackBody != errorBody || calls.Load() != 3 {
		t.Fatalf("stale-if-error = %d %q, want %q; calls = %d", fallback.StatusCode, fallbackBody, errorBody, calls.Load())
	}
	if metadata, ok := CacheMetadataFromResponse(fallback); !ok || metadata.Provenance != CacheStale {
		t.Fatalf("stale-if-error metadata = %#v, %t", metadata, ok)
	}
}

func TestCacheVaryMatchingNormalizesEquivalentHeaderLineLayout(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.Header().Set("Cache-Control", "max-age=60")
		writer.Header().Set("Vary", "Accept")
		_, _ = io.WriteString(writer, "representation")
	}))
	t.Cleanup(server.Close)

	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "normalized-vary-cache", Layer: MiddlewareClient, Store: store,
		VariantKey: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	first, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	first.Header.Set("Accept", "text/plain, application/json")
	second, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	second.Header.Add("Accept", "text/plain")
	second.Header.Add("Accept", "application/json")
	for _, request := range []*http.Request{first, second} {
		response, doErr := client.Do(request)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	if calls.Load() != 1 {
		t.Fatalf("origin calls = %d, want 1", calls.Load())
	}
}

func TestCacheValidationDoesNotReplaceRepresentationDependentHeaders(t *testing.T) {
	t.Parallel()

	stored := http.Header{
		"Content-Length": {"4"},
		"Connection":     {"keep-alive"},
		"Vary":           {"Accept"},
		"X-Version":      {"one"},
	}
	validation := http.Header{
		"Content-Length": {"0"},
		"Connection":     {"close"},
		"Vary":           {"Authorization"},
		"X-Version":      {"two"},
	}
	updateCachedHeaders(stored, validation)
	if stored.Get("Content-Length") != "4" || stored.Get("Connection") != "keep-alive" ||
		stored.Get("Vary") != "Accept" || stored.Get("X-Version") != "two" {
		t.Fatalf("freshened headers = %#v", stored)
	}
}

func TestCacheControlParsingPreservesQuotedListsAndFirstDuplicate(t *testing.T) {
	t.Parallel()

	directives := parseCacheControl([]string{
		`private="Set-Cookie, Authorization", max-age=60, max-age=1`,
	})
	if directives["private"] != "Set-Cookie, Authorization" || directives["max-age"] != "60" {
		t.Fatalf("cache directives = %#v", directives)
	}
}

func TestCacheNoStoreRefreshRemovesPreviouslyStoredResponse(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		if call == 2 {
			writer.Header().Set("Cache-Control", "no-store")
		} else {
			writer.Header().Set("Cache-Control", "max-age=60")
		}
		_, _ = fmt.Fprintf(writer, "version:%d", call)
	}))
	t.Cleanup(server.Close)

	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "no-store-cache", Layer: MiddlewareClient, Store: store,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	request := func(mode CacheMode) string {
		ctx, _ := WithCacheMode(context.Background(), mode)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		response, doErr := client.Do(req)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		defer func() { _ = response.Body.Close() }()
		body, _ := io.ReadAll(response.Body)
		return string(body)
	}

	first := request(CacheModeDefault)
	second := request(CacheModeRefresh)
	third := request(CacheModeDefault)
	if first != "version:1" || second != "version:2" || third != "version:3" || calls.Load() != 3 {
		t.Fatalf("versions = %q, %q, %q; calls = %d", first, second, third, calls.Load())
	}
}

func TestCacheStoreFailuresAreFailOpenByDefaultAndTypedWhenClosed(t *testing.T) {
	t.Parallel()

	secret := errors.New("cache-backend-secret")
	openStore := &failingCacheStore{loadErr: secret, saveErr: secret}
	openMiddleware, err := NewCacheMiddleware(CacheOptions{
		Name: "open-cache", Layer: MiddlewareClient, Store: openStore,
	})
	if err != nil {
		t.Fatalf("construct fail-open cache: %v", err)
	}
	openClient, err := New(Config{
		Middleware: []Middleware{openMiddleware},
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Cache-Control": {"max-age=60"}},
				Body:       io.NopCloser(strings.NewReader("origin")),
				Request:    request,
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct fail-open client: %v", err)
	}
	t.Cleanup(func() { _ = openClient.Close() })
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
	response, err := openClient.Do(request)
	if err != nil {
		t.Fatalf("fail-open request: %v", err)
	}
	_ = response.Body.Close()

	closedStore := &failingCacheStore{loadErr: secret}
	closedMiddleware, err := NewCacheMiddleware(CacheOptions{
		Name: "closed-cache", Layer: MiddlewareClient, Store: closedStore,
		FailureMode: CacheFailClosed,
	})
	if err != nil {
		t.Fatalf("construct fail-closed cache: %v", err)
	}
	closedClient, err := New(Config{
		Middleware: []Middleware{closedMiddleware},
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("fail-closed load reached transport")
			return nil, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct fail-closed client: %v", err)
	}
	t.Cleanup(func() { _ = closedClient.Close() })
	request, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
	_, err = closedClient.Do(request)
	var cacheError *CacheError
	if !errors.As(err, &cacheError) || !errors.Is(err, secret) || strings.Contains(err.Error(), secret.Error()) {
		t.Fatalf("fail-closed error = %#v", err)
	}

	for _, test := range []struct {
		name   string
		method string
		store  *failingCacheStore
	}{
		{name: "save", method: http.MethodGet, store: &failingCacheStore{saveErr: secret}},
		{name: "delete", method: http.MethodPost, store: &failingCacheStore{deleteErr: secret}},
	} {
		t.Run(test.name+" failure closes origin", func(t *testing.T) {
			var closed atomic.Int64
			middleware, middlewareErr := NewCacheMiddleware(CacheOptions{
				Name: "closed-" + test.name, Layer: MiddlewareClient,
				Store: test.store, FailureMode: CacheFailClosed,
			})
			if middlewareErr != nil {
				t.Fatalf("construct middleware: %v", middlewareErr)
			}
			client, clientErr := New(Config{
				Middleware: []Middleware{middleware},
				Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode:    http.StatusOK,
						Header:        http.Header{"Cache-Control": {"max-age=60"}},
						Body:          &cacheTestBody{Reader: strings.NewReader("body"), closed: &closed},
						ContentLength: -1,
						Request:       request,
					}, nil
				}),
			})
			if clientErr != nil {
				t.Fatalf("construct client: %v", clientErr)
			}
			defer func() { _ = client.Close() }()
			request, _ := http.NewRequestWithContext(context.Background(), test.method, "https://example.test", nil)
			_, doErr := client.Do(request)
			var cacheError *CacheError
			if !errors.As(doErr, &cacheError) || !errors.Is(doErr, secret) || closed.Load() != 1 {
				t.Fatalf("%s failure = %#v, closes = %d", test.name, doErr, closed.Load())
			}
		})
	}
}

func TestCacheBodyCapturePreservesBoundsAndReturnsTypedFailures(t *testing.T) {
	t.Parallel()

	t.Run("unknown oversized body remains complete", func(t *testing.T) {
		var calls atomic.Int64
		var closed atomic.Int64
		client := newCacheTestClient(t, 4, func(request *http.Request) (*http.Response, error) {
			calls.Add(1)
			return &http.Response{
				StatusCode:    http.StatusOK,
				Header:        http.Header{"Cache-Control": {"max-age=60"}},
				Body:          &cacheTestBody{Reader: strings.NewReader("complete-body"), closed: &closed},
				ContentLength: -1,
				Request:       request,
			}, nil
		})
		for range 2 {
			response := cacheClientRequest(t, client)
			body, err := io.ReadAll(response.Body)
			if err != nil || string(body) != "complete-body" {
				t.Fatalf("oversized body = %q, %v", body, err)
			}
			_ = response.Body.Close()
		}
		if calls.Load() != 2 || closed.Load() != 2 {
			t.Fatalf("oversized calls = %d, closes = %d", calls.Load(), closed.Load())
		}
	})

	t.Run("known oversized body is not consumed", func(t *testing.T) {
		var reads atomic.Int64
		client := newCacheTestClient(t, 4, func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Cache-Control": {"max-age=60"}},
				Body: &cacheTestBody{
					Reader: strings.NewReader("known-size"), reads: &reads,
				},
				ContentLength: 10,
				Request:       request,
			}, nil
		})
		response := cacheClientRequest(t, client)
		if reads.Load() != 0 {
			t.Fatalf("cache read known oversized body %d times", reads.Load())
		}
		_ = response.Body.Close()
	})

	for _, test := range []struct {
		name string
		body io.ReadCloser
	}{
		{name: "read", body: &cacheTestBody{Reader: &cacheErrorReader{err: errors.New("read-secret")}}},
		{name: "close", body: &cacheTestBody{Reader: strings.NewReader("body"), closeErr: errors.New("close-secret")}},
	} {
		t.Run(test.name+" failure", func(t *testing.T) {
			client := newCacheTestClient(t, 64, func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Cache-Control": {"max-age=60"}},
					Body:       test.body,
					Request:    request,
				}, nil
			})
			request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
			_, err := client.Do(request)
			var cacheError *CacheError
			if !errors.As(err, &cacheError) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("body failure = %#v", err)
			}
		})
	}
}

func TestCacheCoalescedWaiterHonorsCancellation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		writer.Header().Set("Cache-Control", "max-age=60")
		_, _ = io.WriteString(writer, "leader")
	}))
	t.Cleanup(server.Close)
	memory, _ := NewMemoryCache(MemoryCacheOptions{})
	secondLoad := make(chan struct{})
	store := &signalingCacheStore{CacheStore: memory, secondLoad: secondLoad}
	middleware, _ := NewCacheMiddleware(CacheOptions{
		Name: "cancelable-cache", Layer: MiddlewareClient, Store: store,
	})
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	leaderDone := make(chan error, 1)
	go func() {
		request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
		response, doErr := client.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		leaderDone <- doErr
	}()
	<-started
	waiterContext, cancel := context.WithCancel(context.Background())
	waiter, _ := http.NewRequestWithContext(waiterContext, http.MethodGet, server.URL, nil)
	waiterDone := make(chan error, 1)
	go func() {
		_, doErr := client.Do(waiter)
		waiterDone <- doErr
	}()
	<-secondLoad
	cancel()
	if err := <-waiterDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter cancellation error = %v", err)
	}
	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader error = %v", err)
	}
}

func TestCacheDoesNotStoreContentLengthMismatch(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	client := newCacheTestClient(t, 64, func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Cache-Control": {"max-age=60"}},
			Body:          io.NopCloser(strings.NewReader("short")),
			ContentLength: 10,
			Request:       request,
		}, nil
	})
	for range 2 {
		response := cacheClientRequest(t, client)
		body, err := io.ReadAll(response.Body)
		if err != nil || string(body) != "short" {
			t.Fatalf("mismatched body = %q, %v", body, err)
		}
		_ = response.Body.Close()
	}
	if calls.Load() != 2 {
		t.Fatalf("origin calls = %d, want 2", calls.Load())
	}
}

func TestCacheFreshnessAgeAndStaleDirectiveBoundaries(t *testing.T) {
	t.Parallel()

	base := time.Unix(1_700_000_000, 0)
	clock := &cacheTestClock{now: base}
	policy := &cachePolicy{clock: clock}
	entry := CacheEntry{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Cache-Control": {"max-age=60"},
			"Date":          {base.Add(-5 * time.Second).Format(http.TimeFormat)},
			"Age":           {"10"},
		},
		RequestTime:  base.Add(-2 * time.Second),
		ResponseTime: base,
	}
	if age := policy.currentAge(entry); age != 12*time.Second {
		t.Fatalf("current age = %s, want 12s", age)
	}
	request := cacheUnitRequest(t, "")
	if !policy.freshForRequest(request, entry) {
		t.Fatal("explicit max-age response is not fresh")
	}
	request = cacheUnitRequest(t, "max-age=0")
	if policy.freshForRequest(request, entry) {
		t.Fatal("request max-age=0 reused cached response")
	}
	request = cacheUnitRequest(t, "min-fresh=50")
	if policy.freshForRequest(request, entry) {
		t.Fatal("request min-fresh reused insufficient lifetime")
	}

	expiresEntry := entry
	expiresEntry.Header = http.Header{
		"Date":    {base.Format(http.TimeFormat)},
		"Expires": {base.Add(time.Minute).Format(http.TimeFormat)},
	}
	expiresEntry.RequestTime = base
	if !policy.freshForRequest(cacheUnitRequest(t, ""), expiresEntry) {
		t.Fatal("Expires response is not fresh")
	}
	policy.ttlOverride = time.Minute
	noFreshness := entry
	noFreshness.Header = make(http.Header)
	noFreshness.RequestTime = base
	if !policy.freshForRequest(cacheUnitRequest(t, ""), noFreshness) {
		t.Fatal("TTL override response is not fresh")
	}
	policy.ttlOverride = 0
	if policy.freshForRequest(cacheUnitRequest(t, ""), noFreshness) {
		t.Fatal("response without freshness is fresh")
	}

	policy.shared = true
	sharedEntry := entry
	sharedEntry.Header = http.Header{"Cache-Control": {"max-age=60, s-maxage=1"}}
	sharedEntry.RequestTime = base
	clock.Advance(2 * time.Second)
	if policy.freshForRequest(cacheUnitRequest(t, ""), sharedEntry) {
		t.Fatal("shared cache ignored s-maxage")
	}
	policy.shared = false
	if !policy.freshForRequest(cacheUnitRequest(t, ""), sharedEntry) {
		t.Fatal("private cache applied s-maxage")
	}

	staleEntry := entry
	staleEntry.Header = http.Header{"Cache-Control": {"max-age=1, stale-if-error=30"}}
	staleEntry.RequestTime = base
	staleEntry.ResponseTime = base
	if !policy.requestPermitsStale(cacheUnitRequest(t, "max-stale"), staleEntry) {
		t.Fatal("unbounded max-stale was not honored")
	}
	if policy.requestPermitsStale(cacheUnitRequest(t, "max-stale=invalid"), staleEntry) {
		t.Fatal("invalid max-stale was honored")
	}
	if policy.requestPermitsStale(cacheUnitRequest(t, ""), staleEntry) {
		t.Fatal("stale response reused without permission")
	}
	if !policy.staleIfError(staleEntry) {
		t.Fatal("stale-if-error was not honored")
	}
	staleEntry.Header.Set("Cache-Control", "max-age=1, stale-if-error=invalid")
	if policy.staleIfError(staleEntry) {
		t.Fatal("invalid stale-if-error was honored")
	}
	for _, directive := range []string{"no-cache", "must-revalidate"} {
		staleEntry.Header.Set("Cache-Control", "max-age=1, stale-if-error=30, "+directive)
		if policy.staleIfError(staleEntry) {
			t.Fatalf("%s permitted stale-if-error", directive)
		}
	}
	policy.shared = true
	for _, directive := range []string{"proxy-revalidate", "s-maxage=1"} {
		staleEntry.Header.Set("Cache-Control", "max-age=1, stale-if-error=30, "+directive)
		if policy.staleIfError(staleEntry) {
			t.Fatalf("%s permitted shared stale-if-error", directive)
		}
	}
}

func TestCacheHeaderAndMetadataHelpersRejectMalformedState(t *testing.T) {
	t.Parallel()

	if withCacheMetadata(nil, CacheMetadata{}) != nil {
		t.Fatal("nil response gained cache metadata")
	}
	response := &http.Response{}
	if withCacheMetadata(response, CacheMetadata{}) != response {
		t.Fatal("requestless response was replaced")
	}
	for _, entry := range []CacheEntry{
		{},
		{StatusCode: 99, Header: make(http.Header), RequestTime: time.Now(), ResponseTime: time.Now(), VariantID: strings.Repeat("0", 64)},
		{StatusCode: 200, Header: make(http.Header), RequestTime: time.Now(), ResponseTime: time.Now(), VariantID: strings.Repeat("0", 64), Vary: []string{"lowercase"}},
	} {
		if validCacheEntry(entry) {
			t.Fatalf("malformed entry accepted: %#v", entry)
		}
	}
	if vary, ok := parseVary(http.Header{"Vary": {"*"}}); ok || vary != nil {
		t.Fatalf("wildcard Vary = %#v, %t", vary, ok)
	}
	vary, ok := parseVary(http.Header{"Vary": {" Accept, accept, ", "X-Mode"}})
	if !ok || len(vary) != 2 || vary[0] != "Accept" || vary[1] != "X-Mode" {
		t.Fatalf("normalized Vary = %#v, %t", vary, ok)
	}
	parts := splitQuotedList(`one,"two,\\\"three",four`)
	if len(parts) != 3 {
		t.Fatalf("quoted list = %#v", parts)
	}
	if age := parseAge("invalid"); age != 0 {
		t.Fatalf("invalid Age = %s", age)
	}
	if validHTTPToken("bad\x7fmethod") || validHTTPToken("") {
		t.Fatal("invalid HTTP token accepted")
	}
}

func TestCacheFetchFailureAndFallbackBoundaries(t *testing.T) {
	t.Parallel()

	base := time.Unix(1_700_000_000, 0)
	dependencyFailure := errors.New("dependency-secret")
	stale := CacheEntry{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Cache-Control": {"max-age=1, stale-if-error=30"}},
		Body:       []byte("stale"), RequestTime: base, ResponseTime: base,
	}
	request := cacheUnitRequest(t, "")

	t.Run("transport error uses permitted stale response", func(t *testing.T) {
		policy := cacheUnitPolicy(&failingCacheStore{}, base.Add(2*time.Second), CacheFailOpen)
		response, err := policy.fetch(request, func(*http.Request) (*http.Response, error) {
			return nil, dependencyFailure
		}, "key", &stale)
		if err != nil || response == nil {
			t.Fatalf("stale transport fallback = %#v, %v", response, err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if string(body) != "stale" {
			t.Fatalf("stale transport body = %q", body)
		}
	})

	t.Run("transport error without stale is preserved", func(t *testing.T) {
		policy := cacheUnitPolicy(&failingCacheStore{}, base, CacheFailOpen)
		if _, err := policy.fetch(request, func(*http.Request) (*http.Response, error) {
			return nil, dependencyFailure
		}, "key", nil); !errors.Is(err, dependencyFailure) {
			t.Fatalf("transport error = %v", err)
		}
	})

	for _, status := range []int{
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		t.Run(fmt.Sprintf("status %d uses stale", status), func(t *testing.T) {
			policy := cacheUnitPolicy(&failingCacheStore{}, base.Add(2*time.Second), CacheFailOpen)
			response, err := policy.fetch(request, func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status, Header: make(http.Header), Body: http.NoBody, Request: request,
				}, nil
			}, "key", &stale)
			if err != nil || response.StatusCode != http.StatusOK {
				t.Fatalf("status fallback = %#v, %v", response, err)
			}
			_ = response.Body.Close()
		})
	}

	for _, test := range []struct {
		name    string
		store   *failingCacheStore
		status  int
		header  http.Header
		body    io.ReadCloser
		stale   *CacheEntry
		want    error
		failure CacheFailureMode
	}{
		{
			name: "stale fallback close", status: http.StatusServiceUnavailable,
			header: make(http.Header), body: &cacheTestBody{Reader: strings.NewReader("failure"), closeErr: dependencyFailure},
			stale: &stale, want: dependencyFailure,
		},
		{
			name: "validation close", status: http.StatusNotModified,
			header: make(http.Header), body: &cacheTestBody{Reader: strings.NewReader(""), closeErr: dependencyFailure},
			stale: &stale, want: dependencyFailure,
		},
		{
			name: "validation save", store: &failingCacheStore{saveErr: dependencyFailure},
			status: http.StatusNotModified, header: make(http.Header), body: http.NoBody,
			stale: &stale, want: dependencyFailure, failure: CacheFailClosed,
		},
		{
			name: "no-store delete", store: &failingCacheStore{deleteErr: dependencyFailure},
			status: http.StatusOK, header: http.Header{"Cache-Control": {"no-store"}},
			body: http.NoBody, want: dependencyFailure, failure: CacheFailClosed,
		},
		{
			name: "replacement delete", store: &failingCacheStore{deleteErr: dependencyFailure},
			status: http.StatusOK, header: http.Header{"Cache-Control": {"max-age=60"}},
			body: io.NopCloser(strings.NewReader("new")), stale: &stale,
			want: dependencyFailure, failure: CacheFailClosed,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.store == nil {
				test.store = &failingCacheStore{}
			}
			policy := cacheUnitPolicy(test.store, base.Add(2*time.Second), test.failure)
			_, err := policy.fetch(request, func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: test.status, Header: test.header, Body: test.body,
					ContentLength: -1, Request: request,
				}, nil
			}, "key", test.stale)
			var cacheError *CacheError
			if !errors.Is(err, test.want) || !errors.As(err, &cacheError) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("fetch boundary error = %#v", err)
			}
		})
	}
}

func TestCacheCustomKeysAreHashedBoundedAndSecretSafe(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.Header().Set("Cache-Control", "max-age=60")
		_, _ = io.WriteString(writer, "custom-key")
	}))
	t.Cleanup(server.Close)
	memory, _ := NewMemoryCache(MemoryCacheOptions{})
	store := &inspectingPrimaryKeyStore{CacheStore: memory}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "custom-key-cache", Layer: MiddlewareClient, Store: store,
		Key: func(request *http.Request) (string, error) {
			return request.Method + ":" + request.URL.Path, nil
		},
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for _, query := range []string{"?volatile=one", "?volatile=two"} {
		request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/widgets"+query, nil)
		response, doErr := client.Do(request)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		_ = response.Body.Close()
	}
	if calls.Load() != 1 || strings.Contains(store.key, "/widgets") || strings.Contains(store.key, "volatile") {
		t.Fatalf("calls = %d, backend key = %q", calls.Load(), store.key)
	}

	secret := errors.New("key-secret")
	failing, err := NewCacheMiddleware(CacheOptions{
		Name: "failing-key-cache", Layer: MiddlewareClient, Store: memory,
		Key: func(*http.Request) (string, error) { return "", secret },
	})
	if err != nil {
		t.Fatalf("construct failing key middleware: %v", err)
	}
	failingClient, err := New(Config{Middleware: []Middleware{failing}})
	if err != nil {
		t.Fatalf("construct failing key client: %v", err)
	}
	t.Cleanup(func() { _ = failingClient.Close() })
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	_, err = failingClient.Do(request)
	var cacheError *CacheError
	if !errors.As(err, &cacheError) || !errors.Is(err, secret) || strings.Contains(err.Error(), secret.Error()) {
		t.Fatalf("custom key error = %#v", err)
	}
}

func TestCacheStaleWhileRevalidateUsesApplicationOwnedScheduler(t *testing.T) {
	t.Parallel()

	base := time.Unix(1_700_000_000, 0)
	clock := &cacheTestClock{now: base}
	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	scheduler := &cacheTestScheduler{tasks: make(chan func(context.Context), 1)}
	var calls atomic.Int64
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "stale-while-revalidate", Layer: MiddlewareClient, Store: store,
		Clock: clock, RevalidationScheduler: scheduler,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			call := calls.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Cache-Control": {"max-age=1, stale-while-revalidate=30"},
				},
				Body:    io.NopCloser(strings.NewReader(fmt.Sprintf("body-%d", call))),
				Request: request, ContentLength: -1,
			}, nil
		}),
		Middleware: []Middleware{middleware},
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	readBody := func() string {
		request := cacheUnitRequest(t, "")
		response, doErr := client.Do(request)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("read response: %v", readErr)
		}
		_ = response.Body.Close()
		return string(body)
	}

	if got := readBody(); got != "body-1" {
		t.Fatalf("initial body = %q", got)
	}
	clock.Advance(2 * time.Second)
	if got := readBody(); got != "body-1" {
		t.Fatalf("stale body = %q", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("origin calls before task = %d", calls.Load())
	}
	task := <-scheduler.tasks
	task(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("origin calls after task = %d", calls.Load())
	}
	if got := readBody(); got != "body-2" {
		t.Fatalf("revalidated body = %q", got)
	}
}

func TestCacheUnsafeResponseInvalidatesSameOriginReferences(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Method == http.MethodPost {
			writer.Header().Set("Location", "/related")
			writer.Header().Set("Content-Location", "https://other.test/private")
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		writer.Header().Set("Cache-Control", "max-age=60")
		_, _ = io.WriteString(writer, request.URL.Path)
	}))
	t.Cleanup(server.Close)
	store, _ := NewMemoryCache(MemoryCacheOptions{})
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "reference-invalidation", Layer: MiddlewareClient, Store: store,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	do := func(method string, path string) {
		request, requestErr := http.NewRequestWithContext(context.Background(), method, server.URL+path, nil)
		if requestErr != nil {
			t.Fatalf("construct request: %v", requestErr)
		}
		response, doErr := client.Do(request)
		if doErr != nil {
			t.Fatalf("execute request: %v", doErr)
		}
		_ = response.Body.Close()
	}
	for _, path := range []string{"/target", "/related"} {
		do(http.MethodGet, path)
		do(http.MethodGet, path)
	}
	do(http.MethodPost, "/target")
	do(http.MethodGet, "/target")
	do(http.MethodGet, "/related")
	if calls.Load() != 5 {
		t.Fatalf("origin calls = %d, want 5", calls.Load())
	}
}

func TestCacheHitPreservesStandardResponseMetadata(t *testing.T) {
	t.Parallel()

	request := cacheUnitRequest(t, "")
	entry := CacheEntry{
		StatusCode: http.StatusOK, Status: "200 Custom", Proto: "HTTP/2.0",
		ProtoMajor: 2, ProtoMinor: 0, Header: make(http.Header),
		Trailer:          http.Header{"Digest": {"sha-256=value"}},
		TransferEncoding: []string{"chunked"}, Uncompressed: true,
		Body: []byte("body"), RequestTime: time.Now(), ResponseTime: time.Now(),
		VariantID: strings.Repeat("0", 64),
	}
	response := responseFromCache(request, entry, CacheHit, 0)
	if response.Status != entry.Status || response.Proto != entry.Proto ||
		response.ProtoMajor != entry.ProtoMajor || response.ProtoMinor != entry.ProtoMinor ||
		!response.Uncompressed || response.Trailer.Get("Digest") == "" ||
		len(response.TransferEncoding) != 1 {
		t.Fatalf("cached response metadata = %#v", response)
	}
	response.Trailer.Set("Digest", "changed")
	response.TransferEncoding[0] = "changed"
	if entry.Trailer.Get("Digest") == "changed" || entry.TransferEncoding[0] == "changed" {
		t.Fatal("cached response metadata aliases stored entry")
	}
}

func TestCachePolicyBoundaryBranches(t *testing.T) {
	t.Parallel()

	base := time.Unix(1_700_000_000, 0)
	request := cacheUnitRequest(t, "")
	policy := cacheUnitPolicy(&failingCacheStore{}, base.Add(2*time.Second), CacheFailOpen)
	dependencyFailure := errors.New("dependency")
	response := func(status int) *http.Response {
		return &http.Response{
			StatusCode: status, Header: make(http.Header), Body: http.NoBody,
			Request: request, ContentLength: 0,
		}
	}

	t.Run("bypass and non-cacheable transport errors", func(t *testing.T) {
		bypass := request.Clone(context.Background())
		bypass.Header.Set("Range", "bytes=0-1")
		if _, err := policy.execute(bypass, func(*http.Request) (*http.Response, error) {
			return nil, dependencyFailure
		}); !errors.Is(err, dependencyFailure) {
			t.Fatalf("bypass error = %v", err)
		}
		post := request.Clone(context.Background())
		post.Method = http.MethodPost
		if _, err := policy.execute(post, func(*http.Request) (*http.Response, error) {
			return nil, dependencyFailure
		}); !errors.Is(err, dependencyFailure) {
			t.Fatalf("non-cacheable error = %v", err)
		}
	})

	t.Run("unsafe invalidation key failures", func(t *testing.T) {
		failing := cacheUnitPolicy(&failingCacheStore{}, base, CacheFailOpen)
		failing.key = func(*http.Request) (string, error) { return "", dependencyFailure }
		post := request.Clone(context.Background())
		post.Method = http.MethodPost
		if _, err := failing.execute(post, func(*http.Request) (*http.Response, error) {
			return response(http.StatusNoContent), nil
		}); !errors.Is(err, dependencyFailure) {
			t.Fatalf("execute invalidation error = %v", err)
		}
		if _, err := failing.fetch(post, func(*http.Request) (*http.Response, error) {
			return response(http.StatusNoContent), nil
		}, "key", nil); !errors.Is(err, dependencyFailure) {
			t.Fatalf("fetch invalidation error = %v", err)
		}
	})

	t.Run("capture close and vary failures", func(t *testing.T) {
		mismatch := response(http.StatusOK)
		mismatch.Body = &cacheTestBody{Reader: strings.NewReader("body"), closeErr: dependencyFailure}
		mismatch.ContentLength = 10
		if _, _, err := policy.capture(request, mismatch, base, base); !errors.Is(err, dependencyFailure) {
			t.Fatalf("mismatch close error = %v", err)
		}
		closer := response(http.StatusOK)
		closer.Body = &cacheTestBody{Reader: strings.NewReader("body"), closeErr: dependencyFailure}
		closer.ContentLength = 4
		if _, _, err := policy.capture(request, closer, base, base); !errors.Is(err, dependencyFailure) {
			t.Fatalf("capture close error = %v", err)
		}
		wildcard := response(http.StatusOK)
		wildcard.Body = io.NopCloser(strings.NewReader("body"))
		wildcard.ContentLength = 4
		wildcard.Header.Set("Vary", "*")
		if _, complete, err := policy.capture(request, wildcard, base, base); err != nil || complete {
			t.Fatalf("wildcard capture = %t, %v", complete, err)
		}
	})

	t.Run("storage and matching exclusions", func(t *testing.T) {
		noStore := response(http.StatusOK)
		noStore.Header.Set("Cache-Control", "max-age=60, no-store")
		if policy.storable(request, noStore) {
			t.Fatal("stored no-store response")
		}
		policy.shared = true
		private := response(http.StatusOK)
		private.Header.Set("Cache-Control", "max-age=60, private")
		if policy.storable(request, private) {
			t.Fatal("stored private shared response")
		}
		credentialed := request.Clone(context.Background())
		credentialed.Header.Set("Authorization", "Bearer value")
		entry := validCacheTestEntry(base, "max-age=60")
		entry.VariantID = cacheVariantIdentity(policy.variantKey, nil, credentialed.Header)
		if _, found := policy.match(credentialed, []CacheEntry{entry}); found {
			t.Fatal("matched unauthorized shared entry")
		}
	})

	t.Run("freshness and stale exclusions", func(t *testing.T) {
		entry := validCacheTestEntry(base, "max-age=60")
		entry.Header.Set("Cache-Control", "max-age=60, no-cache")
		if policy.freshForRequest(request, entry) {
			t.Fatal("no-cache entry was fresh")
		}
		entry.Header.Set("Cache-Control", "max-age=60")
		invalidMaximum := cacheUnitRequest(t, "max-age=invalid")
		if policy.freshForRequest(invalidMaximum, entry) {
			t.Fatal("invalid request max-age was fresh")
		}
		invalidMinimum := cacheUnitRequest(t, "min-fresh=invalid")
		if policy.freshForRequest(invalidMinimum, entry) {
			t.Fatal("invalid request min-fresh was fresh")
		}
		expires := validCacheTestEntry(base, "")
		expires.Header.Set("Expires", base.Add(time.Minute).Format(http.TimeFormat))
		if _, ok := policy.freshnessLifetime(expires); !ok {
			t.Fatal("Expires without Date had no lifetime")
		}
		noLifetime := validCacheTestEntry(base, "")
		if policy.requestPermitsStale(cacheUnitRequest(t, "max-stale=30"), noLifetime) {
			t.Fatal("max-stale reused entry without lifetime")
		}
		entry.Header.Set("Cache-Control", "max-age=1, must-revalidate")
		if policy.requestPermitsStale(cacheUnitRequest(t, "max-stale"), entry) {
			t.Fatal("max-stale bypassed must-revalidate")
		}
		if policy.staleIfError(validCacheTestEntry(base, "max-age=1")) {
			t.Fatal("stale-if-error absent directive was honored")
		}
		missingLifetime := validCacheTestEntry(base, "stale-if-error=30")
		if policy.staleIfError(missingLifetime) {
			t.Fatal("stale-if-error lacked lifetime")
		}
		for _, control := range []string{
			"max-age=1, must-revalidate, stale-while-revalidate=30",
			"max-age=1",
			"max-age=1, stale-while-revalidate=invalid",
			"stale-while-revalidate=30",
		} {
			if policy.staleWhileRevalidate(validCacheTestEntry(base, control)) {
				t.Fatalf("stale-while-revalidate accepted %q", control)
			}
		}
	})

	t.Run("keys directives and origins", func(t *testing.T) {
		for _, material := range []string{"", strings.Repeat("x", maximumCacheKeyMaterial+1)} {
			policy.key = func(*http.Request) (string, error) { return material, nil }
			if _, err := policy.cacheKey(request); !errors.Is(err, ErrInvalidCache) {
				t.Fatalf("key material length %d error = %v", len(material), err)
			}
		}
		if directives := parseCacheControl([]string{",,max-age=1"}); directives["max-age"] != "1" {
			t.Fatalf("directives = %#v", directives)
		}
		left, _ := url.Parse("https://example.test/path")
		for _, raw := range []string{
			"http://example.test/path", "https://other.test/path", "https://user@example.test/path",
		} {
			right, _ := url.Parse(raw)
			if sameCacheOrigin(left, right) {
				t.Fatalf("origins unexpectedly equal: %s", raw)
			}
		}
		if sameCacheOrigin(nil, left) {
			t.Fatal("nil origin matched")
		}
		httpDefault, _ := url.Parse("http://example.test/path")
		httpExplicit, _ := url.Parse("http://example.test:80/other")
		if !sameCacheOrigin(httpDefault, httpExplicit) {
			t.Fatal("default HTTP port did not match explicit port")
		}
		customLeft, _ := url.Parse("custom://example.test/path")
		customRight, _ := url.Parse("custom://example.test/other")
		if !sameCacheOrigin(customLeft, customRight) {
			t.Fatal("matching custom origins differed")
		}
		invalidResponse := response(http.StatusNoContent)
		invalidResponse.Header.Set("Location", ":bad")
		invalidResponse.Header.Set("Content-Location", request.URL.String())
		if targets := cacheInvalidationTargets(request, invalidResponse); len(targets) != 1 {
			t.Fatalf("deduplicated targets = %d", len(targets))
		}
		var nilScheduler *cacheTestScheduler
		if _, err := NewCacheMiddleware(CacheOptions{
			Name: "nil-scheduler", Layer: MiddlewareClient,
			Store: &failingCacheStore{}, VariantKey: []byte(strings.Repeat("k", 32)),
			RevalidationScheduler: nilScheduler,
		}); !errors.Is(err, ErrInvalidCache) {
			t.Fatalf("typed nil scheduler error = %v", err)
		}
	})
}

func TestCacheExecuteConcurrencyAndRevalidationBoundaries(t *testing.T) {
	t.Parallel()

	base := time.Unix(1_700_000_000, 0)
	newPolicy := func(control string) (*cachePolicy, *cacheStaticStore, *http.Request, string) {
		request := cacheUnitRequest(t, "")
		store := &cacheStaticStore{}
		policy := cacheUnitPolicy(store, base.Add(2*time.Second), CacheFailOpen)
		entry := validCacheTestEntry(base, control)
		entry.VariantID = cacheVariantIdentity(policy.variantKey, nil, request.Header)
		store.entries = []CacheEntry{entry}
		key, err := policy.cacheKey(request)
		if err != nil {
			t.Fatalf("calculate cache key: %v", err)
		}
		return policy, store, request, key
	}
	origin := func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Cache-Control": {"max-age=60"}},
			Body:       io.NopCloser(strings.NewReader("origin")), Request: request,
			ContentLength: -1,
		}, nil
	}

	t.Run("refresh waiter cancellation", func(t *testing.T) {
		policy, _, request, key := newPolicy("max-age=60")
		policy.acquireFlight(key)
		ctx, cancel := context.WithCancel(request.Context())
		cancel()
		ctx, err := WithCacheMode(ctx, CacheModeRefresh)
		if err != nil {
			t.Fatalf("set refresh mode: %v", err)
		}
		if _, err := policy.execute(request.Clone(ctx), origin); !errors.Is(err, context.Canceled) {
			t.Fatalf("refresh waiter error = %v", err)
		}
	})

	t.Run("refresh waiter resumes normal lookup", func(t *testing.T) {
		policy, store, request, key := newPolicy("max-age=60")
		flight, _ := policy.acquireFlight(key)
		close(flight.done)
		store.load = func() {
			policy.mu.Lock()
			delete(policy.flights, key)
			policy.mu.Unlock()
		}
		ctx, err := WithCacheMode(request.Context(), CacheModeRefresh)
		if err != nil {
			t.Fatalf("set refresh mode: %v", err)
		}
		response, err := policy.execute(request.Clone(ctx), origin)
		if err != nil || response == nil {
			t.Fatalf("resumed refresh = %#v, %v", response, err)
		}
		_ = response.Body.Close()
	})

	t.Run("stale background follower", func(t *testing.T) {
		policy, _, request, key := newPolicy("max-age=1, stale-while-revalidate=30")
		policy.scheduler = &cacheTestScheduler{tasks: make(chan func(context.Context), 1)}
		policy.acquireFlight(key)
		response, err := policy.execute(request, origin)
		if err != nil || response == nil {
			t.Fatalf("stale follower = %#v, %v", response, err)
		}
		_ = response.Body.Close()
	})

	t.Run("scheduler nil context releases flight", func(t *testing.T) {
		policy, _, request, key := newPolicy("max-age=1, stale-while-revalidate=30")
		scheduler := &cacheTestScheduler{tasks: make(chan func(context.Context), 1)}
		policy.scheduler = scheduler
		response, err := policy.execute(request, origin)
		if err != nil || response == nil {
			t.Fatalf("scheduled stale = %#v, %v", response, err)
		}
		_ = response.Body.Close()
		(<-scheduler.tasks)(nil)
		policy.mu.Lock()
		_, exists := policy.flights[key]
		policy.mu.Unlock()
		if exists {
			t.Fatal("nil-context task retained flight")
		}
	})

	for _, test := range []struct {
		name    string
		getBody func() (io.ReadCloser, error)
	}{
		{
			name: "replayed request body",
			getBody: func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("replayed")), nil
			},
		},
		{
			name: "request body replay failure releases flight",
			getBody: func() (io.ReadCloser, error) {
				return nil, errors.New("replay")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy, _, request, key := newPolicy("max-age=1, stale-while-revalidate=30")
			scheduler := &cacheTestScheduler{tasks: make(chan func(context.Context), 1)}
			policy.scheduler = scheduler
			var closed atomic.Int64
			request.Body = &cacheTestBody{Reader: strings.NewReader("original"), closed: &closed}
			request.GetBody = test.getBody
			response, err := policy.execute(request, func(request *http.Request) (*http.Response, error) {
				if request.Body != nil {
					_, _ = io.Copy(io.Discard, request.Body)
					_ = request.Body.Close()
				}
				return origin(request)
			})
			if err != nil || response == nil {
				t.Fatalf("scheduled body response = %#v, %v", response, err)
			}
			_ = response.Body.Close()
			if closed.Load() != 1 {
				t.Fatalf("short-circuit request body closes = %d", closed.Load())
			}
			(<-scheduler.tasks)(context.Background())
			policy.mu.Lock()
			_, exists := policy.flights[key]
			policy.mu.Unlock()
			if exists {
				t.Fatal("body replay task retained flight")
			}
		})
	}

	t.Run("non-replayable body disables background revalidation", func(t *testing.T) {
		policy, _, request, _ := newPolicy("max-age=1, stale-while-revalidate=30")
		policy.scheduler = &cacheTestScheduler{tasks: make(chan func(context.Context), 1)}
		request.Body = io.NopCloser(strings.NewReader("body"))
		var calls atomic.Int64
		response, err := policy.execute(request, func(request *http.Request) (*http.Response, error) {
			calls.Add(1)
			return origin(request)
		})
		if err != nil || response == nil || calls.Load() != 1 {
			t.Fatalf("non-replayable response = %#v, %v, calls %d", response, err, calls.Load())
		}
		_ = response.Body.Close()
		if !cacheRevalidationReplayable(&http.Request{Body: http.NoBody}) {
			t.Fatal("http.NoBody was not replayable")
		}
	})

	t.Run("scheduler failure obeys failure mode", func(t *testing.T) {
		policy, _, request, _ := newPolicy("max-age=1, stale-while-revalidate=30")
		policy.failureMode = CacheFailClosed
		policy.scheduler = cacheSchedulerFunc(func(func(context.Context)) error {
			return errors.New("schedule failure")
		})
		if _, err := policy.execute(request, origin); err == nil {
			t.Fatal("fail-closed scheduling succeeded")
		}
	})

	t.Run("validator waiter cancellation", func(t *testing.T) {
		policy, store, request, key := newPolicy("max-age=1")
		store.entries[0].Header.Set("ETag", `"value"`)
		policy.acquireFlight(key)
		ctx, cancel := context.WithCancel(request.Context())
		cancel()
		if _, err := policy.execute(request.Clone(ctx), origin); !errors.Is(err, context.Canceled) {
			t.Fatalf("validator waiter error = %v", err)
		}
	})

	for _, test := range []struct {
		name      string
		validator bool
	}{
		{name: "validator waiter resumes", validator: true},
		{name: "unvalidated waiter resumes", validator: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy, store, request, key := newPolicy("max-age=1")
			if test.validator {
				store.entries[0].Header.Set("ETag", `"value"`)
			}
			flight, _ := policy.acquireFlight(key)
			close(flight.done)
			loads := 0
			store.load = func() {
				loads++
				if loads == 2 {
					policy.mu.Lock()
					delete(policy.flights, key)
					policy.mu.Unlock()
					store.entries[0].Header.Set("Cache-Control", "max-age=60")
				}
			}
			response, err := policy.execute(request, origin)
			if err != nil || response == nil {
				t.Fatalf("resumed waiter = %#v, %v", response, err)
			}
			_ = response.Body.Close()
		})
	}

	t.Run("last modified validator is conditional", func(t *testing.T) {
		policy, store, request, _ := newPolicy("max-age=1")
		modified := base.Add(-time.Hour).Format(http.TimeFormat)
		store.entries[0].Header.Set("Last-Modified", modified)
		response, err := policy.execute(request, func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("If-Modified-Since") != modified {
				t.Fatalf("conditional header = %q", request.Header.Get("If-Modified-Since"))
			}
			return &http.Response{
				StatusCode: http.StatusNotModified, Header: make(http.Header),
				Body: http.NoBody, Request: request,
			}, nil
		})
		if err != nil || response == nil {
			t.Fatalf("last-modified response = %#v, %v", response, err)
		}
		_ = response.Body.Close()
	})

	t.Run("unvalidated stale waiter cancellation", func(t *testing.T) {
		policy, _, request, key := newPolicy("max-age=1")
		policy.acquireFlight(key)
		ctx, cancel := context.WithCancel(request.Context())
		cancel()
		if _, err := policy.execute(request.Clone(ctx), origin); !errors.Is(err, context.Canceled) {
			t.Fatalf("stale waiter error = %v", err)
		}
	})

	t.Run("only-if-cached survives fail-open load error", func(t *testing.T) {
		policy := cacheUnitPolicy(&failingCacheStore{loadErr: errors.New("load")}, base, CacheFailOpen)
		request := cacheUnitRequest(t, "only-if-cached")
		var closed atomic.Int64
		request.Body = &cacheTestBody{Reader: strings.NewReader("body"), closed: &closed}
		response, err := policy.execute(request, origin)
		if err != nil || response.StatusCode != http.StatusGatewayTimeout {
			t.Fatalf("only-if-cached response = %#v, %v", response, err)
		}
		_ = response.Body.Close()
		if closed.Load() != 1 {
			t.Fatalf("only-if-cached request body closes = %d", closed.Load())
		}
	})
}

type cacheStaticStore struct {
	entries []CacheEntry
	load    func()
}

func (store *cacheStaticStore) Load(context.Context, string) ([]CacheEntry, error) {
	if store.load != nil {
		store.load()
	}
	return store.entries, nil
}

func (*cacheStaticStore) Save(context.Context, string, CacheEntry) error { return nil }
func (*cacheStaticStore) Delete(context.Context, string) error           { return nil }

type cacheSchedulerFunc func(func(context.Context)) error

func (schedule cacheSchedulerFunc) ScheduleCacheRevalidation(task func(context.Context)) error {
	return schedule(task)
}

func validCacheTestEntry(at time.Time, control string) CacheEntry {
	header := make(http.Header)
	if control != "" {
		header.Set("Cache-Control", control)
	}
	return CacheEntry{
		StatusCode: http.StatusOK, Header: header, Body: []byte("body"),
		RequestTime: at, ResponseTime: at, VariantID: strings.Repeat("0", 64),
	}
}

type cacheTestScheduler struct {
	tasks chan func(context.Context)
}

func (scheduler *cacheTestScheduler) ScheduleCacheRevalidation(task func(context.Context)) error {
	scheduler.tasks <- task

	return nil
}

type inspectingPrimaryKeyStore struct {
	CacheStore
	key string
}

func (store *inspectingPrimaryKeyStore) Load(ctx context.Context, key string) ([]CacheEntry, error) {
	store.key = key
	return store.CacheStore.Load(ctx, key)
}

func (store *inspectingPrimaryKeyStore) Save(ctx context.Context, key string, entry CacheEntry) error {
	store.key = key
	return store.CacheStore.Save(ctx, key, entry)
}

func cacheUnitPolicy(store CacheStore, now time.Time, failure CacheFailureMode) *cachePolicy {
	return &cachePolicy{
		store: store, clock: &cacheTestClock{now: now}, namespace: "test",
		maximumBody: 64, variantKey: []byte("0123456789abcdef0123456789abcdef"),
		methods:     map[string]struct{}{http.MethodGet: {}},
		statuses:    map[int]struct{}{http.StatusOK: {}},
		failureMode: failure, flights: make(map[string]*cacheFlight),
	}
}

func cacheUnitRequest(t *testing.T, cacheControl string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	if cacheControl != "" {
		request.Header.Set("Cache-Control", cacheControl)
	}

	return request
}

type signalingCacheStore struct {
	CacheStore
	loads      atomic.Int64
	secondLoad chan struct{}
}

func (store *signalingCacheStore) Load(ctx context.Context, key string) ([]CacheEntry, error) {
	entries, err := store.CacheStore.Load(ctx, key)
	if store.loads.Add(1) == 2 {
		close(store.secondLoad)
	}

	return entries, err
}

func newCacheTestClient(
	t *testing.T,
	maximumBody int64,
	transport roundTripFunc,
) *Client {
	t.Helper()
	store, err := NewMemoryCache(MemoryCacheOptions{})
	if err != nil {
		t.Fatalf("construct memory cache: %v", err)
	}
	middleware, err := NewCacheMiddleware(CacheOptions{
		Name: "body-cache", Layer: MiddlewareClient, Store: store,
		MaximumBodyBytes: maximumBody,
	})
	if err != nil {
		t.Fatalf("construct cache middleware: %v", err)
	}
	client, err := New(Config{Middleware: []Middleware{middleware}, Transport: transport})
	if err != nil {
		t.Fatalf("construct cache client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return client
}

func cacheClientRequest(t *testing.T, client *Client) *http.Response {
	t.Helper()
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute cache request: %v", err)
	}

	return response
}

type cacheTestBody struct {
	io.Reader
	reads    *atomic.Int64
	closed   *atomic.Int64
	closeErr error
}

func (body *cacheTestBody) Read(buffer []byte) (int, error) {
	if body.reads != nil {
		body.reads.Add(1)
	}

	return body.Reader.Read(buffer)
}

func (body *cacheTestBody) Close() error {
	if body.closed != nil {
		body.closed.Add(1)
	}

	return body.closeErr
}

type cacheErrorReader struct {
	err error
}

func (reader *cacheErrorReader) Read([]byte) (int, error) { return 0, reader.err }

type failingCacheStore struct {
	loadErr   error
	saveErr   error
	deleteErr error
}

func (store *failingCacheStore) Load(context.Context, string) ([]CacheEntry, error) {
	return nil, store.loadErr
}

func (store *failingCacheStore) Save(context.Context, string, CacheEntry) error {
	return store.saveErr
}

func (store *failingCacheStore) Delete(context.Context, string) error {
	return store.deleteErr
}

type inspectingCacheStore struct {
	CacheStore
	inspect func(CacheEntry) error
	failure error
}

func (store *inspectingCacheStore) Save(ctx context.Context, key string, entry CacheEntry) error {
	if err := store.inspect(entry); err != nil {
		store.failure = err
		return err
	}

	return store.CacheStore.Save(ctx, key, entry)
}

type cacheTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *cacheTestClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()

	return clock.now
}

func (*cacheTestClock) Wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (clock *cacheTestClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}
