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
`sha256:5680c6b6b35014d3646b49993b11b30149fe2f639368fbacf0152b99d654d56d`
under the canonical digest algorithm in
`.ai/identity-platform/fragments/public_contracts_meta.json`. A current-API digest mismatch MUST
stop execution for a fresh contract reconciliation; it MUST NOT be silently
re-pinned.

## Objective and exact additions

Extend the existing authentication primitive only enough to supply the typed,
bounded proof vocabulary required by identity-platform consumers. The module
MUST add these exact public symbols and declarations:

- `Assurance`: `enum{AssuranceUnspecified,AssuranceSingleFactor,AssuranceMultiFactor,AssurancePhishingResistant}`.
- `OneTimeCode`: opaque immutable value containing 4..128 ASCII symbols in
  unexported storage and constructible only through `NewOneTimeCode`.
- `PermanentAccountProof`: opaque immutable authority-issued value with
  unexported proof and permanent-subject bindings.
- `PermanentAccountProofSpec`: `struct{Proof Proof; PermanentSubject string}`
  accepted only by the authority-gated issuer boundary.
- `PermanentAccountProofIssuer`: interface with exactly
  `IssuePermanentAccountProof(context.Context, PermanentAccountProofSpec) (PermanentAccountProof, error)`.
- `Proof`: `struct{ID ProofID; Tenant string; Subject string; Method string; Assurance Assurance; Purpose Purpose; Audience string; IssuedAt time.Time; ExpiresAt time.Time}`.
- `ProofID`: defined string containing 22..128 canonical ASCII characters.
- `Purpose`: defined string containing 1..128 canonical ASCII characters.
- `ReauthenticationProof`: `struct{Proof Proof; SessionID string; SessionVersion uint64; Action string}`.
- `ReauthenticationOrRecoveryProof`: the closed tagged union selected by `ProofKind`, with exactly `Reauthentication ReauthenticationProof` and `Recovery Proof` arms.
- `RecoveryCode`: opaque immutable value containing 8..256 canonical ASCII
  symbols in unexported storage and constructible only through
  `NewRecoveryCode`.
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
  logging, or formatting disclosure. Their concrete representation and secret
  bytes MUST be inaccessible outside the package; no accessor, conversion,
  marshal method, exported field, or error may reveal them. Callers MUST be
  able to release the opaque value immediately after terminal verification.
- `Proof` construction MUST require every identifier, bound each to at most
  256 UTF-8 bytes, require `IssuedAt < ExpiresAt`, and cap lifetime at 24 hours.
- Proof validation MUST bind the exact tenant, subject, purpose, and audience
  and MUST fail closed on expiry or mismatch.
- `PermanentAccountProof` MUST bind a non-anonymous permanent subject, tenant,
  audience, purpose, proof identity, issuance, and expiry. Only an
  authentication-authority implementation that has authoritatively validated
  the underlying permanent account may mint it. The package MUST NOT expose a
  struct literal or public constructor that accepts caller-supplied subject or
  identifier data as sufficient authority, and identifier existence alone MUST
  NOT produce the proof. Consumers MAY inspect only bounded non-secret binding
  projections needed for exact validation; they MUST NOT recover or replace
  the authority evidence.
- `PermanentAccountProofIssuer` MUST be constructed only by the package's
  trusted authentication-authority composition with an unexported minting
  capability. Its method MUST independently validate the supplied `Proof`,
  require the exact permanent-account-upgrade purpose, require subject equality
  with `PermanentSubject`, reject anonymous subjects, and copy all bindings.
  Merely implementing the public interface MUST NOT grant minting authority;
  consumers receive the issuer instance by explicit trusted composition and
  cannot instantiate an equivalent issuer from public values.
- The exact factory MUST be
  `PermanentAccountProofIssuerFromAuthenticator(Authenticator) (PermanentAccountProofIssuer, error)`.
  It MUST return an issuer only when the supplied authenticator is a
  package-owned trusted authority carrying the package's unexported minting
  capability; arbitrary external implementations of `Authenticator`,
  `PermanentAccountProofIssuer`, or both MUST be rejected with
  `ErrInvalidConfiguration`. The factory MUST retain no caller credential and
  MUST NOT authenticate as a side effect.
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
expiry, mismatch errors, compile-time non-constructibility of opaque values,
secret non-disclosure across conversion/marshal/format/error surfaces,
authority-only permanent-account-proof issuance, defensive copying, and
race-safe concurrent validation. Applicable fuzz tests MUST cover hostile code
and proof inputs. The worker MUST run the module's exact coverage, mutation, race, fuzz,
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
