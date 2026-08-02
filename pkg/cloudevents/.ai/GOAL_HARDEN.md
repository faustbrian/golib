# Goal Harden: `cloudevents`

## Mission

Prove complete supported-specification conformance, loss-aware conversions,
transport interoperability, hostile-input safety, and deterministic resource
ownership across every CloudEvents format and binding.

## Required Audit

1. Refresh pinned specifications, normative matrices, errata, registries,
   official conformance kits, SDK versions, formats, bindings, and extensions.
2. Inventory every attribute, extension, parser, serializer, conversion,
   binding, adapter, limit, error, and diagnostic.
3. Differentially test official Go and non-Go SDKs for structured and binary
   HTTP, JSON, Kafka, batches, unknown extensions, and selected extensions.
4. Verify absent/null/empty data, content-type parameters, encoded bytes,
   Unicode, URIs, timestamps, duplicate fields, header casing, and conflicts.
5. Prove conversions preserve declared metadata or return explicit loss; test
   round trips across event-sourcing, outbox, queue, Kafka, and workflow.
6. Exercise malformed and oversized HTTP/Kafka records, short reads, partial
   writes, cancellation, retries, ownership, compression, and shutdown.
7. Verify schema lookup is explicit, bounded, cache-safe, and incapable of SSRF
   or hidden I/O.
8. Fuzz all formats and bindings; run race, leak, stress, soak, differential,
   and allocation-bound tests.
9. Audit trace, correlation, tenant, subject, source, schema, and payload data
   for leakage and unsafe metric cardinality.
10. Review every support claim against executable evidence and remove or mark
    unsupported behavior rather than shipping partial compliance.

## Required Evidence

- complete normative matrix and official conformance report;
- independent SDK interoperability matrix;
- exact 100% meaningful statement coverage and 100% viable mutation kills;
- race, fuzz, leak, stress, soak, hostile-input, and transport-failure results;
- equivalent benchmarks against the official Go SDK for identical behavior;
- security, conversion-loss, docs, and clean-consumer review.

No passing round trip may conceal normalized-away extensions, changed payload
bytes, metadata collisions, or a transport mapping mislabeled as standard.
