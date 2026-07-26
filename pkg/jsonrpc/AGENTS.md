# Package Maintenance Rules

These rules apply to all package work in this repository. RFC 2119 keywords
(MUST, MUST NOT, SHOULD, SHOULD NOT, MAY) are used intentionally.

## Release And Documentation Hygiene

- You MUST update `CHANGELOG.md` for every implementation task that creates,
  modifies, or deletes files before claiming completion.
- You MUST update `README.md`, examples, and package documentation when public
  behavior, configuration, installation, or usage changes.
- You MUST document breaking changes, removals, deprecations, and migration
  steps in `CHANGELOG.md`.
- You SHOULD provide a documented deprecation path before removing or renaming
  public APIs.

## Public API And Compatibility

- You MUST treat exported Go APIs, configuration, wire formats, error
  contracts, and command behavior as SemVer-governed surface area.
- You MUST NOT introduce backward-incompatible behavior without an explicit,
  documented reason and release-note coverage.
- You MUST keep changes focused. Unrelated refactors MUST be split into
  separate work.

## Testing And Verification

- You MUST add or update automated tests for every bug fix and user-visible
  behavior change.
- You MUST prefer regression coverage before changing existing behavior.
- You MUST maintain meaningful 100% coverage for production package code.
  Line-hitting without behavior proof does not satisfy this requirement.
- Before pushing, you MUST run `make check` and any package-specific
  integration checks required by `CONTRIBUTING.md`.
- You MUST report the exact verification commands and results when handing
  work off for review.

## Dependency Discipline

- You MUST keep dependencies minimal and MUST NOT add, upgrade, or remove one
  without a clear maintenance benefit.
- You MUST verify dependency constraints against the supported Go and platform
  matrix.
- You SHOULD prefer the standard library and existing project utilities for
  small conveniences.
- You MUST update `THIRD_PARTY_NOTICES.md` when copied, forked, generated, or
  vendored code changes attribution obligations.

## Runtime Safety

- You MUST treat concurrency, cancellation, shutdown, resource ownership, and
  error paths as first-class runtime concerns.
- You MUST NOT introduce shared mutable state without clear ownership and
  synchronization.
- You SHOULD prefer explicit context propagation, deterministic cleanup, and
  bounded resource lifetimes over globals or hidden background work.

## Go Safety Baseline

- You MUST follow the `GO-SAFETY-1` requirements in
  `docs/go-safety-and-concurrency.md`.
- Production code MUST NOT use `unsafe`, cgo, or `go:linkname`.
- Every goroutine and mutable shared value MUST have explicit ownership,
  synchronization, and lifecycle semantics.
- Concurrency MUST be bounded, cancellation-aware, and tested with
  deterministic coordination rather than timing sleeps.
- Concurrency-sensitive changes MUST pass `make safety`, including source
  policy, vet, lint, race, and fuzz gates.
- Untrusted parsing and protocol boundaries MUST have fuzz properties and
  explicit resource-bound tests.
- Performance work MUST include allocation-reporting benchmarks and
  statistically supported before-and-after evidence.
- You MUST NOT describe these controls as equivalent to Rust's compile-time
  ownership and data-race guarantees.

## Repository-Specific Rules

- JSON-RPC parsing, validation, dispatch, transport, and error behavior MUST
  remain specification-compliant and deterministic.
- Batch execution, cancellation, notification semantics, and malformed-input
  handling MUST be treated as security boundaries.
