# http-client

`http-client` is a policy layer for typed outbound HTTP integrations. It is
built on `net/http`, preserves standard requests and responses, and is neutral
about vendor models and payload codecs.

The module is under active pre-v1 development. The current foundation provides
finite transport defaults, immutable request specifications, deterministic
operation and attempt middleware, origin-bound authentication, explicit
transport ownership, response lifecycle management, and redacted errors.

The [public API reference](docs/api-reference.md) maps the complete Go
documentation. Start with the [transport guide](docs/transport.md),
[typed integration patterns](docs/integrations.md), and
[error classification](docs/errors.md). Release-facing policies and additional
guides are indexed in the repository `docs` directory.

## Install

```console
go get github.com/faustbrian/golib/pkg/http-client
```

## Quickstart

```go
client, err := httpclient.New(httpclient.Config{})
if err != nil {
	return err
}
defer client.Close()

request, err := http.NewRequestWithContext(
	ctx,
	http.MethodGet,
	"https://api.example.com/widgets",
	nil,
)
if err != nil {
	return err
}

response, err := client.Do(request)
if err != nil {
	return err
}
defer response.Body.Close()
```

Reusable endpoint definitions can use an immutable request specification:

```go
spec, err := httpclient.NewRequestSpec(
	"https://api.example.com/v1/",
	"widgets",
)
if err != nil {
	return err
}

spec, err = spec.WithQuery(
	httpclient.LayerRequest,
	"include",
	httpclient.RepeatedQuery("owner", "history"),
)
if err != nil {
	return err
}

request, err := spec.Build(ctx, http.MethodGet)
```

Each build returns an independent `*http.Request`; its URL, headers, and query
state do not alias the specification or another build. See
[request construction and serialization](docs/request-construction.md) for
precedence, encoding, and body ownership details.

Request bodies include replayable byte and canonical form snapshots,
replayable factories, explicitly one-shot streams, and bounded streaming
multipart composition. Multipart bodies use a caller-supplied stable boundary,
derive replay safety from every part, compute an exact length when possible,
and close all owned part readers when encoding completes or is abandoned.
Immutable layered trailers use standard `net/http` framing and remain
replay-safe with replayable bodies.

Middleware is explicit, immutable, and local to a client or call. Register each
handler with a stage, operation-or-attempt scope, layer, priority, and stable
name:

```go
observe, err := httpclient.NewCompletionMiddleware(
	httpclient.MiddlewareOptions{
		Name:  "observe",
		Scope: httpclient.ScopeOperation,
		Layer: httpclient.MiddlewareClient,
	},
	func(
		request *http.Request,
		response *http.Response,
		failure error,
	) error {
		return nil
	},
)
```

Pass registrations through `Config.Middleware`, or add invocation-local
registrations with `Client.DoWithMiddleware`. See the
[middleware lifecycle guide](docs/middleware.md) for exact execution order,
short-circuit behavior, ownership, and error semantics.

Credentials are immutable request editors applied to each trusted physical
attempt. Authentication requires HTTPS and is same-origin by default,
including redirects:

```go
bearer, err := httpclient.NewBearerAuth(token)
if err != nil {
	return err
}

authentication, err := httpclient.NewAuthenticationMiddleware(
	httpclient.AuthenticationOptions{
		Name:  "vendor-auth",
		Layer: httpclient.MiddlewareClient,
	},
	bearer,
)
if err != nil {
	return err
}

client, err := httpclient.New(httpclient.Config{
	Middleware: authentication,
})
```

See the [authentication cookbook](docs/authentication.md) for Basic, bearer,
API-key, HMAC, OAuth2 token-source, client-credentials, redirect-boundary, and
generated-client examples.

Optional egress enforcement applies exact scheme, host, port, origin, CIDR,
and address-class policy to attempts, redirects, proxies, DNS answers, and
connection targets. DNS answers are all validated before any numeric-address
dial, preventing validation-to-connection rebinding. Immutable TLS policy can
set roots, server name, client identity, and rotating SPKI pins without
disabling normal certificate verification. See the
[egress security guide](docs/egress.md).

Opaque policy-scope keys separate transport, cookie, token, cache, coalescing,
limiter, breaker, and metric state by explicit resource defaults. Cache and
coalescing keys enforce origin, credential, tenant, and account separation.
See the [policy scope guide](docs/policy-scopes.md).

Named `interactive/v1`, `batch/v1`, `streaming/v1`, and
`webhook-delivery/v1` policy profiles expose finite defaults with deterministic
profile, client, and request precedence. Resolved values and provenance are
available to both operation and attempt middleware. See the
[policy profile guide](docs/policy-profiles.md).

