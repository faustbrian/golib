# Goal: pkg/identity/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/postgres`
- Canonical module: `pkg/identity/postgres`
- Canonical goal after scaffolding: `pkg/identity/postgres/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/postgres:v1`; owned operation IDs: none
- Requires: `identity`
- Consumes existing primitives: `postgres`, `migrations`, `outbox`, `audit`
- Unlocks after verification: `identity/session/postgres`, `identity/delivery/postgres`, `identity/risk/postgres`, `identity/password/postgres`, `identity/otp/postgres`, `identity/anonymous/postgres`, `identity/mfa/postgres`, `webauthn/postgres`, `passkey/postgres`, `identity/oauth/postgres`, `identity/apikey/postgres`, `identity/impersonation/postgres`, `organization/postgres`, `sso/postgres`, `scim/postgres`, `oauth-server/postgres`, `identity/reference`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `identity/postgres` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/identity/postgres` module that owns durable PostgreSQL implementation of identity repositories and transactions. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns durable PostgreSQL implementation of identity repositories and transactions. It does not own identity business policy, sessions, and protocol handling. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define schema, migrations, repository, transaction, outbox, pagination, locking, cleanup, and reconciliation contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST enforce canonical-identifier uniqueness; atomically mutate identity plus outbox; recover ambiguous commits; paginate stable snapshots; run concurrent linking and deletion; upgrade with mixed binaries. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- The schema MUST persist users, accounts, identifiers, credentials references,
  verifications, status transitions, typed additional fields and aggregate
  versions without storing recoverable credential secrets.
- Database constraints MUST enforce tenant-scoped canonical identifier and
  provider-subject uniqueness, one valid primary identifier per kind, account
  link integrity and optimistic version checks under concurrent writers.
- Attribute storage MUST validate declared schema versions, preserve supported
  scalar/list types without lossy conversion, prevent undeclared-field writes,
  support indexed fields only by explicit migration, and redact sensitive
  values from diagnostics and evidence.
- Only compile-time typed, bounded contributors declared before their first
  write MAY enlist in the unit of work. Before hooks MUST run before the domain
  transaction opens. After hooks MUST observe the attempted/committed outcome
  only after terminalization has been attempted and the transaction has closed;
  they MUST NOT run while any transaction remains open. Caller-supplied hook
  callbacks have no unit-of-work role and MUST NOT execute under an open
  transaction. Only the typed compile-time contributors defined by
  `.ai/identity-platform/TRANSACTION_CONTRACT.md` may enlist. After hooks and
  outbox consumers MUST NOT be presented as atomic with the transaction.
- List/search plans MUST remain index-backed for the documented filters and
  cursor ordering at production-shaped cardinality. Arbitrary attribute search
  is unsupported unless an explicit indexed field contract exists.
- Link/unlink, primary replacement, suspension expiry, anonymization and delete
  MUST have race tests with database-enforced outcomes and outbox consistency.
- Migrations MUST cover empty, populated and mixed-schema attribute data,
  interrupted backfills, constraint validation, rollback boundary and old/new
  binary coexistence. Unsupported destructive downgrades MUST be explicit.
- Reconciliation MUST classify missing outbox events, unknown commits,
  orphaned references and partially migrated attributes without fabricating a
  successful identity result.
- Its transaction implementation MUST participate in the coordinator protocol
  in `.ai/identity-platform/TRANSACTION_CONTRACT.md`, including stable command
  identity, prepare/reserve/finalize semantics where required, rollback versus
  unknown classification, durable recovery records and idempotent replay. A
  local SQL transaction MUST NOT be described as atomic with another module's
  database transaction merely because both use PostgreSQL.
- Privacy export and destructive lifecycle storage operations MUST implement
  the checkpoints, tombstones, retention/legal-hold exceptions and verified-
  deletion evidence defined in `.ai/identity-platform/LIFECYCLE_CASCADES.md`.
  The public `identity` unit owns global-compromise semantics and its lifecycle
  acknowledgement; this adapter MUST persist the global authority version,
  cascade generation, and owner checkpoint in the same authoritative
  transaction. It MUST NOT claim a second semantic acknowledgement.
  Provider-link rows, account-link metadata, provider-subject uniqueness, and
  the `lifecycle.dimension.social_link` authority version MUST remain owned here
  and MUST NOT be duplicated by provider adapters. An enlisted provider adapter
  MAY atomically contribute token-vault metadata, but the link row, uniqueness
  decision and social-link version bump remain this participant's mutation.

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
