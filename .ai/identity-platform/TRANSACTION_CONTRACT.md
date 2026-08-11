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

HTTP/SCIM idempotency admission for proprietary application operations MUST atomically map one scoped
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

Standards-defined OAuth authorization, device authorization, and SCIM mutation
requests MUST remain interoperable without `Idempotency-Key`. Their admission
MUST derive one server-owned random command identity and persist a canonical
protocol-request fingerprint before mutation. When a client voluntarily sends
the extension header, its scoped digest MAY additionally map to that command
using the same conflict and recovery rules, but absence MUST NOT be rejected.
Protocol preconditions, authorization continuations, device codes, resource
versions, and durable request fingerprints remain the authoritative replay and
conflict boundaries.

A SCIM Bulk request has one random parent command and a bounded ordered list of
independently random child command IDs persisted atomically at admission. Each
declared child stores its `bulkId`, dependency IDs, request fingerprint, order,
state, stable preallocated resource ID where applicable, strongly connected
component (SCC) ID, SCC order, and durable result. Admission MUST build the
complete bounded dependency graph, reject unknown/cross-request references,
compute SCCs, and persist the graph plus its deterministic execution plan before
the first mutation. The SCC condensation graph executes in topological order;
ties use the lowest original operation index. Forward references therefore wait
for the referenced predecessor SCC rather than being rejected. Child states are
exactly `not-started`, `in-progress`, `succeeded`, `failed`,
`blocked-unknown-dependency`, and `skipped-fail-on-errors`. An acyclic singleton
enters `in-progress` only after every predecessor SCC conclusively succeeds. A
cyclic SCC MUST preallocate final resource IDs for all of its create members,
validate and substitute every within-SCC reference against those IDs, and apply
the complete bounded SCC in one transaction with deferred within-SCC referential
checks. Its child results and SCC checkpoint commit atomically; any member
failure rolls back the SCC and records a deterministic dependency failure for
every member, so no partial cycle is exposed. This SCC boundary MUST NOT be
described as whole-request atomicity. A failed predecessor produces a durable
SCIM dependency failure, while an unknown predecessor durably blocks the
dependent SCC until reconciliation proves its outcome. An omitted or zero `failOnErrors` disables
the cutoff and executes every otherwise-admissible child. A positive value N
triggers the cutoff only after the Nth child durably reaches `failed`; operation
failures and dependency failures use that state, while `blocked-unknown-dependency`
and `skipped-fail-on-errors` do not increment the count. At that checkpoint,
every remaining `not-started` child atomically becomes
`skipped-fail-on-errors`; skipped children are unprocessed and therefore have
no BulkResponse `Operations` member, `status`, `location`, `version`, or SCIM
Error body;
already completed or blocked children are not rewritten. The parent checkpoint
persists the failed count and `cutoff_active=true`. A
`blocked-unknown-dependency` child remains incomplete until reconciliation
proves its dependency: a failed dependency transitions the child to durable
`failed` with the SCIM dependency response, while a successful dependency
transitions it to `skipped-fail-on-errors` when the cutoff is already active,
again without a wire operation result.
Such a child MUST NOT enter `in-progress` after the cutoff. The public parent
result remains `InProgress`/`Unknown` and cannot emit a terminal Bulk response
while any child is blocked. There is no later child admission. Each acyclic
singleton commits independently through the unit of work; each cyclic SCC
commits through the bounded atomic SCC rule above. The terminal parent
result is a deterministic replay, in original request order, of exactly the
children that reached `succeeded` or `failed`. It MUST omit every
`skipped-fail-on-errors` child as unprocessed, and it cannot be terminal while a
child is `not-started`, `in-progress`, or `blocked-unknown-dependency`. The
durable parent checkpoint still retains every declared child and its final
state. Savepoints MUST NOT be used to claim
durability or survive a root rollback. Delete results and unresolved mappings
remain replayable through the configured terminal/unknown retention even after
the resource tombstone ages.

