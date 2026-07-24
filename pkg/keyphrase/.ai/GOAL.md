# Goal: Secure Keyphrase, Password, and Mnemonic Generation for Go

## Objective

Build `keyphrase` as an open-source cryptographic generation library for
random passwords, diceware-style passphrases, EFF word-list phrases, and BIP-39
mnemonics.

The package MUST provide secure, unbiased generation and validation without
becoming a password-hashing library, wallet implementation, secret store, key
derivation framework, or general-purpose random-data toolkit.

## Core Principles

- Cryptographic randomness MUST come from `crypto/rand` by default.
- Randomness sources MUST be injectable for deterministic testing and explicit
  hardware-backed integrations.
- Selection MUST use rejection sampling or another proven unbiased method.
- Entropy claims MUST be derived from the actual generator and constraints.
- APIs MUST make insecure or impossible policies fail explicitly.
- Generated secrets MUST never be included in errors, logs, traces, metrics, or
  debug representations by default.
- Go cannot guarantee complete memory erasure of strings or copied buffers; the
  API and documentation MUST state this limitation honestly.

## Required Capabilities

### Passwords

- length and character-alphabet policies;
- required character classes without biased post-generation shuffling;
- caller-defined Unicode or byte alphabets with duplicate detection;
- exclusion of ambiguous or unsafe characters;
- entropy estimation based on the effective constrained distribution; and
- generation into caller-owned byte buffers where practical.

### Passphrases

- EFF long and short word lists with pinned provenance and checksums;
- caller-provided validated word lists;
- configurable word count, separators, casing, and optional independently
  generated affixes;
- exact entropy reporting based on list size and policy; and
- parsing and policy validation without treating entropy estimates as password
  strength guarantees.

### BIP-39 Mnemonics

- complete official entropy sizes, checksum creation, parsing, and validation;
- every official BIP-39 language list;
- NFKD normalization and language detection with ambiguity reporting;
- seed derivation with optional passphrase exactly as specified; and
- official vectors and cross-implementation compatibility.

The package MUST NOT implement BIP-32/BIP-44 wallets, address generation,
private-key custody, or cryptocurrency transaction behavior.

## Word-List Governance

- Embedded lists MUST include source, version, license, expected length, and
  cryptographic checksum metadata.
- Builds and CI MUST verify lists have not changed unexpectedly.
- Duplicate, prefix-ambiguous where prohibited, malformed, or normalization-
  colliding entries MUST be rejected.
- Adding or changing a list is a security-sensitive compatibility event.

## Package Structure

Prefer focused packages such as:

- `keyphrase` for shared randomness and entropy contracts;
- `password` for constrained password generation;
- `passphrase` for word-list generation;
- `bip39` for mnemonic and seed behavior;
- `wordlist` for validated list loading and metadata; and
- `keyphrasetest` for deterministic and statistical test helpers.

Password hashing remains in `password`. Secret distribution remains outside
this package.

## Failure And Resource Semantics

- Short reads, source errors, stuck or malicious randomness providers, context
  cancellation, invalid alphabets, impossible constraints, and oversized
  requests MUST have typed errors.
- Lengths, list sizes, normalization work, parsing, and seed derivation MUST be
  bounded or explicitly configured.
- APIs MUST avoid partial secret return on failure unless clearly documented
  and represented by a dedicated type.

## Testing And Quality

- Meaningful 100% statement coverage is REQUIRED.
- Official BIP-39 vectors and independently sourced interoperability fixtures
  MUST pass.
- Property tests MUST cover policy satisfaction, round trips, checksum
  detection, and entropy calculations.
- Statistical tests MUST detect obvious selection bias without claiming to
  certify a random source.
- Fuzz tests MUST cover alphabets, word lists, mnemonic parsing, Unicode
  normalization, and malformed inputs.
- Race tests MUST cover shared generators and immutable word lists.
- Mutation tests MUST prove checksum, policy, normalization, and error-path
  assertions.
- Benchmarks MUST measure generation, normalization, parsing, seed derivation,
  allocations, and large-policy rejection.

## Documentation And Delivery

Documentation MUST include threat model, quick starts, complete API reference,
password and passphrase policy examples, entropy interpretation, BIP-39 usage,
word-list provenance, error handling, secret-lifetime caveats, migration advice,
adoption guide, FAQ, and explicit boundaries with hashing and wallet libraries.

CI MUST enforce formatting, vetting, strict linting, tests, race tests, fuzz
smoke tests, meaningful coverage, mutation, vulnerability/dependency/license
review, word-list integrity, examples, docs, and benchmarks. Every check MUST be
runnable locally.

## Completion Criteria

Completion requires unbiased generators, validated lists, complete BIP-39
support, typed failures, resource limits, interoperability evidence, security
documentation, CI, benchmarks, and meaningful 100% coverage. Wrapping
`crypto/rand` with a few convenience functions is not sufficient.
