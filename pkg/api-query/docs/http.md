# Conventional HTTP integration

Pass the raw query string and `schema.Bounds().MaxRequestBytes` (or a stricter
positive byte limit) to `apiqueryhttp.Parse`.
Supported names are `schema_revision`, `fields`, `include`, `filter`, `sort`,
`page[mode]`, `page[size]`, `page[after]`, `page[before]`, and `page[offset]`.

Fields and includes are comma-separated. Sorts are comma-separated and use a
leading `-` for descending order. Filter is strict JSON for `FilterExpr` after
URL decoding. Empty `fields`, `include`, or `sort` is explicitly empty; absence
remains absent.

The parser rejects invalid percent encoding, malformed UTF-8, unknown names,
repeated names (even with equal values), empty list members, invalid integers,
malformed or duplicate JSON members, trailing JSON, and excess bytes. HTTP
servers should also enforce request-line and header limits before calling it.

Example:

```text
schema_revision=orders-v1&fields=id,status&sort=-created_at,id&page%5Bmode%5D=cursor&page%5Bsize%5D=25
```

Compile the returned request with the same schema and request-scoped policy used
by other transports. Cross-transport conformance tests should compare canonical
plans, not raw syntax.
