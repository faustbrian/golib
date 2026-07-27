# Aggregate scenario testing

The `eventtest` package exercises aggregate behavior without a custom runner,
global state, or a third-party assertion framework. A scenario is immutable:
`GivenNone`, `Given`, and `GivenHistory` return independent setups, and every
run constructs a fresh aggregate.

The [runnable scenario example](../scenario_example_test.go) exercises the
given-no-history workflow and reports committed and pending versions.

## Basic scenario

```go
scenario, err := eventtest.NewScenario(eventtest.AggregateConfig[*Account]{
	New: func() (*Account, error) {
		return NewAccountForReconstitution(), nil
	},
	Lifecycle: func(account *Account) *eventsourcing.Lifecycle {
		return account.EventLifecycle()
	},
	Apply: func(account *Account, event eventsourcing.DecodedEvent) error {
		return account.ApplyHistorical(event)
	},
})
if err != nil {
	t.Fatal(err)
}

given, err := scenario.Given(accountOpened)
if err != nil {
	t.Fatal(err)
}
result := given.When(func(account *Account) error {
	return account.Close()
})
if result.Error() != nil {
	t.Fatal(result.Error())
}
if result.CommittedVersion() != 1 || result.Version() != 2 {
	t.Fatalf("unexpected versions")
}
events := result.Events()
if len(events) != 1 || events[0].Name().String() != "account.closed" {
	t.Fatalf("unexpected events")
}
```

`Given` treats every supplied decoded event as one consecutive stored stream
version. Use `GivenHistory` when testing split upcasts or deliberately corrupt
source-version coordinates. `Reconstitute` applies history without invoking
behavior. A reconstitution failure returns no partially usable result and does
not invoke the behavior function.

## Errors and panics

`When` returns behavior errors and propagates behavior panics, matching normal
Go test semantics. `WhenCapturingPanic` is the explicit alternative for a test
whose contract is a panic:

```go
result := scenario.WhenCapturingPanic(func(account *Account) error {
	panic("programmer error")
})
value, panicked := result.Panic()
if !panicked || value != "programmer error" {
	t.Fatalf("unexpected panic result")
}
```

Events from `Record` calls that completed before a later behavior error or
captured panic remain visible. Failed event application poisons the lifecycle,
and the result reports that error without exposing pending state as valid.

## Payload, metadata, codecs, and upcasters

`MatchEvent` checks stable name and schema version, then invokes an optional
application predicate. `MatchMetadata` compares complete metadata maps.
`CheckPayloadRoundTrip` and `CheckUpcast` exercise the public codec and upcaster
boundaries. Their diagnostics never format event values, metadata values,
payload bytes, or predicate errors; applications control any additional test
output.

Core `FixedClock` and `ManualClock` values provide deterministic time.
`NewMessageIDSequence` provides validated deterministic IDs, is safe for
concurrent use, and returns `ErrSequenceExhausted` rather than recycling an ID.

## Dispatcher conformance

`CheckSynchronousDispatcher` verifies the default replaceable synchronous
dispatcher contract without a custom runner or assertion framework. A factory
adapts ordinary conformance registrations to the implementation's own
registration mechanism:

```go
err := eventtest.CheckSynchronousDispatcher(ctx, func(
	registrations []eventtest.DispatcherRegistration,
) (eventsourcing.Dispatcher, error) {
	return newApplicationDispatcher(registrations)
})
if err != nil {
	t.Fatal(err)
}
```

The suite checks empty batches, message-major and registration ordering,
live/replay mode preservation, stop-on-error behavior, pre-call cancellation,
filters, duplicate registration, reentrant dispatch, panic containment, and
panic-value redaction. The factory must return a fresh default stop-on-error
dispatcher. Continue-on-error is a separately selected policy and is not the
default conformance profile.

## Event-store conformance

`CheckEventStore` verifies a committed store through fresh factory results:

```go
err := eventtest.CheckEventStore(ctx, func() (
	eventsourcing.EventStore,
	error,
) {
	return newApplicationStore()
})
if err != nil {
	t.Fatal(err)
}
```

