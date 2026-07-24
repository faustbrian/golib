# Compatibility policy

## Versioning

The project follows Semantic Versioning.

- Before `v1.0.0`, minor releases may refine public APIs, but changes must be
  documented with migration guidance.
- At and after `v1.0.0`, incompatible exported API or wire-format changes
  require a new major version.
- Patch releases fix defects without intentionally changing valid behavior.

## Governed surface

Compatibility includes:

- exported Go names, method signatures, interfaces, and constants;
- JSON member presence, null/empty behavior, and canonical encoding;
- accepted and rejected document shapes;
- typed error fields, stable codes, and JSON pointer paths;
- query parsing and negotiation selection rules;
- official extension/profile URI constants and semantics;
- transaction callback ordering and rollback behavior;
- typed panic conversion and redacted execution error text;
- callback phases, cause unwrapping, and redacted extension/profile/cursor/sort
  error text;
- profile-validator purity and mutation rejection;
- constructor input-copying and concurrent-use guarantees;
- constructed recursive-link depth and cycle rejection;
- default resource limits and stable limit error classification;
- HTTP quality-value parsing and negotiation selection rules;
- supported Go version policy.

JSON object order is not semantically significant, but deterministic output is
still treated as a tested compatibility property.

## Deprecation

When practical, an obsolete exported API is deprecated in documentation and
kept through at least the next minor release before removal in a major release.
Immediate removal is reserved for severe security or correctness defects and
must include explicit release notes.

## Go support

The initial package requires Go 1.24 or later. Raising the minimum Go version
is announced in release notes and normally occurs in a minor release before
v1, or according to a documented support window after v1.

## Specification evolution

JSON:API 1.1 follows an additive compatibility model. New specification
members are reviewed before support is claimed. Until implemented, strict core
decoding may reject a newly defined member except where `@`-member or registered
extension/profile mechanisms apply. Such updates are tracked as protocol
compatibility work, not silently inferred.
