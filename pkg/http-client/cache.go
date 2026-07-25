package httpclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrInvalidCache indicates malformed cache policy, storage, or metadata.
	ErrInvalidCache = errors.New("invalid HTTP cache")
	// ErrCacheLimit indicates that cache storage exceeded a finite bound.
	ErrCacheLimit               = errors.New("HTTP cache limit reached")
	cacheRandomReader io.Reader = rand.Reader
)

// CacheError reports cache storage or body processing failure without
// rendering backend, key, request, header, or body details.
type CacheError struct {
	Operation string
	Cause     error
}

// Error implements error without rendering the cause.
func (err *CacheError) Error() string { return "HTTP cache " + err.Operation + " failed" }

// Unwrap returns the cache failure cause.
func (err *CacheError) Unwrap() error { return err.Cause }

const (
	cacheMiddlewarePriority     = -1000
	defaultCacheMaximumBody     = 8 << 20
	defaultMemoryCacheEntries   = 1_000
	defaultMemoryCacheBytes     = 64 << 20
	maximumMemoryCacheEntries   = 1_000_000
	maximumCacheNamespaceLength = 128
	maximumCacheKeyMaterial     = 4 << 10
)

// CacheProvenance describes how a response was obtained.
type CacheProvenance uint8

const (
	// CacheMiss indicates that the response came from the next HTTP policy.
	CacheMiss CacheProvenance = iota
	// CacheHit indicates that a fresh stored response satisfied the request.
	CacheHit
	// CacheRevalidated indicates that a 304 freshened a stored response.
	CacheRevalidated
	// CacheStale indicates explicitly permitted stale response reuse.
	CacheStale
)

// CacheMetadata describes cache handling without changing standard response
// access. It never contains a URL, cache key, credential, or response body.
type CacheMetadata struct {
	Provenance CacheProvenance
	Age        time.Duration
}

type cacheMetadataContextKey struct{}
type cacheModeContextKey struct{}

// CacheMode controls one request's cache lookup and storage behavior.
type CacheMode uint8

const (
	// CacheModeDefault applies normal RFC cache behavior.
	CacheModeDefault CacheMode = iota
	// CacheModeBypass skips cache lookup and storage.
	CacheModeBypass
	// CacheModeRefresh skips lookup and replaces a storable response.
	CacheModeRefresh
)

// CacheFailureMode controls whether backend failures bypass cache or fail the
// logical operation.
type CacheFailureMode uint8

const (
	// CacheFailOpen preserves origin availability when cache storage fails.
	CacheFailOpen CacheFailureMode = iota
	// CacheFailClosed surfaces cache storage failures as typed operation errors.
	CacheFailClosed
)

// WithCacheMode returns a context carrying one explicit request cache mode.
func WithCacheMode(ctx context.Context, mode CacheMode) (context.Context, error) {
	if ctx == nil || mode > CacheModeRefresh {
		return nil, fmt.Errorf("%w: request mode is invalid", ErrInvalidCache)
	}

	return context.WithValue(ctx, cacheModeContextKey{}, mode), nil
}

// CacheMetadataFromResponse returns immutable cache metadata when a configured
// cache handled response.
func CacheMetadataFromResponse(response *http.Response) (CacheMetadata, bool) {
	if response == nil || response.Request == nil {
		return CacheMetadata{}, false
	}
	metadata, ok := response.Request.Context().Value(cacheMetadataContextKey{}).(CacheMetadata)

	return metadata, ok
}

// CacheEntry is one complete stored response variant. Store implementations
// must treat slices, headers, and times as caller-owned values.
type CacheEntry struct {
	StatusCode       int
	Status           string
	Proto            string
	ProtoMajor       int
	ProtoMinor       int
	Header           http.Header
	Trailer          http.Header
	TransferEncoding []string
	Uncompressed     bool
	Body             []byte
	StoredAt         time.Time
	RequestTime      time.Time
	ResponseTime     time.Time
	Vary             []string
	VariantID        string
}

// CacheStore persists complete response variants under opaque primary keys.
// Implementations must be safe for concurrent use and honor context.
type CacheStore interface {
	Load(context.Context, string) ([]CacheEntry, error)
	Save(context.Context, string, CacheEntry) error
	Delete(context.Context, string) error
}

// CacheKeyFunc returns bounded caller-defined key material. The middleware
// hashes it before storage and the callback must not consume request bodies.
type CacheKeyFunc func(*http.Request) (string, error)

// CacheRevalidationScheduler owns asynchronous cache revalidation work and
// supplies its lifecycle context. Implementations must queue tasks rather than
// execute them inline.
type CacheRevalidationScheduler interface {
	ScheduleCacheRevalidation(func(context.Context)) error
}

// MemoryCacheOptions configures the finite in-memory reference store.
type MemoryCacheOptions struct {
	MaximumEntries int
	MaximumBytes   int64
}

type memoryCacheItem struct {
	primary string
	variant string
	entry   CacheEntry
}

// MemoryCache is a finite concurrency-safe FIFO cache reference backend.
type MemoryCache struct {
	mu             sync.Mutex
	maximumEntries int
	maximumBytes   int64
	bytes          int64
	items          map[string][]memoryCacheItem
	order          []memoryCacheItem
}

