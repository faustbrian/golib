# Typed Bounded Pagination

Pagination is a typed lazy iterator over caller-owned vendor models. Core does
not decode envelopes into untyped maps or replace endpoint request and response
types.

```go
paginator, err := httpclient.NewCursorPaginator(
	httpclient.CursorPaginationOptions[Widget]{
		InitialCursor: initial,
		Fetch: func(
			ctx context.Context,
			cursor string,
		) (httpclient.CursorPaginationPage[Widget], error) {
			response, err := vendor.ListWidgets(ctx, cursor)
			if err != nil {
				return httpclient.CursorPaginationPage[Widget]{}, err
			}

			return httpclient.CursorPaginationPage[Widget]{
				Items:         response.Widgets,
				NextCursor:    response.NextCursor,
				HasNext:       response.HasNext,
				ResponseBytes: response.BodyBytes,
			}, nil
		},
	},
)
```

`NewPaginator` is the generic extension point for any typed continuation and
page callback. `NewPageNumberPaginator`, `NewOffsetPaginator`,
`NewCursorPaginator`, and `NewLinkPaginator` layer common continuation rules on
the same engine.

## Lazy item ownership

Construction performs no network or callback work. `Next` fetches only when no
buffered item remains. The returned item retains its caller-defined type;
pagination never serializes, reflects over, or copies item internals.

The iterator copies item slices so fetcher slice mutation cannot change its
buffer. Item values themselves follow normal Go copy semantics. Pointer, map,
slice, or mutable object fields remain caller-owned and should be treated as
immutable when the same state may be resumed elsewhere.

`Next` is safe for concurrent calls but serializes them. Sequential iteration
is the only sound default for cursors, Link relations, and unknown total-page
models. Independent known page ranges can be submitted to the bounded request
pool; preserve page keys and stable input-order results before flattening them.

## Finite budgets

Every paginator resolves finite zero-value defaults:

- 100 pages;
- 10,000 fetched items;
- five minutes elapsed time;
- 64 MiB of declared response bytes;
- three consecutive empty pages; and
- 4 KiB continuation keys.

Override them with `PaginationLimits`. A page that would exceed an item, byte,
elapsed, or continuation budget is rejected before any of its items are
exposed. The page callback must report the actual bounded bytes consumed by its
response decoder. Negative byte counts are invalid.

Elapsed time includes caller pauses between `Next` calls. Pagination checks the
context and elapsed budget before returning buffered items and before creating
new work. No timer or goroutine is owned by the sequential iterator.

`PaginationError` renders only a stable category. It unwraps
`ErrPaginationLimit`, `ErrPaginationCycle`, `ErrPaginationFetch`, or the
underlying callback failure without rendering cursor, item, response, or vendor
text.

## Resume state

`Paginator.State` returns an independent `PaginationState` containing:

- the next typed continuation and whether it exists;
- unconsumed buffered items and their index;
- page, item, byte, empty-page, and elapsed counters; and
- continuation cycle history.

Pass that state through the strategy's `Resume` field. Resuming midway through
a page yields the remaining buffered items before fetching again, so no item is
lost or duplicated. Limits are revalidated against the new paginator policy.
Malformed, over-budget, duplicated-history, and inconsistent terminal states
are rejected.

Resume state contains caller items and opaque continuation material. Protect it
as application data; do not log it or use it as an uncontrolled telemetry
attribute. Persistence format, encryption, expiry, and tenant scope belong to
the vendor application.

## Cycle and empty-page handling

The generic `PaginationContinuationKey` supplies deterministic cycle identity.
The current key is committed only after a page passes every budget. A transient
fetch error or rejected page therefore does not poison a later retry or a
resume with adjusted limits.

Repeated continuations fail before refetch. Consecutive empty pages are bounded
even when each supplies a different cursor, preventing a provider from driving
an infinite no-item loop. A non-empty page resets the empty-page streak.

For secret cursors, a custom paginator can return a bounded digest as the cycle
key while retaining the original cursor as its typed continuation.

## Built-in strategies

### Page number

Page numbers start at one by default and increment only when `HasNext` is true.
Non-positive and overflowing continuations fail deterministically.

### Offset and limit

`OffsetContinuation` keeps the fixed positive limit beside each non-negative
offset. The next offset advances by that limit, not by returned item count, so
short pages do not silently change provider semantics. Overflow is rejected.

### Opaque cursor

Cursor strings are passed byte-for-byte to the callback. Core performs no URL
escaping, decoding, Unicode normalization, or whitespace trimming. Extraction
from a typed envelope or response header belongs in the callback.

### RFC Link header

`NewLinkPaginator` starts from an absolute HTTP(S) URL, parses one `rel=next`
target, and resolves relative targets against the current page URL. It rejects
userinfo, malformed syntax, non-HTTP resolved targets, and multiple next
relations. Commas in URI references and quoted parameters are preserved.

`ParseNextLink` is available when a generated client already owns request
construction. Link targets still pass through the HTTP client's redirect,
egress, credential, and scope policies when fetched.

## Cancellation and callback contracts

Every fetch receives the current `Next` context. A canceled context creates no
page work. Fetchers should use `Client.Do` so retries, rate limits, circuit
breaking, authentication, and operation identity apply normally.

Fetchers must close or transfer every response according to the client response
ownership contract before returning a `PaginationPage`. Pagination owns only
typed items, continuation state, and declared byte accounting; it never owns an
`http.Response` body.
