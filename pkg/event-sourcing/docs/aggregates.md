# Aggregate modeling

An aggregate owns business invariants and the deterministic application of its
events. The library owns only lifecycle bookkeeping, persistence composition,
and message delivery. There is no aggregate base class, reflective handler
discovery, or required command abstraction.

## Root shape

Keep the identifier, state, and `eventsourcing.Lifecycle` on the aggregate
root. Creation and reconstitution are separate application operations:

```go
type Order struct {
	id        OrderID
	lines     map[LineID]*Line
	lifecycle eventsourcing.Lifecycle
}

func NewOrder(id OrderID) *Order {
	return &Order{id: id, lines: make(map[LineID]*Line)}
}
```

The repository factory creates an empty root for replay. Domain creation is an
ordinary named constructor or method that records the first event. The
repository does not hydrate fields or bypass constructors.

Expose the root lifecycle to `RepositoryConfig.Lifecycle` and route historical
events through one explicit application switch:

```go
func (order *Order) apply(event eventsourcing.DecodedEvent) error {
	switch value := event.Value().(type) {
	case LineAdded:
		order.attachLine(value.LineID, value.Quantity)
	case LineQuantityChanged:
		line := order.lines[value.LineID]
		if line == nil {
			return eventsourcing.ErrCorruptHistory
		}
		line.quantity = value.Quantity
	default:
		return eventsourcing.ErrUnknownEvent
	}
	return nil
}
```

Stored input must return an error for unknown, malformed, incompatible, or
invariant-breaking history. It must never be skipped or allowed to panic.
Application handlers must be deterministic and side-effect free because the
same switch runs during live recording and replay.

## Recording behavior

Create a `DecodedEvent` with an explicit stable name and schema version, then
record it through the root lifecycle:

```go
func (order *Order) record(event eventsourcing.DecodedEvent) error {
	return order.lifecycle.Record(event, order.apply)
}
```

`Record` applies the event before returning and adds it to the pending change
set only when application succeeds. A command may record zero, one, or many
events. If application fails or panics, the lifecycle is poisoned because its
in-memory state can no longer be proven safe to persist.

The repository appends the complete pending batch with the lifecycle's
committed version as the optimistic expectation. It acknowledges and releases
that exact change set only after the store reports a committed outcome.

## Child entities

A child entity is part of the root's consistency boundary. Give it a narrow
application-owned recorder function that delegates to the root:

```go
type Line struct {
	id       LineID
	quantity uint32
	record   func(eventsourcing.DecodedEvent) error
}

func (order *Order) attachLine(id LineID, quantity uint32) {
	order.lines[id] = &Line{
		id:       id,
		quantity: quantity,
		record:   order.record,
	}
}
```

The child checks its local invariant, constructs the event, and calls
`record`. The root applies the event and remains the only lifecycle and stream
owner. The repository therefore persists child-originated events with the
root aggregate type, root identifier, and root stream version:

```go
func (line *Line) ChangeQuantity(quantity uint32) error {
	if quantity == 0 || quantity == line.quantity {
		return ErrInvalidQuantity
	}
	event, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    "order.line-quantity-changed",
			Version: 1,
			Value: LineQuantityChanged{
				LineID:   line.id,
				Quantity: quantity,
			},
		},
	)
	if err != nil {
		return err
	}
	return line.record(event)
}
```

Reattach the recorder whenever replay creates a child. Do not serialize the
function into an event or snapshot. A snapshot decoder likewise reconstructs
children with a recorder bound to the restored root before later events or
new behavior can use them.

Do not give a child its own repository or stream while also treating it as
part of the root. If it needs independent concurrency, lifecycle, or retention,
model it as a separate aggregate root and coordinate explicitly through
application services or process managers.

## Identifier ownership

Aggregate identifiers are application values. `IdentifierEncoder` converts
them to stable bounded stream data. Persisted identity must not depend on Go
type names, package paths, reflection output, or `fmt` formatting. A custom ID
type should validate at construction and have an explicit canonical encoder.

The core stores the encoded identifier as UTF-8 text. Applications that use a
UUID type may encode its canonical lowercase textual form. PostgreSQL users may
choose UUID-native application tables, but the event-store contract and wire
identity remain the explicit encoded string. See
[aggregate identifiers and UUID encoding](identifiers.md) for canonical codecs,
ordering, PostgreSQL representation, and history migration.

## Concurrency and ownership

One aggregate instance must not execute behavior or save concurrently. The
root owns its lifecycle and child graph; callers must not copy a lifecycle
after first use. Optimistic concurrency protects the persisted stream from
stale roots, but it does not make an in-memory aggregate safe for concurrent
mutation.

Keep external reads, clocks, random values, and service calls outside event
application. Capture required decisions in the event before recording it so
live execution and replay produce the same state.
