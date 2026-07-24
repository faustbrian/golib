# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Added the `GO-SAFETY-1` ownership, concurrency, race, fuzz, resource, and
  benchmark standard with an executable `make safety` gate.
- Moved AI planning and hardening briefs into `.ai/` and clarified the
  separate purposes of project and third-party notice files.

### Added

- A standardized OSS repository skeleton covering policy, documentation,
  legal notices, Go tooling, pinned CI, security, and release automation.
- CI resolves the latest supported Go 1.25 patch while `go.mod` declares the
  portable Go 1.25 minimum.
- Evidence-driven audit and hardening goal covering every supported format,
  parser resource safety, codec dependencies, and read/write boundaries.
- A shared `wire.Error` model with parse, validation, unsupported-format,
  envelope, and SOAP-fault classifications.
- Size-limit, invalid-target, and encoding classifications shared by every
  format package.
- Explicit JSON, XML, SOAP, YAML, TOML, MessagePack, CBOR, and BSON format
  identifiers.
- Opt-in JSON/XML format detection based on the first significant byte.
- Bounded JSON byte-slice and reader decoding with optional unknown-field
  rejection and single-value enforcement.
- Deterministic JSON encoding with configurable indentation and HTML escaping.
- Explicit JSON normalization that strips a UTF-8 BOM, compacts whitespace,
  orders object keys, and preserves number lexemes.
- JSON fixtures, malformed-input regression tests, fuzz seeds, and parse and
  encode benchmarks.
- Bounded strict XML decoding, opt-in non-strict recovery, exact
  namespace-aware root validation, and resolved-root inspection.
- Deterministic XML encoding with optional declaration and indentation.
- Built-in, explicit charset conversion for UTF-8, US-ASCII, ISO-8859-1, and
  Windows-1252, including rejection of invalid or undefined bytes.
- Namespace, malformed-document, charset, and trailing-root XML fixtures and
  regressions, plus fuzz seeds and parse and encode benchmarks.
- Bounded SOAP 1.1 and 1.2 envelope parsing with exact raw envelope, header,
  and body access and namespace-preserving body decoding.
- Typed SOAP 1.1 and 1.2 fault extraction, localized SOAP 1.2 reasons,
  subcodes, details, and `errors.Is` classification.
- Deterministic envelope and fault serialization from validated raw fragments.
- SOAP envelope, fault, malformed-input, structural-regression, fuzz, and
  benchmark coverage.
- Compile-checked examples and adoption documentation covering quickstart,
  architecture, the complete public API, supported formats, behavioral
  guarantees, limitations, and rollout guidance.
- End-to-end JSON, XML, and SOAP examples, a scenario cookbook, FAQ, and
  error-focused troubleshooting guide.
- Migration, versioning, release, contribution, security, conduct, and roadmap
  documentation for open-source operation.
- Reproducible local formatting, vet, lint, race-test, 100% coverage, fuzz,
  benchmark, documentation-link, and vulnerability quality gates.
- GitHub Actions for the Go 1.25 and 1.26 build/race-test matrix,
  formatting, static analysis, lint, exact coverage, documentation, fuzz smoke,
  benchmark smoke, and vulnerability scanning.
- Scheduled extended fuzzing and benchmark baselines, Dependabot configuration,
  and an interoperability-focused pull request template.
- Tagged release automation that validates SemVer tags and changelog entries,
  reruns quality and security gates, creates a deterministic source archive and
  checksum, and publishes a GitHub Release with extracted notes.
- Explicit method-level documentation for every exported error API.
- JSON and XML writer APIs, typed SOAP header/body encoding, and SOAP envelope
  and fault writer APIs for symmetric input and output handling.
- Bounded YAML read/write APIs with duplicate-key and multi-document policy,
  alias and merge controls, expansion limits, deterministic output, fixtures,
  fuzzing, and benchmarks.
- Complete TOML document read/write APIs with strict-field metadata, native
  datetime handling, checked numeric conversion, deterministic output,
  fixtures, fuzzing, and benchmarks.
- MessagePack read/write APIs with exact one-object enforcement, map-key and
  extension policy, checked numeric widths, timestamp support, deterministic
  map encoding, fixtures, fuzzing, and benchmarks.
- Canonical, Core Deterministic, and CTAP2 CBOR read/write profiles with tag,
  indefinite-length, duplicate-key, and resource-limit policy, plus fixtures,
  fuzzing, and benchmarks.
- BSON document read/write APIs with recursive duplicate-key rejection, exact
  length validation, ordered and raw document support, ObjectID and datetime
  aliases, checked numeric conversion, fixtures, fuzzing, and benchmarks.
- Pinned, reviewed YAML, TOML, MessagePack, CBOR, and BSON codec dependencies
  with documented maintenance, security, and residual-risk rationale.
- Compile-checked round-trip examples and full API, format, migration,
  adoption, security, troubleshooting, and dependency documentation for all
  eight supported formats.
- A distinct `wire.ErrWrite` classification for destination failures.
- Consistent repository automation for generated portable AI documentation,
  dependency review, and guarded semantic release commands.
- Configurable output byte limits for every JSON, XML, SOAP, YAML, TOML,
  MessagePack, CBOR, and BSON byte and writer encoding path. Zero selects the
  safe 1 MiB default.
- Size-bounded SOAP raw-envelope and fault APIs through `MarshalOptions`,
  `MarshalWithOptions`, `MarshalWriterWithOptions`,
  `MarshalFaultWithOptions`, and `MarshalFaultWriterWithOptions`.

### Changed

- The minimum supported Go version is Go 1.25.0; later Go 1.25 patch
  releases and newer language versions are supported.
