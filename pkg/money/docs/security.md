# Security model

## Bounds

Amounts are limited to 256 digits and 18 fractional places. Rates, cash steps,
ratio digits, allocation parts, bag entries, encoded bytes, formatted output,
intermediate rational bits, and diagnostics have separate fixed bounds.

## Rejection rules

The package rejects absent or unknown currencies, unavailable default minor
units, mismatched currency or context, negative or excessive rates, zero or
negative allocation weights, excessive part counts, non-terminating automatic
precision, malformed versions, and precision-losing construction.

## Ownership and concurrency

Public values are immutable. Mutable `math/big` internals are owned by
`math`; accessors return immutable values or defensive copies. Bag and
allocation slices are copied. Shared values and formatters are exercised under
the race detector.

## Privacy

Errors contain operation categories, not source monetary records or customer
identifiers. The package has no logger. Applications should avoid attaching
raw payment or customer data when wrapping errors.
