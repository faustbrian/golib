# Security, privacy, and observability review

## Scope and conclusion

This review covers the core event model, JSON/HTTP/Kafka formats and bindings,
schema-validation boundary, and `adapters/golib`. The packages do not create
logs, spans, baggage, or metrics. They return values and errors; applications
and observability adapters therefore own every emission decision.

CloudEvents context and transport metadata are producer assertions, not
authenticated identity. Payload bytes, attribute values, extension values,
schema responses, and retained adapter state are sensitive by default. No
value from an untrusted event is suitable as an unrestricted metric label.

This policy covers this module's behavior only. It does not claim that a
logger, OpenTelemetry provider, Kafka client, schema resolver, registry,
event-store, queue, workflow engine, or other downstream consumer applies the
same controls.

## Data inventory and trust classification

| Data | Where represented or propagated | Classification and boundary |
| --- | --- | --- |
| `id` | Event context; HTTP `ce-id`; Kafka `ce_id`; adapter stable IDs | Potentially identifying and unbounded-cardinality. Preserve on the wire; do not emit by default. |
| `source` | Event context; HTTP/Kafka context metadata | URI-reference controlled by the producer. It can contain path/query/user-info secrets. Syntax validation does not make it safe to dereference, log, or label. |
| `type` | Event context; HTTP/Kafka context metadata; selected envelope mappings | Producer-controlled taxonomy. It is an allowed metric dimension only through an application-owned finite allowlist; otherwise map to `other`. |
| `subject` | Optional event context; event-sourcing stream and workflow mappings | Commonly identifies a resource, account, stream, or person. Sensitive and unbounded-cardinality; forbidden from default telemetry. |
| `dataschema` | Optional absolute URI; explicit schema-validator input | May disclose internal hosts, paths, versions, or query data. It is never fetched by decoding. Do not emit the URI; use a caller-owned finite schema family/version code if needed. |
| `datacontenttype`, `specversion`, content mode | Context or binding metadata | Suitable only after parsing and normalization to a finite allowlist. Raw media-type parameters remain producer-controlled and are not metric labels. |
| `time` | Optional event context | Usually low sensitivity but may reveal business timing. Emit only when the telemetry purpose requires it; never use the exact timestamp as a metric label. |
| Unknown extension names and values | Event extension map; structured members; `ce-*`/`ce_*` transport metadata | Both are untrusted. Values are sensitive. Names are bounded by decoder limits but remain attacker-controlled and may encode identifiers; do not use raw names or values as metric labels. |
| `correlationid`, `causationid`, `requestid` | Golib extensions; event-sourcing/queue retained metadata | Operational identifiers with unbounded cardinality. Adopt only through the trusted extraction path. They may be used in access-controlled logs or span attributes when an incident/debugging policy explicitly allows it, but never in metric labels or baggage. |
| `tenantid` | Golib extension; event-sourcing/queue/audit mappings | Routing assertion, not authentication or authorization. Treat as confidential identifying data. Trusted extraction still requires application authentication and authorization. Raw tenant IDs are forbidden in metric labels and baggage. |
| `traceparent`, `tracestate` | Selected extensions; Golib propagation policy | Trace identifiers and vendor state. The adapter copies only these two selected fields and delegates trust decisions to the caller-owned propagation policy. Do not log either value or use it as a metric label. |
| `baggage` | Caller context only | No selected CloudEvents extension. Injection reports explicit loss and does not flatten baggage into the event. The adapter does not extract baggage from CloudEvents. |
| `auditid`, `auditaction`, `auditoutcome` | Golib audit metadata subset | Non-authoritative assertions. `auditid` is unbounded and sensitive. `auditaction` and `auditoutcome` may be metrics only through finite application-owned enumerations. Actor, audit subject, changes, policy, integrity, attributes, description, and recording time remain outside CloudEvents. |
| `eventschema`, `partitionkey` | Golib event-sourcing/Kafka mappings | `eventschema` is a version assertion; parse or bucket before telemetry. A partition key may identify a tenant, aggregate, or account and is forbidden from default telemetry and metric labels. |
| Payload/data bytes | `Data`; HTTP body; Kafka value; queue/outbox/event-store/workflow payloads; schema-validator input | Secret-bearing by default. Never log, trace, add to baggage, or place in metric labels. Emit only byte counts after limits have been applied. |
| HTTP metadata | Content type and `ce-*` headers | Header values are untrusted and may be sensitive. The decoder consumes only binding-owned metadata but does not sanitize caller logging of the original request. Authorization, cookies, and unrelated headers are outside this module. |
| Kafka metadata | Key, value, headers, topic/partition/offset/timestamp retained by the Golib adapter | Key and arbitrary transport headers may contain identifiers or credentials. CloudEvents decoding bounds copied key/header metadata. Topic/partition may be emitted only through deployment-owned finite allowlists; offset and timestamp are not metric labels. |
| Envelope state | Event-store metadata, queue tags/retry/timeout/trace maps, outbox topic/version, workflow details, Kafka transport state | Retained out of band to preserve ownership and conversion fidelity. It is not made safe for telemetry by conversion. Apply the source package's privacy policy before emission. |

