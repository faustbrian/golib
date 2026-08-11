# Goal: pkg/identity/delivery/postgres

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Execution metadata

- Unit: `identity/delivery/postgres`
- Canonical module: `pkg/identity/delivery/postgres`
- Canonical goal after scaffolding: `pkg/identity/delivery/postgres/.ai/GOAL.md`
- Requires: `identity/delivery`, `identity/postgres`
- Consumes existing primitives: `postgres`, `migrations`, `outbox`, `workflow`, `secret-envelope`, `audit`
- Unlocks after verification: `identity/reference`

## Start gate and objective

The worker MUST read and satisfy
`.ai/identity-platform/COMMON_REQUIREMENTS.md`. It MUST NOT begin until the
coordinator has marked `identity/delivery/postgres` `in-progress`, recorded
this worker, and verified every unit listed in Requires. Build the durable PostgreSQL
Queue and attempt store for identity-message intents without owning provider
transport.

## Ownership boundary

This adapter owns durable intent, template-version snapshot, recipient,
rendered-payload envelope, deduplication, schedule, attempt, lease, outcome,
cleanup and reconciliation state required by `identity/delivery`. It MUST
implement that module's public Queue/Attempt contracts and use the existing
outbox/workflow primitives through public APIs. It MUST NOT own SMTP/SMS vendor
configuration, call a Sender while a database transaction is open, invent a
second workflow engine, or treat queue admission as provider delivery.

## Required persistence contract

- The schema MUST bind deployment, tenant, message purpose, recipient scope,
  template ID/version, locale, idempotency key, intent version and timestamps.
  Database constraints MUST enforce the declared deduplication scope under
  concurrent enqueue.
- Recipient addresses, rendered bodies, one-time links and OTP content MUST be
  classified as sensitive. Recoverable queued payloads MUST use
  `secret-envelope` with tenant/purpose/intent context and key rotation; lookup
  indexes MUST contain only bounded non-secret identifiers or digests.
- Enqueue MUST atomically persist the immutable send intent and its outbox or
  workflow admission record. It MUST return one stable intent ID for duplicate
  commands and distinguish known rollback, committed enqueue and unknown
  commit.
- Cross-module enqueue MUST enlist through the public `identity/postgres`
  carrier defined by `TRANSACTION_CONTRACT.md`; this adapter MUST NOT copy its
  SQL or open a second transaction around an identity mutation.
- Template and locale changes after enqueue MUST NOT mutate an already queued
  message. The exact version and safely encrypted render input or rendered
  payload needed for deterministic retry MUST be retained explicitly.
- Claim/lease/heartbeat/release MUST use database time, bounded lease duration,
  fencing or version checks and skip-locked or equivalent safe ownership.
  Expired workers MUST NOT overwrite a later worker's outcome.
- Provider effects MUST use only the versioned states defined by
  `.ai/identity-platform/TRANSACTION_CONTRACT.md`. Attempt start, accepted,
  delivered, transient failure, permanent failure, bounced, cancelled and
  unknown provider outcome MUST remain bounded outcome or receipt
  classifications, not additional states. An unknown send MUST enter
  reconciliation and MUST NOT be retried blindly when the provider lacks
  idempotency.
- Retry scheduling MUST apply the delivery contract's bounded backoff,
  maximum-attempt, expiry and provider-idempotency policy. It MUST preserve the
  original one-time credential rather than generating a different secret
  outside the owning workflow.
  It MUST consume the exact `delivery.provider_retry` policy: an ambiguous
  outcome reconciles, resubmission requires the pinned provider idempotency
  identity, permanent rejection dead-letters, and queue acceptance is not
  delivery.
- A Sender MUST run after committed dequeue/lease acquisition and outside
  locks and transactions with a bounded context. Cancellation or process loss
  MUST leave a recoverable lease/outcome, not a fabricated failure or success.
- Enumeration-safe no-op-equivalent intents MUST have indistinguishable public
  enqueue outcomes while avoiding delivery to fabricated recipients. Their
  retention and audit behavior MUST NOT become an account-existence oracle.
- Cleanup MUST use bounded indexed batches, preserve records needed for retry,
  reconciliation, audit and legal hold, and cryptographically erase or remove
  sensitive payloads at the declared retention boundary.

Verification applicability is exact for this unit: `race=required`,
`fuzz=required`, `hostile=required`, `leak=required`, `benchmark=required`,
`infrastructure=required`, and `provider_interoperability=required`; a gate
MAY be satisfied by the required composed reference evidence but MUST NOT be
silently skipped.

## Required evidence

Real PostgreSQL tests MUST cover concurrent duplicate enqueue, worker lease
loss, retry races, cancellation, disconnect before/after commit, deadlock or
serialization classification, key rotation, sender unknown outcomes, cleanup,
populated migrations, mixed binaries and backup/restore. Integration tests MUST
use the public `identity/delivery` contracts and task-owned capture/Sender
implementations; a process-local fake Queue is not durable proof.

Exact coverage and mutation, race/stress/leak, production-shaped queue and
cleanup benchmarks, clean-consumer, API/docs/examples/changelog, migration,
security and supply-chain gates are REQUIRED. The unit MUST remain unverified
if queued secret material is exposed, duplicate enqueue can produce duplicate
delivery, a lease race loses an attributable outcome, or reference composition
requires a consumer-written Queue adapter.
