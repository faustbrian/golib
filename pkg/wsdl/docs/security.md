# Security and limits

The XML reader rejects DTDs and custom entity declarations. Parsing and
serialization bound bytes, depth, elements, attributes, text, schemas,
imports, operations, bindings, endpoints, extensions, diagnostics, and output.
Compilation adds graph depth, documents, references, cumulative bytes, schema
limits, and components. Code-generation models have independent limits.

No package opens a file, follows a redirect, contacts a host, reads an
environment variable, consults a global registry, or starts background work.
Injected resolvers are the only external I/O seam. Production resolvers should
allowlist schemes and hosts, reject private and link-local address changes,
bound redirects and response bytes, and avoid forwarding credentials.

Cancellation stops parsing and graph resolution. Returned models and compiled
sets own their mutable slices so callers cannot mutate shared compiler state.
