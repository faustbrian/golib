# Composition and integration

Composition is ordered. The breaker observes exactly the dependency boundary
the caller places inside `Execute` or between permit acquisition/completion.

## Adoption checklist

1. Name one stable dependency/failure domain; do not include endpoint IDs,
   tenants, requests, or errors in the breaker name.
2. Choose count or time retention, minimum throughput, opening thresholds, and
   a bounded half-open recovery sample from observed workload behavior.
3. Define protocol classification before enabling admission. Decide explicitly
   how cancellation, deadlines, local saturation, and expected domain misses
   affect dependency health.
4. Place cache, validation, limiter, retry, authentication/signing, telemetry,
   transport, and fallback according to the logical-operation boundary below.
5. Preserve operation errors and protocol resources in caller code. Complete or
   cancel every two-step permit, and own async observer shutdown.
6. Roll out with snapshots and transition telemetry, compare against existing
   behavior, then tune from measured traffic rather than repeated resets.

## Recommended order

```text
cache -> local validation -> rate limit -> breaker -> retry -> auth/sign -> transport
```

This order records one logical operation after its retry policy. Put retry
outside the breaker only when attempt-level dependency health is intentionally
desired. Never wrap the same operation with the same breaker twice.

- Cache hits and fallbacks normally bypass admission.
- Rate limiting normally precedes admission so local rejection consumes no
  half-open probe.
- Bulkhead rejection is normally ignored unless it signals dependency
  saturation by explicit policy.
- The caller owns timeout creation. Classify cancellation/deadline according to
  whether the dependency caused it.
- Authentication/signing may occur inside only when it is part of the remote
  attempt; local credential/configuration failures should usually be ignored.
- Telemetry failure must not affect process-local admission.

## HTTP

`http-client` should own status classification and canonical middleware
order. Core does not read or close bodies. The caller/transport must close every
received body, including responses classified as failure. Classification may
inspect status and headers but must not consume or retain the body.

The integration contract harness proves that two retry attempts produce one
logical breaker success, intermediate bodies close once without being read,
the final body remains caller-owned, cache/validation/limiter/bulkhead/fallback
bypass the breaker, pre-admission cancellation invokes no transport, admitted
cancellation/deadline outcomes record exactly once, and rejection does not
invoke transport. The concrete `http-client` acceptance suite additionally
proves these retry, body, limiter, and cancellation boundaries through the
first-party adapter, including a bounded retry sequence owned by one half-open
probe.

## RPC

Map transport-unavailable, resource-exhausted, and server-side deadline codes
to dependency failure as appropriate. Treat caller cancellation, local marshal
errors, and authentication configuration explicitly. Preserve the original RPC
error; classification changes breaker state, not the returned error.

The nested consumer suite exercises this contract through the published
`jsonrpc` client and proves that local method validation is ignored while a
wrapped transport failure opens the breaker and later rejection skips the
transport.

## PostgreSQL and Valkey

Wrap the complete `QueryContext`, `ExecContext`, or command call. Connection,
protocol, and server availability failures are typical failures. Domain misses
such as `sql.ErrNoRows`, optimistic conflicts, and caller cancellation require
application policy. Do not hold a permit while rows are idle unless iteration
is deliberately part of the protected dependency operation; always close rows.

The nested consumer suite wraps the real `database/sql` `QueryContext` boundary
through a deterministic driver. It verifies dependency failure classification,
open rejection before another driver call, and caller ownership of successful
`Rows` closure.

For Valkey, distinguish local pool/bulkhead exhaustion from remote connection or
server failure. Cache misses are normally successful dependency interactions.

## Queues and object storage

Broker unavailability and publish failures are typical queue failures. A local
bounded producer queue being full is normally ignored. For consumers, decide
whether the protected operation is receive, handler execution, or ack; never
record one delivery into the same breaker at multiple layers.

For object storage, classify transport/server failures separately from expected
not-found and precondition responses. Close response bodies/streams outside
core. Multipart operations need a caller-owned aggregate policy so one logical
operation is recorded once.

## Filesystems and arbitrary functions

Remote filesystem gateway failures may be dependency failures; local path
validation and permission policy often are not. Two-step permits fit callbacks,
streaming, or async APIs that cannot live in one function, provided every permit
is completed or canceled within its finite TTL.
