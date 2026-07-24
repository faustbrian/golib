# Goal: Audit and harden `jsonapi`

## Mission

Perform an evidence-driven specification, security, interoperability,
compatibility, and resource-safety audit of `jsonapi`, then implement every
justified hardening change required for a trustworthy JSON:API boundary.

Audit the JSON:API 1.1 core specification, the Atomic Operations extension,
the Cursor Pagination profile, registered extension/profile behavior, query
parsing, content negotiation, codecs, validation, and execution seams. Do not
treat 100% coverage or existing conformance matrices as proof without tracing
each claim to code and adversarial tests.

## Authoritative inputs

- The official JSON:API 1.1 specification.
- The official Atomic Operations extension.
- The official Cursor Pagination profile.
- Official JSON:API recommendations, clearly separated from normative rules.
- Relevant HTTP semantics and media-type specifications.
- Go's `encoding/json`, `net/http`, `net/url`, and error contracts.
- The repository's `.ai/GOAL.md`, `AGENTS.md`, matrices, public API, fixtures,
  docs, tests, fuzzers, benchmarks, and changelog.

Use primary sources and stable section links. Label every requirement as core,
extension, profile, recommendation, application policy, or package policy.

## Phase 1: Establish the baseline

1. Inventory every exported API, document/member type, validation context,
   option, query family, negotiation rule, extension/profile registry,
   transaction seam, error, limit, dependency, fuzzer, and benchmark.
2. Rebuild the conformance matrix from the primary specifications, linking
   every normative statement to source, tests, fixtures, and documentation.
3. Run the complete format, vet, Staticcheck, race, coverage, fuzz, benchmark,
   docs, vulnerability, and workflow gates; record flakes and skips.
4. Produce a threat model covering malicious documents, hostile query strings,
   content negotiation abuse, extension/profile confusion, resource
   exhaustion, error disclosure, and faulty transaction implementations.
5. Record every undocumented behavior and every documented claim without
   executable evidence.

Do not alter production behavior until a failing regression proves the gap.

## Core document and codec audit

Prove behavior for:

- top-level `data`, `errors`, `meta`, `jsonapi`, `links`, `included`, and
  extension members, including all mutual-exclusion and presence rules;
- resource identifiers, resource objects, local IDs, type/ID requirements,
  attributes, relationships, links, meta, and reserved/member-name rules;
- relationship linkage for null, to-one, to-many, empty, links-only, and
  meta-only relationships;
- compound-document full linkage, uniqueness, duplicate resources, cycles,
  conflicting representations, and include-path semantics;
- error objects, sources, pointers, parameters, headers, status/code/title,
  links, meta, and multiple errors;
- strict decoding of unknown or extension members by scope;
- duplicate JSON members, invalid UTF-8, number precision, deep nesting,
  oversized documents, trailing values, and scalar top-level values;
- deterministic output, stable member ordering policy, round trips, and
  non-mutation of caller-owned data;
- validation-context differences for requests, responses, relationships, and
  endpoint-specific constraints;
- typed errors and preservation of useful causes without leaking document data.

## Negotiation and query audit

Audit:

- exact `application/vnd.api+json` parsing and serialization;
- `ext` and `profile` parameters, quoted lists, duplicates, unknown values,
  parameter ordering, case rules, and forbidden media-type parameters;
- `Accept` quality values, wildcards, specificity, ties, unacceptable
  candidates, malformed values, and deterministic selection;
- query parsing for `include`, `fields`, `sort`, `page`, `filter`, custom
  families, and extension namespaces;
- duplicates, empty values, percent-encoding, Unicode, bracket syntax,
  conflicting families, huge key/value counts, and bounded complexity;
- separation between parsing syntax and application-defined filtering,
  sorting, cursor, or authorization policy.

## Atomic Operations audit

Prove:

- document shape and media-type extension requirements;
- operation/ref/href/data/meta/member rules for every operation type;
- local-ID references, relationship operations, ordering, and result
  cardinality;
