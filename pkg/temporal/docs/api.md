# API reference

## Root

- `Bounds`, typed `Side`, inclusion/exclusion/replacement helpers, text
  encoding.
- `Relation`, `AllRelations`, `Converse`, and canonical names.
- Sentinel errors and `LimitError`, usable with `errors.Is`/`errors.As`.
- `Limits`, `DefaultLimits`, `Resolve`, and `Validate`.

## instant

`New`, `Range`, `Point`, `After`, `Before`, and `Around` construct periods.
Period methods cover membership, formal and set predicates, checked duration
comparison, endpoint/bound changes, resizing, movement, expansion, equality,
intersection, exact union/difference, convex-hull merge, and gap. Forward and
backward split methods require positive fixed steps. `Snap` and `SnapOutward`
require an explicit location and `calendar` DST policy.

`Set` is normalized, immutable, and stably ordered. It provides length, copied
periods, equality, span, total duration, containment, gaps, union,
intersection, subtraction, iteration, search, transform, and generic `Reduce`.
Normalization is `O(n log n)`; binary search and membership are `O(log n)`;
two-set intersection is `O(n+m)`.

## dateperiod

Factories cover day, ISO week, month, quarter, semester, year, and ISO year.
Periods expose discrete days, membership, policy-driven calendar movement,
immutable replacement/expansion, relations and set predicates, exact
union/difference, hull merge, gaps, fixed-day splitting, and explicit instant
conversion.
`Set` mirrors the instant set where discrete semantics permit it.

## timeofday

`Time` supports strict construction/parsing, offsets, precision metadata,
comparison, clamp, rounding, explicit wrapping shifts, signed and circular
differences, and `24:00`. Applying it to a date or deriving it from an instant
requires explicit calendar context.
`Duration` wraps `time.Duration` for checked sum, negate, absolute, multiply,
divide/remainder, clamp, and rounding.

`Interval` distinguishes ordinary, circular, collapsed, and full-day kinds. It
supports membership, set predicates, immutable endpoint/bound replacement,
shift/expand, civil-date conversion, intersection, union, difference, gap,
bounded split, and steps. `IntervalSet` is a normalized linear daily
representation with duration, stable iteration/search, union, intersection,
subtraction, gaps, and explicit full-day complement.

## Adapters

`notation` provides strict instant/date/daily and fixed-duration codecs.
`postgres` provides pgx ranges/multiranges and nullable `database/sql` values.
`temporalwire.Document` and `CollectionDocument` are the `temporal/v1` stable
scalar and normalized-set wire envelopes.
`temporalconfig` supplies atomic text wrappers. `temporalvalidation` supplies
deterministic validators. `temporaltest` supplies exhaustive fixtures and
assertions.

The Go package documentation is the signature-level source of truth:

```sh
go doc -all github.com/faustbrian/golib/pkg/temporal/instant
```
