# Goal: pkg/identity/apikey/valkey

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/apikey/valkey`
- Canonical module: `pkg/identity/apikey/valkey`
- Canonical goal after scaffolding: `pkg/identity/apikey/valkey/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:identity/apikey/valkey:v1`; owned operation IDs: none
- Requires: `identity/apikey`
- Consumes existing primitives: `audit`, `telemetry`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed assignment with the listed prerequisite verified. Build
the Valkey positive metadata and quota-result projection over one injected
API-key authority. Valkey is never an API-key, authorization or debit authority.

## Ownership and public contract

The adapter owns namespaced positive metadata and known quota-result
projections, TTL/index ownership, invalidation classification, cluster policy,
cleanup and projection reconciliation. It never owns raw API-key creation,
credential/digest authority, management state, authorization policy, a quota
debit, or PostgreSQL data. Every usable constructor MUST receive a non-nil
public `identity/apikey.Store` and `identity/apikey.AtomicVerifier` source (which
MAY be one object); nil source, authoritative mode and source-free degraded mode
MUST be rejected at construction.

## Required behavior and evidence

- Raw keys, presented digests and authoritative candidate digest records MUST
  never appear in Valkey keys or values. Projection keys MUST use tenant,
  configuration, key ID, authority-version and namespace bindings with bounded
  collision handling; values MUST contain only redacted positive metadata and
  known quota results already returned by the source.
- Cache-aside/write-through policy MUST define a maximum staleness of at most
  60 seconds and invalidation on update, rotate, revoke, expiry, owner/user/
  organization/authorization change and configuration revision. Miss, stale or
  unknown version, invalidation loss, epoch loss and negative lookup MUST call
  the injected source or fail closed; absence MUST NOT be cached as an
  authorization decision. A positive projection MAY only narrow a current
  source result and MUST never authorize, resurrect material or broaden
  permissions.
- The adapter MUST depend only on the public `identity/apikey.Store` and
  `identity/apikey.AtomicVerifier` contracts. It MUST NOT import
  `identity/apikey/postgres` or assume a PostgreSQL implementation;
  `identity/reference` owns the selected PostgreSQL-plus-Valkey composition and
  cross-adapter evidence.
- `VerifyAndConsume` MUST invoke the injected `AtomicVerifier` exactly once for
  the supplied verification attempt ID. Valkey MAY publish the source's known
  `not-applicable`/`debited`/`not-debited` quota result as a short-lived
  projection after that call, but MUST NOT perform refill, enforcement or debit
  and MUST NOT use the projection to skip the source on a later verification.
  Matching replay and `ReconcileVerifyAndConsume` MUST delegate with the same
  attempt ID and fingerprint/query. A source `possible-debit` outcome MUST pass
  through unchanged, publish no positive/quota projection and forbid fallback
  or a new attempt; Valkey availability errors MUST NOT obscure that state.
- Namespace/hash-tag policy MUST prevent tenant/configuration collisions and
  fail unsupported cross-slot operations at construction.
- Eviction, flush, failover, replication lag, MOVED/ASK, script-cache loss and
  partial pipeline outcomes MUST discard or mark the projection unavailable and
  use the injected source; they MUST NOT become successful verification. A
  failure while publishing a projection after a known source result MUST NOT
  change or hide that source result, and retry MUST not call the source again
  except as a matching attempt-ID replay.
- Projection indexes and list pages MUST be bounded and expire with their
  projected values. Primary delete-expired remains a source operation; after a
  known source result this adapter only invalidates affected projections.
  Reconciliation MUST remove stale projection entries without copying raw
  secrets or claiming primary-record cleanup.
- Real Valkey standalone and declared cluster/failover tests plus an independent
  source-authority contract implementation MUST cover rotation/revocation
  races, quota-result projection, one-source-call and possible-debit ambiguity,
  staleness, outage/recovery, hot keys and cleanup. These
  package-local tests and real Valkey profiles are the complete interoperability
  evidence required to verify this unit. PostgreSQL-plus-Valkey reference
  composition is owned by `identity/reference` after this unit is verified and
  MUST NOT be a prerequisite or evidence dependency for this unit.

Exact coverage/mutation, race, script/fuzz where applicable, hot-key/resource
benchmarks, clean-consumer, API/docs/changelog and supply-chain gates are
REQUIRED. Raw-key/digest retention, stale revocation acceptance, Valkey-owned
debit/authorization, nil-source construction, possible-debit fallback or
fake-only interoperability blocks verification.