Optional telemetry models one logical-operation lifecycle and numbered
physical-attempt lifecycles for retries, redirects, and revalidation. It offers
safe `slog`/`log` hooks, standard OpenTelemetry/telemetry adapter ports,
strict W3C Trace Context, baggage allowlists, and an enforced low-cardinality
metric projection. See the [observability guide](docs/observability.md).

Strict ordered fixtures support deterministic vendor contract tests with
bounded scripted failures, selected headers and trailers, and unused-script
verification. An optional recorder persists only versioned sanitized data:
request bodies become match-only digests, response bodies require an explicit
redactor, and credentials, secret query fields, volatile headers, and raw
transport errors are excluded. See the
[sanitized HTTP fixture guide](docs/testing-fixtures.md).

Cookie state is disabled by default. Opt in with an isolated jar and a strict
same-origin redirect policy:

```go
client, err := httpclient.New(httpclient.Config{
	Session: &httpclient.SessionConfig{},
})
```

The secure default uses the maintained public-suffix list. Custom jars,
cross-origin jar scope, persistence, and ownership are explicit; see the
[cookies and isolated sessions guide](docs/cookies.md).

Every `Client.Do` receives one logical operation identity that remains stable
across redirects and retry attempts. Endpoints can opt into a separate
idempotency key policy:

```go
idempotency, err := httpclient.NewIdempotencyMiddleware(
	httpclient.IdempotencyOptions{
		Name:  "create-widget-idempotency",
		Layer: httpclient.MiddlewareEndpoint,
	},
)
```

Generated keys use 128 bits of cryptographic entropy by default. A key does not
make an unsafe operation retryable; retry policy must still classify the method,
body replayability, endpoint contract, and outcome. See the
[operation identity and idempotency guide](docs/idempotency.md).

Retry is explicit endpoint policy and disabled unless registered:

```go
retry, err := httpclient.NewRetryMiddleware(httpclient.RetryOptions{
	Name:            "list-widgets-retry",
	Layer:           httpclient.MiddlewareEndpoint,
	MaximumAttempts: 3,
})
```

The default policy retries replayable safe or idempotent methods for transient
transport failures and selected transient statuses. Unsafe methods additionally
require both endpoint opt-in and applied idempotency middleware. See the
[retry safety guide](docs/retries.md).

Proactive admission and server-directed throttling compose at the physical
attempt boundary:

```go
limiter, err := httpclient.NewTokenBucketLimiter(
	httpclient.TokenBucketOptions{Rate: 20, Burst: 40},
)
rateLimit, err := httpclient.NewRateLimitMiddleware(
	httpclient.RateLimitOptions{
		Name:    "vendor-rate-limit",
		Layer:   httpclient.MiddlewareClient,
		Limiter: limiter,
	},
)
```

Every wait is bounded and cancellation-aware. `Retry-After` is observed by
default; vendor remaining/reset headers are opt-in and configurable. See the
[rate-limit and admission guide](docs/rate-limits.md).

Circuit breaking wraps the complete logical operation, including its bounded
retries, through a narrow port. The first-party adapter uses
`circuit-breaker` for all state and half-open probe control:

```go
classifier, err := httpclient.NewGoCircuitBreakerClassifier(nil)
circuit, err := breaker.New(breaker.Config{
	Name:       "widgets",
	Classifier: classifier,
})
adapter, err := httpclient.NewGoCircuitBreakerAdapter(circuit)
circuitPolicy, err := httpclient.NewCircuitBreakerMiddleware(
	httpclient.CircuitBreakerOptions{
		Name:    "widgets-circuit",
		Layer:   httpclient.MiddlewareClient,
		Breaker: adapter,
	},
)
```

The breaker remains caller-owned. See the
[circuit-breaker composition guide](docs/circuit-breakers.md).

Typed pagination remains lazy and vendor-model neutral:

```go
paginator, err := httpclient.NewCursorPaginator(
	httpclient.CursorPaginationOptions[Widget]{
		Fetch: func(
			ctx context.Context,
			cursor string,
		) (httpclient.CursorPaginationPage[Widget], error) {
			return widgetsPage(ctx, cursor)
		},
	},
)

widget, ok, err := paginator.Next(ctx)
```

Page, offset, cursor, Link-header, and custom continuations share the same
finite budgets, cycle detection, cancellation, and exact resume state. See the
[pagination guide](docs/pagination.md).

Bounded request pools provide controlled fan-out without creating one
goroutine per request:

```go
pool, err := httpclient.NewPool(
	httpclient.PoolOptions[WidgetID, Widget]{
		Concurrency: 4,
		Pending:     8,
		Key: func(id WidgetID) (string, error) {
			return id.String(), nil
		},
		Execute: func(
			ctx context.Context,
			id WidgetID,
		) (httpclient.PoolValue[Widget], error) {
			widget, responseBytes, err := getWidget(ctx, id)

			return httpclient.PoolValue[Widget]{
				Value:         widget,
				ResponseBytes: responseBytes,
			}, err
		},
	},
)
results, err := pool.RunSlice(ctx, widgetIDs)
```

