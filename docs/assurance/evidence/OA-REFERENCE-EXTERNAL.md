# OA-REFERENCE-EXTERNAL Evidence

Observed at `2026-08-13T23:31:44Z` on `darwin/arm64` with Go `1.26.6`.

## Scope

The maintained `pkg/service/integration/reference-external` module composes the
following public APIs without a private framework layer:

- `http-client` for finite outbound HTTP ownership and response cleanup;
- `rate-limit`, `adaptive-throttle`, `bulkhead`, `retry`, `circuit-breaker`,
  `concurrency-limit`, and `hedge` for distinct bounded control roles;
- `webhook` for exact-byte HMAC signing, receiving verification, idempotent
  delivery, endpoint policy, and bounded attempts;
- `filesystem` through the S3 adapter against a deterministic S3-compatible
  loopback boundary; and
- `secret-envelope` with a versioned keyring, authenticated context, encrypted
  persistence, and exact plaintext recovery.

## Executed Proof

- The canonical module check passed formatting, isolated tidy validation,
  safety, vet, tests, lint, static analysis, vulnerability scanning, secret
  scanning, license policy, SBOM generation, NilAway advisory analysis, and
  documentation checks.
- A separate race-enabled execution passed for the complete reference module.
- The behavioral tests proved a transient `503` is retried exactly once, all
  process-local controls settle, a healthy hedge wins and cancels blocked work,
  caller deadlines remain authoritative, signed webhook bytes verify at the
  receiver, encrypted bytes traverse the public S3 filesystem adapter, stored
  bytes do not contain plaintext, authenticated decryption restores the exact
  payload, and shutdown rejects new admission.
- Constructor validation rejects both nil and typed-nil borrowed interfaces
  before any owned resource is created.

## Claim Boundary

This evidence uses loopback HTTP servers, an S3-compatible protocol fixture,
an in-memory versioned keyring, and process-local policy stores. It does not
claim live AWS, Cloudflare R2, Infisical, FTP, SFTP, provider credential
rotation, container, ECS, multi-architecture, load, soak, chaos, deployment,
or production readiness. Those remain owned by later operational-assurance
scenarios.