// NewMemoryCache constructs an empty finite in-memory cache.
func NewMemoryCache(options MemoryCacheOptions) (*MemoryCache, error) {
	maximumEntries := options.MaximumEntries
	if maximumEntries == 0 {
		maximumEntries = defaultMemoryCacheEntries
	}
	maximumBytes := options.MaximumBytes
	if maximumBytes == 0 {
		maximumBytes = defaultMemoryCacheBytes
	}
	if maximumEntries < 1 || maximumEntries > maximumMemoryCacheEntries || maximumBytes < 1 {
		return nil, fmt.Errorf("%w: memory bounds are invalid", ErrInvalidCache)
	}

	return &MemoryCache{
		maximumEntries: maximumEntries,
		maximumBytes:   maximumBytes,
		items:          make(map[string][]memoryCacheItem),
	}, nil
}

// Load returns independent copies of every stored variant for key.
func (cache *MemoryCache) Load(ctx context.Context, key string) ([]CacheEntry, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is nil", ErrInvalidCache)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	items := cache.items[key]
	entries := make([]CacheEntry, len(items))
	for index, item := range items {
		entries[index] = cloneCacheEntry(item.entry)
	}

	return entries, nil
}

// Save inserts or replaces one response variant.
func (cache *MemoryCache) Save(ctx context.Context, key string, entry CacheEntry) error {
	if ctx == nil {
		return fmt.Errorf("%w: context is nil", ErrInvalidCache)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" || int64(len(entry.Body)) > cache.maximumBytes {
		return ErrCacheLimit
	}
	entry = cloneCacheEntry(entry)
	if !validCacheVariantID(entry.VariantID) {
		return fmt.Errorf("%w: variant identity is invalid", ErrInvalidCache)
	}
	variant := entry.VariantID
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.removeLocked(key, variant)
	item := memoryCacheItem{primary: key, variant: variant, entry: entry}
	cache.items[key] = append(cache.items[key], item)
	cache.order = append(cache.order, item)
	cache.bytes += int64(len(entry.Body))
	for len(cache.order) > cache.maximumEntries || cache.bytes > cache.maximumBytes {
		oldest := cache.order[0]
		cache.order = cache.order[1:]
		cache.removeItemLocked(oldest.primary, oldest.variant, false)
	}

	return nil
}

