# Contributing

Follow the repository-wide contribution and quality rules. Changes affecting
parsing, canonicalization, signing, verification, digest handling, HTTP
integration, registries, errata, or compatibility adapters must update the
[specification decision register](docs/specification-decisions.md), normative
conformance matrix, executable evidence, security analysis, and changelog when
observable behavior changes.

Do not resolve a specification ambiguity in code or tests without recording
the credible interpretations, selected behavior, consequences, and condition
for reconsideration. New protocols belong in explicit compatibility adapters;
they must not widen RFC 9421 parser acceptance.
