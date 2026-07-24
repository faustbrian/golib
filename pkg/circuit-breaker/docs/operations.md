# Operations, observability, and incident response

## Metrics and events

Poll immutable snapshots at an operator-chosen interval. Recommended
low-cardinality series include state/mode, transition count, classified window
counts, failure/slow ratios when defined, admitted/rejected totals, active
probes, observer failures, and dropped events. Labels should be limited to a
stable caller-controlled breaker name and deployment dimensions.

Never label with endpoints containing IDs, operation results, errors, users,
request IDs, or secrets. Rejections emit no event by default, which prevents an
open dependency from amplifying logs/traces. Transition observers should emit
one event per committed transition.

Synchronous callbacks from different transitioning callers may overlap and
must be concurrency-safe. Generation establishes committed state order;
callback completion order is not an ordering guarantee, and event timestamps
are observational metadata that may repeat or move backward. Async delivery
uses one bounded worker and explicitly counts dropped events. The creator owns
lifecycle: `Close` is a callback-safe nonblocking request, while `Shutdown(ctx)`
waits for the callback and queue and must not be called from the async callback.

Alert on sustained open state, repeated open/half-open oscillation, unexpected
force-open/isolation, observer failures/drops, and a rising failure or slow ratio
near threshold. Dashboard ratio definedness separately from a numeric zero.

## Kubernetes and replicas

Each process makes local decisions from its observed dependency path. Do not
synchronize admission through Valkey, PostgreSQL, or a control plane. Aggregate
telemetry across replicas for visibility, while allowing replicas to recover at
slightly different times. Downward jitter reduces synchronized probes.

Do not couple readiness/liveness directly to one dependency breaker without an
application-specific availability decision; doing so can turn a dependency
incident into a restart storm.

## Incident runbook

1. Inspect `Snapshot`: state, mode, generation, ratios, throughput, next probe,
   probe progress, and observer health.
2. Confirm whether the signal is dependency failure, dependency latency, caller
   cancellation, or a local rejection misclassified as failure.
3. Reduce retries/load outside the breaker before increasing thresholds.
4. Use `Isolate` for controlled maintenance or `ForceOpen` to stop dependency
   traffic. Record the operator action externally.
5. Use `Release` to resume the existing policy state, or `Reset` only when a new
   empty closed generation is intentional.
6. Watch half-open completion and oscillation. Do not repeatedly reset to force
   probes.

Administrative calls are process-local and reversible. They are not persisted
across restart.

## Tuning

Start with enough minimum throughput to resist a few random failures, a window
matching the dependency traffic horizon, and an open interval long enough for
the dependency to recover. Keep half-open probes small relative to dependency
capacity. Prefer a slow-call rule when latency collapse precedes hard failure.

Benchmark budgets are environment-specific. Track closed execution, open
rejection, snapshot, half-open contention, observer, and window benchmarks on a
stable runner. Investigate material regressions (a practical initial trigger is
20%) rather than encoding one machine's nanoseconds as a universal promise.

## Troubleshooting and FAQ

**Why did the breaker not open?** Check minimum throughput, ignored outcomes,
window expiry/eviction, and `OpenWhenAll` composition.

**Why is the ratio zero?** Check `FailureRatioDefined`/`SlowRatioDefined`; an
empty window has an undefined ratio represented numerically as zero.

**Why is a probe rejected after the interval?** The bounded half-open sample or
active capacity is full. Use fail-fast handling or explicitly configure finite
waiting.

**Why did completion not update the health window?** The permit may belong to a
stale generation or disabled mode. A successful `Complete` still increments
exactly one lifetime outcome total. Already completed, canceled, or expired
permits return their stable sentinel and do not increment totals.

`PermitTTL` applies to caller-owned two-step permits so abandoned work cannot
hold half-open capacity forever. `Execute` owns its permit internally and still
records one lifetime outcome if protected work terminates after TTL. If another
admission already expired that half-open permit, the late outcome does not
mutate the replacement generation or probe sample.

**Does `Close` disable the breaker?** No. It only requests async observer
shutdown. Use `Shutdown(ctx)` outside the callback when cleanup must be complete
before continuing.

**Should every client share one breaker?** Share only when calls represent the
same dependency boundary and failure domain. Do not use a global name registry.

**Can state be distributed?** Not in v1. Shared state adds a synchronous failure
dependency and needs a separate consistency/fencing design.

## Migration

When replacing an existing breaker, first match its unit of observation
(logical operation or attempt), classifier, minimum throughput, window horizon,
half-open recovery, and cancellation behavior. Deploy metrics-only comparison
where possible, then enable admission. Treat changed defaults, timing,
classifiers, error identity, transitions, and snapshots as semantic API changes.