// Delete removes every stored variant for key.
func (cache *MemoryCache) Delete(ctx context.Context, key string) error {
	if ctx == nil {
		return fmt.Errorf("%w: context is nil", ErrInvalidCache)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for _, item := range cache.items[key] {
		cache.bytes -= int64(len(item.entry.Body))
	}
	delete(cache.items, key)
	filtered := cache.order[:0]
	for _, item := range cache.order {
		if item.primary != key {
			filtered = append(filtered, item)
		}
	}
	cache.order = filtered

	return nil
}

func (cache *MemoryCache) removeLocked(primary string, variant string) {
	cache.removeItemLocked(primary, variant, true)
}

func (cache *MemoryCache) removeItemLocked(primary string, variant string, removeOrder bool) {
	items := cache.items[primary]
	filtered := items[:0]
	for _, item := range items {
		if item.variant == variant {
			cache.bytes -= int64(len(item.entry.Body))
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) == 0 {
		delete(cache.items, primary)
	} else {
		cache.items[primary] = filtered
	}
	if !removeOrder {
		return
	}
	ordered := cache.order[:0]
	for _, item := range cache.order {
		if item.primary != primary || item.variant != variant {
			ordered = append(ordered, item)
		}
	}
	cache.order = ordered
}

// CacheOptions configures RFC-aware operation cache middleware.
type CacheOptions struct {
	Name                  string
	Layer                 MiddlewareLayer
	Priority              int
	Namespace             string
	Shared                bool
	MaximumBodyBytes      int64
	TTLOverride           time.Duration
	Store                 CacheStore
	Clock                 RetryClock
	VariantKey            []byte
	Methods               []string
	Statuses              []int
	FailureMode           CacheFailureMode
	Key                   CacheKeyFunc
	RevalidationScheduler CacheRevalidationScheduler
}

type cachePolicy struct {
	store       CacheStore
	clock       RetryClock
	namespace   string
	shared      bool
	maximumBody int64
	ttlOverride time.Duration
	variantKey  []byte
	methods     map[string]struct{}
	statuses    map[int]struct{}
	failureMode CacheFailureMode
	key         CacheKeyFunc
	scheduler   CacheRevalidationScheduler
	mu          sync.Mutex
	flights     map[string]*cacheFlight
}

type cacheFlight struct{ done chan struct{} }

// NewCacheMiddleware creates operation-scoped cache middleware.
func NewCacheMiddleware(options CacheOptions) (Middleware, error) {
	if nilLike(options.Store) {
		return Middleware{}, fmt.Errorf("%w: store is nil", ErrInvalidCache)
	}
	if options.FailureMode > CacheFailClosed {
		return Middleware{}, fmt.Errorf("%w: failure mode is invalid", ErrInvalidCache)
	}
	namespace := options.Namespace
	if namespace == "" {
		namespace = "default"
	}
	if len(namespace) > maximumCacheNamespaceLength || !validCacheNamespace(namespace) {
		return Middleware{}, fmt.Errorf("%w: namespace is invalid", ErrInvalidCache)
	}
	maximumBody := options.MaximumBodyBytes
	if maximumBody == 0 {
		maximumBody = defaultCacheMaximumBody
	}
	if maximumBody < 1 {
		return Middleware{}, fmt.Errorf("%w: maximum body is invalid", ErrInvalidCache)
	}
	if options.TTLOverride < 0 {
		return Middleware{}, fmt.Errorf("%w: TTL override is invalid", ErrInvalidCache)
	}
	clock := options.Clock
	if clock == nil {
		clock = systemRetryClock{}
	} else if nilLike(clock) {
		return Middleware{}, fmt.Errorf("%w: clock is nil", ErrInvalidCache)
	}
	if options.RevalidationScheduler != nil && nilLike(options.RevalidationScheduler) {
		return Middleware{}, fmt.Errorf("%w: revalidation scheduler is nil", ErrInvalidCache)
	}
	variantKey := append([]byte(nil), options.VariantKey...)
	if len(variantKey) == 0 {
		variantKey = make([]byte, 32)
		if _, err := io.ReadFull(cacheRandomReader, variantKey); err != nil {
			return Middleware{}, fmt.Errorf("%w: variant key generation failed", ErrInvalidCache)
		}
	} else if len(variantKey) < 32 {
		return Middleware{}, fmt.Errorf("%w: variant key is too short", ErrInvalidCache)
	}
	methods, err := resolveCacheMethods(options.Methods)
	if err != nil {
		return Middleware{}, err
	}
	statuses, err := resolveCacheStatuses(options.Statuses)
	if err != nil {
		return Middleware{}, err
	}
	policy := &cachePolicy{
		store: options.Store, clock: clock, namespace: namespace,
		shared: options.Shared, maximumBody: maximumBody,
		ttlOverride: options.TTLOverride,
		variantKey:  append([]byte(nil), variantKey...),
		methods:     methods,
		statuses:    statuses,
		failureMode: options.FailureMode,
		key:         options.Key,
		scheduler:   options.RevalidationScheduler,
		flights:     make(map[string]*cacheFlight),
	}

	return NewTransportMiddleware(MiddlewareOptions{
		Name: options.Name, Scope: ScopeOperation, Layer: options.Layer,
		Priority: cacheMiddlewarePriority + options.Priority,
	}, policy.execute)
}

func (policy *cachePolicy) execute(request *http.Request, next Next) (*http.Response, error) {
	mode := cacheModeFromRequest(request)
	if mode == CacheModeBypass || request.Header.Get("Range") != "" || requestHasDirective(request, "no-store") {
		response, err := next(request)
		if err != nil {
			return nil, err
		}

		return withCacheMetadata(response, CacheMetadata{Provenance: CacheMiss}), nil
	}
	if _, cacheableMethod := policy.methods[request.Method]; !cacheableMethod {
		response, err := next(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode >= 200 && response.StatusCode < 400 {
			if err := policy.invalidate(request, response); err != nil {
				return nil, errors.Join(err, closeResponse(response))
			}
		}

		return withCacheMetadata(response, CacheMetadata{Provenance: CacheMiss}), nil
	}
	key, keyErr := policy.cacheKey(request)
	if keyErr != nil {
		return nil, keyErr
	}
	for {
		if mode == CacheModeRefresh {
			flight, leader := policy.acquireFlight(key)
			if !leader {
				if err := waitCacheFlight(request.Context(), flight); err != nil {
					return nil, err
				}
				mode = CacheModeDefault
				continue
			}
			return policy.fetchFlight(request, next, key, nil, flight)
		}
		entries, loadErr := policy.store.Load(request.Context(), key)
		if loadErr != nil && policy.failureMode == CacheFailClosed {
			return nil, &CacheError{Operation: "load", Cause: loadErr}
		}
		if loadErr == nil {
			entry, found := policy.match(request, entries)
			if found && !requestHasDirective(request, "no-cache") {
				if policy.freshForRequest(request, entry) {
					return responseFromCache(request, entry, CacheHit, policy.currentAge(entry)), nil
				}
				if policy.requestPermitsStale(request, entry) {
					return responseFromCache(request, entry, CacheStale, policy.currentAge(entry)), nil
				}
				if policy.scheduler != nil && safeCacheMethod(request.Method) &&
					cacheRevalidationReplayable(request) && policy.staleWhileRevalidate(entry) {
					flight, leader := policy.acquireFlight(key)
					if !leader {
						return responseFromCache(request, entry, CacheStale, policy.currentAge(entry)), nil
					}
					backgroundRequest := request.Clone(request.Context())
					backgroundRequest.Body = nil
					scheduleErr := policy.scheduler.ScheduleCacheRevalidation(func(ctx context.Context) {
						if ctx == nil {
							policy.finishFlight(key, flight)
							return
						}
						scheduledRequest := backgroundRequest.Clone(ctx)
						if request.GetBody != nil {
							body, err := request.GetBody()
							if err != nil {
								policy.finishFlight(key, flight)
								return
							}
							scheduledRequest.Body = body
						}
						response, _ := policy.fetchFlight(scheduledRequest, next, key, &entry, flight)
						if response != nil {
							_ = response.Body.Close()
						}
					})
					if scheduleErr == nil {
						return responseFromCache(request, entry, CacheStale, policy.currentAge(entry)), nil
					}
					policy.finishFlight(key, flight)
					if policy.failureMode == CacheFailClosed {
						return nil, &CacheError{Operation: "schedule revalidation", Cause: scheduleErr}
					}
				}
			}
			if requestHasDirective(request, "only-if-cached") {
				return onlyIfCachedMiss(request), nil
			}
			if found && hasValidator(entry) {
				flight, leader := policy.acquireFlight(key)
				if !leader {
					if err := waitCacheFlight(request.Context(), flight); err != nil {
						return nil, err
					}
					continue
				}
				conditional := request.Clone(request.Context())
				if etag := entry.Header.Get("ETag"); etag != "" && conditional.Header.Get("If-None-Match") == "" {
					conditional.Header.Set("If-None-Match", etag)
				}
				if modified := entry.Header.Get("Last-Modified"); modified != "" && conditional.Header.Get("If-Modified-Since") == "" {
					conditional.Header.Set("If-Modified-Since", modified)
				}
				return policy.fetchFlight(conditional, next, key, &entry, flight)
			}
			if found {
				flight, leader := policy.acquireFlight(key)
				if !leader {
					if err := waitCacheFlight(request.Context(), flight); err != nil {
						return nil, err
					}
					continue
				}
				return policy.fetchFlight(request, next, key, &entry, flight)
			}
		}
		if requestHasDirective(request, "only-if-cached") {
			return onlyIfCachedMiss(request), nil
		}
		flight, leader := policy.acquireFlight(key)
		if !leader {
			if err := waitCacheFlight(request.Context(), flight); err != nil {
				return nil, err
			}
			continue
		}
		return policy.fetchFlight(request, next, key, nil, flight)
	}
}

func (policy *cachePolicy) fetchFlight(
	request *http.Request,
	next Next,
	key string,
	stale *CacheEntry,
	flight *cacheFlight,
) (*http.Response, error) {
	defer policy.finishFlight(key, flight)

	return policy.fetch(request, next, key, stale)
}

func (policy *cachePolicy) fetch(
	request *http.Request,
	next Next,
	key string,
	stale *CacheEntry,
) (*http.Response, error) {
	requestTime := policy.clock.Now()
	response, err := next(request)
	responseTime := policy.clock.Now()
	if err != nil {
		if stale != nil && policy.staleIfError(*stale) {
			return responseFromCache(request, *stale, CacheStale, policy.currentAge(*stale)), nil
		}
		return nil, err
	}
	if !safeCacheMethod(request.Method) && response.StatusCode >= 200 && response.StatusCode < 400 {
		if err := policy.invalidate(request, response); err != nil {
			return nil, errors.Join(err, closeResponse(response))
		}
	}
	if stale != nil && cacheErrorStatus(response.StatusCode) && policy.staleIfError(*stale) {
		if closeErr := response.Body.Close(); closeErr != nil {
			return nil, &CacheError{Operation: "stale response body", Cause: closeErr}
		}

		return responseFromCache(request, *stale, CacheStale, policy.currentAge(*stale)), nil
	}
	if stale != nil && response.StatusCode == http.StatusNotModified {
		if closeErr := response.Body.Close(); closeErr != nil {
			return nil, &CacheError{Operation: "validation body", Cause: closeErr}
		}
		freshened := cloneCacheEntry(*stale)
		updateCachedHeaders(freshened.Header, response.Header)
		freshened.StoredAt = responseTime
		freshened.RequestTime = requestTime
		freshened.ResponseTime = responseTime
		if err := policy.save(request.Context(), key, freshened); err != nil {
			return nil, err
		}

		return responseFromCache(request, freshened, CacheRevalidated, policy.currentAge(freshened)), nil
	}
	response = withCacheMetadata(response, CacheMetadata{Provenance: CacheMiss})
	if _, noStore := parseCacheControl(response.Header.Values("Cache-Control"))["no-store"]; noStore {
		if err := policy.delete(request.Context(), key); err != nil {
			return nil, errors.Join(err, closeResponse(response))
		}

		return response, nil
	}
	if !policy.storable(request, response) {
		return response, nil
	}
	entry, complete, readErr := policy.capture(request, response, requestTime, responseTime)
	if readErr != nil {
		return nil, &CacheError{Operation: "response body", Cause: readErr}
	}
	if !complete {
		return response, nil
	}
	if stale != nil || cacheModeFromRequest(request) == CacheModeRefresh {
		if err := policy.delete(request.Context(), key); err != nil {
			return nil, errors.Join(err, closeResponse(response))
		}
	}
	if err := policy.save(request.Context(), key, entry); err != nil {
		return nil, errors.Join(err, closeResponse(response))
	}

	return response, nil
}

func (policy *cachePolicy) save(ctx context.Context, key string, entry CacheEntry) error {
	if err := policy.store.Save(ctx, key, entry); err != nil && policy.failureMode == CacheFailClosed {
		return &CacheError{Operation: "save", Cause: err}
	}

	return nil
}

func (policy *cachePolicy) delete(ctx context.Context, key string) error {
	if err := policy.store.Delete(ctx, key); err != nil && policy.failureMode == CacheFailClosed {
		return &CacheError{Operation: "delete", Cause: err}
	}

	return nil
}

func (policy *cachePolicy) invalidate(request *http.Request, response *http.Response) error {
	for _, target := range cacheInvalidationTargets(request, response) {
		key, err := policy.cacheKeyFor(http.MethodGet, target)
		if err != nil {
			return err
		}
		if err := policy.delete(request.Context(), key); err != nil {
			return err
		}
	}

	return nil
}

func (policy *cachePolicy) capture(
	request *http.Request,
	response *http.Response,
	requestTime time.Time,
	responseTime time.Time,
) (CacheEntry, bool, error) {
	if response.ContentLength > policy.maximumBody {
		return CacheEntry{}, false, nil
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, policy.maximumBody+1))
	if err != nil {
		return CacheEntry{}, false, errors.Join(err, response.Body.Close())
	}
	if int64(len(content)) > policy.maximumBody {
		response.Body = &prefixedReadCloser{
			Reader: io.MultiReader(bytes.NewReader(content), response.Body),
			closer: response.Body,
		}
		return CacheEntry{}, false, nil
	}
	if response.ContentLength >= 0 && response.ContentLength != int64(len(content)) {
		if err := response.Body.Close(); err != nil {
			return CacheEntry{}, false, err
		}
		response.Body = io.NopCloser(bytes.NewReader(content))

		return CacheEntry{}, false, nil
	}
	if err := response.Body.Close(); err != nil {
		return CacheEntry{}, false, err
	}
	response.Body = io.NopCloser(bytes.NewReader(content))
	response.ContentLength = int64(len(content))
	vary, ok := parseVary(response.Header)
	if !ok {
		return CacheEntry{}, false, nil
	}

	return CacheEntry{
		StatusCode: response.StatusCode, Status: response.Status,
		Proto: response.Proto, ProtoMajor: response.ProtoMajor, ProtoMinor: response.ProtoMinor,
		Header: response.Header.Clone(), Trailer: response.Trailer.Clone(),
		TransferEncoding: append([]string(nil), response.TransferEncoding...),
		Uncompressed:     response.Uncompressed,
		Body:             append([]byte(nil), content...), StoredAt: responseTime,
		RequestTime: requestTime, ResponseTime: responseTime,
		Vary: vary, VariantID: cacheVariantIdentity(policy.variantKey, vary, request.Header),
	}, true, nil
}

func (policy *cachePolicy) storable(request *http.Request, response *http.Response) bool {
	_, cacheableMethod := policy.methods[request.Method]
	_, cacheableStatus := policy.statuses[response.StatusCode]
	if !cacheableMethod || request.Header.Get("Range") != "" ||
		requestHasDirective(request, "no-store") || !cacheableStatus ||
		response.Header.Get("Vary") == "*" {
		return false
	}
	directives := parseCacheControl(response.Header.Values("Cache-Control"))
	if _, blocked := directives["no-store"]; blocked {
		return false
	}
	if len(response.Header.Values("Set-Cookie")) > 0 {
		return false
	}
	if policy.shared {
		if _, private := directives["private"]; private {
			return false
		}
	}
	if requestCarriesIdentity(request.Header) && !sharedCachePermission(directives) {
		return false
	}
	_, maxAge := cacheMaxAge(directives, policy.shared)
	_, expires := parseHTTPDate(response.Header.Get("Expires"))

	return policy.ttlOverride > 0 || maxAge || expires
}

func (policy *cachePolicy) match(request *http.Request, entries []CacheEntry) (CacheEntry, bool) {
	for _, entry := range entries {
		if !validCacheEntry(entry) || !policy.varyMatches(request.Header, entry) {
			continue
		}
		directives := parseCacheControl(entry.Header.Values("Cache-Control"))
		if len(entry.Header.Values("Set-Cookie")) > 0 ||
			requestCarriesIdentity(request.Header) && !sharedCachePermission(directives) {
			continue
		}

		return entry, true
	}

	return CacheEntry{}, false
}

func (policy *cachePolicy) acquireFlight(key string) (*cacheFlight, bool) {
	policy.mu.Lock()
	defer policy.mu.Unlock()
	if flight, exists := policy.flights[key]; exists {
		return flight, false
	}
	flight := &cacheFlight{done: make(chan struct{})}
	policy.flights[key] = flight

	return flight, true
}

func (policy *cachePolicy) finishFlight(key string, flight *cacheFlight) {
	policy.mu.Lock()
	if policy.flights[key] == flight {
		delete(policy.flights, key)
		close(flight.done)
	}
	policy.mu.Unlock()
}

func waitCacheFlight(ctx context.Context, flight *cacheFlight) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-flight.done:
		return nil
	}
}

