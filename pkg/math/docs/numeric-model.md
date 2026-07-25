# Numeric model

`Integer` is closed under exact integral operations. `Rational` preserves an
exact normalized fraction. `Decimal` stores `coefficient * 10^exponent` and can
distinguish numeric equality from representation equality. `Float` is binary
and inexact; every construction and operation requires a precision and
rounding policy.

There is deliberately no common arithmetic interface: division, closure,
rounding, and exceptional behavior differ. Prefer `int`, `uint`, or `float64`
for naturally bounded counters, indexes, protocol fields, and numerical work
whose binary error is acceptable.

