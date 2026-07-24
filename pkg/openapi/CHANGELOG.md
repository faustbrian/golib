# Changelog

## Unreleased

### Changed

- Bound fuzz worker parallelism independently of host CPU count so the full
  repository matrix preserves per-input deadlines under contention.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.
- Run the existing specification checks through the repository's attributable
  conformance gate.
- Use the repository-pinned current `apidiff` revision for historical API
  compatibility comparisons.

### Security

- Apply the exact public GitHub specification-fixture secret-scan exclusion
  from both repository-root and isolated module scans.
- Reject remote reference responses whose explicit `Content-Type` is not a
  supported JSON or YAML media type, even when the URL has a recognized file
  extension.
- Deny every IANA special-purpose IPv4 and IPv6 range in the HTTP resolver
  unless a caller explicitly authorizes the range.
- Reject unpaired UTF-16 surrogate escapes in strict JSON input instead of
  silently replacing them with U+FFFD.
- Redact remote reference paths and underlying transport details from HTTP
  resolver access errors.
- Redact filesystem paths and underlying operating-system details from file
  resolver access errors.
- Enforce parameter item limits before allocating or decoding over-budget
  OpenAPI and Swagger collections.
- Reject `math.MaxInt64` parser byte limits instead of overflowing the
  limit-detection sentinel.
- Reject over-depth JSON Pointer fragments before allocating or decoding their
  tokens.
- Redact base URI, requested URI, anchor names, and explicit resolver details
  from reference resolution errors while preserving error classification.
- Enforce anchor, reference-inventory, bundle, and dereference node and depth
  budgets before copying children from wide semantic values.
- Enforce composition semantic-node and depth budgets before copying children
  during rewriting and equality comparison.
- Enforce diff input and resolved-reference traversal budgets before copying
  children from wide semantic values.
- Enforce validation document node and depth budgets before copying children
  from wide semantic values.
- Enforce JSON and YAML serialization node and depth budgets before copying
  children from wide semantic values.
- Add bounded Schema Object compiler traversal and reject wide or deep schemas
  before copying semantic children.
- Enforce the conversion root-member budget before copying wide document roots.
- Deduplicate concurrent JSON Schema dialect engine construction so one
  compiler cannot repeat the same external load and compilation work.
- Redact caller-controlled JSON Schema dialect, URI, vocabulary, keyword, and
  loader details from compiler errors while preserving classification.
- Reject oversized or concatenated provenance and conformance manifests, and
  oversized model-generation field inventories, before expensive decoding.
- Observe context cancellation while the YAML decoder constructs its syntax
  tree and at every document boundary.

### Tooling

- Cover the unsupported Swagger 2.0 API-key cookie location explicitly in the
  downgrade decision matrix.
- Reject mutation reports containing timed-out or skipped mutants instead of
  accepting aggregate percentages alone.
- Exercise every production package in the scheduled mutation matrix,
  including command packages in integration mode.
- Include dereferencing and lossy cross-version conversions in the standard
  fuzz gate instead of leaving their existing fuzzers dormant.
- Add bounded Schema Object, document-validation, and JSON/YAML serialization
  fuzz targets to the standard adversarial gate.
- Fuzz server-sent event parsing and the mutation-report CLI decoder, and
  redact unknown mutant status text from gate errors.
- Reconcile prerelease documentation with the complete normative ledger and
  enforced 100% production statement coverage.
- Require source, revision or retrieval date, and license provenance for every
  official artifact group checked by the provenance gate.
- Allow the API compatibility gate to run on an initial repository commit while
  preserving strict validation for explicit baseline references.
- Inventory every selected Go module with pinned ownership, license,
  maintenance, necessity, and replacement evidence, and reject build-list
  drift in the dependency gate.
- Fuzz file-identifier and HTTP-response resolver trust boundaries, plus every
  provenance, conformance, and model-generation decoder.
- Add scaling, invalid-input, cyclic, and schema-heavy benchmark workloads,
  reproducible raw evidence with peak process memory, and blocking allocation
  budgets.
- Add a pinned independent interoperability matrix with round-trip evidence and
  explicit classifications for implementation limitations and strict parser
  policy.
- Enforce an explicit security-and-correctness golangci-lint configuration
  instead of relying on the tool's changing default linter set.
- Bound direct `jsonvalue.Value` JSON marshaling by bytes, depth, and semantic
  nodes, with an explicit tighter-limit API for untrusted caller-built values.
- Allow reviewed large descriptions to select bounded official-schema
  evaluation limits on an isolated validator cache.
- Pin unmodified Swagger Petstore and GitHub REST API descriptions with
  licenses, checksums, stable diagnostic expectations, and differential
  interoperability evidence.
- Keep strict conformance-manifest decoding synchronized with reviewed
  non-normative public-description groups.
- Report NilAway execution and its advisory outcome explicitly in every full
  quality-gate log.
