# Goal: Harden Adaptive Concurrency Limiting

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Prove algorithm correctness, bounded adaptation, stable recovery, fair
admission, and lifecycle safety under noisy and adversarial workloads.

## Required Campaigns

- Compare every limit update against deterministic reference equations.
- Simulate constant, bursty, ramp, bimodal, heavy-tail, periodic, sparse, and
  capacity-collapse workloads with reproducible seeds.
- Test downstream slowdown before errors, explicit overload, random failures,
  caller cancellations, local rejections, and workload-class shifts.
- Prove min/max bounds, finite numeric state, bounded windows, and no collapse
  to permanent rejection or runaway growth.
- Race permit completion, cancellation, duplicate completion, queue timeout,
  limit change, reset, snapshot, and shutdown.
- Test fairness and starvation with priorities, partitions, mixed durations,
  and dynamic queue bounds when supported.
- Simulate pod scale-up cold starts, mixed-version rollout, scale-down drain,
  abrupt death, and HPA feedback-loop scenarios.
- Prove observers, classifiers, quantile estimators, timers, waiters, and
  permits cannot leak or corrupt state.

## Comparative Evidence

Benchmark equivalent algorithms and semantics against Netflix
concurrency-limits, Failsafe-Go, and maintained Go implementations. Publish
convergence plots, utilization, goodput, rejection, queue latency, tail
latency, adaptation time, CPU, memory, and allocations. Avoid selecting only
workloads favorable to this implementation.

## Completion Gates

- Meaningful exact 100% statement coverage and exactly 100% viable mutation
  kills.
- Race, fuzz, simulation, stress, leak, fault, benchmark, API compatibility,
  docs, security, and supply-chain gates pass.
- Cross-package tests prove correct rejection classification and bounded
  retry/hedge interaction.
- Final review finds no unstable defaults, unbounded state, false global claim,
  completion leak, HPA recommendation without caveats, or dubious benchmark.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
