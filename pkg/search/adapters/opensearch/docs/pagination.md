# Pagination and cursor ownership

Offset pagination is bounded and targets the resolved read alias directly. Use
it only for shallow, non-snapshot result windows.

Cursor pagination uses an OpenSearch point in time (PIT) plus `search_after`.
The final sort must be stable and end in `_id`. The signed cursor binds tenant,
logical index, normalized request fingerprint, physical mapping fingerprint,
PIT ID, sort values, page/item/byte totals, and expiry. A cursor from another
tenant, query, index generation, or signing key is rejected before search.
The first continuation fixes an absolute deadline bounded by
`Limits.MaxCursorDuration`; fetching later pages does not slide that deadline.

The first cursor request creates a PIT. A full page transfers PIT ownership to
the returned cursor. A short/empty page closes it. Any failure while the
adapter still owns the PIT triggers cleanup; final cleanup failures are returned
instead of hidden. Abandoned cursors live until their configured OpenSearch
keep-alive, so keep that duration short and cap concurrent cursors.

PIT expiry is an expected classified failure. Restart traversal from the first
page if policy permits; never splice a new PIT into an old cursor. Rotate cursor
signing keys with an explicit overlap or invalidation plan, and never log cursor
contents.
