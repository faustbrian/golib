# FAQ

## Is entropy a strength guarantee?

No. It is the exact mathematical distribution produced by the configured
generator. Operational controls and attacker knowledge determine practical
resistance.

## Does statistical testing prove `crypto/rand` is secure?

No. It catches obvious selection regressions. Security relies on Go's
`crypto/rand`, the operating system, and reviewed rejection sampling.

## Can generated secrets be erased completely?

No. Byte slices can be cleared as a best-effort measure, but Go and the
operating system may retain copies.

## Does this hash passwords?

No. Use `password` or another maintained Argon2id, bcrypt, or scrypt
implementation for storage verification.

## Does BIP-39 support wallets or addresses?

No. It ends at the specified seed bytes and intentionally excludes BIP-32,
BIP-44, custody, addresses, transactions, and chain behavior.

## Why can language detection fail as ambiguous?

Official lists share some vocabulary, especially the Chinese lists. Guessing
could silently choose the wrong compatibility domain, so the API reports safe
candidate language names.

## May I inject a deterministic source in production?

Only if it is an explicitly reviewed cryptographic hardware integration.
`keyphrasetest` sources are predictable and must never generate real secrets.
