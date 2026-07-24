# BIP-39 behavior

`bip39` supports 128, 160, 192, 224, and 256 bits of entropy and the resulting
12, 15, 18, 21, and 24-word mnemonics. It creates and validates checksum bits,
normalizes mnemonic and passphrase text with NFKD, uses all ten official lists,
and derives a 64-byte seed with PBKDF2-HMAC-SHA512, 2,048 rounds, and salt
`"mnemonic" + passphrase` exactly as specified.

`Parse` checks vocabulary across every official list before checksum
validation. If all words occur in multiple lists, it returns
`CodeAmbiguousLanguage` and safe language candidates instead of guessing.
`ParseLanguage` is appropriate when the surrounding protocol already supplies
the language. Whitespace is normalized for parsing; `Mnemonic.String` renders
Japanese vectors with ideographic spaces and other languages with ASCII spaces.

The tests consume the complete vector set pinned from Trezor's
`python-mnemonic` repository and compare English mnemonic and seed output with
the independently maintained `tyler-smith/go-bip39` implementation.

BIP-39 is included only for mnemonic and seed interoperability. This module
does not implement BIP-32, BIP-44, wallets, addresses, private keys,
transactions, custody, account discovery, or chain-specific behavior.
