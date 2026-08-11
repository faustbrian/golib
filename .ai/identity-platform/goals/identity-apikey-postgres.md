# Goal: pkg/identity/apikey/postgres

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/apikey/postgres`
- Canonical module: `pkg/identity/apikey/postgres`
- Canonical goal after scaffolding: `pkg/identity/apikey/postgres/.ai/GOAL.md`
- Requires: `identity/apikey`, `identity/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `outbox`, `audit`
- Unlocks after verification: `identity/apikey/valkey`, `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed assignment with both prerequisites verified. Build the
PostgreSQL API-key record, digest lookup, permissions, metadata, quota/refill,
rotation and revocation adapter.

## Ownership and public contract

The adapter owns key metadata/schema, secret digest/prefix, configuration ID,
user/organization ownership, permissions, expiry, rotation lineage,
rate/quota/refill state, usage timestamps/counters, migrations, cleanup and
reconciliation. It never generates or returns raw keys after the core's
reveal-once result and does not decide authorization policy.

## Required behavior and evidence

- Digest and prefix collisions MUST be handled safely; authentication MUST
  select candidates by bounded prefix/configuration and verify the complete
  digest without timing-dependent early acceptance.
- Create/rotate MUST atomically persist only digest metadata and return a known
  commit result before the core reveals success. Unknown commits MUST not cause
  a second raw secret to represent the same intended rotation silently.
- Get/list/update/delete/delete-expired MUST enforce owner/tenant scope, stable
  pagination and optimistic versioning. Ownership changes are forbidden.
- Permission and typed metadata updates MUST validate schemas and preserve
  explicit empty versus inherit semantics without privilege broadening.
- Quota/remaining/refill and optional rate limits MUST update atomically by
  database time under concurrent verification. Usage observation MUST be
  write-amplification bounded and MUST not turn authentication success unknown.
- Revocation/expiry/rotation MUST win against concurrent verification according
  to a documented isolation point and propagate to any API-key session.
- Cleanup MUST use bounded indexed batches and retain required rotation/audit/
  unknown-outcome evidence.
- Real PostgreSQL races, disconnects, migrations from older key formats,
  mixed binaries, backup/restore and query plans are REQUIRED.

Exact coverage/mutation, race, query/lock benchmarks, clean-consumer,
API/docs/changelog and supply-chain gates are REQUIRED. Raw-key persistence,
quota races, fallback resurrection or cross-owner access blocks verification.