- Fuzz, benchmark, release, and documentation automation now covers YAML,
  TOML, MessagePack, CBOR, and BSON alongside JSON, XML, and SOAP.
- Corrected codec troubleshooting and migration guidance to use the exported
  option and profile names, describe YAML's actual safe defaults, distinguish
  legacy size classifications, and document BSON double truncation as
  explicitly lossy.
- MessagePack decoding now enforces configurable safe defaults of 32 nesting
  levels, 131,072 array elements, and 65,536 map pairs. Structural limit
  failures are classified as `wire.ErrSizeLimit`.
- MessagePack decoding now rejects duplicate map keys recursively by default,
  with explicit last-key-wins compatibility through `AllowDuplicateKeys`.
- BSON now re-exports the official array, raw value, Decimal128, binary, regex,
  timestamp, JavaScript, scoped code, sentinel, pointer, and symbol types.
- Published the evidence-driven hardening report, threat model, per-format
  conformance matrix, allocation policy, dependency residual risks, and
  pre-v1 semantic-version recommendation.
- Writer APIs now complete encoding within the configured output quota before
  writing, so an encode limit failure cannot emit a partial destination
  payload.

### Fixed

- Bound fuzz-smoke concurrency to avoid deadline flakes on high-core hosts.
- MessagePack now performs allocation-safe structural preflight for impossible
  collection lengths and composite map keys. Numeric preflight rejects
  narrowing overflow in typed maps, array-encoded structs, and automatically
  inlined embedded fields before the decoder can apply a lossy Go conversion.
- MessagePack structural validation now stops recursive traversal and compact
  collection allocation amplification at explicit caller-configurable limits.
- All typed encoders now reject cyclic values and nesting beyond 1,000
  traversed levels before recursive codecs can exhaust the process stack.
- MessagePack numeric preflight now also prevents duplicate keys from being
  silently overwritten before assignment.
- BSON documentation and tests now prove Decimal128, binary subtype, and regex
  interoperability instead of claiming it from an incomplete alias set.
- JSON decoding and normalization now reject invalid UTF-8 as a parse failure
  instead of silently accepting replacement characters.
- XML tests now prove directive handling, non-expansion of declared entities,
  and embedded-NUL rejection.
- SOAP tests now prove fault text and language attributes cannot inject XML
  while validated raw Detail fragments remain explicit.
- Cross-format writer regressions now prove zero-progress destinations are
  rejected as `wire.ErrWrite`, including all three SOAP writer families.
- CBOR tests now lock simple-value, tagged-bignum, and preferred integer and
  shortest-float behavior to the selected deterministic profile.
- TOML tests now lock dotted-key conflicts, special floats, and arrays of
  tables to the documented dependency behavior.
- YAML tests now lock custom tags, core-schema implicit values, and typed
  non-JSON map keys to the documented compatibility boundary.
- YAML now classifies the dependency's built-in excessive-alias and maximum
  nesting protections as `wire.ErrSizeLimit`, matching caller-configured
  resource limits.
- YAML merge-key rejection now follows the parsed merge tag without rejecting
  an ordinary quoted `<<` scalar value.
- Every encoder now rejects output beyond its configured byte limit as
  `wire.ErrSizeLimit` instead of allowing unbounded output buffering.
- SOAP raw header, body, and fault Detail fragments that cannot fit the output
  quota are rejected before XML validation allocates a wrapper copy.
- Package-local boundary regressions now prove exact, undersized, negative,
  and dependency-construction behavior while retaining 100% production
  statement coverage.
- XML and SOAP parsing now enforce a configurable 1,000-element default token
  depth and classify excess nesting as `wire.ErrSizeLimit`.
- A cross-format round-trip fuzzer now proves writer/reader semantic parity for
  bounded strings, signed integers, and booleans across all eight formats.
- YAML block scalars now use an explicit indentation indicator, preventing the
  YAML v4 emitter from producing tab-leading multiline output that its own
  parser rejects.
- JSON, XML, and SOAP input byte limits now use `wire.ErrSizeLimit`, and their
  invalid decode targets now use `wire.ErrTarget`, matching every other format.
- Hostile-reader regressions prove every streaming decoder reads no more than
  `MaxBytes + 1` bytes before rejecting an oversized source.
- Cross-format target tests prove successful reuse and record partial mutation
  on conversion failure; adversarial benchmarks cover hostile decode shapes
  and cyclic encodes with allocation counts.
- Decoder fuzz corpora now seed empty/whitespace input, deep nesting,
  duplicates, invalid encodings and numbers, multiple documents, aliases,
  tags/extensions, and malformed binary length fields as applicable.
- Dependency-differential tests lock the wrapper's stricter invalid-UTF-8 and
  duplicate-key policies and the YAML block-indentation repair against pinned
  codec behavior.
- A compiler-derived public API inventory test and audit evidence ledger map
  every exported option, boundary requirement, format hazard, fuzzer, and
  benchmark to traceable tests.
- An all-format input-shape matrix locks empty, whitespace-only, truncated, and
  concatenated behavior, including binary whitespace-as-data semantics.
- The public API inventory parses production files explicitly without the
  deprecated Go 1.25 `parser.ParseDir` helper.
- Cross-format malformed-input regressions ensure classified decoder errors do
  not echo sensitive values from the payload, and failed-target coverage avoids
  promising a dependency-specific partial-assignment order.
- The final hardening verdict records Go 1.25 compatibility, symmetric bounded
  APIs, current direct codec dependencies, and the remaining upstream risks.

[Unreleased]: https://github.com/faustbrian/golib/pkg/wire/compare/v0.0.0...HEAD
