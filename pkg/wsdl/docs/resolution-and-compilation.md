# Resolution and compilation

`compile.New` creates an immutable reusable compiler. A nil WSDL or schema
resolver becomes a deny resolver, so the default performs no network or file
access. Supply `resolve.Memory`, a policy-wrapped custom resolver, and a
separate `xsd/resolve.Resolver` for schema resources when loading is
intended.

The compiler checks absolute resource identities, resolver response identity,
version and namespace edges, cycles, cumulative bytes, depth, reference count,
document count, and component count. It then validates and returns an immutable
`Set` with deterministic documents, interfaces, operations, messages, faults,
bindings, services, endpoints, and the compiled `xsd` set.

All accessors return owned slices and nested message data. The set is safe for
concurrent lookup. Resolvers must implement their own network, redirect, file,
host, and credential policy; core never adds an implicit fallback.
