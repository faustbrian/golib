# Goal: Cohesive Resilience Architecture

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Complete a cohesive, explicit, dependency-light resilience stack across
`retry`, `circuit-breaker`, `rate-limit`, `cache`, `semaphore`, `bulkhead`,
`hedge`, `concurrency-limit`, `adaptive-throttle`, and `fault-injection`.
Each module MUST own one understandable policy boundary, compose without
hidden behavior, and remain independently usable.

The stack MUST protect services running as multiple short-lived Kubernetes
replicas. It MUST distinguish process-local protection from cluster-wide
coordination and MUST NOT imply that per-pod state is globally consistent.

## Authoritative References

At execution time, verify behavior against current primary sources:

- Go `context`, memory model, timers, race detector, fuzzing, and profiling
  documentation;
- Failsafe-Go policy and composition documentation:
  https://failsafe-go.dev/policies/
- Google SRE overload handling:
  https://sre.google/sre-book/handling-overload/
- Netflix concurrency-limits:
  https://github.com/Netflix/concurrency-limits
- Shopify Semian:
  https://github.com/Shopify/semian
- Microsoft bulkhead guidance:
  https://learn.microsoft.com/azure/architecture/patterns/bulkhead
- Kubernetes autoscaling and pod lifecycle documentation:
  https://kubernetes.io/docs/concepts/workloads/autoscaling/horizontal-pod-autoscale/
  and https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/

References inform requirements; they do not define this repository's public
API. Record consulted versions and unresolved ambiguities in package docs.

## Package Ownership

| Concern | Owner | Required Boundary |
| --- | --- | --- |
| Bounded repeat after completion | `retry` | No concurrency isolation or circuit state |
| Dependency health state | `circuit-breaker` | No timeout, fallback, retry, or bulkhead |
| Work admitted per time or quota | `rate-limit` | No in-flight concurrency ownership |
| Reusable results and stale policy | `cache` | No generic fallback engine |
| Counting and weighted permits | `semaphore` | No work queue or resource-health policy |
| Fixed concurrency isolation | `bulkhead` | No adaptive capacity algorithm |
| Concurrent duplicate attempts | `hedge` | No sequential retry semantics |
| Adaptive in-flight capacity | `concurrency-limit` | No time-based quota |
| Probabilistic overload shedding | `adaptive-throttle` | No circuit state or fixed rate quota |
| Deterministic failure injection | `fault-injection` | No production orchestration platform |

Modules MUST NOT duplicate state machines merely for API convenience. Optional
adapters MAY bridge modules through narrow public contracts without creating
dependency cycles.

## Deliberate Non-Packages

### Timeout

A standalone generic timeout module MUST NOT be created merely to wrap
`context.WithTimeout`. Total-operation deadlines belong to callers; queue wait,
per-attempt, transport, and shutdown deadlines belong to the policy or adapter
that can define their exact scope.

Any execution helper MUST state whether expiry merely stops waiting or
cooperatively cancels work. It MUST NOT claim that a goroutine or external side
effect has stopped when the operation ignores context cancellation. A future
timeout module requires a semantic capability beyond standard `context` and a
written decision proving that it cannot leak unbounded work.

### Fallback

A generic fallback runner MUST NOT be created. Choosing stale data, a default,
an alternate vendor, or degraded functionality is application policy and may
have authorization, freshness, or financial consequences. Package-owned
fallback behavior, such as explicitly eligible stale cache reads, remains with
the package that can enforce those invariants.

## Shared Execution Vocabulary

Packages SHOULD converge on compatible concepts without importing a universal
execution framework:

- `context.Context` carries caller cancellation and the total deadline;
- attempts have stable ordinal and policy-origin metadata;
- admission rejection is distinct from operation failure;
- success, failure, rejection, cancellation, timeout, and ignored outcomes are
  distinguishable;
- typed errors preserve `errors.Is` and `errors.As` behavior;
- observers receive bounded, immutable, secret-safe snapshots;
- clocks, timers, sleepers, and randomness are injectable where behavior
  depends on them;
- permit and completion APIs enforce exactly-once accounting; and
- no callback runs while an internal lock is held.

The packages MUST NOT share mutable package-global registries, clocks, random
sources, metrics, or policy state.

## Composition Contract

Composition order changes behavior and MUST be explicit in code and docs. The
documentation MUST provide worked timelines rather than one universal order.
At minimum it MUST explain:

- whether bulkhead/adaptive-limiter queue wait consumes the total deadline;
- whether a rate or concurrency permit is acquired once per logical operation
  or once per attempt;
