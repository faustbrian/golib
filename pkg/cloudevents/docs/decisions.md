# Interoperability decisions

Normative prose in the pinned specification outranks examples, schemas, and SDK
behavior. These decisions cover places where the stable documents leave a
runtime or conflict policy to implementations.

The canonical, stable, evidence-linked decisions are maintained in the
[specification decision register](specification-decisions.md). This overview
groups the same policies by adoption concern; it does not override the register.

## Event and data ownership

- Events and attribute values are immutable values. Constructors and decoders
  copy caller-owned maps and byte slices; accessors return copies.
- Absent data, JSON `null`, empty text, and empty binary data are distinct.
- The core stores payload bytes and runtime data kind. It never fetches a schema,
  resolves a URI, opens a broker connection, or starts a goroutine.
- JSON input without `datacontenttype` implies JSON while it remains in the JSON
  format. Conversion to a binary binding materializes `application/json` so the
  meaning is not lost.

## Attributes and unknown extensions

- Unknown valid extensions are retained. JSON Boolean and Integer values retain
  their abstract types; JSON strings decode as String because an unknown
  extension's URI, URI-reference, Timestamp, or Binary semantics cannot be
  inferred without its specification.
- Callers constructing a known extension may retain its stronger abstract type.
  A JSON round trip guarantees the canonical extension value, not an
  unregistered semantic type that JSON cannot identify.
- Duplicate context attributes are rejected even when their values are equal.
  Null optional attributes are treated as absent. Null required attributes are
  invalid.
- Diagnostics contain field names and stable reason codes, never rejected
  values or payload bytes.
- URI and URI-reference values must already use RFC 3986 ASCII serialization.
  Parsers do not silently percent-encode spaces or Unicode and call the result
  equivalent to the supplied value.

## JSON determinism

JSON object members are emitted in lexicographic order, insignificant
whitespace is removed, timestamps use UTC RFC 3339 with only necessary
fractional digits, integers use base 10, and binary values use padded RFC 4648
base64. This is Golib policy, not a CloudEvents conformance requirement.

## Binding conflicts

- Header names are compared case-insensitively. Multiple occurrences of a
  singleton CloudEvents or content-type header are rejected, even if equal.
- Structured metadata is owned by the event-format payload. Any redundant
  CloudEvents protocol metadata must decode to the same canonical value or the
  message is rejected as conflicting.
- Unsupported structured media types return an unsupported-format error. The
  decoder does not silently reinterpret them as binary events.
- Decoders never close caller-owned request, response, or record resources.
  Helpers that create an HTTP message transfer ownership of their created body
  to that message.
- CloudEvents context attributes are read from HTTP header fields, as required
  by the pinned binding. HTTP trailer fields remain caller-owned transport
  metadata and are not interpreted as CloudEvents context attributes.
- HTTP binary mode treats an empty body with a non-JSON data content type as
  present empty binary data. With neither body bytes nor a data content type,
  data is absent. A JSON data content type still requires a valid JSON value.
- Kafka binary mode represents absent data with a nil record value, which is a
  tombstone on compacted topics. Structured events always have a non-nil value.

HTTP cancellation is checked before and after the bounded body read. Prompt
interruption requires a reader whose own `Read` operation observes
cancellation; the package does not start a goroutine to force cancellation of
an arbitrary blocking reader.

Schema validation rejects nil contexts and nil or typed-nil validators before
calling application code. Receiving and decoding an event never invokes a
validator.

The package does not compress, retry, write to streams, manage brokers, or own
transport shutdown. Partial-write, compression, retry, and shutdown behavior
therefore belongs to the caller's HTTP, Kafka, queue, or broker integration;
the binding helpers return complete owned byte slices or transport-neutral
records. Fault coverage at this boundary exercises short/failed reads,
cancellation, malformed records, and ownership without implying lifecycle
guarantees for an external transport.

## Transport and conversion boundary

Kafka keys and partitions, HTTP targets and methods, queue delivery fields,
outbox state, event-store sequence and aggregate identity, workflow execution
state, retries, acknowledgements, audit policy, and broker settlement are not
CloudEvents context attributes. Adapters preserve them out of band unless an
explicit selected extension owns the meaning. A conversion that cannot retain a
declared field returns structured loss information rather than silently dropping
or overwriting it.
