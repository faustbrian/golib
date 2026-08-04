package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCacheExactConfigurationAndMemoryBoundaries(t *testing.T) {
	validVariant := strings.Repeat("a", 64)
	entry := func(body string, variant string) CacheEntry {
		return CacheEntry{Body: []byte(body), VariantID: variant}
	}

	for _, options := range []MemoryCacheOptions{
		{MaximumEntries: 1, MaximumBytes: 1},
		{MaximumEntries: maximumMemoryCacheEntries, MaximumBytes: 1},
	} {
		if _, err := NewMemoryCache(options); err != nil {
			t.Fatalf("valid memory bounds %#v: %v", options, err)
		}
	}
	for _, options := range []MemoryCacheOptions{
		{MaximumEntries: -1, MaximumBytes: 1},
		{MaximumEntries: maximumMemoryCacheEntries + 1, MaximumBytes: 1},
		{MaximumEntries: 1, MaximumBytes: -1},
	} {
		if _, err := NewMemoryCache(options); !errors.Is(err, ErrInvalidCache) {
			t.Fatalf("invalid memory bounds %#v: %v", options, err)
		}
	}

	cache, err := NewMemoryCache(MemoryCacheOptions{MaximumEntries: 2, MaximumBytes: 3})
	if err != nil {
		t.Fatalf("construct bounded cache: %v", err)
	}
	if err := cache.Save(context.Background(), "", entry("a", validVariant)); !errors.Is(err, ErrCacheLimit) {
		t.Fatalf("empty key error = %v", err)
	}
	if err := cache.Save(context.Background(), "one", entry("1234", validVariant)); !errors.Is(err, ErrCacheLimit) {
		t.Fatalf("oversized body error = %v", err)
	}
	if err := cache.Save(context.Background(), "one", entry("123", validVariant)); err != nil {
		t.Fatalf("exact-size body: %v", err)
	}
	if cache.bytes != 3 || len(cache.order) != 1 {
		t.Fatalf("exact-size accounting = %d bytes, %d entries", cache.bytes, len(cache.order))
	}
	if err := cache.Save(context.Background(), "one", entry("12", validVariant)); err != nil {
		t.Fatalf("replace variant: %v", err)
	}
	if cache.bytes != 2 || len(cache.order) != 1 {
		t.Fatalf("replacement accounting = %d bytes, %d entries", cache.bytes, len(cache.order))
	}
	secondVariant := strings.Repeat("b", 64)
	if err := cache.Save(context.Background(), "one", entry("3", secondVariant)); err != nil {
		t.Fatalf("save second variant: %v", err)
	}
	if cache.bytes != 3 || len(cache.order) != 2 || len(cache.items["one"]) != 2 {
		t.Fatalf("variant accounting = %d bytes, %d order, %d variants", cache.bytes, len(cache.order), len(cache.items["one"]))
	}
	if err := cache.Save(context.Background(), "two", entry("x", validVariant)); err != nil {
		t.Fatalf("evict oldest: %v", err)
	}
	if cache.bytes != 2 || len(cache.order) != 2 || len(cache.items["one"]) != 1 || len(cache.items["two"]) != 1 {
		t.Fatalf("eviction accounting = %d bytes, %d order, %#v", cache.bytes, len(cache.order), cache.items)
	}
	if err := cache.Delete(context.Background(), "one"); err != nil {
		t.Fatalf("delete one primary: %v", err)
	}
	if cache.bytes != 1 || len(cache.order) != 1 || len(cache.items) != 1 || len(cache.items["two"]) != 1 {
		t.Fatalf("delete accounting = %d bytes, %d order, %#v", cache.bytes, len(cache.order), cache.items)
	}

	store := &cacheRecordingStore{}
	for _, namespace := range []string{strings.Repeat("a", maximumCacheNamespaceLength), "a.b_c-d"} {
		middleware, middlewareErr := NewCacheMiddleware(CacheOptions{
			Name: "cache-boundary", Layer: MiddlewareClient, Priority: 17,
			Namespace: namespace, Store: store, VariantKey: []byte(strings.Repeat("k", 32)),
		})
		if middlewareErr != nil {
			t.Fatalf("valid namespace length %d: %v", len(namespace), middlewareErr)
		}
		if middleware.information.Priority != -983 {
			t.Fatalf("middleware priority = %d", middleware.information.Priority)
		}
	}
	for _, options := range []CacheOptions{
		{Name: "long", Layer: MiddlewareClient, Namespace: strings.Repeat("a", maximumCacheNamespaceLength+1), Store: store, VariantKey: []byte(strings.Repeat("k", 32))},
		{Name: "body", Layer: MiddlewareClient, MaximumBodyBytes: -1, Store: store, VariantKey: []byte(strings.Repeat("k", 32))},
	} {
		if _, middlewareErr := NewCacheMiddleware(options); !errors.Is(middlewareErr, ErrInvalidCache) {
			t.Fatalf("invalid cache options %#v: %v", options, middlewareErr)
		}
	}
	if _, err := NewCacheMiddleware(CacheOptions{
		Name: "one-byte", Layer: MiddlewareClient, MaximumBodyBytes: 1,
		Store: store, VariantKey: []byte(strings.Repeat("k", 32)),
	}); err != nil {
		t.Fatalf("one-byte body limit: %v", err)
	}

	entryLimited, _ := NewMemoryCache(MemoryCacheOptions{MaximumEntries: 1, MaximumBytes: 10})
	if err := entryLimited.Save(context.Background(), "one", entry("a", validVariant)); err != nil {
		t.Fatalf("entry-limited first save: %v", err)
	}
	if err := entryLimited.Save(context.Background(), "two", entry("b", validVariant)); err != nil {
		t.Fatalf("entry-limited second save: %v", err)
	}
	if len(entryLimited.order) != 1 || entryLimited.bytes != 1 || len(entryLimited.items["two"]) != 1 {
		t.Fatalf("entry-only eviction = %d entries, %d bytes, %#v", len(entryLimited.order), entryLimited.bytes, entryLimited.items)
	}
	byteLimited, _ := NewMemoryCache(MemoryCacheOptions{MaximumEntries: 10, MaximumBytes: 1})
	if err := byteLimited.Save(context.Background(), "one", entry("a", validVariant)); err != nil {
		t.Fatalf("byte-limited first save: %v", err)
	}
	if err := byteLimited.Save(context.Background(), "two", entry("b", validVariant)); err != nil {
		t.Fatalf("byte-limited second save: %v", err)
	}
	if len(byteLimited.order) != 1 || byteLimited.bytes != 1 || len(byteLimited.items["two"]) != 1 {
		t.Fatalf("byte-only eviction = %d entries, %d bytes, %#v", len(byteLimited.order), byteLimited.bytes, byteLimited.items)
	}
	deletion, _ := NewMemoryCache(MemoryCacheOptions{MaximumEntries: 10, MaximumBytes: 10})
	_ = deletion.Save(context.Background(), "one", entry("aa", validVariant))
	_ = deletion.Save(context.Background(), "two", entry("b", validVariant))
	if err := deletion.Delete(context.Background(), "one"); err != nil {
		t.Fatalf("simple deletion: %v", err)
	}
	if deletion.bytes != 1 || len(deletion.order) != 1 || deletion.order[0].primary != "two" {
		t.Fatalf("simple deletion = %d bytes, %#v", deletion.bytes, deletion.order)
	}
}

