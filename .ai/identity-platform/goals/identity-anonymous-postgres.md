# Goal: pkg/identity/anonymous/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/anonymous/postgres`
- Canonical module: `pkg/identity/anonymous/postgres`
- Canonical goal after scaffolding: `pkg/identity/anonymous/postgres/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/anonymous/postgres:v1`; owned operation IDs: none
- Requires: `identity/anonymous`, `identity/postgres`, `identity/session`
- Consumes existing primitives: `postgres`, `migrations`, `outbox`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST read and satisfy
`.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin until the
coordinator has marked `identity/anonymous/postgres` `in-progress`, recorded
this worker, and verified both prerequisites. Build the PostgreSQL Store and
unit-of-work integration for anonymous-subject lifecycle and one-time upgrade.

## Ownership boundary

This adapter owns anonymous subject metadata, expiry, upgrade command identity,
merge plan/version, transfer journal, cleanup and reconciliation. Ordinary
user, account and identifier records remain authoritative in
`identity/postgres`; session bearer records remain outside this adapter. It
MUST implement only the public `identity/anonymous` persistence contracts and
compose with the public `identity/postgres` unit of work. It MUST NOT duplicate
identity rows, credential proof, session tokens or application resource data.

## Required persistence contract

- Anonymous records MUST bind opaque anonymous subject, tenant, identity ID,
  creation/expiry, state/version and cleanup policy. Placeholder identifiers
  MUST remain in the reserved anonymous namespace and MUST NOT become verified
  contact data.
- Issuance MUST atomically create the anonymous lifecycle record with the
  identity mutation and outbox evidence, or return a typed known rollback or
  unknown commit. No raw session or upgrade bearer value may be stored.
- Upgrade MUST enlist the public `identity/session` contributor so anonymous
  revocation and permanent-session rotation commit with the identity and merge
  transition. This adapter MUST persist only the anonymous side of that
  coordination and MUST NOT create, copy, or become authority for session
  bearer records.
- Upgrade MUST lock or compare the anonymous version, stable command ID and
  target identity so exactly one concurrent command can own the transition.
  Retries MUST return the prior attributable result or reconciliation state and
  MUST NOT repeat transferred effects.
- The merge journal MUST record a typed bounded plan and per-resource outcome
  references without copying arbitrary application payloads. Unsupported
  resource transfers MUST fail before the authoritative identity transition.
- Linking to an existing permanent identity MUST preserve that identity as
  authority, reject credential takeover, and atomically record consumed,
  conflicted or reconciliation-required status.
- Delete-on-link, retain, anonymize and legal-hold policies MUST have distinct
  durable states. Cleanup MUST NOT delete an identity or journal that a
  concurrent upgrade has committed or still owns.
- Expiry and abandoned cleanup MUST use database time, indexed bounded batches,
  optimistic/fenced ownership and observable lag. Cleanup MUST emit required
  outbox/audit evidence through the same transaction boundary.
- Cross-tenant subject, identity and command collisions MUST be rejected by
  constraints and typed without revealing another tenant's record.
- Reconciliation MUST classify missing identity/outbox linkage, unknown
  upgrade commit, partially applied merge references and interrupted cleanup.
  It MUST NOT infer success from the absence of an anonymous row.
- Migrations MUST preserve active anonymous sessions indirectly through stable
  identity references, active upgrade commands, merge journals and expiry
  ownership across populated data and old/new binaries.

Verification applicability is exact for this unit: `race=required`,
`fuzz=required`, `hostile=required`, `leak=required`, `benchmark=required`,
`infrastructure=required`, and `provider_interoperability=required`; a gate
MAY be satisfied by the required composed reference evidence but MUST NOT be
silently skipped.

## Required evidence

Real PostgreSQL tests MUST cover concurrent issuance and upgrade, same-target
retry, different-target conflict, credential collision, banned target, unknown
commit, delete-versus-upgrade, expiry-versus-upgrade, cross-tenant isolation,
outbox consistency, populated migration, backup/restore and production-shaped
cleanup plans. The composed reference journey MUST prove that session
transition and application merge callbacks use only public contracts and do
not require a second anonymous store.

Exact coverage and mutation, race/stress, query/lock and cleanup benchmarks,
clean-consumer, API/docs/examples/changelog, migration, security and
supply-chain gates are REQUIRED. The unit MUST remain unverified if two
upgrades can win, anonymous state can seize permanent credentials, cleanup can
erase an in-flight winner, raw bearer material is durable, or identity state is
duplicated.
