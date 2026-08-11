# Identity Platform Transaction Contract

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

Every declarative table row and numbered protocol step in this document is a
normative **MUST** requirement unless the row or step explicitly says otherwise.
`tx.foundation` is the stable requirement ID for command identity, fingerprint,
ledger fields, reservation fencing/reconciliation, immutable risk evidence,
encrypted bearer delivery, and provider-effect record semantics. Selecting any
`tx.*` row or step MUST import `tx.foundation`; assignments MUST enumerate both.

## Scope and owners

This specification is the sole transaction contract for identity-platform
PostgreSQL adapters and one-time workflows. `identity` owns the storage-neutral
command/result vocabulary. `identity/postgres` owns the PostgreSQL unit-of-work,
command ledger, reservation/reconciliation worker, and public enlistment
carrier. `outbox/postgres` and `capability/postgres` MUST support that carrier;
standalone transactions are not conforming substitutes.

An adapter that needs the carrier MUST declare `identity/postgres` as a verified
prerequisite. `capability/postgres` enlistment is a prerequisite for every
one-time workflow. If either public API is absent, the dependant is blocked; a
worker MUST NOT copy SQL, open a second transaction, or invent a split-commit
saga.

## Command identity and fingerprint

A `CommandID` MUST contain exactly 128 bits of cryptographically random entropy
and use the sole canonical representation of 22-character base64url without
padding. It is globally collision-resistant, while all
lookup and authorization remain scoped by tenant and purpose. Callers MUST keep
the same ID across retries and MUST NOT derive it from user data.

The fingerprint input MUST use RFC 8949 deterministic CBOR, profile
`identity-command-v1`: map keys sort by deterministic encoded-key byte order;
integers and lengths use shortest form; floats and indefinite-length items are
forbidden; timestamps are signed integer Unix microseconds in PostgreSQL UTC;
durations are signed integer microseconds; IDs are their decoded fixed-length
byte strings; decimal values are forbidden unless the command schema declares a
fixed-scale signed integer; absent optionals are omitted; text is valid UTF-8
without implicit normalization. It MUST include command schema/version, tenant,
purpose, actor, effective subject, target aggregate and expected version,
authorization-policy version, applicable authority-version snapshot, all
behavior-affecting options, and a keyed digest of each secret-bearing input.
Each secret digest MUST be HMAC-SHA-256 with the stored fingerprint key version
over the ASCII domain `identity-command-secret-v1`, then a 32-bit unsigned
big-endian byte length and UTF-8 canonical field path, then a 64-bit unsigned
big-endian byte length and raw secret bytes. A path beyond 4,096 bytes or secret
beyond its command-schema bound MUST be rejected before framing. The digest
replaces the secret in CBOR.
The persisted fingerprint MUST be HMAC-SHA-256 over that encoding using a
versioned command-fingerprint key. The key version MUST be stored. New commands
use the newest key; lookup MUST verify retained prior keys until no
`pending`/unknown-observation row references the version and then for the
configured terminal result-retention period. Rotation MUST NOT make an existing
command unverifiable or reusable; retirement requires a proved atomic re-key of
safe canonical fingerprints or retention of the old key.
A same-ID mismatch, including an improbable digest collision revealed by stored
canonical safe fields, MUST return `Conflict` and MUST perform no mutation.

HTTP/SCIM idempotency admission MUST atomically map one scoped
`Idempotency-Key` keyed digest to one random `CommandID` before reservation. The
lookup and uniqueness key is exactly tenant, actor, method, canonical route ID,
and the keyed digest; it MUST NOT include the request body, request fingerprint,
or any other behavior-bearing field. The mapping stores the canonical request
fingerprint separately. A retry with the same scoped key digest and matching
fingerprint recovers that same command; the same scoped key digest with any
different canonical request fingerprint returns `Conflict` without allocating
a command or performing a mutation. An in-progress or unknown mapping remains
blocked and is never remapped after the 24-hour replay window. The mapping,
command ledger, and SCIM mutation MUST NOT be implemented as parallel
authorities.

