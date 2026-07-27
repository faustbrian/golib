# Replay

`ReplayReader` takes explicit topic, partition, inclusive start, and exclusive
end offsets. It directly assigns partitions, does not join a group, and does
not read or alter committed group offsets.

## Planning and authorization

`Plan` is a local dry run. It returns an owned range list, each effective next
offset, and the exact aggregate remaining offset span after applying
`ReplayConfig.Checkpoint`. It performs no broker request, so it does not prove
that retention still contains the range. `PlanAgainstBroker` returns the same
owned plan after listing current broker log-start and high-watermark offsets
under `PlanningTimeout`. It rejects an effective next offset before the log
start or an exclusive end after the high watermark without polling records,
invoking a handler, changing group offsets, or consuming the reader's single
replay execution. Any error returns a zero plan; use `Plan` explicitly when an
unvalidated local plan is wanted. Broker planning participates in the reader
lifecycle: replay or another broker plan excludes it, and shutdown waits for
it. `Replay` repeats the same validation before the first handler call.

`Inspector.PlanReplayByTimestamp` resolves one inclusive-start,
exclusive-end timestamp window for 1 through 1,024 explicit partitions across
at most 64 topics. Both boundaries must be non-negative, exact Kafka
milliseconds. Four bounded exact-partition `ListOffsets` requests resolve the
current log starts, high watermarks, start offsets, and end offsets. The method
polls no records, joins no group, changes no offsets, invokes no handler, and
returns a zero plan on every error.

The returned partitions are sorted and owned. `ReplayRanges` returns new exact
offset ranges and omits empty partition windows. A timestamp before deleted
history can resolve to the current log start; when that makes the requested
start ambiguous, planning fails with `ErrReplayTimestampRangeIncomplete`
instead of silently presenting retained history as complete. Broker movement
after planning remains possible, so execution performs its normal boundary
preflight.

Kafka timestamp lookup chooses partition offsets; it does not establish global
order across partitions or guarantee that every record inside the resulting
offset span has a timestamp inside the requested window. Producer timestamps
can be non-monotonic. Applications must retain the original timestamp request
and resolved offset plan in their replay audit record.

Handler execution is disabled by default. Applications must set
`SideEffects: ReplaySideEffectsAllowed` after applying their own authorization,
schema, and operational review. This is an execution opt-in, not a claim that
the handler is idempotent or safe.

`Replay` accepts a dedicated `ReplayHandler`, commonly adapted with
`ReplayHandlerFunc`. Every callback receives a `ReplayRecord` containing the
borrowed consumed record plus `ReplayMetadata`. The metadata repeats the
complete requested range and its checkpoint-derived effective inclusive start.
This distinguishes replay callbacks from consumer-group delivery and lets an
application propagate reviewed source-range and resume provenance. It does not
provide an application replay identifier, authorization decision, or audit
record; those remain application-owned.

## Progress and resume

`ReplayResult` reports polled, processed, skipped, and failed record counts,
completed and incomplete range counts, and one stable `ReplayRangeResult` for
every configured range. Counts are 64-bit. `Checkpoint` returns owned
topic-partition next offsets in configured range order.

Persist the checkpoint in an application-owned durable store only after
application side effects meet that application's recovery contract. To resume,
construct a new reader with that checkpoint. Missing positions restart their
configured ranges; unknown, duplicate, before-start, and after-end positions
are rejected before a Kafka client is allocated. This package does not store,
commit, reset, or delete replay or consumer-group offsets.

Optional `ReplayConfig.Observers` report broker-validated plan completion,
each processed, skipped, or failed record, the exact returned aggregate
progress, bounded shutdown, and replay-client broker activity. Record events
contain only validated Kafka coordinates and conservative byte counts, never
keys, values, or headers. Parallel partition handlers can invoke the same
observer concurrently. `ReplayConfig.Validate` checks and copies the observer
policy without constructing a client. Replay operations and `Close` fail with
`ErrObserverReentry` while a same-reader callback is active.

## Partition concurrency

Replay is sequential by default. `MaxConcurrentFetches` bounds franz-go broker
fetch requests and `MaxConcurrentHandlers` bounds application callbacks; both
accept 1 through 64 and default to one. They are independent limits.

