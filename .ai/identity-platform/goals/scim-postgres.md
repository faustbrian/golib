# Goal: pkg/scim/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `scim/postgres`
- Canonical module: `pkg/scim/postgres`
- Canonical goal after scaffolding: `pkg/scim/postgres/.ai/GOAL.md`
- Requires: `scim`, `scim/organization`, `identity/postgres`, `organization/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `outbox`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `scim/postgres` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/scim/postgres` module that owns durable SCIM resources, external IDs, versions, idempotency, filters, patches, bulk transactions, and change journal. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns durable SCIM protocol projections, external IDs, versions,
idempotency, filters, patches, bulk transactions, and change journal. It does
not own authoritative identity users, organization memberships/teams/roles,
SCIM parsing, organization mapping policy, and HTTP authentication. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define schema, resource projection, uniqueness, version/ETag, filter planning, patch transaction, bulk boundaries, tombstones, pagination, migrations, and reconciliation contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST enforce external-ID uniqueness by tenant,
organization, provider connection, resource type and schema-exact `caseExact`
comparison; compare versions atomically; translate safe indexed filters and
bounded fallbacks; apply patches and outbox atomically; replay idempotently;
preserve tombstones and retention; and verify plans and mixed-binary migrations. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- The schema MUST persist provider connections/ownership, digested bearer
  tokens, mapping versions, bounded SCIM Users/Groups protocol projections,
  external IDs, ETags, tombstones, reconciliation cursors and idempotency keys
  under tenant and organization foreign keys. A projection MUST NOT become a
  second authoritative identity, membership, team or role record.
- Token creation/rotation MUST reveal once, store only lookup digest/prefix and
  atomically revoke prior material according to overlap policy. Unknown commits
  MUST NOT cause the same secret to be reissued.
- `externalId` uniqueness and lookup MUST use the exact key `(tenant_id,
  organization_id, provider_connection_id, resource_type, external_id)` and
  the resource schema's exact `caseExact` rule. Indexes and comparisons MUST
  use protocol-defined bytes/folding explicitly; database collation and generic
  identifier canonicalization MUST NOT alter or widen that tuple. Other schema
  uniqueness constraints MUST likewise honor their declared scope and
  `caseExact` semantics.
- Admission and projection writes MUST reject exact duplicate JSON members,
  SCIM case-insensitive attribute-name collisions, and set-valued duplicates
  under the schema's canonical comparison. Constraints or canonical projection
  keys MUST make concurrent case-variant inserts single-winner; database
  collation MUST NOT define SCIM attribute identity.
- The filter planner MUST have an explicit supported AST-to-index mapping,
  parameterize all values, use indexed plans or bounded fallback for every
  schema-valid advertised filter/sort, reject only invalid grammar/path, and
  honor schema type, `caseExact` and multi-valued primary/value selection.
  Budget exhaustion MUST return a stable SCIM error without partial results or
  an incorrect count. The adapter MUST publish production-shaped query-plan
  budgets. List queries MUST provide exact
  `totalResults`, 1-based offset/count behavior, below-one `startIndex` as 1,
  negative `count` as zero, actual returned `itemsPerPage`, one transaction
  snapshot and a stable server-ID sort tie-break. Absent `sortBy` MUST use
  server ID ascending and absent `sortOrder` MUST mean ascending. Queries MUST
  enforce `scim.page_default`,
  `scim.page_max`, `scim.group_members` and admitted resource bounds from the
  effective manifest.
- Replace/PATCH/delete and projection/outbox/version updates MUST be one
  transaction. The adapter MUST compose `scim/organization` mapping with the
  public `identity/postgres` and `organization/postgres` units of work so a
  mapped local transition and its SCIM version/outbox are atomic where the
  selected PostgreSQL profile supports it. Any cross-owner or external outcome
  that cannot share that transaction MUST enter explicit reconciliation rather
  than rewriting local success.
- Bulk MUST implement the SCIM contract in
  `.ai/identity-platform/TRANSACTION_CONTRACT.md`: atomically admit the scoped
  idempotency mapping, parent and ordered independently random child commands;
  persist each child's bulk ID, dependencies, fingerprint, order and result;
  commit executing children independently; and deterministically rebuild parent
  state for every declared child and responses for every processed child from
  durable ordered checkpoints after partial commit or restart. A positive
  `failOnErrors` value durably marks every
  remaining not-started child skipped only after that many child results are
  durably failed; zero or omission disables the cutoff. Unknown dependencies
  remain blocked for reconciliation, and savepoints MUST NOT be represented as
  durable checkpoints. The terminal wire response MUST replay only processed
  `succeeded`/`failed` children in request order and omit every durable
  `skipped-fail-on-errors` child as unprocessed; skipped children retain no wire
  status/location/version/Error body.
- The adapter MUST implement the HTTP/SCIM idempotency mapping and command
  ledger as one authority with each mutation. Matching key/fingerprint retries
  recover the same command/result, mismatches conflict without mutation, and
  pending or unknown mappings remain reserved. DELETE tombstones and original
  results MUST support same-key/fingerprint replay according to
  `scim.delete_tombstone_retention` and `scim.idempotency_retention`; different
  keys receive normal precondition/not-found outcomes. Cleanup MUST remove only
  bounded expired evidence and MUST NOT time-release unresolved mappings.
- Migration and restore evidence MUST include active connections/tokens,
  custom mappings, large groups, mixed binaries and resumption of a partially
  completed reconciliation.

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
