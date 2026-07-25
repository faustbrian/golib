# Conditions and traps

Decimal results report `Rounded`, `Inexact`, `Overflow`, `Underflow`,
`Subnormal`, and `DivisionByZero`. Conditions remain available even when a
matching context trap returns an error. Callers must decide whether a rounded
result is acceptable; financial code commonly traps inexact conversion at
domain boundaries and explicitly quantizes display or settlement amounts.

