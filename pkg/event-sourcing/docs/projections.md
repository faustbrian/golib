# Projection and replay foundations

Projections and replay are optional consumers of persisted event history. The
core event store remains usable without CQRS, a read model, or a global event
index.

## Optional global reads

`GlobalReader` is a capability separate from `EventStore`. A store implements
it only when it can provide one stable total order across streams. Callers can
discover support with an ordinary interface assertion:

```go
reader, ok := store.(eventsourcing.GlobalReader)
if !ok {
	return eventsourcing.ErrUnsupportedCapability
}
```

`ReadGlobalOptions` requires a one-based inclusive `FromPosition`, an optional
inclusive `ToPosition`, and a non-zero `Limit` no greater than
`MaxReadMessages`. A start beyond the current end returns an empty iterator;
it is not an error and does not imply that the store will remain empty.

Every returned message has a store-assigned `GlobalPosition`. The iterator is
caller-closed, cancellation-aware, and follows the same ownership rules as a
stream iterator. It is a snapshot of the selected range: appends after the
read begins are observed only by a later read.

The in-memory store implements `GlobalReader` using the exact positions
assigned during atomic append. Its process-local ordering and concurrency
behavior match the durable contract, but it provides no cross-process
durability.

## Projection runner and checkpoints

`projection.Runner` loads durable progress, requests the next bounded global
range, creates only `DeliveryReplay` values, invokes one application handler,
and advances the checkpoint after each successful call. It starts no
goroutines and processes one batch per explicit `RunBatch` call.

The [runnable replay example](../replay_example_test.go) persists one aggregate
event, consumes it through the global reader, checkpoints it, and proves that
the next bounded call resumes after durable progress.

`CheckpointStore.Save` is an optimistic compare-and-swap operation. A zero
expected position creates the first checkpoint; stale writers return
`projection.ErrCheckpointConflict`. `CheckpointStore.Status` atomically
returns the run state and optional checkpoint used to start one batch. A
missing direct `memory.ProjectionStore.Load` is explicit through
`projection.ErrCheckpointNotFound`; a successful load never returns zero.

Before declaring a checkpointed projection terminal, the runner performs one
bounded exact-position read to prove that the durable checkpoint still exists
in authoritative history. A restored or truncated event store behind the
checkpoint fails closed with `projection.ErrCheckpointAheadOfHistory`, which
also matches `projection.ErrCheckpointCorrupt`; the after-replay hook does not
run. Operators must reconcile or reset the derived checkpoint against the
authoritative restored history before resuming. Cancellation at this terminal
boundary is returned even when no after-replay hook is configured.

`BatchResult` distinguishes successful handler calls from successfully saved
checkpoints. `Scanned` includes every message examined, `Filtered` includes
messages deliberately omitted from handling, and `Checkpointed` includes both
handled and filtered messages whose progress was durably saved. If handling
succeeds and checkpoint saving fails, `Handled` is greater than
`Checkpointed`. The same delivery can then be observed again, so projection
handlers must be idempotent. The runner test suite exercises that failure and
retry through an application handler keyed by message ID: the handler is
called twice, its state transition occurs once, and the successful retry
advances the checkpoint. Its process-local set proves the runner contract, not
crash-safe deduplication. Production idempotency state must be durable and,
where supported, commit with the read-model update and checkpoint.

## Poisoned deliveries

A handler failure stops replay without advancing the checkpoint by default.
This fail-closed behavior means the same message is retried by a later batch
after the application repairs the handler, data, or dependency.

Applications may configure a `projection.PoisonPolicy` to inspect the immutable
replay delivery and preserved handler cause. `StopOnPoison` keeps the safe
default. `SkipPoison` explicitly checkpoints the failed message and continues:

```go
config.PoisonPolicy = func(
	ctx context.Context,
	poisoned projection.PoisonedDelivery,
) (projection.PoisonDecision, error) {
	if errors.Is(poisoned.Cause(), errRetiredProjectionInput) {
		return projection.SkipPoison, nil
	}

	return projection.StopOnPoison, nil
}
```

