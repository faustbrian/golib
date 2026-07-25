# Public API design

This document validates representative workflows before implementation. Names
may change while the module remains unreleased, but the observable contracts
and ownership decisions require explicit review before they change.

## Design principles

- The aggregate repository, event store, and dispatcher remain separately
  replaceable.
- Storage operates on encoded immutable messages; application aggregates own
  domain invariants and event application.
- Stored event names and schema versions are explicit data and never derive
  from Go package paths or type names.
- Pure codecs and upcasters do not accept contexts. Every operation that may
  block on I/O accepts `context.Context`.
- Constructors reject nil dependencies, typed nils, invalid limits, duplicate
  registrations, and conflicting options.
- Core values defensively copy caller-owned bytes and metadata.
- The core starts no goroutines and owns no hidden transaction.

## Stable identities and encoded events

```go
type StreamID struct {
    AggregateType string
    AggregateID   string
}

type EventName string
type SchemaVersion uint32
type MessageID string
type GlobalPosition uint64

type EncodedEvent struct {
    Name        EventName
    Version     SchemaVersion
    ContentType string
    Payload     []byte
}
```

Constructors validate bounded, non-empty canonical strings and defensively copy
payload bytes. Exported structs shown here are illustrative; the implementation
will use constructors and read-only accessors where direct fields would allow a
caller to violate immutability.

Application identifiers remain application-defined. A repository receives an
explicit codec instead of depending on `fmt.Stringer`, reflection, or a Go type
name:

```go
type IdentifierCodec[ID any] interface {
    Encode(ID) (string, error)
    Decode(string) (ID, error)
}
```

## Pending and persisted messages

A pending message contains all immutable identity and event data except the
store-assigned stream version and optional global position. The event store
returns persisted messages:

```go
type PendingMessage struct {
    // constructed through NewPendingMessage
}

type Message struct {
    // constructed by a conformant store or decoded by a MessageCodec
}

func NewPendingMessage(input PendingMessageInput) (PendingMessage, error)

func (m Message) ID() MessageID
func (m Message) Stream() StreamID
func (m Message) StreamVersion() uint64
func (m Message) Event() EncodedEvent
func (m Message) Metadata() map[string]string
func (m Message) RecordedAt() time.Time
func (m Message) CorrelationID() (MessageID, bool)
func (m Message) CausationID() (MessageID, bool)
func (m Message) Tenant() (string, bool)
func (m Message) Partition() (string, bool)
func (m Message) GlobalPosition() (GlobalPosition, bool)
```

Message IDs, stream identity, event identity, payload, application metadata,
and optional correlation, causation, tenant, and partition values are supplied
before append. A repository normally generates the message ID and recorded
time through injected dependencies. The event store assigns stream versions
and, if supported, global positions atomically.

Metadata is initially restricted to bounded UTF-8 string keys and values.
Protocol fields are dedicated envelope fields rather than reserved entries in
the application map. Keys using the reserved `es.` prefix are rejected. This
intentional difference avoids EventSauce's mixed PHP header values and gives
metadata deterministic ownership and encoding.

Time values are normalized to UTC, have monotonic data removed, and use a
documented precision. Diagnostic formatting exposes identities and sizes but
never payload or metadata values.

## Event store

```go
type EventStore interface {
    Append(
        ctx context.Context,
        stream StreamID,
        expected ExpectedVersion,
        messages []PendingMessage,
    ) ([]Message, error)

    ReadStream(
        ctx context.Context,
        stream StreamID,
        options ReadStreamOptions,
    ) (MessageIterator, error)
}

type MessageIterator interface {
    Next(context.Context) bool
    Message() Message
    Err() error
    Close() error
}
```

`Append` rejects an empty batch. It atomically appends one ordered batch to one
stream and commits it, or appends nothing. Success means the batch is durably
committed according to the store's documented durability contract; an appender
bound to an uncommitted caller transaction must use a different interface.
Returned messages correspond one-for-one with the input order and contain
assigned stream versions. A global-capable store also assigns committed global
positions.

An append error carries a durability outcome: definitely not committed,
definitely committed, or unknown. Only the first outcome is safely retryable
without reconciliation. A repository acknowledges a definitely committed
result even if a post-commit operation reports an error. An unknown result
poisons the in-memory aggregate until message IDs are reconciled against the
store.

Expected-version constructors distinguish the supported modes without magic
integers:

```go
func ExpectNewStream() ExpectedVersion
func ExpectExistingStream() ExpectedVersion
func ExpectExactVersion(version uint64) ExpectedVersion
func ExpectAnyVersion() ExpectedVersion
```

`ExpectAnyVersion` is an explicit lost-update opt-out. The store still assigns
the next versions; callers never guess them. Stores that cannot safely provide
a mode return a typed unsupported-capability error.

The iterator is bounded by validated read options, checks cancellation during
iteration, and has caller-owned closure. A false `Next` means end, error, or
cancellation; `Err` distinguishes them. Messages returned from an iterator do
not alias store-owned mutable memory.

Optional capabilities use separate narrow interfaces discovered with ordinary
type assertions:

