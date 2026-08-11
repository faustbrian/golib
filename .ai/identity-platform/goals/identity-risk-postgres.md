# Goal: pkg/identity/risk/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/risk/postgres`
- Canonical module: `pkg/identity/risk/postgres`
- Canonical goal after scaffolding: `pkg/identity/risk/postgres/.ai/GOAL.md`
- Requires: `identity/risk`, `identity/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/risk/postgres` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/risk/postgres` module that owns durable risk counters, decisions, evidence retention, and investigation queries. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module is the sole mutation authority for durable lockout state, durable
risk counters, the decision/evidence journal, retention, and investigation
queries. It does not own ephemeral attempt, velocity, concurrency or challenge
windows, risk policy, or external signals. Those exclusions MUST remain
outside its public API and dependency graph.

The adapter owns the durable one-use RiskEvidence journal and its `issued`,
`reserved`, `finalized`, `released`, `expired`, and `revoked` transitions. The
`identity/risk` service owns decision policy and bearer construction; no caller,
workflow, cache, or other adapter may create, reserve, finalize, release, or
recover the authoritative journal row.

## Required public contract

The design MUST define schema, durable risk counters, decision journal,
CaptchaEvidence and CaptchaEvidenceContributor persistence contracts,
retention, indexed query, anonymization, and migration contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

This adapter owns the durable CAPTCHA evidence participant and implements
`tx.captcha.issue`, `tx.captcha.reserve`, `tx.captcha.apply`,
`tx.captcha.finalize`, and `tx.captcha.reconcile`. Ambiguous insertion MUST
reconcile the same command and fingerprint on the primary and MUST NOT create a
second evidence row or reference.
The adapter MUST enforce one durable replay-fingerprint winner across all
issuance commands using the exact tenant/provider/site/profile/configuration
scope. It MUST retain the keyed fingerprint, key version and terminal tombstone
through the configured replay-retention boundary; unresolved issuance has no
time-based release.

The implementation and tests MUST increment concurrently without lost updates; enforce windows by database time; retain minimal evidence; clean expired data; recover ambiguous writes; prove query plans at scale. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Schema/key dimensions MUST be derived from the core's bounded canonical
  action/subject/signal identities and versioned keyed scoped digests. Each
  digest MUST domain-separate canonical tenant, operation, dimension kind and
  dimension value. Unkeyed hashes and cross-tenant digest reuse MUST be
  rejected. Raw identifiers, network addresses, passwords, tokens, OTPs and
  unbounded request strings MUST never become columns, keys or labels.
- Atomic operations MUST support durable risk counters, lockout state,
  evidence/decision journal and compare-and-set overrides exactly for the
  profiles declared by core. Ephemeral fixed/sliding velocity windows remain
  owned by `identity/risk/valkey`; unsupported durable algorithms MUST fail
  setup.
- Database time and isolation semantics MUST define every inclusive/exclusive
  boundary. Concurrent increments/decisions MUST NOT undercount or extend a
  lockout incorrectly.
- IPv4/IPv6 dimensions MUST be canonicalized before digesting. The selected
  reference profile uses the full canonical RFC 5952 IPv6 address without
  subnet aggregation; configured IPv6 subnet dimensions MAY exist only in a
  future, separately selected profile. Key rotation/retention MUST NOT create
  bypass windows.
- Provider-signal evidence MUST retain safe attributable status including
  unavailable/ambiguous without storing challenge tokens or full HIBP data.
- RiskEvidence issuance MUST atomically persist the exact-bound `issued` row
  before exposing its opaque reference. Reservation MUST lock the keyed digest,
  bind one command ID/fingerprint/generation, and give exactly one winner when
  two commands race; validation or signature success without reservation grants
  no recovery authority. Reservation MUST run only through this adapter's
  predeclared contributor in the coordinator's single reservation transaction
  with the exact participants declared by the operation profile: initiation
  reserves only its command and RiskEvidence, while completion also reserves
  the existing purpose-bound OTP and reset capability. A separate/private
  RiskEvidence reservation transaction is forbidden.
- Issue MUST enlist in the identity command unit of work and atomically persist
  the exact `issued` row and committed command result before any opaque
  reference is returned. A proved pre-commit failure MUST leave no issued row;
  an ambiguous commit MUST return no reference and reconcile the same command
  from primary authority. Matching committed replay MUST return the recorded
  opaque reference without a second row or provider evaluation. Adapter results
  MUST NOT expose raw facts, provider evidence, embedded evidence payloads, keyed digests,
  signatures, journal identifiers, or persistence records.
- The two phone-reset purposes MUST use distinct journal rows and keyed digests;
  one purpose MUST NOT validate, reserve, replay, or substitute for the other.
  Initiation reservation/apply/finalize MUST share the authoritative coordinator
  unit of work whose domain commit issues the OTP challenge, reset capability,
  outbox/audit records, and command result. Completion MUST use a separately
  issued `phone-password-reset-complete` row.
- Expired command-owner takeover MUST CAS the RiskEvidence reservation from the
  exact prior generation to the new generation in the same coordinator
  reservation transaction that transfers every other participant declared by
  the operation profile. Same-command and
  same-fingerprint matching is REQUIRED; stale, partial, missing, terminal, or
  mismatched participant generations MUST fail closed without apply/finalize
  authority.
- Apply MUST recheck the reservation, expiry, exact phone-recovery binding,
  policy/provider versions, decision, and authoritative counters inside the
  coordinator transaction. Initiation finalize MUST commit with issuance of the
  purpose-bound OTP challenge, reset capability, outbox/audit, and command
  result. Completion finalize MUST commit with the existing purpose-bound OTP,
  reset capability, password mutation, session invalidation, outbox/audit, and
  command result. This adapter MUST use the shared transaction carrier and MUST
  NOT open a private transaction for those steps.
- Unknown completion MUST retain `reserved` and reconcile the owning command
  before finalizing or releasing; expiry, lease loss, cleanup, or another
  command MUST NOT make the evidence reusable. A retryable rollback may reuse
  the reservation only under the same live command ownership. Release requires
  proof of non-commit, is terminal, and forces newly issued evidence.
- Cleanup/anonymization MUST use bounded indexed batches and preserve minimum
  evidence for active windows, incident audit and unknown-outcome reconciliation.
  It MUST expire untouched issued rows by database time, retain unresolved
  reservations, and wait through the later of original evidence expiry and
  `command.result_retention` before payload/linkage crypto-shredding. The
  restricted keyed terminal tombstone has no time-based deletion and MAY be
  removed only after all referenced evidence-verification and keyed-digest key
  versions are retired and proof shows every bearer fails cryptographic
  validation before lookup.
- Real PostgreSQL tests MUST cover contention/hot keys, isolation levels,
  deadlock/serialization, disconnect/commit ambiguity, clock boundaries,
  partition/retention and production-shaped query plans.
- Migrations and restore MUST preserve active windows/lockouts or document a
  deliberate security-safe reset; silent allowance reset is forbidden.
- The adapter MUST reject configuration that assigns durable lockout mutation
  authority to another store. Valkey loss or reset MUST NOT clear, shorten or
  supersede a PostgreSQL lockout, and digest-key rotation MUST preserve active
  lockouts and their investigation linkage.
- Each mutation MUST bind the trusted operation, tenant, action, purpose,
  subject dimensions, policy version and idempotency identifier. A
  `not-committed` result MAY be retried within that binding, a `committed`
  result MUST return the post-mutation snapshot, and an `unknown` result MUST
  map to core unavailable without an unproved retry.
- Lockout, override, reconciliation, expiry and anonymization events MUST use
  `.ai/identity-platform/SECURITY_EVENTS.md`; retention, erasure, tenant
  deletion, restore and key rotation MUST follow
  `.ai/identity-platform/LIFECYCLE_CASCADES.md`.

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
