# Goal: pkg/identity/oauth/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/oauth/postgres`
- Canonical module: `pkg/identity/oauth/postgres`
- Canonical goal after scaffolding: `pkg/identity/oauth/postgres/.ai/GOAL.md`
- Requires: `identity/oauth`, `identity/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `capability/postgres`, `secret-envelope`, `outbox`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed assignment with both prerequisites verified. Build the
PostgreSQL encrypted token-vault and refresh-coordination adapter that enlists
the authoritative identity and capability stores.

## Ownership and public contract

The adapter owns encrypted access/refresh token sets, granted scopes,
expiry/version, refresh leases, provider revocation reconciliation, migrations
and cleanup. Authoritative provider-subject uniqueness and account-link metadata
belong to `identity/postgres`; this adapter MUST enlist that participant through
`.ai/identity-platform/TRANSACTION_CONTRACT.md` rather than duplicate its rows or
constraints. This adapter MUST NOT own provider-link rows, account-link metadata,
provider-subject uniqueness constraints, or the `lifecycle.dimension.social_link`
authority version. It owns token-vault metadata and the
`lifecycle.dimension.social_connection` authority version; account-link and
social-link changes MUST enlist `identity/postgres` to mutate its authoritative
row and version in the same unit of work. It does not perform OAuth requests,
validate identity proof, choose linking policy or issue sessions.
Authorization state signing, expiry, revocation and replay consumption belong
exclusively to `capability` and the `identity/oauth` workflow. This adapter MUST
NOT create an authorization-state token format, replay table or fallback
consumption authority; it MUST enlist `capability/postgres` for the canonical
reserve/apply/finalize/recover state transitions.

## Required behavior and evidence

- Provider token records MUST reference the authoritative identity account-link
  version. A missing, moved, unlinked, or version-mismatched account MUST fail
  closed and enter bounded reconciliation; forced/administrator collision
  resolution remains an explicit audited `identity` command.
- Recoverable provider tokens MUST use `secret-envelope` with provider/account/
  tenant/client context, key ID and rotation. Lookup metadata MUST NOT expose
  token values or raw provider responses.
- Callback persistence MUST atomically enlist `identity/postgres` to link/update
  its authoritative provider-link row, account-link metadata, uniqueness claim,
  and `social_link` authority version while this adapter stores token metadata
  with identity/outbox state, or produce the shared command ledger's
  reconciliation-required unknown outcome; it MUST NOT issue duplicate users.
- Callback persistence MUST accept only an immutable `CapabilityProof` produced
  by read-only validation; that proof grants no authority and MUST NOT be already
  consumed. In the same authoritative unit of work, it MUST enlist
  `capability/postgres` to reserve the existing proof, apply its bound expiry,
  version, audience, origin, and risk checks to the identity link/token mutation,
  and finalize the capability with the command result. It MAY persist the
  proof's non-secret identity for audit/reconciliation linkage, but MUST NOT
  directly mutate capability state or reinterpret the proof.
- Refresh leases/locking MUST ensure one exchange per grant, compare token
  version, rotate encrypted values and preserve known revoked versus unknown
  provider outcomes. Token refresh, unlink, and token deletion MUST enlist
  `identity/postgres` to bump the authoritative `social_link` version in the
  same unit of work as the local token mutation.
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
