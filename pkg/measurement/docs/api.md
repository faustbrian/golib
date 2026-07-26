# API And Supported Units

`Quantity` owns a `decimal.Decimal` amount and `Unit`. `New` validates unit
identity; accessors return immutable values. `Convert`, `Add`, `Subtract`,
`Compare`, `Equal`, `Multiply`, `Divide`, `Round`, `Clamp`, `Times`, and
`Format` return new values.

| Dimension | Units |
| --- | --- |
| Dimensionless | `1` |
| Length | `mm`, `cm`, `m`, `km`, `in`, `ft`, `yd` |
| Area | `mm2`, `cm2`, `m2`, `in2`, `ft2` |
| Volume | `mm3`, `cm3`, `m3`, `mL`, `L`, `in3`, `ft3` |
| Mass | `mg`, `g`, `kg`, `t`, `oz`, `lb` |
| Absolute temperature | `K`, `degC`, `degF` |
| Density | `kg/m3`, `g/cm3` |
| Loading metre | `ldm` |

`Units(dimension)` returns a stable sorted copy of the catalog. V1 dimensions
are closed: unsupported derived exponents return `ErrUnsupportedDimension`.
Loading metres deliberately cannot combine with ordinary lengths.

`Dimensions` validates positive length, width, and height plus a bounded count.
It calculates floor area, package volume, total volume, and loading metres.
`VolumetricDivisor` and `VolumetricIndex` calculate dimensional mass.
