# Exactness And Rounding

All amounts and constants are `math/decimal.Decimal`. No package operation
converts through `float32` or `float64`. SI prefixes and international inch,
foot, yard, pound, and ounce definitions are finite decimal ratios. Fahrenheit
uses the exact affine ratio `(F + 459.67) * 5 / 9` before rounding.

`ExactConversion()` rejects a non-terminating base-10 quotient with the shared
`math` conversion error. `RoundedConversion(scale, mode)` combines the full
source-to-target ratio and rounds exactly once at the requested fractional
scale. The zero `ConversionContext` is invalid, preventing ambient defaults.

`Quantity.Round` quantizes an existing amount. `Format` first converts with its
conversion context and then quantizes with its independent display scale. This
separation prevents carrier payload precision from becoming display policy.

Arithmetic uses `math` limits. `WithLimits` can apply tighter bounds. Invalid
limits, oversized exponents, coefficients, inputs, counts, and outputs fail
before unbounded work.
