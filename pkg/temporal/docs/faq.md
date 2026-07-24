# FAQ

## Why is `[a,a)` empty but `[a,a]` a singleton?

Endpoint membership defines the represented set. Equal endpoints do not erase
bounds.

## Why does `Duration()` ignore endpoint inclusion?

Elapsed measure is determined by endpoint distance. A singleton has zero
elapsed duration even though it has one represented instant.

## Why reject ISO months in fixed durations?

A month has no fixed elapsed length without a reference date and arithmetic
policy. Use `calendar` for calendar movement.

## Is `24:00` midnight?

It denotes the end boundary of the current local day. It does not silently
equal the next day's `00:00` in all contexts.

## Where is Gantt chart rendering?

Deferred from v1. The compatibility matrix inventories it, but core makes no
claim of full PHP compatibility while charting is absent.
