# Changelog

All notable changes will be documented here. The project follows Keep a
Changelog structure and semantic versioning after v1.

## Unreleased

- Expose official-suite conformance as an explicit repository gate distinct
  from ordinary tests and interoperability harnesses.
- Contain and redact custom keyword compiler, keyword evaluator, and format
  checker panics behind the typed `ErrCallbackPanic` boundary.
- Extend panic containment to resource loaders, supplied filesystems, and
  caller-provided JSON marshalers.
- Redact callback error text while retaining the original error for
  `errors.Is` and `errors.As` inspection.
- Reject malformed and duplicate schema resource identifiers and ambiguous
  duplicate anchors instead of silently ignoring or overwriting them.
- Resolve references to the pinned official meta-schema bundle without
  requiring callers or Bowtie registries to duplicate those resources.
- Replace quadratic `uniqueItems` scans for large arrays with canonical,
  collision-safe hash buckets while retaining direct small-array checks.
- Normalize RFC 3986 resource identity across scheme and host case, default
  ports, dot segments, and percent-encoded unreserved characters.
- Prevent `$anchor`, `$recursiveAnchor`, and `$dynamicAnchor` semantics from
  leaking into dialects where those keywords are not defined.
- Apply `MaxRegexBytes` to asserted `regex` format values before compiling
  attacker-controlled expressions.
- Restrict every built-in format name to the dialect that defines it while
  preserving explicit application-defined format extensions.
- Execute schema regular expressions with bounded ECMAScript and Unicode
  semantics, including lookaround and backreferences.
- Add executable examples, official-fixture-backed fuzz corpora, and separate
  compile, validation, reference, and adversarial scaling benchmarks.
- Scope built-in and custom vocabulary activation to each schema resource in
  compound documents, and reject unindexed reference sources without panic.
- Add local release gates and monorepo CI for per-dialect conformance,
  coverage, race, fuzz, mutation, analysis, API, docs, and releases.
- Emit deterministic keyword diagnostics with by-reference evaluation paths,
  condensed Detailed output, complete uncondensed Verbose hierarchies, and a
  dedicated retained-annotation API.

- Added exact JSON parsing, all six released dialects, complete pinned-suite
  discovery, references and dynamic scope, vocabulary processing, official
  meta-validation, standard formats and content policy, annotations, standard
  output, custom extensions, secure loaders, explicit limits, and Bowtie
  protocol support.
- Added pinned official fixture and meta-schema provenance plus a generated
  zero-skip, zero-failure manifest for 8,505 cases.
