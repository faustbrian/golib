# Conversion policy

CloudEvents is never Golib's canonical internal envelope. A conversion adapter
must expose its ownership decisions and return a structured report containing
every field it could not represent. Silent dropping and overwriting are not
permitted.

## Reserved ownership

| Meaning | CloudEvents owner | Canonical owner outside CloudEvents | Rule |
| --- | --- | --- | --- |
| stable event identity | `id` | event-sourcing message ID or outbox envelope ID | preserve the canonical stable ID; report a collision when both populated values differ |
| producer context | `source` | application boundary | require an explicit mapping; never infer from a broker address |
| event classification | `type` | domain event name and schema version | use a documented reversible name/version mapping or report version loss |
| occurrence time | `time` | domain occurrence time | do not substitute queue enqueue, outbox creation, broker, or audit-record time |
| payload media type | `datacontenttype` | canonical payload codec | preserve with exact payload bytes where the target supports them |
| schema reference | `dataschema` | schema policy or registry subject/version | preserve only a URI; registry IDs and compatibility policy stay out of band |
| aggregate/entity scope | `subject` | stream or domain entity identity | require an escaping and collision policy when composing multiple fields |
| correlation and causation | selected application extensions | `correlation` types | adopt only after the transport trust policy approves inbound metadata |
| tenant | application extension | `tenancy.TenantID` | conversion does not authenticate or authorize the tenant |
| trace context | `traceparent`, `tracestate` | OpenTelemetry/W3C propagation | never synthesize trace IDs from correlation IDs |
| partition hint | `partitionkey` | Kafka key/partition or queue ordering key | opt-in hint only; explicit partitions and ordering state remain transport metadata |
| audit data | application extensions or payload | `audit.Record` | integrity, actor, policy, and disclosure state are not flattened implicitly |

## Transport mappings

The official HTTP and Kafka bindings are implemented by the core package.
Queue and outbox have no selected official CloudEvents binding and must be
named **Golib queue mapping** and **Golib outbox mapping** in APIs and
documentation.

A Kafka adapter may map `partitionkey` to a record key only when the caller
opts in. An existing record key is transport-owned and cannot be overwritten
silently. Explicit partition, topic, timestamp, retries, ordering, offset,
headers not owned by CloudEvents, and settlement state remain out of band.

An outbox adapter must preserve relay attempts, availability, creation time,
topic, ordering, idempotency, and arbitrary non-CloudEvents metadata outside
the CloudEvent. Event-sourcing adapters likewise retain stream identity,
stream version, global position, recorded time, and application metadata in
their canonical envelope.

Workflow adapters retain workflow/run/activity identity, execution state,
timeouts, retries, and compensation state outside CloudEvents. Telemetry and
audit adapters attach or extract explicitly approved metadata; they never turn
telemetry or audit records into proof of domain-event equivalence.

## Round-trip levels

Adapters must state one of these levels for each direction:

1. **Exact payload**: payload bytes and all canonical metadata are unchanged.
2. **Semantic event**: the CloudEvent information model is unchanged, but an
   event format may be reserialized deterministically.
3. **Loss reported**: every unrepresentable or collided field is listed and the
   caller decides whether to proceed.

Core JSON, batch, HTTP, and Kafka encoders use the same rule. `EncodeJSON`,
`EncodeJSONBatch`, `EncodeHTTP`, and `EncodeKafka` reject implicit changes with
`ErrConversionLoss`. Their `Encode*WithReport` variants return a deterministic
`ConversionReport` when a caller explicitly accepts a target-format
normalization or metadata materialization. Present JSON, text, and binary data
without a declared content type materialize `application/json`, `text/plain`,
and `application/octet-stream`, respectively, when a binding needs that value
to preserve the runtime data kind. An explicit content type that contradicts
the runtime data kind is also a reported loss and is rejected by strict
encoding.

No adapter may claim an exact round trip when it reassigns an ID, substitutes a
timestamp, infers a schema, coerces data kinds, overwrites reserved metadata,
or drops transport or canonical-envelope state.
