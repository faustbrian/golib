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
- Exact identity-platform consumers: `identity`, `identity/email`, `identity/magiclink`, `identity/oauth`, `identity/oauth/onetap`, `identity/oauth/postgres`, `identity/oauth/proxy`, `identity/password`, `identity/session`, `oauth-server`, `oauth-server/device`, `oauth-server/oidc`, `organization`, `sso/domain-verification`, `sso/postgres`, `sso/saml`, `webauthn`
- Unlocks after verification: `identity`, `identity/email`, `identity/magiclink`, `identity/oauth`, `identity/oauth/onetap`, `identity/oauth/postgres`, `identity/oauth/proxy`, `identity/password`, `identity/session`, `oauth-server`, `oauth-server/device`, `oauth-server/oidc`, `organization`, `primitive/capability-postgres-identity-contracts`, `sso/domain-verification`, `sso/postgres`, `sso/saml`, `webauthn`

## Start gate and pinned contract

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST
start only after the coordinator marks this unit `in-progress`. The current
`pkg/capability` API MUST match
`sha256:7ef868d0e91c94244a190ef8cd6d0b61f1aee787cd7032e7135ae9353edaf817`,
and its completed required contract MUST reproduce
`sha256:6af858713d36ea5902174d067f0b63d95f4f620c183c777951b482aaff8b17e0`.
All digests MUST use the canonical algorithm in
`.ai/identity-platform/fragments/public_contracts_meta.json`; mismatch blocks
execution and MUST NOT be silently re-pinned.

## Objective and exact additions

Extend only the capability core with the identity-platform capability boundary:

- `Proof`: defined string containing 32..4096 canonical capability-token bytes.
- `Purpose`: defined string containing 1..128 canonical ASCII characters.
- `Reference`: `struct{CapabilityID string; Purpose Purpose; Digest [32]byte; ExpiresAt time.Time}`.
- `Service`: interface with exactly `Issue(context.Context, Payload) (Proof, Reference, error)` and `Verify(context.Context, Proof, Purpose, string) (Grant, error)`.

The core support contract MUST retain the exact pinned `CaveatEntry`,
`Consumption`, `ConsumptionResult`, `Payload`, and `RevocationQuery`
declarations and exact `ConsumptionStore`, `Resolver`, and `RevocationChecker`
interfaces. It MUST expose `NewPurpose` and `NewReference` with the pinned
signatures while retaining the pinned grant, issue, parse, verify methods and
error set. PostgreSQL behavior belongs exclusively to the ordered
`primitive/capability-postgres-identity-contracts` unit.

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
- Public errors MUST remain stable, typed, redacted, and usable for
  reconciliation without exposing proof material.
- Documentation MUST state secret ownership, zero values, concurrency, expiry,
  replay, revocation, consumption, and unknown-outcome semantics.

## Compatibility, migration, and evidence

The change MUST be additive to the pinned current API. Existing issue, parse,
verify, grant, revocation, and consumption behavior MUST remain compatible.
Consumers MUST migrate from local proof, purpose, reference, and service
facades to these canonical contracts. Stored capability data MUST NOT be
reinterpreted; representation changes require an explicit versioned migration
with rollback and mixed-version tests.

Focused tests MUST prove construction bounds, exact purpose/audience binding,
secret non-disclosure, expiry, revocation, replay, consumption, and concurrent
use. Applicable fuzz and hostile-input tests MUST exercise token parsing. The
worker MUST run exact coverage, mutation, race, fuzz, leak, API-baseline,
clean-consumer, documentation, example, inventory, supply-chain, and changed
reverse-dependant gates for the core package. `pkg/capability/CHANGELOG.md` MUST
describe the additive contracts and migration impact. This unit MUST remain
unverified until its required digest and every applicable gate pass.

## Scope boundary

Changes MUST be limited to the core extension, tests, API baseline,
documentation, examples, changelog, and mechanically required manifests. The
worker MUST NOT modify PostgreSQL or another adapter, redesign token formats or
cryptography, or add identity-specific authority.
