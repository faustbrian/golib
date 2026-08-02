# Goal: Harden Deterministic Fault Injection

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Prove reproducibility, safety isolation, adapter fidelity, boundedness, and
cleanup under concurrent and hostile use.

## Required Campaigns

- Golden-test every deterministic sequence and seeded random schedule.
- Race rule selection, counters, reset, snapshot, observer delivery,
  cancellation, shutdown, and concurrent adapter calls.
- Fuzz rule configuration, predicates, durations, byte boundaries, partial IO,
  sequence counts, probability, and composition order.
- Verify every adapter against the original interface's success, error,
  ownership, close, partial-result, and concurrency contracts.
- Verify delays and blocked operations terminate on caller cancellation.
- Verify injected panic/error/corruption leaves engine accounting consistent
  and resources closed.
- Prove disabled injectors cannot be activated through ambient environment or
  malformed metadata.
- Prove event fields are bounded and secret-safe under hostile inputs.
- Simulate multiple pods with identical and different seeds to demonstrate
  local scope and prevent false fleet-percentage claims.

## Security Review

Threat-model unauthorized activation, stale runtime controls, missing expiry,
rule escalation, wildcard targets, secret exfiltration through observations,
denial of service through latency or cardinality, and accidental publication of
dangerous defaults. Any production experiment adapter requires separate
security review and MUST fail closed on authorization while failing disabled on
configuration ambiguity.

## Performance Proof

Benchmark disabled, no-match, deterministic match, probabilistic match,
latency, byte corruption, and observer paths. Compare equivalent behavior with
Failsafe-Go/goresilience and direct test doubles. Publish CPU, allocations,
contention, throughput, and timer/goroutine cost without comparing in-process
simulation to network proxy fidelity as if equivalent.

## Completion Gates

- Meaningful exact 100% statement coverage and exactly 100% viable mutation
  kills.
- Race, fuzz, stress, leak, adapter conformance, benchmark, API compatibility,
  docs, security, and supply-chain gates pass.
- The package successfully drives failure campaigns for the other resilience
  modules without becoming their production dependency.
- Final review finds no ambient activation, unbounded blast radius, flaky
  randomness, adapter contract break, secret-bearing event, or leaked resource.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
