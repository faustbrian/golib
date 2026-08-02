# Goal: Harden Bulkhead Isolation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Prove isolation, conservation, fairness, boundedness, and drain behavior under
realistic overload and adversarial concurrent histories.

## Required Campaigns

- Saturate one partition while proving independent partitions remain usable.
- Race admission, cancellation, queue timeout, completion, panic, close, and
  partition eviction at every transition.
- Verify FIFO fairness, bounded waiter count, mixed weights, starvation, and
  head-of-line behavior.
- Verify no rejected or canceled call invokes protected work.
- Verify every admitted panic/error/cancellation releases exactly one permit.
- Exercise uncooperative operations that outlive context and prove the module
  does not falsely reclaim their capacity.
- Exercise recursive acquisition, slow observers, observer panic, and callback
  reentrancy without deadlock or state corruption.
- Model SIGTERM, readiness removal, drain timeout, abrupt kill, scale-up cold
  state, scale-down, and mixed Kubernetes revisions.
- Prove all queues, partition registries, observations, and diagnostics are
  cardinality and memory bounded.

## Comparative Evidence

Benchmark equivalent fixed-capacity semantics against Failsafe-Go, direct
semaphores, and maintained bulkhead implementations. Measure admitted and
rejected fast paths, wait wake-ups, p50/p95/p99 wait, fairness, throughput,
allocations, partitions, cancellation churn, and observers. Disclose semantic
differences such as fairness, queue limits, panic cleanup, and shutdown.

## Completion Gates

- Meaningful exact 100% statement coverage and exactly 100% viable mutation
  kills.
- Race, fuzz, stress, leak, fault, benchmark, API compatibility, docs,
  security, and supply-chain gates pass.
- Cross-package tests prove composition does not misclassify bulkhead rejection
  as downstream failure or create retry amplification.
- Final review finds no capacity leak, starvation, hidden global registry,
  unbounded queue, callback under lock, or misleading cluster-wide claim.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
