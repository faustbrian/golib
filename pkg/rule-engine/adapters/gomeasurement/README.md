# Exact measurement rule operators

`rule-engine/adapters/gomeasurement` is an optional bridge between immutable
`measurement.Quantity` values and the rule engine's explicitly registered
custom operators. It compares compatible dimensions with
`measurement.ExactConversion()` and never supplies a unit or rounding policy.

## Quick start

```go
operators := ruleenginemeasurement.Operators()
compiler, err := ruleengine.NewCompilerWithOperators(
    ruleengine.DefaultLimits(),
    operators...,
)

limit := measurement.MustNew(decimal.New(1), measurement.Kilogram)
operand := ruleenginemeasurement.Quantity(limit)
```

Register the returned operators on each compiler that needs them. Registration
is caller-owned; importing this module changes no global registry and starts no
goroutine or I/O.

## Guarantees

- quantities use the canonical `quantity:v1|<amount>|<unit>` string tag;
- the decimal amount and canonical measurement unit symbol are both explicit;
- all five relations use exact compatible-unit conversion;
- malformed, noncanonical, unknown, oversized, incompatible, and
  unrepresentable inputs fail without coercion;
- operator and signature slices are fresh values safe for concurrent reuse;
- cancellation is checked before and between bounded evaluation stages.

## Tradeoffs

The adapter intentionally rejects exact conversions whose decimal result does
not terminate, even when a rounded result might be operationally useful.
Quantities remain strings to preserve the core rule engine's closed value-kind
model, so callers must use `Quantity` instead of constructing tags manually.

## Documentation

- [Encoding](docs/encoding.md)
- [Conversion and dimensions](docs/conversion.md)
- [Operators and API](docs/api.md)
- [Limits](docs/limits.md)
- [Security](docs/security.md)
- [Examples](docs/examples.md)
- [Adoption](docs/adoption.md)
- [Compatibility](docs/compatibility.md)
- [Migration](docs/migration.md)
- [FAQ](docs/faq.md)

Release history is in [CHANGELOG.md](CHANGELOG.md).

## Development

```console
make check MODULES=pkg/rule-engine/adapters/gomeasurement
```

## License

MIT.
