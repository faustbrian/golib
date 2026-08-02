# Goal: Adaptive Concurrency Limiting

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Build `concurrency-limit` as a production-grade adaptive in-flight concurrency
limiter that learns safe local capacity from latency, throughput, and explicit
outcome signals. It MUST reduce queue growth before widespread timeout while
remaining bounded, explainable, deterministic under test, and safe to reset on
pod lifecycle changes.

## Authoritative References

- Netflix concurrency-limits:
  https://github.com/Netflix/concurrency-limits
- Failsafe-Go adaptive limiter:
  https://failsafe-go.dev/adaptive-limiter/
- Google SRE overload guidance:
  https://sre.google/sre-book/handling-overload/
- primary publications for TCP Vegas, AIMD, Gradient/Gradient2, and any
  implemented estimator;
- Go timing, context, memory-model, race, fuzz, and profiling documentation.

## Product Boundary

This module owns adaptive in-flight admission. It MUST NOT own fixed semaphore
capacity, fixed bulkhead partitioning, request rate quotas, failure-rate
throttling, breaker state, retries, fallback, service discovery, or HPA.

Queueing in front of an adaptive limit MAY be supported as a bounded admission
feature, but reusable fixed bulkhead behavior belongs to `bulkhead`.

## Core Permit Model

Provide standalone permit admission and execution helpers:

- `Acquire` accepts context and optional bounded priority/partition metadata;
- each permit records exactly one terminal outcome;
- terminal outcomes distinguish success, dependency failure, local drop,
  ignored/canceled, and overload signal;
- elapsed execution time excludes local queue wait unless an algorithm
  explicitly requires total latency and names it;
- abandoned permits have explicit bounded handling; and
- snapshots expose current limit, in-flight, queued, samples, baseline,
  rejection, and algorithm state.

Construction MUST validate minimum, maximum, initial limit, sample windows,
quantiles, gains, smoothing, queue bounds, priority bounds, and all arithmetic.

## Algorithms

The package SHOULD implement independently selectable, documented algorithms:

- fixed baseline for control and migration;
- AIMD;
- Vegas-style queue estimate;
- Gradient2 or another reviewed latency-gradient algorithm; and
- a conservative default selected only after representative benchmarks.

Each algorithm MUST have a specification, equations, input classification,
state bounds, reset behavior, and deterministic reference model. Algorithm
names MUST not be used when behavior materially diverges from the cited model.

## Sampling And Adaptation

- Use monotonic elapsed time where available.
- Recent and baseline windows MUST be memory bounded.
- Minimum samples MUST prevent unstable changes from sparse traffic.
- Quantile estimation error and memory cost MUST be documented.
- Limit increases and decreases MUST be bounded per update.
- The limit MUST stay within configured minimum and maximum.
- Recovery probes MUST preserve some traffic without allowing runaway growth.
- Clock jumps, counter overflow, idle periods, workload shifts, and bimodal
  latency MUST have explicit behavior.
- Local rejection, breaker-open, rate-limit, and caller cancellation SHOULD be
  excluded from dependency-capacity learning by default.

## Queueing And Priority

Immediate rejection MUST be the simplest mode. Optional queueing MUST be
context-aware, bounded by count and wait, and have explicit fairness. Dynamic
queue bounds based on current limit MUST have absolute maxima.

Priority or tenant-aware admission MAY exist only with bounded key cardinality,
documented fairness, starvation controls, and resistance to caller-inflated
priority. Authorization of priority remains outside the package.

## Kubernetes And HPA

Adaptive state is process-local and pod-local. Every new pod begins with an
explicit initial/warm-start policy; it MUST NOT claim to know fleet capacity.
Scale-up multiplies aggregate concurrency and resets local baselines. Rolling
updates may temporarily mix algorithms and limits.

Docs MUST cover:

- sizing per-pod min/max from downstream fleet capacity and maximum replicas;
- conservative cold-start and optional caller-supplied snapshots with version
  validation;
- graceful drain and exclusion of shutdown cancellations from learning;
- why local rejection can lower CPU and create HPA feedback loops;
- metrics for current limit, in-flight, queue, rejection, and observed latency;
  and
- why a distributed adaptive limit is not implemented without a coherent
  control-plane design.

## Composition

Document and test placement relative to bulkhead, breaker, throttle, rate
limit, retry, hedge, timeout scopes, and cache. A retry or hedge attempt MUST
not bypass admission. Rejected local attempts MUST not become downstream
failure samples. Shared work budgets MUST cap retry/hedge amplification.

## Observability And Safety

Events and snapshots MUST be immutable, bounded, stable, and secret-safe.
Algorithm callbacks and observers MUST execute outside locks. Invalid numeric
state, NaN/Inf, overflow, observer panic, and classifier panic MUST not corrupt
the limiter or disable bounds.

## Documentation And Automation

Document algorithm math, tuning, sampling, workload suitability, queueing,
priority, Kubernetes lifecycle, HPA caveats, composition, migration, examples,
API, operations, dashboards, FAQ, security, benchmarks, and changelog.

Require meaningful exact 100% statement coverage, exactly 100% viable mutation
kills, equation/reference tests, simulation, race, fuzz, leak, fault,
benchmark, API compatibility, docs, security, supply-chain, and clean-consumer
gates.

## Acceptance Criteria

- Limits remain finite and within configured bounds for every history.
- Every admitted execution updates state at most once with the correct signal.
- The limiter converges and recovers in published reproducible simulations.
- Queueing and priority cannot become unbounded or starvation-prone.
- Pod-local learning and fleet-wide capacity changes are explicit.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