For SCIM DELETE, admission MUST persist a server-owned replay lookup keyed by
connection scope, canonical route and the canonical target/precondition request
fingerprint before mutation. A headerless retry with that identical lookup MUST
recover the original terminal command and replay its original successful DELETE
response without evaluating `If-Match` against the tombstone or allocating
another command. When `Idempotency-Key` is supplied, its scoped digest is an
additional lookup for the same command; reuse with any changed body,
precondition, target, or other fingerprint input MUST return `Conflict` without
mutation. A request with a different target or precondition fingerprint is a
new protocol request and observes current resource state. The original result remains recoverable through
`scim.idempotency_retention` even if `scim.delete_tombstone_retention` expires
first. Pending and unknown mappings have no time-based release.

Before the first child mutation, admission MUST compute a conservative
worst-case terminal BulkResponse size from every admitted child, including the
maximum status, location, version, `bulkId`, and bounded SCIM Error
representation. If that bound exceeds `scim.bulk.response_bytes`, the complete
request MUST fail without persisting or executing a child. Runtime serialization
MUST remain within the proved bound; truncating a terminal response after
mutation is forbidden.

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

1. **`tx.uow.enlist`:** `Enlist(owner, contributor)` MUST register every
   compile-time typed command, RiskEvidence, CaptchaEvidence, capability, OTP,
   domain, session,
   outbox, and effect contributor before the reservation transaction's first
   write. Duplicate, undeclared, or late contributors fail.
2. **`tx.uow.reserve`:** `ReserveCommand(ctx, scope, command, contributors)` MUST
   open one short primary-authority transaction and atomically complete the
   command reservation plus every declared RiskEvidence, CaptchaEvidence,
   capability, and OTP
   reservation before `Begin`. It locks command row, tenant, then one-time keys
   ordered as RiskEvidence, CaptchaEvidence, capability, and OTP; keys within a class sort
   lexicographically. After admission, the general `tx.uow.reserve` rule still
   rolls back every participant reservation on denial, error, or cancellation;
   a separate or private participant reservation is forbidden. When an expired
   `pending` command is taken over, this same
   transaction MUST first increment the command generation and then CAS-rebind
   every already-`reserved` one-time participant from the exact prior generation
   to the new generation under the same command ID and fingerprint. A missing,
   terminal, different-command, different-fingerprint, or non-prior-generation
   participant rolls back the entire takeover; the stale generation retains no
   apply/finalize authority.
3. **`tx.uow.begin`:** `Begin(ctx, reservation)` MUST open one bounded domain transaction, lock the
   reserved command row first, verify generation/lease/fingerprint, and use the
   primary database UTC clock.
4. **`tx.uow.contributor`:** Contributors MUST receive only transaction-scoped query/execute, command
   scope, database time, and outbox/effect writers. They MUST NOT commit,
   rollback, change isolation, open nested/private transactions, perform network
   I/O, or invoke unbounded or caller-controlled callbacks.
5. **`tx.uow.locks`:** Before the first mutation, contributors MUST publish all lock keys. The unit
   of work MUST reacquire locks in this order: command row; tenant; reserved
   RiskEvidence, CaptchaEvidence, capability, then OTP rows; primary aggregate type/ID; secondary
   aggregate type/ID; credential/session/grant ID; outbox sequence. Keys within
   a class sort lexicographically.
6. **`tx.uow.commit`:** `Commit(result)` MUST atomically write domain mutations, authority-version
   bumps, lifecycle events, audit/outbox/effect records, enlisted RiskEvidence,
   CaptchaEvidence, and capability finalization, and `pending` to `committed`, guarded by owner
   generation, before the single PostgreSQL commit.
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
   short transaction guarded by generation. That same terminalization MUST
   transition or reconcile every enlisted one-time RiskEvidence,
   CaptchaEvidence, capability, and OTP reservation to its legal non-commit
   terminal state; it MUST NOT leave a known-aborted command holding reusable or
   unresolved authority. Failure or ambiguity during
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

## One-use CAPTCHA evidence protocol

