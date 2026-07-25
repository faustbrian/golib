# Hardening Goal: International Data Primitives

## Objective

Prove that `international` remains standards-correct, deterministic,
privacy-safe, bounded, reproducible, and compatible under hostile Unicode,
malformed identifiers, historical data, metadata updates, and generated-dataset
supply-chain failure.

## Required Audits

### Standards And Dataset Audit

- Map every behavior to an authoritative standard, registry, or documented
  package policy.
- Verify source licenses, checksums, retrieval, transformations, generator
  versions, and deterministic output.
- Differential-test country, locale, currency, and phone data against
  independent implementations and current authoritative fixtures.
- Review additions, removals, aliases, historical transitions, and metadata
  changes for compatibility impact.

### Parsing And Unicode Audit

- Fuzz invalid UTF-8, normalization forms, confusables, mixed scripts, unusual
  whitespace, casing, separators, extensions, private-use tags, and huge input.
- Prove parse, normalize, canonicalize, validate, and format never collapse into
  undocumented coercion.
- Bound segments, lengths, recursion, regex work, errors, and allocations.
- Mutation-test every acceptance and canonicalization branch.

### Phone And Postal Audit

- Differential-test E.164, national, extension, region-hint, possible, valid,
  and number-type behavior.
- Exercise metadata version changes and historical phone fixtures.
- Ensure postal syntax never becomes a deliverability, locality, or address
  correctness claim.
- Verify phone and postal values do not leak into observations by default.

### Compatibility And Integration Audit

- Test JSON, text, SQL, pgx, config, wire, and validation round trips.
- Prove zero, null, absent, obsolete, reserved, and unknown states remain exact.
- Validate persisted values across dataset and package upgrades.
- Race-test concurrent metadata lookup, parsing, formatting, and generation.

## Required Deliverables

- Standards coverage and authoritative provenance matrices.
- Dataset update/diff report and reproducibility evidence.
- Unicode threat model, privacy review, and resource budgets.
- Differential, fuzz, mutation, race, integration, and benchmark reports.
- Updated API, standards, migration, security, data-update, and FAQ docs.

## Release Blockers

- Any standards divergence, non-reproducible data, unlicensed source, silent
  identifier reinterpretation, Unicode ambiguity, privacy leak, panic, race, or
  unbounded parsing behavior.
- Any phone/postal API claiming guarantees unsupported by authoritative data.
- Any generated change merged without classified compatibility review.
- Missing meaningful 100% coverage, mutation evidence, or green blocking CI.

## Completion Criteria

- Standards, provenance, Unicode, phone, postal, and integration suites pass.
- Dataset generation and update review are deterministic and documented.
- Race, fuzz, vulnerability, compatibility, and performance gates pass.
- NilAway runs visibly as advisory without blocking findings.
- No release blocker remains and the changelog is current.
