# Goal: pkg/webauthn

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

## Execution metadata

- Unit: `webauthn`
- Canonical module: `pkg/webauthn`
- Canonical goal after scaffolding: `pkg/webauthn/.ai/GOAL.md`
- Requires: None; this root execution unit may be claimed when its existing primitive audit is current.
- Consumes existing primitives: `identifier`, `authentication`, `secret-envelope`, `audit`
- Unlocks after verification: `identity/mfa`, `webauthn/postgres`, `passkey`, `identity/http`

## Start gate

The worker MUST read and satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin
until the coordinator has marked `webauthn` `in-progress`, recorded this
worker, and verified every unit listed in Requires. The worker MUST reject an
assignment whose rendered prerequisites or scope differs from the inventory.

## Objective and observable completion

Build an independently releasable `pkg/webauthn` module that owns WebAuthn registration and authentication ceremonies, RP/origin/challenge policy, attestation and assertion verification, authenticator metadata, counters, and security-key profiles. Completion
requires executable proof of the behaviors below through its public API and,
where applicable, real supported infrastructure or providers.

## Ownership boundary

This module owns WebAuthn registration and authentication ceremonies, RP/origin/challenge policy, attestation and assertion verification, authenticator metadata, counters, and security-key profiles. It does not own identity signup/signin orchestration, passkey UX, sessions, browser JavaScript, and persistence implementations. Those exclusions MUST remain
outside its public API and dependency graph.

## Required public contract

The design MUST define RelyingParty, OriginPolicy, Challenge, Ceremony, Credential, AuthenticatorData, AttestationPolicy, AssertionPolicy, MetadataProvider, CounterPolicy, Store, and verifier contracts. Public errors MUST be typed, stable,
redacted, and useful for policy decisions without exposing enumeration or
secret state. Zero values, clocks, randomness, identifier canonicalization,
limits, and extension points MUST have explicit semantics.

## Required behavior

The implementation and tests MUST round-trip registration and assertion; verify client-data type, challenge, origin, RP ID hash, flags, algorithms, signatures, attestation policy, extensions, and counters; handle cloned authenticators explicitly; consume challenges once; parse hostile CBOR and COSE within strict limits. Every state transition MUST
define authorization, audit, idempotency, cancellation, cleanup, and
not-committed/committed/unknown outcomes where external or durable state is
involved.

## Package-specific acceptance checklist

- Registration and assertion options/results MUST round-trip WebAuthn JSON
  without lossy base64url, integer or extension conversion and MUST distinguish
  browser transport DTOs from verified domain objects.
- RP ID/origin policy MUST handle HTTPS web origins, permitted loopback
  development, explicit native/app profiles if supported, subdomain scope and
  trusted proxy input without suffix or port confusion.
- Challenges MUST have cryptographic entropy, purpose/RP/user binding,
  expiration, digest-at-rest lookup and atomic single consumption under
  concurrent ceremonies.
- Registration MUST verify clientDataJSON, attestationObject/authData, RP ID
  hash, user presence/verification, algorithm allowlist, credential ID/public
  key consistency, exclude list and attestation trust profile.
- Assertion MUST verify client data, authenticator data, flags, signature,
  allowed/discoverable credential rules, user handle and extensions before
  applying the counter/backup-state policy.
- Attestation profiles MUST explicitly support none, self and named trust-store
  formats only when implemented; metadata retrieval/cache/rotation/revocation
  MUST be bounded and SSRF-safe.
- CBOR, COSE, ASN.1 and JSON parsers MUST enforce depth, map/array/string/byte
  limits, duplicate-key policy, supported algorithms/curves and canonical
  integer conversion before allocation or signature work.
- Official WebAuthn/FIDO fixtures and at least one independent browser or
  authenticator implementation MUST prove registration/assertion, resident and
  non-resident credentials, extensions claimed, backup flags and counter cases.

## Security and abuse requirements

- Inputs MUST be bounded before parsing, allocation, storage, hashing, or
  cryptographic work.
- Subject, tenant, organization, purpose, audience, action, and redirect scope
  MUST be bound wherever applicable and MUST fail closed on mismatch.
- Enumeration, replay, fixation, confused-deputy, downgrade, race, and
  cross-scope attacks MUST have deterministic regression cases.
- Logs, traces, metrics, examples, fixtures, and errors MUST preserve the
  redaction requirements in `.ai/identity-platform/COMMON_REQUIREMENTS.md`.

## Persistence, lifecycle, and compatibility

The core MUST remain adapter-neutral unless this goal is itself an adapter.
State ownership, consistency, retention, deletion, migration, key rotation,
clock skew, concurrent callers, shutdown, and recovery MUST be documented and
tested where applicable. Unsupported protocol or deployment profiles MUST be
stated rather than silently approximated.

## Acceptance evidence

Before this unit becomes `verified`, the owner MUST satisfy every common gate,
the package-specific behavior above, the module's exact coverage and mutation
gates, race/fuzz/interoperability gates that apply, clean-consumer proof,
manifests, public API baseline, security and supply-chain checks, documentation,
changelog, and changed reverse-dependant gates. The final evidence record MUST
name any non-applicable gate with a reviewed reason; absence of infrastructure
or provider access is a blocker, not a pass.

## Release blockers

The unit MUST remain `implemented-unverified` or `blocked` if any prerequisite
is not `verified`, any ownership boundary is unresolved, a protocol claim
lacks pinned specification and interoperability evidence, a durable transition
has unhandled ambiguity, a secret can escape redaction, or any required gate is
stale, skipped, warning-only, or failing.
