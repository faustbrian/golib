# Goal: pkg/identity/impersonation/postgres

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/impersonation/postgres`
- Canonical module: `pkg/identity/impersonation/postgres`
- Canonical goal after scaffolding: `pkg/identity/impersonation/postgres/.ai/GOAL.md`
- Requires: `identity/impersonation`, `identity/postgres`, `identity/session/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `outbox`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed assignment with all listed prerequisites verified.
Build the durable PostgreSQL implementation for impersonation grants, approval
references, actor lineage, revocation and administrator queries.

## Ownership and public contract

This adapter owns grant/reason/approval metadata, actor-target lineage,
versioned status, expiry/revocation indexes, session references, migrations,
cleanup and reconciliation. It MUST implement only public impersonation,
identity and session contracts. It MUST NOT authorize impersonation, issue
ordinary sessions, duplicate session bearer data or treat audit rows as the
authoritative grant store.

## Required behavior and evidence

- Grant creation MUST atomically persist actor, target, tenant, reason,
  approval reference, bounded scope, issue/expiry time and policy version with
  the required outbox/audit linkage.
- Reasons and approval references MUST be length-bounded and access-controlled;
  administrator list/search MUST be tenant-scoped, stable-paginated and
  index-backed without making reason text an unrestricted search oracle.
- Start, stop, expire, actor disable, target disable, explicit revoke and
  global session revoke MUST have versioned single-winner transitions.
- Session references MUST preserve immutable actor/target/grant lineage and
  allow revocation propagation without storing raw session tokens.
- Nested grants MUST be rejected by the domain contract and protected by
  persisted lineage checks under concurrent starts.
- Unknown commit outcomes MUST be reconciled by stable request/grant identity;
  retry MUST NOT create a second grant or a stronger session.
- Cleanup MUST retain the minimum audit/legal-hold linkage while removing or
  anonymizing expired sensitive reason data according to declared policy.
- Migrations MUST cover empty and populated stores, mixed binaries,
  interruption/resume, backup/restore and rollback boundaries.
- Real PostgreSQL races MUST prove grant uniqueness, revoke-versus-use,
  expiry, actor/target disable, list isolation and ambiguous disconnect cases.
- Query and lock benchmarks, exact coverage/mutation, race, clean-consumer,
  API/docs/changelog, vulnerability/secret/license/SBOM and provenance gates
  are REQUIRED.

The unit MUST remain unverified if raw session credentials are stored, actor
lineage can be rewritten, a revoked grant can authorize, reasons leak across
scope, or an unknown commit can duplicate authority.
