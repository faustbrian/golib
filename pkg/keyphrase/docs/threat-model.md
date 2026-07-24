# Threat model

## Protected properties

The library protects uniform selection from the configured output space,
accurate outcome counts, early rejection of impossible policies, BIP-39
checksum and derivation compatibility, list integrity, bounded hostile input,
and omission of generated values from library errors.

The default random source is `crypto/rand`. Injected sources are trusted to
provide cryptographic bytes, obey cancellation, and be concurrency-safe when
shared. Rejection limits prevent a malicious source from forcing unbounded
sampling. A deterministic or biased injected source intentionally weakens the
result and is suitable only for tests or explicitly reviewed hardware adapters.

## In scope

- modulo and policy-selection bias;
- invalid, duplicate, normalization-colliding, or unexpectedly changed lists;
- malformed Unicode and ambiguous BIP-39 language detection;
- short reads, source failures, cancellation, and repeated rejected samples;
- oversized alphabets, lists, phrases, dynamic-programming state, and
  passphrases;
- partial-result and error-message disclosure; and
- concurrent use of immutable lists and generators.

## Out of scope

The module does not defend a compromised process, operating system, compiler,
hardware random generator, terminal, clipboard, crash reporter, or caller log.
It does not assess whether a user-selected mnemonic or password has real-world
strength. It does not hash passwords, rotate credentials, store secrets,
implement wallets, derive BIP-32/BIP-44 keys, or protect cryptocurrency assets.

## Residual risks

Go cannot guarantee erasure of strings or copied buffers. Entropy estimates
describe this generator's mathematical distribution, not guessing resistance
after disclosure, reuse, human modification, or downstream normalization.
Statistical tests detect obvious regressions but do not certify a random source.
Stable release requires independent cryptographic design review.
