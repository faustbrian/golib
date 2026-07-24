# Goal: Secure Password Hashing And Upgrade Policy

## Objective

Build a narrowly scoped, production-grade password package for user-password
hashing, verification, encoded-hash parsing, policy upgrades, and safe migration
from existing Laravel password hashes.

The package MUST complement `authentication`; it MUST NOT manage users,
sessions, credentials, password reset flows, or authorization.

## Product Principles

- Use established cryptographic primitives, never custom password cryptography.
- Argon2id is the preferred default with explicit, benchmarked parameters.
- Bcrypt is supported for compatibility and migration.
- Encoded hashes are strict, versioned, bounded, and interoperable.
- Verification distinguishes match, mismatch, malformed hash, unsupported
  algorithm, policy upgrade required, cancellation, and resource rejection.
- Password contents MUST never appear in errors, logs, traces, metrics, or
  formatted values.
- APIs make unsafe configuration difficult and expensive work explicit.

## Core Model

- Immutable hashing policy with algorithm, version, parameters, salt length,
  output length, input limits, and resource budgets.
- `Hash`, `Verify`, `NeedsRehash`, and `VerifyAndUpgrade` operations.
- Typed verification result carrying match and upgrade state without exposing
  derived material.
- Context-aware operations with documented cancellation limitations of each
  primitive.
- Cryptographically secure salts from an injected, validated entropy source.
- Stable classified errors compatible with `errors.Is` and `errors.As`.
- Safe encoded-hash value whose string form may contain the hash but whose
  diagnostic formatting and observation behavior are explicitly controlled.

## Algorithm Support

### Argon2id

- Standards-compatible PHC string encoding and strict parser.
- Validated time, memory, parallelism, salt, output, and version parameters.
- Secure defaults selected from measured deployment budgets, not copied blindly.
- Rehash detection for weaker or obsolete parameters and versions.

### Bcrypt

- Compatibility with current Laravel/PHP bcrypt hashes and standard prefixes.
- Strict cost bounds and malformed-hash classification.
- Upgrade path from bcrypt to the configured Argon2id policy after successful
  authentication.

Additional algorithms MAY be adapters only when required for a demonstrated
migration and backed by maintained cryptographic implementations and vectors.

## Laravel Migration

- Verify representative Laravel bcrypt and Argon2id hashes independently.
- Document PHP-to-Go parameter and encoding correspondence.
- Support staged login-time rehash without changing user identity semantics.
- Provide safe database compare-and-swap examples so concurrent logins cannot
  overwrite a newer hash.
- Preserve existing hashes until successful verification and durable upgrade.
- Migration fixtures MUST contain synthetic credentials only.

## Denial-Of-Service And Resource Control

- Bound password bytes, encoded-hash bytes, parser fields, parameter values,
  memory, parallelism, concurrent hash operations, and verification queueing.
- Reject attacker-controlled hashes requesting excessive resources before
  invoking the primitive.
- Provide optional bounded worker/admission control without hidden goroutines.
- Document Kubernetes memory and CPU sizing from reproducible benchmarks.
- Avoid pre-hashing by default; if offered for extreme input handling, it MUST
  be versioned and explicitly incompatible with ordinary hashes.

## Side-Channel And Secret Safety

- Use constant-time comparison where the underlying primitive does not already
  define verification.
- Mismatch and malformed outcomes MUST not disclose password-derived data.
- Timing differences and user enumeration remain application concerns but must
  be documented with safe integration patterns.
- Caller-owned password byte slices MUST not be retained. Best-effort zeroing
  MAY be provided but MUST not claim guarantees the Go runtime cannot provide.
- No unsafe, cgo, custom assembly, or custom cryptographic implementation.

## Integration

- `authentication` adapters for application user lookup without creating a
  reverse dependency from authentication core.
- `service` lifecycle and admission hooks.
- Secret-safe `log` and bounded `telemetry` observations.
- `postgres` examples for optimistic hash upgrades, not ORM ownership.
- `passwordtest` deterministic entropy and compatibility fixtures strictly for
  tests; production constructors MUST require secure entropy.

## Non-Goals

- No user repository, registration, login endpoint, sessions, MFA, recovery,
  breach lookup, password-strength UI, authorization, tokens, or API keys.
- No encryption or reversible password storage.
- No home-grown algorithms or generalized cryptography facade.
- No global mutable policy or transparent background rehashing.
- No claim that memory can be reliably wiped under all Go runtime behavior.

## Package Shape

- Root: policy, hasher/verifier contracts, results, limits, errors, encoding.
- `argon2id` and `bcrypt`: native algorithm adapters.
- `passwordauth`: optional integration with `authentication`.
- `passwordtest`: synthetic vectors, deterministic entropy, and assertions.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Required evidence:

- official and independently generated algorithm/encoding vectors
- synthetic Laravel PHP interoperability fixtures
- malformed, truncated, duplicate-field, oversized, and parameter-bomb fuzzing
- mutation testing of match, mismatch, parser bounds, and rehash decisions
- race tests for shared immutable policies and bounded admission
- entropy failure, cancellation, allocation failure boundary, and concurrency
  tests
- benchmarks across approved Argon2id/bcrypt policies and deployment hardware
- timing-distribution analysis sufficient to catch obvious package regressions

## Documentation Deliverables

- Five-minute Argon2id quickstart and Laravel migration guide.
- Complete policy, parser, verification, rehash, error, and resource API docs.
- Guides for database upgrades, concurrency, Kubernetes sizing, algorithm
  selection, secret handling, testing, and authentication integration.
- Threat model, security policy, performance baselines, FAQ, troubleshooting,
  compatibility matrix, examples, and maintained changelog.
- Every exported API and user-facing scenario MUST be documented.

## Automation And Release

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, vulnerability scans, interoperability fixtures,
benchmarks, docs, API compatibility, and releases. Blocking commands MUST be
fully reproducible through documented local `make` targets.

## Execution Plan

1. Specify policy, encoded-hash grammar, errors, bounds, and result semantics.
2. Implement Argon2id and bcrypt adapters with official vectors.
3. Prove Laravel interoperability and safe rehash migration.
4. Add bounded admission, authentication integration, and observations.
5. Complete fuzz, mutation, side-channel, resource, and benchmark hardening.
6. Publish complete documentation and release v1.

## Acceptance Criteria

- Supported hashes interoperate with independent and Laravel implementations.
- Malformed or hostile hashes cannot trigger excessive resource use.
- Verification and upgrade behavior are explicit, race-safe, and secret-safe.
- The package contains no custom cryptographic primitive.
- Meaningful 100% coverage and every required CI gate pass.