```go
type GlobalReader interface {
    ReadAll(context.Context, ReadAllOptions) (MessageIterator, error)
}
```

Deletion and archive are not part of the minimum append/read contract. Stores
that support them expose separately documented interfaces so unsupported
behavior cannot silently look like a missing stream.

## Aggregate lifecycle

Application aggregates own their `Apply` switch and invariants. The helper
tracks versions and a retry-safe pending change set:

```go
type Lifecycle struct {
    // unexported state
}

func (l *Lifecycle) Record(
    event DecodedEvent,
    apply func(DecodedEvent) error,
) error
func (l *Lifecycle) Reconstitute(
    baseVersion uint64,
    history []HistoricalEvent,
    apply func(DecodedEvent) error,
) error
func (l *Lifecycle) Changes() (ChangeSet, error)
func (l *Lifecycle) Acknowledge(
    ChangeSet,
    []PendingMessage,
    []Message,
) error
```

`Record` applies the event first against a protected lifecycle transition and
adds it to the pending set only when application succeeds. Reconstitution
accepts known decoded historical events, requires contiguous source stream
versions after the supplied base version, and never records them as pending.
Explicit segment coordinates allow an upcaster to expand one stored message
into multiple ordered logical events without advancing the aggregate version
more than once.

Application behavior must validate a command before calling `Record`. Applying
a known event is required to be total, deterministic, and side-effect free.
If application returns an error or panics after it may have mutated state, the
lifecycle becomes poisoned: it exposes no saveable change set and the aggregate
must be discarded and loaded again. The library cannot generically roll back
application-owned fields.

`Changes` returns an immutable token and defensive event copy without removing
events. An append known not to have committed leaves the same token retryable.
`Acknowledge` succeeds only when the exact token was persisted and the prepared
and returned messages match in count, stream, envelope data, identities, and
versions. Successful
acknowledgement advances the committed version and removes exactly those
pending events. Reusing or forging a token fails. A conformant store returning
a mismatched successful append poisons the lifecycle and reports that
persistence occurred, so callers cannot retry blindly.

An aggregate is not implicitly safe for concurrent mutation. The application
owns synchronization around one in-memory aggregate instance. Lifecycle
mutation and persistence acknowledgement are rejected while an event is being
applied; application handlers must not reenter the lifecycle.

Representative application code:

```go
type Account struct {
    id        AccountID
    balance   int64
    lifecycle eventsourcing.Lifecycle
}

func (a *Account) Deposit(cents int64) error {
    if cents <= 0 {
        return ErrInvalidDeposit
    }

    event, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
        Name:    "account.money-deposited",
        Version: 1,
        Value:   MoneyDeposited{Cents: cents},
    })
    if err != nil {
        return err
    }

    return a.lifecycle.Record(event, a.apply)
}

func (a *Account) apply(decoded eventsourcing.DecodedEvent) error {
    switch event := decoded.Value().(type) {
    case AccountOpened:
        a.balance = 0
    case MoneyDeposited:
        a.balance += event.Cents
    default:
        return fmt.Errorf("%w: %T", eventsourcing.ErrUnknownEvent, decoded.Value())
    }

    return nil
}
```

The type switch is explicit application code. The library does not discover
`applyX` methods through reflection.

## Aggregate repository

The public replacement contract stays small:

```go
type Repository[ID, Aggregate any] interface {
    Load(context.Context, ID) (Aggregate, error)
    Save(context.Context, Aggregate) (SaveResult, error)
}
```

The reference repository is assembled from an identifier codec, aggregate
factory, lifecycle access callbacks, payload codec, store, decorators,
dispatcher, clock, and message-ID generator. Construction performs all static
validation.

`Load` reads an ordered stream, upcasts encoded events at the read boundary,
decodes them, and reconstitutes a new aggregate. Unknown, malformed,
incompatible, duplicated, missing, or reordered history returns a classified
error and no partially usable aggregate.

`Save` snapshots pending changes without releasing them, creates and decorates
pending messages, appends them, acknowledges the aggregate only after durable
append success, and then invokes the selected post-persistence dispatcher.
`SaveResult` always reports whether persistence happened and which messages
were stored, even when dispatch later fails. A dispatch failure never makes
successfully appended events pending again. Saving an aggregate with no pending
events is a successful no-op that neither appends nor dispatches.

Caller-owned transactions use a two-phase application API instead of `Save`:

```go
plan, err := repository.PrepareSave(aggregate)
if err != nil {
    return err
}

staged, err := postgres.Stage(ctx, tx, plan)
if err != nil {
    return err // the aggregate change set remains pending
}

if err := tx.Commit(ctx); err != nil {
    return repository.CommitUnknown(plan, staged, err)
}

result, err := repository.ConfirmCommitted(aggregate, plan, staged)
if err != nil {
    return err
}

return repository.DispatchCommitted(ctx, result)
```

`PrepareSave` is pure with respect to storage and does not release changes.
`Stage` neither commits nor acknowledges the aggregate. `ConfirmCommitted`
validates the exact plan and stored messages before acknowledgement.
`CommitUnknown` poisons the in-memory lifecycle until the application
reconciles the staged message IDs against durable storage. This explicit path
also lets the optional outbox adapter stage event and outbox rows in the same
transaction without dispatching before commit.

