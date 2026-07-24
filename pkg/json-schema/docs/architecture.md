# Architecture

The package separates exact JSON ingestion, schema-resource indexing,
dialect and vocabulary processing, immutable plan compilation, evaluation,
annotation propagation, output construction, format/content policy, and
resource loading.

## Lifecycle

1. `NewCompiler` copies all options, registries, and loaders into
   compiler-owned state and compiles the selected pinned meta-schema.
2. `Compile` parses schema JSON without `float64`, validates it against that
   meta-schema, indexes resources and anchors, resolves explicitly available
   references, negotiates vocabularies, and emits an immutable plan.
3. `Validate` parses an instance into the same exact internal representation
   and evaluates the plan with a request-local context, budget counters,
   dynamic scope, and evaluated-location sets.
4. Output APIs repeat bounded diagnostic traversal to produce detached,
   deterministic output units and successful-path annotations.

`Compiler` owns mutable setup policy. `Schema` contains no validation-time
mutation and is safe for concurrent calls. Each validation creates its own
state; returned slices, maps, resource bytes, names, and annotations do not
expose internal mutable buffers.

## Dependency direction

The core imports only the Go standard library plus `golang.org/x/net/idna`
and its `x/text` dependency. It does not import framework, service, OpenRPC,
JSON:API, database, cache, filesystem implementation, or network client code.
Consumer integrations belong in consumers or separate adapters.

## Errors

Malformed JSON, invalid schemas, unavailable or missing resources,
unsupported dialects or required vocabularies, limit exhaustion, callback
errors, cancellation, and ordinary invalid instances remain separate.
Public sentinels support `errors.Is`; `JSONError` and `LimitError` support
`errors.As`. Invalid instances are results, not errors.
