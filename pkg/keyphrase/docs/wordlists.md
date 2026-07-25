# Word-list provenance

Adding or changing an embedded list is a security-sensitive compatibility
event. Review source ownership, license, normalization, entry count, duplicate
and prefix behavior, raw-source checksum, transformed checksum, vectors, and
release notes. Never silently replace a list under an existing identifier.

## EFF lists

The embedded EFF lists are the 7,776-word long list and both 1,296-word short
lists published in 2016. Source rows contain dice indices; the embedded files
contain only the word column. `Metadata.SourceSHA256` pins the downloaded source
and `Metadata.SHA256` pins the newline-terminated transformed words. They are
licensed CC BY 3.0 US. Exact URLs and checksums live in `wordlist/eff/eff.go`.

## BIP-39 lists

All ten official files are pinned to Bitcoin BIPs revision
`8c369ac8e60629ac6c032ffe21bb5ec5b35213d7`. Each has 2,048 NFKD entries, a
unique four-character NFC display prefix, MIT metadata, and a SHA-256 checksum.
Exact values live in `bip39/lists.go`.

## Verification

`make wordlists` loads every embedded list through the same validation used at
runtime. `make test` and `make coverage` repeat the check. Source refreshes must
be explicit review changes; builds do not fetch mutable network content.
