# Core semantic specification

This document fixes the mathematical and resource contracts before production
implementation begins.

## Domain and bounds

Core intervals are finite and bounded. Unbounded PostgreSQL ranges may be read
only through an explicit optional representation; they cannot silently become a
core period. Constructors reject reversed endpoints.

The four bounds are closed `[a,b]`, open `(a,b)`, start-closed/end-open `[a,b)`,
and start-open/end-closed `(a,b]`. The operational default is `[a,b)`.

For equal endpoints:

- `[a,a]` is a singleton;
- all other bound modes are empty;
- empty is a valid set result but must be explicitly constructed or returned;
- a daily collapsed interval is an explicit empty kind and a full-day interval
  is an explicit universe kind. Neither is inferred from equal endpoints.

Changing bounds returns a new value and may change a singleton into empty. Set
equality compares represented members; structural equality also compares bound
mode and explicit kind.

## Ordering and Allen relations

For non-empty intervals, the primary endpoint relations are the thirteen Allen
relations: before, meets, overlaps, starts, during, finishes, equals, and the
six converses finished-by, contains, started-by, overlapped-by, met-by, after.
Endpoint ordering chooses the Allen relation. Boundary inclusion additionally
determines whether equal endpoints share a member:

- `meets` means ordered endpoints are equal and the intersection is empty;
- `borders` means ordered endpoints are equal and both intervals include it;
- `abuts` means either `meets` or `borders`;
- `overlaps` means the set intersection is non-empty and neither set contains
  the other;
- singleton and empty cases are classified by explicit set predicates rather
  than forced into an invalid Allen relation.

Every relation has one converse, equality is self-converse, and exactly one
primary Allen endpoint relation applies to each pair of non-empty intervals.
Singleton membership is additionally classified by set predicates. The
executable truth table covers all 16 bound pairs for every endpoint ordering.

## Set operations

Intersection returns the exact common members. Union returns a normalized set;
it merges intervals when their union has no excluded point between them.
Difference returns exact fragments with boundary sides inverted at cuts.
Complement is difference from an explicit universe. Hull returns the smallest
interval enclosing inputs and is never named union.

Normalized sets are stably ordered by start, start inclusion, end, then end
inclusion. They contain no empty members, duplicates, overlaps, or mergeable
adjacencies. Normalization and complement are idempotent in their applicable
domains; union and intersection are commutative; difference is not.

## Time policies

Instant periods compare `time.Time` values by instant. Constructors and codecs
strip process-local monotonic readings and preserve the numeric UTC offset plus
the caller's chosen location policy. Serialized equality cannot depend on a
monotonic reading.

Local times are date-independent nanosecond offsets. `24:00` is an end boundary,
not ordinary midnight. Applying local time or snapping an instant requires a
calendar adapter, location, and explicit nonexistent/ambiguous-time policy.

`time.Duration` represents fixed elapsed nanoseconds. A fixed day is 24 hours
and a fixed week is 7 fixed days. Months, years, and civil days are calendar
operations and require `calendar` plus a reference date.

## Default resource limits

All limits are configurable downward per operation but never disabled by a zero
value. Implementations must check limits before allocation or append.

| Resource | Default hard limit |
|---|---:|
| input text | 64 KiB |
| fractional precision | 9 digits |
| error text | 1 KiB |
| formatted output | 64 KiB |
| periods accepted by one set operation | 100,000 |
| periods emitted by one set operation | 100,000 |
| split or iteration steps | 1,000,000 |
| parser nesting | 8 |

Zero or negative steps fail before iteration. Arithmetic is checked before a
value changes or memory is allocated. Failure returns a typed sentinel-compatible
error and no partial result.

## Complexity targets

Pairwise relation and period operations are O(1). Normalization is O(n log n)
time and O(n) space. Operations over two normalized sets are O(n+m) after
validation. Splitting and iteration are O(k), where `k` is checked against the
output limit. No public operation may use an algorithm exponential in input
cardinality.
