# Errors and resource limits

Each package exposes a typed `Error` and stable `ErrorCode` values. Use
`errors.As` for package failures and `errors.Is` for wrapped cancellation or
source causes. Library error strings identify only the operation and code; they
never include generated passwords, phrases, mnemonics, entropy, passphrases,
seeds, alphabets, words, or arbitrary source diagnostics.

Random reads reject zero progress, invalid counts, source errors, cancellation,
oversized requests, and too many rejected samples. `Fill` clears its destination
on failure. Generation methods return no partial result. Caller-owned output
buffers are changed only after complete generation succeeds.

Password length, encoded alphabet and class bytes, alphabet symbol count,
required-class count, dynamic-programming cells, and dynamic-programming
operations are bounded before allocation-intensive validation. Word lists bound
list count and word bytes. Passphrases bound word count, list size, separator
size, and worst-case output bytes. BIP-39 bounds encoded mnemonic and
passphrase bytes and fixes seed derivation cost to the specification.

Cancellation depends on the injected `Source` honoring `ReadContext`; adapters
for hardware devices must return promptly when the context is canceled.

## Fixed limits

- selectors default to 128 rejected samples, are explicitly configurable, and
  allow at most 1,048,576 sampled bits;
- password policies allow 1,024 output symbols, 4,096 Unicode alphabet
  symbols, 16,384 encoded bytes per Unicode alphabet, exclusion, or class,
  256 byte-alphabet entries, 16 required classes, 1,048,576 dynamic cells,
  and 8,388,608 dynamic operations;
- validated lists allow 65,536 entries of at most 1,024 bytes each;
- passphrases allow 128 words from lists of at most 32,768 entries,
  64-byte separators, and 131,199 output bytes; and
- BIP-39 parsing allows 65,536 encoded mnemonic bytes and 1,048,576
  passphrase bytes, while derivation always uses the specified 2,048 rounds.