- whether a circuit breaker observes each attempt or the final logical result;
- whether retry can handle breaker, throttle, rate, or bulkhead rejection;
- how retry and hedge budgets prevent multiplicative amplification;
- whether hedge attempts share or independently acquire admission permits;
- whether cache lookup occurs before load-shedding policies;
- which failures are excluded from adaptive-throttle samples; and
- when an operation-specific fallback is outside all protective policies.

`http-client` MUST expose reviewed presets for common outbound calls while
allowing explicit policy ordering. Presets MUST NOT silently retry non-replayable
bodies, hedge non-idempotent work, or weaken caller deadlines.

## Budgets And Amplification

Retries and hedges MUST support shared budgets that bound additional work by
resource or operation class. A budget MUST have bounded cardinality, an
injectable clock, observable exhaustion, and process-local semantics unless a
distributed implementation is explicitly selected.

Documentation MUST calculate worst-case work amplification from attempts,
hedges, redirects, pagination, fan-out, and replica count. It MUST show that a
policy composition cannot accidentally create `retries * hedges` unbounded
parallel work.

## Kubernetes And Horizontal Scaling

Every stateful module MUST document:

- whether state is invocation-local, process-local, pod-local, or distributed;
- how effective capacity changes as replicas scale up or down;
- cold-start and rolling-update behavior when local history is empty;
- graceful termination: stop admission, cancel waits, drain accepted work,
  release permits, stop timers, and close observers;
- what happens when a pod is killed before reporting completion;
- whether configuration changes can temporarily double capacity across
  revisions;
- which metrics are safe for HPA and which create dangerous feedback loops;
- metric aggregation, stable low-cardinality labels, and missing-series
  behavior; and
- why dependency failure or an open breaker MUST NOT by itself fail liveness.

Memory-backed rate limits, breakers, caches, semaphores, bulkheads, adaptive
limiters, throttlers, and budgets are per replica. Their aggregate capacity or
sample behavior changes with replica count. Cluster-wide guarantees require an
explicit distributed backend or infrastructure primitive; they MUST NOT be
simulated with best-effort pod communication.

Readiness MAY stop new traffic during drain. Dependency degradation SHOULD be
reported separately from process health so Kubernetes does not amplify an
outage through restart loops.

## Observability

All modules MUST provide vendor-neutral observations sufficient to answer:

- what was admitted, rejected, delayed, retried, hedged, served stale, or
  fault-injected;
- why a decision occurred and which policy revision made it;
- current bounded state, utilization, queue depth, and budget remaining;
- duration spent waiting versus executing; and
- whether the state is local or distributed.

Core modules MUST NOT require OpenTelemetry or a logging package. Optional
adapters MAY emit metrics, logs, and spans. Labels MUST be bounded and MUST NOT
contain raw URLs, credentials, tenant identifiers, arbitrary errors, or
attacker-controlled keys.

## Failure And Shutdown Semantics

- Cancellation MUST remove waiters and release capacity without races.
- Panic handling MUST be explicit, account for the operation once, clean up,
  then preserve the original panic when configured to re-panic.
- Observer failure MUST NOT corrupt policy state or change decisions unless an
  explicitly selected fail-closed observer contract requires it.
- Slow or reentrant callbacks MUST NOT deadlock the policy.
- Timers and goroutines MUST have bounded ownership and deterministic shutdown.
- A caller that abandons a permit MUST not permanently leak capacity; expiry or
  an explicit lifecycle contract is REQUIRED.
- Errors MUST distinguish local rejection from downstream failure so outer
  policies do not learn from the wrong signal.

## Repository Deliverables

1. Implement each package's `GOAL.md` before its hardening goal.
2. Execute each existing package's supplemental `GOAL_RESILIENCE.md` without
   rewriting historical base goals.
3. Publish a repository resilience guide with package selection, composition
   diagrams, timing examples, Kubernetes deployment guidance, error taxonomy,
   migration guidance, and anti-patterns.
4. Add equivalent-behavior benchmarks against maintained competitors, with
   environment, versions, semantics, allocation data, and statistical method.
5. Add root CI selection so every module's local contract runs independently
   and reverse dependants run when public contracts change.

## Acceptance Criteria

- Every listed concern has one unambiguous owner and no hidden duplicate state.
- Timeout and fallback boundaries are documented without unnecessary wrappers.
- Composition behavior is proven for every supported package combination.
- Kubernetes replica scaling, rollout, shutdown, and HPA implications are
  explicit and tested where executable.
- Every production package has meaningful exact 100% statement coverage and
  exactly 100% viable mutation kills under repository policy.
- Race, fuzz, leak, fault, benchmark, API compatibility, documentation, and
  supply-chain gates pass for all affected modules.
- All user-facing APIs, decisions, caveats, examples, and operational failure
  modes are documented and linked from the repository entry point.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
