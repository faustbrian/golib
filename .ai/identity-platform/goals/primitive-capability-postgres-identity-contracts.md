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
- Requires: `primitive/capability-identity-contracts`, `identity/postgres`
- Exact identity-platform consumers: `identity/mfa/postgres`, `identity/oauth/postgres`, `identity/reference`, `oauth-server/postgres`, `sso/postgres`
- Unlocks after verification: `identity/mfa/postgres`, `identity/oauth/postgres`, `identity/reference`, `oauth-server/postgres`, `sso/postgres`

## Start gate and pinned contract

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST
start only after the coordinator marks this unit `in-progress` and verifies
`primitive/capability-identity-contracts` and `identity/postgres`. The current
`pkg/capability/postgres` API MUST match
`sha256:490d0a2a90718f8cdcb0f36ded70fb356c5f8bfe9da7a8ad08e7a5beb4d04431`,
and its completed required contract MUST reproduce
`sha256:38fbe861c4cf767bbdddcc005c91b614d5f6d2516846a555a87c608d6dcdccd7`.
All digests MUST use the canonical algorithm in
`.ai/identity-platform/fragments/public_contracts_meta.json`; mismatch blocks
execution and MUST NOT be silently re-pinned.

## Objective and exact addition

Extend only `pkg/capability/postgres` with the capability participant for the
sole `identity/postgres` transaction boundary. Its consumption store MUST
implement the exact versioned, openly implementable
`identitypostgres.Contributor` contract produced by the verified
`identity/postgres` unit. It MUST expose its immutable contributor descriptor
and capability apply operation through the exact required public contract, and
MUST implement the coordinator-only reserve, takeover rebind, finalize,
release, status, and recover lifecycle semantics required by
`.ai/identity-platform/TRANSACTION_CONTRACT.md`. It MUST NOT publish
`capabilitypostgres.Enlister`, accept or expose `pgx.Tx`, implement a parallel
coordinator, or offer an independently committable transaction API. It MUST
retain the pinned consumption-store constructor, cleanup behavior, and error
mappings and MUST NOT redefine core proof, purpose, reference, service,
payload, grant, caveat, revocation, or consumption contracts.

## Behavioral, compatibility, and evidence requirements

- The contributor implementation MUST remain open to independent modules, but
  invocation authority MUST be closed by the `identity/postgres` immutable
  registry plus its unexported generation- and phase-bound handles. A caller
  MUST NOT directly invoke or fabricate reserve, rebind, finalize, release,
  status, or recovery authority.
- Descriptor identity, contract version, participant input, command ID,
  fingerprint, tenant, purpose, capability ID, signed maximum uses, expiry,
  and coordinator generation MUST be validated before mutation. Nil, stale,
  foreign-registry, wrong-phase, closed-work, or mismatched handles MUST fail
  before a consumption transition.
- Reserve MUST atomically bind one capability consumption to the exact command,
  fingerprint, and generation before domain work. Takeover rebind MUST use a
  compare-and-swap from exactly the prior live generation and MUST reject a
  missing, terminal, different-command, different-fingerprint, or non-prior
  reservation without changing it.
- Apply MUST accept only the generation-bound `identitypostgres.Work` supplied
  for that operation, atomically increment only below the signed maximum-use
  bound, and return the in-transaction consumption result. It MUST NOT retain
  Work or open a private transaction.
- Finalize MUST transition the exact reservation to its committed consumption
  state in the coordinator's domain transaction. Release MUST transition only
  a proved-not-committed reservation to its legal reusable or terminal state
  under the same command and generation. Neither hook may reactivate a
  finalized, released, expired, or revoked record.
- Status MUST report bounded authoritative participant evidence without proof
  material. Recover MUST be idempotent for the frozen command plan and may
  transition only this capability participant according to authoritative
  evidence; it MUST NOT terminalize the command ledger, recover another
  contributor, guess success or rollback, or create a second consumption.
- `identitypostgres.Coordinator` alone MUST begin, commit, roll back, classify
  committed/not-committed/unknown, and reconcile the command. This module MUST
  NOT map coordinator commit errors to `capability.ErrConsumptionUnknown` or
  claim the transaction outcome.
- Consumption MUST atomically increment only below the signed maximum-use bound.
- Public errors MUST remain stable, typed, redacted, and usable for
  reconciliation without exposing proof material.
- The addition MUST remain compatible with the verified core contract and
  existing stored consumption data. Representation changes require an explicit
  versioned migration with rollback and mixed-version tests.

Focused PostgreSQL tests MUST prove registry construction and descriptor
version checks; coordinator-only reserve, takeover rebind, apply, finalize,
release, status, and recovery; stale/foreign/wrong-phase handle rejection;
same-transaction rollback and commit; conflict and coordinator-level unknown-
outcome recovery without contributor outcome claims; concurrent maximum-use
enforcement; terminal non-reactivation; and cleanup. Clean-consumer tests MUST
prove there is no `capabilitypostgres.Enlister`, raw transaction API, or bypass
of the public `identitypostgres.Contributor` contract. The worker MUST run exact coverage,
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
