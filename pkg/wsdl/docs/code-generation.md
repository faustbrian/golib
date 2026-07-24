# Code generation model

Package `codegen` converts a compiled set into an owned deterministic model
containing interfaces, operations, input/output payloads, message parts,
faults, bindings, services, endpoints, schema types, and elements. Independent
limits bound every major collection.

The package does not generate files, choose Go identifiers, invoke formatters,
or embed a transport. A client or server generator is an optional consumer and
must define naming, collision, nullable-value, schema-mapping, and output-byte
policies of its own.
