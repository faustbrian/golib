# Goal: pkg/identity/risk/valkey

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/risk/valkey`
- Canonical module: `pkg/identity/risk/valkey`
- Canonical goal after scaffolding: `pkg/identity/risk/valkey/.ai/GOAL.md`
- Requires: `identity/risk`
- Consumes existing primitives: `rate-limit`, `cache`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/risk/valkey` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/risk/valkey` module that owns ephemeral high-volume abuse counters and challenge state in Valkey. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module is the sole mutation authority for short-lived attempt, velocity,
concurrency and one-time challenge windows in Valkey. It does not own durable
lockout state, durable investigation history, the evidence/decision journal or
risk policy. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define namespaced keys, atomic windows, TTL, cluster behavior, bounded values, failover classification, and flush recovery contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST prove exact window boundaries; prevent tenant collisions; handle eviction and restart; bound hot-key amplification; test supported Valkey topology and versions. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Namespaces and keys MUST use bounded action/tenant/scoped subject digests and
  explicit hash tags. Each digest MUST be versioned, keyed and domain-separated
  over canonical tenant, trusted operation, dimension kind and dimension value;
  unkeyed hashes and cross-tenant reuse MUST be rejected. Raw identifiers, IP
  addresses, tokens or provider responses MUST NOT appear in keys or diagnostic
  values.
- Atomic scripts/transactions MUST implement only declared window, velocity,
  attempt, concurrency and one-time challenge counters with exact TTL ownership
  and server-time semantics. They MUST NOT create, extend, clear or represent a
  durable lockout.
- The selected reference profile MUST use the full canonical RFC 5952 IPv6
  address without subnet aggregation. Aggregated IPv6 keys MAY exist only in a
  future, separately selected profile. Canonicalization and key-version
  rotation MUST NOT permit alternate-representation or version-change bypass.
- Standalone, replicated and cluster profiles MUST declare which multi-key
  decisions are atomic. Cross-slot configurations MUST fail construction rather
  than degrade to multiple non-atomic commands.
- Eviction, flush, failover, replication lag, script-cache loss, MOVED/ASK and
  partial pipeline outcomes MUST map to core unavailable/unknown signals under
  the operation-specific matrix in
  `.ai/identity-platform/REFERENCE_CONFIGURATION.md`. Loss or reset MUST fail
  closed for credential- or session-issuing operations and MUST NOT erase or
  supersede PostgreSQL durable lockout state.
- Hot-key amplification, key cardinality and value size MUST be bounded before
  command execution; cleanup/expiry MUST NOT require unbounded scans.
- Real supported Valkey topology tests MUST cover exact boundaries, contention,
  restart/failover/eviction, cluster routing and recovery. Protocol fakes are
  deterministic unit evidence only.
- Each mutation MUST bind the trusted operation, tenant, action, purpose,
  subject dimensions, policy version and replay/idempotency identifier. A
  `not-committed` result MAY be retried within that binding; a `committed`
  result MUST consume the returned post-mutation state; and an `unknown` result
  MUST be unavailable and MUST NOT be retried without proven idempotency.
- Window exhaustion, replay, failover and reset events MUST use
  `.ai/identity-platform/SECURITY_EVENTS.md`; expiry, tenant deletion and digest
  rotation MUST follow `.ai/identity-platform/LIFECYCLE_CASCADES.md`.

## Security and abuse requirements

- Inputs MUST be bounded before parsing, allocation, storage, hashing, or
  cryptographic work.
- Subject, tenant, organization, purpose, audience, action, and redirect scope
  MUST be bound wherever applicable and MUST fail closed on mismatch.
- Enumeration, replay, fixation, confused-deputy, downgrade, race, and
  cross-scope attacks MUST have deterministic regression cases.
- Logs, traces, metrics, examples, fixtures, and errors MUST preserve the
  redaction requirements in `.ai/identity-platform/COMMON_REQUIREMENTS.md`.

## Persistence, lifecycle, and compatibility

The core MUST remain adapter-neutral unless this goal is itself an adapter.
State ownership, consistency, retention, deletion, migration, key rotation,
clock skew, concurrent callers, shutdown, and recovery MUST be documented and
tested where applicable. Unsupported protocol or deployment profiles MUST be
stated rather than silently approximated.

## Acceptance evidence

Before this unit becomes `verified`, the owner MUST satisfy every common gate,
the package-specific behavior above, the module's exact coverage and mutation
gates, race/fuzz/interoperability gates that apply, clean-consumer proof,
manifests, public API baseline, security and supply-chain checks, documentation,
changelog, and changed reverse-dependant gates. The final evidence record MUST
name any non-applicable gate with a reviewed reason; absence of infrastructure
or provider access is a blocker, not a pass.

## Release blockers

The unit MUST remain `implemented-unverified` or `blocked` if any prerequisite
is not `verified`, any ownership boundary is unresolved, a protocol claim
lacks pinned specification and interoperability evidence, a durable transition
has unhandled ambiguity, a secret can escape redaction, or any required gate is
stale, skipped, warning-only, or failing.
