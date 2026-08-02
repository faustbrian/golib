# Goal: Harden Resilience Policy Composition

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Prove deterministic ordering, exact accounting, cancellation propagation, and
bounded execution across every supported policy composition.

## Required Campaigns

- Model generated policy stacks and compare invocation/return order with a
  deterministic reference executor.
- Exercise zero, one, repeated-compatible, and incompatible policies.
- Race cancellation, total deadline, budget admission, attempts, completion,
  observer callbacks, executor reuse, and shutdown.
- Prove retry and hedge cannot exceed one shared work budget under nesting,
  concurrency, panic, cancellation, or stale completion.
- Prove every physical attempt is accounted once and every local rejection is
  classified without invoking downstream work.
- Exercise uncooperative functions and prove no false cancellation or leaked
  result-delivery goroutine.
- Fuzz policy order, metadata, typed errors, budgets, clocks, and generated
  event histories.
- Test every supported focused-package combination and preserve discovered
  interleavings as deterministic regressions.
- Simulate pod scale-up, mixed revisions, SIGTERM, and abrupt loss while shared
  budgets and stateful policies are active.

## Comparative Evidence

Benchmark equivalent stacks against Failsafe-Go and direct nested functions.
Publish admitted/rejected fast paths, one and many policies, allocations,
contention, typed result cost, observation overhead, and budget contention.
Disclose semantic differences rather than optimizing away required behavior.

## Completion Gates

Meaningful exact 100% statement coverage, exactly 100% viable mutation kills,
race, fuzz, model, stress, leak, fault, benchmark, API compatibility, docs,
security, and supply-chain gates MUST pass. Final review MUST find no hidden
algorithm, policy-order ambiguity, accounting leak, global state, unbounded
metadata, or misleading cancellation/cluster claim.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
