# Pagination

Cursor pagination is first class. A cursor is authenticated, expiring, bound to
tenant, logical index, index-definition fingerprint, normalized query fingerprint,
stable sort, page limits, point-in-time ID, and search-after values. Tampering,
expiry, changed query, changed index definition, or limit escalation fails locally.

Every cursor request needs a deterministic total order. Add the unique stable
`DocumentIDSortField` tie-breaker; adapters translate that virtual field to
backend metadata. The point-in-time handle freezes the searchable view for its
keep-alive; cursors fail when that handle expires. Applications must bound page
size, total pages, total items, bytes, and elapsed time. The initial cursor fixes
one absolute deadline no later than `Limits.MaxCursorDuration`; later pages
cannot extend it. Applications must not silently restart a traversal after
changed-index or expired-PIT failure.
Cursor encoding preflights binding, PIT, sort-value count, and raw sort bytes
against the codec token bound before copying or JSON encoding continuation
state.

Offset pagination is available only for explicitly bounded shallow navigation.
It has increasing backend cost and may duplicate or skip results while the
index changes.
