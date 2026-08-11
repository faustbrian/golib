# Goal: pkg/webauthn/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `webauthn/postgres`
- Canonical module: `pkg/webauthn/postgres`
- Canonical goal after scaffolding: `pkg/webauthn/postgres/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:webauthn/postgres:v1`; owned operation IDs: none
- Requires: `webauthn`, `identity/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `outbox`, `audit`
- Unlocks after verification: `identity/mfa/postgres`, `passkey/postgres`, `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed assignment with every listed prerequisite verified.
Build the generic PostgreSQL WebAuthn ceremony and credential store shared by
passkeys and non-passkey MFA security keys.

## Ownership and public contract

The adapter owns RP-scoped credential ID/public key/algorithm/transports/
AAGUID/counter/backup/attestation metadata, opaque user-handle bytes supplied by
consumers, registration/assertion ceremony state, challenge digests, versions,
locking, migrations, cleanup and reconciliation. It does not own identity user
records, passkey labels/recovery policy, MFA factor state, WebAuthn verification
or private authenticator keys.

It MUST implement only public store/transaction interfaces defined by
`webauthn`; passkey and MFA adapters MUST reference its stable credential IDs
rather than duplicate cryptographic records.

## Required behavior and evidence

- Credential/challenge/user-handle bytes MUST round-trip losslessly with
  explicit maximum lengths. Credential uniqueness MUST be scoped by RP and the
  declared multi-tenant policy and resist cross-RP lookup.
- Ceremony creation MUST store only a scoped challenge digest plus bounded
  policy/context, expiration and version; raw browser challenge tokens MUST NOT
  appear in diagnostics or durable evidence.
- Registration MUST atomically consume the ceremony and insert one credential,
  rejecting duplicate credential/user-handle mappings with one winner.
- Assertion MUST atomically consume the ceremony, compare credential version,
  update signature counter and backup state, and return typed stale/cloned/
  unknown outcomes without accepting the assertion itself.
- Discoverable lookup MUST bind RP, credential and user handle in one bounded
  operation and MUST NOT create an independent user-enumeration endpoint.
- Attestation/metadata references and transports/extensions MUST be bounded and
  versioned; large raw attestation objects MUST NOT be retained unless a named
  audited policy requires them.
- Cleanup MUST remove expired ceremonies in bounded indexed batches and retain
  active credentials. Credential deletion MUST use optimistic versioning and
  preserve audit/outbox boundaries for owning consumers.
- Real PostgreSQL tests MUST cover concurrent registration/assertion,
  counter/backup updates, disconnect/unknown commit, cross-RP isolation,
  populated migrations, mixed binaries, backup/restore and query plans.
- Tenant and RP scope MUST be explicit on ceremonies, credentials, user handles
  and mappings, but credential IDs and user handles MUST remain unique across
  the complete RP namespace; tenant MUST NOT disambiguate either uniqueness
  constraint. Deployment/Host input MUST NOT infer scope, and shared RP IDs
  MUST follow `.ai/identity-platform/PROTOCOL_BASELINES.md` without cross-tenant
  lookup or disclosure.
- Ceremony consumption plus credential insert, or ceremony consumption plus
  counter/backup/version update, MUST implement the WebAuthn participant in
  `.ai/identity-platform/TRANSACTION_CONTRACT.md`. A verified cryptographic
  result MUST NOT be returned as authenticated before finalization, and unknown
  commits MUST reconcile by stable ceremony/command/version identity.

Exact coverage/mutation, race, query/lock benchmarks, clean-consumer,
API/docs/changelog and supply-chain gates are REQUIRED. Duplicate ownership,
challenge replay, cross-RP leakage or non-atomic counter updates block
verification.
