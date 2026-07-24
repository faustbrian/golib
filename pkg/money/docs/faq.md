# FAQ

## Why no float constructor?

Binary floats cannot represent many decimal fractions exactly. Callers must
provide exact text, minor units, `math` values, or validated persistence.

## Why does `1.230` fail in a scale-2 context?

The extra represented scale is a context difference. Rejecting it avoids silent
normalization. Parse `1.23` or select a scale-3 context explicitly.

## Why does multiplication return `RationalMoney`?

Rates can create repeating or higher-scale results. Returning a rational keeps
the value exact until the caller selects the accounting boundary.

## Does the package fetch exchange rates?

No. Conversion accepts only an injected exact directed rate with attribution.

## Can a `MoneyBag` total different currencies?

No single scalar total exists across currencies without an injected conversion
policy. The bag preserves each currency/context entry independently.
