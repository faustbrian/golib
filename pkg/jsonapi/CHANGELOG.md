# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and releases follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

### Changed

- Regenerated the complete machine-readable documentation bundle from the
  current user, adoption, compatibility, and release documentation.
- Expose JSON:API specification verification as an explicit conformance gate.
- Added the `GO-SAFETY-1` ownership, concurrency, race, fuzz, resource, and
  benchmark standard with an executable `make safety` gate.
- Moved AI planning and hardening briefs into `.ai/` and clarified the
  separate purposes of project and third-party notice files.

### Added

- A standardized OSS repository skeleton covering policy, documentation,
  legal notices, Go tooling, pinned CI, security, and release automation.
- Evidence-driven audit and hardening goal covering JSON:API 1.1, Atomic
  Operations, Cursor Pagination, negotiation, queries, and resource safety.
- Strict JSON:API 1.1 document models, codecs, validation, links, errors,
  compound-document support, and deterministic serialization.
- Query parsing primitives for inclusion, sparse fieldsets, sorting,
  pagination, filtering, implementation families, and extension namespaces.
- Content negotiation for the JSON:API media type, extensions, profiles,
  quality values, and wildcard candidates.
- Complete Atomic Operations document validation and transaction-oriented
  execution interfaces.
- Cursor Pagination query, link, metadata, item-cursor, total, estimated-total,
  and profile error helpers.
- Extension-member registries across JSON:API-defined object scopes and
  document-level profile validation hooks.
- Golden fixtures, malformed-input regressions, round-trip tests, fuzz targets,
  and representative benchmarks.
- Project documentation, conformance matrices, adoption guidance, and
  contribution and security policies.
- GitHub Actions quality, compatibility, fuzzing, security, benchmark,
  documentation, and tagged-release automation.
- MIT licensing for public use, modification, and distribution.
- Bounded decoding with configurable byte, nesting, object-member, array-item,
  and total-value limits shared by core, Atomic, and configured codecs.
- Bounded query and media negotiation APIs with production defaults for
  decoded parameters, header candidates, and extension/profile URI lists.
- Distinct registered scopes for extension members inside `links` objects and
  individual link objects, including opaque links-object value helpers.
- A finding ledger, verification-backed hardening report, and package threat
  model for the pre-v1 audit.
- Constructed-validation, member-registry, cursor-metadata, and canonical
  round-trip fuzz targets plus adversarial compound and pagination benchmarks.
- `CallbackError` for redacted, inspectable extension/profile/cursor/sort
  callback failures and panic values.

### Fixed

- Bound fuzz-smoke concurrency to avoid deadline flakes on high-core hosts.
- Preserve large JSON numbers in attributes without `float64` precision loss.
- Invoke registered member validators only once during configured decoding.
- Preserve explicitly empty string members, including empty resource IDs and
  empty URI-references, without confusing presence with a Go zero value.
- Allow update-request endpoint identity checks to target a valid empty ID
  through explicit expected-ID presence state.
- Preserve empty `id` and `lid` presence when Atomic relationship data is
  validated as resource identifier objects.
- Support href-targeted Atomic relationship add, update, and remove data
  shapes while rejecting relationship-shaped operations without a target.
- Validate Atomic results against their request operations before commit,
  including data-free removal/relationship results and singular resource data.
- Require lid-only Atomic resource targets and relationship linkage to resolve
  to the current or a prior resource add operation.
- Reject directly comparable type, ID, or LID mismatches between an Atomic
  resource update's `ref` target and `data` representation.
- Resolve compound-document linkage through either `id` or `lid` when an
  included resource carries both identities.
- Detect duplicate canonical resource objects across `id` and `lid` aliases.
- Reject pagination links beside non-collection top-level data and known
  to-one relationship linkage.
- Require optional `jsonapi.ext` and `jsonapi.profile` arrays to include all
  configured applied URIs while continuing to ignore unknown profiles.
- Ignore unrecognized profiles when calculating `Accept` media-range
  specificity so they cannot override an otherwise acceptable base range.
- Separate forward-compatible @-Member names from semantic member names and
  ignore constructed @ members in relationships and links.
- Require error-object `status` strings to contain a valid HTTP status code in
  the inclusive range from 100 through 599.
- Reject core link members used in the wrong links-object scope while retaining
  registered extension members and the core pagination names where permitted.
- Validate URI-references and absolute registration URIs using RFC 3986 wire
  characters, requiring spaces, Unicode, and reserved path data to be escaped.
- Reject underscores in registered link relation types as prohibited by the
  Web Linking grammar.
- Reserve only `type` and `id` in resource field namespaces, allowing `lid` as
  an attribute or relationship name as required by JSON:API 1.1.
- Reject U+007F DELETE in member names while retaining the specification's
  globally allowed U+0080-and-above Unicode range.
- Reject invalid UTF-8 in constructed member names before encoding can replace
  it and change the registered or validated name.
- Bound constructed `describedby` link chains and reject pointer cycles before
  recursive validation can exhaust the stack.
- Reject profile validators that mutate their document view so callbacks
  cannot invalidate already-checked core or extension semantics.
- Reject extension member names that place `@` after the namespace separator;
  the @-Member exception applies only at the start of the complete name.
- Accept valid constructed @-Members without extension registration while
  preventing them from satisfying required document or relationship content.
- Require relationship-only links to include `self` or `related` rather than
  accepting an empty or unrelated links object as relationship content.
- Enforce Cursor Pagination boundary-link nullability and support aliases for
  the profile's `page` metadata element across pages, items, and errors.
- Accept mathematically integral JSON number forms in Cursor Pagination totals
  without binary floating-point conversion or precision loss.
- Reject malformed UTF-8 instead of accepting Go's replacement-character
  decoding at JSON:API boundaries.
- Enforce the HTTP `qvalue` grammar instead of accepting floating-point forms
  such as `NaN`, exponents, signs, or excess fractional precision.
- Enforce literal U+0020 separators in media-type extension and profile URI
  lists instead of normalizing tabs or Unicode whitespace.
- Preserve cursor/sort validator causes through `errors.Is` without copying
  their potentially sensitive text into public pagination errors.
- Contain extension/profile/cursor/sort callback panics and redact returned
  callback error text while preserving trusted `errors.Is`/`errors.As` access.
- Allow a registered extension member to provide the required content of an
  error object while keeping ignored @-Members structurally invisible.
- Require Atomic create results to return server-generated resource data before
  commit while retaining the extension's client-generated-ID omission rule.
- Convert panics from Atomic transaction callbacks into typed, redacted
  `AtomicPanicError` failures and attempt rollback exactly once.
- Reject canceled or nil contexts before beginning an Atomic transaction.
- Apply HTTP media-range specificity before quality when selecting a JSON:API
  representation, so a wildcard cannot override a more specific rejection.
- Reject unknown core or Atomic validation contexts and negative Atomic
  expected-result counts instead of silently weakening contextual checks.
- Enforce canonical SemVer release tags, including leading-zero and prerelease
  identifier rules, before publishing artifacts.
- Decouple validated changelog release dates from the wall-clock day on which
  a prepared tag is published.

[Unreleased]: https://github.com/faustbrian/golib/pkg/jsonapi/commits/main
