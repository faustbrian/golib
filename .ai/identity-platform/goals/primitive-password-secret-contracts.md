# Goal: primitive/password-secret-contracts

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `primitive/password-secret-contracts`
- Canonical module: `pkg/password`
- Canonical goal after scheduling: `pkg/password/.ai/GOAL_IDENTITY_CONTRACTS.md`
- Requires: None
- Exact identity-platform consumers: `identity/otp`, `identity/password`, `identity/phone`, `identity/username`
- Unlocks after verification: `identity/otp`, `identity/password`, `identity/phone`, `identity/username`

## Start gate and pinned contract

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and MUST
start only after the coordinator marks this unit `in-progress`. The current API
MUST match
`sha256:f72bb24fae258535ba83f5c323d8bc5ee9cf9d4f29b162f5b8550536a06872f4`.
The completed required contract MUST reproduce
`sha256:d16e6923889a4a30acc0a22e489815ecac57e913213d0bad51010b97c18b20b9`
under the canonical digest algorithm in
`.ai/identity-platform/fragments/public_contracts_meta.json`. A current-API mismatch MUST block the
goal for reconciliation and MUST NOT be accepted by changing the pin.

## Objective and exact additions

Extend the existing password primitive with the exact owned secret and
algorithm-parameter values required by identity consumers. The module MUST add:

- `Parameters`: `struct{Algorithm Algorithm; Argon2id Argon2idParameters; BcryptCost int}`.
- `Secret`: a struct with unexported owned `[]byte`, constructed only by `NewSecret`, with a maximum length of 1,048,576 bytes.
- `NewParameters(Parameters) (Parameters, error)`.
- `NewSecret([]byte) (Secret, error)`.
- `Secret.Destroy()`.
- `Secret.Len() int`.
- `Secret.Use(func([]byte) error) error`.

The implementation MUST retain the exact pinned error set, including
`ErrAdmission`, `ErrCanceled`, `ErrClosed`, `ErrEntropy`, `ErrInvalidPolicy`,
`ErrInvalidSecret`, `ErrMalformedHash`, `ErrMismatch`, `ErrResourceRejected`,
`ErrUnsupportedAlgorithm`, and `ErrUnsupportedVersion`. It MUST NOT expose the
owned byte slice, add a generic secret container, add an opaque parameter map,
or move password lifecycle ownership into identity packages.

## Behavioral and security requirements

- `Parameters` MUST populate exactly the arm selected by `Algorithm`.
- Argon2id parameters MUST use version 19 and MUST remain within the enclosing
  policy's work, memory, parallelism, salt, and output limits.
- Bcrypt cost MUST be within 4..31.
- `NewParameters` MUST reject an unspecified algorithm, mismatched arms, and
  every value outside the policy envelope.
- `NewSecret` MUST copy input and MUST reject empty or over-limit input.
- `Secret.Use` MUST expose bytes only to one synchronous callback and MUST NOT
  permit the slice to escape as an owned password value.
- `Secret.Destroy` MUST clear owned bytes and MUST be idempotent.
- `Secret.Len` and `Secret.Use` MUST report the destroyed state without
  restoring or disclosing secret bytes.
- `Secret` MUST NOT implement string, text, JSON, logging, or formatting disclosure.
- Copies, zero values, callback panics, cancellation, and concurrent calls MUST
  have explicit, tested ownership semantics and MUST NOT cause double-use or leakage.
- Errors MUST be stable, typed, redacted, and MUST NOT contain secret material.
- Public documentation MUST define ownership, maximum size, callback lifetime,
  destruction, copying, concurrency, zero-value, and error semantics.

## Compatibility, migration, and evidence

The extension MUST be additive to the pinned current password API. Existing
algorithm, policy, hashing, verification, admission, and error behavior MUST
remain source- and behavior-compatible. Consumers MUST migrate from raw or
locally wrapped password bytes to `Secret`, and from duplicated algorithm
configuration to `Parameters`. Migration MUST keep secret lifetime bounded and
MUST NOT serialize `Secret`; persisted hashes retain their existing versioned
format and compatibility contract.

Focused tests MUST prove parameter-arm closure, policy limits, input copying,
synchronous callback lifetime, destruction, idempotence, zero/destroyed state,
non-disclosure, cancellation, panic cleanup, and race-safe concurrent behavior.
Applicable fuzz tests MUST cover hostile parameter and secret lengths without
retaining input. The worker MUST run exact coverage, mutation, race, fuzz,
leak, API-baseline, clean-consumer, documentation, example, inventory,
supply-chain, and changed reverse-dependant gates. `pkg/password/CHANGELOG.md`
MUST describe the additive API and consumer migration. Every exported addition
MUST have useful Go documentation. This unit MUST remain unverified until the
required digest and every applicable gate pass; skipped, stale, warning-only,
or missing evidence MUST NOT count as a pass.

## Scope boundary

The worker MUST NOT redesign hashing algorithms, policy defaults, credential
flows, identity ownership, storage, or transport. Changes MUST be limited to
the exact parameter and secret extension, its tests, API baseline,
documentation, examples, changelog, and mechanically required manifests.
