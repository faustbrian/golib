# Aggregate lifecycle

The package preserves EventSauce's recognizable lifecycle while keeping
application invariants and event application in ordinary Go:

1. create an aggregate or load it through `AggregateRepository`;
2. execute domain behavior;
3. call `Lifecycle.Record`, which applies the decoded event immediately;
4. save the aggregate, which encodes and atomically appends the ordered pending
   batch against the committed version; and
5. dispatch the persisted messages after the append commits.

The [five-minute quickstart](quickstart.md) is a complete executable example.

## Live behavior

An application aggregate owns its behavior and apply switch. The lifecycle
only owns committed and current versions, pending events, reconstitution
coordinates, and persistence acknowledgement.

```go
func (account *Account) Rename(owner string) error {
	event, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    "account.renamed",
			Version: 1,
			Value:   AccountRenamed{Owner: owner},
		},
	)
	if err != nil {
		return err
	}

	return account.lifecycle.Record(event, account.apply)
}
```

`Record` first validates lifecycle state, then invokes the application's apply
function synchronously. A successful call changes aggregate state immediately
and adds one pending event. A returned apply error or contained apply panic
poisons the lifecycle because the library cannot prove whether application
state was partially mutated.

One command may record no events, one event, or multiple events. A no-op save
does not call storage or dispatch.

## Reconstitution

Repository loads read a bounded ordered stream, upcast encoded historical
events at the read boundary, decode each logical event, and apply it without
calling domain behavior. The repository rejects missing streams, corrupt
source coordinates, unknown event identities, incompatible schema versions,
malformed payloads, and application errors. It never skips invalid history or
returns a partially usable aggregate.

Historical apply functions must be deterministic and side-effect free. They
must not call clocks, random generators, databases, networks, queues, or
external services. Those dependencies belong in command handling before an
event is recorded or in explicit post-persistence consumers.

The model-based repository test generates bounded event histories, executes
them live, persists them, replays them into a fresh aggregate, and requires
identical state and versions.

## Persistence acknowledgement

Saving prepares immutable messages from the complete pending change set and
uses the lifecycle's committed version as the optimistic expectation. Pending
events are acknowledged only after the store returns the exact persisted
messages. A concurrency conflict or known non-commit leaves the change set
available for application-directed resolution.

An unknown commit outcome is different: the lifecycle enters reconciliation
state and must not be saved again until the application determines whether the
prepared message IDs are durable. A caller-owned transaction uses the explicit
`PrepareSave`, adapter stage, commit, `ConfirmCommitted`, and
`DispatchCommitted` flow instead of treating staged rows as committed. If the
commit result is ambiguous, `MarkCommitUnknown` preserves the planned and
staged messages for reconciliation and poisons that aggregate lifecycle.

## Dispatch boundary

The aggregate repository dispatches only `DeliveryLive` messages returned by a
successful durable append. Dispatch failure does not undo the append and the
save result preserves the committed messages. External delivery that must
survive a process crash requires the optional transactional outbox
composition; direct database-to-broker dispatch is not atomic.

Replay is a separately selected operation and uses `DeliveryReplay`.
Process managers, queues, Kafka publication, outbox insertion, and other side
effects must reject replay unless an explicitly named operation enables it.

## Child entities

A child entity receives a narrow root recorder function. It may decide and
describe a change, but the root lifecycle records the event so the envelope
retains the root aggregate type, ID, ordering, and optimistic version. A nested
object must not own an independently persisted stream unless it is modeled as
a separate aggregate root and repository. See the complete
[aggregate modeling guide](aggregates.md#child-entities) for the composition and
reconstitution pattern.
