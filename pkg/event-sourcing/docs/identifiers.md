# Aggregate identifiers and UUID encoding

Aggregate identifiers are application values. The core repository is generic
over the identifier type and requires an explicit `IdentifierEncoder[ID]` to
produce the stable stream identifier stored in every message.

The core deliberately does not depend on a UUID package and does not infer an
identifier from `String`, `fmt`, reflection, a Go type name, or memory layout.
Applications may use UUIDs, integers, domain strings, or custom value types as
long as their encoder produces one canonical bounded UTF-8 representation.

## Canonical encoding

An encoder must:

- reject its invalid or unassigned identifier values;
- return the same bytes for the same domain identifier for the lifetime of the
  event history;
- return only bounded UTF-8 text accepted by `NewStreamID`;
- avoid locale, process, machine, package, and compiler-dependent formatting;
- never embed secrets or mutable display names; and
- remain stable across application and library releases.

For UUIDs, canonical lowercase `8-4-4-4-12` hexadecimal text is a pragmatic
default:

```go
type AccountID [16]byte

func encodeAccountID(id AccountID) (string, error) {
	if id == (AccountID{}) {
		return "", eventsourcing.ErrInvalidArgument
	}
	// Encode all 16 bytes explicitly as lowercase 8-4-4-4-12 text.
	return canonicalUUIDText(id), nil
}
```

The application owns parsing and construction of `AccountID`. If identifiers
arrive as text, parse them once at the application boundary and reject
noncanonical case, separators, whitespace, invalid hexadecimal data, and the
zero value according to domain policy.

The repository calls the encoder for both save and load. An encoding error is
returned before stream I/O. Stored messages expose only the encoded string
through `StreamID.AggregateID`; they do not reconstruct the application value.

## Ordering

Identifier ordering is not event ordering. Stream versions order events within
one aggregate, and a capable store's global positions order committed messages
across streams.

Canonical UUID text made from the bytes in network order has the same lexical
order as those bytes because separators occur at fixed positions. Applications
may rely on that only when their own identifier contract explicitly defines it.
The event-store API does not promise enumeration or chronological ordering by
aggregate identifier.

## PostgreSQL representation

The first-party PostgreSQL event schema stores `aggregate_id` as text. This
keeps the storage contract compatible with every validated application ID and
avoids imposing UUID semantics on the core module.

Applications may use PostgreSQL `uuid` columns in their own read models or
domain tables. Convert through the same canonical application codec at that
boundary. Do not expose PostgreSQL row or UUID types through the core event
store interfaces.

Choosing a native UUID column for a custom event store is also valid, but that
store owns conversion, constraint, index, backup, restore, and migration
semantics. It must still return the same canonical `StreamID` values required
by the public conformance suite.

## Message identifiers

EventSauce applies its UUID encoder to event message IDs as well as aggregate
root IDs. In Go, `MessageID` is a separate stable canonical token supplied by
the application or an injected generator. The first-party random generator
encodes 128 random bits as 32 lowercase hexadecimal characters; it does not
impose UUID version or layout semantics.

An importer must preserve each source message's identity through one reviewed
conversion and must not generate replacement IDs. Duplicate-ID detection,
causation references, idempotency, and operational reconciliation depend on
that value remaining stable. The same binary and string migration fixture used
for aggregate IDs proves the canonical UUID text is accepted as a `MessageID`.

## Migrating EventSauce histories

EventSauce's UUID encoding package defines `BinaryUuidEncoder` as the UUID's
raw bytes and `StringUuidEncoder` as its textual form. Custom encoders are also
allowed. Compatibility here is conceptual, not automatic wire or schema
compatibility. Before importing an existing history:

1. pin the source repository and UUID codec version;
2. decode each source identifier with that codec;
3. encode it with the reviewed Go application encoder;
4. verify a one-to-one mapping with no collisions or rejected values;
5. preserve event names, schema versions, message IDs, stream versions, and
   recorded times independently of identifier conversion;
6. compare per-stream counts and hashes before cutover; and
7. retain a rollback copy and the exact migration tool provenance.

Do not reinterpret raw binary UUID bytes through host endianness or a Go struct
layout. UUID variants that reorder timestamp fields for storage require the
matching source decoder before canonical encoding.

Review the pinned source repository and the
[EventSauce 3.9.1 UUID encoding documentation](https://github.com/EventSaucePHP/EventSauce/blob/33ea9b97ec3ac56991caad03b791fee418a43e41/docs/docs/message-storage/uuid-encoding.md)
used by the history. The executable migration fixture in `identifier_test.go`
proves that raw 16-byte and canonical string source values converge on the same
Go application identifier while malformed and zero binary values fail closed.

Changing the encoder for an existing stream changes its identity. Treat such a
change as an explicit data migration, not a refactor. Dual-read or alias logic
belongs in an application repository adapter with a bounded retirement plan;
the core does not maintain a global identifier alias registry.

## Security and privacy

Aggregate identifiers appear in envelopes, indexes, diagnostics, traces, queue
records, and backups unless an adapter documents otherwise. Use opaque IDs
when natural identifiers contain personal or sensitive data. Redaction cannot
make an identifier private after it has become persistent stream identity.