A protected action that risk policy conditions on CAPTCHA MUST use one typed
`CaptchaEvidenceContributor` implemented by `identity/risk/postgres`. A provider
adapter verifies the remote response and returns only normalized bounded
verification facts; it MUST NOT issue evidence or own durable state. The
`identity/risk` core decides issuance through the injected contributor, and
`tx.captcha.issue` durably inserts the opaque `CaptchaEvidence` reference and
bounded safe metadata in the same transaction that advances the command ledger
from `pending` to `committed` with the safe result. A separate evidence insert
is forbidden. `tx.captcha.reconcile` MUST classify the same command and
fingerprint as exactly `committed` with its recorded safe result, `aborted`
with proof no evidence row committed, or still `pending` and outcome-unknown.
An ambiguous insert MUST use `tx.captcha.reconcile` for
the same command and fingerprint on the primary and MUST NOT issue a second
reference. The durable evidence row MUST bind tenant, exact subject or
anonymous-flow ID, a flow-context variant of pre-auth transaction for
unauthenticated flows or authenticated subject/session or administrator actor
context for authenticated or administrative flows, exact registered action,
canonical request fingerprint, provider/site/configuration version, hostname,
decision, database-issued time, expiry, keyed-digest key version, and proof
fingerprint. Raw response tokens,
remote IP, provider payloads, scores, `cdata`, and provider error text MUST NOT
enter the command, journal result, audit event, or caller-visible result.

The CAPTCHA replay fingerprint MUST be HMAC-SHA-256 under
`secrets.captcha_replay_digest_key` over the ASCII domain
`identity-captcha-replay-v1`, one zero byte, then unsigned 32-bit big-endian
length-prefixed UTF-8 provider ID, site ID, API/profile ID and configuration
version fields in that order, followed by the unsigned 64-bit big-endian raw
provider-token length and raw provider-token bytes. `identity/risk` is the sole
derivation authority; callers and provider adapters MUST NOT supply or override
the fingerprint. A unique constraint on tenant, provider, site ID,
profile/configuration version, and replay fingerprint MUST give exactly one
issuance command authority. A collision with the same scope and fingerprint
replays only the original command/result; any different command remains denied
without revealing which field matched. Replay tombstones MUST survive evidence
payload erasure and unresolved commands have no time-based release. Terminal
tombstones and referenced digest-key versions remain through
`captcha.replay_tombstone_retention`; key retirement MUST NOT make an otherwise
replayable provider token admissible.

`CaptchaEvidence` states are exactly `issued`, `reserved`, `finalized`,
`released`, `expired`, and `revoked`. Only `issued` may become `reserved`;
`reserved` may become `finalized`, `released`, or `revoked`; untouched `issued`
may become `expired` or `revoked`; every other transition is forbidden. The
protected action MUST enlist the typed contributor before reservation. Its
command fingerprint MUST cover the opaque evidence reference and every bound
action/request/subject/version input. `tx.captcha.reserve` MUST lock the issued
row inside the coordinator's one `tx.uow.reserve` transaction and bind the
command ID, command fingerprint, owner generation, and target versions. A
precheck grants no authority, and the contributor MUST NOT open a private
transaction or call the provider.

`tx.captcha.apply` MUST recheck command/generation ownership, PostgreSQL expiry,
exact action, subject or anonymous flow, the applicable pre-auth or authenticated
subject/session or administrator actor context, request fingerprint,
provider/site/configuration versions, and the current risk-policy version inside
the domain transaction.
`tx.captcha.finalize` MUST transition the reservation to `finalized` in the
same commit as the protected mutation, authority-version changes, session
transition, audit/outbox records, and command result. Any reservation denial,
binding mismatch, expiry, replay by another command, cancellation, or
participant failure MUST leave the protected action uncommitted. An ambiguous
commit MUST leave the evidence `reserved`, return `Unknown`, and reconcile the
same command on the primary; timeout, lease loss, or evidence expiry MUST NOT
release or reassign it. Only authoritative proof that the owning command did
not commit MAY transition it to terminal `released`, after which retry requires
new evidence. Expired-owner takeover MUST CAS-rebind this contributor with
every other declared participant under the rule in `tx.uow.reserve`.