func (policy *cachePolicy) freshForRequest(request *http.Request, entry CacheEntry) bool {
	directives := parseCacheControl(entry.Header.Values("Cache-Control"))
	if _, revalidate := directives["no-cache"]; revalidate {
		return false
	}
	lifetime, ok := policy.freshnessLifetime(entry)
	if !ok {
		return false
	}
	requestDirectives := parseCacheControl(request.Header.Values("Cache-Control"))
	if maximum, exists := requestDirectives["max-age"]; exists {
		if requested, valid := parseDeltaSeconds(maximum); valid {
			lifetime = min(lifetime, requested)
		} else {
			return false
		}
	}
	age := policy.currentAge(entry)
	if minimum, exists := requestDirectives["min-fresh"]; exists {
		additional, valid := parseDeltaSeconds(minimum)
		if !valid || age+additional < age {
			return false
		}
		age += additional
	}

	return age < lifetime
}

func (policy *cachePolicy) freshnessLifetime(entry CacheEntry) (time.Duration, bool) {
	directives := parseCacheControl(entry.Header.Values("Cache-Control"))
	if policy.ttlOverride > 0 {
		return policy.ttlOverride, true
	}
	if lifetime, explicit := cacheMaxAge(directives, policy.shared); explicit {
		return lifetime, true
	}
	expires, ok := parseHTTPDate(entry.Header.Get("Expires"))
	if !ok {
		return 0, false
	}
	date, ok := parseHTTPDate(entry.Header.Get("Date"))
	if !ok {
		date = entry.ResponseTime
	}

	return max(expires.Sub(date), 0), true
}

