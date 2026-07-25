# Serialization And Adapters

Quantity JSON is an object with decimal text and unit metadata:

```json
{"value":"12.50","unit":"kg"}
```

The decimal is always a string to prevent JSON-number precision loss. XML uses
`<quantity><value>12.50</value><unit>kg</unit></quantity>`. `driver.Valuer` and
`sql.Scanner` store the same JSON document in SQL text or JSON columns.
`Dimensions` JSON and XML preserve each side's original unit plus quantity.

Direct JSON decoding rejects numbers, unknown fields, trailing values,
duplicate fields, unsupported units, invalid decimals, and documents beyond
`MaxSerializedBytes`. Text uses canonical symbols and `MaxTextBytes`.

Direct XML decoding rejects unknown and duplicate fields. Core XML callbacks
cannot bound bytes already read by a caller-owned decoder, so untrusted XML
must enter through the bounded `measurementwire` adapter.

The optional `measurementwire` package uses `wire/jsonwire` and
`wire/xmlwire` with caller-selected byte limits. It supports only explicit
JSON and XML formats. Use this adapter at untrusted streaming boundaries; core
`encoding/xml` callbacks cannot bound bytes already consumed by an external
decoder.