Skipping can permanently omit a read-model transition. A policy must therefore
be deterministic, narrowly reviewed, and free of irreversible side effects.
It runs after the handler but before checkpoint persistence; even a skip
decision can be retried when checkpoint saving fails. `BatchResult.Skipped`
counts only failed messages whose skip was durably checkpointed.
`PoisonSkipCheckpointError` preserves both the redacted handler failure and
checkpoint failure when that final save is rejected.

Cancellation prevents policy invocation. Policy failures, unknown decisions,
and contained panics stop replay without checkpointing. Diagnostics redact
application errors and panic values while preserving error causes for
`errors.Is` and `errors.As`. Dead-letter publication is an application or
adapter operation and is not hidden inside the generic runner.

## Replay lifecycle hooks

`RunnerConfig.BeforeReplay` and `RunnerConfig.AfterReplay` provide explicit,
optional application hooks corresponding to EventSauce's replay lifecycle.
The before hook runs before the first read when no checkpoint exists. The
after hook runs only after a terminal batch scans no messages, or immediately
at the maximum global position.

The runner closes the terminal iterator and checks its error before invoking
the after hook. A reader or close failure therefore cannot be mistaken for
successful replay completion. Hook failures and panics stop the call through a
redacted `ReplayHookError`; the phase and cause remain available through
`errors.As` and `errors.Is`.

Hooks carry no hidden durable lifecycle state. A failed first batch can invoke
the before hook again. An empty history has no checkpoint, so a repeated
terminal probe can invoke both hooks again; a completed history with a
checkpoint repeats only the after hook. Both hooks must be idempotent. They are
not transactions, dead-letter publishers, process-manager triggers, or
permission to execute replay side effects.

## Replay filters

`projection.ReplayFilter` supports bounded exact allowlists for streams,
aggregate types, and event names, plus inclusive global-position and
recording-time ranges. All configured dimensions are conjunctive; values
within one allowlist are alternatives. An empty constructed filter matches
every assigned persisted message, while its zero value is invalid.

The constructor rejects invalid or duplicate identities, reversed ranges, and
more than `MaxReplayFilterValues` combined allowlist values. It copies every
allowlist and normalizes time bounds to the envelope's UTC microsecond
precision. Matching is deterministic and performs no I/O.

A runner checkpoints filtered messages without invoking the handler. This is
required for forward progress: a rejected message cannot trap every later
batch behind the same checkpoint. `BatchSize` bounds scanned messages rather
than handler calls, so highly selective filters do not create unbounded work.

Changing a filter does not revisit messages below the durable checkpoint. A
filter change that must backfill earlier history requires an explicitly
coordinated read-model reset and checkpoint reset. Applications should treat
the projection name and filter configuration as one operational identity.

## Replay authorization and auditing

Every `RunnerConfig` requires a `ReplayGuard`. The runner invokes it before
every initial, resumed, and terminal batch, and before any replay hook, history
read, handler, poison policy, or checkpoint mutation. `ReplayAttempt` exposes
only the stable projection name, current optional checkpoint, and bounded batch
size. This lets an application authorize the operation and durably record its
own operator, reason, approval, and request identity without exposing event
payloads.

A guard rejection stops the batch without reading or handling history. Guard
errors and panics return a redacted `ReplayGuardError`; causes remain available
through `errors.Is` and `errors.As`, while panic values are discarded. If a
guard cancels the context, cancellation is observed before any later callback
or I/O.

The guard can be invoked repeatedly after failures and for repeated terminal
probes, so authorization and audit writes must be idempotent. Applications that
enforce both concerns outside the runner must still opt in explicitly with
`projection.PermitReplay`. That function is not an authorization mechanism; it
only records the caller's deliberate decision to own the boundary elsewhere.

## Operational control

