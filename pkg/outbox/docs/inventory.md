# Hardening Inventory

This inventory is the release-candidate surface audited by the hardening
matrix. `go doc -all` remains the symbol-level source of truth; this document
groups every exported contract with its persistent or operational effect.

## Core API

| Contract | Exported surface |
|---|---|
| Envelope limits | `Limits` with `MaxIDBytes`, `MaxTopicBytes`, `MaxPayloadBytes`, `MaxMetadataEntries`, `MaxMetadataBytes`, `MaxOrderingKeyBytes`, and `MaxIdempotencyKeyBytes`; `Validate`; `DefaultLimits` |
| Envelope data | `Envelope` with `ID`, `Topic`, `Payload`, `PayloadVersion`, `Metadata`, `OrderingKey`, `IdempotencyKey`, `Attempts`, `AvailableAt`, and `CreatedAt`; `CanonicalJSON`; `ValidateForInsert` |
| Construction | `NewEnvelopeParams` with all caller fields; `EnvelopeBuilder`; `EnvelopeBuilderOption`; `NewEnvelopeBuilder`; `WithClock`; `WithIDGenerator`; `WithLimits`; `Build` |
| Envelope errors | `ErrIDRequired`, `ErrIDTooLarge`, `ErrTopicRequired`, `ErrTopicTooLarge`, `ErrPayloadTooLarge`, `ErrMetadataTooLarge`, `ErrMetadataEntriesTooLarge`, `ErrOrderingKeyTooLarge`, `ErrIdempotencyKeyTooLarge`, `ErrPayloadVersionRequired`, `ErrAttemptsInvalid`, `ErrAvailableAtRequired`, `ErrCreatedAtRequired`, `ErrTimestampOutOfRange`, `ErrInvalidLimits` |
| Observation | `Operation`, `Outcome`, `Event`, `BacklogStats`, `Observer`, `ObserverFunc`; `Observe` |
| Operations | `claim`, `publish`, `deliver`, `retry`, `dead_letter`, `release`, `extend_lease`, `replay`, `prune`, `archive` |
| Outcomes | `success`, `failure` |
| Event fields | operation, outcome, count, message ID, topic, attempts, duration; never payload, metadata, or error text |

Default envelope ceilings are 255-byte IDs/topics/ordering/idempotency keys,
1 MiB payloads, 64 metadata entries, 16 KiB combined metadata key/value bytes,
and payload version 1 when omitted.

## PostgreSQL writer and migrations

| Contract | Exported surface |
|---|---|
| Migration source | `Migrations`, returning canonical `000001_create_outbox.sql` |
| Writer configuration | `WriterConfig` with schema, table, limits, and maximum batch size |
| Writer | `Writer`; `NewWriter`; `Insert`; `InsertBatch` |
| Writer errors | `ErrEmptyBatch`, `ErrBatchTooLarge`, `ErrInvalidBatchLimit`, `ErrTransactionRequired`, plus core envelope errors and `ErrValueOutsideBounds` |

The writer accepts only caller-owned `pgx.Tx`. It never begins, commits, or
rolls back. Default insert batches are 100 and the absolute parameter-derived
maximum is 6553 envelopes.

## PostgreSQL store API

| Contract | Exported surface |
|---|---|
| Configuration | `StoreConfig`: schema, table, maximum claim/admin batches, maximum lease duration, token generator, replay authorizer, observer, clock |
| Claiming | `SerializationMode` (`SerializeNone`, `SerializeByOrderingKey`, `SerializeByTopic`); `ClaimRequest`; `Claim`; `LeaseRef`; `Claim` |
| Lease transitions | `ExtendLease`, `MarkDelivered`, `Retry`, `DeadLetter`, `ReleaseLease` |
| Replay | `ReplayRequest`; `ReplayAuthorizer`; `ReplayAuthorizeFunc`; `Replay` |
| Inspection | `MessageState` and its four values; `InspectRequest`; `MessageSummary`; `Inspect`; `Backlog`; `Ping` |
| Retention | `DeliveredMessage`; `DeadMessage`; `Archiver`; `ArchiveFunc`; `DeadArchiver`; `DeadArchiveFunc`; `PruneDelivered`; `PruneDead`; `ArchiveAndPruneDelivered`; `ArchiveAndPruneDead` |
| Store construction | `Store`; `NewStore`; `BacklogStats` alias |
| Store errors | `ErrPoolRequired`, `ErrNotWritable`, `ErrValueOutsideBounds`, `ErrClaimOwnerRequired`, `ErrInvalidClaimLimit`, `ErrInvalidLeaseDuration`, `ErrInvalidRetryDelay`, `ErrLeaseLost`, `ErrInvalidAdminLimit`, `ErrReplayIDsRequired`, `ErrReplayRequestedBy`, `ErrReplayReasonRequired`, `ErrReplayDuplicateID`, `ErrReplayUnauthorized`, `ErrReplayConflict`, `ErrPruneCutoffRequired`, `ErrArchiverRequired`, `ErrArchiverPanic`, `ErrInvalidMessageState`, `ErrInvalidSerialization` |

Default claim and administrative batches are 100 and the absolute ceiling is
1,000. Default maximum lease duration is five minutes and the absolute ceiling
is 24 hours. Replay is denied unless a configured authorizer approves it.
Lease and audit identities are at most 255 bytes; replay reasons and stored
failure diagnostics are at most 4096 bytes.
The store clock affects observation latency only; panic containment substitutes
zero time and never changes a database transition.