func TestCacheExactPolicyAndProtocolBoundaries(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	request := cacheUnitRequest(t, "")
	store := &cacheRecordingStore{}
	policy := cacheUnitPolicy(store, base, CacheFailOpen)

	for _, test := range []struct {
		status      int
		wantDeletes int
	}{
		{status: 199, wantDeletes: 0},
		{status: 200, wantDeletes: 1},
		{status: 399, wantDeletes: 1},
		{status: 400, wantDeletes: 0},
	} {
		store.reset()
		unsafe := request.Clone(context.Background())
		unsafe.Method = http.MethodPost
		response, err := policy.execute(unsafe, func(request *http.Request) (*http.Response, error) {
			return cacheMutationResponse(request, test.status, ""), nil
		})
		if err != nil {
			t.Fatalf("status %d execute: %v", test.status, err)
		}
		_ = response.Body.Close()
		if got := store.deleteCount(); got != test.wantDeletes {
			t.Fatalf("status %d deletes = %d, want %d", test.status, got, test.wantDeletes)
		}
	}

	store.reset()
	store.loadErr = errors.New("load")
	originCalls := 0
	response, err := policy.execute(request.Clone(context.Background()), func(request *http.Request) (*http.Response, error) {
		originCalls++
		return cacheMutationResponse(request, http.StatusOK, "max-age=60"), nil
	})
	if err != nil || originCalls != 1 {
		t.Fatalf("fail-open load = %#v, %v, calls %d", response, err, originCalls)
	}
	_ = response.Body.Close()

	store.reset()
	fresh := validCacheTestEntry(base, "max-age=60")
	fresh.VariantID = cacheVariantIdentity(policy.variantKey, nil, request.Header)
	store.entries = []CacheEntry{fresh}
	originCalls = 0
	response, err = policy.execute(request.Clone(context.Background()), func(request *http.Request) (*http.Response, error) {
		originCalls++
		return cacheMutationResponse(request, http.StatusOK, "max-age=60"), nil
	})
	if err != nil || originCalls != 0 {
		t.Fatalf("fresh hit = %#v, %v, calls %d", response, err, originCalls)
	}
	_ = response.Body.Close()

	noCache := request.Clone(context.Background())
	noCache.Header.Set("Cache-Control", "no-cache")
	response, err = policy.execute(noCache, func(request *http.Request) (*http.Response, error) {
		originCalls++
		return cacheMutationResponse(request, http.StatusOK, "max-age=60"), nil
	})
	if err != nil || originCalls != 1 {
		t.Fatalf("no-cache refresh = %#v, %v, calls %d", response, err, originCalls)
	}
	_ = response.Body.Close()

	for _, failureMode := range []CacheFailureMode{CacheFailOpen, CacheFailClosed} {
		failing := cacheUnitPolicy(&failingCacheStore{saveErr: errors.New("save"), deleteErr: errors.New("delete")}, base, failureMode)
		saveErr := failing.save(context.Background(), "key", CacheEntry{})
		deleteErr := failing.delete(context.Background(), "key")
		if failureMode == CacheFailOpen {
			if saveErr != nil || deleteErr != nil {
				t.Fatalf("fail-open storage = %v, %v", saveErr, deleteErr)
			}
		} else {
			if !errors.As(saveErr, new(*CacheError)) || !errors.As(deleteErr, new(*CacheError)) {
				t.Fatalf("fail-closed storage = %v, %v", saveErr, deleteErr)
			}
		}
	}
}

