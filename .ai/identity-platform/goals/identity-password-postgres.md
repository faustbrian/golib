# Goal: pkg/identity/password/postgres

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14.

## Execution metadata

- Unit: `identity/password/postgres`
- Canonical module: `pkg/identity/password/postgres`
- Canonical goal after scaffolding: `pkg/identity/password/postgres/.ai/GOAL.md`
- Requires: `identity/password`, `identity/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `password`, `outbox`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST satisfy `.ai/identity-platform/COMMON_REQUIREMENTS.md` and start only from the
coordinator's committed `in-progress` assignment with both prerequisites
verified. Build the PostgreSQL credential store and atomic password-workflow
transactions required by the complete password lifecycle.

## Ownership and public contract

This adapter owns password credential schema, hash/algorithm/parameter storage,
credential version, previous-hash policy where enabled, lookup/update locking,
hash-upgrade transaction, reset/change transaction support, migration,
cleanup and reconciliation. It does not hash or verify passwords, issue reset
capabilities, decide password policy, or store plaintext/recoverable passwords.

It MUST implement the exact consumer interfaces defined by `identity/password`
and integrate with `identity/postgres` units of work through public contracts.
Public results MUST distinguish missing credential, version conflict, known
rollback, committed update and unknown commit without exposing hash material.

## Required behavior and evidence

- Hash bytes/encoding, algorithm ID, reviewed parameters, creation/update time
  and credential version MUST round-trip without normalization or truncation.
- Set/change/reset/rehash MUST use compare-and-swap versioning and atomically
  update identity credential reference, outbox/audit state and configured
  session-revocation intent. Concurrent successful changes MUST have one winner.
- Administrator set and recovery reset MUST remain distinguishable audited
  commands and MUST not fabricate knowledge of the previous password.
- Optional password-history storage MUST use non-reversible hashes with bounded
  count/retention and a reviewed verification-cost policy; it MUST be disabled
  by default unless the password goal requires it.
- Queries/errors/plans/fixtures/evidence MUST never print hashes, salts,
  peppers, password bytes or reset material. Database dumps used in tests MUST
  contain only synthetic task-owned secrets.
- Migrations MUST cover legacy algorithm/parameter encodings, populated rows,
  interrupted backfill, mixed binaries, uniqueness/foreign-key validation and
  backup/restore with credentials remaining usable according to policy.
- Real PostgreSQL tests MUST cover concurrent rehash/change/reset, disconnect
  before/after commit, deadlock/serialization retry classification, account
  deletion and session-revocation/outbox consistency.

Exact coverage/mutation, race, production-shaped query/transaction benchmarks,
clean-consumer, API/docs/changelog and supply-chain gates are REQUIRED. The unit
MUST remain unverified if plaintext is recoverable, an unknown commit is
reported as success, or the reference password journey lacks durable proof.
