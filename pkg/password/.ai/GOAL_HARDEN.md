# Hardening Goal: Secure Password Hashing

## Objective

Prove that `password` preserves correct verification, strict encoding,
resource bounds, interoperability, and secret safety under hostile hashes,
concurrent authentication, migration races, entropy failure, and operational
misconfiguration.

## Required Audits

### Cryptographic And Interoperability Audit

- Validate Argon2id and bcrypt against official and independent vectors.
- Generate synthetic hashes in PHP/Laravel and verify them in Go and vice versa
  where the encoding is shared.
- Exhaust algorithm versions, prefixes, parameters, salts, outputs, and rehash
  transitions.
- Obtain specialist cryptographic review before v1.

### Parser And Resource Audit

- Fuzz malformed separators, duplicate fields, numeric overflow, invalid base64,
  unsupported versions, truncation, trailing data, and oversized encodings.
- Reject excessive memory, time, cost, parallelism, salt, output, and input
  parameters before expensive work.
- Stress bounded concurrent operations under CPU and memory pressure.
- Benchmark approved policies on representative Kubernetes limits.

### Secret And Side-Channel Audit

- Inspect every error, formatter, log, trace, metric, panic, and test failure for
  password, salt, derived-key, or encoded-hash leakage.
- Test mismatch and malformed paths for obvious timing regressions while
  documenting limits of statistical guarantees.
- Verify caller buffers are not retained or mutated unexpectedly.
- Prove entropy errors fail closed and deterministic entropy cannot enter
  production constructors.

### Migration And Concurrency Audit

- Race-test concurrent verify-and-upgrade operations and optimistic writes.
- Exercise crashes before verification, after verification, during hashing, and
  before/after durable compare-and-swap.
- Prove failed upgrades never invalidate the existing usable hash.
- Mutation-test every branch affecting match and rehash decisions.

## Required Deliverables

- Cryptographic review and interoperability matrix.
- Threat model, parser grammar, approved parameter table, and resource budgets.
- Fuzz, mutation, race, timing, migration, and benchmark evidence.
- Synthetic PHP compatibility corpus with no real credentials.
- Updated API, migration, security, operations, FAQ, and troubleshooting docs.

## Release Blockers

- Any incorrect match, false mismatch, downgrade, parser ambiguity, parameter
  bomb, secret leak, insecure entropy, race, panic, or custom cryptography.
- Any migration path capable of destroying a valid hash before durable upgrade.
- Any unsupported claim of guaranteed memory erasure or side-channel immunity.
- Missing meaningful 100% coverage, specialist review, or green blocking CI.

## Completion Criteria

- Vector, PHP interoperability, parser, migration, and resource suites pass.
- Approved policies have measured deployment budgets.
- Race, fuzz, mutation, vulnerability, and compatibility gates pass.
- NilAway runs visibly as advisory without blocking findings.
- No release blocker remains and the changelog is current.
