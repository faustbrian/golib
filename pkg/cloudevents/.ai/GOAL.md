# Goal: CloudEvents Interoperability

## Objective

Build `cloudevents` as a complete, interoperable Go implementation of the
stable CloudEvents specification, its normative event formats, and the protocol
bindings selected for Golib's HTTP, Kafka, queue, outbox, event-sourcing, and
workflow ecosystem.

At implementation time, pin exact CloudEvents specification, SDK, binding,
format, extension, registry, conformance-kit, and errata revisions. The package
MUST NOT claim support for an extension or binding without complete normative
coverage and interoperability evidence.

Authoritative starting points:

- https://github.com/cloudevents/spec
- https://github.com/cloudevents/sdk-conformance
- https://github.com/cloudevents/sdk-go

## Product Boundary

CloudEvents is an interoperability envelope. It MUST NOT replace richer
canonical domain, event-sourcing, outbox, queue, Kafka, workflow, audit, or
application envelopes. Conversions MUST document loss, reserved-field
collisions, metadata ownership, and round-trip guarantees.

The core MUST remain transport-independent and must not require brokers,
network access, global registries, background goroutines, or telemetry.

## Specification Commitment

Create a normative matrix for:

- the core CloudEvents specification and context attributes;
- structured and binary content modes;
- the normative JSON event format and batch format where standardized;
- HTTP protocol binding;
- Kafka protocol binding when selected by the official project;
- distributed tracing and partitioning extensions selected for support;
- data content type, schema URI, subject, time, source, type, ID, and extension
  attribute semantics;
- current official conformance tests and verified errata.

Normative prose outranks examples and third-party SDK behavior. Ambiguities
MUST receive explicit decisions and differential tests.

## Event Model And Encoding

Represent required, optional, and extension context attributes without losing
unknown valid extensions. Define validation, Unicode, URI, timestamps, media
types, exact values, absent/null/empty data, `data` versus encoded data,
ownership, canonical diagnostics, and strict byte/depth/attribute limits.

Provide deterministic JSON serialization as a package policy while clearly
separating determinism from CloudEvents conformance. Parsing untrusted events
MUST not panic, allocate from unchecked lengths, fetch schemas, or perform
implicit I/O.

## Bindings

HTTP support MUST correctly implement structured/binary modes, headers,
content types, duplicate/conflicting metadata, request/response ownership,
trailers where relevant, cancellation, body limits, and intermediary behavior.

Kafka support MUST define header/value mapping, keys/partitions, tombstones,
batching, duplicate headers, size limits, ordering, retries, and malformed
records. Queue and outbox adapters MUST state whether they implement an
official binding or a documented Golib transport mapping.

## Conversion And Schema

Provide explicit adapters for event-sourcing, outbox, Kafka, queue, workflow,
correlation, tenancy, telemetry, and audit metadata. Conversion MUST preserve
stable IDs, source, type, time, subject, schema reference, correlation,
causation, trace context, tenant policy, and payload bytes where representable.

Schema validation or registry lookup MUST be opt-in through `json-schema` and
`schema-registry`; receiving an event MUST never trigger hidden network access.

## Verification

Run official conformance fixtures and independent SDK interoperability in both
directions. Test unknown extensions, all content modes, mode conversion,
malformed attributes, conflicting metadata, hostile payloads, transport
round-trips, cancellation, and size limits. Fuzz parsers and bindings. Run race,
leak, stress, soak, and fault injection. Exact 100% statement coverage and 100%
viable mutation kills are REQUIRED.

## Documentation And Delivery

Document supported specification versions, formats, bindings, extensions,
conversion loss, schema use, security, transport examples, migration, FAQ, and
when not to use CloudEvents. Add manifests, CI, benchmarks, changelog, license
notices, fixture provenance, and clean-consumer proof.

## Non-Goals

- an event bus, broker, event store, workflow engine, or schema registry;
- imposing CloudEvents as Golib's internal canonical envelope;
- application-specific event taxonomies or compatibility policy;
- claiming an unofficial transport mapping is a standard binding.
