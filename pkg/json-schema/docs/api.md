# API guide

The package has one compile-and-validate lifecycle:

1. construct an isolated `Compiler` with options;
2. compile one schema document into an immutable `Schema`;
3. reuse that schema concurrently for validation.

## Construction

`NewCompiler` defaults to Draft 2020-12 and `DefaultLimits`. Its options
select a dialect, replace limits, authorize a resource loader, enable format
or content assertions, and register custom formats or vocabularies. Options
are copied into the compiler; there is no process-wide registry.

`Compiler.Compile` accepts UTF-8 JSON bytes. It parses exact JSON numbers,
validates the document against the selected official meta-schema, resolves
authorized references, and returns an immutable evaluation plan. Compilation
can fail with malformed JSON, an invalid schema, an unavailable resource, a
resource limit, a contained extension panic, or context cancellation.

## Validation

`Schema.Validate` accepts raw JSON bytes. `Schema.ValidateValue` accepts a Go
value by encoding it as JSON under the configured limits. Prefer raw bytes
when lexical number fidelity matters; `json.Number` retains its number text
on the value path.

Both methods return `Result`, whose `Valid` field is the validation decision.
An error means validation could not finish, rather than a schema mismatch.
Use `errors.Is` and `errors.As` with the exported sentinels, `JSONError`, and
`LimitError`; do not branch on error text.

Application callback panics, including loaders, supplied filesystems,
`json.Marshaler` values, custom keywords, and formats, are returned as
`ErrCallbackPanic`. Recovered panic values are not included in diagnostics.
Returned callback errors remain inspectable with `errors.Is` and `errors.As`
without copying their text into the package error string.

`ValidateOutput` and `ValidateValueOutput` return an `OutputUnit` in the
requested Flag, Basic, Detailed, or Verbose format. See [output](output.md)
for location and annotation behavior. `CollectAnnotations` instead returns
the retained successful-path annotations as a flat deterministic list.

## Resources

`ResourceLoader` is the only remote-resource seam. `MapLoader` serves an
immutable in-memory map, `FSLoader` maps one URI base to a confined `fs.FS`,
and `CompositeLoader` tries loaders in order. A loader must return
`ErrResourceNotFound` to permit composite fallback. The package never creates
an HTTP client or reads arbitrary files on its own.

Resource identifiers must be valid URI references. A schema document cannot
assign one resolved resource identifier or anchor name to multiple schemas;
such ambiguity fails compilation with `ErrInvalidSchema`.
Resource and loader keys use RFC 3986 normalization for scheme and host case,
default HTTP ports, dot segments, percent-escape case, and percent-encoded
unreserved characters.
References to the package's pinned official meta-schema URIs resolve from the
embedded bundle before the application loader. Other non-local identifiers
remain exclusively under the configured loader's policy.

## Extension points

`WithFormat` installs a context-aware string checker owned by one compiler.
`WithVocabulary` installs compiler and evaluator callbacks for explicitly
declared custom keywords. Extension callbacks receive immutable values and
share the compiler's work budgets. See [extensions](extensions.md).

## Concurrency and ownership

A compiled `Schema`, `MapLoader`, `FSLoader`, or `CompositeLoader` may be
shared by concurrent validators. Callers must make their own loader and
extension implementations concurrency-safe. Returned results and output units
are caller-owned values.

[output]: output.md
[extensions]: extensions.md
