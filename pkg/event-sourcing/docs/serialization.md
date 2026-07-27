# Serialization and schema evolution

Payload serialization and message serialization are separate boundaries.
`PayloadCodec` converts between an application event value and an
`EncodedEvent`. Stores persist the complete message envelope. Transport
adapters provide their own bounded message codecs without coupling aggregate
code to JSON, PostgreSQL, Kafka, queues, or outbox schemas.

## Stable registration

The first-party JSON codec requires every event name and schema version to be
registered with its Go value type:

```go
codec, err := eventsourcing.NewJSONCodec(
	eventsourcing.JSONEvent[AccountOpened]("account.opened", 1),
	eventsourcing.JSONEvent[OwnerChanged]("account.owner-changed", 2),
	eventsourcing.JSONAlias(
		"account.renamed",
		2,
		"account.owner-changed",
		2,
	),
	eventsourcing.WithJSONStrictDecoding(),
)
```

Construction rejects invalid identities, duplicate event or alias claims,
self-aliases, version-changing aliases, missing alias targets, duplicate
strict-mode options, and an empty registry. The completed codec is immutable
and safe for concurrent use.

Aliases are decode-only names for the same schema. They preserve the schema
version and do not modify stored history. Use an upcaster when payload,
metadata, event name, or schema version must change.

## JSON behavior

Encoding uses `encoding/json` for the exact registered Go type and emits
canonical content type `application/json`. A value of another type fails with
`ErrEventTypeMismatch`. An unregistered event name fails with
`ErrUnknownEvent`; a registered encode name with an unsupported schema version
fails with `ErrIncompatibleVersion`. Decode-only aliases remain unavailable to
encoding.

Decoding validates before constructing the Go value:

- payload bytes must be valid UTF-8 and within the envelope limit;
- exactly one JSON value is accepted, with no trailing data;
- duplicate object keys are rejected;
- nesting is limited to `MaxJSONDepth`;
- each object or array is limited to `MaxJSONContainerEntries`;
- numbers are parsed with `json.Number` before assignment;
- an unsupported content type fails explicitly;
- an unknown event name fails with `ErrUnknownEvent`, while a known canonical
  or alias name at an unsupported version fails with
  `ErrIncompatibleVersion`; and
- strict mode rejects fields absent from the registered Go type.

Use integer Go fields for integer domain data. Avoid decoding important numbers
through `float64`. Use explicit string or integer representations for decimal
values whose scale is part of the contract. `time.Time` follows Go's JSON time
format; applications that require another stable precision or representation
should define a dedicated value type and codec.

JSON object key ordering emitted by the Go standard library is deterministic,
but semantic compatibility must not depend on insignificant whitespace or
object-member order. Golden fixtures should pin the bytes only when the exact
wire representation is a documented contract.

## Custom codecs

Protobuf, MessagePack, or another format implements the two-method
`PayloadCodec` contract. A custom codec must:

- use explicit stable event names and positive schema versions;
- return a canonical bounded content type;
- own input and output bytes according to the message contract;
- reject unknown and malformed input without panic;
- preserve exact domain integers and time semantics;
- be deterministic and safe for concurrent use; and
- return errors compatible with the core error categories where applicable.

Aggregate, repository, and store contracts do not change when the codec
changes. Persisting multiple formats in one store is possible because content
type is part of each encoded event, but the selected application codec must
know how to decode every retained history.

Codecs may additionally implement `ContextPayloadCodec`. Aggregate repositories
prefer its `EncodeContext` and `DecodeContext` methods so cancellation, tracing,
and other operation-scoped signals reach decorators without changing encoded
data. Implementations must not retain the context or use context values to make
serialization output nondeterministic. Existing two-method codecs remain fully
supported.

## Upcasting

Upcasters transform encoded historical events at the repository read boundary.
They never rewrite stored rows. A rule matches one exact event name and schema
version and returns zero, one, or many logical events. This supports:

- payload and metadata migration;
- event renaming;
- monotonic schema-version advancement;
- splitting one historical event into several logical events; and
- dropping an obsolete event only with `ReviewedDropPolicy`.

Rules compose in `UpcasterChain`. Each path must advance identity, may not
cycle, and is bounded by `MaxUpcastSteps`, output-segment limits, and total
work limits. The chain invokes a rule twice on defensive copies and rejects
different outputs with `ErrNonDeterministicUpcast`. Panics are contained and
reported without exposing payload, metadata, or panic values.

`EventDecoder` is the reusable read-boundary composition of a `PayloadCodec`
and the small `Upcaster` contract implemented by `UpcasterChain`. It returns
ordered `LogicalEvent` values for one persisted
source message. Each value exposes the decoded event, transformed metadata,
source message, and split segment coordinates. A reviewed drop returns no
logical events. Aggregate repositories use this same decoder internally;
projection and controlled-replay handlers can use it directly without
reimplementing evolution rules.

`DecodeContext` propagates cancellation and operation context through optional
`ContextPayloadCodec` and `ContextUpcaster` extensions. The original `Decode`
method remains available and uses a background context. `UpcasterChain`
implements both upcaster contracts and checks cancellation before deterministic
transformation.

Custom `Upcaster` implementations own the same safety contract as the chain:
deterministic ordered output, bounded work and output, independent ownership,
concurrency safety, and errors rather than panics for hostile stored input.

For a split during projection replay, process every returned logical event
inside one projection handler call. The runner checkpoints the persisted source
message only after that call succeeds. A retry can repeat earlier logical
segments, so projection updates remain idempotent.

An alias is appropriate when old and new names have the same schema. An
upcaster is appropriate when the logical event changes. An anti-corruption
translator is appropriate at an integration boundary after persistence. These
mechanisms are intentionally distinct.

## Evolution workflow

For every event change:

1. retain the old registration or readable stored identity;
2. add the new explicit name/version registration;
3. add an alias or monotonic upcast rule;
4. add golden old-history fixtures and round-trip tests;
5. prove live execution and replay reach equivalent aggregate state;
6. prove snapshot restoration upcasts newer events after restored state;
7. verify projection replay decodes all logical segments before checkpointing;
   and
8. deploy readers before writers when compatibility requires it.

Never mutate an existing event's meaning while keeping its name and version.
Never remove a decoder while retained history or backups still contain that
identity. Snapshot schema evolution is separate from event schema evolution;
an incompatible snapshot may be discarded and rebuilt, but event history
remains authoritative.

## Security and diagnostics

Codecs and upcasters process stored hostile input. They must bound allocation,
depth, collection sizes, and outputs before construction. Errors may identify
event name and schema version but must not include payload bytes, metadata
values, tenant data, credentials, or recovered panic values.

Schema evolution does not solve retention or erasure. Avoid placing secrets or
unnecessary personal data in immutable events. Encryption, key rotation,
cryptographic shredding, legal holds, and deletion policy remain application
and operational responsibilities.