## Relay API and timers

| Contract | Exported surface |
|---|---|
| Dependencies | `Publisher`; `Store` |
| Failure policy | `ErrorClass`; `ErrorTransient`; `ErrorPermanent` |
| Configuration | `Config`: owner, batch size, workers, lease duration, renewal interval, maximum attempts, poll interval, transition timeout, clock, backoff, classifier, wait function, serialization, observer, logger, heartbeat |
| Runtime | `Relay`; `New`; `Run`; `RunOnce`; `Readiness`; `Result` with claimed, published, delivered, retried, dead-lettered, and released counts |
| Errors | `ErrStoreRequired`, `ErrPublisherRequired`, `ErrPublisherPanic`, `ErrClassifierPanic`, `ErrInvalidErrorClass`, `ErrBackoffPanic`, `ErrInvalidBackoff`, `ErrHeartbeatPanic`, `ErrClaimBatchOverflow`, `ErrOwnerRequired`, `ErrInvalidConfig` |

Defaults are batch 100, `runtime.NumCPU()` workers, 30-second leases,
half-lease renewal, 10 attempts, one-second polling, five-second detached
transition cleanup, and at most one minute of retry backoff. A full batch is
repolled immediately; a partial batch waits once. Absolute ceilings are 1,000
records, 256 workers, 10,000 attempts, and a 24-hour lease.
The relay clock affects observation latency only; panic containment substitutes
zero time and never changes a relay transition.

## Adapter APIs and metrics

| Module | Exported surface |
|---|---|
| `adapters/goqueue` | `Queue`; `Publisher`; `New`; `Publish`; `ErrQueueRequired` |
| `adapters/gotelemetry` | `Runtime`; `Publisher`; `Telemetry`; `New`; `Inject`; `Observe`; `RecordBacklog`; `WrapPublisher`; `ErrRuntimeRequired`; `ErrPublisherRequired` |

Telemetry instruments `outbox.operations`, `outbox.operation.duration`,
`outbox.backlog.depth`, and `outbox.backlog.oldest_pending_age`. Metric
attributes are only operation, outcome, and backlog state. Publish spans add
destination, message ID, and attempt count; no payload or error text is added.

## Persistent schema

### `outbox_messages`

Columns are `id`, `topic`, `payload`, `payload_version`, `metadata`,
`ordering_key`, `idempotency_key`, `attempts`, `available_at`, `created_at`,
`updated_at`, `state`, `lease_owner`, `lease_token`, `leased_until`,
`delivered_at`, `dead_lettered_at`, and `last_error`.

States and field invariants are:

| State | Required fields | Forbidden fields |
|---|---|---|
| `pending` | availability and creation | all lease and terminal timestamps |
| `leased` | owner, token, lease deadline | delivered and dead timestamps |
| `delivered` | delivered timestamp | lease fields and dead timestamp |
| `dead` | dead timestamp | lease fields and delivered timestamp |

Indexes are the primary key; partial claim, lease-expiry, ordering,
delivered-retention, dead-retention, and non-empty idempotency indexes.
Constraints cover state shape, attempts from 0 through 10,000, versions 1
through 65535, object metadata with string-only values, the absolute field
ceilings, finite envelope-range timestamps, and non-empty bounded identity.

### `outbox_replay_audit`

Columns are `replay_id`, `message_id`, `previous_state`, `requested_by`,
`reason`, `requested_at`, and `available_at`. The composite primary key makes
one replay action/message pair immutable. Previous state is `delivered` or
`dead`; identity, requester, and reason lengths are constrained. The secondary
index is `(requested_at, replay_id)`. Both audit timestamps must be finite and
within envelope years 0000–9999.

## Query inventory

| Query | Lock or transaction behavior | Result |
|---|---|---|
| Writable readiness | no mutation | session is read-write and server is not in recovery |
| Backlog | snapshot aggregate | pending, leased, dead, oldest pending |
| Inspect | bounded ordered read | payload-free summaries |
| Claim | CTE `FOR UPDATE SKIP LOCKED` plus update | disjoint generation-token leases |
| Mark delivered | token-qualified update | terminal delivered state |
| Extend lease | token-qualified update | PostgreSQL-clock deadline |
| Retry | token-qualified update with PostgreSQL-relative duration | pending with bounded delay and safe diagnostic |
| Dead-letter | token-qualified update | terminal dead state with safe diagnostic |
| Release | token-qualified update | immediately pending |
| Replay | range preflight, default-deny authorization, explicit transaction, row locks | approved terminal selection reset plus immutable audit |
| Prune delivered/dead | CTE row locks and bounded delete | terminal IDs deleted |
| Archive delivered/dead | explicit transaction, row locks, callback, delete | terminal IDs deleted only after successful hook |
| Writer insert | caller transaction, one multi-row statement | all envelopes inserted or none |

Every lease deadline and durable transition timestamp uses PostgreSQL
`clock_timestamp()`. Retry policy chooses a bounded duration and PostgreSQL
owns its absolute schedule. Relay host time is used only for polling, metrics,
and test injection; it never proves lease ownership or retry eligibility.
