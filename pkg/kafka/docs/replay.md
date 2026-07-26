# Replay

`ReplayReader` takes explicit topic, partition, inclusive start, and exclusive
end offsets. It directly assigns partitions, does not join a group, and does
not read or alter committed group offsets.

## Planning and authorization

`Plan` is a local dry run. It returns an owned range list, each effective next
offset, and the exact aggregate remaining offset span after applying
`ReplayConfig.Checkpoint`. It performs no broker request, so it does not prove
that retention still contains the range. Before the first handler call,
`Replay` lists current broker log-start and high-watermark offsets under
`PlanningTimeout`. It rejects an effective next offset before the log start or
an exclusive end after the high watermark. Broker-validated dry-run and
timestamp planning remain pre-v1 work.

Handler execution is disabled by default. Applications must set
`SideEffects: ReplaySideEffectsAllowed` after applying their own authorization,
schema, and operational review. This is an execution opt-in, not a claim that
the handler is idempotent or safe.

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
an out-of-range start that changes before or during consumption. A range whose
high watermark changes does not make an already validated exclusive end
unsafe: newly appended records are outside the requested end and are skipped.

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
borrowed for the callback and must be retained before escape. Context
cancellation and handler expiry override a nil callback result, leave the
current offset unprocessed, and return it in the checkpoint.

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
