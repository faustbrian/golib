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
type SchemaVersion uint32
type GlobalPosition uint64

type StreamID struct {
    // opaque validated aggregate type and identifier
}

type EventName struct {
    // opaque validated stable event identity
}

type MessageID struct {
    // opaque validated stable message identity
}

type EncodedEvent struct {
    // constructed through NewEncodedEvent
}

func NewEventName(string) (EventName, error)
func NewMessageID(string) (MessageID, error)
func NewStreamID(aggregateType, aggregateID string) (StreamID, error)
```

Constructors validate bounded, non-empty canonical strings and defensively
copy payload bytes. Identity values and encoded events expose read-only
accessors so direct fields cannot violate immutability.

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
func (m Message) EventName() EventName
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
positions. Core validation limits one append to `MaxAppendMessages`; adapters
must reject larger work before starting a transaction.

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

Every append error has an explicit `CommitOutcome`. Validation, cancellation
before mutation, concurrency conflicts, duplicate message IDs, and in-memory
failures are `CommitNotCommitted`. Unclassified adapter errors are
conservatively `CommitUnknown`. `AppendError` preserves its cause for
`errors.Is` and `errors.As` while its diagnostic string omits driver text.
`ConcurrencyError` exposes the expected and actual versions for inspection but
does not print the potentially sensitive stream identifier.

The iterator is bounded by validated read options, checks cancellation during
iteration, and has caller-owned closure. A false `Next` means end, error, or
cancellation; `Err` distinguishes them. Messages returned from an iterator do
not alias store-owned mutable memory. Cancellation and invalid contexts are
terminal for an iterator; iterators are not implicitly safe for concurrent
use.

The `memory` package implements the same atomic append, optimistic concurrency,
duplicate-ID, global-position, cancellation, range, and ownership contract as
a durable store. It is safe for concurrent store operations but provides only
process-local durability. Its `Store` zero value is invalid; callers construct
it with `memory.NewStore`.

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

## Time, message IDs, and metadata decoration

```go
type Clock interface {
    Now() time.Time
}

type MessageIDGenerator interface {
    NewMessageID(context.Context) (MessageID, error)
}

type MessageDecoratorFunc func(PendingMessage) (PendingMessage, error)

func NewMessageDecoratorChain(
    ...MessageDecoratorFunc,
) (*MessageDecoratorChain, error)
func NewMetadataDecorator(map[string]string) (MessageDecoratorFunc, error)
```

`SystemClock`, validated `FixedClock`, concurrency-safe `ManualClock`, and
`ClockFunc` return UTC, microsecond-precision values with monotonic data
removed. They replace global clock mutation. The repository receives a
`Clock`; tests can inject fixed or explicitly advanced manual time.

`MessageIDGeneratorFunc` adapts application generators. The first-party random
generator reads 128 bits from an explicit entropy callback and returns 32
lowercase hexadecimal characters; `NewCryptoRandomMessageIDGenerator` selects
`crypto/rand`. Generator failures retain their cause for inspection but redact
the wrapped diagnostic. Cancellation is checked before entropy is read. The
entropy callback itself must be bounded because its function signature cannot
interrupt an active read.

Message decorators are ordered pure callbacks over immutable pending messages.
They may replace validated application metadata but cannot change message,
stream, or event identity, time, correlation, causation, tenant, or partition.
The chain defensively owns inputs and outputs, stops on the first error,
contains panics, and returns a redacted indexed `DecoratorError`. The
first-party static metadata decorator rejects collisions instead of silently
overwriting application data. `PendingMessage.WithMetadata` enables custom
metadata decorators without exposing mutable envelope fields.

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
validation. `RepositoryConfig` also accepts an optional `MessageContext`
callback for application metadata, correlation, causation, tenant, and
partition values, plus a bounded read-page size.

`Load` reads an ordered stream, upcasts encoded events at the read boundary,
decodes them, and reconstitutes a new aggregate one stored version at a time.
This bounds retained logical history even when a stored event splits. Explicit
drops still advance the committed source version. Unknown, malformed,
incompatible, duplicated, missing, or reordered history returns a classified
error and no partially usable aggregate.

`Save` snapshots pending changes without releasing them, creates and decorates
pending messages, appends them, acknowledges the aggregate only after durable
append success, and then invokes the selected post-persistence dispatcher.
`SaveResult` exposes `CommitOutcome`, the exact prepared envelopes, persisted
messages, and whether dispatch began, even when dispatch later fails. Prepared
message IDs remain available to reconcile an unknown commit outcome. A dispatch
failure never makes successfully appended events pending again. A definitely
uncommitted failure leaves the same change set retryable. An unknown outcome
poisons the lifecycle until application reconciliation. Saving an aggregate
with no pending events is a successful no-op that neither appends nor
dispatches.

Caller-owned transactions use a two-phase application API instead of `Save`:

```go
plan, err := repository.PrepareSave(ctx, aggregate)
if err != nil {
    return err
}
if plan.Empty() {
    _, err = repository.ConfirmCommitted(aggregate, plan, nil)
    return err
}

