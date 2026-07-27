# Event messages and metadata

An event message separates the domain event payload from the stable envelope
needed to store, evolve, correlate, route, and diagnose it. Event identity is
explicit data and never depends on Go package paths, concrete type names,
reflection output, or symbol names.

## Envelope lifecycle

Application repository composition supplies:

- message ID through the configured generator;
- aggregate type through repository configuration;
- aggregate ID through the application ID encoder;
- event name, schema version, and payload through the payload codec;
- recorded time through the configured clock;
- optional correlation, causation, tenant, partition, and application metadata
  through `MessageContext`; and
- optional metadata changes through ordered decorators.

The event store supplies the positive stream version and, when supported, a
positive global position. `PendingMessage` represents the validated envelope
before those positions are assigned. `Message` represents the persisted
envelope.

The zero value of `MessageID`, `StreamID`, `EventName`, `EncodedEvent`,
`PendingMessage`, and `Message` is intentionally unassigned and rejected where
a complete value is required. A zero global position means the store does not
provide global ordering.

## Stable identity

Aggregate types and event names are lowercase dotted names. Segments begin
with a letter and may contain lowercase letters, digits, hyphens, and
underscores. Use business identities such as `billing.account` and
`account.owner-changed`, not Go type names.

Message IDs are bounded canonical tokens. Aggregate IDs are application-defined
bounded UTF-8 text without control characters. Applications may use UUIDs,
ULIDs, numeric encodings, or domain identifiers, but must choose one canonical
encoding and preserve it for the stream's lifetime.

Event schema versions are positive `uint32` values. A rename uses an explicit
codec alias for stored identity compatibility or an upcaster when the logical
identity must change. Renaming a Go type alone never changes stored identity.

## Ownership and immutability

Constructors copy payload bytes and metadata maps. Accessors that expose them
return defensive copies. Store implementations and adapters must apply the
same rule: callers may reuse or mutate their input buffers after a successful
call, and mutating returned buffers must not alter a message.

Messages contain no mutable slices or maps observable without a copy. Values
are therefore immutable by contract and safe to pass between synchronous
components. This does not make application event values returned by a payload
codec immutable; applications own those Go values.

`Message.Equal` compares every observable envelope field: message and stream
identity, stream version, event name, schema version, content type, payload
bytes, metadata, normalized time, correlation, causation, tenant, partition,
and global position. Tests should use it only when exact envelope equality is
the contract. Domain assertions should normally compare stable event identity
and the decoded payload fields that matter.

## Canonical validation limits

The core enforces these byte limits before storage or adapter encoding:

| Field | Limit |
| --- | ---: |
| aggregate type | 255 |
| aggregate ID | 512 |
| event name | 255 |
| message, correlation, or causation ID | 128 |
| canonical content type | 128 |
| encoded payload | 1 MiB |
| metadata entries | 64 |
| metadata key | 128 |
| metadata value | 4 KiB |
| combined metadata keys and values | 64 KiB |
| tenant | 255 |
| partition | 255 |

Payloads must be non-empty. Times must be assigned and are normalized to UTC
at microsecond precision. Content types must be canonical media types.
Application metadata keys are canonical tokens and values are bounded valid
UTF-8 without control characters.

Adapters may impose a stricter total envelope or transport limit, but must
validate it before allocation or publication and document that boundary.

## Reserved fields and decoration

Typed envelope fields are not duplicated in application metadata. Every
metadata key beginning with `es.` in any ASCII case is reserved and rejected.
This prevents application metadata from shadowing protocol fields.

`MetadataDecorator` rejects collisions rather than silently overwriting an
existing key. Custom decorators may replace the complete metadata map through
`WithMetadata`, but the decorator chain rejects changes to any other envelope
field. Decorators run before persistence and must be deterministic, bounded,
and side-effect free unless their explicitly injected dependency defines the
effect, such as a clock or ID generator.

Correlation identifies one wider operation. Causation identifies the message
or command that directly caused this event. They are typed optional message
IDs, not magic metadata keys. Tenant and partition are optional routing or
isolation hints; the package does not prescribe multitenancy or authorize
using either field as the sole security boundary.

## Diagnostics and privacy

Validation errors identify the invalid field and rule without formatting its
value. Message string forms include stable identities, versions, and byte or
entry counts but omit payload bytes and metadata values. Consumer, codec,
upcaster, decorator, and adapter errors preserve inspectable causes while their
public diagnostics must not expose payloads, metadata, credentials, tenant
data, or panic values.

`MessageVerifier` is the application hook for authenticating or otherwise
checking complete stored envelopes. `VerifyingEventStore` and
`VerifyingGlobalReader` invoke it before a stream or global iterator exposes a
message. Verification failures and panics are redacted and terminal for that
iterator. The stream decorator deliberately does not invoke the read verifier
during append: applications own signing or integrity metadata through their
explicit write pipeline, and verification can depend on store-assigned
positions.

Identifiers and event names may still be sensitive in a particular domain.
Applications must review their logging and telemetry policy and may need to
hash or omit them at external diagnostic boundaries. The optional telemetry
adapter deliberately uses finite-cardinality operation attributes rather than
event or aggregate identities.