func (policy *cachePolicy) requestPermitsStale(request *http.Request, entry CacheEntry) bool {
	responseDirectives := parseCacheControl(entry.Header.Values("Cache-Control"))
	if staleReuseProhibited(responseDirectives, policy.shared) {
		return false
	}
	maximum, exists := parseCacheControl(request.Header.Values("Cache-Control"))["max-stale"]
	if !exists {
		return false
	}
	if maximum == "" {
		return true
	}
	allowed, valid := parseDeltaSeconds(maximum)
	if !valid {
		return false
	}
	lifetime, ok := policy.freshnessLifetime(entry)
	if !ok {
		return false
	}
	age := policy.currentAge(entry)

	return age >= lifetime && age-lifetime <= allowed
}

func (policy *cachePolicy) staleIfError(entry CacheEntry) bool {
	directives := parseCacheControl(entry.Header.Values("Cache-Control"))
	if staleReuseProhibited(directives, policy.shared) {
		return false
	}
	value, exists := directives["stale-if-error"]
	if !exists {
		return false
	}
	allowed, valid := parseDeltaSeconds(value)
	if !valid {
		return false
	}
	lifetime, ok := policy.freshnessLifetime(entry)
	if !ok {
		return false
	}
	age := policy.currentAge(entry)

	return age >= lifetime && age-lifetime <= allowed
}