A same-command, same-fingerprint replay MUST return the recorded protected
action result without re-verifying the provider or consuming new evidence. A
different command MUST receive the same non-enumerating replay denial whether
the evidence is missing, terminal, bound elsewhere, or invalid. Cleanup MUST
retain unresolved reservations and, after terminal payload crypto-shredding,
retain the scoped keyed-digest/state tombstone until every referenced key is
retired and the bearer can no longer validate before lookup.

Evidence-producing `identity.risk.evaluate` MUST enlist
`identity/risk/postgres` in the identity command unit of work and atomically
commit `tx.risk_evidence.issue` with the command result before returning an
opaque reference. Issuance phase is a closed enum containing exactly `none`,
`phone-reset-initiation`, and `phone-reset-completion`; `none` cannot issue.
Phase `phone-reset-initiation` maps only to purpose
`phone-password-reset-initiate`; phase `phone-reset-completion` maps only to
purpose `phone-password-reset-complete`. Purpose MUST be derived exclusively
from those two issuing phases. An unsupported phase, a purpose paired with
`none`, or any caller-supplied purpose MUST fail before provider evaluation,
command reservation, or state access. The command fingerprint MUST cover the
phase, purpose, tenant, subject, recovery operation, canonical number,
pre-auth transaction, attempt ID, risk-policy version, and authoritative
server-resolved signal/provider inputs. Callers MUST NOT supply a decision,
purpose override, raw provider evidence, or a fabricated evidence result.
Denied and proved pre-commit failure return no reference. An ambiguous commit
returns `Unknown` without a reference and requires primary-authority recovery
of the same command; it MUST NOT rerun providers or mint a replacement. A
matching committed replay returns the exact recorded opaque reference and safe
purpose/issued-at/expires-at/one-use metadata. No raw signal, provider evidence,
decision internals, embedded evidence payload, keyed digest, signature, journal identifier,
or persistence record may cross the operation result.

## One-use RiskEvidence protocol

Phone password recovery and every future one-use RiskEvidence profile MUST use
the durable journal owned by `identity/risk/postgres`. An immutable or signed
bearer without its authoritative journal row grants no authority.

Phone reset initiation and completion are separate one-use profiles.
Initiation and completion MUST use separate RiskEvidence references, keyed
digests, reservations, and terminal records with purposes
`phone-password-reset-initiate` and `phone-password-reset-complete`,
respectively. Neither phase's RiskEvidence MAY validate, reserve, replay, or
substitute for the other phase. A caller therefore obtains fresh evidence for
each phase even when both phases share the same tenant, subject, canonical
number, pre-auth transaction, attempt, or policy version.

1. **`tx.risk_evidence.issue`: Issue:** persist an `issued` row keyed by tenant,
   purpose, and versioned keyed evidence digest. Bind subject, recovery
   operation, canonical number digest, pre-auth transaction, attempt ID,
   risk-policy and provider/configuration versions, decision, database-issued
   time, expiry, evidence-verification key version, keyed-digest key version,
   and proof fingerprint before returning the opaque reference.
2. **`tx.risk_evidence.reserve`: Reserve:** verify the immutable evidence locally
   and use the predeclared `identity/risk/postgres` reservation contributor to
   lock the existing row and bind command ID, command fingerprint, reservation
   generation, and target versions. `tx.risk_evidence.reserve` MUST run only in
   the coordinator's single `tx.uow.reserve` transaction that atomically
   reserves the command and the exact one-time participants declared by the
   operation profile; it MUST NOT open a separate or private transaction. Phone
   reset initiation reserves only the command and initiation RiskEvidence,
   because its OTP challenge and capability do not exist until the domain
   commit. Phone reset completion reserves the command, completion RiskEvidence,
   existing capability, and existing OTP together. A read-only precheck grants no authority;
   command acceptance
   requires `tx.risk_evidence.reserve`. Reserve MUST NOT insert a missing row or
   reserve a terminal row. The same command, fingerprint, and live generation
   replay the stable reservation; a takeover generation obtains it only through
   the guarded all-participant CAS in `tx.uow.reserve`. A stale generation and a
   different command never obtain apply/finalize authority. Two commands MAY precheck
   the same item concurrently, but the PostgreSQL row lock and unique keyed
   digest MUST give exactly one command the `issued` to `reserved` transition;
   every different command receives the same non-enumerating replay denial.
