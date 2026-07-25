# FAQ

## Is this a translation catalog?

No. It stores one immutable localized domain value. It does not load messages,
format templates, pluralize, interpolate, detect languages, or call translation
services.

## Why does `Get` not fall back?

Exact presence is a different question from preference matching or application
fallback. Separating them prevents hidden defaults and makes missing data
observable.

## Is an empty string missing?

No. `Get` returns `("", true)` for present-empty and `("", false)` for missing.

## Why are entries sorted?

Go map order is intentionally unstable. Canonical lexical ordering makes
iteration, hashes, tests, and encoding reproducible.

## Why no `Value[T]`?

Arbitrary `T` can contain caller-owned maps, slices, pointers, or mutable
objects. v1 avoids a generic API until a clone/ownership contract remains both
safe and comprehensible.

## Does normalization happen automatically?

No. User-authored text is preserved. Call an explicit normalization function
and retain its returned value.

## Are strings HTML safe?

No. Escape for HTML, JSON, SQL, shell, or other output contexts at that
boundary. SQL adapters pass values as parameters; they do not construct SQL.

## Can I configure a default locale globally?

No. Pass a fallback plan at the operation or application boundary.
