# Composition

Composition is application policy. `bulkhead` does not import retry, breaker,
rate-limit, adaptive-limit, hedge, cache, timeout, or fallback packages.

## Common outbound timeline

One conservative logical-call timeline is:

```text
caller total context
  -> validate and authorize
  -> cache lookup
  -> logical-operation rate admission
  -> retry budget
      -> per-attempt bulkhead admission
      -> circuit-breaker admission
      -> attempt timeout and transport operation
      -> classify only an executed downstream outcome
  -> application-owned fallback
```

Queue wait consumes the caller's total context. A bulkhead rejection occurs
before protected work and must not be reported to the breaker as downstream
failure. If breaker admission precedes bulkhead admission instead, the caller
must cancel the unused breaker permit without classification.

## Retry

Default to treating `ErrRejected`, `ErrQueueFull`, `ErrWaitTimeout`, and
`ErrClosed` as local terminal admission outcomes. Retrying them can amplify
overload. If a reviewed policy retries admission, every retry must share a
finite attempt, elapsed, and queue-wait budget. A permit is normally acquired
per downstream attempt so sleeps do not occupy resource capacity.

The non-releasable
[`integration/resilience`](../integration/resilience) module executes this
ordering through the public bulkhead, retry, and circuit-breaker APIs. Its
saturation test proves one local rejection produces one retry attempt, no
breaker admission, and no downstream call.

## Circuit breaker

The breaker owns dependency health; the bulkhead owns local capacity. Only a
callback that actually ran produces a downstream result for breaker
classification. Rejection, queue saturation, caller cancellation before grant,
and shutdown are not dependency failures.

## Rate limiting

A rate permit normally applies once per logical operation when the intent is
user or tenant rate control. Applying it per retry attempt measures downstream
work instead. State which behavior the application chose. Rate-over-time and
in-flight concurrency remain different policies.

## Adaptive concurrency

An adaptive limiter may choose a lower changing limit while the bulkhead
provides the fixed hard resource ceiling. Put adaptive admission outside the
bulkhead so rejected adaptive samples do not consume fixed capacity. Do not
feed bulkhead rejection or queue time into downstream latency samples unless
the adaptive algorithm explicitly models local admission.

## Hedges

Every hedge attempt consumes its own bulkhead weight. Attempts share the
logical deadline and a finite hedge budget. Never acquire one permit and run
multiple concurrent attempts behind it. Worst-case in-flight downstream work
is:

```text
logical_calls * attempts_per_call * hedges_per_attempt * fanout
```

The bulkhead capacity must cap the actual concurrent attempts, not only the
number of logical calls.

## Timeout and fallback

The caller or transport owns attempt timeouts. A timeout does not stop a Go
callback that ignores cancellation, so the bulkhead retains its permit until
return. Fallback choice belongs outside generic resilience policies because it
can carry freshness, authorization, or financial semantics.
