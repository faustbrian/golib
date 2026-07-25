# Operators

All operators require exact kinds. Missing operands return false. Null is only
comparable with null through equality. There is no coercion.

| Operator | Left | Right | Meaning |
| --- | --- | --- | --- |
| `equal`, `not_equal` | any same kind | same kind | structural equality |
| `less_than`, `less_or_equal` | int, float, string, time, duration | same kind | total ordering |
| `greater_than`, `greater_or_equal` | int, float, string, time, duration | same kind | total ordering |
| `in`, `not_in` | any | list | exact element membership |
| `contains` | string or list | string or any | substring or exact element |
| `starts_with`, `ends_with` | string | string | byte-exact UTF-8 affix |
| `matches` | string | string literal | Go RE2 regular expression |

Regex patterns are validated during compilation, limited by `MaxRegexBytes`,
and use Go's linear-time regular-expression implementation. Dynamic patterns
are rejected.

`All`, `Any`, and `Not` are logical combinators. Children execute left to
right. `All` stops on false; `Any` stops on true. Operator truth tables are
executable in [operator_truth_table_test.go](../operator_truth_table_test.go).