func (policy *cachePolicy) staleWhileRevalidate(entry CacheEntry) bool {
	directives := parseCacheControl(entry.Header.Values("Cache-Control"))
	if staleReuseProhibited(directives, policy.shared) {
		return false
	}
	value, exists := directives["stale-while-revalidate"]
	if !exists {
		return false
	}
	allowed, valid := parseDeltaSeconds(value)
	if !valid {
		return false
	}
	lifetime, ok := policy.freshnessLifetime(entry)
	if !ok {
		return false
	}
	age := policy.currentAge(entry)

	return age >= lifetime && age-lifetime <= allowed
}

func cacheRevalidationReplayable(request *http.Request) bool {
	return request.Body == nil || request.Body == http.NoBody || request.GetBody != nil
}

func (policy *cachePolicy) currentAge(entry CacheEntry) time.Duration {
	date, ok := parseHTTPDate(entry.Header.Get("Date"))
	if !ok {
		date = entry.ResponseTime
	}
	apparentAge := max(entry.ResponseTime.Sub(date), 0)
	ageValue := parseAge(entry.Header.Get("Age"))
	correctedReceivedAge := max(apparentAge, ageValue)
	responseDelay := max(entry.ResponseTime.Sub(entry.RequestTime), 0)
	residentTime := max(policy.clock.Now().Sub(entry.ResponseTime), 0)

	return correctedReceivedAge + responseDelay + residentTime
}

func cacheInvalidationTargets(request *http.Request, response *http.Response) []*http.Request {
	targets := []*http.Request{request}
	seen := map[string]struct{}{request.URL.String(): {}}
	for _, header := range []string{"Location", "Content-Location"} {
		reference := strings.TrimSpace(response.Header.Get(header))
		if reference == "" {
			continue
		}
		parsed, err := request.URL.Parse(reference)
		if err != nil || !sameCacheOrigin(request.URL, parsed) {
			continue
		}
		if _, exists := seen[parsed.String()]; exists {
			continue
		}
		seen[parsed.String()] = struct{}{}
		target := request.Clone(request.Context())
		target.URL = parsed
		targets = append(targets, target)
	}

	return targets
}

func sameCacheOrigin(left *url.URL, right *url.URL) bool {
	if left == nil || right == nil || left.User != nil || right.User != nil {
		return false
	}
	leftScheme := strings.ToLower(left.Scheme)
	rightScheme := strings.ToLower(right.Scheme)
	if leftScheme != rightScheme || !strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	port := func(target *url.URL, scheme string) string {
		if target.Port() != "" {
			return target.Port()
		}
		if scheme == "http" {
			return "80"
		}
		if scheme == "https" {
			return "443"
		}
		return ""
	}

	return port(left, leftScheme) == port(right, rightScheme)
}

func (policy *cachePolicy) cacheKey(request *http.Request) (string, error) {
	if request == nil {
		return "", &CacheError{Operation: "scope", Cause: ErrInvalidPolicyScope}
	}
	return policy.cacheKeyFor(request.Method, request)
}

