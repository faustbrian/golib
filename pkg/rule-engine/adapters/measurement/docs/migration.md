# Migration

The current encoding replaces the historical unversioned form:

```text
quantity:<amount> <unit>
```

with:

```text
quantity:v1|<amount>|<unit>
```

Regenerate persisted rule literals and facts from validated
`measurement.Quantity` values by calling `Quantity`. Do not rewrite arbitrary
strings in place: parse legacy values with an explicit canonical measurement
profile, validate the unit and amount, then encode the resulting quantity.

Deploy readers that understand v1 before writing v1 values. The adapter does
not accept both formats because permissive dual parsing would make persisted
identity and retirement of the legacy grammar ambiguous.