A SCIM Bulk request has one random parent command and a bounded ordered list of
independently random child command IDs persisted atomically at admission. Each
declared child stores its `bulkId`, dependency IDs, request fingerprint, order,
state, and durable result. Child states are exactly `not-started`, `in-progress`,
`succeeded`, `failed`, `blocked-unknown-dependency`, and
`skipped-fail-on-errors`. Only conclusively successful dependencies permit a
child to enter `in-progress`; failed dependencies produce a durable SCIM
dependency failure, while an unknown dependency durably blocks the child until
reconciliation proves its outcome. An omitted or zero `failOnErrors` disables
the cutoff and executes every otherwise-admissible child. A positive value N
triggers the cutoff only after the Nth child durably reaches `failed`; operation
failures and dependency failures use that state, while `blocked-unknown-dependency`
and `skipped-fail-on-errors` do not increment the count. At that checkpoint,
every remaining `not-started` child atomically becomes
`skipped-fail-on-errors` with the stable SCIM response selected for that state;
already completed or blocked children are not rewritten. The parent checkpoint
persists the failed count and `cutoff_active=true`. A
`blocked-unknown-dependency` child remains incomplete until reconciliation
proves its dependency: a failed dependency transitions the child to durable
`failed` with the SCIM dependency response, while a successful dependency
transitions it to `skipped-fail-on-errors` when the cutoff is already active.
Such a child MUST NOT enter `in-progress` after the cutoff. The public parent
result remains `InProgress`/`Unknown` and cannot emit a terminal Bulk response
while any child is blocked. There is no later child admission. Each executing
child commits independently through the unit of work; the parent result is a
deterministic replay of every declared child in
original order from its durable checkpoint, including dependency failures,
blocked outcomes, and fail-on-errors skips. Savepoints MUST NOT be used to claim
durability or survive a root rollback. Delete results and unresolved mappings
remain replayable through the configured terminal/unknown retention even after
the resource tombstone ages.

## Durable command ledger and reservation state machine

The ledger key is `(tenant_id, purpose, command_id)`. It MUST store command and
fingerprint versions, keyed fingerprint and key version, actor/target safe
identifiers, authorization-policy version, state, owner generation, lease and
heartbeat timestamps, attempt number, authoritative creation/update time,
redacted terminal classification, safe result, aggregate versions, lifecycle
and outbox IDs, capability reservation ID, and external-effect IDs.

`unknown` is a caller observation, never a stored state. Stored states are
`pending`, `committed`, and `aborted`; `committed` MAY carry
`pending_effect_classes` and reconciliation handles as metadata. Public results
are `NotCommitted`, `Committed`, `Unknown`, `Conflict`, and `InProgress`.

Before opening the domain transaction, `ReserveCommand` MUST use a short
autocommit transaction against the primary PostgreSQL authority:

| Row ID | Existing row | Required result and transition |
| --- | --- | --- |
| `tx.command.first` | none | insert `pending`, generation 1, attempt 1, lease deadline, and heartbeat; return ownership |
| `tx.command.committed` | matching `committed` | return the same redacted `Committed` result; perform no work |
| `tx.command.aborted` | matching `aborted` | return the same redacted `NotCommitted` result; perform no work |
| `tx.command.live` | matching live `pending` | return `InProgress` with a bounded retry-after; perform no work |
| `tx.command.expired` | matching expired `pending` | atomically increment generation and attempt, replace lease owner/deadline, and return takeover ownership |
| `tx.command.conflict` | any fingerprint/scope mismatch | return `Conflict`; never disclose whether another tenant or purpose owns the ID |

An error, cancellation, or disconnect during `ReserveCommand` is ambiguous.
The caller MUST query the same scoped command ID on the primary and MUST NOT
allocate or accept a replacement ID. Only authoritative absence permits the
same reservation attempt to be repeated.

Only the current `(command_id, generation, lease_owner)` may heartbeat or enter
the domain transaction. Heartbeats use short autocommit transactions and stop
before commit begins. A stale owner MUST fail its generation check before its
first domain write and again when terminalizing. Lease expiry alone never means
`NotCommitted`; it permits takeover and reconciliation only.

The reconciler MUST query the primary, not a replica or cache. For an expired
`pending` row it MUST acquire a new generation, inspect authoritative aggregate,
capability, lifecycle, outbox, and effect evidence, and then either replay the
idempotent domain command, atomically finish `committed`, or finalize `aborted`
in a separate short transaction after proving no domain mutation committed.
Unprovable outcomes remain `pending` and surface as `Unknown`/`InProgress` until
the configured recovery deadline; crossing that deadline MUST alert an operator
and fail closed, not guess rollback.

## PostgreSQL unit of work

1. **`tx.uow.reserve`:** `ReserveCommand(ctx, scope, command)` MUST complete the durable reservation
   protocol above before `Begin`.
2. **`tx.uow.begin`:** `Begin(ctx, reservation)` MUST open one bounded domain transaction, lock the
   reserved command row first, verify generation/lease/fingerprint, and use the
   primary database UTC clock.
