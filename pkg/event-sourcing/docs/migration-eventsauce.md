# Migrating from EventSauce to Go

This guide maps EventSauce 3.9.1 concepts to the idiomatic Go API. It does not
promise PHP source compatibility or automatic wire and database compatibility.
Pin the source application, EventSauce packages, schemas, codecs, and event
corpus before migrating any durable history.

## Start with the three responsibilities

Keep the same architectural seams:

| EventSauce | Go |
| --- | --- |
| `AggregateRootRepository` | `Repository[ID, Aggregate]` |
| `MessageRepository` | `EventStore` |
| `MessageDispatcher` | `Dispatcher` |

Construct them explicitly. Do not replace a PHP service container with a Go
service locator or package-global registry. The Go reference repository takes
the aggregate factory, identifier encoder, lifecycle accessor, application
function, codecs, store, clock, message-ID generator, decorators, and dispatcher
as validated constructor configuration.

## Aggregate roots

EventSauce aggregate behavior is commonly composed through traits and named
event-handling methods. In Go, the aggregate is an ordinary application struct
with one explicit type switch:

```php
final class Account
{
    use AggregateRootBehaviour;

    public function open(string $owner): void
    {
        $this->recordThat(new AccountOpened($owner));
    }

    private function applyAccountOpened(AccountOpened $event): void
    {
        $this->owner = $event->owner;
    }
}
```

```go
type Account struct {
	id        AccountID
	owner     string
	lifecycle eventsourcing.Lifecycle
}

func (account *Account) Open(owner string) error {
	event, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    "account.opened",
			Version: 1,
			Value:   AccountOpened{Owner: owner},
		},
	)
	if err != nil {
		return err
	}
	return account.lifecycle.Record(event, account.apply)
}

func (account *Account) apply(event eventsourcing.DecodedEvent) error {
	switch value := event.Value().(type) {
	case AccountOpened:
		account.owner = value.Owner
		return nil
	default:
		return eventsourcing.ErrUnknownEvent
	}
}
```

The Go application switch must be deterministic and side-effect free. Unknown,
malformed, or invariant-breaking stored events return errors. There is no
reflection-driven mapping from event type to method name.

