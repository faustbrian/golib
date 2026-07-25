# Goal: Localized Value Foundation

## Objective

Build a production-grade open-source Go package for immutable values keyed by
BCP 47 language tags, with deterministic lookup, matching, fallback, merging,
validation, encoding, and persistence semantics.

The package MUST replace ad hoc `map[locale]string` values across services while
remaining a focused domain-value library. It MUST NOT become a translation
catalog, message formatter, pluralization engine, UI framework, or remote
translation-management client.

## Product Principles

- Locale identity uses standards-backed BCP 47 tags, not arbitrary strings.
- Exact lookup, language matching, and application fallback are different
  operations with explicit policies.
- Values are immutable and safe for concurrent reads.
- Missing, present-empty, invalid, and fallback-resolved values are distinct.
- Canonicalization never invents translations or silently discards variants.
- Ordering and serialization are deterministic regardless of Go map order.
- Generic values are optional; localized text receives the strongest ergonomic
  and interoperability support.
- `international` owns locale primitives and registry data.

## Core Value Model

- Immutable `Text` for BCP 47-tagged UTF-8 strings.
- Optional generic `Value[T]` only if it preserves a comprehensible API,
  immutable ownership, validation, and encoding behavior.
- Construction from entries, maps, pairs, builders, and canonical wire values.
- Explicit duplicate-tag policy after canonicalization.
- Presence, count, locale list, exact get, required get, and deterministic
  iteration.
- Addition, replacement, removal, filter, map, and merge return new values.
- Stable equality, canonical representation, hashing, and zero-value semantics.
- Caller maps, slices, byte strings, and mutable generic values MUST NOT alias
  internal state unexpectedly.

## Locale Identity And Canonicalization

- Integrate with `international` locale types backed by BCP 47 and the IANA
  Language Subtag Registry.
- Treat tags case-insensitively while preserving or producing a documented
  canonical representation.
- Support language, script, region, variant, extension, private-use,
  grandfathered, and deprecated-tag behavior according to the locale layer.
- Registry updates and canonical preferred-value mappings are versioned,
  reproducible, and visible in compatibility documentation.
- `und`, `mul`, private-use tags, and invalid or unknown tags have explicit
  acceptance and matching policies.
- No process-global default locale or mutable registry.

## Lookup, Matching, And Fallback

- Exact lookup performs no implicit fallback.
- Standards-aligned language matching is distinct from configured fallback.
- Ordered caller preferences support quality weights and stable tie-breaking
  through optional HTTP `Accept-Language` adapters.
- Configured fallback chains can include exact tags, parent language ranges,
  application defaults, and final missing behavior.
- Script and region removal follows an explicit matcher rather than naive string
  truncation.
- Lookup results identify exact, matched, fallback, default, missing, or
  present-empty resolution.
- Cyclic fallback chains, duplicates, excessive candidates, and invalid ranges
  fail predictably.
- Fallback never writes or materializes an invented localized entry.

## Merge And Conflict Semantics

- Explicit left-wins, right-wins, reject-conflict, and resolver policies.
- Conflict detection occurs after locale canonicalization.
- Present-empty and absent behavior is configurable only through named policy.
- Merge results preserve deterministic ordering and immutable ownership.
- Three-way merge MAY be provided only with fully specified conflict behavior.
- Bulk overlays and defaults have explicit cardinality and allocation limits.
- Provenance MAY be returned separately but MUST not leak into value equality
  unless a distinct source-aware type is used.

## Text Validation And Normalization

- Require valid UTF-8 by default and distinguish empty from whitespace-only.
- Optional Unicode normalization policy is explicit and never silently changes
  identifiers or user-authored text.
- Bound bytes, runes, grapheme-aware concerns where claimed, line count, and
  control characters through composable validation policies.
- No HTML safety claim; escaping remains the responsibility of output contexts.
- No language detection, machine translation, transliteration, profanity
  filtering, or semantic equivalence guesses.

## Parsing And Encoding

- Stable deterministic JSON object encoding keyed by canonical language tags.
- Optional entry-array encoding when duplicate detection, order, or metadata
  requires it.
- Strict and permissive decode modes with explicit unknown/invalid handling.
- Text, JSON, YAML, TOML, and MessagePack integration belongs through `wire`
  adapters rather than mandatory core dependencies.
- Canonical encoding defines ordering, escaping, duplicate handling, null,
  empty, and zero-value semantics.
- Invalid UTF-8, duplicate canonical tags, oversized values, deep nesting,
  trailing input, and unsupported data fail with typed bounded errors.

## Persistence And Integration

- `database/sql` scanner/valuer and native `pgx` JSONB codecs.
- Optional normalized-row helpers without owning migrations or database access.
- `wire`, `validation`, `config`, and `api-query` adapters.
- `http-client` adapter for `Accept-Language` negotiation and response
  selection where useful, without coupling core values to HTTP.
- `jsonapi`, `jsonrpc`, and `openrpc` examples for localized fields and
  documentation descriptions without changing those specifications.