func TestCacheExactCaptureStorageAndMatchingBoundaries(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	request := cacheUnitRequest(t, "")
	policy := cacheUnitPolicy(&cacheRecordingStore{}, base, CacheFailOpen)
	policy.maximumBody = 3

	for _, test := range []struct {
		name          string
		contentLength int64
		body          string
		wantComplete  bool
	}{
		{name: "declared over limit", contentLength: 4, body: "1234", wantComplete: false},
		{name: "declared exact limit", contentLength: 3, body: "123", wantComplete: true},
		{name: "stream over limit", contentLength: -1, body: "1234", wantComplete: false},
		{name: "stream exact limit", contentLength: -1, body: "123", wantComplete: true},
		{name: "declared mismatch", contentLength: 2, body: "123", wantComplete: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := cacheMutationResponse(request, http.StatusOK, "max-age=60")
			response.ContentLength = test.contentLength
			response.Body = io.NopCloser(strings.NewReader(test.body))
			entry, complete, err := policy.capture(request, response, base, base)
			if err != nil || complete != test.wantComplete {
				t.Fatalf("capture = %#v, %t, %v", entry, complete, err)
			}
			body, readErr := io.ReadAll(response.Body)
			if readErr != nil || string(body) != test.body {
				t.Fatalf("caller body = %q, %v", body, readErr)
			}
			_ = response.Body.Close()
		})
	}

	for _, test := range []struct {
		name   string
		mutate func(*http.Request, *http.Response, *cachePolicy)
		want   bool
	}{
		{name: "default", want: true},
		{name: "range", mutate: func(req *http.Request, _ *http.Response, _ *cachePolicy) { req.Header.Set("Range", "bytes=0-0") }},
		{name: "request no-store", mutate: func(req *http.Request, _ *http.Response, _ *cachePolicy) { req.Header.Set("Cache-Control", "no-store") }},
		{name: "status", mutate: func(_ *http.Request, res *http.Response, _ *cachePolicy) { res.StatusCode = http.StatusCreated }},
		{name: "vary wildcard", mutate: func(_ *http.Request, res *http.Response, _ *cachePolicy) { res.Header.Set("Vary", "*") }},
		{name: "response no-store", mutate: func(_ *http.Request, res *http.Response, _ *cachePolicy) {
			res.Header.Set("Cache-Control", "max-age=60, no-store")
		}},
		{name: "set-cookie", mutate: func(_ *http.Request, res *http.Response, _ *cachePolicy) { res.Header.Add("Set-Cookie", "a=b") }},
		{name: "private shared", mutate: func(_ *http.Request, res *http.Response, policy *cachePolicy) {
			policy.shared = true
			res.Header.Set("Cache-Control", "max-age=60, private")
		}},
		{name: "identity without permission", mutate: func(req *http.Request, _ *http.Response, _ *cachePolicy) {
			req.Header.Set("Authorization", "Bearer value")
		}},
		{name: "ttl override", mutate: func(_ *http.Request, res *http.Response, policy *cachePolicy) {
			policy.ttlOverride = time.Second
			res.Header.Del("Cache-Control")
		}, want: true},
		{name: "expires", mutate: func(_ *http.Request, res *http.Response, _ *cachePolicy) {
			res.Header.Del("Cache-Control")
			res.Header.Set("Expires", base.Add(time.Minute).Format(http.TimeFormat))
		}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := request.Clone(context.Background())
			res := cacheMutationResponse(req, http.StatusOK, "max-age=60")
			candidate := cacheUnitPolicy(&cacheRecordingStore{}, base, CacheFailOpen)
			if test.mutate != nil {
				test.mutate(req, res, candidate)
			}
			if got := candidate.storable(req, res); got != test.want {
				t.Fatalf("storable = %t, want %t", got, test.want)
			}
		})
	}

	valid := validCacheTestEntry(base, "max-age=60")
	valid.VariantID = cacheVariantIdentity(policy.variantKey, nil, request.Header)
	invalid := valid
	invalid.StatusCode = 99
	matched, found := policy.match(request, []CacheEntry{invalid, valid})
	if !found || matched.StatusCode != valid.StatusCode {
		t.Fatalf("match skipped invalid entry = %#v, %t", matched, found)
	}
	setCookie := valid
	setCookie.Header.Add("Set-Cookie", "a=b")
	if _, found = policy.match(request, []CacheEntry{setCookie}); found {
		t.Fatal("matched Set-Cookie entry")
	}
	credentialed := request.Clone(context.Background())
	credentialed.Header.Set("Cookie", "session=value")
	credentialedEntry := valid
	credentialedEntry.VariantID = cacheVariantIdentity(policy.variantKey, nil, credentialed.Header)
	if _, found = policy.match(credentialed, []CacheEntry{credentialedEntry}); found {
		t.Fatal("matched credentialed entry without shared permission")
	}
}

