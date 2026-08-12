# Goal: primitive/capability-postgres-identity-contracts

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `primitive/capability-postgres-identity-contracts`
- Canonical module: `pkg/capability/postgres`
- Canonical goal after scheduling: `pkg/capability/postgres/.ai/GOAL_IDENTITY_CONTRACTS.md`
- Requires: `primitive/capability-identity-contracts`
- Exact identity-platform consumers: `identity/magiclink`, `identity/mfa/postgres`, `identity/oauth/postgres`, `identity/otp`, `identity/reference`, `identity/session`, `oauth-server`, `oauth-server/device`, `oauth-server/postgres`, `organization`, `sso`, `sso/postgres`
- Unlocks after verification: `identity/magiclink`, `identity/mfa/postgres`, `identity/oauth/postgres`, `identity/otp`, `identity/reference`, `identity/session`, `oauth-server`, `oauth-server/device`, `oauth-server/postgres`, `organization`, `sso`, `sso/postgres`

## Start gate and pinned contract

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST
start only after the coordinator marks this unit `in-progress` and verifies
`primitive/capability-identity-contracts`. The current
`pkg/capability/postgres` API MUST match
`sha256:490d0a2a90718f8cdcb0f36ded70fb356c5f8bfe9da7a8ad08e7a5beb4d04431`,
and its completed required contract MUST reproduce
`sha256:670937908b9793ccb7fae6865dbe35291a2474356829a27f19a8749bdd1ada7b`.
All digests MUST use the canonical algorithm in
`.ai/identity-platform/fragments/public_contracts_meta.json`; mismatch blocks
execution and MUST NOT be silently re-pinned.

## Objective and exact addition

Extend only `pkg/capability/postgres` with the identity-platform transaction
boundary. It MUST add `capabilitypostgres.Enlister` with exactly
`Enlist(context.Context, pgx.Tx) (capability.ConsumptionStore, error)` and MUST
retain the pinned `ConsumptionStore`, constructor, cleanup, consume methods,
and error mappings. It MUST NOT redefine core proof, purpose, reference,
service, payload, grant, caveat, revocation, or consumption contracts.

## Behavioral, compatibility, and evidence requirements

- `Enlister.Enlist` MUST bind consumption to the caller-owned PostgreSQL
  transaction and MUST NOT commit, roll back, or escape that transaction.
- `Enlister.Enlist` MUST reject nil, closed, or foreign transactions before mutation.
- PostgreSQL commit ambiguity MUST map to `capability.ErrConsumptionUnknown`.
- Consumption MUST atomically increment only below the signed maximum-use bound.
- Public errors MUST remain stable, typed, redacted, and usable for
  reconciliation without exposing proof material.
- The addition MUST remain compatible with the verified core contract and
  existing stored consumption data. Representation changes require an explicit
  versioned migration with rollback and mixed-version tests.

Focused PostgreSQL tests MUST prove same-transaction enlistment, rollback,
commit, conflict, unknown outcome, nil/closed/foreign rejection, concurrent
maximum-use enforcement, and cleanup. The worker MUST run exact coverage,
mutation, race, leak, API-baseline, clean-consumer, documentation, example,
inventory, infrastructure, supply-chain, and changed reverse-dependant gates
for the owned module. Its changelog MUST describe the additive contract and
migration impact. This unit MUST remain unverified until its required digest
and every applicable gate pass.

## Scope boundary

Changes MUST be limited to `pkg/capability/postgres`, its tests, API baseline,
documentation, examples, changelog, and mechanically required manifests. The
worker MUST NOT modify capability token formats, core cryptography, identity
flows, another adapter, or another module's persistence ownership.
