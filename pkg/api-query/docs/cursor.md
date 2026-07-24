# Cursor protocol and rotation

Cursor pagination requires an exact total order. Declare one stable unique
tie-breaker and fixed null ordering. The compiler appends the tie-breaker when a
request omits it. Storage reads fetch `page size + 1` rows to determine the next
boundary and return items in canonical order even for backward traversal.

`cursor.Payload` binds schema revision, traversal direction, exact ordered sort
terms, one typed position per term, expiration, and an optional policy identity.
`NullValue` represents a null position. The encrypted wire payload additionally
binds the cursor protocol version and key ID as authenticated data.

Pass the `*cursor.Codec` as `CompileOptions.CursorDecoder`. When `after` or
`before` is present, compilation authenticates and decodes it against the final
schema revision and total sort order. Failures become a sanitized
`cursor_failure`; successful plans erase the raw token and retain bounded typed
`Plan.Cursor` state. Canonical output hashes that state so randomized encryption
nonces do not break cache equivalence or expose positions.

Use a random 32-byte secret per key. Rotate atomically:

1. Activate the new key and retain the old key for decoding.
2. Issue only new-key cursors for at least the maximum TTL.
3. Retire the old key after every old cursor has expired.

Key IDs are public routing metadata and must not contain secrets. Changing the
schema, sort, or cursor version causes a stable rejection. Use `ReplayGuard`
only when one-time consumption is required; it receives a token fingerprint,
not plaintext, and must expire retained state.

For forward reads, compare lexicographically *after* the decoded positions. For
backward reads, invert comparisons and database directions, fetch one extra,
then reverse results back to canonical response order. Mixed directions and
null ordering require explicit application-owned seek variants. Never splice a
decoded position into SQL.

`BuildPage` snapshots items and emits a backward cursor from the first item only
when a previous page exists, and a forward cursor from the last item only when a
next page exists. Empty, first, and last pages have explicit booleans and omit
unavailable cursors.

See the consistency limits in [the security model](../SECURITY.md).
