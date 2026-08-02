# Goal: Harden Hedged Execution

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Prove that hedging reduces eligible tail latency without duplicate side-effect
assumptions, unbounded amplification, race-dependent results, or leaked work.

## Required Campaigns

- Deterministically race winner success, loser success, all failures, total
  deadline, caller cancellation, budget exhaustion, and timer firing.
- Test exact ties and every permutation of result arrival order.
- Test attempts that honor cancellation, delay cancellation, ignore context,
  panic, return resource-owning results, and block result delivery.
- Prove each result is returned or disposed exactly once.
- Fuzz delay schedules, attempt counts, classifiers, budgets, and attempt event
  sequences including overflow and zero boundaries.
- Verify shared budgets under concurrent callers and high-cardinality resource
  identities remain bounded and fair.
- Test retry, breaker, bulkhead, rate-limit, adaptive-throttle, cache, and
  per-attempt timeout compositions for accounting and amplification.
- Simulate Kubernetes scale-up, mixed rollout revisions, endpoint imbalance,
  and SIGTERM while hedges are scheduled and active.

## Performance Proof

Benchmark equivalent hedge semantics against Failsafe-Go and direct idiomatic
implementations. Report no-hedge overhead, one and multiple hedges, allocations,
timer pressure, contention, p50/p95/p99 latency, work amplification, winner
cleanup, and observers. Use reproducible latency distributions and disclose
whether competitors use dynamic delay, budgets, endpoint diversity, or result
cleanup.

## Completion Gates

- Meaningful exact 100% statement coverage and exactly 100% viable mutation
  kills.
- Race, fuzz, stress, leak, fault, benchmark, API compatibility, docs,
  security, and supply-chain gates pass.
- Integration evidence proves retries and hedges share a hard amplification
  bound.
- Final review finds no hidden replay, leaked loser, ambiguous winner, unbounded
  timer/goroutine, or misleading cancellation claim.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
