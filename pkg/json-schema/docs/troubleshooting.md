# Troubleshooting

## A valid schema is rejected during compilation

The compiler validates against the selected dialect's embedded official
meta-schema. Confirm that `WithDialect` matches the schema's `$schema` and that
keywords use the form defined by that draft. Draft migrations are summarized
in [dialects](dialects.md).

## A reference cannot be loaded

The core has no implicit network or filesystem access. Install a
`ResourceLoader`, register the exact canonical resource identifier, and wrap
missing resources with `ErrResourceNotFound`. URI user information, queries,
and fragments are redacted from resource errors. See [resolvers](resolvers.md).
Scheme and host case, default HTTP ports, dot segments, and unreserved percent
escapes are normalized; custom loader registries should use the normalized URI
received by their `Load` method.

## A format or content value is not rejected

Those keywords are annotations unless assertion behavior is explicitly
enabled. Use `WithFormatAssertion` or `WithContentAssertion`. Also confirm the
keyword exists in the selected dialect and vocabulary.

## Validation returns an error instead of `Valid == false`

Invalid instances return a result with `Valid == false`. Errors indicate an
operational failure such as malformed JSON, cancellation, an exhausted limit,
a loader failure, or an extension failure. Inspect with `errors.Is` and
`errors.As`; `LimitError` identifies the exhausted resource and configured
bound.

## A Go value does not preserve the intended JSON representation

`ValidateValue` uses JSON encoding. Unsupported values, cycles, non-string map
keys, and non-finite floats cannot be represented. Use `Validate` with raw JSON
for complete control. Use `json.Number` rather than `float64` when exact number
text matters.

## Output is unexpectedly large

Flag output is the smallest decision. Basic output emits leaf annotations and
failures. Detailed and Verbose expose the typed output-unit model. Lower
`MaxOutputUnits` or `MaxAnnotationBytes` for untrusted schemas and instances.

## Validation stops under adversarial input

This is expected when a configured budget is exhausted. Tune only the named
limit after measuring representative workloads. Avoid disabling bounds at an
external trust boundary. See [limits](limits.md), [security](security.md), and
[performance](performance.md).

## A custom keyword is ignored

Register its vocabulary URI with `WithVocabulary`, declare that vocabulary in
the schema where the dialect supports `$vocabulary`, and ensure the keyword is
registered under the same vocabulary. Required unknown vocabularies fail
compilation; optional unknown vocabularies remain annotations.

## A composite loader does not try the next loader

Fallback occurs only for errors matching `ErrResourceNotFound`. Authentication,
authorization, I/O, parsing, and cancellation errors stop the chain so that a
real failure is not silently masked.