3. **`tx.risk_evidence.apply`: Apply:** inside the domain transaction, lock the
   reserved row and recheck command/generation ownership, database expiry,
   exact binding, risk-policy/provider versions, decision, and current
   authoritative counters. It MUST perform no provider or caller callback.
4. **`tx.risk_evidence.finalize`: Finalize:** transition `reserved` to
   `finalized` in the domain commit that records the recovery result.
5. **`tx.risk_evidence.release`: Release:** only after authoritative proof that
   the owning command did not commit, transition `reserved` to terminal
   `released`; released evidence is never eligible for another reservation.
6. **`tx.risk_evidence.recover`: Recover:** query the primary authority by the
   same tenant, purpose, digest, and caller scope, reconcile the owning command,
   and return only a stable redacted classification. It MUST NOT infer rollback
   from timeout, disconnect, lease expiry, or evidence expiry.

RiskEvidence states are `issued`, `reserved`, `finalized`, `released`, `expired`,
and `revoked`. Legal transitions are only `absent` to `issued`, `issued` to
`reserved`, `issued` to `expired` or `revoked`, `reserved` to `finalized`, and
`reserved` to `released` or `revoked`. `finalized`, `released`,
`expired`, and `revoked` are terminal and MUST NOT return to `issued` or
`reserved`.

The phone-recovery completion command MUST enlist `identity/risk/postgres`,
`identity/otp/postgres`, `capability/postgres`, `identity/password/postgres`, and
`identity/session/postgres` in one unit of work before the reservation
transaction's first write. The completion reservation transaction MUST transition the
RiskEvidence, purpose-bound OTP, and reset capability together or none of them;
the later domain commit MUST finalize the RiskEvidence reservation, purpose-bound OTP, reset
capability, password mutation, session invalidation, outbox/audit records, and
command result, or commit none of them. A failure to reserve any participant
MUST leave credential and session state unchanged and release another
reservation only after authoritative proof that the command did not commit.

Phone password-reset initiation MUST use one initiation command and coordinator
unit of work to reserve, apply, and finalize initiation RiskEvidence. The
initiation reservation MUST accept only purpose
`phone-password-reset-initiate`; a completion-purpose artifact receives the
same non-enumerating denial as any other binding mismatch. The initiation
domain commit MUST apply and finalize that initiation RiskEvidence in the same
commit that issues the purpose-bound OTP challenge, canonical reset capability,
outbox/audit records, and command result, or commit none of them. No challenge,
capability, or externally visible delivery effect may be published before that
commit. A same-command, same-fingerprint initiation replay MUST return the exact
recorded challenge and capability result without issuing replacements. Two
concurrent initiation commands MAY precheck the same evidence, but exactly one
MAY reserve it; the loser receives the stable non-enumerating replay denial.
An expired initiation-command takeover MUST CAS-rebind the initiation
RiskEvidence from the exact prior generation to the new generation before apply
or finalize authority is granted. Retry rollback, release, ambiguous outcome,
recovery, and terminal-state rules are the same rules defined above: rollback
does not release without authoritative non-commit proof, and unknown remains
reserved until authoritative recovery.
An initiation rollback MUST NOT release its RiskEvidence without authoritative
proof that the command did not commit. An ambiguous initiation outcome MUST
remain `reserved` until authoritative recovery resolves the owning command.

Phone password-reset completion MUST accept only purpose
`phone-password-reset-complete`. Its RiskEvidence is a fresh completion-only
artifact and is not the initiation artifact that authorized challenge and
capability issuance.

A retryable transaction rollback under the same live command reservation
retains the RiskEvidence reservation for that command; a different command
never takes it over. Expired-owner takeover MUST atomically transfer the command
and every reservation declared by the operation profile to one new generation
as defined by `tx.uow.reserve`: initiation transfers its RiskEvidence, while
completion transfers RiskEvidence, capability, and OTP. Partial generation
transfer is an unresolved `Unknown`, not authority to proceed. An ambiguous commit MUST leave the item `reserved`, return
`Unknown`, and use `tx.risk_evidence.recover`; expiry or lease timeout alone
MUST NOT release it. Only authoritative proof that the owning command did not
commit MAY transition `reserved` to `released`, after which that evidence
remains terminal and a retry requires newly issued evidence.