Child entities receive an application-owned recorder function bound to the
root. They never acquire a hidden independent stream. See
[aggregate modeling](aggregates.md#child-entities).

## Aggregate identifiers

Replace `AggregateRootId` implementations with an application value type and
an explicit `IdentifierEncoder`. The encoded value becomes permanent stream
identity. Do not derive it from a Go type name or default string formatting.

EventSauce binary and string UUID storage require a reviewed import codec. See
[identifier and UUID migration](identifiers.md#migrating-eventsauce-histories)
for pinned binary and textual fixtures, ordering limits, and message-ID
preservation.

## Events and serialization

Replace PHP payload objects and class-name inflection with ordinary Go values
and explicit registrations:

```go
codec, err := eventsourcing.NewJSONCodec(
	eventsourcing.JSONEvent[AccountOpened]("account.opened", 1),
)
```

Persisted event name and schema version are stable data. Package paths, struct
names, and symbol renames never define persisted identity. Register an alias
when a historical name must decode to the current event. Use an upcaster when
payload or metadata meaning changes.

The payload codec remains separate from a Kafka, queue, or other message codec.
To adopt protobuf or MessagePack, replace `PayloadCodec`; aggregate and store
contracts do not change.

Do not migrate EventSauce object-hydrator conventions mechanically. Explicit
Go constructors and typed codecs make required values, validation, numeric
width, and time handling observable.

## Repository version handling

EventSauce 0.7 made the message repository report the final aggregate version
and added reads after a specified version for snapshotting. In Go:

- every stored `Message` has an explicit one-based `StreamVersion`;
- `Lifecycle.CommittedVersion` reports the reconstituted or persisted version;
- `ReadStreamOptions` selects an inclusive version range with a required bound;
- `Repository.Restore` verifies the snapshot version and reads strictly later
  messages; and
- optimistic append receives an explicit expected-version mode.

There is no generator return value carrying hidden version state. The iterator,
message, lifecycle, and save result expose it directly. Custom stores must pass
the public conformance suite for missing streams, ranges, order, cancellation,
corrupt history, and concurrency conflicts.

This is the migration outcome for the pinned
[EventSauce 0.7 upgrade contract](https://github.com/EventSaucePHP/EventSauce/blob/33ea9b97ec3ac56991caad03b791fee418a43e41/docs/docs/upgrading/to-0.7.0.md).

## Messages and headers

EventSauce messages combine payloads and headers. Go uses typed envelope fields
for identity and persistence-critical data and bounded string metadata for
application decoration.

| EventSauce header or concept | Go envelope field |
| --- | --- |
| event ID | `Message.ID` |
| aggregate root ID and type | `Message.Stream` |
| aggregate version | `Message.StreamVersion` |
| event type | `Message.EventName` |
| recording time | `Message.RecordedAt` |
| correlation and causation | `Message.CorrelationID` and `Message.CausationID` |
| custom headers | `Message.Metadata` |

Reserved `es.` metadata keys cannot be supplied by applications. Decorators
return validated copies and cannot change immutable envelope identity.

## Dispatchers and consumers

Replace `MessageDispatcher` implementations with the small Go dispatcher
contract. The synchronous implementation defines ordering, cancellation,
filtering, duplicate consumers, reentrancy, panic containment, and error
continuation explicitly.

Persist before dispatch. Direct broker publication after a PostgreSQL append is
not atomic. For durable production publication, stage event and outbox rows in
one caller-owned PostgreSQL transaction, commit it, then publish through the
outbox-owned Kafka adapter. Delivery remains at-least-once.

Kafka is not reduced to a generic queue. Topics, keys, partitions, offsets,
groups, acknowledgements, rebalance, retry, poison, and dead-letter policies
remain visible in `gokafka`.

## Snapshots

EventSauce snapshots map to the optional `snapshot` package and replaceable
`SnapshotStore`. A snapshot includes aggregate identity and version, snapshot
schema version, encoded state, metadata, and creation time.

Snapshots remain derived acceleration data. The repository verifies the
authoritative event stream at the snapshot version and applies only later
events. Stale, missing, corrupt, and incompatible fallback policies are named.
Refresh is explicit and synchronous; the package starts no hidden worker.

## Projections, replay, and process managers

Projection consumers use bounded global reads and durable checkpoints where a
store supports that optional capability. Replay is `DeliveryReplay`, not a
header convention. Filters can select streams, aggregate types, event names,
positions, and recording times.

Process managers are pure bounded planners. The application owns command
execution, durable process state, duplicate suppression, and retries. Replay is
rejected by default so rebuilding a projection cannot accidentally execute
business side effects.

## Testing migration

Replace EventSauce aggregate test cases with `eventtest` scenarios using
ordinary `testing.TB`, values, functions, and explicit equality:

1. stage no history or ordered historical events;
2. execute one behavior function;
3. assert events, metadata, versions, error, or panic policy; and
4. use store, dispatcher, global-reader, and snapshot conformance suites for
   custom infrastructure.

Pin representative production histories as licensed, redacted fixtures. Prove
that PHP decoding followed by canonical import produces the same ordered event
identity, payload meaning, aggregate state, and committed version as Go replay.
Do not treat equal JSON text alone as state equivalence.

## Intentional exclusions

The Go library does not reproduce:

- PHP traits or inheritance;
- method-name event-handler discovery;
- class-name event inflection;
- arbitrary object hydration;
- Laravel, Doctrine, or service-container integration;
- framework bootstrapping and lifecycle hooks;
- a mandatory command, query, or event bus; or
- generated PHP object conventions.

Use explicit application structs, constructors, codecs, aliases, upcasters,
and public adapter contracts instead. These differences preserve EventSauce's
outcomes without importing PHP runtime conventions into Go.

## Cutover checklist

1. Inventory every event name, schema version, payload, header, identifier
   codec, and stream version from the pinned source.
2. Define explicit Go registrations and upcasters without rewriting history.
3. Import a redacted corpus and prove full-state replay equivalence.
4. Run store and dispatcher conformance against the selected infrastructure.
5. Exercise optimistic conflicts, duplicates, cancellation, and crash recovery.
6. Rebuild snapshots and projections from imported history.
7. Verify queue or Kafka duplicate handling and replay isolation separately.
8. Reconcile counts, stream versions, message IDs, and global ordering before
   cutover.
9. Retain rollback data and document irreversible schema decisions.

Keep the original application authoritative until the migration evidence covers
the complete retained history and every external delivery boundary.
