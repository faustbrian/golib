# Compatibility

The module requires Go 1.26.5. Public compatibility follows semantic
versioning. Canonical text and binary encodings are persistence contracts.

UUID behavior follows RFC 9562 canonical text and RFC variant/version bits.
ULID follows the canonical ULID Crockford encoding. TypeID is pinned to
specification 0.3.0 at revision
`be8ff0daf5dc1f6d40c62a03cfc89945263a69af`. The complete official valid and
invalid JSON corpora are vendored byte-for-byte, hash-checked by the provenance
gate, and executed by `TestOfficialValidCorpus` and
`TestOfficialInvalidCorpus`. KSUID is differentially tested against
`github.com/segmentio/ksuid`. NanoID uses the reference URL alphabet but adds
an explicit 120-bit minimum that intentionally rejects weak custom
configurations accepted by permissive libraries.

TypeID parsing and byte construction accept all 128-bit UUID payloads in the
official corpus. `FromUUID` accepts canonical UUID text for that complete
domain and also accepts this module's validated `uuid.ID`. The unprefixed,
all-zero TypeID is the Go zero value and serializes as the canonical 26-zero
suffix, matching the official Go implementation.

`slug.LaravelEnglish` is pinned to Laravel 13.18.0 `Str::slug` at revision
`138e5806ed7e4e21591948b661119d3810f052cf`, the default English profile of
spatie/laravel-sluggable 4.0.2 at revision
`82a69be1ef661ce2ff38242b271457ef0b9611dd`, and voku/portable-ascii 2.1.1 at
revision `8e1051fe39379367aecf014f41744ce7539a856f`. It truncates the source to
250 Unicode code points before replacement and may return an empty slug.
Uniqueness and numeric suffixes are not part of the package contract.

Changing case acceptance, null handling, byte order, timestamp extraction,
prefix grammar, minimum entropy, rollback behavior, monotonic behavior, or
slug replacement output is a compatibility change even if method signatures
stay stable.

Snowflake is not part of this module. Adding it requires a separately reviewed
deployment contract and must not silently broaden the existing guarantees.
