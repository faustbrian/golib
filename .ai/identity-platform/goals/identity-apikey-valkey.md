# Goal: pkg/identity/apikey/valkey

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/apikey/valkey`
- Canonical module: `pkg/identity/apikey/valkey`
- Canonical goal after scaffolding: `pkg/identity/apikey/valkey/.ai/GOAL.md`
- Requires: `identity/apikey`, `identity/apikey/postgres`
- Consumes existing primitives: `audit`, `telemetry`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed assignment with all listed prerequisites verified. Build the
Valkey API-key store for declared authoritative and secondary-storage/cache
profiles, including atomic quota/refill and safe PostgreSQL fallback behavior.

## Ownership and public contract

The adapter owns namespaced key metadata/digest records, owner/configuration/
permission/expiry encoding, quota/refill/usage atomicity, TTL/index ownership,
authoritative versus secondary mode, cache invalidation/fallback classification,
cluster policy, cleanup and reconciliation. It never owns raw API-key creation,
authorization policy or PostgreSQL data.

## Required behavior and evidence

- Raw keys MUST never appear in Valkey keys or values. Lookup keys MUST use a
  scoped digest/prefix/configuration design with bounded collision candidates.
- Authoritative mode MUST support the complete management/verification
  interface or reject unsupported operations at construction; it MUST define
  durability, persistence, replication and eviction requirements explicitly.
- Secondary mode MUST declare PostgreSQL/source authority, write-through or
  cache-aside behavior, maximum staleness, negative caching and invalidation on
  update/rotate/revoke/expiry. Fallback MUST never resurrect revoked material
  or broaden permissions.
- Verification, remaining/refill, rate limits and optional usage updates MUST
  be one atomic script/transaction within the supported topology and use
  server time with exact boundaries.
- Namespace/hash-tag policy MUST prevent tenant/configuration collisions and
  fail unsupported cross-slot operations at construction.
- Eviction, flush, failover, replication lag, MOVED/ASK, script-cache loss and
  partial pipeline outcomes MUST remain unavailable/unknown or trigger the
  exact safe authoritative fallback; they MUST not become successful verify.
- Indexes/list/delete-expired MUST be bounded and cleaned with primary records;
  reconciliation MUST remove stale entries without copying raw secrets.
- Real Valkey standalone and declared cluster/failover tests plus integrated
  PostgreSQL fallback tests MUST cover rotation/revocation races, quotas,
  staleness, outage/recovery, hot keys and cleanup.

Exact coverage/mutation, race, script/fuzz where applicable, hot-key/resource
benchmarks, clean-consumer, API/docs/changelog and supply-chain gates are
REQUIRED. Raw-key retention, stale revocation acceptance, non-atomic quota or
fake-only interoperability blocks verification.