3. **`tx.uow.enlist`:** `Enlist(owner, contributor)` MUST register a compile-time typed contributor
   before its first write. Duplicate, undeclared, or late contributors fail.
4. **`tx.uow.contributor`:** Contributors MUST receive only transaction-scoped query/execute, command
   scope, database time, and outbox/effect writers. They MUST NOT commit,
   rollback, change isolation, open nested/private transactions, perform network
   I/O, or invoke unbounded or caller-controlled callbacks.
5. **`tx.uow.locks`:** Before the first mutation, contributors MUST publish all lock keys. The unit
   of work MUST acquire locks in this order: command row; tenant; primary
   aggregate type/ID; secondary aggregate type/ID; credential/session/grant ID;
   outbox sequence. Keys within a class sort lexicographically.
6. **`tx.uow.commit`:** `Commit(result)` MUST atomically write domain mutations, authority-version
   bumps, lifecycle events, audit/outbox/effect records, enlisted capability
   finalization, and `pending` to `committed`, guarded by owner generation,
   before the single PostgreSQL commit.
   Once commit begins, any error, cancellation, or disconnect returns `Unknown`,
   performs no local rollback, retry, or reveal-once material issuance, and
   enters primary-authority query/reconciliation. Only a proved successful
   commit returns `Committed`; a client context cancellation is not rollback
   proof.
7. **`tx.uow.rollback`:** `Rollback(cause)` MUST roll back the domain transaction.
   A proven rollback for a classified retryable deadlock/serialization attempt
   MUST leave the command `pending`; a separate short autocommit transaction
   guarded by owner generation MUST increment attempt, renew the lease, and
   record the redacted retry class before retrying under the policy below. If
   that bookkeeping is ambiguous, recovery takes ownership and no local retry
   occurs. A non-retryable failure or
   exhausted retry budget MUST finalize `pending` to `aborted` in a separate
   short transaction guarded by generation. Failure or ambiguity during
   rollback or terminalization MUST return `Unknown` and enter reconciliation.
8. **`tx.uow.query`:** `QueryCommand(ctx, tenant, purpose, commandID, caller)` MUST use the primary
   authority, authorize the caller for that scope, and return only the redacted
   safe result. Public not-found, wrong-scope, and unauthorized behavior MUST be
   constant-work and indistinguishable.

The default isolation level is `READ COMMITTED` with explicit row/advisory locks
and uniqueness constraints. `SERIALIZABLE` MAY be selected only with a package
rationale. The unit of work uses the exact `postgres.command_retry` policy: at
most three classified serialization/deadlock attempts under the same
reservation generation, with full-jitter retry upper bounds of 10 and 100
milliseconds. It MUST stop on cancellation, lease loss, ambiguous retry
bookkeeping, or any externally visible effect.

Network/provider risk evidence MUST be obtained before reservation as an
immutable signed or keyed `RiskEvidence` binding tenant, subject, operation,
signals, provider/configuration versions, decision, issued time, and expiry. The
transaction MUST perform only local scope/version/age checks plus authoritative
stored counter and policy checks. It MUST NOT invoke a risk/provider callback.

## Outbox, encrypted delivery, and provider effects

Lifecycle events and asynchronous effects MUST be inserted through the enlisted
outbox writer. IDs are deterministic from command ID plus effect class/index and
remain stable across retries. Mutation and insert commit together or neither
does. A `Committed` result with pending effects MUST name only safe effect
classes and handles; it MUST NOT claim external success.

An asynchronously delivered OTP, magic link, invitation, verification link, or
other bearer value MUST be persisted only as authenticated ciphertext using
`secret-envelope`. AAD MUST contain tenant ID, purpose, subject/resource ID,
command ID, effect ID, and template version. The outbox worker MAY decrypt only
for the bound sender invocation. Ciphertext MUST exist only while an effect is
`planned`, `submitted`, `retry-wait`, or `outcome-unknown`, and only until the
bearer/operation expiry bound. Every terminal state (`confirmed`, `rejected`,
`expired`, `cancelled`, `exhausted`, or `superseded`) MUST erase it atomically
with the terminal checkpoint; the keyed digest and redacted outcome stay as
replay authority. Plaintext MUST never be persisted, logged, traced, or put
in evidence. Synchronously revealed API keys, client secrets, session tokens,
and recovery codes remain request-memory only and follow the same unknown-commit
non-reissuance rule.

Provider effects use this legal state machine. `expired`, `cancelled`,
`exhausted`, and `superseded` are redacted terminal non-success states and MUST
NOT be reported as provider rejection or confirmation:

| Row ID | From | To | Condition |
| --- | --- | --- | --- |
| `tx.effect.submit` | `planned` | `submitted` | worker owns a live lease/generation and records attempt before call |
| `tx.effect.confirm` | `submitted` | `confirmed` | provider supplies authoritative success or status query confirms it |
| `tx.effect.reject` | `submitted` | `rejected` | provider supplies authoritative permanent rejection |
| `tx.effect.retry_wait` | `submitted` | `retry-wait` | provider supplies authoritative transient failure; persist bounded retry class and next-attempt time |
| `tx.effect.retry` | `retry-wait` | `submitted` | live lease/generation, unexpired effect, and the bound retry schedule admit the next recorded attempt |
| `tx.effect.retry_cancel` | `retry-wait` | `cancelled` | owning command cancels after the provider authoritatively reported no success for the prior attempt; ciphertext erased |
| `tx.effect.retry_expire` | `retry-wait` | `expired` | bearer or operation expires before another attempt; ciphertext erased |
| `tx.effect.retry_exhaust` | `retry-wait` | `exhausted` | the closed retry policy has no remaining attempt; ciphertext erased without claiming provider rejection |
| `tx.effect.retry_reconcile` | `retry-wait` | `confirmed`, `rejected`, or `outcome-unknown` | callback, status query, or operator evidence authoritatively proves success/rejection or proves the prior attempt remains ambiguous; terminal outcomes erase ciphertext |
| `tx.effect.unknown` | `submitted` | `outcome-unknown` | timeout/disconnect/ambiguous response |
| `tx.effect.reconcile` | `outcome-unknown` | `confirmed` or `rejected` | status query, callback, or operator evidence proves outcome; ciphertext erased |
| `tx.effect.resubmit` | `outcome-unknown` | `submitted` | pinned provider idempotency contract proves resubmission safe |
| `tx.effect.expire_planned` | `planned` | `expired` | bearer or operation expires before submission; ciphertext erased |
| `tx.effect.expire_unknown` | `submitted` or `outcome-unknown` | `expired` | provider status query proves no effect and bearer/operation expired |
| `tx.effect.cancel` | `planned` | `cancelled` | owning command cancels before submission |
| `tx.effect.cancel_submitted` | `submitted` | `outcome-unknown` | record `cancel_requested=true`; invoke provider cancellation/status outside the transaction and do not claim cancellation |
| `tx.effect.cancel_confirmed` | `outcome-unknown` | `cancelled` | provider status authoritatively confirms cancellation or no effect |
| `tx.effect.supersede` | `planned` | `superseded` | a committed replacement atomically names this effect as predecessor |

Every effect MUST store effect ID, state, generation, lease/heartbeat, attempt,
request fingerprint, provider configuration version, idempotency key, redacted
provider receipt/status-query cursor, callback sequence/time, next action, and
retention deadline. Duplicate or earlier callbacks MUST be idempotent; a later
authoritative terminal callback MAY reconcile `outcome-unknown` but MUST NOT
reverse a conflicting terminal state without an audited operator resolution.
Entering `confirmed`, `rejected`, `expired`, `cancelled`, `exhausted`, or
`superseded` MUST erase bearer or provider-response ciphertext in the same
transaction as the terminal checkpoint. Reconciliation MUST also repair any
legacy terminal row that still retains ciphertext before reporting it closed.

Every effect class MUST bind a closed retry policy from
`REFERENCE_CONFIGURATION.md`. Delivery uses `delivery.provider_retry` and its
five-attempt schedule: the first attempt is immediate and the remaining four
use the exact `delivery.backoff` full-jitter ceilings. General external calls use
`http.external_retry`. Authorization-code exchange and any token/credential
rotation have zero resubmission after an ambiguous send unless the selected
provider contract supplies and honors the persisted idempotency key. All retry
loops stop on cancellation, expiry, lease/generation loss, terminal provider
evidence, or exhausted attempts; timeout/disconnect is never rewritten as a
permanent rejection.

A synchronous provider exchange that must precede a local domain mutation uses
the same durable effect reservation before network I/O: reserve and record the
attempt, call outside every database transaction, store the response only as
bounded authenticated ciphertext plus a keyed fingerprint, then apply it in one
local command transaction. Ambiguous exchange becomes `outcome-unknown` and is
reconciled without resubmission unless the pinned provider idempotency contract
proves it safe. Database locks are never held across the provider call.

## One-time capability protocol