func TestCacheExactFreshnessAndAgeBoundaries(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	request := cacheUnitRequest(t, "")
	policy := cacheUnitPolicy(&cacheRecordingStore{}, base.Add(10*time.Second), CacheFailOpen)
	entry := validCacheTestEntry(base, "max-age=10")
	entry.RequestTime = base.Add(-2 * time.Second)
	entry.ResponseTime = base
	entry.Header.Set("Date", base.Add(-3*time.Second).Format(http.TimeFormat))
	entry.Header.Set("Age", "5")
	if age := policy.currentAge(entry); age != 17*time.Second {
		t.Fatalf("current age = %s", age)
	}

	for _, test := range []struct {
		name    string
		control string
		now     time.Time
		want    bool
	}{
		{name: "just fresh", control: "max-age=10", now: base.Add(9 * time.Second), want: true},
		{name: "exact expiry", control: "max-age=10", now: base.Add(10 * time.Second), want: false},
		{name: "min fresh exact expiry", control: "max-age=10", now: base.Add(9 * time.Second), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := cacheUnitPolicy(&cacheRecordingStore{}, test.now, CacheFailOpen)
			candidateEntry := validCacheTestEntry(base, test.control)
			candidateRequest := request.Clone(context.Background())
			if test.name == "min fresh exact expiry" {
				candidateRequest.Header.Set("Cache-Control", "min-fresh=1")
			}
			if got := candidate.freshForRequest(candidateRequest, candidateEntry); got != test.want {
				t.Fatalf("fresh = %t, want %t", got, test.want)
			}
		})
	}

	for _, method := range []struct {
		name string
		call func(*cachePolicy, CacheEntry) bool
	}{
		{name: "max-stale", call: func(policy *cachePolicy, entry CacheEntry) bool {
			req := cacheUnitRequest(t, "max-stale=5")
			return policy.requestPermitsStale(req, entry)
		}},
		{name: "stale-if-error", call: (*cachePolicy).staleIfError},
		{name: "stale-while-revalidate", call: (*cachePolicy).staleWhileRevalidate},
	} {
		t.Run(method.name, func(t *testing.T) {
			directive := method.name + "=5"
			if method.name == "max-stale" {
				directive = ""
			}
			candidateEntry := validCacheTestEntry(base, "max-age=10"+cacheMutationDirective(directive))
			for _, boundary := range []struct {
				now  time.Time
				want bool
			}{
				{now: base.Add(9 * time.Second), want: false},
				{now: base.Add(10 * time.Second), want: true},
				{now: base.Add(15 * time.Second), want: true},
				{now: base.Add(16 * time.Second), want: false},
			} {
				candidate := cacheUnitPolicy(&cacheRecordingStore{}, boundary.now, CacheFailOpen)
				if got := method.call(candidate, candidateEntry); got != boundary.want {
					t.Fatalf("age %s allowed = %t, want %t", boundary.now.Sub(base), got, boundary.want)
				}
			}
		})
	}

	if !cacheRevalidationReplayable(&http.Request{}) ||
		!cacheRevalidationReplayable(&http.Request{Body: http.NoBody}) ||
		!cacheRevalidationReplayable(&http.Request{Body: io.NopCloser(strings.NewReader("x")), GetBody: func() (io.ReadCloser, error) { return http.NoBody, nil }}) ||
		cacheRevalidationReplayable(&http.Request{Body: io.NopCloser(strings.NewReader("x"))}) {
		t.Fatal("request replayability matrix is incorrect")
	}
	overflowPolicy := cacheUnitPolicy(&cacheRecordingStore{}, base.Add(time.Duration(1<<63-1)), CacheFailOpen)
	overflowPolicy.ttlOverride = time.Duration(1<<63 - 1)
	overflowEntry := validCacheTestEntry(base, "")
	overflowRequest := cacheUnitRequest(t, "min-fresh=2")
	if overflowPolicy.freshForRequest(overflowRequest, overflowEntry) {
		t.Fatal("overflowing min-fresh was accepted")
	}
}

