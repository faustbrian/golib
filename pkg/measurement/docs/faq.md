# FAQ

## Why not float64?

Carrier dimensions and divisors cross persistence and billing-adjacent
boundaries where binary rounding drift is unacceptable. `math/decimal`
keeps base-10 values exact and bounded.

## Why can exact conversion fail?

Some rational results, such as one metre in inches or Fahrenheit conversion,
repeat in base ten. Select an explicit rounded context for those boundaries.

## Why is loading metre not a length?

It represents occupied trailer floor capacity, not physical distance. A
separate dimension prevents accidental addition to parcel length.

## Why can I not subtract Celsius values?

Absolute temperatures and temperature intervals have different affine rules.
V1 models only absolute temperature, so interval arithmetic is rejected.

## Where are carrier divisors and stacking rules?

They are explicit constructor inputs owned by caller configuration. This
package contains formulas, not tariffs or packaging recommendations.

## How do I localize unit names?

Build a locale-specific `Profile` and pass it to `Parse`. Formatting emits the
explicit unit symbol; localized display belongs in the presentation layer.
