# measurement

`measurement` is an immutable, exact, unit-safe measurement package for
Track, Postal, Location, and logistics services. It uses
[`math`](../math) decimals exclusively and never requires binary
floating-point conversion.

```go
length := measurement.MustNew(decimal.MustParse("1.25"), measurement.Metre)
centimetres, err := length.Convert(
    measurement.Centimetre,
    measurement.ExactConversion(),
)
// centimetres.String() == "125.00 cm"
```

The package covers length, area, volume, mass, absolute temperature, density,
loading metre, dimensional weight, and rectangular package triples. Compatible
quantities can be converted, compared, added, subtracted, multiplied, divided,
rounded, clamped, counted, formatted, and serialized. Absolute temperatures
can be converted and compared but not added or subtracted because temperature
intervals are not part of the v1 model.

Every conversion selects either `ExactConversion()` or
`RoundedConversion(scale, mode)`. Unit aliases are accepted only through a
caller-selected `Profile`; no locale or preferred unit is inferred.

## Documentation

- [Quickstart](docs/quickstart.md)
- [API and supported units](docs/api.md)
- [Exactness and rounding](docs/exactness.md)
- [Standards and formula sources](docs/sources.md)
- [Dimensional analysis](docs/dimensional-analysis.md)
- [Logistics formulas](docs/logistics-formulas.md)
- [Serialization and adapters](docs/serialization.md)
- [Cookbook](docs/cookbook.md)
- [Migration from shipit/measurements](docs/migration.md)
- [Security](docs/security.md)
- [Performance](docs/performance.md)
- [Compatibility](docs/compatibility.md)
- [FAQ](docs/faq.md)

Run `make check` for all blocking local gates. See [CONTRIBUTING.md](CONTRIBUTING.md)
and [CHANGELOG.md](CHANGELOG.md).
