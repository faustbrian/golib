# Migration

The module is pre-1.0. Pin an exact pseudo-version and review the changelog and
semantic diff when upgrading. Construct documents through `NewDocument11` or
`NewDocument20`; do not depend on lexical namespace prefixes or source
attribute order.

Older parsers that loaded imports implicitly must move loading behind an
injected resolver. A nil resolver now means deny. Callers that treated all
WSDL versions as one model must branch on `Document.Version` and use the
version-specific accessor.

Generated-client users should compile a set and consume package `codegen`
rather than coupling generators to parser internals.
