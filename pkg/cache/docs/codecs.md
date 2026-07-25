# Codecs and schema evolution

The built-in `JSONCodec[V]` uses a one-byte nonzero version followed by strict
JSON. The configured encoded-size limit is checked on encode and before decode.
Unknown fields, trailing values, malformed data, and version mismatch fail
explicitly.

Go map keys supported by `encoding/json` are sorted, so equivalent map values
produce deterministic bytes. A nil pointer encodes as JSON `null` and remains a
valid hit. Empty raw bytes are a schema mismatch, not a nil value or tombstone.

Choose a codec version for the exact Go value schema. When a deployed reader
cannot safely decode old bytes, increment the codec version and normally also
increment the key-space version. The key version prevents repeatedly reading
known-incompatible data; the codec version remains a corruption and rollout
safety check.

Custom codecs should:

- deterministically encode equivalent values;
- preserve zero values without a sentinel conversion;
- carry an explicit schema/version marker;
- reject oversized input before allocating from untrusted lengths;
- reject trailing or unknown data unless the schema explicitly permits it;
- return errors matching `ErrDecode`, `ErrSchemaMismatch`, or
  `ErrValueTooLarge` as appropriate;
- never silently coerce or truncate values.

Treat backend bytes as untrusted even when the backend is private. Mixed
deployments, manual writes, and stale records can all produce incompatible
payloads.

The version byte identifies an application schema, not a Go type at runtime.
Never reuse one key space and codec version for different semantic types even
if their current JSON shapes happen to decode successfully. The built-in codec
does not compress, so decompression bombs do not apply; allocation and parsing
remain bounded by the encoded-size limit.