Cleanup MUST expire untouched `issued` rows in bounded database-time batches
and retain every `reserved` row through authoritative recovery. Only after the
later of original evidence expiry and the configured `command.result_retention`
deadline MAY cleanup crypto-shred terminal payload/linkage; it MUST preserve a
restricted tenant/purpose/keyed-digest/key-version/original-expiry/terminal-state
tombstone with no time-based deletion. The tombstone MAY be deleted only after
every evidence-verification and keyed-digest key version it references is
retired and proof shows every bearer fails cryptographic validation before
lookup. Cleanup MUST NOT delete an unresolved command binding, weaken
constant-work denial, or make a prior evidence reference eligible for reissue.
Cleanup and tombstones MUST independently preserve replay and recovery authority
for both phone-reset RiskEvidence purposes.

## Durable OTP participant protocol

Every OTP that authorizes an owning workflow mutation MUST use the authoritative
`identity/otp/postgres` participant. OTP participant states are `issued`,
`reserved`, `finalized`, `released`, `expired`, `revoked`, and `exhausted`.
Legal transitions are only `absent` to `issued`, `issued` to `issued` with one
durable failed-attempt increment, `issued` to `reserved`, `issued` to `expired`,
`revoked`, or `exhausted`, `reserved` to `finalized`, and `reserved` to
`released` or `revoked`. Every terminal state is ineligible for reservation.

1. **`tx.otp.issue`: Issue:** persist `issued` before delivery. The binding MUST
   include tenant, purpose, subject or channel target, challenge ID, workflow
   target, issued/expiry database time, attempt budget, keyed-code-digest
   version, and issuance fingerprint. Raw codes MUST never be stored. A
   replacement MAY transition an earlier `issued` row to `revoked` in the same
   issue transaction, but MUST NOT replace or revoke a `reserved` row without
   authoritative recovery of its owning command.
2. **`tx.otp.check`: Check:** a configured scanner-safe or user-initiated
   precheck MAY compare the exact binding and digest under a separately bounded
   attempt policy, but grants no authority and MUST NOT reserve or consume.
3. **`tx.otp.attempt`: Attempt:** before accepting a code-bearing verification
   submission, the server MUST issue one unpredictable attempt ID for that
   logical submission. `tx.otp.attempt` MUST use a server-issued attempt ID bound
   to the tenant, purpose, challenge ID, consuming command ID, and canonical
   command fingerprint; callers MUST NOT select or reuse an attempt ID across
   logical submissions. An incorrect code uses a dedicated atomic denial
   transaction. The wrong-code denial transaction is a narrow pre-reservation
   exception to `tx.uow.reserve`. It MUST lock the command row and OTP row,
   verify the server-issued attempt ID and canonical command fingerprint,
   increment the durable attempt counter exactly once, transition to
   `exhausted` when the budget is reached, and store the stable `aborted` command
   result in the same commit. Replaying the same attempt ID returns that result
   without another increment; a different attempt ID or fingerprint for the same
   command conflicts without mutation. An ambiguous wrong-code denial commit
   MUST return `Unknown` and reconcile the same command and attempt ID on the
   primary before any retry. Once code verification admits the command, normal
   reservation, apply, finalize, rollback, and recovery rules apply without this
   exception. Cross-purpose, cross-subject, cross-channel, or unknown challenges
   receive the same constant-work denial and MUST NOT mutate another row.
4. **`tx.otp.reserve`: Reserve:** the predeclared `identity/otp/postgres`
   contributor MUST lock the exact `issued` row and verify the code digest inside
   the coordinator's single `tx.uow.reserve` transaction, then bind consuming
   command ID, command fingerprint, reservation generation, and target versions.
   Two commands MAY
   perform a non-authoritative digest precheck, but exactly one command MAY
   transition the same `issued` row to `reserved`; every other command receives
   the same non-enumerating denial. The same command, fingerprint, and live
   generation replay the stable reservation without decrementing attempts or
   rerunning the workflow.