- Migration fixtures for locale-keyed JSON columns and Spatie Translatable data
  used by Track, Postal, and Location.

## Concurrency And Observability

- Immutable values support safe concurrent reads and deterministic iteration.
- No package-global fallback locale, mutable cache, goroutine, exporter, or
  registry refresh.
- Optional hooks report bounded operation, outcome, candidate count, and match
  kind without logging localized content or high-cardinality language chains.
- Hooks are isolated, panic-safe according to documented policy, and cannot
  alter resolution outcomes.

## Security And Resource Bounds

- Bound locales per value, tag bytes, text bytes, parser input, fallback depth,
  match candidates, merge output, diagnostics, and allocations.
- Threat-model Unicode confusables, invalid UTF-8, duplicate canonical tags,
  private-use leakage, fallback cycles, locale enumeration, and content exposure.
- Errors and telemetry MUST NOT include full localized values by default.
- Production code MUST NOT use unsafe, cgo, `go:linkname`, mutable globals, or
  unbounded reflection.

## Non-Goals

- No gettext replacement, message catalog, ICU MessageFormat, pluralization,
  interpolation, translation loader, remote translation API, UI localization,
  language detection, machine translation, or content management.
- No ownership of BCP 47 parsing or registry lifecycle when `international`
  provides that contract.
- No HTTP server middleware, database ORM, application default configuration,
  authorization, or business-specific required-language policy.
- No implicit global locale or magical fallback chain.

## Package Shape

- Root: immutable localized text/value types, entries, errors, and iteration.
- `match`: explicit preference matching and fallback plans.
- `encoding`: canonical JSON and stable entry representations.
- `http`: optional `Accept-Language` parsing and negotiation adapters.
- `postgres`: SQL/pgx JSONB codecs and migration fixtures.
- `localizedwire`, `localizedvalidation`, and `localizedconfig`: adapters.
- `localizedtest`: builders, standards vectors, fixtures, and assertions.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Tests MUST detect
locale, fallback, conflict, encoding, and ownership defects rather than merely
execute statements.

Required evidence includes:

- exhaustive exact, parent, script, region, variant, private-use, missing,
  empty, default, and weighted-preference lookup matrices
- BCP 47 and language matching vectors sourced from authoritative standards and
  independently maintained implementations where suitable
- canonicalization, deterministic ordering, equality, immutability, and
  round-trip properties
- merge conflict matrices across every policy and absent/empty combination
- fallback cycle, depth, duplicate, cardinality, and tie-breaking properties
- hostile JSON, locale, Unicode, UTF-8, quality weight, and fallback fuzzing
- race and aliasing tests for shared values, iterators, codecs, and hooks
- mutation tests for exact/match/fallback and conflict decisions
- PostgreSQL JSONB and legacy Spatie/Track/Postal/Location compatibility tests
- benchmarks with allocations for construction, exact lookup, matching,
  fallback, merge, large locale sets, encoding, and decoding

## Documentation Deliverables

- Five-minute construction, exact lookup, fallback, merge, JSON, and PostgreSQL
  quickstarts.
- Complete API reference for every exported type, policy, result, and error.
- Formal lookup, matching, fallback, empty/missing, merge, and encoding tables.
- Adoption guides for localized domain values, HTTP negotiation, APIs, and
  migration from locale-keyed maps and Spatie Translatable JSON.
- Cookbook examples for common regional fallbacks, script-sensitive matching,
  required languages, validation, persistence, wire formats, and testing.
- Security model, performance guide, FAQ, troubleshooting, compatibility,
  architecture, roadmap, contribution guide, and maintained changelog.
- Every public API and realistic user-facing scenario MUST be documented so
  consumers can adopt the package without reading implementation source.

## Automation And Release

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, standards and legacy compatibility, PostgreSQL
integration, vulnerability scans, benchmarks, docs, API compatibility, and
releases. Every blocking command MUST be reproducible locally through documented
`make` targets.

Repository setup MUST include README badges for every blocking workflow/job,
Dependabot, security policy, contribution guide, code of conduct, license,
notice and third-party attribution handling, release automation, changelog,
repository topics, and complete adoption documentation.

## Execution Plan

1. Specify locale dependency, immutable ownership, zero values, matching,
   fallback, conflict, and encoding semantics.
2. Implement localized text, deterministic iteration, mutation operations, and
   exact lookup.
3. Implement standards-aligned matching, fallback plans, merges, and limits.
4. Add wire, HTTP, validation, PostgreSQL, and API adapters.
5. Prove standards, legacy, fuzz, race, mutation, and performance behavior.
6. Complete adoption documentation and release v1.

## Acceptance Criteria

- Locale-keyed values cannot be mutated through caller-owned data.
- Exact lookup, standards matching, and configured fallback remain visibly
  distinct and deterministic.
- Missing and present-empty values cannot be confused accidentally.
- Canonical serialization and persistence round-trip without semantic drift.
- Track, Postal, and Location have documented migration paths.
- Meaningful 100% coverage and every required GitHub Actions gate pass.
