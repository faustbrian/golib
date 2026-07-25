# Precision and rounding

Exact integer, rational, and decimal operations never round. Decimal `Context`
operations use significant decimal digits; quantization uses an explicit
fractional scale. Supported modes are half-even, half-up, half-down, down,
up, ceiling, and floor. Binary float operations use the rounding modes that
`math/big.Float` can represent and reject unsupported policies.

| Mode | Tie or discarded remainder behavior |
| --- | --- |
| half-even | nearest; an exact tie chooses an even retained digit |
| half-up | nearest; an exact tie increases the magnitude |
| half-down | nearest; an exact tie decreases the magnitude |
| down | toward zero |
| up | away from zero |
| ceiling | toward positive infinity |
| floor | toward negative infinity |

`rational.Decimal`, decimal context arithmetic, `Decimal.Quantize`, and
`QuantizedQuo` exercise every mode for positive and negative operands. The GDA
rounding vectors independently cover residue and tie behavior. Binary floats
support half-even, half-up, down, up, ceiling, and floor; half-down is rejected
because `math/big.Float` has no equivalent mode.

Exact operations return either the exact value or an error. Context operations
return `Rounded` whenever digits were discarded and additionally return
`Inexact` when discarded digits were nonzero. Overflow, subnormal, underflow,
division-by-zero, and invalid-operation paths are executable in context and
edge tests. A selected trap returns both the condition-bearing result and an
`ErrTrappedCondition` error.

Exact integer, rational, and decimal addition obeys commutativity,
associativity, and the additive identity. `laws_test.go` applies those laws to
the production types. Finite-precision decimal and binary-float addition is
not associative: rounding after each operation can discard a small term. The
same test records executable counterexamples rather than exposing an algebraic
contract that finite precision cannot satisfy.

Decimal precision is significant digits, while quantization scale is the count
of fractional decimal places and may be negative. Binary precision is bits.
These units are never inferred or converted implicitly.

Never infer precision from a `float64`. Construct from strings, integers,
`big.Int`, or `big.Rat`, then inspect the returned conditions.
