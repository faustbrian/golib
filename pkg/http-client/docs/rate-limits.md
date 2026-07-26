# Rate Limits and Admission Delays

Rate limiting controls physical attempts, not only top-level method calls.
Every retry and redirect must gain admission before authentication and network
I/O, preventing retry policy from bypassing the request budget.

```go
limiter, err := httpclient.NewTokenBucketLimiter(
	httpclient.TokenBucketOptions{
		Rate:  20,
		Burst: 40,
	},
)
middleware, err := httpclient.NewRateLimitMiddleware(
	httpclient.RateLimitOptions{
		Name:               "vendor-rate-limit",
		Layer:              httpclient.MiddlewareClient,
		Limiter:            limiter,
		MaximumWait:        10 * time.Second,
		MaximumServerDelay: time.Minute,
	},
)
```

Append all returned middleware values. Operation request policy reserves the
first attempt before breaker admission. Attempt request policy consumes that
reservation once and reserves every redirect or retry after it. Response
policy updates future admission from server direction. No background
goroutine, refill ticker, or detached timer is created.

## First-party algorithms

The constructors share one concurrency-safe `RateLimiter` contract:

- `NewFixedWindowLimiter` admits a finite count in each discrete window;
- `NewSlidingWindowLimiter` tracks exact reservation timestamps in the moving
  window;
- `NewTokenBucketLimiter` refills continuously at a requests-per-second rate
  and permits an explicit burst; and
- `NewLeakyBucketLimiter` schedules constant-rate admission and rejects work
  when its bounded pending capacity is full.

Rates must be finite, positive, and representable by `time.Duration`. Limits,
bursts, capacities, and windows must also be positive. Each limiter is an
in-process policy object; share one instance only across clients and endpoints
that intentionally share the same quota scope.

`Acquire` atomically reserves capacity before waiting. Cancellation after a
reservation consumes that capacity rather than making it available to a later
request, which prevents cancellation races from oversubscribing the server.
`MaximumWait` defaults to 30 seconds. A reservation that would exceed it is not
committed and returns `ErrRateLimitWaitExceeded`. A full leaky-bucket queue
returns `ErrRateLimitCapacity`.

## Server-directed admission

The default observer parses `Retry-After` as delta seconds or an HTTP date and
defers subsequent admission. This field is defined by
[RFC 9110 section 10.2.3](https://www.rfc-editor.org/rfc/rfc9110.html#section-10.2.3).
A server delay is clamped to `MaximumServerDelay`, which defaults to one minute.

There is not one universally deployed remaining/reset representation. Use
`NewHeaderRateLimitObserver` only for a known provider contract:

```go
observer, err := httpclient.NewHeaderRateLimitObserver(
	httpclient.HeaderRateLimitOptions{
		RemainingHeader: "X-RateLimit-Remaining",
		ResetHeader:     "X-RateLimit-Reset",
		Reset:           httpclient.RateLimitResetUnixSeconds,
	},
)
```

Reset values can be delta seconds, Unix seconds, or an HTTP date. Observation
occurs only when the remaining value is exactly zero. Malformed, incomplete,
or positive values are ignored. A valid `Retry-After` takes precedence.

`RateLimitObserver` supports other standardized or vendor fields without
forcing draft syntax into core. It receives the response and limiter time but
must not mutate headers or consume the body.

## Cancellation, errors, and response ownership

Admission waits use the attempt context. Caller cancellation, deadline expiry,
and `Client.Close` stop the wait. `RateLimitError` reports the requested wait
and unwraps the cause without rendering custom limiter messages or header
values. Observation errors close the response because no usable result can be
returned safely after policy failure.

Responses that merely announce a future limit remain caller-owned and readable.
The observer never buffers or consumes their bodies.

## Retry composition

One physical attempt produces one admission. The initial reservation happens
before circuit-breaker admission, so local limiter rejection bypasses breaker
state. Response observation happens before retry classification, making a
`Retry-After` deadline visible before the next physical attempt. With the
default wall clock, retry backoff and admission refer to the same elapsed time
and do not multiply the server delay.

Do not layer another SDK limiter or retry loop unless its independent quota and
attempt multiplication are explicitly bounded. Distributed coordination is an
application-provided `RateLimiter`; core provides only in-process algorithms.
