# math

`math` is an immutable arbitrary-precision numeric foundation for Go. It
provides distinct APIs for signed integers, exact rationals, finite base-10
decimals, and explicitly inexact binary floats. Use ordinary Go numeric types
when their fixed width and machine arithmetic are sufficient.

```go
amount := decimal.MustParse("19.995")
result, err := amount.Quantize(
	context.Background(), 2, decimal.HalfEven, gomath.DefaultLimits(),
)
```

All constructors copy mutable `math/big` inputs. Operations return new values,
accessors return copies, JSON uses strings, and potentially expensive work is
bounded by `gomath.Limits`. Decimal contexts make precision, exponent range,
rounding, conditions, and traps explicit. No conversion passes through
`float64`.

See the [documentation index](docs/README.md), [cookbook](docs/cookbook.md),
and [verification guide](docs/verification.md). The minimum supported toolchain
is Go 1.26.5.

## Packages

- `integer`: exact signed integer arithmetic, roots, GCD/LCM, and unbiased
  injected-source random values.
- `rational`: normalized exact fractions and bounded decimal conversion.
- `decimal`: finite coefficient/exponent values, exact operations, and
  context-rounded operations with conditions.
- `bigfloat`: explicit precision and rounding around `math/big.Float`.
- `encoding`: deterministic versioned binary codecs.
- `mathtest`: reusable algebraic-law and round-trip assertions.

## Development

Run `make check` for blocking local gates and `make check-all` to include the
advisory NilAway analysis. See [CHANGELOG.md](CHANGELOG.md) for releases and
[SECURITY.md](SECURITY.md) for vulnerability reporting.

Licensed under MIT.
