# Encoding

`Quantity` produces a rule-engine string with this v1 grammar:

```text
quantity:v1|<exact-decimal>|<canonical-unit-symbol>
```

For example, one and a half kilograms is `quantity:v1|1.5|kg`. The amount is
the canonical non-exponent text returned by `decimal.Decimal.String`; its
coefficient and scale are therefore preserved without a binary floating-point
path. The unit is the exact stable symbol from `measurement.Unit`, including
case (`mL`, `L`, `K`) and dimension-specific identity (`m` versus `ldm`).

The version is part of the tag. Evaluators accept only v1 and reject the old
unversioned `quantity:<amount> <unit>` form, aliases, whitespace, exponent
notation, leading-zero variants, missing fields, extra fields, and unknown
units. There is no default unit.

Persist the complete rule-engine string unchanged. Reconstructing a tag from
localized display text is unsupported because display profiles may contain
aliases that are not canonical unit identities.
