# Encoded-hash grammar

The parser consumes at most `Limits.EncodedHashBytes` and rejects hostile
parameters before invoking a primitive.

## Argon2id

Canonical grammar, with no whitespace or trailing data:

```text
argon2id = "$argon2id$v=19$m=" uint ",t=" uint ",p=" uint "$" b64 "$" b64
uint     = "0" / (digit1-9 *digit)
b64      = unpadded canonical RFC 4648 base64
```

Parameter order is fixed: `m`, `t`, `p`. Duplicate, missing, reordered, signed,
zero-padded, overflowing, or non-decimal fields are rejected. Only Argon2
version 19 is supported. Memory must be at least eight kibibytes per lane.
Memory, time, lanes, decoded salt, and decoded output must also fit the supplied
limits. Encoded salt/output lengths are bounded before base64 decoding. Salts
must be at least 8 bytes; outputs at least 16 bytes. Invalid base64 or short
values are malformed; values above configured ceilings are resource-rejected.

## Bcrypt

Accepted prefixes are `$2a$`, `$2b$`, and Laravel/PHP `$2y$`. Encodings must be
exactly 60 bytes, contain a two-digit cost from 04 through the configured
maximum, and use the bcrypt `./A-Za-z0-9` alphabet for the 53-byte body. Other
prefixes are unsupported; malformed length/alphabet is malformed; excessive
cost is a resource rejection.

## Classification ordering

1. Empty syntax is malformed.
2. Oversized encoding is resource-rejected without splitting fields.
3. Known algorithm syntax is parsed and bounded.
4. Unknown `$...` algorithms are unsupported.
5. Other input is malformed.

The fuzz corpus covers truncation, extra separators, duplicate fields, numeric
overflow, invalid base64, unsupported versions, trailing data, and parameter
bombs. Successful parsing round-trips exactly through `EncodedHash.String()`.
