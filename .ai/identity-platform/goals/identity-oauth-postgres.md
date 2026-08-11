# Goal: pkg/identity/oauth/postgres

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/oauth/postgres`
- Canonical module: `pkg/identity/oauth/postgres`
- Canonical goal after scaffolding: `pkg/identity/oauth/postgres/.ai/GOAL.md`
- Requires: `identity/oauth`, `identity/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `secret-envelope`, `capability`, `outbox`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed assignment with both prerequisites verified. Build the
PostgreSQL social-provider account, authorization transaction, encrypted token
vault and refresh coordination adapter.

## Ownership and public contract

The adapter owns provider-account uniqueness and link metadata, encrypted
access/refresh token sets, granted scopes, expiry/version, refresh leases,
authorization transaction/replay state where not delegated to capability,
unlink/revocation reconciliation, migrations and cleanup. It does not perform
OAuth requests, validate identity proof, choose linking policy or issue sessions.

## Required behavior and evidence

- Provider account uniqueness MUST bind tenant, provider and stable provider
  subject and prevent concurrent links to different users. Forced/administrator
  collision resolution MUST remain an explicit audited core command.
- Recoverable provider tokens MUST use `secret-envelope` with provider/account/
  tenant/client context, key ID and rotation. Lookup metadata MUST not expose
  token values or raw provider responses.
- Callback persistence MUST atomically link/update the provider account and
  store token metadata with identity/outbox state or produce a
  reconciliation-required unknown outcome; it MUST not issue duplicate users.
- Refresh leases/locking MUST ensure one exchange per grant, compare token
  version, rotate encrypted values and preserve known revoked versus unknown
  provider outcomes.
- Incremental scopes and token history MUST never silently broaden the identity
  or authorization claims; stored granted scopes MUST reflect provider output.
- Unlink/delete MUST coordinate local credential removal, encrypted-token
  deletion and provider-revocation outcome without orphaning the last access
  path or claiming external revocation after failure.
- Cleanup MUST be bounded/indexed and respect active refresh/reconciliation and
  audit retention.
- Real PostgreSQL races, key rotation, unknown commit, populated migrations,
  mixed binaries, backup/restore and query plans are REQUIRED.

Exact coverage/mutation, race, query/lock benchmarks, clean-consumer,
API/docs/changelog and supply-chain gates are REQUIRED. Cross-account token
access, plaintext tokens, duplicate links or hidden unknown outcomes block
verification.
