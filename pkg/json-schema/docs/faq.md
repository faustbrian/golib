# FAQ and troubleshooting

## Why did a remote `$ref` fail?

No loader means no remote retrieval. Register the exact resource in a
`MapLoader`, use a confined `FSLoader`, or supply an application adapter. The
core never falls back to the network.

## Why did an invalid email or UUID pass?

Format is annotation-only by default. Enable `WithFormatAssertion` when the
application deliberately wants assertion behavior.

## Why does `Compile` reject metadata that evaluation would ignore?

Schemas are validated against the selected official meta-schema before plan
compilation. Keyword shape is a schema-validity concern, not an
invalid-instance result.

## Why did validation return an error instead of `Valid:false`?

Errors mean parsing, schema, resolution, cancellation, callback, or resource
policy failure. `Valid:false` is reserved for a successfully evaluated invalid
instance. Inspect sentinels with `errors.Is` and typed errors with `errors.As`.

## Can compiled schemas be shared?

Yes. They are immutable and race-tested. Do not put request-specific mutable
state inside custom loaders, keyword evaluators, or format checkers unless the
callback synchronizes it.

## Are Go `float64` values exact?

The validator preserves the value supplied by the caller, but a `float64` may
already have lost decimal precision before validation. Use raw JSON or
`json.Number` for arbitrary decimal values.

## Why did output exhaust a budget after flag validation succeeded?

Diagnostic output performs additional bounded traversal and may invoke pure
custom callbacks again. Raise output/callback limits only after measuring the
required diagnostic surface.
