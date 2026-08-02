# Goal: Bulkhead Isolation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Build `bulkhead` as a production-grade fixed-capacity isolation policy for
protecting finite resources and preventing one workload or dependency from
exhausting an entire process. It MUST provide explicit resource partitions,
bounded concurrent execution, bounded waiting, predictable rejection, and
graceful drain.

## Authoritative References

- Microsoft Bulkhead pattern:
  https://learn.microsoft.com/azure/architecture/patterns/bulkhead
- Shopify Semian: https://github.com/Shopify/semian
- Failsafe-Go bulkhead behavior: https://failsafe-go.dev/bulkhead/
- Go context, memory-model, synchronization, timer, race, and profiling docs;
- this repository's `semaphore` and resilience architecture contracts.

## Product Boundary

A semaphore counts permits. A bulkhead attaches fixed admission policy,
resource identity, wait bounds, execution lifecycle, observations, and
operational semantics to a protected resource. `bulkhead` MAY build on
`semaphore`, but MUST NOT duplicate its low-level permit accounting.

The module MUST NOT own adaptive capacity, rate-over-time limits, breaker
health state, retries, hedges, generic timeouts, worker pools, durable queues,
or distributed locking.

## Core Model

Provide immutable configuration for:

- stable caller-supplied resource identity;
- maximum concurrent executions or weighted capacity;
- immediate rejection or context-aware bounded waiting;
- maximum queued waiters;
- strict FIFO fairness by default;
- optional explicitly named partitioning policy;
- observer and clock dependencies; and
- shutdown/drain policy.

Provide standalone permit admission and generic context-aware execution APIs.
Every accepted operation MUST release capacity exactly once on success, error,
cancellation, and panic.

## Partitions And Resource Identity

- A bulkhead SHOULD be shared by all call paths consuming the same constrained
  resource.
- Independent failure domains SHOULD use independent bulkheads.
- Partition creation MUST be explicit and bounded; attacker-controlled keys
  MUST NOT create an unbounded registry.
- Eviction MUST NOT discard a partition with active or queued operations.
- Configuration changes MUST define what happens to existing partitions and
  permits.
- No package-global name registry is allowed.

Priority admission MAY be added only with documented starvation bounds and
tenant fairness. It MUST NOT be inferred from arbitrary context values.

## Admission And Execution

- Rejection, queue saturation, caller cancellation, wait timeout, shutdown,
  and protected-operation errors MUST remain distinguishable.
- Queue wait and execution duration MUST be reported separately.
- Waiting consumes the caller's total context deadline.
- An optional maximum wait MAY derive a child deadline but MUST NOT extend the
  caller deadline.
- Operations that ignore cancellation can continue holding capacity; the API
  and docs MUST state this rather than reporting false termination.
- Reentrant acquisition of the same exhausted bulkhead can deadlock and MUST be
  documented, detected where safely possible, or rejected through explicit
  policy.
- Callbacks and observers MUST run outside internal locks.

## Kubernetes And Horizontal Scaling

Bulkheads are process-local by default. Capacity is per pod, and aggregate
capacity changes with replica count and traffic distribution. Documentation
MUST provide sizing equations using pod count, downstream capacity, connection
pool size, request fan-out, and HPA maximum replicas.

The module MUST support application-driven drain:

1. readiness stops new external traffic;
2. bulkheads stop admitting new work;
3. queued callers are canceled or rejected according to policy;
4. admitted work drains under the pod termination deadline; and
5. incomplete work returns explicit cancellation/ambiguity.

Dependency saturation MUST NOT automatically mark the process dead. Queue
depth, rejection, and wait latency MAY feed alerts or carefully designed HPA
signals, but docs MUST warn that rejection can lower CPU and mislead autoscaling.

## Observability

Snapshots and events MUST expose capacity, active weight, queue depth,
admissions, rejections by reason, cancellations, wait duration, execution
duration, drain state, and policy revision. Resource labels MUST be caller
bounded and safe for metrics.

Observer failure MUST not alter admission. Synchronous observers, if supported,
MUST be opt-in and documented as part of request latency.

## Documentation And Verification

Document package selection, resource partitioning, queueing versus immediate
rejection, sizing, Kubernetes rollout/drain, composition order with retry,
breaker, rate limit, adaptive limiting, and hedge, anti-patterns, migration,
operations, examples, API, FAQ, and changelog.

Require meaningful exact 100% statement coverage, exactly 100% viable mutation
kills, reference-model histories, race, stress, fuzz, leak, cancellation,
shutdown, fault-injection, benchmark, API compatibility, security,
supply-chain, docs, and clean-consumer gates.

## Acceptance Criteria

- One overloaded resource cannot consume capacity reserved for another.
- Active plus available capacity is conserved under every terminal path.
- Waiting is bounded, fair, cancelable, and observable.
- Partition cardinality and queues remain bounded.
- Shutdown drains or rejects predictably within caller bounds.
- Per-pod and aggregate Kubernetes behavior is explicit and honest.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