Password reset, email verification/change, magic link, invitation, session
transfer, social and enterprise OAuth/OIDC state, SAML RelayState, and every
one-time capability MUST use these steps. A protocol adapter MAY validate the
opaque bearer, but `capability/postgres` is REQUIRED for durable reserve,
apply, finalize and recovery before a callback can issue authority:

1. **`tx.capability.issue`: Issue:** persist the replay record through the shared transaction. The
   `tx.capability.issue` transition is `absent` to `issued`; `issued` is the sole
   unconsumed capability state. Bind
   tenant, purpose, subject/resource, action, audience, allowed origins/redirects,
   authority versions, issue/expiry database time, key version, and `MaxUses=1`.
   Raw bearer material is released only after proven commit or encrypted for the
   delivery protocol above.
2. **`tx.capability.validate`: Validate:** perform read-only cryptographic and policy validation and return
   a bounded immutable `CapabilityProof`; this grants no authority.
3. **`tx.capability.reserve`: Reserve:** through enlisted `capability/postgres`, atomically lock the
   existing key `(tenant, purpose, keyed_capability_digest)`, bind command ID,
   proof fingerprint, generation, and target versions. The
   `tx.capability.reserve` transition is `issued` to `reserved`; reserve MUST NOT
   insert a missing capability record or reserve from any terminal state.
4. **`tx.capability.apply`: Apply:** inside the domain transaction, recheck expiry using PostgreSQL UTC,
   versions, status, audience, origin, and immutable risk evidence.
5. **`tx.capability.finalize`: Finalize:** transition `reserved` to `finalized` in the same domain commit
   as the mutation, invalidations, lifecycle/outbox records, and command result.
6. **`tx.capability.recover`: Recover:** `QueryCapability(ctx, tenant, purpose, digest, caller)` MUST use
   the primary authority and the same authorization, redaction, constant-work,
   and non-enumeration rules as `QueryCommand`. A finalized record returns the
   same safe result and never runs a second transition.

Capability states are `issued`, `reserved`, `finalized`, `released`, `expired`,
and `revoked`. Legal transitions are only `absent` to `issued`, `issued` to
`reserved`, `issued` to `expired` or `revoked`, `reserved` to `finalized`, and
`reserved` to `released`, `revoked`, or, after the PostgreSQL expiry time has passed, `expired` once
authoritative reconciliation proves the owning command did not commit.
The `reserved` to `revoked` transition additionally requires a lifecycle
invalidation. `finalized`, `released`, `expired`, and `revoked` are terminal and
MUST NOT return to `issued` or `reserved`. Duplicate issue with the same
fingerprint replays the existing record; a mismatch conflicts without replacing
it. `capability.terminal_retention` applies equally to `finalized`, `released`,
`expired`, and `revoked`. A revoked record MUST remain through the later of its
original capability expiry and the configured terminal-retention deadline.
During that interval `QueryCapability` MUST return its stable redacted revoked
classification and reserve MUST continue to return the same non-enumerating
denial. Only after both bounds pass MAY cleanup atomically crypto-shred payload
and subject linkage. It MUST retain a restricted tombstone containing only
tenant, purpose, keyed digest, key version, original expiry, and `revoked`; the
tombstone has no time-based deletion and every reserve/query continues the same
denial. The tombstone MAY be deleted only after the cryptographic key version is
retired and proof shows no bearer under it can validate. Post-disposal replay is
therefore denied by the tombstone or, after that proved key retirement, by
cryptographic validation before lookup. Issuance creates a new random
bearer/digest and MUST NOT reconstruct `issued` from the presented bearer.
Timeout, disconnect, death, or lease expiry MUST retain `reserved` until
reconciliation proves a legal terminal transition. Retention MUST meet
`REFERENCE_CONFIGURATION.md`; capability
expiry alone MUST NOT release an outcome-unknown reservation. There is no
standalone-ledger fallback. When the reference retention policy crypto-shreds
an old unknown payload, its keyed digest, tenant, purpose, command binding, and
`reserved` denial state MUST remain in the primary restricted archive and MUST
participate in every reserve/query decision until authoritative resolution.

## Required proof

Implementations MUST prove against real PostgreSQL: every reservation-table row;
fingerprint canonicalization and rotation; tenant/purpose authorization and
non-enumeration; one winner under races and stale-owner fencing; heartbeats,
takeover, process death, and recovery deadline; deadlock/serialization retry;
cancellation; disconnect before/during/after commit; durable aborted outcome;
outbox and capability atomicity; replay; encrypted queued bearer delivery and
erasure; reveal-once unknown commit; effect callback ordering/status-query
reconciliation; and restart recovery. Fakes and source assertions are
insufficient.