func TestCacheExactHelpersAndParsingBoundaries(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	request := cacheUnitRequest(t, "")
	response := cacheMutationResponse(request, http.StatusNoContent, "")
	response.Header.Set("Location", "")
	response.Header.Set("Content-Location", "/next")
	targets := cacheInvalidationTargets(request, response)
	if len(targets) != 2 || targets[1].URL.Path != "/next" {
		t.Fatalf("invalidation targets = %#v", targets)
	}
	response.Header.Set("Location", "/next")
	if targets = cacheInvalidationTargets(request, response); len(targets) != 2 {
		t.Fatalf("deduplicated targets = %d", len(targets))
	}
	response.Header.Set("Location", request.URL.String())
	response.Header.Set("Content-Location", "/after-duplicate")
	targets = cacheInvalidationTargets(request, response)
	if len(targets) != 2 || targets[1].URL.Path != "/after-duplicate" {
		t.Fatalf("targets after duplicate = %#v", targets)
	}
	response.Header.Set("Location", "https://other.test/next")
	if targets = cacheInvalidationTargets(request, response); len(targets) != 2 {
		t.Fatalf("cross-origin targets = %d", len(targets))
	}

	for _, test := range []struct {
		left, right string
		want        bool
	}{
		{"https://example.test/a", "https://example.test:443/b", true},
		{"https://example.test/a", "https://example.test:444/b", false},
		{"http://example.test/a", "http://example.test:80/b", true},
		{"custom://example.test/a", "custom://example.test/b", true},
	} {
		left, _ := url.Parse(test.left)
		right, _ := url.Parse(test.right)
		if got := sameCacheOrigin(left, right); got != test.want {
			t.Fatalf("same origin %s %s = %t, want %t", test.left, test.right, got, test.want)
		}
	}

	policy := cacheUnitPolicy(&cacheRecordingStore{}, base, CacheFailOpen)
	for _, length := range []int{maximumCacheKeyMaterial, maximumCacheKeyMaterial + 1} {
		policy.key = func(*http.Request) (string, error) { return strings.Repeat("x", length), nil }
		_, err := policy.cacheKey(request)
		if (length == maximumCacheKeyMaterial) != (err == nil) {
			t.Fatalf("key material length %d error = %v", length, err)
		}
	}

	ctx := context.WithValue(context.Background(), cacheModeContextKey{}, CacheModeRefresh+1)
	if mode := cacheModeFromRequest(request.Clone(ctx)); mode != CacheModeDefault {
		t.Fatalf("invalid context mode = %d", mode)
	}
	ctx = context.WithValue(context.Background(), cacheModeContextKey{}, "refresh")
	if mode := cacheModeFromRequest(request.Clone(ctx)); mode != CacheModeDefault {
		t.Fatalf("wrong-type context mode = %d", mode)
	}

	cacheResponse := responseFromCache(request, validCacheTestEntry(base, "max-age=60"), CacheHit, -time.Second)
	if age := cacheResponse.Header.Get("Age"); age != "0" {
		t.Fatalf("negative age header = %q", age)
	}
	_ = cacheResponse.Body.Close()
	cacheResponse = responseFromCache(request, validCacheTestEntry(base, "max-age=60"), CacheHit, 2500*time.Millisecond)
	if age := cacheResponse.Header.Get("Age"); age != "2" {
		t.Fatalf("fractional age header = %q", age)
	}
	_ = cacheResponse.Body.Close()

	valid := validCacheTestEntry(base, "max-age=60")
	for _, status := range []int{99, 100, 599, 600} {
		candidate := valid
		candidate.StatusCode = status
		want := status >= 100 && status <= 599
		if got := validCacheEntry(candidate); got != want {
			t.Fatalf("status %d valid = %t, want %t", status, got, want)
		}
	}
	for _, mutate := range []func(*CacheEntry){
		func(entry *CacheEntry) { entry.Header = nil },
		func(entry *CacheEntry) { entry.RequestTime = time.Time{} },
		func(entry *CacheEntry) { entry.ResponseTime = time.Time{} },
		func(entry *CacheEntry) { entry.VariantID = "bad" },
		func(entry *CacheEntry) { entry.Vary = []string{"accept"} },
	} {
		candidate := valid
		mutate(&candidate)
		if validCacheEntry(candidate) {
			t.Fatalf("malformed entry accepted: %#v", candidate)
		}
	}

	vary, ok := parseVary(http.Header{"Vary": {"Accept, , Accept-Language", "Accept"}})
	if !ok || strings.Join(vary, ",") != "Accept,Accept-Language" {
		t.Fatalf("vary = %#v, %t", vary, ok)
	}
	parts := splitQuotedList(`one,"two,\"three",four`)
	if strings.Join(parts, "|") != `one|"two,\"three"|four` {
		t.Fatalf("quoted list = %#v", parts)
	}
	for _, value := range []struct {
		value string
		want  bool
	}{
		{value: strings.Repeat("0", 64), want: true},
		{value: strings.Repeat("0", 62), want: false},
		{value: strings.Repeat("z", 64), want: false},
	} {
		if got := validCacheVariantID(value.value); got != value.want {
			t.Fatalf("variant %q valid = %t, want %t", value.value, got, value.want)
		}
	}

	directives := parseCacheControl([]string{", MAX-AGE=5, max-age=6", `private="field,other"`})
	if len(directives) != 2 || directives["max-age"] != "5" || directives["private"] != "field,other" {
		t.Fatalf("directives = %#v", directives)
	}
	directives = parseCacheControl([]string{"max-age=5, max-age=6, public"})
	if directives["max-age"] != "5" {
		t.Fatalf("duplicate max-age = %#v", directives)
	}
	if _, ok := directives["public"]; !ok {
		t.Fatalf("directive after duplicate missing: %#v", directives)
	}
	for _, value := range []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{value: "0", want: 0, ok: true},
		{value: "2147483647", want: 2147483647 * time.Second, ok: true},
		{value: "2147483648", ok: false},
		{value: "-1", ok: false},
	} {
		got, ok := parseDeltaSeconds(value.value)
		if got != value.want || ok != value.ok {
			t.Fatalf("delta %q = %s, %t", value.value, got, ok)
		}
	}
	if parsed, ok := parseHTTPDate(base.UTC().Format(http.TimeFormat)); !ok || !parsed.Equal(base.UTC()) {
		t.Fatalf("valid HTTP date = %s, %t", parsed, ok)
	}
	if _, ok := parseHTTPDate("invalid"); ok {
		t.Fatal("invalid HTTP date accepted")
	}

	for _, test := range []struct {
		authorization, cookie string
		want                  bool
	}{
		{"", "", false}, {"Bearer value", "", true}, {"", "a=b", true}, {"Bearer value", "a=b", true},
	} {
		header := make(http.Header)
		header.Set("Authorization", test.authorization)
		header.Set("Cookie", test.cookie)
		if got := requestCarriesIdentity(header); got != test.want {
			t.Fatalf("identity auth=%q cookie=%q = %t", test.authorization, test.cookie, got)
		}
	}

	for _, status := range []int{199, 200, 599, 600} {
		_, err := resolveCacheStatuses([]int{status})
		want := status >= 200 && status <= 599
		if (err == nil) != want {
			t.Fatalf("status %d resolution error = %v", status, err)
		}
	}
	for _, method := range []struct {
		method string
		want   bool
	}{
		{http.MethodGet, true}, {http.MethodHead, true}, {http.MethodOptions, true}, {http.MethodTrace, true}, {http.MethodPost, false}, {"", false},
	} {
		if got := safeCacheMethod(method.method); got != method.want {
			t.Fatalf("safe method %q = %t, want %t", method.method, got, method.want)
		}
	}
}

