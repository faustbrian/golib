# Security

## Untrusted input

Every decoder requires explicit limits. Choose limits no larger than the
surrounding HTTP server, Kafka client, queue, or persistence boundary permits.
Decoders reject duplicate or conflicting metadata and never include rejected
attribute values or payload bytes in validation diagnostics.

JSON depth, batch count, attribute name and value sizes, decoded data size,
total event size, and copied Kafka key and header metadata are bounded.
Decoding never fetches `dataschema`, invokes a registry, starts a goroutine, or
performs broker I/O.

## URI and schema policy

`source` and URI-valued attributes are syntax, not trust. Applications must not
dereference them merely because an event validated. Schema validation is an
explicit call through `ValidateSchema`; resolver allowlists, authentication,
timeouts, response limits, caching, and SSRF controls belong to the supplied
adapter.

## HTTP cancellation

`DecodeHTTP` checks cancellation before and after its bounded body read. An
arbitrary blocking `io.Reader` cannot be interrupted safely by this package
without leaking work. Use request bodies or readers whose own `Read` observes
cancellation when prompt interruption is required.

## Metadata trust

CloudEvents context attributes are untrusted producer assertions. Correlation,
causation, tenant, trace, audit, schema, and subject values do not authenticate
or authorize a caller. Establish transport trust and application policy before
adopting them into canonical metadata.

Report vulnerabilities privately to the repository owner. Do not include
payloads, credentials, or production event data in a report unless an approved
secure channel has been established.

The optional JavaScript interoperability gate installs only the checked npm
lockfile into a temporary directory with lifecycle scripts disabled and a
temporary npm cache. It is test tooling and is absent from the Go module's
runtime dependency graph.