The suite checks ordered atomic batch append, defensive ownership, bounded
range reads, new/existing/exact/any expected versions, stale conflicts,
duplicate IDs within a batch and across streams, empty batches, missing
streams, no partial append, cancellation, iterator closure, and standard error
inspection. Every rejected append must report `CommitNotCommitted`.

The factory must return a committed store without preexisting conformance
fixture identities, and the caller owns its external resources. Several
factory results may share one isolated test database because every scenario
uses a distinct stream and message-ID prefix. Transaction-bound staging APIs
do not implement `EventStore` and do not run this committed-store profile.
Optional global ordering is a separate capability and requires its own
conformance profile.

`CheckGlobalReader` accepts a factory returning both `EventStore` and
`GlobalReader`. It separately verifies empty global reads, positions returned
by sequential committed appends, stable cross-stream order, inclusive position
ranges, limits, defensive ownership, cancellation, and iterator closure.
Positions must be present and strictly increasing; the generic profile does
not require them to be gap-free because stores may reserve or omit positions
under their documented transaction policy.

## Snapshot equivalence

`CheckSnapshotEquivalence` runs authoritative full-history loading and an
independent snapshot-accelerated load. The snapshot loader returns the exact
snapshot version reported by its manager:

```go
err := eventtest.CheckSnapshotEquivalence(ctx,
	eventtest.SnapshotEquivalenceConfig[*Account]{
		FullHistory: loadAccountFromHistory,
		Snapshot: func(ctx context.Context) (*Account, uint64, error) {
			account, info, err := snapshotManager.Load(ctx, id)

			return account, info.SnapshotVersion(), err
		},
		Version: accountVersion,
		Equal:   accountsEqual,
	},
)
```

The helper rejects fallback loads that did not actually use a snapshot,
snapshot versions ahead of restored state, final aggregate-version mismatch,
and application state mismatch. Loader failures preserve their causes while
conformance diagnostics never format aggregate or snapshot state.

## Process-manager scenarios

`CheckProcessManagerScenario` exercises a real process manager through its
public planning contract. Success scenarios compare message identity, live or
replay mode, and ordered application commands. Failure scenarios use
`errors.Is` categories and reject partial plan output:

```go
err := eventtest.CheckProcessManagerScenario(ctx,
	eventtest.ProcessManagerScenario[SendEmail]{
		Manager:  manager,
		Delivery: delivery,
		Commands: []SendEmail{expected},
		Equal: func(left, right SendEmail) bool {
			return left == right
		},
	},
)
```

Command values, event data, and error values are never formatted in mismatch
diagnostics. Expected failures cannot also declare commands or equality, and
successful non-empty plans require application-defined equality.

## Projection scenarios

`CheckProjectionScenario` runs one bounded projection batch and compares every
public progress count plus the durable checkpoint. An optional state predicate
checks application-owned read-model state without reflection or value
formatting. `WantError` uses `errors.Is` while still checking partial batch
progress and state:

```go
err := eventtest.CheckProjectionScenario(ctx, eventtest.ProjectionScenario{
	Runner: runner,
	Expected: eventtest.ExpectedProjectionBatch{
		Scanned:      2,
		Handled:      2,
		Checkpointed: 2,
		Checkpoint:   42,
	},
	State: func() bool { return summary.Accounts == 2 },
})
```

Each call represents one bounded runner batch. Multi-batch resume, rebuild, and
poison-policy tests compose ordinary scenario calls around the same durable
checkpoint store rather than introducing a custom test runner.

## Current boundary

Aggregate scenarios, payload-codec round trips, upcast results, deterministic
IDs, event or metadata matching, committed event-store conformance, and default
synchronous-dispatcher and optional global-reader conformance are implemented.
Snapshot equivalence is implemented and exercised through the real snapshot
manager with later history. Process-manager planning scenarios are implemented.
Projection batch and application-state scenarios are implemented.
