# Goal: International Data Primitives

## Objective

Build a serious open-source package for typed international identifiers and
metadata used consistently across services: countries, subdivisions, languages,
locales, currencies, calling codes, phone numbers, and postal-code values.

The package MUST replace the reusable parts of `cline/intl`, country-list
packages, and phone-number wrappers while preserving authoritative provenance,
versioned data updates, interoperability, and explicit boundaries.

## Product Principles

- Standards-backed identifiers are distinct immutable types, not arbitrary
  strings or one universal code type.
- Authoritative datasets and their versions/provenance are first-class.
- Parsing, validation, normalization, and display formatting are separate.
- No country-specific business rule is inferred from a generic identifier.
- Locale and Unicode behavior are explicit and deterministic.
- Data updates are reviewable, reproducible, diffable, and compatibility tested.

## Country And Subdivision Data

- ISO 3166-1 alpha-2, alpha-3, and numeric country codes.
- ISO 3166-2 subdivision identifiers where authoritative data licensing permits.
- Conversion between representations only through authoritative mappings.
- Official, reserved, transitional, deleted, and user-assigned status where data
  supports it.
- Country names are locale-aware display metadata, never stable identifiers.
- Historical aliases MUST be opt-in and preserve their status.

## Language And Locale Data

- BCP 47 language tags using maintained standards-aware parsing.
- ISO language identifiers where required and unambiguous.
- Canonicalization, casing, script, region, variants, extensions, and private-use
  semantics backed by authoritative registry data.
- Locale fallback is an explicit policy, not automatic lossy truncation.
- No message catalog, translation framework, or content negotiation in core.

## Currency Data

- ISO 4217 alphabetic and numeric codes, minor-unit metadata, active/historic
  status, and effective dates where authoritative data is available.
- Currency identity MUST remain separate from money arithmetic and formatting.
- Metadata changes MUST not silently reinterpret persisted values.
- `money` may consume these types without creating a reverse dependency.

## Phone Numbers

- International parsing and validation through a maintained libphonenumber-
  compatible implementation or audited generated metadata.
- E.164 canonical representation and explicit national/international display.
- Region hints, country calling codes, extensions, possible-versus-valid status,
  and number type where reliable.
- Metadata version and update procedure are public and reproducible.
- No SMS delivery, verification ownership, contact storage, or identity claims.

## Postal Codes

- Typed, bounded postal-code value preserving caller-provided country context.
- Safe normalization primitives such as Unicode/ASCII space handling and casing
  only when explicitly selected.
- Country-specific syntax adapters MAY be included when backed by authoritative
  maintainable evidence and independent vectors.
- The package MUST NOT claim deliverability, locality, address correctness,
  geocoding, or carrier acceptance from syntax alone.
- Postal search and provider-specific mandatory-code behavior remain in Postal.

## Encoding And Integration

- Text, JSON, SQL scanner/valuer, `pgx`, and `wire` codecs with strict input.
- `validation` rules for each typed primitive.
- `config` decode hooks.
- `geo` remains responsible for coordinates and spatial algorithms.
- Optional formatting integration MAY use `golang.org/x/text` without wrapping
  the entire localization ecosystem.
- Zero values, nullable values, and absent values MUST have explicit behavior.

## Dataset Governance

- Every generated dataset records source, retrieval date, upstream version,
  license, checksum, generator version, and transformation steps.
- Generation MUST be deterministic and runnable locally.
- CI MUST detect generated-data drift and validate licenses/provenance.
- Data diffs MUST classify additions, removals, aliases, status changes, and
  metadata changes before merge.
- Releases MUST state data versions and compatibility impact.
- No runtime network fetch is required for core identifier behavior.

## Security And Resource Bounds

- Bound input bytes, Unicode normalization work, tag segments, phone parsing,
  metadata tables, postal patterns, and diagnostic output.
- Reject invalid UTF-8 where a contract requires UTF-8; never repair silently.
- Threat-model Unicode confusables, normalization mismatch, regex denial of
  service, numeric ambiguity, metadata poisoning, and supply-chain compromise.
- No personal phone number or postal code in telemetry/logs by default.

## Non-Goals

- No address validation, geocoding, opening hours, translation catalog, money
  arithmetic, tax, customs, sanctions, carrier routing, or delivery validation.
- No generalized "internationalization framework" or UI formatting system.
- No automatic locale detection from IP, phone, country, or environment.
- No undocumented acceptance of obsolete or unofficial identifiers.
- No home-grown phone numbering metadata without authoritative update support.

## Package Shape

- Root: shared status, provenance, update, and safe error conventions.
- `country`, `subdivision`, `language`, `locale`, `currency`, `phone`, `postal`.
- `internationalpgx`, `internationalwire`, and `internationalvalidation`.
- `internationaltest`: authoritative fixtures, vectors, and assertions.
- `internal/generate`: deterministic dataset acquisition and generation tools.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Required evidence:

- authoritative and independent vectors for every standard and dataset
- round-trip, canonicalization, alias, deleted-code, and status properties
- differential phone tests against independent libphonenumber behavior
- malformed Unicode, tag, number, code, postal, and generated-data fuzzing
- race tests for immutable shared metadata and formatter/parser use
- mutation testing of acceptance, canonicalization, and status decisions
- deterministic generator and dataset-diff tests
- benchmarks for lookup, parse, canonicalization, phone, and bulk conversion

## Documentation Deliverables

- Five-minute country, phone, locale, currency, and postal quickstarts.
- Complete API, standard coverage, data provenance, and compatibility reference.
- Guides for parsing versus validation, canonicalization, databases, JSON,
  config, validation, data updates, privacy, and migrations.
- Migration guide from `cline/intl`, Brick PhoneNumber, and country-list usage.
- Security, performance, FAQ, troubleshooting, examples, and changelog.
- Every exported API and user-facing scenario MUST be documented.

## Automation And Release

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, provenance/license verification, generated-data
drift, vulnerability scans, benchmarks, docs, compatibility, and releases. All
blocking commands MUST be locally reproducible through documented `make` targets.

## Execution Plan

1. Specify identifier, status, provenance, error, and dataset governance models.
2. Implement country, language/locale, and currency packages with generators.
3. Implement phone and bounded postal primitives with authoritative fixtures.
4. Implement SQL, wire, config, and validation integrations.
5. Complete Unicode, provenance, mutation, fuzz, and performance hardening.
6. Publish complete adoption documentation and release v1.

## Acceptance Criteria

- Every accepted identifier has documented standard and dataset provenance.
- Parsing, canonicalization, validity, status, and display remain distinct.
- Data generation is deterministic and update drift is reviewable.
- Postal and phone APIs do not overclaim deliverability or identity.
- Meaningful 100% coverage and every required CI gate pass.