5. **`tx.otp.apply`: Apply:** inside the domain transaction recheck reservation
   generation, database expiry, purpose, subject/channel, challenge, workflow
   target, attempt state, and digest-key version without another code compare.
6. **`tx.otp.finalize`: Finalize:** transition `reserved` to `finalized` in the
   same commit as the owning mutation, session issuance or invalidation,
   outbox/audit records, other one-time finalizations, and command result.
7. **`tx.otp.release`: Release:** only authoritative proof that the owning
   command did not commit MAY transition `reserved` to terminal `released`; a
   retry then requires a newly issued OTP.
8. **`tx.otp.recover`: Recover:** query the primary authority and owning command
   under the same tenant/purpose/challenge/caller authorization, return a stable
   redacted classification, and never infer rollback from timeout or expiry.

Expired-owner takeover MUST CAS-rebind the OTP reservation from the exact prior
generation to the new command generation in the coordinator reservation
transaction with every other reserved one-time participant. Any missing,
terminal, mismatched-command/fingerprint, or non-prior-generation participant
rolls back the complete takeover; the stale generation has no apply/finalize
authority. `tx.otp.apply` MUST recheck reservation generation, purpose and all
bound versions inside the domain transaction; `tx.otp.finalize` MUST transition
`reserved` to `finalized` in the same commit as the owning mutation, session
issuance or invalidation, outbox/audit, and command result.

A retryable rollback retains `reserved` only for the same live command
generation; authoritative non-commit MAY use `tx.otp.release`, which is terminal
and requires a newly issued OTP. An ambiguous commit MUST leave OTP `reserved`,
return `Unknown`, and use `tx.otp.recover`; timeout, lease loss, challenge expiry,
or cleanup MUST NOT release it.

After the later of original OTP expiry and `command.result_retention`, cleanup
MAY crypto-shred terminal payload/linkage but MUST retain a
tenant/purpose/keyed-digest/key-version/original-expiry/terminal-state tombstone
with no time-based deletion until every referenced digest key is retired and no
code can validate before lookup. Untouched `issued` rows expire in bounded
database-time batches; unresolved `reserved` rows are excluded from cleanup.

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

Every issuer, validator, repository and reference composition MUST consume
`struct:ref.capability.crypto`. Capability signing/verification keys and keyed
replay-digest keys are independent, purpose/domain-separated versioned key
sets. New issuance uses only the newest active versions; validation and lookup
accept only the explicitly retained versions. Startup/readiness MUST fail when
an active key or any key still required by an unexpired bearer, pending/unknown
reservation, terminal record, or replay tombstone is unavailable. Retirement
is permitted only after the closed configuration predicate proves that bearer
validation fails before lookup and no unresolved authority references the key.

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

## Composed lifecycle and reconciliation commands

Email-address removal and administrative MFA reset are coordinator commands,
not sequential service calls. Before their first write they MUST enlist the
identity/email or identity/mfa authority, identity/session when authority is
invalidated, capability state for address/recovery bearers, audit, outbox and
the command-result journal. The same commit changes the identifier/factor
version, writes the lifecycle event and command result, and applies every local
invalidation. Unknown commit remains generation-reserved until authoritative
query/recovery; retry MUST NOT remove another address/factor or issue a second
administrative recovery capability.

Administrative MFA recovery issuance is a separate command whose input binds a
terminal `factor.reset.v1` cascade generation. It may enqueue one encrypted
single-use recovery capability only after that generation committed; it cannot
participate in, race ahead of or reinterpret the reset transaction.

Provider delivery receipts, cancellation and status reconciliation use the
provider-effect state machine above. A receipt or status query runs outside
database transactions, authenticates provider evidence, then applies one
generation-CAS transition with the dedupe identity and reconciliation
checkpoint. Local cancellation prevents new leases immediately, but remote
cancellation remains `outcome-unknown` until authenticated provider evidence
proves a terminal outcome. No status query, webhook replay or worker takeover
may resubmit a delivery unless the persisted provider idempotency identity and
pinned provider contract permit it.

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
