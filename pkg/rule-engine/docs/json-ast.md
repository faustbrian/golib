# JSON AST

The `jsonast` package implements grammar version `1`. The root object contains
`version`, `id`, optional `namespace`, `strategy`, and `rules`. Every rule has
`id`, optional `namespace`, `priority`, `tags`, `when`, and `derive`.

Predicate `kind` is one of `true`, `false`, `exists`, `compare`, `all`, `any`,
or `not`. Operands are `variable` with a segment array or `literal` with one
typed value. Values use `type` plus exactly one matching field. Null and
missing have no value field. Times use RFC 3339 with nanoseconds; durations
use signed nanoseconds.

Unknown fields, fields belonging to another variant, trailing JSON values,
unknown operators, invalid kinds, invalid paths, and every limit violation are
rejected. Source values are not copied into diagnostics.

Canonical output fixes field order, sorts rules by evaluation order, sorts and
deduplicates tags, sorts derived facts by path, normalizes times to UTC, and
uses stable typed representations. Logical child order and list order remain
unchanged because they are semantic.

The maintained grammar fixture is
[location-routing.json](../jsonast/testdata/location-routing.json). JMESPath,
SQL-like, and GraphQL-like DSLs are not implemented; accepting those strings
would imply undocumented grammar and security behavior.
