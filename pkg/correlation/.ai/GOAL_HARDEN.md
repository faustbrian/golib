# Hardening Goal: Correlation And Causation

## Objective

Prove identifier trust, generation, propagation, privacy, precedence, and
cross-transport behavior under hostile input and concurrency.

## Required Audits

- Exhaust absent, empty, duplicate, conflicting, malformed, oversized,
  non-canonical, Unicode, and control-bearing carrier values.
- Verify trusted/untrusted proxy and transport boundaries.
- Trace correlation/request/causation through HTTP, JSON-RPC, queue redelivery,
  retry, scheduled work, and webhook chains.
- Prove deterministic strategies are domain-separated, versioned, bounded, and
  privacy-documented.
- Verify no correlation value influences authentication, authorization,
  idempotency, tenancy, or metrics cardinality.
- Race generators and context propagation; detect aliasing or global state.
- Fuzz carriers and codecs; mutation-test precedence, trust, generation, and
  overwrite decisions.

## Release Blockers

- Spoofing across a declared trust boundary, identifier confusion, duplicate
  generation under supported assumptions, secret/business-data disclosure,
  context collision, race, or unbounded carrier parsing.

## Completion Criteria

- Trust, privacy, propagation, retry, fuzz, race, mutation, and benchmark suites
  pass with meaningful 100% coverage.