When handler concurrency exceeds one, a bounded poll is grouped by
topic-partition in first-seen order. A fixed worker set processes those
partition batches concurrently, while records in each partition remain
strictly ascending and sequential. There is no global order across partitions.
The handler must be concurrency-safe.

Every partition batch returned by that poll is admitted as one bounded unit.
If one partition fails, other admitted partitions finish. Replay joins their
errors in stable batch order and returns exact progress for every partition;
the failing partition never advances past its failed record, while successful
independent partitions remain resumable from their later checkpoints.
Cancellation reaches all active callbacks and prevents a queued partition from
starting another callback. Callback cancellation remains cooperative. A
backend result exceeding `MaxPollRecords` is rejected before partition grouping
or handler admission.

## Failure and retention

Every expected offset must be present in ascending partition order. franz-go
clamps an initially assigned exact offset to the nearest broker boundary even
when later resets are disabled. The package therefore validates both range
boundaries through Kafka before invoking a handler, then uses franz-go's
no-reset policy for later retention movement. An unavailable boundary lookup
returns `ErrReplayBoundsUnavailable`; a range outside the returned bounds or a
later Kafka `OFFSET_OUT_OF_RANGE` returns `ErrReplayOffsetOutOfRange`. A gap,
unexpected partition, record-limit failure, fetch failure, handler failure,
panic, timeout, or cancellation stops replay and returns the exact incomplete
checkpoint.

An offset range can remain inside the broker boundaries while compaction has
removed every record available from the effective next offset.
`ProgressTimeout` bounds repeated empty or skipped fetches; expiry returns
`ErrReplayStalled` with the unchanged checkpoint rather than polling forever.

Retention may move after the boundary lookup. The no-reset fetch policy catches
an out-of-range start that changes before or during consumption. An increase in
the high watermark does not make an already validated exclusive end unsafe: newly
appended records are outside the requested end and are skipped. Truncation
below the requested range still fails through bounds, no-reset, or gap
detection.

Already-buffered offsets before an explicit resume position are counted as
skipped. Records beyond a completed end can also be observed from the final
bounded fetch and are counted as skipped; the partition is then paused while
other ranges finish.

Kafka deletion retention can advance a partition's beginning offset. Kafka log
compaction can remove records while preserving their offset positions and
order. The current reader cannot distinguish a compacted missing record from
another gap without broker topic configuration, so both fail closed as
`ErrReplayOffsetGap`. Operators must diagnose retention, compaction,
truncation, corruption, or an incorrect range rather than approving a silent
skip.

## Lifecycle and ownership

Each reader is single-use. A concurrent call returns `ErrReplayBusy`; any later
call returns `ErrReplayAlreadyRun`, including after failure. Resume by
constructing a new reader with the returned checkpoint. The lifecycle lock is
not held across application callbacks, polling, or close. Handler bytes are
borrowed for the callback and must be retained with `ReplayRecord.Retain` before
escape. Replay metadata contains values only and remains safe to copy. Context
cancellation and handler expiry override a nil callback result, leave the
current offset unprocessed, and return it in the checkpoint. With parallel
execution, a canceled queued partition is returned unchanged without invoking
its handler.

`Shutdown` fences new replay calls, waits for the active call, and closes the
direct Kafka client. A deadline leaves shutdown fenced and retriable.
`Close` uses `ReplayConfig.ShutdownTimeout` and returns the shutdown error.

Replay publication uses an ordinary producer and the original deterministic
partition key. Applications remain responsible for authorization, dry-run
review, range digests, schema compatibility, idempotency, quarantine policy,
checkpoint durability, and immutable audit records. Replay does not provide
global order across partitions or exactly-once application side effects.

## Primary contracts

- [Apache Kafka 4.3 log implementation](https://kafka.apache.org/43/implementation/log/)
  defines monotonically increasing partition offsets, out-of-range behavior,
  and deletion retention.
- [Apache Kafka 4.3 topic configuration](https://kafka.apache.org/43/configuration/topic-configs/)
  defines deletion, compaction, and tombstone-retention policy.
- [franz-go v1.21.5 `kgo` documentation](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo@v1.21.5)
  defines direct partition assignment, initial exact-offset boundary clamping,
  no-reset behavior after assignment, bounded polling, and local partition
  pause behavior.
- [franz-go kadm v1.18.0](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kadm@v1.18.0)
  defines log-start and high-watermark offset inspection.
