# Operations

## Readiness

Use `Producer.Health` or `Inspector.Health` with a bounded readiness deadline.
Readiness must also verify required topic policy through infrastructure state;
broker reachability alone is insufficient.

## Lag

Use `Inspector.ConsumerGroupLag` with explicit group names. Alert on lag,
oldest-unprocessed age, handler failure rate, commit failure rate, and rebalance
frequency. Do not export record keys or values.

## Shutdown

Use `Producer.Shutdown` with the service's bounded shutdown context, or handle
the error returned by `Producer.Close`, whose bound is `ShutdownTimeout`.
Timeout fences new production but leaves the client open and admitted records
owned for a retry or explicit `Abort`; do not exit as if shutdown succeeded.

Cancel consumer and replay contexts, wait for the foreground operation, then
close those clients. A canceled consumer exits cleanly; replay cancellation is
an incomplete operator action and must be recorded.

## Recovery

Consumers replay through normal at-least-once group behavior. Audited historical
replay uses `ReplayReader`; never reset production group offsets as a substitute
for an explicit replay plan.
