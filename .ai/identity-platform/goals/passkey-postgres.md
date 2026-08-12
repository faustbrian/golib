# Goal: pkg/passkey/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `passkey/postgres`
- Canonical module: `pkg/passkey/postgres`
- Canonical goal after scaffolding: `pkg/passkey/postgres/.ai/GOAL.md`
- Public contracts: unit ID `contract:unit:passkey/postgres:v1`; owned operation IDs: none
- Requires: `passkey`, `identity/postgres`, `webauthn/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `outbox`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed assignment with all listed prerequisites verified.
Build the PostgreSQL identity-to-WebAuthn mapping, user-handle, label and
recovery-metadata adapter through public passkey/WebAuthn contracts.

## Ownership and public contract

The adapter owns identity-to-WebAuthn-credential references, opaque user-handle
mapping, passkey labels and management metadata, locking, migrations, cleanup
and reconciliation. Cryptographic credential and ceremony records belong to
`webauthn/postgres`. This adapter does not parse or verify WebAuthn, duplicate
public keys/counters, choose authenticator policy or issue sessions.

## Required behavior and evidence

- Credential IDs and opaque user handles MUST be stored losslessly. A
  credential ID is unique across the complete RP namespace. Each identity has
  exactly one stable opaque user handle per RP namespace, that handle is unique
  across the complete RP namespace, and neither constraint includes tenant as
  a disambiguating key. Tenant remains a mandatory authorization/partition
  binding and cross-tenant matches deny without disclosure. No internal database ID may be exposed as a user
  handle by default.
- Registration MUST compose the verified WebAuthn credential transaction with
  identity user-handle/reference and safe passkey metadata so concurrent
  callbacks have one coherent owner or a reconciliation-required outcome.
- Assertion MUST consume the atomic counter/backup result from
  `webauthn/postgres` and update only identity-facing metadata; it MUST NOT
  maintain a second counter or reinterpret cloned/unknown outcomes.
- Discoverable lookup MUST resolve credential and user handle together without
  cross-RP/tenant leakage or a separate enumeration oracle.
- Rename/delete MUST use credential version/recent-auth inputs and preserve the
  last-recovery-method decision owned by passkey.
- The adapter MUST compose with the ceremony/challenge store from
  `webauthn/postgres`; it MUST NOT own, duplicate or conditionally fall back to
  a second ceremony or cryptographic credential store.
- Cleanup and list queries MUST be bounded/indexed. Errors, traces, fixtures
  and evidence MUST NOT expose credential public-key blobs unnecessarily or
  any challenge/session bearer value.
- Real PostgreSQL races, disconnect/unknown commit, populated migrations,
  mixed binaries, backup/restore and production-shaped plans are REQUIRED.
- Every mapping and user-handle lookup MUST use an explicit tenant/RP composite
  scope consistent with `.ai/identity-platform/PROTOCOL_BASELINES.md` while
  retaining credential-ID and user-handle uniqueness across the complete RP
  namespace. Those RP-wide constraints MUST NOT substitute for tenant
  isolation, and shared-RP deployments MUST NOT resolve a handle into another
  tenant's identity.
- Registration, assertion metadata and deletion MUST implement the passkey
  participant in `.ai/identity-platform/TRANSACTION_CONTRACT.md` and coordinate
  with `webauthn/postgres` and `identity/postgres` through public participant
  contracts. Process ordering or independently committed SQL transactions MUST
  not be called atomic; unknown outcomes require durable reconciliation.

Exact coverage/mutation, race, query/lock benchmarks, clean-consumer,
API/docs/changelog and supply-chain gates are REQUIRED. Duplicate credentials,
cross-RP lookup or non-atomic counter/ceremony updates block verification.
