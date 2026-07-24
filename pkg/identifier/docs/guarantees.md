# Guarantees and leakage

## UUID

UUID parsing accepts canonical lowercase RFC text, RFC variant values, and
versions 1 through 8. This does not claim equal semantics for each version.
Generation supports v4 and v7. V4 provides 122 random bits after version and
variant bits. V7 exposes Unix milliseconds and is strictly increasing within
one generator until its 74-bit random field overflows. Clock rollback is an
error; it is never silently clamped.

## ULID

ULID preserves the canonical uppercase 26-character representation and the
full 80-bit entropy field. A generator starts a random sequence each
millisecond and increments it for subsequent values. It rejects clock rollback
and entropy overflow. Time and same-generator issuance order are visible.

## TypeID

TypeID follows specification 0.3.0. Parsing accepts every official 128-bit
suffix vector, including values that are not UUIDv7, for interoperability.
Generation always uses UUIDv7. Prefixes are at most 63 lowercase ASCII letters
or underscores, start and end with a letter, and may contain consecutive
underscores. Ordering is prefix-first and then suffix order.

## KSUID

KSUID uses Segment's 20-byte representation and 27-character Base62 codec.
Timestamps have one-second granularity. This package increments the first
random payload within a second; that makes one generator strictly ordered but
reveals local issuance order. The random starting payload remains 128 bits.

## NanoID

NanoID has no time or generation-order guarantee. Configurations use 2 through
255 unique printable ASCII bytes and must provide at least 120 ideal entropy
bits. Rejection sampling prevents modulo bias. Collision probability still
depends on total generated values; 120 bits is a floor, not a proof that a
collision cannot happen.

All shared generators use a mutex. Distinct generator instances have distinct
monotonic domains and can interleave when sorted. No generator coordinates
across processes.
