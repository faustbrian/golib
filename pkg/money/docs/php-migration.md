# Migration from PHP money libraries

For `moneyphp/money`, migrate the integer amount string with `FromMinorUnits`
and map the ISO code through `international/currency`. Preserve the PHP
currency exponent in an explicit context and verify it against current or
historic metadata.

For `brick/money`, preserve both amount and context. A default-context value can
use `DefaultContext`; custom-scale and cash-context values need the matching Go
constructor. Do not convert PHP decimal strings through JSON numbers or Go
floats.

Map rational intermediate values to `RationalMoney`, not prematurely rounded
`Money`. Map PHP rounding constants explicitly to `math` rounding modes.
Reproduce allocations, inclusive tax, exclusive tax, discount order, and cash
rounding with fixture comparisons before switching writes.

During dual reads, decode the legacy record, encode version-1 persistence, and
compare currency, context, exact minor units, and business result components.
