# API reference

The Go package documentation is authoritative for signatures. This page maps
the complete public surface to its intended use.

## `keyphrase`

`Source` is the context-aware random-byte contract. `Selector` performs bounded
rejection sampling. `NewSelector`, `DefaultSelector`, and `WithMaxAttempts`
construct it. `Selector.Index`, `Selector.BigInt`, and `Selector.Fill` sample
uniform indices, arbitrary-precision ranks, and complete byte buffers.
`Secret` is a byte slice with redacted formatting, structured logging, and
standard text/JSON encoding plus best-effort `Clear`. `Error`, `ErrorCode`, and
the `Code*` constants provide typed failures whose formatting and standard
encoding omit wrapped diagnostics.

## `password`

`Policy` and `Class` describe Unicode code-point alphabets, exclusions,
required classes, length, and minimum entropy. `BytePolicy` and `ByteClass`
provide the equivalent contract for all 256 byte values. `Analyze` and
`AnalyzeBytes` return the exact `Distribution.Outcomes` and derived bit count.
`Validate` checks an existing Unicode password. `Generator`, `NewGenerator`,
and `DefaultGenerator` create generators. `Generate`, `GenerateInto`,
`GenerateBytes`, and `GenerateBytesInto` return only complete results.

## `wordlist` and `wordlist/eff`

`Metadata` pins identity, language, source, version, license, length, embedded
checksum, and source checksum. `New` validates caller lists. `WithNFKD` and
`WithUniquePrefix` add format-specific invariants. Immutable `List` values
provide `Len`, `Word`, `Index`, `Words`, and `Metadata`. `Checksum` uses the
newline-terminated UTF-8 representation. `eff.Large`, `eff.ShortOne`, and
`eff.ShortTwo` load the pinned embedded lists.

## `passphrase`

`Policy` selects a validated list, word count, separator, casing, optional
independent `Affix` policies, and minimum entropy. `Analyze`, `Generator`,
`NewGenerator`, `DefaultGenerator`, `Generate`, and `Parse` provide generation
and validation. `Parsed` owns affix byte buffers; its word strings remain
ordinary immutable Go strings. Default formatting and structured logging of a
`Parsed` value are redacted, as is standard text/JSON encoding.

## `bip39`

`Language` and its ten constants identify official lists. `Languages` and
`List` expose their validated metadata. `FromEntropy` and `Generate` create
`Mnemonic` values. `Parse` detects languages and reports ambiguity;
`ParseLanguage` uses an explicit list. `Mnemonic.String`, `Words`, `Language`,
and `Entropy` return canonical or caller-owned representations. `Seed` performs
the specified 2,048-round PBKDF2-HMAC-SHA512 derivation. Default formatting,
structured logging, and standard text/JSON encoding of `Mnemonic` are redacted;
`String` is the explicit plaintext boundary.

## `keyphrasetest`

`NewSource` creates a finite deterministic source. `NewCounterSource` exercises
sampling boundaries. `ChiSquared` calculates an equal-frequency diagnostic;
it must not be presented as cryptographic certification.