func TestCacheResidualExecutionBoundaries(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	request := cacheUnitRequest(t, "")
	entry := validCacheTestEntry(base, "max-age=1, stale-while-revalidate=30")

	t.Run("scheduler safety and lifecycle", func(t *testing.T) {
		for _, method := range []struct {
			method       string
			wantSchedule bool
		}{
			{method: http.MethodGet, wantSchedule: true},
			{method: http.MethodPost, wantSchedule: false},
		} {
			store := &cacheRecordingStore{}
			policy := cacheUnitPolicy(store, base.Add(2*time.Second), CacheFailOpen)
			policy.methods[http.MethodPost] = struct{}{}
			candidate := entry
			candidate.VariantID = cacheVariantIdentity(policy.variantKey, nil, request.Header)
			store.entries = []CacheEntry{candidate}
			scheduler := &cacheCapturingScheduler{}
			policy.scheduler = scheduler
			candidateRequest := request.Clone(context.Background())
			candidateRequest.Method = method.method
			originCalls := 0
			response, err := policy.execute(candidateRequest, func(request *http.Request) (*http.Response, error) {
				originCalls++
				return cacheMutationResponse(request, http.StatusOK, "max-age=60"), nil
			})
			if err != nil {
				t.Fatalf("method %s: %v", method.method, err)
			}
			_ = response.Body.Close()
			if (scheduler.task != nil) != method.wantSchedule {
				t.Fatalf("method %s scheduled = %t", method.method, scheduler.task != nil)
			}
			if method.wantSchedule && originCalls != 0 || !method.wantSchedule && originCalls != 1 {
				t.Fatalf("method %s origin calls = %d", method.method, originCalls)
			}
		}

		store := &cacheRecordingStore{}
		policy := cacheUnitPolicy(store, base.Add(2*time.Second), CacheFailOpen)
		candidate := entry
		candidate.VariantID = cacheVariantIdentity(policy.variantKey, nil, request.Header)
		store.entries = []CacheEntry{candidate}
		scheduler := &cacheCapturingScheduler{}
		policy.scheduler = scheduler
		bodyRequest := request.Clone(context.Background())
		bodyRequest.Body = io.NopCloser(strings.NewReader("body"))
		bodyRequest.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("replay") }
		response, err := policy.execute(bodyRequest, func(request *http.Request) (*http.Response, error) {
			t.Fatal("origin called after body replay failure")
			return nil, nil
		})
		if err != nil || scheduler.task == nil {
			t.Fatalf("schedule replay failure = %#v, %v", response, err)
		}
		_ = response.Body.Close()
		scheduler.task(context.Background())
		policy.mu.Lock()
		flights := len(policy.flights)
		policy.mu.Unlock()
		if flights != 0 {
			t.Fatalf("replay failure retained %d flights", flights)
		}

		store = &cacheRecordingStore{}
		policy = cacheUnitPolicy(store, base.Add(2*time.Second), CacheFailOpen)
		candidate = entry
		candidate.VariantID = cacheVariantIdentity(policy.variantKey, nil, request.Header)
		store.entries = []CacheEntry{candidate}
		scheduler = &cacheCapturingScheduler{}
		policy.scheduler = scheduler
		closed := &cacheCloseCounter{}
		response, err = policy.execute(request.Clone(context.Background()), func(request *http.Request) (*http.Response, error) {
			result := cacheMutationResponse(request, http.StatusCreated, "")
			result.Body = closed
			result.ContentLength = 0
			return result, nil
		})
		if err != nil || scheduler.task == nil {
			t.Fatalf("schedule close = %#v, %v", response, err)
		}
		_ = response.Body.Close()
		scheduler.task(context.Background())
		if closed.closes != 1 {
			t.Fatalf("background response closes = %d", closed.closes)
		}
	})

	t.Run("conditional headers preserve caller values", func(t *testing.T) {
		store := &cacheRecordingStore{}
		policy := cacheUnitPolicy(store, base.Add(2*time.Second), CacheFailOpen)
		candidate := validCacheTestEntry(base, "max-age=1")
		candidate.Header.Set("ETag", `"stored"`)
		candidate.Header.Set("Last-Modified", base.Format(http.TimeFormat))
		candidate.VariantID = cacheVariantIdentity(policy.variantKey, nil, request.Header)
		store.entries = []CacheEntry{candidate}
		conditional := request.Clone(context.Background())
		conditional.Header.Set("If-None-Match", `"caller"`)
		conditional.Header.Set("If-Modified-Since", "caller-date")
		response, err := policy.execute(conditional, func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("If-None-Match") != `"caller"` || request.Header.Get("If-Modified-Since") != "caller-date" {
				t.Fatalf("conditional headers = %#v", request.Header)
			}
			return cacheMutationResponse(request, http.StatusOK, "max-age=60"), nil
		})
		if err != nil {
			t.Fatalf("conditional request: %v", err)
		}
		_ = response.Body.Close()
	})

	t.Run("unsafe fetch invalidates exact success range", func(t *testing.T) {
		for _, test := range []struct {
			status      int
			wantDeletes int
		}{
			{199, 0}, {200, 1}, {399, 1}, {400, 0},
		} {
			store := &cacheRecordingStore{}
			policy := cacheUnitPolicy(store, base, CacheFailOpen)
			unsafe := request.Clone(context.Background())
			unsafe.Method = http.MethodPost
			response, err := policy.fetch(unsafe, func(request *http.Request) (*http.Response, error) {
				return cacheMutationResponse(request, test.status, ""), nil
			}, "key", nil)
			if err != nil {
				t.Fatalf("status %d fetch: %v", test.status, err)
			}
			_ = response.Body.Close()
			if got := store.deleteCount(); got != test.wantDeletes {
				t.Fatalf("status %d deletes = %d, want %d", test.status, got, test.wantDeletes)
			}
		}
	})

	t.Run("zero-length capture and explicit freshness", func(t *testing.T) {
		policy := cacheUnitPolicy(&cacheRecordingStore{}, base, CacheFailOpen)
		policy.maximumBody = 1
		response := cacheMutationResponse(request, http.StatusOK, "max-age=60")
		entry, complete, err := policy.capture(request, response, base, base)
		if err != nil || !complete || len(entry.Body) != 0 {
			t.Fatalf("zero capture = %#v, %t, %v", entry, complete, err)
		}
		mismatch := cacheMutationResponse(request, http.StatusOK, "max-age=60")
		mismatch.ContentLength = 0
		mismatch.Body = io.NopCloser(strings.NewReader("x"))
		if _, complete, err = policy.capture(request, mismatch, base, base); err != nil || complete {
			t.Fatalf("zero-length mismatch = %t, %v", complete, err)
		}
		candidate := cacheMutationResponse(request, http.StatusOK, "")
		if policy.storable(request, candidate) {
			t.Fatal("response without freshness was storable")
		}
		policy.ttlOverride = time.Nanosecond
		if !policy.storable(request, candidate) {
			t.Fatal("positive TTL override was not storable")
		}
	})

	t.Run("min-fresh exact arithmetic", func(t *testing.T) {
		policy := cacheUnitPolicy(&cacheRecordingStore{}, base.Add(5*time.Second), CacheFailOpen)
		entry := validCacheTestEntry(base, "max-age=10")
		for _, control := range []string{"min-fresh=0", "min-fresh=1"} {
			candidate := cacheUnitRequest(t, control)
			if !policy.freshForRequest(candidate, entry) {
				t.Fatalf("fresh response rejected for %q", control)
			}
		}
	})

	t.Run("matching skips blocked entry and flight identity is exact", func(t *testing.T) {
		policy := cacheUnitPolicy(&cacheRecordingStore{}, base, CacheFailOpen)
		credentialed := request.Clone(context.Background())
		credentialed.Header.Set("Authorization", "Bearer value")
		blocked := validCacheTestEntry(base, "max-age=60")
		blocked.VariantID = cacheVariantIdentity(policy.variantKey, nil, credentialed.Header)
		allowed := validCacheTestEntry(base, "max-age=60, public")
		allowed.Status = "allowed"
		allowed.VariantID = blocked.VariantID
		matched, found := policy.match(credentialed, []CacheEntry{blocked, allowed})
		if !found || matched.Status != "allowed" {
			t.Fatalf("match after blocked entry = %#v, %t", matched, found)
		}

		flight, leader := policy.acquireFlight("key")
		if !leader {
			t.Fatal("first flight was not leader")
		}
		wrong := &cacheFlight{done: make(chan struct{})}
		policy.finishFlight("key", wrong)
		policy.mu.Lock()
		remaining := policy.flights["key"]
		policy.mu.Unlock()
		if remaining != flight {
			t.Fatal("wrong flight removed active flight")
		}
		select {
		case <-flight.done:
			t.Fatal("wrong flight closed active flight")
		default:
		}
		policy.finishFlight("key", flight)
		select {
		case <-flight.done:
		default:
			t.Fatal("active flight was not closed")
		}
	})
}

