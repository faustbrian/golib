# Goal: pkg/oauth-server/postgres

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Execution metadata

- Unit: `oauth-server/postgres`
- Canonical module: `pkg/oauth-server/postgres`
- Canonical goal after scaffolding: `pkg/oauth-server/postgres/.ai/GOAL.md`
- Requires: `oauth-server`, `identity/postgres`, `identity/session/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `secret-envelope`, `outbox`, `audit`
- Unlocks after verification: `identity/http`

## Start gate

The worker MUST read and satisfy `../COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `oauth-server/postgres` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/oauth-server/postgres` module that owns durable OAuth clients, consents, codes, grants, refresh families, revocations, keys, and authorization transactions. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns durable OAuth clients, consents, codes, grants, refresh families, revocations, keys, and authorization transactions. It does not own endpoint policy, token cryptography, OIDC claims, and device-flow semantics. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define schema, client secret storage, redirect sets, single-use code transaction, refresh-family locking, consent versions, key metadata, revocation indexes, migrations, cleanup, and reconciliation contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST race code redemption and refresh rotation; revoke replayed families; hash bearer-equivalent values; rotate client secrets and keys; recover ambiguous commits without duplicate issuance; clean expiry safely; preserve old/new binary compatibility and query plans. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Security and abuse requirements

- Inputs MUST be bounded before parsing, allocation, storage, hashing, or
  cryptographic work.
- Subject, tenant, organization, purpose, audience, action, and redirect scope
  MUST be bound wherever applicable and MUST fail closed on mismatch.
- Enumeration, replay, fixation, confused-deputy, downgrade, race, and
  cross-scope attacks MUST have deterministic regression cases.
- Logs, traces, metrics, examples, fixtures, and errors MUST preserve the
  redaction requirements in `../COMMON_REQUIREMENTS.md`.

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
