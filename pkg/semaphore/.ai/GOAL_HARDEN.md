# Goal: Harden Semaphore

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Prove that `semaphore` conserves capacity and terminates correctly under every
admission, cancellation, release, panic, contention, and shutdown interleaving.

## Required Hardening

- Build a small reference model and compare generated concurrent histories.
- Prove `available + acquired == capacity` at every observable quiescent point.
- Race cancellation against enqueue, grant, wake-up, context deadline,
  shutdown, and release.
- Race duplicate releases and confirm only one changes accounting.
- Test FIFO ordering, weighted head-of-line blocking, starvation resistance,
  queue bounds, oversize requests, and mixed weights.
- Test panic-safe convenience execution and observer panic/reentrancy.
- Test close before acquisition, while queued, while all capacity is held,
  during release, repeated close, and deadline-bounded drain.
- Test integer bounds and duration/context edge cases on supported platforms.
- Prove no goroutine, timer, waiter node, or permit reference leaks after each
  terminal path.

Use deterministic scheduling where practical and prolonged randomized stress
with reproducible seeds. Every discovered race MUST become a deterministic
regression.

## Performance Proof

Benchmark equivalent behavior against channels, `sync.Cond` designs,
`golang.org/x/sync/semaphore`, and maintained semaphore packages. Include
uncontended and contended acquire/release, cancellation churn, mixed weights,
fairness, allocations, queue depth, and observer overhead. Do not compare a
fair bounded implementation against an unfair unbounded one without disclosing
the semantic difference.

## Completion Gates

- Meaningful exact 100% statement coverage.
- Exactly 100% viable mutation kills.
- Race, fuzz, stress, leak, benchmark, API compatibility, docs, security, and
  supply-chain gates pass.
- Public docs prove ownership, local scope, fairness, shutdown, errors, and
  Kubernetes capacity behavior.
- Final review finds no global state, callback under lock, capacity inflation,
  stranded waiter, or hidden goroutine.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