Slice, generator, and channel sources share fixed or dynamically selected
worker bounds, bounded pending work, fail-fast or collect-all behavior, stable
input or completion ordering, and finite request, elapsed, response-byte, and
memory budgets. See the [request-pool guide](docs/request-pools.md).

Optional RFC-aware caching preserves standard HTTP access while bounding
stored bodies and coalescing concurrent misses:

```go
store, err := httpclient.NewMemoryCache(httpclient.MemoryCacheOptions{})
cache, err := httpclient.NewCacheMiddleware(httpclient.CacheOptions{
	Name:      "vendor-cache",
	Layer:     httpclient.MiddlewareClient,
	Namespace: "vendor-v1",
	Store:     store,
})
```

The cache supports freshness and age calculation, `Vary`, ETag and
Last-Modified validation, safe shared-cache rules, explicit methods and
statuses, request bypass and refresh modes, bounded stale behavior, hashed
custom keys, same-origin invalidation, and response provenance. The in-memory
backend is finite; applications can provide another `CacheStore` without
adding a mandatory service dependency. Background revalidation requires an
application-owned bounded scheduler. See the [HTTP cache guide](docs/caching.md).

Bounded typed JSON and caller-selected codec decoding have an explicit
consume-and-close contract:

```go
widget, err := httpclient.DecodeJSONResponse[Widget](
	response,
	httpclient.DecodeOptions{MaximumBodyBytes: 1 << 20},
)
```

The generic codec helper requires explicit media types and rejects unread
trailing bytes; the JSON helper understands JSON document boundaries. Both
handle protocol-defined empty responses and return secret-safe typed limit,
declared-length, decode, and body lifecycle errors. Independent status
classification keeps accepted bodies caller-owned while boundedly draining and
closing rejected responses. Callers can use `DrainResponse` to boundedly
consume and close an otherwise unused final body for connection reuse. Safe
vendor excerpts require an explicit redactor. See the
[response guide](docs/responses.md).

Explicit gzip policy keeps compressed input measurable and bounded:

```go
compression, err := httpclient.NewCompressionMiddleware(
	httpclient.CompressionOptions{
		Name:                     "vendor-gzip",
		Layer:                    httpclient.MiddlewareClient,
		MaximumDecompressedBytes: 64 << 20,
		MaximumExpansionRatio:    100,
	},
)
```

Response decoding streams with absolute and expansion-ratio limits. Optional
request gzip preserves replayability and joins its owned compressor worker on
body close. See the [compression guide](docs/compression.md).

Bounded streaming transfers copy into caller-owned writers with optional
length, SHA-256 or SHA-512, cancellation, and throttled progress policy:

```go
result, err := httpclient.CopyResponse(
	ctx,
	response,
	destination,
	httpclient.TransferOptions{MaximumBytes: 512 << 20},
)
```

Response bodies are always closed, destinations are never closed, and failures
retain partial-byte results through secret-safe typed errors. Atomic file
transfers validate and sync a same-directory temporary file before replacing
the destination. Strict range helpers construct `If-Range` requests and
distinguish validated continuation, restart fallback, and already-complete
responses. High-level resume execution persists same-directory partial files,
rolls rejected appends back to their safe offset, validates the complete file,
and publishes atomically. See the [transfer guide](docs/transfers.md).

Callers retain direct access to the underlying standard client through
`Client.HTTPClient`. Configuration must be completed before sharing a client
between goroutines. Calls made directly through that standard client bypass the
logical-operation pipeline; use `Client.Do` or `Client.DoWithMiddleware` when
middleware policy must apply.

## Ownership

The zero configuration creates an internally owned `http.Transport`.
`Client.Close` cancels pending requests, closes response bodies that callers
have not closed, and closes idle connections owned by the client.

A custom transport is borrowed by default and is not closed implicitly. Set
`TransportOwnership` to `TransportOwned` only when the client should own its
idle-connection lifecycle. `Client.CloseIdleConnections` is always explicit and
therefore applies to either ownership mode.

## Security

Transport errors intentionally omit their cause from rendered messages because
standard-library URL errors can contain sensitive query parameters. The cause
remains available through `errors.Is`, `errors.As`, and `errors.Unwrap`.

Do not put credentials in URLs. Query API keys require the explicitly named
`NewAPIKeyQuery` constructor and remain visible to servers and intermediaries;
prefer header credentials. Rendered authentication and transport errors omit
credential values and query strings. See the maintained
[production hardening audit](docs/hardening.md) for the threat model, policy
matrix, findings, evidence, and release verdict.
