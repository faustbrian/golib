# Snapshot storage

Snapshots are optional derived acceleration data. Event history remains the
authority: applications must be able to delete snapshots and rebuild them from
the event store.

## Snapshot envelope

`Snapshot` owns and validates:

- aggregate type and identifier through `StreamID`;
- the last stored aggregate stream version represented by the state;
- an explicit snapshot-state schema version;
- non-empty encoded state bounded by `MaxSnapshotStateBytes`;
- application metadata under the same bounded, reserved-key-safe rules as
  event metadata; and
- a canonical UTC creation time at microsecond precision.

State and metadata are copied on construction and access. `Equal` compares
every observable field, including creation time. Snapshot schema versions are
independent from event schema versions; changing one does not imply changing
the other.

## Store contract

`SnapshotStore` has three context-aware operations: `Load`, `Save`, and
idempotent `Delete`. `Save` is atomic for the snapshot record itself. It:

- rejects a lower aggregate version;
- rejects a lower snapshot schema version, even when aggregate state is newer;
- accepts an exact retry of the same snapshot;
- rejects different state at identical aggregate and schema versions; and
- accepts a higher aggregate version or a higher schema version.

`SnapshotVersionError` exposes stored and incoming versions while redacting
the stream from its diagnostic string. `SnapshotConflictError` likewise
exposes versions for inspection without printing the aggregate identifier.

The `memory.SnapshotStore` implements this contract for tests and local
development with one synchronization owner. Its zero value is invalid; use
`memory.NewSnapshotStore`.

The independently versioned PostgreSQL module provides a durable implementation
through `postgres.NewSnapshotStore`. It owns one short transaction per save,
serializes concurrent writes to one aggregate, preserves canonical UTC
microsecond timestamps, and uses the same stale, conflict, missing, and
idempotent-delete categories as the core contract. It starts no goroutines and
does not own the supplied pool.

## Aggregate restoration

`AggregateRepository.Restore` first proves that the authoritative event stream
contains the exact snapshot aggregate version. Only then does it invoke the
application snapshot decoder through an ownership-transferring
`AggregateRestorer`. The decoded aggregate identifier must match the requested
stream and its lifecycle must be pristine.

Decoder failures return `SnapshotRestorationError`. Its diagnostic text is
redacted, while `errors.Is` and `errors.As` can inspect the original cause.

The lifecycle establishes the snapshot version without replaying or recording
events, then the repository reads, upcasts, decodes, and applies events
strictly after that version through the same bounded path as ordinary loading.
If verification, decoding, evolution, or later history fails, the repository
returns no aggregate. A successful restorer result is owned by the repository;
the callback must not retain or reuse it after an error.

## Composition and fallback

The `snapshot.Manager` composes an aggregate repository, snapshot store,
application state codec, aggregate identity callbacks, lifecycle accessor, and
clock. `StateCodec` owns the snapshot-state format and reports one explicit
schema version. A manager caches that version at construction, so callers must
replace the manager to deploy a different codec version.

Every manager requires an explicit `FallbackPolicy`. `FailClosed` returns
classified snapshot failures to the caller. `NewFallbackPolicy` can instead
allow full-history restoration separately for:

- a missing snapshot;
- snapshot state classified as corrupt; and
- a snapshot schema classified as incompatible.

An unclassified store, repository, history, cancellation, or application
failure never triggers fallback. Successful fallback returns `LoadInfo` with
`LoadFullHistory` and only the stable error category; application codec
diagnostics are not exposed there. Successful snapshot restoration returns
`LoadSnapshot` and the snapshot version.

```go
policy, err := snapshot.NewFallbackPolicy(
	snapshot.FallbackMissing,
	snapshot.FallbackCorrupt,
	snapshot.FallbackIncompatible,
)
if err != nil {
	return err
}

manager, err := snapshot.NewManager(snapshot.ManagerConfig[AccountID, *Account]{
	AggregateType: "account",
	EncodeID:      encodeAccountID,
	Identify:      accountID,
	Lifecycle:     accountLifecycle,
	Repository:    repository,
	Store:         snapshotStore,
	Codec:         accountSnapshotCodec,
	Clock:         clock,
	Fallback:      policy,
	Migrations:    accountSnapshotMigrations,
})
```

When a codec advances its snapshot schema, applications can provide a bounded
ordered migration chain. Each step names its exact source and target version:

```go
v1ToV2, err := snapshot.NewStateMigration(
	1,
	2,
	func(
		state []byte,
		metadata map[string]string,
	) ([]byte, map[string]string, error) {
		return migrateAccountSnapshotV1(state), metadata, nil
	},
)
if err != nil {
	return err
}

migrations, err := snapshot.NewMigrationChain(v1ToV2)
if err != nil {
	return err
}
```

Assign the chain to `ManagerConfig.Migrations`. Migration occurs only during
load, before the current codec decodes state; it never rewrites the stored
snapshot. Steps must advance monotonically, paths are bounded, duplicate source
versions are rejected, and the same owned input is evaluated twice to reject
non-deterministic output. Missing paths remain `ErrSnapshotIncompatible` and
follow the configured fallback policy. Callback failures and panics are
redacted while preserving stable causes for error inspection.

`Manager.Refresh` accepts only an aggregate with a non-zero committed version,
no pending events, and a healthy lifecycle. It encodes and saves synchronously;
the library never starts a worker or background goroutine. Applications that
prefer a version threshold construct a validated `ThresholdPolicy` and call
`Manager.RefreshIfDue` with the version of the retained snapshot:

```go
policy, err := snapshot.NewThresholdPolicy(100)
if err != nil {
	return err
}

created, refreshed, err := manager.RefreshIfDue(
	ctx,
	accountID,
	account,
	loadInfo.SnapshotVersion(),
	policy,
)
```

The call is synchronous and returns whether it saved a snapshot. A previous
version of zero represents no retained snapshot. The aggregate must be fully
persisted, and a previous snapshot version newer than the aggregate is
rejected. Encoder and metadata-provider failures preserve their cause for
`errors.Is` and `errors.As` while redacting their diagnostic text. State-codec
and metadata-provider panics are contained as stable redacted error categories;
panic values are never retained or reported.

## Atomicity and recovery

Snapshot saving is not atomic with an event append unless a durable adapter
explicitly provides that transaction boundary. A crash after event append and
before snapshot save leaves only a stale or missing cache entry; restoration
must continue from authoritative history. Snapshot failure must never make a
successful event append look uncommitted.

Concurrent refreshes cannot replace newer retained state with an older
aggregate or schema version. A same-version conflict is not silently resolved:
the caller must load, compare, delete, or rebuild according to application
policy.

## Current boundary

The immutable envelope, store contract, classified errors, in-memory store,
lifecycle snapshot version, bounded repository restoration, explicit
application codec composition, classified fallback, blocking refresh, and
explicit threshold refresh are implemented. Eventtest equivalence compares
full replay with snapshot state followed by later history. Stale stored
snapshots remain valid accelerators and are followed by later history;
`ErrSnapshotStale` describes a rejected store write rather than a load-fallback
category. Explicit bounded snapshot schema migrations are implemented. Durable
PostgreSQL storage is implemented in the independently versioned `postgres`
module; its database transaction and operational guarantees are adapter
guarantees rather than core guarantees.
