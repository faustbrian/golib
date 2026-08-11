# Goal: primitive/capability-identity-contracts

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `primitive/capability-identity-contracts`
- Canonical module: `pkg/capability`
- Canonical goal after scheduling: `pkg/capability/.ai/GOAL_IDENTITY_CONTRACTS.md`
- Requires: None
- Exact identity-platform consumers (the union for `capability` and `capability/postgres`): `identity`, `identity/email`, `identity/magiclink`, `identity/oauth`, `identity/oauth/onetap`, `identity/oauth/postgres`, `identity/oauth/proxy`, `identity/password`, `identity/session`, `oauth-server`, `oauth-server/device`, `oauth-server/oidc`, `organization`, `sso/domain-verification`, `sso/postgres`, `sso/saml`, `webauthn`
- Unlocks after verification: `identity`, `identity/email`, `identity/magiclink`, `identity/oauth`, `identity/oauth/onetap`, `identity/oauth/postgres`, `identity/oauth/proxy`, `identity/password`, `identity/session`, `oauth-server`, `oauth-server/device`, `oauth-server/oidc`, `organization`, `sso/domain-verification`, `sso/postgres`, `sso/saml`, `webauthn`

## Start gate and pinned contracts

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST
start only after the coordinator marks this unit `in-progress`. The current
`pkg/capability` API MUST match
`sha256:7ef868d0e91c94244a190ef8cd6d0b61f1aee787cd7032e7135ae9353edaf817`,
and its completed required contract MUST reproduce
`sha256:6af858713d36ea5902174d067f0b63d95f4f620c183c777951b482aaff8b17e0`.
The current `pkg/capability/postgres` API MUST match
`sha256:490d0a2a90718f8cdcb0f36ded70fb356c5f8bfe9da7a8ad08e7a5beb4d04431`,
and its completed required contract MUST reproduce
`sha256:670937908b9793ccb7fae6865dbe35291a2474356829a27f19a8749bdd1ada7b`.
All digests MUST use the canonical algorithm in
`.ai/identity-platform/fragments/public_contracts_meta.json`. Any current-API mismatch MUST block
execution for reconciliation and MUST NOT be silently re-pinned.

## Objective and exact additions

Extend the capability primitive and its existing PostgreSQL adapter only with
the identity-platform capability boundary. The core module MUST add:

- `Proof`: defined string containing 32..4096 canonical capability-token bytes.
- `Purpose`: defined string containing 1..128 canonical ASCII characters.
- `Reference`: `struct{CapabilityID string; Purpose Purpose; Digest [32]byte; ExpiresAt time.Time}`.
- `Service`: interface with exactly `Issue(context.Context, Payload) (Proof, Reference, error)` and `Verify(context.Context, Proof, Purpose, string) (Grant, error)`.

The core support contract MUST retain the exact pinned `CaveatEntry`,
`Consumption`, `ConsumptionResult`, `Payload`, and `RevocationQuery`
declarations and the exact `ConsumptionStore`, `Resolver`, and
`RevocationChecker` interfaces. It MUST expose `NewPurpose` and `NewReference`
with the pinned signatures while retaining the pinned grant, issue, parse, and
verify methods and error set.

`pkg/capability/postgres` MUST add `capabilitypostgres.Enlister` with exactly
`Enlist(context.Context, pgx.Tx) (capability.ConsumptionStore, error)`. The
adapter MUST retain the pinned `ConsumptionStore`, constructor, cleanup,
consume methods, and error mappings. Neither module MUST add an opaque payload,
arbitrary caveat map, generic token service, or identity-specific authority.

## Behavioral and security requirements

- `Proof` MUST remain secret and MUST NOT implement string, text, JSON,
  logging, or formatting disclosure.
- `Purpose` MUST name one exact operation and MUST reject prefix, wildcard,
  hierarchy, and cross-purpose substitution.
- `Reference` MUST require a bounded capability ID, exact purpose,
  domain-separated SHA-256 digest, and expiry; it MUST grant no authority alone.
- `Service.Issue` MUST use pinned signer, limits, and clock configuration.
- `Service.Verify` MUST bind exact purpose and audience and MUST check signature,
  time, revocation, and consumption before returning a grant.
- Every verification, revocation, or consumption error MUST fail closed.
- Capability audiences and caveats MUST remain bounded, canonical,
  bytewise-sorted, and duplicate-free as defined by `Limits`.
- Capability timestamps MUST preserve the pinned UTC-second ordering and
  maximum-lifetime contract.
- `Enlister.Enlist` MUST bind consumption to the caller-owned PostgreSQL
  transaction and MUST NOT commit, roll back, or escape that transaction.
- `Enlister.Enlist` MUST reject nil, closed, or foreign transactions before mutation.
- PostgreSQL commit ambiguity MUST map to `capability.ErrConsumptionUnknown`.
- Consumption MUST atomically increment only below the signed maximum-use bound.
- Public errors MUST remain stable, typed, redacted, and usable for
  reconciliation without exposing proof material.
- Documentation MUST state secret ownership, zero values, transaction
  ownership, concurrency, expiry, replay, and unknown-outcome semantics.

## Compatibility, migration, and evidence

Both changes MUST be additive to their pinned current APIs. Existing issue,
parse, verify, grant, consumption, cleanup, and transaction behavior MUST
remain compatible. Consumers MUST migrate from local proof, purpose, reference,
service, and enlistment facades to these canonical contracts. Stored capability
or consumption data MUST NOT be reinterpreted; any representation change MUST
use an explicit versioned migration with rollback and mixed-version tests.

Focused core tests MUST prove construction bounds, exact purpose/audience
binding, secret non-disclosure, expiry, revocation, replay, consumption, and
concurrent use. Focused PostgreSQL tests MUST prove same-transaction enlistment,
rollback, commit, conflict, unknown outcome, nil/closed/foreign rejection, and
cleanup. Applicable fuzz and hostile-input tests MUST exercise token parsing.
The worker MUST run exact coverage, mutation, race, fuzz, leak,
API-baseline, clean-consumer, documentation, example, inventory, supply-chain,
infrastructure, and changed reverse-dependant gates for both affected packages.
`pkg/capability/CHANGELOG.md` and the PostgreSQL adapter changelog MUST describe
the additive contracts and migration impact. Every exported addition MUST have
useful Go documentation. This unit MUST remain unverified until both required
digests and every applicable gate pass; skipped, stale, warning-only, or
missing evidence MUST NOT count as verification.

## Scope boundary

The worker MUST NOT redesign token formats, cryptography, identity flows,
storage ownership, or unrelated capability adapters. Changes MUST be limited
to the exact core and PostgreSQL extensions, their tests, API baselines,
documentation, examples, changelogs, and mechanically required manifests.
