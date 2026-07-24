# HTTP Caching

Caching is optional operation middleware. It preserves `http.Request` and
`http.Response`, never installs a global backend, and stores only complete
bounded response bodies.

```go
store, err := httpclient.NewMemoryCache(httpclient.MemoryCacheOptions{
	MaximumEntries: 1_000,
	MaximumBytes:   64 << 20,
})
if err != nil {
	return err
}

cache, err := httpclient.NewCacheMiddleware(httpclient.CacheOptions{
	Name:             "vendor-cache",
	Layer:            httpclient.MiddlewareClient,
	Namespace:        "vendor-v1",
	Store:            store,
	MaximumBodyBytes: 8 << 20,
})
```

Register `cache` in `Config.Middleware`. The default policy stores only `GET`
responses with status 200 and explicit freshness from `max-age` or `Expires`.
`Methods`, `Statuses`, and `TTLOverride` make broader behavior explicit.
Unsafe methods are not cached by default; a successful unsafe request
invalidates the effective request URI and same-origin `Location` and
`Content-Location` targets.

Every persistent key includes the opaque default cache `PolicyScopeKey`:
origin, credential, tenant, and account. The same scoped key controls in-flight
request coalescing. Existing credential and cookie headers are hashed
automatically; credentials added later by attempt middleware require an
explicit request `PolicyScope`. See the [policy scope guide](policy-scopes.md).

## Freshness, validation, and stale responses

The middleware evaluates request and response cache directives, corrected
`Age`, `Date`, `Expires`, `max-age`, and shared-cache `s-maxage`. It selects
variants using normalized `Vary` values and conditionally revalidates stale
entries with `ETag` or `Last-Modified`. A 304 response updates permitted stored
headers and returns the stored representation.

`max-stale`, `stale-if-error`, `must-revalidate`, `proxy-revalidate`, and
`no-cache` are enforced before stale reuse. `stale-while-revalidate` is enabled
only when an application-owned scheduler is configured:

```go
type RevalidationQueue struct {
	// Application-owned workers and shutdown context.
}

func (queue *RevalidationQueue) ScheduleCacheRevalidation(
	task func(context.Context),
) error {
	// Queue task for a bounded worker. The worker supplies its lifecycle
	// context and must not invoke task inline.
	return queue.Enqueue(task)
}
```

The package never starts a detached revalidation goroutine. The scheduler owns
worker bounds, cancellation, draining, and shutdown. Background revalidation
is limited to safe methods and replayable request bodies. Concurrent misses,
refreshes, and revalidations for one opaque primary key are coalesced.

## Identity and shared-cache safety

Primary key material is SHA-256 hashed before reaching a backend. A custom
`CacheKeyFunc` can omit volatile URL components, but it must preserve every
dimension that changes the representation. Returned material is bounded and
must not consume the request body.

`Vary` identities are HMAC protected, so raw authorization, cookie, tenant, or
other header values never reach the store. The default HMAC key is random for
each middleware instance. Supply a stable secret `VariantKey` of at least 32
bytes when entries must survive process restarts or be shared by instances.
Namespaces are included in opaque primary keys.

Responses carrying `Set-Cookie` are never stored. Credentialed requests are
isolated conservatively. Shared caches reject private responses and require an
explicit shared-cache permission before storing or reusing authenticated
responses. Applications must still scope each store and variant key so tenant
or credential boundaries cannot share entries accidentally.

## Request controls and provenance

Use `WithCacheMode` for one request:

- `CacheModeDefault` performs normal lookup and storage;
- `CacheModeBypass` skips lookup and storage;
- `CacheModeRefresh` skips lookup and replaces a storable response.

`Range` and request `no-store` also bypass caching. `only-if-cached` returns a
synthetic 504 without network I/O when no reusable entry exists. Cache hits and
other short circuits close the request body under the normal RoundTripper
ownership contract.

`CacheMetadataFromResponse` reports `CacheMiss`, `CacheHit`,
`CacheRevalidated`, or `CacheStale` plus the computed age. It does not expose
URLs, keys, credentials, or bodies.

## Backends and failures

`MemoryCache` is a concurrency-safe finite FIFO reference backend. It copies
stored and loaded data, limits entry count and body bytes, and supports
multiple `Vary` variants per primary key. Implement `CacheStore` to use an
application backend; methods receive only opaque keys and protected variant
identities and must honor context cancellation.

Backend failures are fail-open by default so an unavailable cache does not
make the origin unavailable. Set `FailureMode: CacheFailClosed` when cache
integrity is required. Fail-closed errors are `CacheError` values whose text
does not render backend errors, keys, request data, or response bodies.

## Middleware composition

Cache middleware is operation-transport policy at priority `-1000`. It wraps
circuit-breaker and retry transport policy. A fresh hit therefore performs no
breaker call, retry, attempt-stage admission, signing, authentication, or
network I/O. Operation request policy, including initial limiter admission,
has already run. Misses and revalidation requests enter the inner policies
normally; retry remains bounded inside the one coalesced operation.
