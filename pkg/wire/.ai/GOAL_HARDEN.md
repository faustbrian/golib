# Goal: Audit and harden `wire`

## Mission

Perform an evidence-driven security, correctness, interoperability, and
resource-safety audit of every wire format supported by `wire`, then
implement all justified hardening needed for production trust boundaries.

Discover the supported format packages from the repository at audit time. The
scope includes JSON, XML, SOAP, and any added YAML, TOML, MessagePack, CBOR,
BSON, or future format packages. Audit both byte-slice and streaming read/write
APIs. Do not assume API similarity means format semantics are equivalent.

## Authoritative inputs

- The primary specification for each supported format and protocol version.
- Go's standard-library contracts for JSON, XML, readers, writers, errors,
  contexts, and memory safety.
- Primary documentation and security guidance for every third-party codec.
- The repository's `.ai/GOAL.md`, `AGENTS.md`, format matrix, API docs, fixtures,
  compatibility policy, tests, fuzz targets, benchmarks, and changelog.

Record exact dependency versions and specification editions. Clearly separate
normative behavior, library policy, dependency behavior, and intentional
limitations.

## Phase 1: Establish the baseline

1. Inventory every supported format, exported symbol, option, default, size
   limit, recursion path, allocation, reader/writer boundary, error class,
   dependency, fixture, fuzzer, and benchmark.
2. Build a per-format behavior matrix linking specification requirements and
   documented guarantees to implementation and test evidence.
3. Run formatting, vet, Staticcheck, race, exact coverage, docs, fuzz,
   benchmarks, vulnerability scanning, and workflow validation.
4. Record flakes, skips, unsupported shapes, dependency limitations, and
   claims that lack evidence.
5. Produce a threat model for malicious payloads, malicious destinations,
   parser differentials, resource-exhaustion inputs, and unsafe application
   assumptions.

Do not change behavior until a minimal failing regression or fuzz seed proves
the issue.

## Cross-format boundary audit

For every decoder and encoder, prove:

- nil, typed-nil, non-pointer, invalid, unsupported, and reused targets;
- zero, negative, boundary, and overflowing size limits;
- exact behavior for empty, whitespace-only, truncated, trailing, concatenated,
  and multiple-document inputs;
- invalid UTF encodings, BOMs, embedded NULs, duplicate members or keys,
  invalid numbers, deep nesting, huge collections, and cyclic values;
- bounded streaming reads that do not read or allocate beyond documented limits;
- short writes, zero-progress writers, partial writes, destination errors, and
  correct `wire.ErrWrite` classification;
- deterministic or canonical output only where the format and options can
  actually guarantee it;
- stable, inspectable error classification without leaking full sensitive
  payloads or losing useful causes;
- absence of panics, stack exhaustion, unbounded allocations, hangs, races,
  and mutation surprises;
- parity between byte and reader/writer APIs without duplicate inconsistent
  implementations.

Audit `DetectFormat` separately. It must never claim certainty that the bytes
cannot support. Ambiguous text and binary formats must remain explicit unless
the detection contract is reliable, tested, and documented.

## Format-specific audit

### JSON

Audit duplicate keys, number precision, unknown fields, single-value
enforcement, BOM/whitespace normalization, HTML escaping, map ordering, depth,
and trailing values.

### XML and SOAP

Audit strict versus non-strict parsing, namespaces, prefixes, root validation,
directives, entities, charset conversion, invalid bytes, token depth, multiple
roots, raw-fragment preservation, envelope structure, header/body cardinality,
SOAP versions, faults, localized reasons, subcodes, and XML injection on write.

### YAML

Audit aliases, anchors, merge keys, duplicate keys, tags, implicit typing,
multi-document streams, alias expansion bombs, recursive structures, and
JSON-incompatible map keys.

### TOML

Audit duplicate and dotted keys, tables and arrays of tables, datetime
precision, integer and float boundaries, special floats, unknown fields,
trailing input, and deterministic ordering policy.

### MessagePack

Audit integer widths, map keys, duplicate keys, extension types, timestamps,
nil, concatenated objects, depth and collection limits, and canonical ordering
claims.

### CBOR

Audit deterministic/canonical modes, tags, duplicate map keys, indefinite
lengths, preferred serialization, simple values, floats, big numbers, nesting,
and decoder resource limits.

### BSON

Audit document versus scalar rules, length prefixes, terminators, duplicate
keys, ObjectID, datetime, decimal and numeric widths, binary subtypes, regex,
raw documents, and malformed length-based allocations.

If a listed format is not implemented, record it as out of the current audit
rather than inventing support. If another format exists, add an equivalent
format-specific section to the report.

## Dependency and compatibility audit

- Justify every codec dependency using maintenance, security, specification
  fidelity, Go/platform support, and transitive dependency evidence.
- Identify dependency defaults that conflict with `wire` guarantees and
  enforce explicit safe modes at the package boundary.
- Verify exported docs and examples match actual defaults and limitations.
- Treat accepted inputs, emitted bytes, error classes, limits, and option
  defaults as SemVer-governed behavior.
- Prefer additive hardening. Document breaking corrections and migrations.

## Test and hardening requirements

- Write failing regressions before behavioral fixes.
- Maintain meaningful 100% production statement coverage.
- Maintain at least one decoder fuzz target per format and add encoder or
  round-trip fuzzing where it proves useful invariants.
- Seed fuzzers with specification examples, malformed length fields, deep
  structures, duplicate keys, invalid encodings, dependency CVE shapes, and
  every discovered regression.
- Add cross-API parity tests, hostile reader/writer tests, allocation bounds,
  and dependency differential tests where appropriate.
- Benchmark representative and adversarial decode/encode paths and track
  allocations without turning unstable performance samples into correctness
  gates.
- Run the entire repository hardening gate with no unexplained skips.

## Required Deliverables

1. A finding report with severity, format, API, payload or reproduction,
   evidence, impact, and disposition.
2. A per-format conformance, safety, and intentional-limit matrix.
3. Focused tests, fuzz seeds, fixes, and documentation for each finding.
4. Updated API, format, security, troubleshooting, compatibility, migration,
   performance, dependency, and changelog documentation.
5. A final release-readiness verdict with exact commands and results,
   dependency residual risks, and semantic-version recommendation.

## Release Blockers

- Any format divergence, unsafe decoding, unbounded hostile-input behavior,
  silent data corruption, or interoperability defect.
- Missing meaningful 100% coverage or a failing GitHub Actions gate.

## Completion Criteria

The work is complete only when:

- every supported format and every read/write API has traceable evidence;
- every high- and medium-severity finding is fixed or rejected with evidence;
- all untrusted parsing and writing paths are bounded and panic-free;
- format-specific security hazards and intentional limits are documented;
- no deterministic, canonical, strict, or interoperable behavior is claimed
  beyond what tests and specifications prove;
- the full quality, fuzz, vulnerability, and documentation gates pass; and
- the report accurately distinguishes verified guarantees from dependency
  behavior and unsupported features.