func (policy *cachePolicy) cacheKeyFor(method string, request *http.Request) (string, error) {
	scope, err := ResolvePolicyScope(request, PolicyResourceCache)
	if err != nil {
		return "", &CacheError{Operation: "scope", Cause: err}
	}
	keyRequest := request.Clone(request.Context())
	keyRequest.Method = method
	var material string
	if policy.key != nil {
		value, err := policy.key(keyRequest)
		if err != nil {
			return "", &CacheError{Operation: "key", Cause: err}
		}
		material = value
	} else {
		target := *keyRequest.URL
		target.Fragment = ""
		material = method + "\x00" + target.String()
	}
	if len(material) == 0 || len(material) > maximumCacheKeyMaterial {
		return "", &CacheError{Operation: "key", Cause: ErrInvalidCache}
	}
	sum := sha256.Sum256([]byte(scope.String() + "\x00" + material))

	return policy.namespace + ":" + hex.EncodeToString(sum[:]), nil
}

func cacheModeFromRequest(request *http.Request) CacheMode {
	mode, ok := request.Context().Value(cacheModeContextKey{}).(CacheMode)
	if !ok || mode > CacheModeRefresh {
		return CacheModeDefault
	}

	return mode
}

func responseFromCache(
	request *http.Request,
	entry CacheEntry,
	provenance CacheProvenance,
	age time.Duration,
) *http.Response {
	if request.Body != nil {
		_ = request.Body.Close()
	}
	header := entry.Header.Clone()
	header.Set("Age", strconv.FormatInt(max(int64(age/time.Second), 0), 10))
	response := &http.Response{
		StatusCode:       entry.StatusCode,
		Status:           entry.Status,
		Proto:            entry.Proto,
		ProtoMajor:       entry.ProtoMajor,
		ProtoMinor:       entry.ProtoMinor,
		Header:           header,
		Trailer:          entry.Trailer.Clone(),
		TransferEncoding: append([]string(nil), entry.TransferEncoding...),
		Uncompressed:     entry.Uncompressed,
		Body:             io.NopCloser(bytes.NewReader(entry.Body)),
		ContentLength:    int64(len(entry.Body)),
		Request:          request,
	}
	if response.Status == "" {
		response.Status = fmt.Sprintf("%d %s", entry.StatusCode, http.StatusText(entry.StatusCode))
	}

	return withCacheMetadata(response, CacheMetadata{Provenance: provenance, Age: age})
}

func onlyIfCachedMiss(request *http.Request) *http.Response {
	if request.Body != nil {
		_ = request.Body.Close()
	}
	return withCacheMetadata(&http.Response{
		StatusCode:    http.StatusGatewayTimeout,
		Status:        "504 Gateway Timeout",
		Header:        make(http.Header),
		Body:          http.NoBody,
		ContentLength: 0,
		Request:       request,
	}, CacheMetadata{Provenance: CacheMiss})
}

func withCacheMetadata(response *http.Response, metadata CacheMetadata) *http.Response {
	if response == nil || response.Request == nil {
		return response
	}
	ctx := context.WithValue(response.Request.Context(), cacheMetadataContextKey{}, metadata)
	response.Request = response.Request.Clone(ctx)

	return response
}

type prefixedReadCloser struct {
	io.Reader
	closer io.Closer
}

func (body *prefixedReadCloser) Close() error { return body.closer.Close() }

func cloneCacheEntry(entry CacheEntry) CacheEntry {
	entry.Header = entry.Header.Clone()
	entry.Trailer = entry.Trailer.Clone()
	entry.TransferEncoding = append([]string(nil), entry.TransferEncoding...)
	entry.Body = append([]byte(nil), entry.Body...)
	entry.Vary = append([]string(nil), entry.Vary...)

	return entry
}

func validCacheEntry(entry CacheEntry) bool {
	if entry.StatusCode < 100 || entry.StatusCode > 599 || entry.Header == nil ||
		entry.ResponseTime.IsZero() || entry.RequestTime.IsZero() || !validCacheVariantID(entry.VariantID) {
		return false
	}
	for _, name := range entry.Vary {
		if name == "" || http.CanonicalHeaderKey(name) != name {
			return false
		}
	}

	return true
}

func parseVary(header http.Header) ([]string, bool) {
	var names []string
	seen := make(map[string]struct{})
	for _, line := range header.Values("Vary") {
		for _, value := range strings.Split(line, ",") {
			name := http.CanonicalHeaderKey(strings.TrimSpace(value))
			if name == "*" {
				return nil, false
			}
			if name == "" {
				continue
			}
			if _, exists := seen[name]; !exists {
				seen[name] = struct{}{}
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)

	return names, true
}

func (policy *cachePolicy) varyMatches(requestHeader http.Header, entry CacheEntry) bool {
	want := cacheVariantIdentity(policy.variantKey, entry.Vary, requestHeader)

	return hmac.Equal([]byte(entry.VariantID), []byte(want))
}

func cacheVariantIdentity(key []byte, vary []string, requestHeaders http.Header) string {
	digest := hmac.New(sha256.New, key)
	for _, name := range vary {
		_, _ = io.WriteString(digest, name)
		_, _ = digest.Write([]byte{0})
		_, _ = io.WriteString(digest, normalizeVaryValues(requestHeaders.Values(name)))
		_, _ = digest.Write([]byte{0})
	}

	return hex.EncodeToString(digest.Sum(nil))
}

func normalizeVaryValues(values []string) string {
	joined := strings.Join(values, ",")
	parts := splitQuotedList(joined)
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}

	return strings.Join(parts, ",")
}

func splitQuotedList(value string) []string {
	parts := make([]string, 0, strings.Count(value, ",")+1)
	start := 0
	quoted := false
	escaped := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if escaped {
			escaped = false
			continue
		}
		if quoted && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			continue
		}
		if character == ',' && !quoted {
			parts = append(parts, value[start:index])
			start = index + 1
		}
	}
	parts = append(parts, value[start:])

	return parts
}

