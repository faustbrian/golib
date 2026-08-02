# Goal: Harden HTTP Client Resilience Composition

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Required Proof

- Exercise every policy order with deterministic request timelines and assert
  physical attempts, logical results, classifications, and work budget.
- Race caller cancellation, admission, connect, headers, body streaming,
  retry delay, hedge winner, loser cleanup, cache refresh, and shutdown.
- Test replayable and non-replayable bodies, partial writes, ambiguous delivery,
  malformed/hostile `Retry-After`, redirects, pagination, and fan-out.
- Fault DNS, dial, TLS, connection reset, partial response, body corruption,
  slow headers/body, backend policy stores, and observer callbacks.
- Simulate multiple pods, connection-pool cold start, uneven endpoints, mixed
  revisions, scale changes, dependency recovery, SIGTERM, and abrupt kill.
- Prove every response body, timer, goroutine, permit, waiter, and observation
  is bounded and cleaned up.

Meaningful exact 100% statement coverage, exactly 100% viable mutation kills,
race, fuzz, stress, leak, network fault, benchmark, API compatibility, docs,
security, and supply-chain gates MUST pass. Final review MUST find no hidden
retry/hedge, body leak, timeout extension, local-rejection misclassification,
unbounded amplification, or misleading Kubernetes capacity claim.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