type cacheRecordingStore struct {
	mu        sync.Mutex
	entries   []CacheEntry
	loadErr   error
	deletions []string
}

type cacheCapturingScheduler struct {
	task func(context.Context)
}

func (scheduler *cacheCapturingScheduler) ScheduleCacheRevalidation(task func(context.Context)) error {
	scheduler.task = task

	return nil
}

type cacheCloseCounter struct {
	closes int
}

func (*cacheCloseCounter) Read([]byte) (int, error) { return 0, io.EOF }

func (counter *cacheCloseCounter) Close() error {
	counter.closes++

	return nil
}

func (store *cacheRecordingStore) Load(context.Context, string) ([]CacheEntry, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	return append([]CacheEntry(nil), store.entries...), store.loadErr
}

func (store *cacheRecordingStore) Save(context.Context, string, CacheEntry) error { return nil }

func (store *cacheRecordingStore) Delete(_ context.Context, key string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.deletions = append(store.deletions, key)

	return nil
}

func (store *cacheRecordingStore) reset() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.entries = nil
	store.loadErr = nil
	store.deletions = nil
}

func (store *cacheRecordingStore) deleteCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()

	return len(store.deletions)
}

func cacheMutationResponse(request *http.Request, status int, cacheControl string) *http.Response {
	header := make(http.Header)
	if cacheControl != "" {
		header.Set("Cache-Control", cacheControl)
	}

	return &http.Response{
		StatusCode: status, Header: header, Body: http.NoBody, Request: request,
		ContentLength: 0,
	}
}

func cacheMutationDirective(value string) string {
	if value == "" {
		return ""
	}

	return ", " + value
}