## Allowed and forbidden observability emissions

The default is **deny** for producer-controlled values.

### Logs and error reports

Allowed without event values:

- stable error class (`invalid_event`, `limit_exceeded`, `metadata_collision`,
  `untrusted_metadata`, `schema_mapping`, or equivalent application code);
- bounded field code from `ValidationError.Issues`, after treating an unknown
  extension field name as untrusted text;
- normalized binding (`json`, `http_binary`, `http_structured`,
  `http_batch`, `kafka_binary`, `kafka_structured`);
- bounded sizes and counts, such as payload bytes, header count, and batch
  count; and
- cancellation/timeout state without URI, payload, or metadata values.

Forbidden by default:

- payload bytes or decoded payload fields;
- raw standard or extension attribute values;
- raw HTTP headers, Kafka headers, Kafka keys, registry requests/responses, or
  schema documents;
- tenant, subject, source, schema URI, partition, trace, correlation,
  causation, request, audit, or event identifiers; and
- formatting an entire `Event`, adapter state, record, message, request, or
  validation input into a diagnostic.

Core validation diagnostics contain stable field/code pairs rather than
rejected values. Some errors intentionally contain a bounded field or header
**name** to identify a collision. Names remain untrusted and must be escaped by
the log sink. Schema validators and registry/cache adapters can return errors
from caller-supplied or downstream implementations; this module does not
guarantee those external error strings are value-free. At an observability
boundary, classify with `errors.Is`/`errors.As` and emit a stable local code
instead of the complete delegated error unless that implementation has been
separately reviewed.

### Traces and baggage

- Trace linkage may use the OpenTelemetry span context created by the
  caller-configured propagation policy. Do not duplicate `traceparent` or
  `tracestate` as span attributes.
- Span attributes follow the same deny-by-default rule as logs. Binding mode,
  bounded byte/count measurements, and finite application-owned type/schema
  categories are allowed. Payloads and raw identifiers are forbidden.
- Baggage must not be synthesized from event extensions. In particular,
  `tenantid`, subject, source, schema URI, event ID, correlation IDs, audit IDs,
  extension names/values, payload data, Kafka keys, and transport headers must
  not be added to baggage.
- A trusted propagation policy controls inbound trace adoption. Trust means
  the caller has accepted the transport boundary; it does not make trace or
  vendor state suitable for logs, metrics, or authorization.

### Metrics and cardinality

Metric attributes must have a documented finite domain. The following are the
only generally safe dimensions after normalization:

- operation (`encode`, `decode`, `validate`, `convert`);
- format/binding/content mode from a fixed enumeration;
- success/failure and a fixed local error class;
- stable specification version;
- normalized media-type family from a fixed allowlist; and
- application-owned event-type, schema-family, topic, or audit-action buckets
  only when the complete allowed set and an `other` bucket are configured.

Raw `id`, `source`, `type`, `subject`, `dataschema`, timestamps, extension
names or values, tenant/correlation/causation/request/audit identifiers,
trace/vendor state, baggage, partition keys, Kafka keys, topics, partitions,
offsets, header values, payload fields, URIs, and error strings are forbidden
metric labels. Counts and byte sizes are measurements, not labels. New labels
require a cardinality budget, a finite-domain proof, and a privacy review.

