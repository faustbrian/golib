# Structured-format conformance matrix

JSON, YAML, and TOML sources normalize their shared data model before merge.
The matrix below is the compatibility contract; differences are deliberate and
must not depend on parser defaults.

| Feature | JSON | YAML | TOML |
|---|---|---|---|
| Object root | required | mapping required | table required |
| Nested object | object | mapping | table or inline table |
| Array of objects | array | sequence | array of tables |
| String, boolean | native | strict core scalar | native |
| Signed integer | `int64` when possible | `int64` when possible | `int64` |
| Large unsigned integer | `uint64` when possible | `uint64` when possible | rejected above `int64` |
| Floating point | finite `float64` | finite `float64` | finite `float64` |
| Explicit null | normalized to `nil` | normalized to `nil` | not in the language |
| Date and time syntax | ordinary string only | timestamp normalized to string | all four date/time types normalized to strings |
| Duplicate key | rejected | rejected | rejected |
| Multiple roots/documents | rejected | rejected | not applicable |
| Aliases and merge keys | not applicable | rejected | not applicable |
| Custom tags | not applicable | rejected | not applicable |
| Non-string object key | not in the language | rejected | not in the language |
| NaN and infinity | rejected | rejected | rejected during normalization |
| Comments | not supported | supported | supported |

Equivalent documents produce the same canonical tree for objects, arrays,
strings, booleans, finite numbers, and nested arrays of objects. The executable
conformance tests also prove the intentional null and timestamp differences.

All formats require one object root and enforce caller-configurable byte,
depth, and key limits. Parser diagnostics expose safe category and location
metadata without retaining source values.
