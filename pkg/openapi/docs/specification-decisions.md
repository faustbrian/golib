# Specification decisions

This document records package decisions where the pinned OpenAPI prose,
published artifacts, or accepted errata need interpretation. The order of
authority is normative prose, accepted OpenAPI Initiative errata, published
schemas and registries, then local package policy. A published schema never
silently overrides normative prose.

## OpenAPI 3.2 default Schema Object dialect

OpenAPI 3.2.0 was published with an incorrect 3.1 dialect URI in the Schema
Object prose. The published 3.2 schema instead defaults `jsonSchemaDialect` to
the dated 3.2 dialect. The OpenAPI Initiative accepted the correction in
[OAI/OpenAPI-Specification#4994](https://github.com/OAI/OpenAPI-Specification/pull/4994),
merged as commit `0f65e951d63d34c207cd79081cff4743c0d763fb` for the
3.2.1 patch line.

The package applies that accepted erratum to 3.2.0 documents: when both the
root `jsonSchemaDialect` and a resource-root `$schema` are absent, Schema
Objects use `https://spec.openapis.org/oas/3.2/dialect/2025-09-17`. An explicit
root or resource declaration still takes precedence. This is required to
recognize 3.2 vocabulary additions such as XML `nodeType` and discriminator
`defaultMapping`.

The pinned 3.2.0 Markdown remains byte-identical to its released revision.
The erratum is recorded separately in `specification/manifest.json` so the
historical source is not rewritten.

## HTTP reference representation selection

Observed fact: a remote server can return an OpenAPI-looking body whose file
extension conflicts with its explicit `Content-Type` field.

Specification requirement: RFC 9110 section 8.3 defines `Content-Type` as the
representation's data format and processing model. It permits recipients to
inspect data when the field is absent, while warning that overriding a received
type creates interoperability and security risks.

Package policy: the HTTP resolver accepts JSON, structured-syntax `+json`, and
the documented YAML media types. When `Content-Type` is present, any other type
is rejected. A `.json`, `.yaml`, or `.yml` suffix selects a parser only when the
field is absent. This avoids media-type confusion without guessing from bytes.

## Unpaired JSON surrogate escapes

Observed fact: Go's standard JSON decoder replaces unpaired UTF-16 surrogate
escapes with U+FFFD, making the decoded value differ from the received member
name or string.

Specification requirement: RFC 8259 sections 8.2 and 9 explain that the JSON
grammar admits these non-Unicode bit sequences, that receiver behavior is
unpredictable, and that parsers may constrain string character contents.

Package policy: strict JSON input rejects an unpaired high or low surrogate.
Valid escaped surrogate pairs remain accepted. This is an intentional input
compatibility restriction that prevents implementation-dependent identities
and silent replacement during parsing.
