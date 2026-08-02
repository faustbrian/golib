# Composition

Place policies so every physical downstream attempt crosses admission:

```text
shared work budget
  -> rate quota / throttle
  -> retry or hedge scheduler
       -> adaptive concurrency admission
            -> breaker admission
                 -> per-attempt timeout
                      -> downstream call
```

Exact breaker placement depends on whether breaker latency should include time
waiting for adaptive admission; local limiter rejection must never become a
downstream breaker failure. A retry or hedge must acquire its own permit and
must not reuse or bypass the original attempt's permit. A shared caller-owned
attempt and elapsed-time budget caps retry/hedge amplification.

Fixed resource partitioning belongs to `bulkhead`. Request-rate quotas belong
to `rate-limit` or `throttle`. Failure state belongs to `circuit-breaker`.
Timeouts, retries, hedges, caches, and fallbacks remain caller owned. Cache hits
normally sit outside downstream admission; cache fills and refreshes that call
the dependency must cross it.

Classify caller cancellation, breaker-open, rate-limit rejection, cache
short-circuit, and other local drops as ignored or local drop. Use overload only
for a credible capacity signal from an admitted downstream attempt.

The non-releasable `integration/resilience` module executes these public
contracts with the repository retry and hedge modules. It proves a local
admission rejection is not retried, each hedge attempt crosses admission, a
rejected hedge does not invoke downstream work or become a learning sample, and
all permits and the shared hedge budget are released.