- transaction begin, commit, rollback, error propagation, panic behavior,
  context cancellation, and exactly-once cleanup calls;
- prevention of partial success when atomicity is promised;
- safe mapping of execution failures to JSON:API error objects without
  disclosing internal state.

Do not claim database atomicity beyond the transaction interface contract and
the caller's implementation.

## Cursor Pagination audit

Prove:

- supported `page` parameters and mutual-exclusion rules;
- cursor presence, empty values, direction, limits, range pagination, and
  boundary conditions;
- first, last, prev, and next links for empty, first, middle, and final pages;
- item cursors, page metadata, exact total, estimated total, and their
  omission/null rules;
- profile negotiation and profile-specific errors;
- opaque treatment of cursor values without accidental decoding or policy;
- bounded cursor and query sizes and safe link construction.

## Extensions, profiles, and API audit

- Verify namespace/member syntax, allowed scopes, collisions, registry
  immutability, duplicate registration, and validator invocation counts.
- Verify profile validators cannot weaken core or extension requirements.
- Audit nil traps, unsafe zero values, ownership/aliasing, mutable registries,
  concurrency, and inspectable error contracts.
- Treat accepted documents, emitted bytes, validation, negotiation, query
  parsing, and execution order as SemVer-governed behavior.
- Prefer additive fixes and document breaking corrections with migrations.

## Security and resource hardening

- Enforce documented limits for body size, nesting, collection sizes, included
  resources, operations, errors, query parameters, extension members, and
  validator work.
- Prevent integer overflow, quadratic validation, recursion exhaustion,
  unbounded allocation, panics, races, and mutation during validation.
- Redact payloads, attributes, relationship data, cursors, URLs, and extension
  values from errors or logs unless callers explicitly choose otherwise.
- Audit URLs and links as data; do not silently impose application URL or SSRF
  policy, but document ownership clearly.

## Test and hardening requirements

- Add a failing regression before each behavioral fix.
- Maintain meaningful 100% production statement coverage.
- Fuzz core and atomic decoding, validation, member registries, negotiation,
  query parsing, cursor metadata, and marshal/unmarshal round trips.
- Seed fuzzers with official examples, invalid documents, duplicate members,
  deep compound graphs, hostile media types, query explosions, and every
  discovered regression.
- Add race, aliasing, panic, cancellation, rollback, resource-bound, and
  deterministic-output tests.
- Benchmark representative and adversarial compound documents, atomic batches,
  negotiation headers, queries, and pagination metadata.
- Run the full repository quality and security gate without unexplained skips.

## Required Deliverables

1. A hardening report with severity, specification classification, section
   link, evidence, impact, reproduction, and disposition.
2. Rebuilt core, Atomic Operations, Cursor Pagination, extension/profile, and
   recommendation matrices with executable evidence.
3. Focused regression tests, fuzz seeds, fixes, and documentation.
4. Updated API, security, conformance, feature, troubleshooting, compatibility,
   migration, performance, and changelog documentation.
5. A final release-readiness verdict with exact commands and results,
   remaining risks, and the semantic-version recommendation.

## Release Blockers

- Any unproven specification divergence, unsafe extension behavior, protocol
  ambiguity, resource-exhaustion path, or security defect.
- Missing meaningful 100% coverage or a failing GitHub Actions gate.

## Completion Criteria

The work is complete only when:

- every normative core, Atomic Operations, and Cursor Pagination rule has
  traceable passing evidence;
- recommendations and application policy are never presented as normative;
- every high- and medium-severity finding is fixed or rejected with evidence;
- parsing, validation, negotiation, query, pagination, and execution paths are
  bounded, panic-free, race-free, and accurately documented;
- no known atomicity, full-linkage, negotiation, number-precision, duplicate,
  or information-disclosure gap remains;
- the complete quality, fuzz, vulnerability, and documentation gates pass; and
- the final report does not overstate compliance, atomicity, interoperability,
  or production readiness.
