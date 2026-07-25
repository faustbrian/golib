# Retry Safety and Backoff

Retry is disabled by default. Register `NewRetryMiddleware` only for endpoints
whose duplicate-request contract is understood. The middleware runs once per
logical operation and invokes the complete physical-attempt pipeline for every
exchange.

```go
retry, err := httpclient.NewRetryMiddleware(httpclient.RetryOptions{
	Name:              "read-widget-retry",
	Layer:             httpclient.MiddlewareEndpoint,
	MaximumAttempts:   3,
	MaximumElapsed:    10 * time.Second,
	BaseDelay:         100 * time.Millisecond,
	MaximumDelay:      2 * time.Second,
	MaximumRetryAfter: 30 * time.Second,
})
```

`MaximumAttempts` is required and includes the initial physical exchange. It
must be between 2 and 100. Zero duration fields select the finite defaults in
the example. The client-wide total timeout and caller deadline remain outer
bounds.

## Executable safety rules

The default policy retries only when the request body is absent or has a
working `GetBody` replay factory. It recognizes `GET`, `HEAD`, `OPTIONS`,
`TRACE`, `PUT`, and `DELETE` as safe or idempotent HTTP methods. Eligible
outcomes are non-cancellation transport failures and statuses `408`, `425`,
`429`, `500`, `502`, `503`, and `504`.

`POST`, `PATCH`, and other unsafe methods are not retried merely because a key
is present. They require all of the following:

- endpoint `RetryUnsafeWithIdempotency` opt-in;
- idempotency middleware applied to this logical operation;
- a validated caller or generated key selected by that middleware; and
- a replayable body.

A context key without endpoint idempotency middleware is insufficient. This
prevents a correlation value from being mistaken for a provider duplicate-
suppression contract.

`RetryPolicy` is the escape hatch for provider-specific classification. Its
callback receives a bodyless request snapshot, response or failure, physical
attempt number, replayability, and verified idempotency-policy state. It must
not consume or close the response body. A custom policy owns the safety
consequences of returning true.

## Delay and `Retry-After`

Absent usable server direction, delay grows exponentially from `BaseDelay` to
`MaximumDelay`. Cryptographic full jitter selects a value from zero through the
current bound to spread concurrent clients. `RetryJitter` is injectable for
deterministic tests.

`Retry-After` accepts delta seconds and HTTP dates. A valid value replaces
local backoff but is clamped to `MaximumRetryAfter`. Malformed values fall back
to exponential jitter; past dates mean no additional delay. A prospective wait
that would exceed `MaximumElapsed` is never started.

Every wait uses `RetryClock.Wait` with the operation context. The default uses
a stopped local timer, creates no goroutine, and returns promptly for caller
cancellation, deadline expiry, or client shutdown.

## Bodies, responses, and errors

Before another attempt, the middleware drains at most 64 KiB plus one byte and
closes the discarded response. Small responses can therefore return their
connection to the pool without allowing an unbounded error body to stall or
allocate. Drain or close failures stop retry.

Each replay calls the original request's `GetBody` and installs an independent
reader. A non-replayable stream never receives a second physical attempt.
Redirect replay remains under `net/http`; retry does not weaken its rules.

When a retryable exchange cannot continue because attempts, elapsed time,
replayability, delay, or body lifecycle is exhausted, the middleware returns
`RetryExhaustedError`. It exposes attempts, elapsed time, final status, and an
unwrap-able cause without rendering transport text, URLs, headers, keys, or
payloads. Non-retryable HTTP responses remain ordinary `http.Response` values.

## Operation composition

One operation identity and idempotency key remain stable across retries. Every
physical attempt reruns session, idempotency propagation, authentication,
signing, and attempt telemetry. This refreshes expiring credentials and
signatures without regenerating logical identity.

Do not nest provider SDK retries, generated-client retries, and this middleware.
Choose one retry owner so maximum attempts cannot multiply. Direct calls to
`Client.HTTPClient().Do` bypass this policy.
