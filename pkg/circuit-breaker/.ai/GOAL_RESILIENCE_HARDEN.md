# Goal: Harden Circuit Breaker Resilience Integration

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Required Proof

- Generate histories mixing breaker transitions, local rejections, attempts,
  hedges, cancellation, timeout, reset, administrative modes, and shutdown.
- Prove stale generation completions cannot change current state.
- Prove half-open probes remain bounded when retry and hedge are configured.
- Simulate per-pod state divergence, pod restart, mixed revisions, dependency
  outage/recovery, and SIGTERM without restart loops or probe storms.
- Verify observer/classifier panic, reentrancy, slow callbacks, clock jumps,
  jitter bounds, and snapshot consistency.

Meaningful exact 100% statement coverage, exactly 100% viable mutation kills,
race, fuzz, stress, leak, fault, benchmark, API compatibility, docs, security,
and supply-chain gates MUST pass. Final review MUST find no false failure
sample, unbounded probe, global-state claim, stale generation mutation, or
liveness coupling.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