`projection.Controller` binds a canonical projection name to a
`ControlStore`. `Status` reports one atomic running-or-paused state and
optional checkpoint. `Pause` and `Resume` are idempotent. A runner observes
the same status before opening a global iterator and returns
`ErrProjectionPaused` without reading or handling when paused.

Pause prevents new batches and checkpoint advancement; it does not interrupt
a handler that already started. Operators must stop scheduling new calls and
drain application-owned in-flight calls before changing the read model.
Checkpoint stores reject an in-flight save after pause, so such a handler can
be retried after the operation is coordinated.

`ResetCheckpoint` requires the projection to be paused and uses an expected
position to reject a concurrent or stale reset. It resets only checkpoint
acceleration state. It never clears, migrates, or rebuilds an application read
model.

`Controller.Rebuild` provides the explicit non-transactional composition for
an application-owned read-model reset followed by expected checkpoint reset.
It requires the projection to already be paused, never resumes it, and returns
the last confirmed status on failure. The reset callback must be idempotent:
an error, panic, cancellation, or checkpoint conflict returns
`ErrRebuildPartial` because the library cannot know whether application state
committed. Callers must serialize rebuild, resume, and other operational
controls; the controller does not hold a lock across the application callback.
`RebuildError.Phase` distinguishes application reset from the following
checkpoint boundary so recovery tooling can select the correct repair.

When the read model and checkpoint cannot share a transaction, applications
must use this coordinated sequence:

1. pause and drain the runner;
2. call `Rebuild` with the paused status checkpoint and an idempotent reset;
3. resume bounded replay.

The projection must remain paused after any partial failure. Operators inspect
status and repair either side before resuming; the generic API does not infer
or roll back application state.

A durable adapter that can update the read model and checkpoint in one
transaction exposes that stronger operation separately. The PostgreSQL module
provides `TxCheckpointWriter.Stage` for this purpose. It accepts a caller-owned
`pgx.Tx`, never commits or rolls back, and deliberately does not implement
`CheckpointStore` because staged progress is not durable.

`memory.ProjectionStore` implements checkpoint and control contracts with one
mutex. It is suitable for tests and local development, provides no
cross-process durability, and must not be presented as an operational
checkpoint store.

The generic runner cannot make an application projection update and checkpoint
save atomic. `postgres.ProjectionStore` provides durable cross-process
checkpoint and control operations, while `postgres.TxCheckpointWriter` lets an
application update its PostgreSQL read model and stage the expected checkpoint
in the same caller-owned transaction. Other stores require idempotent updates
or an equivalent consumer-owned transactional composition.

Handler errors are redacted while preserving their cause for `errors.Is` and
`errors.As`. Handler panics are contained as `projection.ErrHandlerPanic`
without retaining the panic value. Missing, duplicated, or reordered global
positions fail as corrupt history before the handler runs.

## Replay safety

Global iteration alone performs no delivery and causes no side effects.
Callers must construct replay deliveries explicitly with `DeliveryReplay`.
Replay must not invoke process managers, external publication, queue
publication, or outbox insertion unless a separately named operation selects
those effects.

Handlers that need current event schemas compose an `eventsourcing.EventDecoder`
from the same payload codec and upcaster chain as aggregate repositories. The
decoder preserves the persisted source message while exposing transformed
metadata and ordered split segments. Process every logical segment before the
handler returns: the runner checkpoints the one stored global position only
after the complete handler call succeeds. Handler state must remain idempotent
because a later segment failure retries the source message.

## Scenario testing

`eventtest.CheckProjectionScenario` executes one real bounded runner batch and
compares scanned, handled, filtered, skipped, and checkpointed counts plus the
durable checkpoint. An application predicate can verify read-model state after
successful or expected-failure batches without exposing state in diagnostics.
Repeated calls exercise resume and backpressure through the same checkpoint
store.

Durable PostgreSQL checkpoints and same-transaction PostgreSQL read-model
updates are implemented. The compatibility matrix marks the projection and
bounded message-replay capabilities implemented without implying release or
deployment readiness.