## Trust and redaction boundaries

1. **Decode boundary:** limits are required and applied before retaining
   event, payload, HTTP, and Kafka metadata. Successful parsing proves syntax,
   not provenance or authority.
2. **Canonical event boundary:** `Event` owns copies of its data and extension
   map. Accessors intentionally expose values to application code; callers
   must not interpret immutability as redaction.
3. **Golib extraction boundary:** correlation, tenant, trace, and audit
   adoption requires the explicit trusted path or a configured propagation
   policy. Collision checks prevent silent replacement. Tenant metadata still
   requires authentication and authorization outside this module.
4. **Conversion boundary:** reports name fields that cannot be represented;
   they do not contain the lost values. Retained state remains caller-owned and
   sensitive.
5. **Schema boundary:** receipt and decoding perform no schema I/O.
   `ValidateSchema` is explicit. The registry adapter requires an exact
   caller-owned URI-to-lookup allowlist, positive timeout, configured cache and
   adapter, and explicit availability policy. Resolver/cache/registry security
   remains the responsibility of those supplied implementations.
6. **Observability boundary:** applications must classify errors and select
   fields before calling a logger, tracer, or meter. Redaction after exporting
   is too late.

## Executable evidence

The following tests exercise the reviewed boundaries:

- `TestNewEventReportsCanonicalValidationIssues` asserts the exact,
  deterministic value-free validation message and field/code diagnostics.
- `TestDecodeJSONEnforcesDecodedAndExactResourceBoundaries`,
  `TestDecodeHTTPBoundsContextAttributeNamesInEveryMode`, and
  `TestDecodeKafkaBoundsAllCopiedRecordMetadata` exercise bounds on payload,
  attribute, HTTP, Kafka-key, and Kafka-header data before retention.
- `TestTracingExtensionsRejectEveryGrammarBoundary` and
  `TestTraceStateAcceptsExactNormativeBoundaries` exercise bounded selected
  trace-context syntax.
- `TestMetadataAdapterRejectsEveryMalformedOrUntrustedBoundary` proves that
  correlation and tenant metadata are rejected when malformed and are not
  silently accepted through an invalid boundary.
- `TestTelemetryAdapterCoversNilCollisionBaggageAndTrustedExtraction` proves
  collision rejection, explicit baggage loss, and caller-selected trusted
  trace extraction.
- `TestAuditMetadataAdapterSelectsSafeFieldsAndRequiresTrust` proves that only
  the selected audit subset is emitted and that inbound adoption requires an
  explicit trust decision.
- `TestRegistryJSONSchemaValidatorRejectsUnmappedEventSchemaWithoutResolution`
  proves an event-controlled, unmapped schema URI cannot trigger resolution.
- `TestRegistryJSONSchemaValidatorRequiresBoundedExplicitConfiguration` and
  `TestRegistryJSONSchemaValidatorBoundsResolutionAndPreservesParentCancellation`
  prove explicit cache/adapter/policy/timeout configuration and bounded,
  cancellation-aware resolution.
- `TestKafkaAdapterPreservesTransportOwnershipAndBindingRoundTrip` and
  `TestKafkaAdapterRejectsTransportCollisions` prove that arbitrary Kafka
  transport metadata remains explicitly owned and cannot overwrite
  CloudEvents binding metadata.

These tests establish package behavior, not the privacy behavior of consumers
that receive returned events, state, errors, or contexts.

## Review checklist for consumers

- Configure limits no larger than the surrounding transport boundary.
- Authenticate the transport and authorize tenant/resource access before
  trusted extraction.
- Use a finite observability schema and map all unknown values to `other`.
- Log stable error classes and field codes, not complete events or delegated
  schema/resolver error strings.
- Keep payload, Kafka key/header, schema, trace/vendor, and retained envelope
  state out of telemetry unless a separately approved, access-controlled
  diagnostic workflow requires a narrowly redacted value.
- Apply retention, access-control, deletion, and regional policies to telemetry
  independently of CloudEvents transport retention.