## Codecs and registration

Payload and message encoding are separate:

```go
type PayloadCodec interface {
    Encode(DecodedEvent) (EncodedEvent, error)
    Decode(EncodedEvent) (DecodedEvent, error)
}

type MessageCodec interface {
    Encode(Message) ([]byte, error)
    Decode([]byte) (Message, error)
}
```

The JSON payload codec is built from explicit registrations:

```go
codec, err := eventsourcing.NewJSONCodec(
    eventsourcing.JSONEvent[AccountOpened]("account.opened", 1),
    eventsourcing.JSONAlias(
        "account.created",
        1,
        "account.opened",
        1,
    ),
    eventsourcing.WithJSONStrictDecoding(),
)
```

Registrations bind an explicit stable name and schema version to a Go value
type. Duplicate identities, ambiguous aliases, missing alias targets, and
invalid versions fail during construction. Aliases apply only while decoding
historical data; encoding always requires the canonical identity. Type
assertions remain local to generic registration wrappers and runtime
reflection is not used.

The codec enforces the envelope payload limit, UTF-8 validity, unique object
keys, maximum nesting depth, and per-container entry limits before typed
decoding. It uses `json.Number` while parsing and preserves typed `int64` and
`time.Time` values. Strict mode additionally rejects fields unknown to the
registered Go type. `encoding/json` provides deterministic output for the same
Go value, including sorted string map keys; the package does not claim RFC
8785 canonical JSON.

## Upcasting

Upcasters operate on encoded stored messages before payload decoding:

```go
type Upcaster interface {
    Upcast(StoredEvent) ([]LogicalEvent, error)
}

type UpcasterChain interface {
    Upcast(StoredEvent) ([]LogicalEvent, error)
}
```

A logical event retains its source stream version and has a deterministic
zero-based segment index. Splitting one stored event does not invent stored
versions. Dropping requires a separately named policy object supplied to chain
construction. Chains have explicit maximum steps and output counts, require
monotonic schema progress for repeated transformations, and reject cycles,
non-progress, ambiguous matches, panics, and budget exhaustion.

Upcasting never writes to the event store. Snapshot compatibility is checked
against the snapshot schema and source stream version before post-snapshot
events are read.

## Dispatch

```go
type Consumer interface {
    Handle(context.Context, Delivery) error
}

type Dispatcher interface {
    Dispatch(context.Context, []Delivery) error
}

type Delivery struct {
    Message Message
    Mode    DeliveryMode // Live or Replay
}
```

The synchronous dispatcher preserves message order and consumer registration
order. Its options explicitly select stop-on-error or continue-and-join,
document panic conversion, reject duplicate consumers unless allowed, and
define partial success. An empty batch succeeds without calling consumers.
Cancellation stops new calls but does not claim to interrupt a consumer that
ignores its context.

Reentrant dispatch is permitted only when selected during construction and is
never implemented with a held lock. Dispatcher chains preserve configured
order. Filters are ordinary explicit functions. Repository dispatch occurs
only after confirmed commit. A transaction-staging operation cannot invoke a
dispatcher. Durable external publication requires the outbox adapter.

## Snapshots

Snapshots contain aggregate identity, aggregate version, snapshot schema
version, encoded state, metadata, and creation time. They are derived cache
entries, never authoritative history.

A snapshot repository returns missing, stale, corrupt, and incompatible states
as distinct outcomes. Policy decides whether each outcome fails loading or
falls back to full history. Restoration reads events strictly after the
snapshot aggregate version. Refresh is an explicit blocking call; the package
starts no background goroutine.

## Projections, replay, and process managers

Projection runners consume a bounded global iterator, apply a message, and
advance a durable checkpoint. Stores that support atomic projection update
plus checkpoint expose that as an optional capability. Reset, rebuild, resume,
pause, and status are explicit operations.

Process managers are pure planners:

```go
type ProcessManager[Command any] interface {
    Plan(context.Context, Delivery) ([]Command, error)
}
```

Planning returns commands or messages for an application-owned executor. A
replay delivery is rejected by default, so rebuilding a projection cannot
silently execute external effects.

## Errors

Sentinel categories support `errors.Is`; structured errors support
`errors.As`, preserve causes, and expose safe fields:

```go
var (
    ErrStreamNotFound       = errors.New("event stream not found")
    ErrConcurrencyConflict  = errors.New("event stream concurrency conflict")
    ErrDuplicateMessage     = errors.New("duplicate message identifier")
    ErrCommitUnknown        = errors.New("event store commit outcome unknown")
    ErrCorruptHistory       = errors.New("corrupt event history")
    ErrUnknownEvent         = errors.New("unknown event")
    ErrIncompatibleVersion  = errors.New("incompatible event version")
    ErrUnsupported          = errors.New("unsupported capability")
)
```

Errors never include payloads, metadata values, credentials, connection
strings, or arbitrary codec input.
