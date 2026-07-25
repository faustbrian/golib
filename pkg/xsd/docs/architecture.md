# Model and compilation

The root package parses schema bytes into an explicit `Document`. Parsing does
not resolve imports or includes. The model retains expanded QNames, form and
derivation policies, declarations, named and anonymous types, groups,
wildcards, particles, facets, notations, and identity constraints. Annotations
are retained at every XML Schema component location. Documentation exposes a
plain-text view while preserving mixed XML markup for lossless serialization.

`compile.Compiler` resolves a bounded document graph through an injected
resolver. It applies chameleon namespaces, indexes component symbol spaces,
expands groups and supported derivations, checks references and content-model
ambiguity, and returns an immutable `compile.Set`. Accessors deep-copy slices,
maps, pointers, and nested model groups so callers cannot mutate validation
plans.

Recognized invalid restrictions, derivations, references, and ambiguous
content models fail compilation instead of producing a different validation
plan.
