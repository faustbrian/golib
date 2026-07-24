# Architecture

The root package owns immutable OpenRPC values and deterministic serialization.
It depends only on package-owned arbitrary JSON and JSON Schema value types.

- `jsonvalue` validates and preserves arbitrary exact JSON.
- `jsonschema` isolates the pinned Draft 7 validator behind owned contracts.
- `parse` constructs the complete model in strict or preserving mode.
- `validate` emits bounded stable structural and semantic diagnostics.
- `reference` provides URI, JSON Pointer, stores, and explicit resolution.
- `expression` evaluates JSON Template Language without code execution.
- `builder` and `compose` provide owned construction, filtering, and merging.
- `discovery` provides transport-neutral snapshots, revisions, and caching.
- `jsonrpc` adapts discovery to the JSON-RPC handler call shape.
- `diff` classifies deterministic compatibility changes.
- `observe` is an optional payload-free wrapper for operation telemetry.

Dependencies point inward toward immutable data contracts. Transport adapters
are optional leaves. No server, router, HTTP framework, reflection registry, or
code generator is required to construct a compliant design-first document.

Concurrent operations use immutable values or explicitly owned synchronized
objects. Discovery cache invalidation, resolver per-call caches, and registries
have visible lifetimes; the module starts no hidden goroutines.
