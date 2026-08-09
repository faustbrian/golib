# Golib CloudEvents adapters

`golib` is the optional integration module between the transport-independent
CloudEvents package and Golib's canonical event, transport, workflow,
metadata, audit, and schema contracts. Importing it performs no registration,
network access, schema lookup, telemetry emission, or background work.

Conversions retain canonical state that CloudEvents cannot represent and
return explicit loss reports. Queue and outbox conversions are Golib mappings,
not official CloudEvents protocol bindings. Schema resolution occurs only when
the caller explicitly invokes CloudEvents schema validation with a configured
registry validator.

See the parent module's [conversion policy](../../docs/conversions.md) for
reserved ownership, collisions, and round-trip guarantees.

## Quick start

```go
message := job.Message{
    Timeout: time.Minute,
    Body: []byte(`{"order":"A-123"}`),
    Metadata: &job.Metadata{
        OriginalID: "job-1",
        JobType: "order.notify",
        ContentType: "application/json",
    },
}

event, retained, report, err := golib.QueueToCloudEvent(
    message,
    golib.QueueOptions{Source: "/queue/orders"},
)
```

`retained` remains the authoritative queue execution state. Inspect `report`
before accepting any conversion whose target cannot represent all source
fields. The compiling example in `example_test.go` shows the complete imports.

## Supported adapters

| Boundary | Direction | Round-trip policy |
| --- | --- | --- |
| event sourcing | message to/from CloudEvent | payload and canonical message restored with caller-retained stream, version, position, recording, and metadata state |
| outbox | envelope to/from CloudEvent | Golib mapping, not an official binding; relay state remains out of band |
| queue | job to/from CloudEvent | Golib mapping, not an official binding; callbacks, retry, settlement, and operational metadata remain out of band |
| Kafka | producer/consumed record to/from official binding | CloudEvents headers/value use the parent binding; topic, partition, offset, timestamp, and non-CloudEvents headers remain transport-owned |
| workflow | history event to/from CloudEvent | caller supplies the stable CloudEvents ID; durable workflow state remains retained |
| correlation and tenancy | attach/extract | tenant extraction requires present, valid metadata and an explicit trust decision; tenant identity is not authorization |
| telemetry | inject/extract | delegates W3C propagation to a caller-configured Golib policy; baggage is reported as loss |
| audit | attach/extract selected metadata | never reconstructs or replaces the canonical audit record |
| JSON Schema | validate | caller supplies a compiled schema; no lookup occurs |
| schema registry | resolve then validate | lookup and resolver are caller configured and run only on explicit validation |

## Collisions, trust, and loss

Portable metadata uses the documented CloudEvents extensions
`correlationid`, `requestid`, `causationid`, `tenantid`, `traceparent`,
`tracestate`, `partitionkey`, `eventschema`, `auditid`, `auditaction`, and
`auditoutcome`. An equal pre-existing value is idempotent. A different value
returns `ErrMetadataCollision`. Malformed canonical input returns
`ErrInvalidAdapterInput`; adopting protected inbound metadata without trust
returns `ErrUntrustedMetadata`. Tenant extraction fails closed when the
`tenantid` extension is absent, and outbound audit tenant values are validated
before they become extensions. Queue and event-sourcing conversions validate
tenant metadata on both outbound conversion and inbound replay, including when
the retained envelope and CloudEvent contain the same malformed value.

`Report.Losses` names every portable field deliberately not represented by the
target. A successful conversion does not imply an exact CloudEvents round trip
unless its adapter row above says so and the retained state is supplied back.

## Schema validation and cancellation

`JSONSchemaValidator` accepts one already compiled Golib JSON Schema and an
exact schema URI. `RegistryJSONSchemaValidator` additionally requires an
explicit URI-to-lookup function, resolver, and JSON Schema adapter. Merely
receiving, decoding, or converting an event never invokes either validator.
Registry and validation errors preserve cancellation and resolver errors for
caller policy while using `ErrSchemaMapping` and `ErrSchemaViolation` for
stable classification.

## Concurrency and ownership

All conversions are synchronous and start no goroutines. Returned maps, byte
slices, headers, payloads, timestamps, and retained envelope state are copied
where mutable aliasing would cross the boundary. Configured schema resolvers,
compiled schemas, and telemetry policies retain the concurrency guarantees of
their owning Golib packages.

## Adoption and migration

Adopt this nested module only at a boundary that already owns both a Golib
canonical value and a CloudEvents interoperability requirement. Keep existing
domain, event-store, outbox, queue, workflow, audit, and transport envelopes as
the source of truth. During migration, persist or carry the returned retained
state before replacing any bespoke envelope mapping, and reject unexpected
losses or collisions explicitly.

Do not use this module as an event bus, broker, store, dispatcher, schema
registry, workflow engine, audit log, tenant authorization mechanism, or
application event taxonomy.

## FAQ

**Are queue and outbox mappings official CloudEvents bindings?** No. They are
documented Golib mappings.

**Does a schema URI trigger registry access?** No. Resolution occurs only when
the caller explicitly invokes schema validation with the registry adapter.

**Can an audit record be reconstructed from extensions?** No. Only selected,
non-authoritative metadata is portable.

**Can inbound tenant or correlation fields be trusted automatically?** No. The
caller owns transport authentication and must select trusted extraction.

See the [security policy](SECURITY.md), parent [specification matrix](../../docs/specification-matrix.md),
[provenance](../../docs/provenance.md), [benchmark evidence](docs/benchmarks.md),
and [changelog](CHANGELOG.md).

## Verification

Run package-specific repository gates for
`pkg/cloudevents/adapters/golib`. They cover formatting, tests, race detection,
exact statement coverage, fuzz smoke, mutation, API compatibility, security,
documentation, benchmarks, and clean-consumer installation.
