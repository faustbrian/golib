# Quickstart

Construct decimals with `math/decimal`, then attach a supported unit:

```go
mass := measurement.MustNew(decimal.MustParse("12.50"), measurement.Kilogram)
grams, err := mass.Convert(measurement.Gram, measurement.ExactConversion())
```

Use `New`, not `MustNew`, for untrusted or dynamically selected units. Parsing
requires a profile:

```go
profile, err := measurement.NewProfile(map[string]measurement.Unit{
    "metres": measurement.Metre,
})
length, err := measurement.Parse("1.25 metres", profile)
```

Choose exact conversion when a non-terminating result must fail. Choose rounded
conversion with an explicit fractional scale and `math` rounding mode at
interoperability boundaries. Use `Format` when conversion and final display
rounding are separate decisions.
