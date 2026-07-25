# Aggregate scenario testing

The `eventtest` package exercises aggregate behavior without a custom runner,
global state, or a third-party assertion framework. A scenario is immutable:
`GivenNone`, `Given`, and `GivenHistory` return independent setups, and every
run constructs a fresh aggregate.

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

## Current boundary

Aggregate scenarios, payload-codec round trips, upcast results, deterministic
IDs, and event or metadata matching are implemented. Reusable event-store and
dispatcher conformance suites, snapshot equivalence, projections, and process
manager scenarios remain planned and are not current package guarantees.
