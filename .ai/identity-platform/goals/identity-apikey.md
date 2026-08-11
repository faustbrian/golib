# Goal: pkg/identity/apikey

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Execution metadata

- Unit: `identity/apikey`
- Canonical module: `pkg/identity/apikey`
- Canonical goal after scaffolding: `pkg/identity/apikey/.ai/GOAL.md`
- Requires: `identity`
- Consumes existing primitives: `authentication`, `authorization`, `secret-envelope`, `audit`, `rate-limit`
- Unlocks after verification: `identity/apikey/postgres`, `identity/apikey/valkey`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/apikey` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/apikey` module that owns user-owned API-key issuance, display-once secrets, digested lookup, naming, scopes, expiry, rotation, revocation, and usage metadata. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns user-owned API-key issuance, display-once secrets, digested lookup, naming, scopes, expiry, rotation, revocation, and usage metadata. It does not own generic API-key request validation, OAuth clients, service-to-service PKI, and UI. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define Service, KeyID, SecretGenerator, Record, ScopePolicy, Store, IssuanceResult, AuthenticatorAdapter, Rotation, Revocation, and UsageObserver contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST issue unbiased secrets once; store only digest and prefix metadata; authenticate in constant-work shape; scope principals; race rotate and revoke; expire by authoritative clock; list without secrets; update last-used with bounded write amplification; prevent prefix collisions and tenant leakage. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Operations MUST include create, verify, get metadata, list with stable
  pagination, update name/metadata/permissions, rotate, revoke/delete, and
  bounded deletion of expired keys. Secrets MUST be returned only at creation
  or rotation.
- Keys MUST support user or organization ownership, tenant binding, named
  configurations, permissions/scopes and explicit authorization for every
  management operation. Ownership changes are forbidden; rotation creates a
  traceable successor.
- Multiple configuration profiles MUST define prefix, length, entropy,
  expiration, storage, rate/quota and verification policies. The configuration
  ID MUST be stored and used for verification rather than inferred from
  attacker-controlled key text.
- Database, secondary-storage with explicit fallback, and custom store
  profiles MUST declare consistency, revocation propagation and unknown-outcome
  behavior. Fallback MUST not resurrect revoked or expired keys.
- Fixed-window or token-bucket limits, remaining allowance, refill interval,
  refill amount and expiry MUST have atomic semantics under concurrency.
  Verification MUST return bounded safe quota metadata where configured.
- Custom generators/verifiers MUST meet minimum entropy, redaction and
  constant-work contracts; unsafe configurations MUST fail construction.
- Metadata MUST be schema-validated, size/depth bounded, authorization-filtered
  on read/write and excluded from unbounded telemetry labels.
- API-key-derived sessions, when enabled, MUST preserve the key's owner,
  permissions, expiry and revocation and MUST never outlive or broaden the key.

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
