# Middleware Lifecycle

The middleware pipeline is an immutable, inspectable policy plan around
ordinary `*http.Request` and `*http.Response` values. There is no package-global
registry and no implicit `init` registration.

## Registration identity and precedence

Every registration has:

- a stable lowercase `Name` of at most 64 bytes;
- `ScopeOperation` or `ScopeAttempt`;
- one lifecycle stage;
- a registration layer; and
- an integer priority.

Layers resolve from lowest to highest precedence:

1. `MiddlewareClient`
2. `MiddlewareEndpoint`
3. `MiddlewareRequest`
4. `MiddlewareOneShot`

A higher-layer registration replaces a lower-layer registration with the same
name, scope, and stage. Registering that identity twice at the same layer is an
error. Other registrations execute by stage, then priority, layer, and name.
Priority therefore defines semantic policy order across registration layers;
the layer breaks equal-priority ties. Names make the final order deterministic
without relying on append order.

`Pipeline.Inspect` and `Client.InspectPipeline` return independent operation and
attempt plans. Mutating an inspection result cannot mutate the pipeline.

## Built-in request-policy order

Request-stage priority is global across registration layers. The current
built-in order is:

1. logical operation identity at priority `-2000`;
2. operation and attempt telemetry at priority `-1900`;
3. endpoint idempotency selection and attempt propagation at `-1500`;
4. cache lookup and session redirect stripping at `-1000`;
5. attempt admission rate limiting at `-750`; and
6. authentication at its zero-value priority `0`.

This ensures authentication and HMAC signing can observe the resolved
operation identity and idempotency header. A request editor that must mutate
signed input uses a priority between session and authentication. A distinct
signing middleware should use a priority greater than authentication. The
resolved numeric priorities remain visible through pipeline inspection.

Retry is operation transport middleware. Every call to its `Next` enters the
attempt request stages again, so idempotency, session, authentication, and
signing policy are reevaluated in that order without regenerating operation
identity or the operation key.

Initial limiter admission is operation request policy. It reserves capacity
before breaker admission and marks that reservation for the first physical
attempt. Attempt request policy consumes that mark once, then every retry and
redirect reserves new capacity before authentication or network I/O. A local
limiter rejection therefore never enters breaker state.

The telemetry operation request handler wraps cache, breaker, and retry policy;
its attempt handler runs before attempt admission, authentication, signing, and
transport. The built-in circuit-breaker operation-transport priority is `-600`,
wrapping retry at `-500`. One breaker admission and outcome therefore cover the
complete logical operation, while retry continues to enter the attempt pipeline.
Attempt telemetry still runs per physical exchange. Inspect the resolved
pipeline when composing custom transport middleware; registration layer
controls replacement, while priority controls execution order.

## Scope and physical attempts

Operation middleware surrounds one `Client.Do` or `Client.DoWithMiddleware`
call. Attempt middleware surrounds every physical `RoundTrip` performed by the
standard client. Redirects therefore run attempt middleware again without
re-running operation middleware.

An operation transport middleware can call `Next` more than once to implement
a bounded retry policy. Every call enters a fresh attempt plan. The retry
middleware remains responsible for deciding whether the method and operation
are safe and whether `GetBody` can produce a fresh body. The core pipeline does
not retry automatically.

Calls made directly through `Client.HTTPClient()` do not enter the
logical-operation pipeline. Use `Client.Do` for the complete lifecycle.

## Stage order

Within each scope, the lifecycle is:

1. request middleware as nested around handlers;
2. transport middleware as nested around handlers;
3. the inner attempt scope or physical transport;
4. response middleware when a response exists;
5. error middleware when a failure exists; and
6. completion middleware for every exit.

The attempt scope completes before the operation transport handler resumes.
For a single network exchange, the effective order is:

```text
operation request before
  operation transport before
    attempt request before
      attempt transport before
        net/http RoundTrip
      attempt transport after
    attempt request after
    attempt response or error
    attempt completion
  operation transport after
operation request after
operation response or error
operation completion
```

Request and transport middleware can return a response without calling `Next`.
An operation short circuit skips the entire attempt plan. An attempt short
circuit skips network I/O. Synthetic responses are normalized with non-nil
headers and bodies and retain the request that produced them.

When a scope short circuits before invoking its terminal, the pipeline closes
the last request body passed through that scope. This applies normal
`RoundTripper` ownership even when no physical transport is reached and joins
streaming request workers such as gzip compression. Once a terminal is
invoked, body closure remains that terminal's responsibility.

## Request mutation and snapshots

The pipeline clones caller request metadata before operation middleware. Request
mutations are visible to later request and transport middleware in that scope
but cannot mutate the caller-owned URL or header map. Each attempt starts with a
new standard request clone.

Response, error, and completion middleware receive independent stable request
snapshots. Their URL and headers reflect the last request passed through the
around chain. The body is replaced with `http.NoBody`, and `GetBody` is removed,
so after-stage observers cannot consume or replay request payloads. Mutating one
snapshot cannot affect another observer.

## Response and error ownership

Response middleware may return the same response or a replacement. When it
returns a replacement, the pipeline closes the superseded body first. If a
response handler fails, returns nil, or cannot close a superseded body, every
response still owned by the pipeline is closed before error middleware runs.

Error middleware receives the current failure. It can:

- return `nil, failure` to propagate or replace the failure;
- return `response, nil` to recover with a synthetic response; or
- return `response, failure`, in which case the response is closed and the
  failure continues.

Returning `nil, nil` from error middleware is invalid. A response recovered by
error middleware does not re-enter response middleware; completion observes the
recovered final result.

Completion middleware always runs for its entered scope. A completion error
fails that scope and closes any successful response because the caller will not
receive ownership.

## Cancellation and panics

The pipeline checks request cancellation before every around handler and before
transport. Backoff, limiter, retry, authentication, or cache middleware must
also honor the request context while doing its own work. Completion middleware
still runs when cancellation prevents an operation from starting.

Panics from request, transport, response, error, completion, and the underlying
`RoundTripper` are contained as `MiddlewarePanicError`. The panic value remains
available in the typed error but is never included in `Error()` output.

Failures returned by response, error, and completion handlers are represented
by `MiddlewareExecutionError`, which exposes the resolved middleware metadata
and unwraps to the cause. Invalid response/error pairs use
`MiddlewareResultError`, which unwraps to `ErrInvalidMiddlewareResult` and any
underlying cause without rendering that cause. Rendered pipeline errors omit
causes and panic values that may contain request or payload data.

## Invocation-local middleware

`Client.DoWithMiddleware` resolves a derived pipeline without mutating the
client pipeline. Use `MiddlewareOneShot` for invocation-local policy. The same
name, scope, stage, and layer cannot be registered twice; use a higher layer to
replace client or endpoint policy deliberately.
