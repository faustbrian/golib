# Goal: Harden keyphrase for Production

## Objective

Prove that generation is cryptographically unbiased, specification compliant,
bounded, and resistant to accidental secret disclosure under failure,
concurrency, Unicode edge cases, and hostile inputs.

## Randomness And Distribution

- Audit every random selection for modulo bias and constrained-policy bias.
- Test rejection-sampling boundaries, short reads, repeated bytes, failing
  readers, blocked readers with cancellation, and deterministic readers.
- Property-test that generated outputs satisfy policy without deterministic
  character placement.
- Run statistically justified distribution checks with documented false-positive
  thresholds; do not use them as proof of cryptographic security.
- Verify exact entropy calculations for all supported policies and reject
  misleading estimates where distributions cannot be calculated exactly.

## Password And Passphrase Safety

- Test empty, duplicate, huge, Unicode, normalization-colliding, and impossible
  alphabets and word lists.
- Verify minimum/maximum lengths, required classes, separators, casing, affixes,
  and caller-provided lists.
- Ensure list metadata, licenses, lengths, and checksums are verified in CI.
- Test compatibility implications of list changes and reject silent replacement.

## BIP-39 Compliance

- Run all official vectors for every supported language.
- Differential-test mnemonic parsing, checksum validation, normalization, and
  seed derivation against independent mature implementations.
- Test every valid entropy length, checksum-bit boundary, invalid word,
  ambiguous language, mixed language, whitespace form, and normalization edge.
- Confirm exact PBKDF2 parameters and passphrase handling from independent test
  vectors.

## Secret Handling

- Verify errors, formatting, logs, traces, metrics, panic recovery, and test
  diagnostics do not disclose generated secrets.
- Audit buffer ownership, copying, aliasing, and best-effort zeroing behavior.
- Document that immutable strings, compiler copies, runtime copies, crash dumps,
  and swap prevent guaranteed erasure in Go.
- Ensure partial results are not returned after randomness or derivation errors.

## Hostile Input And Concurrency

- Fuzz all alphabet, word-list, mnemonic, Unicode, and import parsers.
- Bound request sizes, list counts, word lengths, normalization, derivation
  costs, and retries against malicious randomness sources.
- Race-test shared immutable lists and concurrent generators.
- Verify no goroutine leaks or unbounded waits after cancellation.

## Verification Gates

- Meaningful 100% statement coverage.
- Passing official vectors, differential, property, statistical, fuzz, race,
  and mutation suites.
- Stable benchmarks for each generator, mnemonic validation, normalization,
  seed derivation, allocations, and failure paths.
- Static analysis, vulnerability scanning, dependency review, license checks,
  and reproducible embedded-list integrity checks.
- Independent cryptographic design review before a stable release.

## Release Blockers

Release MUST be blocked by biased selection, incorrect entropy claims, BIP-39
vector failures, word-list provenance gaps, secret disclosure, unbounded hostile
input, incomplete cancellation, race findings, misleading memory-erasure claims,
or meaningful coverage below 100%.