writer, err := postgres.NewTx(tx, postgres.Config{Schema: "event_store"})
if err != nil {
    return err
}
staged, err := writer.Stage(
    ctx,
    plan.Stream(),
    plan.ExpectedVersion(),
    plan.PreparedMessages(),
)
if err != nil {
    return err // the aggregate change set remains pending
}

if err := tx.Commit(ctx); err != nil {
    _, unknownErr := repository.MarkCommitUnknown(
        aggregate,
        plan,
        staged,
        err,
    )
    return unknownErr
}

result, err := repository.ConfirmCommitted(aggregate, plan, staged)
if err != nil {
    return err
}

_, err = repository.DispatchCommitted(ctx, result)
return err
```

`PrepareSave` is pure with respect to storage and does not release changes.
`Stage` neither commits nor acknowledges the aggregate. `ConfirmCommitted`
validates the exact plan and stored messages before acknowledgement.
`MarkCommitUnknown` poisons the in-memory lifecycle until the application
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
historical data, preserve the schema version, and cannot substitute for an
upcaster; encoding always requires the canonical identity. Type
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
type UpcasterFunc func(UpcastEvent) ([]UpcastEvent, error)

func NewUpcastEvent(EncodedEvent, map[string]string) (UpcastEvent, error)
func NewUpcastRule(
    string,
    SchemaVersion,
    UpcasterFunc,
    ...UpcastRuleOption,
) (UpcastRule, error)
func NewUpcasterChain(...UpcastRule) (*UpcasterChain, error)
func (c *UpcasterChain) Upcast(UpcastEvent) ([]UpcastEvent, error)
```

Rules match one exact event name and schema version. Each callback receives
defensive encoded-event and metadata copies and may rename the event, change
payload or metadata, advance its schema, split it, or explicitly drop it. The
chain invokes a successful rule twice with identical immutable input and
rejects different output, so stateful or nondeterministic transforms fail
before their result is used.

A same-name transformation must strictly advance its schema version. Renames
must not revisit an identity already seen in that path. Unique rule identities,
maximum path steps, per-rule outputs, total work, and final segment counts
bound execution. Callback errors and panics become redacted `UpcastError`
values while retaining inspectable causes.

Dropping requires `AllowUpcastDrop` with a validated
`ReviewedDropPolicy` recording rationale, reviewer, and canonical review time.
The policy is operational evidence; upcasting still never rewrites stored
history. Repository integration attaches the stored source stream version and
deterministic zero-based segment coordinates after the final ordered output, so
splitting does not invent stored versions.

Upcasting never writes to the event store. Snapshot compatibility is checked
against the snapshot schema and source stream version before post-snapshot
events are read.

## Dispatch

```go
type ConsumerFunc func(context.Context, Delivery) error
type DeliveryFilter func(Delivery) bool

type Dispatcher interface {
    Dispatch(context.Context, []Delivery) error
}

func NewDelivery(Message, DeliveryMode) (Delivery, error)
func NewConsumer(string, ConsumerFunc, ...ConsumerOption) (Consumer, error)
func FilterDelivery(DeliveryFilter) ConsumerOption
func NewSyncDispatcher(...SyncDispatcherOption) (*SyncDispatcher, error)
func ContinueOnConsumerError() SyncDispatcherOption
```

The synchronous dispatcher preserves message order and consumer registration
order. Stop-on-first-error is the default. The explicit continue option invokes
the remaining selected consumers in order and returns a joined error that
preserves every cause for `errors.Is` and `errors.As`. Consumer and filter
panics are converted to a redacted `ConsumerError`; panic values and wrapped
application diagnostics are not printed.

Consumer identities are stable canonical tokens and duplicate registrations
are rejected. Filters are ordered ordinary functions and a false result
short-circuits the remaining filters for that consumer. An empty batch
succeeds without calling consumers. Cancellation stops new calls but does not
claim to interrupt a consumer that ignores its context.

The dispatcher is immutable after construction, safe for concurrent use, and
permits reentrant dispatch without a held lock. Application callbacks own any
synchronization required by their mutable state. Repository dispatch occurs
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

The generic control API pauses new work, reports one atomic state and
checkpoint, and compare-and-resets checkpoint state. It does not imply that an
application read model was reset. Callers drain in-flight work and explicitly
coordinate read-model replacement; capable durable adapters may provide a
stronger same-transaction operation.

Handler failures stop before checkpoint advancement by default. An optional
`PoisonPolicy` can explicitly stop or skip one failed replay delivery. A skip
is counted only after its checkpoint succeeds, and the policy owns no dead
letter, retry worker, transaction, or side effect.

Optional replay hooks run before the first cursor read and after a terminal
empty batch. They are explicit idempotent callbacks with panic containment,
not framework lifecycle discovery. `Controller.Rebuild` requires an already
paused runner and composes an application reset with expected checkpoint reset
without claiming atomicity or automatically resuming work.

Process managers are pure planners:

```go
type ProcessManager[Command any] interface {
    Plan(
        context.Context,
        Delivery,
    ) (processmanager.PlanResult[Command], error)
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
