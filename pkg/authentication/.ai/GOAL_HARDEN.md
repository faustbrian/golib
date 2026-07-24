# Hardening Goal: Application Authentication

## Objective

Perform an evidence-driven security, protocol, concurrency, interoperability,
and resource-safety audit of `authentication`, then close every justified gap
before a
stable release.

## Required Audits

### Credential And HTTP Audit

- Fuzz missing, empty, duplicate, conflicting, malformed, oversized, Unicode,
  and whitespace-variant credential headers.
- Verify Basic decoding, bearer grammar, API-key placement, proxy behavior,
  header canonicalization, and challenge escaping.
- Prove deterministic composition when credentials are absent, invalid,
  unavailable, or rejected by multiple authenticators.
- Verify no secret reaches URLs, logs, telemetry, errors, panic output, or test
  failure messages.

### Secret And Rotation Audit

- Prove constant-time static-secret comparisons and bounded candidate sets.
- Test active, previous, revoked, unknown, duplicated, and rotated key IDs.
- Exercise concurrent key updates and authentication under the race detector.
- Define cache invalidation and stale-key behavior precisely.

### JWT, JWK, And OIDC Audit

- Reject `none`, algorithm confusion, key-type mismatch, duplicate claims,
  malformed numeric dates, missing issuer/audience, and unexpected critical
  headers.
- Test clock skew, expiration, not-before, issued-at, audience arrays, key ID
  collisions, stale JWKS, rotation races, HTTP cache semantics, and issuer loss.
- Bound token size, claim count, nesting, JWK count, refresh rate, waiters, and
  response bodies.
- Validate against authoritative interoperability vectors and supported upstream
  versions.

### Lifecycle And Boundary Audit

- Verify cancellation, deadlines, retry limits, cache ownership, shutdown, and
  goroutine cleanup for all network-backed authenticators.
- Prove principal immutability and context isolation.
- Ensure no authorization behavior is accidentally introduced.
- Verify `service`, `http-client`, and `authorization` dependency
  directions remain acyclic.

## Required Deliverables

- Authentication threat model and credential-flow diagrams.
- Findings report with severity, evidence, reproduction, and disposition.
- Protocol, interoperability, failure-injection, race, fuzz, and benchmark
  matrices.
- Security vectors for JWT/JWK/OIDC and static-key rotation.
- Updated API, adoption, operations, security, compatibility, FAQ, and
  `CHANGELOG.md` documentation.

## Release Blockers

- Authentication bypass, ambiguous credential precedence, algorithm confusion,
  secret disclosure, unsafe anonymous fallback, or mutable principal state.
- Any race, deadlock, unbounded cache/retry/input, leaked goroutine, or request
  that ignores cancellation.
- Any undocumented trust decision or standards divergence.
- Missing meaningful 100% coverage or a failing GitHub Actions gate.

## Completion Criteria

- Every supported authentication flow has adversarial and interoperability
  evidence.
- Race, fuzz, vulnerability, compatibility, and performance gates pass.
- All high and medium findings are fixed or rejected with documented proof.
- No release blocker remains and `CHANGELOG.md` is current.
