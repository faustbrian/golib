# Persistence

The `encoding` package writes deterministic version-1 JSON:

```json
{"version":1,"amount":"12.30","currency":"EUR","context":{"kind":"default","scale":2}}
```

Amounts are JSON strings. Decoding rejects unknown fields, trailing input,
unknown versions, unknown currencies, incompatible default metadata, impossible
contexts, excessive bytes, and precision-losing amounts. Historic ISO codes are
accepted explicitly during reconstruction.

`SQLMoney` implements `database/sql.Scanner` and `driver.Valuer` using the same
versioned representation. `NumericValue` and `ScanNumeric` support PostgreSQL
`numeric` columns when currency and context are stored in separate columns.

Recommended schema fields are version, exact numeric/text amount, currency code,
context kind, scale, cash step, and any business-level rounding or rate metadata.
Do not store only a binary float or infer historic metadata during reads.
