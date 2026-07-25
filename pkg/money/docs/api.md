# API reference

## Construction

`Parse` accepts strict exact decimal text, a validated currency code, and an
explicit context. `FromMinorUnits` accepts an arbitrary-precision `math`
integer. `ParseAmount`, `ParseRate`, `ParseTaxRate`, and `ParseDiscountRate`
apply their domain bounds without float conversion.

## Fixed money

`Money` exposes `Amount`, `Currency`, `Context`, `Valid`, `String`, `Sign`, and
`IsZero`. `Add`, `Sub`, `Compare`, and `Equal` require identical currency and
resolved context. `Neg` and `Abs` preserve both identities.

`MinorUnits` returns the exact integer coefficient. `EqualSplit` and `Allocate`
return immutable `AllocationResult` values. `MoneyBag` combines only entries
with identical currency and context.

## Rational operations

`Mul` and `Quo` take an exact `Rate` and return `RationalMoney`. `Ratio` returns
an exact signed relationship between compatible money values. `Round` is the
only path from a rational result to fixed `Money` and returns rounding
conditions.

## Policies and results

`AddTax`, `ExtractTax`, and `ApplyDiscount` return conserved result types.
`Convert` requires an `ExchangeRate` containing base, quote, exact rate,
observation time, and bounded source attribution.

Use `go doc github.com/faustbrian/golib/pkg/money` for signatures and package-level
examples.
