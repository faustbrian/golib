# Error-correction tradeoffs

QR exposes L, M, Q, and H. More correction increases redundant codewords and
usually increases the version for the same payload. Use the lowest level that
meets the damage and scanning environment; increasing correction does not
repair poor quiet zones, distorted modules, blur, or insufficient print size.

Aztec accepts a minimum correction percentage and optionally a compact/full
layer count. Data Matrix ECC 200 always uses its defined Reed-Solomon scheme.
PDF417 exposes levels 0 through 8; higher levels add more correction codewords
and reduce data capacity.

Correction metadata does not imply a confidence probability. A decoder result
reports checksum/correction diagnostics when the underlying format supplies
them. Callers should treat a successful decode as validated content, not as
proof that differently transformed image input will remain decodable.
