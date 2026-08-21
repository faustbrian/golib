# cloudevents

`cloudevents` is Golib's transport-independent CloudEvents interoperability
envelope. It implements the stable 1.0 information model, deterministic JSON
event and batch serialization, and selected HTTP and Kafka mappings. It does
not replace richer domain, event-sourcing, outbox, queue, workflow, audit, or
application envelopes.

This module is pre-release. The [normative matrix](docs/specification-matrix.md)
is the authority for pinned revisions and support claims; an intended-support
row is not complete until its listed evidence exists.

## Quick start

```go
data, err := cloudevents.NewJSONData([]byte(`{"order":"A-123"}`))
if err != nil {
    return err
}
event, err := cloudevents.NewEvent(cloudevents.Attributes{
    ID:              "evt-123",
    Source:          "/orders",
    Type:            "com.example.order.created.v1",
    DataContentType: "application/json",
}, data)
if err != nil {
    return err
}

wire, err := cloudevents.EncodeJSON(event)
```

`EncodeJSON`, `EncodeJSONBatch`, `EncodeHTTP`, and `EncodeKafka` are strict:
they return `ErrConversionLoss` when a target representation would normalize a
declared extension type, materialize metadata, or discard payload-byte
distinctions. The corresponding `Encode*WithReport` functions return the
encoded value plus a deterministic `ConversionReport` when the caller has an
explicit policy for those changes. JSON payload bytes, including significant
internal whitespace, otherwise remain exact across structured round trips.

Use `DecodeJSON`, `DecodeJSONBatch`, `DecodeHTTP`, or `DecodeKafka` with an
explicit `Limits` value for untrusted input. `DefaultLimits` is conservative;
applications remain responsible for choosing limits appropriate to their
transport and threat model.

## Ownership and I/O

- Events, data, attributes, Kafka records, and returned bodies do not retain
  caller-owned mutable byte slices or maps.
- HTTP decoders never close caller-owned bodies. Cancellation is checked before
  and after reading; prompt interruption requires a reader whose own `Read`
  operation observes cancellation.
- Kafka helpers perform record mapping only. Topics, partitions, offsets,
  ordering, retries, acknowledgements, settlement, and broker I/O remain with
  the caller. `DecodeKafka` applies explicit key, total-header, header-name,
  and header-value limits before copying record metadata.
- Schema validation is invoked only through a caller-supplied
  `SchemaValidator`. Receiving or constructing an event never resolves a schema
  or performs network I/O.

## Formats and bindings

- JSON event format: structured encode/decode with distinct absent, JSON null,
  text, empty, and binary data semantics.
- JSON batch format: empty and non-empty batches with bounded event counts.
- HTTP: binary, structured JSON, and JSON batch content modes.
- Kafka: binary and structured JSON content modes. Kafka has no supported batch
  mode in the pinned official binding.

Queue and outbox representations are Golib transport mappings, not official
CloudEvents bindings. Conversion rules and loss reporting are specified in
[conversion policy](docs/conversions.md).

## Adoption guidance

Use CloudEvents at an interoperability boundary where producers and consumers
benefit from its portable context attributes and selected binding. Keep the
application's canonical envelope when it owns richer invariants such as stream
versions, transactional state, workflow execution, retry settlement, audit
integrity, or authorization context.

Do not use this package as a broker, dispatcher, event taxonomy, compatibility
policy, schema registry, audit log, or replacement for application validation.

See the canonical [specification decision register](docs/specification-decisions.md),
[interoperability overview](docs/decisions.md), [security policy](SECURITY.md),
[security and cardinality review](docs/security-review.md),
[fixture provenance](docs/provenance.md), [benchmark baseline](docs/benchmarks.md),
and [changelog](CHANGELOG.md). Interoperability evidence covers the official Go
SDK and the independent JavaScript SDK; importing the package never invokes
either SDK or a runtime outside Go.

## Ecosystem

Use the [Golib documentation portal](https://github.com/faustbrian/golib/blob/main/docs/index.md)
to choose companion packages, supported stacks, recipes, and operations guidance.