func validCacheVariantID(value string) bool {
	decoded, err := hex.DecodeString(value)

	return err == nil && len(decoded) == sha256.Size
}

func parseCacheControl(lines []string) map[string]string {
	directives := make(map[string]string)
	for _, line := range lines {
		for _, part := range splitQuotedList(line) {
			name, value, found := strings.Cut(strings.TrimSpace(part), "=")
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			if _, exists := directives[name]; exists {
				continue
			}
			if found {
				value = strings.TrimSpace(value)
				if strings.HasPrefix(value, `"`) {
					if unquoted, err := strconv.Unquote(value); err == nil {
						value = unquoted
					}
				}
			}
			directives[name] = value
		}
	}

	return directives
}

func requestHasDirective(request *http.Request, name string) bool {
	_, exists := parseCacheControl(request.Header.Values("Cache-Control"))[name]

	return exists
}

func cacheMaxAge(directives map[string]string, shared bool) (time.Duration, bool) {
	if shared {
		if value, exists := directives["s-maxage"]; exists {
			return parseDeltaSeconds(value)
		}
	}
	value, exists := directives["max-age"]
	if !exists {
		return 0, false
	}

	return parseDeltaSeconds(value)
}

func parseDeltaSeconds(value string) (time.Duration, bool) {
	seconds, err := strconv.ParseUint(value, 10, 31)
	if err != nil {
		return 0, false
	}

	return time.Duration(seconds) * time.Second, true
}

func parseAge(value string) time.Duration {
	age, ok := parseDeltaSeconds(strings.TrimSpace(value))
	if !ok {
		return 0
	}

	return age
}

func parseHTTPDate(value string) (time.Time, bool) {
	parsed, err := http.ParseTime(value)
	if err != nil {
		return time.Time{}, false
	}

	return parsed, true
}

func sharedCachePermission(directives map[string]string) bool {
	_, public := directives["public"]
	_, revalidate := directives["must-revalidate"]
	_, sharedAge := directives["s-maxage"]

	return public || revalidate || sharedAge
}

func staleReuseProhibited(directives map[string]string, shared bool) bool {
	if _, blocked := directives["no-cache"]; blocked {
		return true
	}
	if _, blocked := directives["must-revalidate"]; blocked {
		return true
	}
	if shared {
		if _, blocked := directives["proxy-revalidate"]; blocked {
			return true
		}
		if _, blocked := directives["s-maxage"]; blocked {
			return true
		}
	}

	return false
}

func cacheErrorStatus(status int) bool {
	return status == http.StatusInternalServerError || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func requestCarriesIdentity(header http.Header) bool {
	return header.Get("Authorization") != "" || header.Get("Cookie") != ""
}

func hasValidator(entry CacheEntry) bool {
	return entry.Header.Get("ETag") != "" || entry.Header.Get("Last-Modified") != ""
}

func updateCachedHeaders(stored http.Header, validation http.Header) {
	for name, values := range validation {
		if strings.EqualFold(name, "Content-Length") || strings.EqualFold(name, "Connection") ||
			strings.EqualFold(name, "Vary") {
			continue
		}
		stored[name] = append([]string(nil), values...)
	}
}

func validCacheNamespace(namespace string) bool {
	for index := 0; index < len(namespace); index++ {
		character := namespace[index]
		if !lowerAlphaNumeric(character) && character != '.' && character != '_' && character != '-' {
			return false
		}
	}

	return namespace != ""
}

func resolveCacheMethods(configured []string) (map[string]struct{}, error) {
	if len(configured) == 0 {
		configured = []string{http.MethodGet}
	}
	methods := make(map[string]struct{}, len(configured))
	for _, method := range configured {
		if !validHTTPToken(method) {
			return nil, fmt.Errorf("%w: cacheable method is invalid", ErrInvalidCache)
		}
		if _, duplicate := methods[method]; duplicate {
			return nil, fmt.Errorf("%w: cacheable method is duplicated", ErrInvalidCache)
		}
		methods[method] = struct{}{}
	}

	return methods, nil
}

func resolveCacheStatuses(configured []int) (map[int]struct{}, error) {
	if len(configured) == 0 {
		configured = []int{http.StatusOK}
	}
	statuses := make(map[int]struct{}, len(configured))
	for _, status := range configured {
		if status < 200 || status > 599 {
			return nil, fmt.Errorf("%w: cacheable status is invalid", ErrInvalidCache)
		}
		if _, duplicate := statuses[status]; duplicate {
			return nil, fmt.Errorf("%w: cacheable status is duplicated", ErrInvalidCache)
		}
		statuses[status] = struct{}{}
	}

	return statuses, nil
}

func validHTTPToken(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character <= 32 || character >= 127 ||
			strings.ContainsRune("()<>@,;:\\\"/[]?={}", rune(character)) {
			return false
		}
	}

	return true
}

func safeCacheMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead ||
		method == http.MethodOptions || method == http.MethodTrace
}
