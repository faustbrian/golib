# Goal: primitive/authentication-identity-contracts

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `primitive/authentication-identity-contracts`
- Canonical module: `pkg/authentication`
- Canonical goal after scheduling: `pkg/authentication/.ai/GOAL_IDENTITY_CONTRACTS.md`
- Requires: None
- Exact identity-platform consumers: `identity/anonymous`, `identity/email`, `identity/magiclink`, `identity/mfa`, `identity/otp`, `identity/password`, `identity/phone`, `identity/username`, `passkey`, `webauthn`
- Unlocks after verification: `identity/anonymous`, `identity/email`, `identity/magiclink`, `identity/mfa`, `identity/otp`, `identity/password`, `identity/phone`, `identity/username`, `passkey`, `webauthn`

## Start gate and pinned contract

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. The
coordinator MUST mark this unit `in-progress` before work begins. The worker
MUST confirm that the current `pkg/authentication` API still has digest
`sha256:9f69f2ae325c4478da230e363f03f1990ffdee45093bf91fa07b1e593852b45e`.
The completed public contract MUST have required-contract digest
`sha256:4846e4ef155c350b63ce8c24e13e6411f1aaf18150caa365e971345af1ad601d`
under the canonical digest algorithm in
`.ai/identity-platform/fragments/public_contracts_meta.json`. A current-API digest mismatch MUST
stop execution for a fresh contract reconciliation; it MUST NOT be silently
re-pinned.

## Objective and exact additions

Extend the existing authentication primitive only enough to supply the typed,
bounded proof vocabulary required by identity-platform consumers. The module
MUST add these exact public symbols and declarations:

- `Assurance`: `enum{AssuranceUnspecified,AssuranceSingleFactor,AssuranceMultiFactor,AssurancePhishingResistant}`.
- `OneTimeCode`: defined string containing 4..128 ASCII symbols.
- `PermanentAccountProof`: `struct{Proof Proof; PermanentSubject string}`.
- `Proof`: `struct{ID ProofID; Tenant string; Subject string; Method string; Assurance Assurance; Purpose Purpose; Audience string; IssuedAt time.Time; ExpiresAt time.Time}`.
- `ProofID`: defined string containing 22..128 canonical ASCII characters.
- `Purpose`: defined string containing 1..128 canonical ASCII characters.
- `ReauthenticationProof`: `struct{Proof Proof; SessionID string; SessionVersion uint64; Action string}`.
- `ReauthenticationOrRecoveryProof`: the closed tagged union selected by `ProofKind`, with exactly `Reauthentication ReauthenticationProof` and `Recovery Proof` arms.
- `RecoveryCode`: defined string containing 8..256 canonical ASCII symbols.
- `RecoveryPath`: `enum{RecoveryPathUnspecified,RecoveryPathEmail,RecoveryPathPhone,RecoveryPathPasskey,RecoveryPathAdministrator}`.
- `SignInState`: `enum{SignInStateUnspecified,SignInStateAuthenticated,SignInStateChallengeRequired,SignInStateRejected}`.

The support contract MUST include `ProofKind`, `ProofSpec`, `ProofUse`, and
`ReauthenticationUse` exactly as declared by the pinned required contract. It
MUST expose `NewOneTimeCode`, `NewProof`, `NewPurpose`, `NewRecoveryCode`,
`Proof.Validate`, and `ReauthenticationProof.Validate` with the exact
signatures in `.ai/identity-platform/fragments/public_contracts_meta.json`. It MUST expose
`ErrExpiredProof`, `ErrInvalidProof`, `ErrProofAudienceMismatch`, and
`ErrProofPurposeMismatch` in addition to retaining the pinned authentication
errors. It MUST NOT introduce an opaque payload, generic claims map, alternate
proof authority, or identity-specific service facade.

## Behavioral and security requirements

- Successful assurance MUST reject `AssuranceUnspecified`; ordering MUST be
  compared through a package helper rather than numeric conversion.
- `OneTimeCode` and `RecoveryCode` MUST NOT implement string, text, JSON,
  logging, or formatting disclosure, and callers MUST be able to release them
  immediately after terminal verification.
- `Proof` construction MUST require every identifier, bound each to at most
  256 UTF-8 bytes, require `IssuedAt < ExpiresAt`, and cap lifetime at 24 hours.
- Proof validation MUST bind the exact tenant, subject, purpose, and audience
  and MUST fail closed on expiry or mismatch.
- `PermanentAccountProof` MUST bind a non-anonymous permanent subject and MUST
  NOT be constructible from identifier existence alone.
- `ReauthenticationProof` MUST require a positive session version and exact
  session, action, purpose, subject, tenant, and audience matches.
- The recovery union MUST populate exactly the arm selected by `ProofKind` and
  MUST reject recovery where the consuming operation does not explicitly allow it.
- Constructors MUST reject invalid zero values and MUST copy every retained
  caller-owned value.
- Errors MUST be stable, typed, redacted, and distinguish invalid, expired,
  audience-mismatched, and purpose-mismatched proofs.
- Public documentation MUST define zero values, lifetime, ownership,
  concurrency safety, cleanup, and redaction for every addition.

## Compatibility, migration, and evidence

The change MUST be additive for the pinned current authentication API and MUST
NOT alter existing `Principal`, `Authenticator`, credential, result, or error
semantics. Existing consumers MUST continue to compile unchanged. Consumers
MUST migrate only from locally duplicated proof/code types to these canonical
types; no wire or persisted representation may be rewritten without an
explicit versioned migration and compatibility test.

Focused tests MUST prove constructor bounds, closed enums/unions, exact binding,
expiry, mismatch errors, secret non-disclosure, defensive copying, and race-safe
concurrent validation. Applicable fuzz tests MUST cover hostile code and proof
inputs. The worker MUST run the module's exact coverage, mutation, race, fuzz,
API-baseline, clean-consumer, documentation, example, inventory, supply-chain,
and changed reverse-dependant gates. `pkg/authentication/CHANGELOG.md` MUST
describe the additive public contract and migration impact. All exported
symbols MUST have complete Go documentation. This unit MUST remain unverified
until the required digest is reproduced and every applicable gate passes; a
skipped, stale, warning-only, or missing result MUST NOT count as verification.

## Scope boundary

The worker MUST NOT redesign authentication flows, identity ownership,
authorization, storage, sessions, delivery, or protocol packages. Changes MUST
be limited to this exact primitive extension, its tests, API baseline,
documentation, examples, changelog, and mechanically required manifests.
