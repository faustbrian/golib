# Operations

## Readiness

`Inspector.Health` is a bounded dependency-connectivity probe. Do not wire a
broker outage to process liveness or an automatic restart. Readiness must also
inspect every required topic and evaluate partition leadership, offline and
under-replicated state, offsets, replication factor, and
`min.insync.replicas` against deployment policy. Stateful readiness
thresholds/hysteresis remain application-owned until the package exposes that
policy.

Use `Inspector.Cluster` for cluster/controller/broker identity and
`Inspector.Topics` with explicit targets for topic state. Treat any inspection
error as unknown diagnostic state rather than silently healthy.

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
