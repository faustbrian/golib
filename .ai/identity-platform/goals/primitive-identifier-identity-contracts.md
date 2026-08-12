# Goal: primitive/identifier-identity-contracts

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `primitive/identifier-identity-contracts`
- Canonical module: `pkg/identifier`
- Canonical goal after scheduling: `pkg/identifier/.ai/GOAL_IDENTITY_CONTRACTS.md`
- Requires: None
- Exact identity-platform consumers: `identity/email`, `identity/magiclink`, `identity/otp`, `identity/password`, `identity/risk/captcha`, `identity/username`, `passkey`, `webauthn`
- Unlocks after verification: `identity/email`, `identity/magiclink`, `identity/otp`, `identity/password`, `identity/username`, `passkey`, `webauthn`

## Start gate and pinned contract

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST
start only after the coordinator marks this unit `in-progress`. The current API
MUST match
`sha256:a0c2c1605a1086202e38d5d00963dadc20fb18a361c2d5703e598ea4087c5ec9`.
The completed required contract MUST reproduce
`sha256:c683662a1c639d6dfc5751fe0ea297b5bb1bfeaefdfa111ed32f8a5c7df9cbbd`
under the canonical digest algorithm in
`.ai/identity-platform/fragments/public_contracts_meta.json`. A current-API mismatch MUST block the
goal for reconciliation and MUST NOT be silently re-pinned.

## Objective and exact additions

Extend the existing identifier primitive with the exact bounded input,
normalization, canonical, and reference vocabulary required by identity
consumers. The module MUST add:

- `Canonical`: defined string containing 1..4096 valid UTF-8 bytes and accepted only from `Normalize`.
- `Input`: defined string containing 1..4096 valid UTF-8 bytes.
- `NormalizationProfile`: `struct{Unicode UnicodeNormalization; CaseFold bool; TrimSpace bool; IDNA bool; MaximumBytes uint32}`.
- `Reference`: `struct{Kind string; Canonical Canonical; Display string}`.
- `UnicodeNormalization`: `enum{UnicodeNormalizationUnspecified,UnicodeNormalizationNone,UnicodeNormalizationNFC,UnicodeNormalizationNFKC}`.
- `NewInput(string) (Input, error)`.
- `NewNormalizationProfile(NormalizationProfile) (NormalizationProfile, error)`.
- `NewReference(string, Canonical, string) (Reference, error)`.
- `Normalize(Input, NormalizationProfile) (Canonical, error)`.

The module MUST expose `ErrInvalid`, `ErrNormalization`, and `ErrUnsupported`
with the exact pinned semantics. It MUST NOT add a generic identifier payload,
an untyped normalization option map, a hidden global profile, authentication
meaning, authorization meaning, or identity-record ownership.

## Behavioral and security requirements

- `NewInput` MUST validate UTF-8 and byte length without normalizing before a
  profile is selected.
- Input values MUST be treated as enumeration-sensitive and MUST NOT appear in
  logs, traces, metrics, errors, or fixtures.
- `NormalizationProfile` MUST explicitly select Unicode behavior and MUST
  require `MaximumBytes` within 1..4096.
- `UnicodeNormalizationUnspecified` MUST be invalid.
- `Normalize` MUST be deterministic for one immutable profile and MUST reject
  unsupported or over-limit results.
- A profile change MUST require a migration and MUST NOT silently reinterpret
  an existing canonical value.
- `Canonical` equality MUST have meaning only under the same profile and MUST
  NOT be presented as a display value.
- `Reference.Kind` MUST contain 1..64 canonical ASCII bytes.
- `Reference.Canonical` MUST be non-zero; `Display` MUST be optional valid
  UTF-8 bounded to 4096 bytes.
- `Reference` MUST carry identifier identity only and MUST NOT constitute
  authentication or authorization evidence.
- Constructors and normalization MUST copy or immutably own retained values.
- Errors MUST be stable, typed, bounded, redacted, and distinguish invalid,
  normalization-failed, and unsupported cases.
- Public documentation MUST define profile identity, zero values, ordering,
  ownership, concurrency, privacy, migration, and error semantics.

## Compatibility, migration, and evidence

The extension MUST be additive to the pinned current identifier API. Existing
identifier kinds and normalization behavior MUST remain source- and
behavior-compatible. Consumers MUST migrate from local input, canonical,
profile, and reference types to the canonical additions. Stored canonical
identifiers MUST retain their profile/version identity; a profile or algorithm
change MUST use a versioned, collision-aware, rollback-capable migration with
mixed-version compatibility evidence.

Focused tests MUST prove UTF-8 and byte bounds, deterministic normalization,
every Unicode mode, case/space/IDNA flags, profile immutability, collision
handling, reference bounds, redaction, defensive ownership, and concurrent use.
Applicable fuzz tests MUST exercise hostile Unicode, IDNA, length, and invalid
encoding inputs. The worker MUST run exact coverage, mutation, race, fuzz,
API-baseline, clean-consumer, documentation, example, inventory, supply-chain,
and changed reverse-dependant gates. `pkg/identifier/CHANGELOG.md` MUST describe
the additive API and migration impact. Every exported addition MUST have useful
Go documentation. This unit MUST remain unverified until the required digest
and every applicable gate pass; skipped, stale, warning-only, or missing
evidence MUST NOT count as a pass.

## Scope boundary

The worker MUST NOT redesign identifier kinds, identity records, authentication,
authorization, delivery, storage, or protocol packages. Changes MUST be
limited to the exact extension, its tests, API baseline, documentation,
examples, changelog, and mechanically required manifests.
